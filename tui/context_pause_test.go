package tui

import (
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
)

// paused:context refuses run-now with the three-fix hint (ADR 0158).
func TestContextPauseRefusesRunNow(t *testing.T) {
	b := bd.Bead{ID: "cx", Labels: []string{"context:gone", "paused:context"}}
	if !pauseRefusesRunNow(b, "") || !strings.Contains(pauseHint(b), "relabel it, remove the label, or declare it") {
		t.Errorf("refuses=%v hint=%q", pauseRefusesRunNow(b, ""), pauseHint(b))
	}
}

// paused:not-built is not refused and reads "retry", like integrate-conflict (ADR 0248).
func TestNotBuiltPauseReadsRetry(t *testing.T) {
	for _, cause := range []string{bd.PausedIntegrateConflict, bd.PausedNotBuilt} {
		b := bd.Bead{ID: "nb", Status: "open", Kind: "build", Labels: []string{"kind:build", "paused:" + cause}}
		if pauseRefusesRunNow(b, "") {
			t.Errorf("%s refuses run-now", cause)
		}
		if got := (graphData{}).runNowLabel(&b); got != "retry" {
			t.Errorf("%s label = %q, want retry", cause, got)
		}
	}
}
