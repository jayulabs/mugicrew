package tui

import (
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// With the run-now set empty, a bead's retry in flight is read off its labels and
// effort liveness alone, so it survives a TUI restart and a detached host.
func TestRetryInFlightReadsLiveness(t *testing.T) {
	cases := []struct {
		name       string
		label      string
		live       bool
		wantPaused int
	}{
		{"failing live", "paused:failing", true, 0},
		{"foreign-hold live", "paused:foreign-hold", true, 0},
		{"manual live", "paused:manual", true, 1},
		{"failing not live", "paused:failing", false, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := dbead("b", contract.KindBuild, "open", "", 1, labeled(c.label))
			m := sized(t, 120, 30)
			m.beads, m.live = []bd.Bead{b}, map[string]bool{"b": c.live}
			_, _, paused, _ := m.crewCounts(func(string) bool { return true }, m.retrying())
			if paused != c.wantPaused {
				t.Errorf("paused = %d, want %d", paused, c.wantPaused)
			}
		})
	}
}

func TestRecoverOptsAlwaysExcludesOwnSession(t *testing.T) {
	m, _ := actModel(t)
	ownSession = func() (string, error) { return "tui-1", nil }
	o, err := m.recoverOpts("b")(false)
	if err != nil || o.ExcludeTUISession != "tui-1" {
		t.Errorf("opts = %+v, %v", o, err)
	}
}
