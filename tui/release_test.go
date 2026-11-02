package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"jayulabs.org/mugicrew/looplock"
)

func TestReleaseSegment(t *testing.T) {
	for _, c := range []struct{ name, latest, want string }{
		{"before the check lands", "", ""},
		{"nothing newer", "", ""},
		{"newer release", "2.0.0", "↑ v2.0.0"},
	} {
		for _, w := range []int{120, 40} {
			m := sized(t, w, 30)
			m.loop = looplock.Status{State: looplock.On}
			if c.latest != "" {
				next, _ := m.Update(releaseMsg{c.latest})
				m = next.(Model)
			}
			got := ansi.Strip(m.headerRight(w))
			if has := strings.Contains(got, "↑ v"); has != (c.want != "") || c.want != "" && !strings.Contains(got, c.want) {
				t.Errorf("%s at %d: header %q, want segment %q", c.name, w, got, c.want)
			}
			if !strings.Contains(got, "loop on") {
				t.Errorf("%s at %d: header %q lost the loop segment", c.name, w, got)
			}
		}
	}
}

func TestReleaseSegmentKeepsLoopSpan(t *testing.T) {
	m := sized(t, 120, 30)
	m.loop = looplock.Status{State: looplock.On}
	base := m.headerSpans()
	m.releaseLatest = "2.0.0"
	spans := m.headerSpans()
	for _, sp := range spans {
		if sp.kind != headerLoop {
			continue
		}
		if sp.x1-sp.x0 != 7 {
			t.Errorf("loop span width %d, want 7", sp.x1-sp.x0)
		}
		return
	}
	t.Errorf("no loop span with the segment on; before: %v", base)
}

func TestReleaseCheckRunsAtStart(t *testing.T) {
	m := sized(t, 120, 30)
	if m.checkRelease() != nil {
		t.Error("no check wired must run nothing")
	}
	m = m.WithRelease(func() string { return "2.0.0" })
	if got := m.checkRelease()(); got != (releaseMsg{"2.0.0"}) {
		t.Errorf("check msg = %v", got)
	}
}
