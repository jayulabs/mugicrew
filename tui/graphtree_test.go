package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

func treeModel(t *testing.T) Model {
	t.Helper()
	forceANSI256(t)
	beads := []bd.Bead{
		{ID: "ini", Kind: contract.KindInitiative, Container: true, Status: "open", Title: "Ship it", Created: epoch},
		{ID: "ch", Kind: contract.KindChart, Container: true, Status: "open", Title: "Full chart", Parent: "ini", Created: epoch},
		{ID: "kid", Kind: contract.KindBuild, Status: "open", Title: "Kid one", Parent: "ch", Created: epoch.Add(time.Hour)},
		{ID: "sib", Kind: contract.KindBuild, Status: "open", Title: "Kid two", Parent: "ch", Created: epoch.Add(2 * time.Hour)},
		{ID: "bare", Kind: contract.KindChart, Container: true, Status: "open", Title: "Bare chart", Created: epoch},
	}
	m := sized(t, 120, 30)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: map[string]session.Info{}})
	m, _ = send(t, m, runeKey("4"))
	return m
}

func lineWith(t *testing.T, view, title string) string {
	t.Helper()
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(ansi.Strip(l), title) {
			return l
		}
	}
	t.Fatalf("no line with %q:\n%s", title, ansi.Strip(view))
	return ""
}

func TestGraphCaretOnlyWithVisibleKids(t *testing.T) {
	v := treeModel(t).View()
	if l := ansi.Strip(lineWith(t, v, "Full chart")); !strings.Contains(l, "▾ ") {
		t.Errorf("a header with kids has a caret: %q", l)
	}
	if l := ansi.Strip(lineWith(t, v, "Bare chart")); strings.ContainsAny(l, "▾▸") {
		t.Errorf("a childless header has no caret: %q", l)
	}
}

// A container and its leaf siblings share a glyph column: the leaf pads the
// caret slot.
func TestGraphSiblingsAlign(t *testing.T) {
	forceANSI256(t)
	beads := []bd.Bead{
		{ID: "ini", Kind: contract.KindInitiative, Container: true, Status: "open", Title: "Ship it", Created: epoch},
		{ID: "ch", Kind: contract.KindChart, Container: true, Status: "open", Title: "Full chart", Parent: "ini", Created: epoch},
		{ID: "kid", Kind: contract.KindBuild, Status: "open", Title: "Kid one", Parent: "ch", Created: epoch.Add(time.Hour)},
		{ID: "lone", Kind: contract.KindBuild, Status: "open", Title: "Lone build", Parent: "ini", Created: epoch.Add(2 * time.Hour)},
	}
	m := sized(t, 120, 30)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: map[string]session.Info{}})
	m, _ = send(t, m, runeKey("4"))
	v := m.View()
	col := func(title string) int {
		l := ansi.Strip(lineWith(t, v, title))
		return ansi.StringWidth(l[:strings.Index(l, "○")])
	}
	if col("Lone build") != col("Full chart") {
		t.Errorf("leaf col %d, container sibling col %d", col("Lone build"), col("Full chart"))
	}
	if col("Kid one") <= col("Full chart") {
		t.Errorf("child col %d not right of its header col %d", col("Kid one"), col("Full chart"))
	}
}

func TestGraphAncestorShade(t *testing.T) {
	m := selectBead(t, treeModel(t), "kid")
	v := m.View()
	for title, want := range map[string]bool{"Ship it": true, "Full chart": true, "Kid two": false, "Bare chart": false} {
		if got := strings.Contains(lineWith(t, v, title), "48;5;236"); got != want {
			t.Errorf("%s shaded = %v, want %v", title, got, want)
		}
	}
	if l := lineWith(t, v, "Kid one"); !strings.Contains(l, "48;5;25") || strings.Contains(l, "48;5;236") {
		t.Errorf("the selected row keeps 25: %q", l)
	}
	// the shade survives the inner resets.
	l := lineWith(t, v, "Full chart")
	if strings.Contains(strings.ReplaceAll(l, "\x1b[0m\x1b[48;5;236m", ""), "\x1b[0m") && !strings.HasSuffix(l, "\x1b[0m") {
		t.Errorf("shade dropped after a reset: %q", l)
	}
}

func TestGraphInitiativeHasNoLiveTrailer(t *testing.T) {
	v := ansi.Strip(treeModel(t).View())
	if l := lineWith(t, v, "Ship it"); strings.Contains(l, "live:") || !strings.Contains(l, "landed 0/1") {
		t.Errorf("Initiative row: %q", l)
	}
}

// Graph's master list keeps a selected in-progress bead's yellow ◐ and badge, and the
// grey open ○ of an unselected-style glyph stays plain.
func TestGraphSelectedRowKeepsGlyphColour(t *testing.T) {
	m := treeModel(t)
	m, _ = send(t, m, loadedMsg{beads: []bd.Bead{
		{ID: "ch", Kind: contract.KindChart, Container: true, Status: "open", Title: "Full chart", Created: epoch},
		{ID: "k", Kind: contract.KindBuild, Status: "in_progress", Title: "Claimed", Parent: "ch", Created: epoch},
		{ID: "o", Kind: contract.KindBuild, Status: "open", Title: "Opened", Parent: "ch", Created: epoch.Add(time.Hour)},
	}, live: map[string]bool{}, sessions: map[string]session.Info{}})
	for id, want := range map[string]string{"k": "\x1b[38;5;220m◐", "o": ""} {
		l := lineWith(t, selectBead(t, m, id).View(), map[string]string{"k": "Claimed", "o": "Opened"}[id])
		if got := strings.Contains(l, "38;5;220m◐"); got != (want != "") {
			t.Errorf("%s: yellow ◐ kept = %v in %q", id, got, l)
		}
		if !strings.Contains(l, "48;5;25") || !strings.Contains(l, "38;5;224m") {
			t.Errorf("%s: selection or badge lost: %q", id, l)
		}
	}
}
