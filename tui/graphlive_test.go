package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

func liveModel(t *testing.T, w int, beads []bd.Bead, live map[string]bool, ss map[string]session.Info) Model {
	t.Helper()
	m := sized(t, w, 30)
	m, _ = send(t, m, loadedMsg{beads: beads, live: live, sessions: ss})
	m, _ = send(t, m, runeKey("4"))
	return m
}

func TestGraphFoldedHeadersCountLive(t *testing.T) {
	ss := map[string]session.Info{
		"paused": {State: session.Alive, Waiting: session.WaitTurn}, // waiting counts
		"oldb":   {State: session.Alive},                            // done (closed + alive) counts
		"long":   {State: session.Exited},                           // exited does not
		"ch":     {State: session.Alive},                            // the header's own is excluded from its count
	}
	ss["kindless"] = session.Info{State: session.Alive} // never rendered, so never counted
	beads := append(stubBeads(),
		bd.Bead{ID: "stalled", Kind: contract.KindBuild, Status: "in_progress", Title: "Dead effort", Parent: "ch", Created: epoch}, // AFK effort, not in the live map: ○
		bd.Bead{ID: "kindless", Status: "open", Title: "No kind", Parent: "ch", Created: epoch})
	m := liveModel(t, 120, beads, map[string]bool{"run": true}, ss)
	v := ansi.Strip(m.View())
	if strings.Contains(v, "live landed") || strings.Contains(v, "live 0/3") {
		t.Errorf("unfolded headers carry no trailer:\n%s", v)
	}
	if !strings.Contains(v, "Old chart · old 1 live 1/1") {
		t.Errorf("the seeded-folded Chart counts its done session before closed/total:\n%s", v)
	}
	m = selectBead(t, m, "ch")
	m = press(t, m, tea.KeyMsg{Type: tea.KeySpace})
	if v := ansi.Strip(m.View()); !strings.Contains(v, "2 live 0/4") {
		t.Errorf("ch: waiting + alive AFK, not its own, the exited, the dead ○ or the kindless:\n%s", v)
	}
	m = selectBead(t, m, "ini")
	m = press(t, m, tea.KeyMsg{Type: tea.KeySpace})
	if v := ansi.Strip(m.View()); !strings.Contains(v, "3 live landed 0/1") {
		t.Errorf("ini counts at depth, including ch's own session, before landed:\n%s", v)
	}
	m = filtered(t, m, "Graph") // a words filter leaves the count alone
	m = press(t, m, tea.KeyMsg{Type: tea.KeySpace})
	if v := ansi.Strip(m.View()); !strings.Contains(v, "3 live landed 0/1") {
		t.Errorf("words must not change the count:\n%s", v)
	}
}

func TestGraphNarrowKeepsTrailer(t *testing.T) {
	beads := []bd.Bead{{ID: "c", Kind: contract.KindChart, Container: true, Status: "open", Title: strings.Repeat("Long title ", 8), Created: epoch},
		{ID: "b", Kind: contract.KindBuild, Status: "open", Parent: "c", Title: "x", Created: epoch}}
	m := liveModel(t, 50, beads, nil, map[string]session.Info{"b": {State: session.Alive}})
	m = press(t, m, tea.KeyMsg{Type: tea.KeySpace})
	if v := ansi.Strip(m.View()); !strings.Contains(v, "1 live 0/1") {
		t.Errorf("narrow render lost the trailer:\n%s", v)
	}
}

func TestGraphUnparentedOpensFolded(t *testing.T) {
	sig := bd.Bead{ID: "sig", Kind: contract.KindSignal, Status: "open", Title: "A signal", Created: epoch}
	m := liveModel(t, 120, stubBeads(), nil, nil) // the first snapshot has no Unparented group
	m, _ = send(t, m, loadedMsg{beads: append(stubBeads(), sig), sessions: map[string]session.Info{"sig": {State: session.Alive}}})
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "▸ Unparented  1 live") || strings.Contains(v, "A signal") {
		t.Errorf("Unparented should open folded with its trailer:\n%s", v)
	}
	m = selectBead(t, m, unparentedSel)
	m = press(t, m, tea.KeyMsg{Type: tea.KeySpace})
	if v := ansi.Strip(m.View()); !strings.Contains(v, "A signal") || strings.Contains(v, "1 live") {
		t.Errorf("space unfolds it:\n%s", v)
	}
	m, _ = send(t, m, loadedMsg{beads: append(stubBeads(), sig)})
	if v := ansi.Strip(m.View()); !strings.Contains(v, "A signal") {
		t.Errorf("a refresh must not re-fold it:\n%s", v)
	}
}

// unfoldUnparented opens the Unparented group (if any), which starts folded.
func unfoldUnparented(t *testing.T, m Model) Model {
	t.Helper()
	if indexRow(m.tabAt(tabGraph).(graphTab).rows(), unparentedSel) < 0 {
		return m
	}
	for range 20 {
		if sel(m) == unparentedSel {
			return press(t, m, tea.KeyMsg{Type: tea.KeySpace})
		}
		m = press(t, m, runeKey("k"))
	}
	t.Fatalf("no Unparented header; at %s", sel(m))
	return m
}
