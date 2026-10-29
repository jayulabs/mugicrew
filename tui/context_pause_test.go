package tui

import (
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
)

// paused:context refuses run-now with the three-fix hint (ADR 0158).
func TestContextPauseRefusesRunNow(t *testing.T) {
	b := bd.Bead{ID: "cx", Labels: []string{"context:gone", "paused:context"}}
	if !strings.Contains(pauseHint(b), "relabel it, remove the label, or declare it") {
		t.Errorf("hint=%q", pauseHint(b))
	}
}

// paused:not-built is not refused and reads "retry", like integrate-conflict (ADR 0248).
func TestNotBuiltPauseReadsRetry(t *testing.T) {
	for _, cause := range []string{bd.PausedIntegrateConflict, bd.PausedNotBuilt} {
		b := bd.Bead{ID: "nb", Status: "open", Kind: "build", Labels: []string{"kind:build", "paused:" + cause}}
		if pauseHint(b) != "" {
			t.Errorf("%s refuses run-now", cause)
		}
		if got := (graphData{}).runNowLabel(&b); got != "retry" {
			t.Errorf("%s label = %q, want retry", cause, got)
		}
	}
}

// paused:unmet offers run-now (retry), is not refused, and its pause line names the fixes.
func TestUnmetPauseOffersRunNowAndNamesFixes(t *testing.T) {
	b := bd.Bead{ID: "ul", Status: "open", Kind: "latch", Labels: []string{"kind:latch", "paused:" + bd.PausedUnmet}}
	if pauseHint(b) != "" {
		t.Errorf("unmet refuses run-now: %q", pauseHint(b))
	}
	if got := (graphData{}).runNowLabel(&b); got != "retry" {
		t.Errorf("label = %q, want retry", got)
	}
	if line := (graphData{}).pauseLine(&b); !strings.Contains(line, "watched MR is closed or missing — reopen it and run-now, or withdraw the latch") {
		t.Errorf("pauseLine = %q", line)
	}
	if _, ok := pauseSty[bd.PausedUnmet]; !ok {
		t.Error("no colour for unmet")
	}
}
