package tui

import (
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// An armed Auto-exit outranks waiting; Done outranks both; cancelling (AutoExit zero) waits again.
func TestArmedAutoExitReadsLiveNotWaiting(t *testing.T) {
	armed := time.Now().Add(time.Minute)
	for _, c := range []struct {
		name     string
		w        session.Waiting
		auto     time.Time
		status   string
		leaf     lstate
		word     string // lookOf word
		discWord string
	}{
		{"turn armed", session.WaitTurn, armed, "open", sLive, "alive", "live"},
		{"ask armed", session.WaitAsk, armed, "open", sLive, "alive", "live"},
		{"turn cancelled", session.WaitTurn, time.Time{}, "open", sTurn, "your turn", "your turn"},
		{"ask cancelled", session.WaitAsk, time.Time{}, "open", sAsk, "needs approval", "needs approval"},
		{"closed and armed", session.WaitTurn, armed, "closed", sDone, "done — /exit", "live"},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := dbead("l", contract.KindGrilling, c.status, "c", 1)
			s := session.Info{State: session.Alive, Waiting: c.w, AutoExit: c.auto}
			g := graphData{beads: []bd.Bead{b}, sessions: map[string]session.Info{"l": s}, launching: map[string]bool{}}
			if got := g.leafState(g.beadByID("l")); got != c.leaf {
				t.Errorf("leafState = %d, want %d", got, c.leaf)
			}
			if l, _ := g.lookOf(g.beadByID("l")); l.word != c.word {
				t.Errorf("look word = %q, want %q", l.word, c.word)
			}
			m := Model{sessions: map[string]session.Info{"l": s}}
			wantWait := c.leaf == sTurn || c.leaf == sAsk
			if got := m.waiting(b); got != wantWait {
				t.Errorf("waiting = %v, want %v", got, wantWait)
			}
			if got := m.discussionState("l"); got != c.discWord {
				t.Errorf("discussionState = %q, want %q", got, c.discWord)
			}
		})
	}
}

// A Closing session is never Waiting (ADR 0108 over ADR 0099): it bands live and counts nowhere.
func TestClosingSessionReadsLiveNotWaiting(t *testing.T) {
	for _, w := range []session.Waiting{session.WaitTurn, session.WaitAsk} {
		t.Run(string(w), func(t *testing.T) {
			b := dbead("l", contract.KindGrilling, "open", "c", 1)
			s := session.Info{State: session.Alive, Waiting: w, Closing: true}
			g := graphData{beads: []bd.Bead{b}, sessions: map[string]session.Info{"l": s}, launching: map[string]bool{}}
			if got := g.leafState(g.beadByID("l")); got != sLive {
				t.Errorf("leafState = %d, want sLive", got)
			}
			if l, _ := g.lookOf(g.beadByID("l")); l.word != "alive" {
				t.Errorf("look word = %q, want alive", l.word)
			}
			if got := g.rowSuffix(g.beadByID("l")); !strings.Contains(got, "closing…") {
				t.Errorf("rowSuffix = %q, want closing…", got)
			}
			m := Model{sessions: map[string]session.Info{"l": s}}
			if m.waiting(b) {
				t.Error("waiting = true, want false")
			}
			if got := m.discussionState("l"); got != "live" {
				t.Errorf("discussionState = %q, want live", got)
			}
		})
	}
	t.Run("failed close reads exited", func(t *testing.T) {
		b := dbead("l", contract.KindGrilling, "open", "c", 1)
		g := graphData{beads: []bd.Bead{b}, sessions: map[string]session.Info{"l": {State: session.Exited, Closing: true}}}
		if l, _ := g.lookOf(g.beadByID("l")); l.word != "exited" {
			t.Errorf("failed close word = %q, want exited", l.word)
		}
	})
}
