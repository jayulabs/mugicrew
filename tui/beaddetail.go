package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// BeadDetail is the Graph drawer's detail text for bead id, minus the launch preview,
// wrapped to width: what the F10 bead pane shows (a one-time snapshot). It loads one
// slow-tier snapshot synchronously, from the sources the TUI's refresh reads; an
// unknown id or a failed load is an error.
func BeadDetail(ctx contract.Ctx, crew map[string]contract.Crewmate, id string, width int) (string, error) {
	// This is a pane's process, not the TUI's: it must not touch tmux's client map or bars.
	recordClient = func() error { return nil }
	setBars = func(map[string]bool, map[string]string, map[string]session.Info) error { return nil }
	m := New(ctx, crew)
	m.w = width
	next, _, _ := m.fold(m.loadSlow()())
	m = next.(Model)
	if m.loadErr != nil {
		return "", m.loadErr
	}
	if b := beadByID(m.beads, id); b != nil && b.CommentCount > 0 { // a one-shot pane process, not the render loop
		cs, err := listComments(id)
		m.comments = map[commentKey]commentEntry{{id, b.CommentCount}: {cs: cs, err: err}}
	}
	if b := beadByID(m.beads, id); b != nil { // likewise: real numbers, no loading line
		if lin, err := rollupLineage(); err != nil {
			m.rollups = map[string]rollupEntry{id: {err: err, loaded: true}}
		} else {
			e := computeRollup(lin, id)
			e.loaded = true
			m.rollups = map[string]rollupEntry{id: e}
		}
	}
	g := m.tabAt(tabGraph).(graphTab)
	if g.beadIndex(id) < 0 {
		return "", fmt.Errorf("no bead %q", id)
	}
	g.sel = id
	return lipgloss.NewStyle().Width(drawerContentW(width)).Render(g.detailBody()), nil
}
