package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/eventlog"
)

// rollupModel is a Graph-tab Model selecting a bead with the rollup reads stubbed and
// counted: lineage loads and log reads.
func rollupModel(t *testing.T, loads, reads *int) Model {
	t.Helper()
	origL, origE := rollupLineage, rollupEvents
	t.Cleanup(func() { rollupLineage, rollupEvents = origL, origE })
	rollupLineage = func() (*bd.Lineage, error) { *loads++; return bd.NewLineage(nil), nil }
	rollupEvents = func() ([]eventlog.Event, error) { *reads++; return nil, nil }
	return selectBead(t, graphModel(t, 120, 44), "long")
}

// runCmds runs a Cmd and folds its message (a batch included), as the loop would.
func runCmds(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			m = runCmds(t, m, c)
		}
	case rollupMsg:
		m, _ = send(t, m, msg)
	}
	return m
}

func TestSyncRollupOneCmdPerUncachedBeadAndLineageReused(t *testing.T) {
	var loads, reads int
	m := rollupModel(t, &loads, &reads)
	m, cmd := send(t, m, runeKey("d"))
	if v := ansi.Strip(m.View()); !strings.Contains(v, "subtree     loading…") {
		t.Fatalf("not loading until the result lands:\n%s", v)
	}
	if loads+reads != 0 {
		t.Fatal("opening the drawer ran the reads on the event loop")
	}
	if _, c := m.syncRollup(); c != nil {
		t.Fatal("a second fetch started while one is in flight")
	}
	m = runCmds(t, m, cmd)
	if loads != 1 || reads != 1 {
		t.Fatalf("loads=%d reads=%d, want 1 each", loads, reads)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "subtree     no sessions") || !strings.Contains(v, "not on a spine") {
		t.Fatalf("result not rendered:\n%s", v)
	}
	if _, c := m.syncRollup(); c != nil {
		t.Fatal("a cached bead refetched")
	}
	// the next bead, same snapshot: a new fetch, the lineage reused
	m, cmd = send(t, m, runeKey("j"))
	m = runCmds(t, m, cmd)
	if loads != 1 || reads != 2 {
		t.Fatalf("second bead: loads=%d reads=%d, want 1 and 2", loads, reads)
	}
	// a fast-tier load keeps the cache
	m, cmd = send(t, m, loadedMsg{beads: m.beads, live: map[string]bool{}})
	m = runCmds(t, m, cmd)
	if loads != 1 || reads != 2 {
		t.Fatalf("fast-tier load refetched: loads=%d reads=%d", loads, reads)
	}
	// a slow-tier snapshot drops the lineage cache
	m, cmd = send(t, m, loadedMsg{beads: m.beads, live: map[string]bool{}, slowTier: true})
	m = runCmds(t, m, cmd)
	if loads != 2 {
		t.Fatalf("lineage not re-read after a snapshot: loads=%d", loads)
	}
}

func TestF5RetriesRollupError(t *testing.T) {
	var loads, reads int
	m := rollupModel(t, &loads, &reads)
	rollupLineage = func() (*bd.Lineage, error) { loads++; return nil, errors.New("bd down\nhint: retry") }
	m, cmd := send(t, m, runeKey("d"))
	m = runCmds(t, m, cmd)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "couldn't load: bd down hint: retry") {
		t.Fatalf("error line missing:\n%s", v)
	}
	m, cmd = send(t, m, tickMsg{})
	m = runCmds(t, m, cmd)
	if loads != 1 {
		t.Fatalf("error entry refetched without f5: %d", loads)
	}
	_, cmd = send(t, m, tea.KeyMsg{Type: tea.KeyF5})
	runCmds(t, m, cmd)
	if loads != 2 {
		t.Errorf("f5 did not retry: %d", loads)
	}
}
