package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// Orphaned claims (ADR 0391): the model counts them apart from live in-flight
// efforts, the row carries the orphan marker, and x offers recover.
func TestOrphanedClaimsShownApart(t *testing.T) {
	beads := []bd.Bead{
		{ID: "ch", Kind: contract.KindChart, Container: true, Status: "open", Title: "Chart", Created: epoch},
		{ID: "livea", Kind: contract.KindBuild, Status: "in_progress", Title: "Live one", Parent: "ch", Created: epoch},
		{ID: "orph1", Kind: contract.KindBuild, Status: "in_progress", Title: "Orphan one", Parent: "ch", Created: epoch},
		{ID: "orph2", Kind: contract.KindBuild, Status: "in_progress", Title: "Orphan two", Parent: "ch", Created: epoch},
	}
	m := sized(t, 120, 30)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{"livea": true}, orphans: map[string]bool{"orph1": true, "orph2": true}})
	if m.inflight != 1 || m.orphaned != 2 {
		t.Fatalf("inflight %d orphaned %d, want 1 and 2", m.inflight, m.orphaned)
	}
	var pool string
	for _, r := range m.settingRows() {
		if r.name == poolKey {
			pool = r.value
		}
	}
	if !strings.HasSuffix(pool, "1 in flight · 2 orphaned") {
		t.Errorf("pool readout %q lacks the orphan suffix", pool)
	}
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{"livea": true}})
	for _, r := range m.settingRows() {
		if r.name == poolKey && strings.Contains(r.value, "orphaned") {
			t.Errorf("pool readout %q names orphans when there are none", r.value)
		}
	}

	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{"livea": true}, orphans: map[string]bool{"orph1": true}})
	m, _ = send(t, m, runeKey("4"))
	m = selectBead(t, m, "ch")
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "⊘") {
		t.Errorf("an orphan row carries the orphan marker:\n%s", v)
	}
	if strings.Count(v, "⊘") != 1 {
		t.Errorf("only the orphan carries the marker:\n%s", v)
	}
	m = selectBead(t, m, "orph1")
	if h := hintsOf(m); !strings.Contains(h, "x recover") {
		t.Errorf("orphan row hints %q lack x recover", h)
	}
}
