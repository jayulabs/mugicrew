package tui

import (
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/session"
)

// One Ideas-tab frame builds the idea list a small fixed number of times (Spec mc-fydg
// FR6): the expandAll passes plus one for the expanded tree, plus one for the shown rows.
// With folded and unfolded headers that is 2 passes + 1 + 1 = 4; 5 leaves one pass of slack.
func TestIdeaViewBuildsListOncePerFrame(t *testing.T) {
	beads := []bd.Bead{ini("ini", "open", "Ship", 1), ini("ini2", "open", "Other", 2), sig("a", "in_progress", 1, "ini"), sig("r", "open", 2, "ini"),
		sig("o", "open", 2, "ini2"), sig("n", "open", 2, ""), sig("bk", "open", 4, "ini")}
	m := ideaModel(t, 100, 30, beads, map[string]session.Info{"a": {State: session.Alive}})
	m, _ = send(t, m, runeKey("j")) // move off the create row
	m = press(t, m, runeKey(" "))   // fold or unfold a header: mixed folds
	tab := m.tabAt(tabIdeate).(ideaTab)
	n := 0
	onIdeaListHeld = func() { n++ }
	defer func() { onIdeaListHeld = nil }()
	_ = tab.view(100, 30)
	if n > 5 {
		t.Errorf("view built the idea list %d times, want at most 5", n)
	}
	if n < 1 {
		t.Errorf("hook never fired")
	}
}
