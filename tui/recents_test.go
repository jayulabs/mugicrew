package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// recentsModel is a loaded model whose common dir is a temp dir; the file is seeded
// with the ids, most recent first.
func recentsModel(t *testing.T, beads []bd.Bead, ids ...string) Model {
	t.Helper()
	p := recentsFile(t)
	seam(t, &recentsPath, func() string { return p })
	if len(ids) > 0 {
		b := `["` + strings.Join(ids, `","`) + `"]`
		seedRecents(t, p, b)
	}
	m := sized(t, 120, 30)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}})
	return m
}

func recentsOpen(m Model) bool { return len(m.modals) > 0 && m.modals[len(m.modals)-1].recents }

func TestRecentsRInEveryTabAndFocus(t *testing.T) {
	m := recentsModel(t, focusFixture(), "s1")
	for _, k := range []string{"1", "2", "3", "4", "5", "6", "#", "$", "*"} {
		m = press(t, m, runeKey(k))
		m = press(t, m, runeKey("R"))
		if !recentsOpen(m) {
			t.Errorf("R on tab %s opened no Recents", k)
		}
		m = press(t, m, runeKey("R"))
		if len(m.modals) != 0 {
			t.Errorf("R did not close Recents on tab %s", k)
		}
	}
	m = ideaFocus(t, m)
	m = press(t, m, runeKey("R"))
	if !recentsOpen(m) {
		t.Fatal("R in the focus view opened no Recents")
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if len(m.modals) != 0 || !focused(m) {
		t.Errorf("esc: modals %d, focused %v; want closed and still focused", len(m.modals), focused(m))
	}
}

func TestRecentsEmptyOpensNothing(t *testing.T) {
	m := press(t, recentsModel(t, focusFixture()), runeKey("R"))
	if len(m.modals) != 0 || m.status != "no recent beads yet" {
		t.Errorf("modals %d, status %q", len(m.modals), m.status)
	}
}

func TestRecentsShellKeysInert(t *testing.T) {
	m := press(t, recentsModel(t, focusFixture(), "s1", "s2"), runeKey("R"))
	for _, k := range []string{"2", "o", "?", "tab", "q"} {
		m = press(t, m, runeKey(k))
		if !recentsOpen(m) || m.tab != tabIdeate || m.ref {
			t.Fatalf("shell key %q acted under Recents", k)
		}
	}
	m = press(t, m, runeKey("j"))
	if m.modals[0].cur != 1 {
		t.Errorf("j did not move: cur %d", m.modals[0].cur)
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // jumps to the bead
	if len(m.modals) != 0 || m.tab != tabIdeate {
		t.Errorf("enter: modals %d, tab %d", len(m.modals), m.tab)
	}
}

func TestRecentsHelpAndHints(t *testing.T) {
	m := press(t, recentsModel(t, focusFixture(), "s1"), runeKey("?"))
	if v := view(m); !strings.Contains(v, "R") || !strings.Contains(v, "recents") {
		t.Errorf("? lacks R recents:\n%s", v)
	}
	m = press(t, m, runeKey("?"))
	if bar := lines(m.View())[m.h-1]; strings.Contains(bar, "recents") {
		t.Errorf("hints bar lists recents: %q", bar)
	}
	m = press(t, m, runeKey("R"))
	if v := view(m); !strings.Contains(v, "j/k move • enter jump • esc/R close") {
		t.Errorf("modal hints lack keys:\n%s", v)
	}
}

func TestRecentsRowKeepsIDAndTruncatesTitle(t *testing.T) {
	long := dbead("longone", contract.KindSignal, "open", "", 1, titled(strings.Repeat("wide title ", 12)))
	m := press(t, recentsModel(t, []bd.Bead{long}, "longone"), runeKey("R"))
	var row string
	for _, l := range lines(m.View()) {
		if strings.Contains(l, "longone") {
			row = l
		}
	}
	if !strings.Contains(row, "…") || !strings.Contains(row, "SIGNAL") || !strings.Contains(row, "○") {
		t.Errorf("row %q", row)
	}
	if i := strings.Index(row, "│"); i < 0 || lipgloss.Width(row) > 120 {
		t.Errorf("row %q", row)
	}
	// "longone" sits right before the box's closing border
	if !strings.Contains(row, "longone │") {
		t.Errorf("id not kept at the end: %q", row)
	}
}

func TestRecentsClosedRowDim(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	bs := []bd.Bead{
		dbead("done", contract.KindSignal, "closed", "", 1, titled("finished work")),
		dbead("live", contract.KindSignal, "open", "", 1, titled("open work")),
	}
	m := press(t, recentsModel(t, bs, "done", "live"), runeKey("R"))
	var done, live string
	for _, l := range strings.Split(m.View(), "\n") {
		if strings.Contains(l, "finished work") {
			done = l
		}
		if strings.Contains(l, "open work") {
			live = l
		}
	}
	if !strings.Contains(done, dimSt.Render("✓ SIGNAL P1 · finished work · done")) {
		t.Errorf("closed row not dim: %q", done)
	}
	if strings.Contains(live, dimSt.Render("○ SIGNAL P1 · open work · live")) {
		t.Errorf("open row dim: %q", live)
	}
}

func TestRecentsSkipsIDsNotInSnapshot(t *testing.T) {
	m := press(t, recentsModel(t, focusFixture(), "ghost", "s1"), runeKey("R"))
	if n := len(m.modals[0].items); n != 1 {
		t.Errorf("%d rows, want 1", n)
	}
}

func TestDetailOpenRecordsAndRListsIt(t *testing.T) {
	m := recentsModel(t, focusFixture(), "s2")
	m = press(t, m, runeKey("4"))
	m = selectBead(t, m, "s1")
	m = press(t, m, runeKey("d"))
	m = press(t, m, runeKey("R"))
	if !recentsOpen(m) {
		t.Fatal("R opened no Recents after d")
	}
	if first := m.modals[0].items[0]; !strings.Contains(first, "s1") {
		t.Errorf("top row %q, want s1", first)
	}
}

func TestDetailClosingOrBlockedDoesNotRecord(t *testing.T) {
	m := recentsModel(t, focusFixture())
	m = press(t, m, runeKey("4"))
	m = selectBead(t, m, "s1")
	m = press(t, m, runeKey("d")) // opens: recorded
	m = press(t, m, runeKey("d")) // shuts
	p := recentsPath()
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	m = press(t, m, runeKey("d")) // reopens: records again
	if got := loadRecents(p); len(got) != 1 || got[0] != "s1" {
		t.Errorf("after reopen %v", got)
	}
	m = press(t, m, runeKey("j")) // a cursor move
	if got := loadRecents(p); len(got) != 1 {
		t.Errorf("cursor move recorded: %v", got)
	}
}

func TestFailingSaveShowsStatusDrawerStillOpens(t *testing.T) {
	m := recentsModel(t, focusFixture())
	recentsPath = func() string { return "" }
	m = press(t, m, runeKey("4"))
	m = selectBead(t, m, "s1")
	m = press(t, m, runeKey("d"))
	if !m.tabAt(tabGraph).(graphTab).open {
		t.Error("drawer did not open")
	}
	if !strings.Contains(m.status, "recents") {
		t.Errorf("status %q", m.status)
	}
}

// A Recents row is its SBRN with no dot column.
func TestRecentsRowIsSBRN(t *testing.T) {
	m := press(t, recentsModel(t, []bd.Bead{sig("s1", "open", 1, "")}, "s1"), runeKey("R"))
	if v := ansi.Strip(m.View()); !strings.Contains(v, "○ SIGNAL P1 · sig s1 · s1") {
		t.Errorf("recents row not SBRN:\n%s", v)
	}
}
