package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

var refCounter = regexp.MustCompile(`(\d+)–(\d+)/(\d+)`)

// ? is sectioned this row / this tab / navigation / g goto › / global, one column
// below 100 cols (Spec mc-bruk D6, FR19–22, 26).
func TestRefSectionsInOrder(t *testing.T) {
	v := ansi.Strip(press(t, graphModel(t, 80, 60), runeKey("?")).View())
	at := -1
	for _, h := range []string{"keys — Graph", "this row", "this tab", "navigation", "g goto ›", "global"} {
		i := strings.Index(v, h)
		if i <= at {
			t.Fatalf("%q out of order or missing:\n%s", h, v)
		}
		at = i
	}
	if strings.Contains(v, "tmux") {
		t.Errorf("tmux section in ?:\n%s", v)
	}
}

// A tab with no row verbs keeps the heading with a dim line (FR21).
func TestRefHeaderRowShowsNothing(t *testing.T) {
	m := press(t, sized(t, 80, 60), runeKey("6"))
	if v := ansi.Strip(press(t, m, runeKey("?")).View()); !strings.Contains(v, "nothing for this row") {
		t.Errorf("no nothing-for-this-row line:\n%s", v)
	}
}

// Only this row gets the dim line; an empty navigation section is omitted whole (FR21).
func TestRefNothingOnlyUnderThisRow(t *testing.T) {
	for _, k := range []string{"6"} {
		v := ansi.Strip(press(t, press(t, sized(t, 80, 70), runeKey(k)), runeKey("?")).View())
		if n := strings.Count(v, "nothing for this row"); n != 1 {
			t.Errorf("tab %s: %d nothing lines:\n%s", k, n, v)
		}
		if i, j := strings.Index(v, "nothing for this row"), strings.Index(v, "navigation"); j >= 0 && j < i+60 && j > i && !strings.Contains(v[j:], "j/k") {
			t.Errorf("tab %s: empty navigation heading:\n%s", k, v)
		}
	}
}

// From 100 cols the box has two columns: g goto › sits beside this row (FR22).
func TestRefTwoColumnsFrom100(t *testing.T) {
	v := ansi.Strip(press(t, graphModel(t, 120, 40), runeKey("?")).View())
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "this row") && strings.Contains(l, "g goto ›") {
			return
		}
	}
	t.Errorf("not two columns:\n%s", v)
}

// At 40x12 every key is reachable by scrolling; title, footer and counter stay (FR23–24).
func TestRefScrollsAtFloor(t *testing.T) {
	m := press(t, graphModel(t, floorCols, floorRows), runeKey("?"))
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		v := ansi.Strip(m.View())
		if !strings.Contains(v, "keys — Graph") || !strings.Contains(v, "j/k scroll • ?/esc close") {
			t.Fatalf("chrome scrolled off:\n%s", v)
		}
		c := refCounter.FindStringSubmatch(v)
		if c == nil {
			t.Fatalf("no counter:\n%s", v)
		}
		for _, l := range strings.Split(v, "\n") {
			seen[strings.TrimSpace(l)] = true
		}
		if c[2] == c[3] {
			break
		}
		m = press(t, m, runeKey("j"))
	}
	for _, want := range []string{"quit", "next tab", "loop"} {
		found := false
		for l := range seen {
			found = found || strings.Contains(l, want)
		}
		if !found {
			t.Errorf("%q never reachable by scrolling", want)
		}
	}
	top := press(t, m, runeKey("g"))
	if c := refCounter.FindStringSubmatch(ansi.Strip(top.View())); c == nil || c[1] != "1" {
		t.Errorf("g did not reach the top: %v", c)
	}
	bot := press(t, top, runeKey("G"))
	if c := refCounter.FindStringSubmatch(ansi.Strip(bot.View())); c == nil || c[2] != c[3] {
		t.Errorf("G did not reach the bottom: %v", c)
	}
	if back := press(t, top, runeKey("k")); back.View() != top.View() {
		t.Error("k scrolled above the top")
	}
}

// Reopening ? starts at the top.
func TestRefResetsScrollOnOpen(t *testing.T) {
	m := press(t, graphModel(t, floorCols, floorRows), runeKey("?"))
	m = press(t, press(t, m, runeKey("G")), runeKey("?"))
	if c := refCounter.FindStringSubmatch(ansi.Strip(press(t, m, runeKey("?")).View())); c == nil || c[1] != "1" {
		t.Errorf("reopened at %v", c)
	}
}

// ?, esc and q close it (q does not quit), ctrl+c quits, the rest is swallowed (FR25).
func TestRefCloseKeys(t *testing.T) {
	base := graphModel(t, 100, 30)
	open := press(t, base, runeKey("?"))
	for _, k := range []tea.KeyMsg{runeKey("?"), {Type: tea.KeyEsc}, runeKey("q")} {
		closed, cmd := send(t, open, k)
		if cmd != nil || closed.View() != base.View() {
			t.Errorf("%s did not just close the reference", k)
		}
	}
	if _, cmd := send(t, press(t, sized(t, 100, 30), runeKey("?")), tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Error("ctrl+c did not quit")
	}
	if next, cmd := send(t, open, runeKey("x")); cmd != nil || !next.ref {
		t.Error("x was not swallowed")
	}
}

func TestRefModeBindingsCoverScroll(t *testing.T) {
	m := press(t, graphModel(t, 100, 30), runeKey("?"))
	have := map[string]bool{}
	for _, b := range m.modeBindings() {
		for _, k := range b.Keys() {
			have[k] = true
		}
	}
	for _, k := range []string{"j", "k", "down", "up", "pgdown", "pgup", "ctrl+u", "ctrl+d", " ", "g", "G", "?", "esc", "q", "ctrl+c"} {
		if !have[k] {
			t.Errorf("modeBindings under ? lacks %q", k)
		}
	}
}

// Headings are the brand style in both layouts; the empty placeholder stays dim.
func TestRefHeadingsAreBrand(t *testing.T) {
	forceANSI256(t)
	heads := []string{"this row", "this tab", "navigation", "g goto ›", "global"}
	for _, h := range heads {
		if brand.Render(h) == dimSt.Render(h) {
			t.Fatalf("brand and dim render %q alike: test would be vacuous", h)
		}
	}
	for _, w := range []int{80, 120} {
		v := strings.Join(bodyOf(t, press(t, graphModel(t, w, 60), runeKey("?"))), "\n")
		for _, h := range heads {
			if !strings.Contains(v, brand.Render(h)) {
				t.Errorf("w=%d: heading %q not brand-styled:\n%q", w, h, v)
			}
		}
		v = strings.Join(bodyOf(t, press(t, press(t, sized(t, w, 60), runeKey("6")), runeKey("?"))), "\n")
		if !strings.Contains(v, brand.Render("this row")) {
			t.Errorf("w=%d: empty this row heading not brand-styled", w)
		}
		if !strings.Contains(v, dimSt.Render("nothing for this row")) {
			t.Errorf("w=%d: placeholder not dim", w)
		}
	}
}

func bodyOf(t *testing.T, m Model) []string {
	t.Helper()
	body, _, _ := m.refLayout()
	return body
}
