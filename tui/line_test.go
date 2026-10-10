package tui

import (
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// The single-line input's keys (Spec FR8–FR11): text inserts at the
// cursor, ← / → move one rune, home / end jump, backspace and delete edit around it.
func TestLineKeys(t *testing.T) {
	k := func(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }
	cases := []struct {
		name    string
		start   line
		keys    []tea.KeyMsg
		want    string
		wantCur int
	}{
		{"insert mid-text", line{}, []tea.KeyMsg{runeKey("helo"), k(tea.KeyLeft), runeKey("l")}, "hello", 4},
		{"home then delete", lineAt("hello"), []tea.KeyMsg{k(tea.KeyHome), k(tea.KeyDelete)}, "ello", 0},
		{"ctrl+a / ctrl+e", lineAt("ab"), []tea.KeyMsg{k(tea.KeyCtrlA), runeKey("x"), k(tea.KeyCtrlE), runeKey("y")}, "xaby", 4},
		{"end jumps", line{"ab", 0}, []tea.KeyMsg{k(tea.KeyEnd), runeKey("c")}, "abc", 3},
		{"space at cursor", lineAt("ab"), []tea.KeyMsg{k(tea.KeyLeft), k(tea.KeySpace)}, "a b", 2},
		{"backspace before cursor", lineAt("abc"), []tea.KeyMsg{k(tea.KeyLeft), k(tea.KeyBackspace)}, "ac", 1},
		{"backspace at 0 is a no-op", line{"abc", 0}, []tea.KeyMsg{k(tea.KeyBackspace)}, "abc", 0},
		{"delete at end is a no-op", lineAt("abc"), []tea.KeyMsg{k(tea.KeyDelete)}, "abc", 3},
		{"left at 0, right at end stay", line{"ab", 0}, []tea.KeyMsg{k(tea.KeyLeft), k(tea.KeyEnd), k(tea.KeyRight)}, "ab", 2},
		{"multi-byte runes", lineAt("héé✓"), []tea.KeyMsg{k(tea.KeyLeft), k(tea.KeyBackspace), k(tea.KeyLeft), k(tea.KeyDelete)}, "h✓", 1},
		{"other keys ignored", lineAt("ab"), []tea.KeyMsg{k(tea.KeyUp), k(tea.KeyTab)}, "ab", 2},
	}
	for _, c := range cases {
		l := c.start
		for _, msg := range c.keys {
			l = l.apply(msg)
		}
		if l.text != c.want || l.cur != c.wantCur {
			t.Errorf("%s: %q@%d, want %q@%d", c.name, l.text, l.cur, c.want, c.wantCur)
		}
	}
}

// FR12: the cursor is a reverse-video cell on the rune under it, or the ▏ block at the end.
func TestLineRender(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	defer lipgloss.SetColorProfile(prev)
	if got, want := lineAt("ab").render(), "ab"+dim.Render("▏"); got != want {
		t.Errorf("at end: %q, want %q", got, want)
	}
	if got, want := (line{"héllo", 1}).render(), "h"+cursorSty.Render("é")+"llo"; got != want || cursorSty.Render("é") == "é" {
		t.Errorf("mid: %q, want %q", got, want)
	}
}

// FR11, FR8, FR10, FR12 through Update: t opens the title pre-filled with the
// cursor at the end, ← moves it, typing inserts mid-title and the cursor draws there.
func TestTitleEditMidText(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	defer lipgloss.SetColorProfile(prev)
	m := New(nil, nil)
	m.beads, m.w, m.h = []bd.Bead{{ID: "grl1", Kind: contract.KindGrilling, Status: "open", Title: "Fix tpyo"}}, 120, 24
	m.rebuildGroups()
	m.selGraph = "grl1"
	m, _ = send(t, m, runeKey("e"))
	m, _ = send(t, m, runeKey("t"))
	m = typeText(t, m, "!")
	if m.editBuf.text != "Fix tpyo!" {
		t.Fatalf("pre-filled typing: %q, want appended", m.editBuf.text)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	for _, k := range []tea.KeyType{tea.KeyLeft, tea.KeyLeft, tea.KeyBackspace, tea.KeyRight} { // Fix tp|yo → Fix ty|o
		m, _ = send(t, m, tea.KeyMsg{Type: k})
	}
	m = typeText(t, m, "p")
	if m.editBuf.text != "Fix typo" {
		t.Errorf("mid-text: %q, want Fix typo", m.editBuf.text)
	}
	if !strings.Contains(m.helpLine(), "Fix typ"+cursorSty.Render("o")) {
		t.Errorf("cursor not drawn on o: %q", m.helpLine())
	}
}

// FR13: tab switches field and back without losing either field's cursor.
func TestCreateFormFieldCursor(t *testing.T) {
	m := New(nil, nil)
	m.w, m.h = 120, 24
	m = press(t, m, "n")
	m = typeText(t, m, "ac")
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = typeText(t, m, "xz")
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyHome})
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = typeText(t, m, "b")
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = typeText(t, m, "w")
	if m.createTitle.text != "abc" || m.createDesc.text != "wxz" {
		t.Errorf("fields %q / %q, want abc / wxz", m.createTitle.text, m.createDesc.text)
	}
}

// FR14: editing Search's text mid-string refilters on that change.
func TestSearchTextMidEdit(t *testing.T) {
	m := searchModel(t, 120)
	m.search.status = statusAny
	m, _ = send(t, m, runeKey("/"))
	m = typeText(t, m, "pivt grill")
	if _, l2 := searchHeader(m); !strings.Contains(l2, "0 of 5") {
		t.Fatalf("pivt grill: %q, want none", l2)
	}
	for range len(" grill") + 1 {
		m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	}
	m = typeText(t, m, "o")
	if l1, l2 := searchHeader(m); m.search.text != "pivot grill" || !strings.Contains(l2, "2 of 5") || !strings.HasPrefix(l1, "/ pivot grill") {
		t.Errorf("mid-edit: text %q header %q / %q, want pivot grill, 2 of 5", m.search.text, l1, l2)
	}
}
