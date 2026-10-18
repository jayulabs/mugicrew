package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// drainedBeads: a drained Initiative "d", an under-way one without Signals "ua", one
// under way with a Signal "us", a closed one "cl", and a loose offshoot Signal.
func drainedBeads() []bd.Bead {
	return []bd.Bead{
		ini("d", "open", "Drained", 1), ini("ua", "open", "Quiet", 1), ini("us", "open", "Busy", 2), ini("cl", "closed", "Gone", 1),
		{ID: "ub", Kind: contract.KindBuild, Status: "open", Title: "ub", Parent: "ua", Created: epoch},
		sig("s1", "open", 1, "us"), sig("off", "open", 1, ""),
	}
}

func drainedModel(t *testing.T, beads []bd.Bead, sess map[string]session.Info) (Model, *[]string) {
	t.Helper()
	calls := stubActions(t)
	m := sized(t, 160, 50)
	m.crew = map[string]contract.Crewmate{"luffy": fakeCrew{}}
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: sess})
	m = treesOpen(m)
	return m, calls
}

func TestIdeateListsDrainedInitiative(t *testing.T) {
	keys := func(m Model) string {
		var ks []string
		for _, r := range m.tabAt(tabIdeate).(ideaTab).rows() {
			if !r.act && !r.sec && !r.place {
				ks = append(ks, r.key)
			}
		}
		return strings.Join(ks, ",")
	}
	m, _ := drainedModel(t, drainedBeads(), nil)
	if got, want := keys(m), ",i:d,h:d,i:us,s1,off"; got != want {
		t.Errorf("rows %q, want %q", got, want)
	}
	m = press(t, ideaAt(t, m, "i:d"), runeKey(" "))
	if got, want := keys(m), ",i:d,i:us,s1,off"; got != want {
		t.Errorf("folded rows %q, want %q", got, want)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Drained · d · drained") {
		t.Errorf("folded header lacks the drained trailer:\n%s", v)
	}
}

func TestIdeateHintRowDrawing(t *testing.T) {
	bs := drainedBeads()
	bs[0].Labels = []string{"paused:manual"}
	m, _ := drainedModel(t, bs, map[string]session.Info{"d": {State: session.Alive, Waiting: session.WaitTurn}})
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "‖ all members closed · enter: Luffy cuts the next phase, or closes it") {
		t.Errorf("hint row:\n%s", v)
	}
	if !strings.Contains(v, "Drained") || !strings.Contains(v, "your turn") {
		t.Errorf("header suffix:\n%s", v)
	}
}

func TestIdeateEnterCutsDrainedInitiative(t *testing.T) {
	for _, id := range []string{"i:d", "h:d"} {
		t.Run(id, func(t *testing.T) {
			m, calls := drainedModel(t, drainedBeads(), nil)
			o, cmd := send(t, ideaAt(t, m, id), enterKey())
			settle(t, o, cmd)
			if got := strings.Join(*calls, ";"); !strings.Contains(got, "hitlClaim d") || !strings.Contains(got, "startSession d") {
				t.Errorf("calls %v", *calls)
			}
		})
	}
}

func TestIdeateEnterAttachesAndAsksWhenPaused(t *testing.T) {
	m, calls := drainedModel(t, drainedBeads(), map[string]session.Info{"d": {State: session.Alive}})
	o, cmd := send(t, ideaAt(t, m, "h:d"), enterKey())
	settle(t, o, cmd)
	if !slices.Contains(*calls, "switchSession d") {
		t.Errorf("attach: calls %v", *calls)
	}
	bs := drainedBeads()
	bs[0].Labels = []string{"paused:manual"}
	m, calls = drainedModel(t, bs, nil)
	o = press(t, ideaAt(t, m, "i:d"), enterKey())
	if len(o.modals) != 1 || len(*calls) != 0 {
		t.Errorf("paused: modals %d calls %v", len(o.modals), *calls)
	}
}

func TestIdeateEnterRefusesOpenMembers(t *testing.T) {
	m, calls := drainedModel(t, drainedBeads(), nil)
	o := press(t, ideaAt(t, m, "i:us"), enterKey())
	if !strings.Contains(statusOf(o), "members still open · cut from Graph") || len(*calls) != 0 {
		t.Errorf("status %q calls %v", statusOf(o), *calls)
	}
}

func TestIdeateDrainedHints(t *testing.T) {
	m, _ := drainedModel(t, drainedBeads(), nil)
	want := func(o Model, s string) {
		t.Helper()
		if got := hintsOf(o); got != s {
			t.Errorf("hints\n got %q\nwant %q", got, s)
		}
	}
	want(ideaAt(t, m, "i:d"), "enter cut · space fold · d detail · +/- prio · J/K section")
	want(ideaAt(t, m, "h:d"), "enter cut · d detail · +/- prio · J/K section")
	m, _ = drainedModel(t, drainedBeads(), map[string]session.Info{"d": {State: session.Exited}})
	want(ideaAt(t, m, "i:d"), "enter attach · space fold · d detail · +/- prio · J/K section")
	want(ideaAt(t, m, "i:us"), "space fold · d detail · +/- prio · J/K section")
	for _, id := range []string{"i:d", "h:d"} {
		checkCoverage(t, "Ideate "+id, ideaAt(t, m, id), true, "1", "J", "K", "ctrl+down", "ctrl+up")
	}
}

func TestIdeateHintRowFoldAndFallback(t *testing.T) {
	dir := t.TempDir()
	m := treesOpen(foldModel(t, dir, []bd.Bead{ini("d", "open", "Drained", 1)}))
	m = press(t, ideaAt(t, m, "h:d"), runeKey(" "))
	if cursorKey(m) != "i:d" || readFolds(t, dir) != `{}` {
		t.Errorf("cursor %q folds %s", cursorKey(m), readFolds(t, dir))
	}
	// the Initiative un-drains while the hint row is selected: cursor goes to its header
	m = treesOpen(foldModel(t, t.TempDir(), []bd.Bead{ini("d", "open", "Drained", 1)}))
	m = ideaAt(t, m, "h:d")
	m, _ = send(t, m, loadedMsg{beads: []bd.Bead{ini("d", "open", "Drained", 1), sig("new", "open", 1, "d")}, live: map[string]bool{}})
	if got := cursorKey(m); got != "i:d" {
		t.Errorf("un-drained: cursor %q", got)
	}
	m = ideaAt(t, treesOpen(foldModel(t, t.TempDir(), []bd.Bead{ini("d", "open", "Drained", 1)})), "h:d")
	m, _ = send(t, m, loadedMsg{beads: nil, live: map[string]bool{}})
	if got := cursorKey(m); got != "" {
		t.Errorf("gone: cursor %q", got)
	}
}

func TestIdeateDetailHasNoOnLaunch(t *testing.T) {
	m, _ := drainedModel(t, drainedBeads(), nil)
	for _, id := range []string{"i:d", "h:d", "i:us"} {
		if v := ansi.Strip(press(t, ideaAt(t, m, id), runeKey("d")).View()); strings.Contains(v, "ON LAUNCH") {
			t.Errorf("%s: detail shows ON LAUNCH:\n%s", id, v)
		}
	}
}

func TestIdeateListsCutSessionInitiative(t *testing.T) {
	keys := func(m Model) string {
		var ks []string
		for _, r := range m.tabAt(tabIdeate).(ideaTab).rows() {
			if !r.act && !r.sec && !r.place {
				ks = append(ks, r.key)
			}
		}
		return strings.Join(ks, ",")
	}
	for _, st := range []session.State{session.Alive, session.Exited} {
		m, _ := drainedModel(t, drainedBeads(), map[string]session.Info{"ua": {State: st, Waiting: session.WaitTurn}})
		if got, want := keys(m), ",i:d,h:d,i:ua,i:us,s1,off"; got != want {
			t.Errorf("state %v rows %q, want %q", st, got, want)
		}
	}
	m, _ := drainedModel(t, drainedBeads(), map[string]session.Info{"ua": {State: session.Alive, Waiting: session.WaitTurn}})
	v := ansi.Strip(m.View())
	if strings.Contains(v, "all members closed") && strings.Count(v, "all members closed") > 2 || !strings.Contains(v, "your turn") {
		t.Errorf("header suffix or stray hint row:\n%s", v)
	}
	// with a Signal the existing header keeps its Signal row
	m, _ = drainedModel(t, drainedBeads(), map[string]session.Info{"us": {State: session.Alive}})
	if got, want := keys(m), ",i:d,h:d,i:us,s1,off"; got != want {
		t.Errorf("with Signals: rows %q, want %q", got, want)
	}
}

func TestIdeateCutSessionHeaderEnterHintsAndDetail(t *testing.T) {
	sess := map[string]session.Info{"ua": {State: session.Alive}, "us": {State: session.Alive}}
	m, calls := drainedModel(t, drainedBeads(), sess)
	if got, want := hintsOf(ideaAt(t, m, "i:ua")), "enter attach · d detail · +/- prio · J/K section"; got != want {
		t.Errorf("no Signals hints %q, want %q", got, want)
	}
	if got, want := hintsOf(ideaAt(t, m, "i:us")), "enter attach · space fold · d detail · +/- prio · J/K section"; got != want {
		t.Errorf("Signals hints %q, want %q", got, want)
	}
	if v := ansi.Strip(press(t, ideaAt(t, m, "i:ua"), runeKey("d")).View()); !strings.Contains(v, "SESSION — enter attaches") { // Graph's preview for a session
		t.Errorf("no launch preview:\n%s", v)
	}
	o, cmd := send(t, ideaAt(t, m, "i:ua"), enterKey())
	settle(t, o, cmd)
	if !slices.Contains(*calls, "switchSession ua") || slices.Contains(*calls, "hitlClaim ua") {
		t.Errorf("attach: calls %v", *calls)
	}

	m, calls = drainedModel(t, drainedBeads(), map[string]session.Info{"ua": {State: session.Exited}})
	seam(t, &resumeSession, func(b bd.Bead, s session.Info, h contract.Harness) error {
		*calls = append(*calls, "resume "+b.ID)
		return nil
	})
	m.ctx = harnessCtx{}
	o, cmd = send(t, ideaAt(t, m, "i:ua"), enterKey())
	if cmd == nil {
		t.Fatal("exited: no resume command")
	}
	settle(t, o, cmd)
	if !slices.Contains(*calls, "resume ua") {
		t.Errorf("exited: calls %v", *calls)
	}
}
