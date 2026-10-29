package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"jayulabs.org/mugicrew/mdrender"
)

// longBody is a synthetic ~35 KB Markdown body, the shape the drawer prototype measured.
func longBody() string {
	var b strings.Builder
	for i := 0; b.Len() < 35_000; i++ {
		b.WriteString("## Section\n\nA paragraph with **bold**, `code` and enough words to wrap inside the drawer at a narrow width, so the wrapped text has many lines.\n\n- a bullet\n  - nested\n\n")
	}
	return b.String()
}

// uncachedVP is drawerVP as it was before the memo.
func uncachedVP(open bool, w, h int, text string, off int) viewport.Model {
	vp := viewport.New(drawerContentW(w), max(h-drawerH(open, h)-2, 1))
	vp.SetContent(lipgloss.NewStyle().Width(vp.Width).Render(text))
	vp.SetYOffset(off)
	return vp
}

func TestCachedDrawerRendersLikeUncached(t *testing.T) {
	body := longBody()
	for _, w := range []int{40, 100, 40, 100} { // the repeats hit the memo
		raw := strings.Trim(mdrender.Render(body, drawerContentW(w)), "\n")
		if got := mdBody(body, w); got != raw {
			t.Fatalf("w=%d: cached markdown differs from uncached", w)
		}
		if got, want := drawerVP(true, w, 30, raw, 5).View(), uncachedVP(true, w, 30, raw, 5).View(); got != want {
			t.Fatalf("w=%d: cached drawer view differs from uncached", w)
		}
	}
}

func TestDrawerMemoMissesOnChangedBody(t *testing.T) {
	a, b := "# one\n\nalpha", "# one\n\nbeta"
	if mdBody(a, 60) == mdBody(b, 60) {
		t.Error("a changed body rendered the cached text")
	}
	if drawerVP(true, 60, 30, mdBody(a, 60), 0).View() == drawerVP(true, 60, 30, mdBody(b, 60), 0).View() {
		t.Error("a changed body showed the cached drawer")
	}
	if mdBody(a, 60) == mdBody(strings.Repeat("word ", 40)+a, 20) {
		t.Error("sanity")
	}
	long := strings.Repeat("word ", 60)
	if mdBody(long, 30) == mdBody(long, 90) {
		t.Error("a changed width rendered the cached text")
	}
}

func TestWheelEventScrollsDrawerOneLine(t *testing.T) {
	forceANSI256(t)
	s := listSurfaces()[0]
	m := press(t, s.model(t, 40), runeKey("d"))
	g := graphOf(m)
	for i := range g.beads {
		if g.beads[i].ID == g.sel {
			g.beads[i].Description = longBody()
		}
	}
	m.tabs[m.tab] = g
	for i := 1; i <= 3; i++ {
		m = wheel(t, m, 30, false)
		if got := graphOf(m).off; got != i {
			t.Fatalf("after %d wheel events the drawer is at %d, want %d", i, got, i)
		}
	}
	if m = wheel(t, m, 30, true); graphOf(m).off != 2 {
		t.Errorf("wheel up: offset %d, want 2", graphOf(m).off)
	}
}

// drawerWheelGate is how many times cheaper a wheel event over a long body must be with the
// drawer memo warm than with it cold (the Spec measured about 3 ms against 10-20 ms).
const drawerWheelGate = 1.5

// TestDrawerWheelStaysUnderUncachedCost times a wheel event through Update and View on a
// Model whose drawer is open on the 35 KB body, once with the memo warm and once with it
// cleared before every event (the uncached cost: Markdown render plus the lipgloss wrap), so
// a memo that misses on every frame fails here.
func TestDrawerWheelStaysUnderUncachedCost(t *testing.T) {
	if testing.Short() {
		t.Skip("times a benchmark")
	}
	forceANSI256(t)
	s := listSurfaces()[0]
	m := press(t, s.model(t, 40), runeKey("d"))
	g := graphOf(m)
	for i := range g.beads {
		if g.beads[i].ID == g.sel {
			g.beads[i].Description = longBody()
		}
	}
	m.tabs[m.tab] = g
	m = m.WithMouse(true)
	m = wheel(t, m, 30, false)
	event := func(cold bool) int64 {
		return testing.Benchmark(func(b *testing.B) {
			for i := 0; b.Loop(); i++ {
				if cold {
					mdMemo.mu.Lock()
					mdMemo.es = nil
					mdMemo.mu.Unlock()
					wrapMemo.mu.Lock()
					wrapMemo.es = nil
					wrapMemo.mu.Unlock()
				}
				m, _ = send(t, m, tea.MouseMsg{X: 5, Y: 30, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
				if i%50 == 49 { // back up so the offset never pins at the end
					m = wheel(t, m, 30, true)
				}
				_ = m.View()
			}
		}).NsPerOp()
	}
	warm, cold := event(false), event(true)
	t.Logf("wheel event %.2f ms cached, %.2f ms uncached", float64(warm)/1e6, float64(cold)/1e6)
	if float64(warm)*drawerWheelGate > float64(cold) {
		t.Errorf("a cached wheel event costs %.2f ms against %.2f ms uncached; gate: ×%.1f cheaper", float64(warm)/1e6, float64(cold)/1e6, drawerWheelGate)
	}
}

func BenchmarkDrawerWheel(b *testing.B) {
	body, w := longBody(), 120
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		vp := drawerVP(true, w, 40, mdBody(body, w), i%100)
		_ = wheelDrawer(vp, 1)
	}
}
