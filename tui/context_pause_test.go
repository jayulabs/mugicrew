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

// paused:behind-mainline refuses run-now, says r requests a rebase, and the detail
// pause line says the Checkpoint is behind mainline.
func TestBehindMainlinePauseHint(t *testing.T) {
	b := bd.Bead{ID: "bm", Parent: "cp", Status: "open", Kind: "build", Labels: []string{"kind:build", "paused:" + bd.PausedBehindMainline}}
	if h := pauseHint(b); !strings.Contains(h, "checkpoint/cp is behind mainline") || !strings.Contains(h, "r requests a rebase") {
		t.Errorf("hint = %q", h)
	}
	if line := (graphData{}).pauseLine(&b); !strings.Contains(line, "behind mainline") || !strings.Contains(line, "r requests a rebase") {
		t.Errorf("pause line = %q", line)
	}
}

// The Behind mainline: note rides the hint only from cached comments; the hint itself
// never shells out to bd.
func TestBehindMainlineHintNoteFromCache(t *testing.T) {
	b := bd.Bead{ID: "bm", Parent: "cp", Status: "open", Kind: "build", CommentCount: 1, Labels: []string{"kind:build", "paused:" + bd.PausedBehindMainline}}
	g := graphData{comments: map[commentKey]commentEntry{{"bm", 1}: {cs: []bd.Comment{{Text: "Behind mainline: lacks selfexec"}}}}}
	if h := g.withBehindNote(&b, pauseHint(b)); !strings.Contains(h, "Behind mainline: lacks selfexec") {
		t.Errorf("cached note missing: %q", h)
	}
	if h := (graphData{}).withBehindNote(&b, pauseHint(b)); strings.Contains(h, "Behind mainline:") || !strings.Contains(h, "r requests a rebase") {
		t.Errorf("uncached hint = %q", h)
	}
}
