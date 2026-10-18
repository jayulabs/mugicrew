package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/session"
)

// A bead being launched (or just launched, awaiting its reload) is under Live unless
// the tab's Actionable still lists it (Spec mc-vl0q FR3, FR5), on every queue tab.
func TestLiveListsLaunchingBeadsNotInActionable(t *testing.T) {
	for _, mark := range []string{"launching", "launched"} {
		set := func(g *graphData, id string) {
			if mark == "launching" {
				g.launching = map[string]bool{id: true}
			} else {
				g.launched = map[string]time.Time{id: time.Now()}
			}
		}
		// launching drops a ready bead from Actionable (leafState), so it shows under
		// Live; a launched bead still shows in Actionable on the exec and ideate tabs
		// (their stale snapshot rows), while defog's check ignores inAct.
		inAct := mark == "launched"
		db, ds := defogFixture()
		dg := graphData{beads: db, sessions: ds}
		set(&dg, "ready")
		if got := strings.Contains(defogLiveKeys(dg.defogList(nil)), "live:ready"); !got { // a held launch reads launching, never ready
			t.Errorf("defog %s: ready under Live = %v", mark, got)
		}

		eg, _ := execLiveFixture()
		eg.live, eg.sessions = nil, nil
		set(&eg, "bl")
		if got := execLiveKeys(eg.execList(nil)); got != "live:bl" {
			t.Errorf("exec %s: live = %s", mark, got)
		}
		eg, _ = execLiveFixture()
		set(&eg, "sg") // waiting: in Actionable unless launching takes it out
		if got := strings.Contains(execLiveKeys(eg.execList(nil)), "live:sg"); got == inAct {
			t.Errorf("exec %s: sg under Live = %v", mark, got)
		}

		ib, is := ideaLiveFixture()
		ig := graphData{beads: ib, sessions: is}
		set(&ig, "x")
		if got := ideaLiveKeys(ig.ideaLive(ig.ideaExpanded(), nil)); got != "live:a,live:x" {
			t.Errorf("ideate %s: live = %s", mark, got)
		}
	}
}

// A successful launch holds the bead in Live until the next snapshot lands; a failed
// one clears it at once.
func TestLaunchDoneHoldsLiveUntilReload(t *testing.T) {
	b := bd.Bead{ID: "afk", Title: "afk"}
	m := sized(t, 100, 30)
	m.launching = map[string]bool{"afk": true}
	ok, _ := send(t, m, launchDoneMsg{b: b, bg: true})
	if len(ok.launching) != 0 || ok.launched["afk"].IsZero() {
		t.Fatalf("after success: launching=%v launched=%v", ok.launching, ok.launched)
	}
	// A load that began before the launch landed is a pre-launch snapshot: it keeps the hold.
	stale, _ := send(t, ok, loadedMsg{started: time.Now().Add(-time.Minute)})
	if stale.launched["afk"].IsZero() {
		t.Errorf("pre-launch snapshot cleared the hold: %v", stale.launched)
	}
	// A later snapshot that does not show the bead alive keeps the hold.
	notYet, _ := send(t, ok, loadedMsg{started: time.Now().Add(time.Second)})
	if notYet.launched["afk"].IsZero() {
		t.Errorf("snapshot without the session cleared the hold: %v", notYet.launched)
	}
	// One showing the session alive (or the effort alive) clears it.
	alive, _ := send(t, ok, loadedMsg{started: time.Now().Add(time.Second), sessions: map[string]session.Info{"afk": {State: session.Alive}}})
	if len(alive.launched) != 0 {
		t.Errorf("alive session kept the hold: %v", alive.launched)
	}
	alive, _ = send(t, ok, loadedMsg{started: time.Now().Add(time.Second), live: map[string]bool{"afk": true}})
	if len(alive.launched) != 0 {
		t.Errorf("alive effort kept the hold: %v", alive.launched)
	}
	// Any snapshot launchHoldMax after the success ends the hold.
	late, _ := send(t, ok, loadedMsg{started: time.Now().Add(launchHoldMax + time.Second)})
	if len(late.launched) != 0 {
		t.Errorf("timeout kept the hold: %v", late.launched)
	}
	bad, _ := send(t, m, launchDoneMsg{b: b, err: errors.New("boom")})
	if len(bad.launching) != 0 || len(bad.launched) != 0 {
		t.Errorf("failure kept a mark: launching=%v launched=%v", bad.launching, bad.launched)
	}
}

// The whole Live (N) header is bold in the live green, name and count alike.
func TestLiveTitleIsBoldLiveGreen(t *testing.T) {
	forceANSI256(t)
	got := liveTitle("Live (2)")
	if !strings.Contains(got, "38;5;114") || !strings.Contains(got, "1") || strings.Count(got, "38;5;114") != 1 || !strings.Contains(got, "Live (2)") {
		t.Errorf("liveTitle = %q, want one bold 114 run over name and count", got)
	}
}

// A held Initiative launch whose cut session isn't Alive yet is in neither Ideate band
// (so it stays under Live); once Alive, the waiting check applies again.
func TestIdeateHeldInitiativeLaunchStaysOutOfActionable(t *testing.T) {
	g := graphData{beads: []bd.Bead{ini("ini", "open", "Ship", 1)}, launched: map[string]time.Time{"ini": time.Now()}}
	if act := g.ideaAct(); len(act) != 0 {
		t.Errorf("held launch in Actionable: %v", act)
	}
	if got := ideaLiveKeys(g.ideaLive(g.ideaExpanded(), nil)); got != "live:ini" {
		t.Errorf("held launch: live = %s", got)
	}
	g.sessions = map[string]session.Info{"ini": {State: session.Alive, Waiting: session.WaitTurn}}
	if act := g.ideaAct(); len(act) != 1 {
		t.Errorf("alive waiting session: Actionable = %v", act)
	}
}
