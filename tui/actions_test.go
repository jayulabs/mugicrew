package tui

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/chopper"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/dispatch"
	"jayulabs.org/mugicrew/session"
)

// fakeCrew launches every HITL kind into a fixed argv.
type fakeCrew struct{}

func (fakeCrew) Run(contract.Ctx, contract.Bead) error { return nil }
func (fakeCrew) Launch(contract.Ctx, contract.Bead) (contract.LaunchCommand, error) {
	return contract.AgentCommand{Argv: []string{"agent"}}, nil
}

// stubActions fakes every seam the Graph's action keys reach and returns the call log.
func stubActions(t *testing.T) *[]string {
	t.Helper()
	var calls []string
	rec := func(name string, args ...any) {
		calls = append(calls, strings.TrimSpace(fmt.Sprintln(append([]any{name}, args...)...)))
	}
	var saved []func()
	save := func(f func()) { saved = append(saved, f) }
	{
		a, b, c, d, e, f := startSession, hitlClaim, hitlUnclaim, switchSession, dropSession, dropOrphan
		save(func() {
			startSession, hitlClaim, hitlUnclaim, switchSession, dropSession, dropOrphan = a, b, c, d, e, f
		})
		startSession = func(b bd.Bead, _ contract.AgentCommand) error { rec("startSession", b.ID); return nil }
		hitlClaim = func(id string) error { rec("hitlClaim", id); return nil }
		hitlUnclaim = func(id string) error { rec("hitlUnclaim", id); return nil }
		switchSession = func(id string) error { rec("switchSession", id); return nil }
		dropSession = func(b bd.Bead, _ session.Info) error { rec("dropSession", b.ID); return nil }
		dropOrphan = func(wt string) error { rec("dropOrphan", wt); return nil }
	}
	{
		a, b, c := pauseBead, unpauseBead, updateBead
		save(func() { pauseBead, unpauseBead, updateBead = a, b, c })
		pauseBead = func(id, _ string) error { rec("pauseBead", id); return nil }
		unpauseBead = func(id, _ string) error { rec("unpauseBead", id); return nil }
		updateBead = func(id string, args []string) error { rec("updateBead", id, args); return nil }
	}
	{
		a, b, c, d, e := abortEffort, assessRecover, recoverBead, ownSession, readyFrontier
		save(func() { abortEffort, assessRecover, recoverBead, ownSession, readyFrontier = a, b, c, d, e })
		abortEffort = func(id, _ string) ([]int, bool, error) { rec("abortEffort", id); return nil, false, nil }
		assessRecover = func(id string, _ chopper.Opts) (chopper.Inventory, string, error) {
			rec("assessRecover", id)
			return chopper.Inventory{Tree: "/tmp/tree", Files: dirtyFiles}, "dead", nil
		}
		recoverBead = func(id string, o chopper.Opts) (chopper.Report, error) {
			rec("recoverBead", id, o.Discard)
			return chopper.Report{Verdict: "dead"}, nil
		}
		ownSession = func() (string, error) { return "", nil }
		readyFrontier = func() ([]bd.Bead, error) { rec("readyFrontier"); return nil, errors.New("stop here") }
	}
	{
		a := readSessions
		save(func() { readSessions = a })
		readSessions = func() (map[string]session.Info, error) { return actionSessions(), nil }
	}
	t.Cleanup(func() {
		for _, f := range saved {
			f()
		}
		dirtyFiles = 0
	})
	return &calls
}

var dirtyFiles int // the files the stubbed recover inventory reports

func actionSessions() map[string]session.Info {
	return map[string]session.Info{"sess": {State: session.Alive, Session: "s1"}}
}

func actionBeads() []bd.Bead {
	return []bd.Bead{
		{ID: "sig", Kind: contract.KindSignal, Status: "open", Title: "Triage me", Priority: 2, Created: epoch},
		{ID: "dep", Kind: contract.KindSignal, Status: "open", Title: "Dependency", Priority: 2, Created: epoch},
		{ID: "blk", Kind: contract.KindSignal, Status: "open", Title: "Blocked signal", BlockedBy: []string{"dep"}, Created: epoch},
		{ID: "res", Kind: contract.KindResearch, Status: "open", Title: "Research it", Created: epoch},
		{ID: "resblk", Kind: contract.KindResearch, Status: "open", Title: "Blocked research", BlockedBy: []string{"dep"}, Created: epoch},
		{ID: "live", Kind: contract.KindBuild, Status: "in_progress", Title: "Running build", Created: epoch},
		{ID: "dead", Kind: contract.KindBuild, Status: "in_progress", Title: "Dead build", Created: epoch},
		{ID: "sess", Kind: contract.KindSignal, Status: "in_progress", Title: "Has session", Created: epoch},
		{ID: "gone", Kind: contract.KindSignal, Status: "in_progress", Title: "Session gone", Created: epoch},
		{ID: "done", Kind: contract.KindBuild, Status: "closed", Title: "Done build", Created: epoch},
		{ID: "ini", Kind: contract.KindInitiative, Container: true, Status: "open", Title: "An initiative", Created: epoch},
		{ID: "pz", Kind: contract.KindSignal, Status: "open", Title: "Paused signal", Labels: []string{"paused:manual"}, Created: epoch},
		{ID: "pzblk", Kind: contract.KindSignal, Status: "open", Title: "Paused blocked signal", Labels: []string{"paused:manual"}, BlockedBy: []string{"dep"}, Created: epoch},
		{ID: "pini", Kind: contract.KindInitiative, Container: true, Status: "open", Title: "Paused initiative", Labels: []string{"paused:manual"}, Created: epoch},
		{ID: "pkid", Kind: contract.KindSignal, Status: "open", Title: "Under a pause", Parent: "pini", Created: epoch},
	}
}

func actModel(t *testing.T) (Model, *[]string) {
	t.Helper()
	calls := stubActions(t)
	m := sized(t, 160, 40)
	m.crew = map[string]contract.Crewmate{"luffy": fakeCrew{}, "nami": fakeCrew{}, "franky": fakeCrew{}}
	m, _ = send(t, m, loadedMsg{beads: actionBeads(), live: map[string]bool{"live": true}, sessions: actionSessions()})
	m, _ = send(t, m, runeKey("4"))
	if sel(m) == unparentedSel { // the Unparented group opens folded
		m, _ = send(t, m, tea.KeyMsg{Type: tea.KeySpace})
	}
	return m, calls
}

func enterKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }
func escKey() tea.KeyMsg   { return tea.KeyMsg{Type: tea.KeyEsc} }

func statusOf(m Model) string { l := lines(m.View()); return strings.TrimSpace(l[len(l)-2]) }

func hintsOf(m Model) string {
	var hs []string
	for _, b := range m.tabBindings() {
		h := b.Help()
		hs = append(hs, h.Key+" "+h.Desc)
	}
	return strings.Join(hs, " · ")
}

// at selects the bead and presses k.
func at(t *testing.T, m Model, id string, k tea.KeyMsg) (Model, tea.Cmd) {
	t.Helper()
	m, cmd := send(t, selectBead(t, m, id), k)
	return m, cmd
}

func has(calls []string, prefix string) bool {
	for _, c := range calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

// Each guarded action opens a dialog, calls its seam only after y or enter and never
// after n or esc (FR24, FR25; ADR 0142 §7).
func TestGuardedActionsAskFirst(t *testing.T) {
	for _, c := range []struct {
		name, id, key string
		title, seam   string
		prep          func()
	}{
		{"drop a session", "sess", "x", "Drop session?", "dropSession", nil},
		{"abort a live effort", "live", "x", "Abort effort?", "abortEffort", nil},
		{"recover a dead effort", "dead", "x", "Recover?", "recoverBead", nil},
		{"recover a gone session", "gone", "x", "Recover?", "recoverBead", nil},
		{"launch a blocked bead", "blk", "enter", "Launch blocked bead?", "startSession", nil},
		{"launch a paused bead", "pz", "enter", "Launch paused bead?", "startSession", nil},
		{"launch a blocked, paused bead", "pzblk", "enter", "Launch blocked, paused bead?", "startSession", nil},
		{"launch a paused Initiative head", "pini", "enter", "Launch paused bead?", "startSession", nil},
		{"run a blocked bead now", "resblk", "enter", "Run blocked bead now?", "readyFrontier", nil},
	} {
		for _, answer := range []struct {
			name string
			k    tea.KeyMsg
			ran  bool
		}{
			{"y", runeKey("y"), true}, {"enter", enterKey(), true}, {"n", runeKey("n"), false}, {"esc", escKey(), false},
		} {
			t.Run(c.name+"/"+answer.name, func(t *testing.T) {
				m, calls := actModel(t)
				k := runeKey(c.key)
				if c.key == "enter" {
					k = enterKey()
				}
				m, cmd := at(t, m, c.id, k)
				if c.key == "x" && c.id != "sess" && c.id != "live" { // recover reads first, then asks
					m = settle(t, m, cmd)
				}
				if len(m.modals) != 1 || m.modals[0].title != c.title {
					t.Fatalf("modals = %+v, want one %q", m.modals, c.title)
				}
				if has(*calls, c.seam) {
					t.Fatalf("%s called before the answer: %v", c.seam, *calls)
				}
				m, cmd = send(t, m, answer.k)
				m = settle(t, m, cmd)
				if got := has(*calls, c.seam); got != answer.ran {
					t.Errorf("after %s: %s called = %v, want %v (calls %v)", answer.name, c.seam, got, answer.ran, *calls)
				}
				if answer.ran {
					return
				}
				if len(m.modals) != 0 {
					t.Errorf("a refusal left a dialog open")
				}
			})
		}
	}
}

// A paused bead's dialog names the pause; an inherited one names its ancestor; both
// blocked and paused gives one dialog with both lines (FR10–FR12).
func TestPausedLaunchDialogNamesCauses(t *testing.T) {
	for _, c := range []struct {
		id, title string
		lines     []string
	}{
		{"pz", "Launch paused bead?", []string{"paused: manual"}},
		{"pkid", "Launch paused bead?", []string{"manual via pini"}},
		{"pzblk", "Launch blocked, paused bead?", []string{"paused: manual", "Dependency"}},
	} {
		m, calls := actModel(t)
		m, _ = at(t, m, c.id, enterKey())
		if len(m.modals) != 1 || m.modals[0].title != c.title {
			t.Fatalf("%s: modals = %+v, want one %q", c.id, m.modals, c.title)
		}
		wide, _ := send(t, m, tea.WindowSizeMsg{Width: 300, Height: 60}) // wide: no wrapping
		body := ansi.Strip(wide.View())
		for _, l := range c.lines {
			if !strings.Contains(body, l) {
				t.Errorf("%s: body %q lacks %q", c.id, body, l)
			}
		}
		if len(*calls) != 0 {
			t.Errorf("%s: calls before the answer: %v", c.id, *calls)
		}
	}
}

// A dirty-tree recover takes two dialogs, and the tree is discarded only after both.
func TestDirtyRecoverTakesTwoDialogs(t *testing.T) {
	m, calls := actModel(t)
	dirtyFiles = 3
	m, cmd := at(t, m, "dead", runeKey("x"))
	m = settle(t, m, cmd)
	m, cmd = send(t, m, runeKey("y"))
	m = settle(t, m, cmd)
	if len(m.modals) != 1 || m.modals[0].title != "Discard changes?" || !strings.Contains(m.modals[0].body[0], "3 changed files") {
		t.Fatalf("second dialog = %+v", m.modals)
	}
	if has(*calls, "recoverBead") {
		t.Fatalf("recovered before the second yes: %v", *calls)
	}
	m, cmd = send(t, m, runeKey("n"))
	m = settle(t, m, cmd)
	if has(*calls, "recoverBead") || len(m.modals) != 0 {
		t.Fatalf("n on the second dialog still recovered: %v", *calls)
	}
	m, cmd = at(t, m, "dead", runeKey("x"))
	m = settle(t, m, cmd)
	m, cmd = send(t, m, runeKey("y"))
	m = settle(t, m, cmd)
	m, cmd = send(t, m, runeKey("y"))
	settle(t, m, cmd)
	if !has(*calls, "recoverBead dead true") {
		t.Errorf("calls = %v, want a discarding recover", *calls)
	}
}

// A clean-tree recover asks once and does not discard.
func TestCleanRecoverAsksOnce(t *testing.T) {
	m, calls := actModel(t)
	m, cmd := at(t, m, "dead", runeKey("x"))
	m = settle(t, m, cmd)
	m, cmd = send(t, m, runeKey("y"))
	settle(t, m, cmd)
	if !has(*calls, "recoverBead dead false") {
		t.Errorf("calls = %v", *calls)
	}
}

// enter, b, p, +/=/- and an unblocked t call their seams with no dialog (FR26).
func TestUnguardedActionsFireAtOnce(t *testing.T) {
	for _, c := range []struct {
		name, id string
		k        tea.KeyMsg
		want     string
	}{
		{"enter launches", "sig", enterKey(), "startSession sig"},
		{"enter attaches", "sess", enterKey(), "switchSession sess"},
		{"enter launches an Initiative head", "ini", enterKey(), "startSession ini"},
		{"b launches", "sig", runeKey("b"), "startSession sig"},
		{"p pauses", "sig", runeKey("p"), "pauseBead sig"},
		{"p unpauses", "pz", runeKey("p"), "unpauseBead pz"},
		{"+ raises", "sig", runeKey("+"), "updateBead sig [--priority 1]"},
		{"= raises", "sig", runeKey("="), "updateBead sig [--priority 1]"},
		{"- lowers", "sig", runeKey("-"), "updateBead sig [--priority 3]"},
		{"enter runs", "res", enterKey(), "readyFrontier"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, calls := actModel(t)
			m, cmd := at(t, m, c.id, c.k)
			if len(m.modals) != 0 {
				t.Fatalf("opened a dialog: %+v", m.modals)
			}
			settle(t, m, cmd)
			if !has(*calls, c.want) {
				t.Errorf("calls = %v, want %q", *calls, c.want)
			}
		})
	}
}

// b starts without switching this terminal; enter does switch.
func TestBackgroundLaunchDoesNotSwitch(t *testing.T) {
	m, calls := actModel(t)
	m, cmd := at(t, m, "sig", runeKey("b"))
	settle(t, m, cmd)
	if !has(*calls, "startSession sig") || has(*calls, "switchSession") {
		t.Errorf("b calls = %v", *calls)
	}
	m, calls = actModel(t)
	m, cmd = at(t, m, "sig", enterKey())
	settle(t, m, cmd)
	if !has(*calls, "switchSession sig") {
		t.Errorf("enter calls = %v", *calls)
	}
}

// enter on a row that cannot launch calls nothing and says why on the status line (FR26).
func TestEnterOnNonLaunchableRows(t *testing.T) {
	for _, c := range []struct{ id, say string }{
		{"done", "closed"},
		{"done", "nothing to launch or run"},
		{"gone", "x recovers it"},
	} {
		m, calls := actModel(t)
		m = pick(t, m, "s", "any") // done is closed
		m, cmd := at(t, m, c.id, enterKey())
		if cmd != nil || len(m.modals) != 0 || len(*calls) != 0 {
			t.Errorf("%s: cmd=%v modals=%v calls=%v", c.id, cmd != nil, m.modals, *calls)
		}
		if s := statusOf(m); !strings.Contains(s, c.say) {
			t.Errorf("%s: status %q lacks %q", c.id, s, c.say)
		}
	}
	// An in-flight write.
	m, calls := actModel(t)
	m = selectBead(t, m, "sig")
	m.pending = map[string]string{"sig": "updating"}
	m, cmd := send(t, m, enterKey())
	if cmd != nil || len(*calls) != 0 || !strings.Contains(statusOf(m), "write in flight") {
		t.Errorf("in flight: cmd=%v calls=%v status=%q", cmd != nil, *calls, statusOf(m))
	}
}

// The guard refuses a second write on a key already pending, never queues it.
func TestPerKeyGuardRefusesSecondAction(t *testing.T) {
	m, calls := actModel(t)
	m, cmd := at(t, m, "sig", runeKey("p"))
	if cmd == nil || m.pending["sig"] == "" || !strings.Contains(statusOf(m), "pausing sig…") {
		t.Fatalf("no in-flight marker: pending=%v status=%q", m.pending, statusOf(m))
	}
	m, cmd = send(t, m, runeKey("+"))
	if cmd != nil || !strings.Contains(statusOf(m), "write in flight") {
		t.Errorf("second write: cmd=%v status=%q", cmd != nil, statusOf(m))
	}
	_ = calls
}

// The hints bar lists exactly the verbs that apply to the selected row, in ADR 0144
// §7's order, ending with ? keys (FR27).
func TestGraphHintsListApplicableVerbs(t *testing.T) {
	const edit = "e edit"
	const find = "/ search · s status · f kind · "
	for _, c := range []struct{ id, want string }{
		{"sig", "enter launch · b bg · d detail · " + find + "p pause · t transcript · z reject · " + edit + " · +/- prio · c checkup · a adopt · B add blocker · r retrospective"},
		{"pz", "enter launch · b bg · d detail · " + find + "p unpause · t transcript · z reject · " + edit + " · +/- prio · c checkup · a adopt · B add blocker · r retrospective"},
		{"done", "d detail · " + find + "t transcript · " + edit + " · c checkup"},
		{"ini", "enter launch · b bg · d detail · " + find + "p pause · t transcript · " + edit + " · +/- prio · c checkup · B add blocker · r retrospective"},
		{"res", "enter run · d detail · " + find + "p pause · t transcript · " + edit + " · +/- prio · c checkup · a adopt · B add blocker"},
		{"sess", "enter attach · d detail · " + find + "p pause · x drop · t transcript · " + edit + " · +/- prio · c checkup · a adopt · B add blocker · r retrospective"},
		{"live", "d detail · " + find + "p pause · x abort · t transcript · " + edit + " · +/- prio · c checkup · a adopt · B add blocker"},
		{"dead", "d detail · " + find + "p pause · x recover · t transcript · " + edit + " · +/- prio · c checkup · a adopt · B add blocker"},
	} {
		m, _ := actModel(t)
		m = selectBead(t, pick(t, m, "s", "any"), c.id) // done is closed
		if got := hintsOf(m); got != c.want {
			t.Errorf("%s: hints\n got  %s\n want %s", c.id, got, c.want)
		}
		l := lines(m.View())
		if bar := strings.TrimRight(l[len(l)-1], " "); !strings.HasSuffix(bar, "? keys") {
			t.Errorf("%s: bar %q does not end with ? keys", c.id, bar)
		}
	}
}

// Every launch path answers with a y/n dialog whose frame fits and runs no
// subprocess; the binding coverage of each row's mode holds.
func TestGraphActionBindingCoverage(t *testing.T) {
	for _, id := range []string{"sig", "done", "ini", "res", "sess", "live", "dead", "pz"} {
		m, _ := actModel(t)
		m = selectBead(t, pick(t, m, "s", "any"), id) // done is closed
		bound := map[string]bool{}
		for _, b := range m.modeBindings() {
			for _, k := range b.Keys() {
				bound[k] = true
			}
		}
		for _, k := range candidateKeys() {
			next, cmd := send(t, m, k)
			if handled := cmd != nil || next.View() != m.View(); handled != bound[k.String()] && k.String() != "4" {
				t.Errorf("%s: key %q handled=%v bound=%v", id, k, handled, bound[k.String()])
			}
		}
	}
}

// Rendering each action's dialog starts no subprocess and fits the floor.
func TestActionDialogsFit(t *testing.T) {
	log := fakeTools(t)
	defer func() {
		if got, _ := os.ReadFile(log); len(got) > 0 {
			t.Errorf("rendering started subprocesses:\n%s", got)
		}
	}()
	for _, c := range []struct{ id, k string }{{"sess", "x"}, {"live", "x"}, {"blk", ""}, {"resblk", "enter"}} {
		m, _ := actModel(t)
		k := runeKey(c.k)
		if c.k == "" || c.k == "enter" {
			k = enterKey()
		}
		m, _ = at(t, m, c.id, k)
		if len(m.modals) != 1 {
			t.Fatalf("%s: no dialog", c.id)
		}
		for _, sz := range [][2]int{{40, 12}, {80, 24}} {
			m, _ = send(t, m, tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
			if ls := lines(m.View()); len(ls) != sz[1] {
				t.Errorf("%s %v: %d lines", c.id, sz, len(ls))
			}
		}
	}
}

// Under a load error a dialog still draws over the notice and owns its keys, and q
// routes through the guarded quit: Abandon runs only after y/enter (FR20, FR24, ADR 0082).
func TestLoadErrorQuitKeepsGuard(t *testing.T) {
	m, _ := actModel(t)
	m, _ = at(t, m, "sess", runeKey("x"))
	m, _ = send(t, m, errMsg{errors.New("dolt down")})
	if v := ansi.Strip(m.View()); strings.Contains(v, "bd read failed") {
		t.Errorf("dialog hidden under the notice:\n%s", v)
	}
	if _, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyF5}); len(m.modals) != 1 || cmd != nil {
		t.Errorf("f5 reached past the dialog")
	}

	m, _ = actModel(t)
	m.runs = []dispatch.Inflight{{}}
	m.pending = map[string]string{"p1": "closing"}
	m, _ = send(t, m, errMsg{errors.New("dolt down")})
	m, cmd := send(t, m, runeKey("q"))
	if cmd != nil && cmd() == tea.Quit() {
		t.Fatalf("q quit at once with work in flight")
	}
	if len(m.modals) != 1 || !strings.Contains(m.modals[0].title, "Quit") {
		t.Fatalf("modals = %+v, want the Quit dialog", m.modals)
	}
	if _, cmd := send(t, m, runeKey("n")); cmd != nil && cmd() == tea.Quit() {
		t.Errorf("n quit")
	}
	if _, cmd := send(t, m, runeKey("y")); cmd == nil || cmd() != tea.Quit() {
		t.Errorf("y did not quit")
	}
}

// An orphaned worktree asks first: dropOrphan runs only after y or enter (FR24, ADR 0041 §4).
func TestOrphanDialogAsksFirst(t *testing.T) {
	for _, answer := range []struct {
		name string
		k    tea.KeyMsg
		ran  bool
	}{
		{"y", runeKey("y"), true}, {"enter", enterKey(), true}, {"n", runeKey("n"), false}, {"esc", escKey(), false},
	} {
		t.Run(answer.name, func(t *testing.T) {
			m, calls := actModel(t)
			b, _ := m.pinned("sig")
			next, _ := m.onBusy(launchDoneMsg{b: b}, "/tmp/mugicrew-wt-orphan/wt")
			m = next.(Model)
			if len(m.modals) != 1 || m.modals[0].title != "Drop orphaned worktree?" {
				t.Fatalf("modals = %+v", m.modals)
			}
			if has(*calls, "dropOrphan") {
				t.Fatalf("dropped before the answer: %v", *calls)
			}
			m, cmd := send(t, m, answer.k)
			m = settle(t, m, cmd)
			if got := has(*calls, "dropOrphan"); got != answer.ran {
				t.Errorf("after %s: dropOrphan called = %v, want %v (calls %v)", answer.name, got, answer.ran, *calls)
			}
			if len(m.modals) != 0 {
				t.Errorf("answer left a dialog open")
			}
		})
	}
}

// enter on an open Initiative head claims it once, then starts Luffy's cut session
// (FR1–3).
func TestEnterOnInitiativeHeadClaimsIt(t *testing.T) {
	m, calls := actModel(t)
	m, cmd := at(t, m, "ini", enterKey())
	settle(t, m, cmd)
	if !has(*calls, "hitlClaim ini") || !has(*calls, "startSession ini") {
		t.Errorf("calls = %v", *calls)
	}
}

// A paused Initiative claims only after y; answering n claims nothing (Spec FR4).
func TestPausedInitiativeClaimsAfterY(t *testing.T) {
	for _, c := range []struct {
		k    tea.KeyMsg
		want bool
	}{{runeKey("y"), true}, {runeKey("n"), false}} {
		m, calls := actModel(t)
		m, cmd := at(t, m, "pini", enterKey())
		m = settle(t, m, cmd)
		if has(*calls, "hitlClaim") {
			t.Fatalf("claimed before the answer: %v", *calls)
		}
		m, cmd = send(t, m, c.k)
		settle(t, m, cmd)
		if has(*calls, "hitlClaim pini") != c.want || has(*calls, "startSession pini") != c.want {
			t.Errorf("answer %v: calls = %v", c.k, *calls)
		}
	}
}

// An Initiative whose session fails to start releases the claim it took (Spec FR5).
func TestFailedInitiativeStartUnclaims(t *testing.T) {
	m, calls := actModel(t)
	seam(t, &startSession, func(bd.Bead, contract.AgentCommand) error { return errors.New("no tmux") })
	m, cmd := at(t, m, "ini", enterKey())
	settle(t, m, cmd)
	if !has(*calls, "hitlClaim ini") || !has(*calls, "hitlUnclaim ini") {
		t.Errorf("calls = %v", *calls)
	}
}

// Below the floor the too-small notice is all that shows, so no key but quit may
// act: a hidden dialog is neither confirmed nor cancelled, and no tab key opens one
// (FR15, FR21, FR24).
func TestBelowFloorKeysDoNothingButQuit(t *testing.T) {
	m, _ := actModel(t)
	m, _ = at(t, m, "sess", runeKey("x"))
	if len(m.modals) != 1 {
		t.Fatalf("modals = %+v, want the drop dialog", m.modals)
	}
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 39, Height: 11})
	for _, k := range []tea.KeyMsg{runeKey("y"), {Type: tea.KeyEnter}, runeKey("n"), {Type: tea.KeyEsc}} {
		var cmd tea.Cmd
		if m, cmd = send(t, m, k); cmd != nil || len(m.modals) != 1 {
			t.Fatalf("%s acted below the floor: cmd=%v modals=%d", k, cmd != nil, len(m.modals))
		}
	}
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Drop session?") {
		t.Fatalf("dialog lost after regrow:\n%s", v)
	}

	m, _ = actModel(t)
	m = selectBead(t, m, "sess")
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 39, Height: 11})
	if m, _ = send(t, m, runeKey("x")); len(m.modals) != 0 {
		t.Errorf("x opened a dialog below the floor")
	}
	if _, cmd := send(t, m, runeKey("q")); cmd == nil || cmd() != tea.Quit() {
		t.Errorf("q did not quit below the floor")
	}
}

// There is no l key: the HITL refusal must point at enter (FR34, ADR 0144 §7).
func TestRunNowHintHITLNamesEnterNotL(t *testing.T) {
	got := runNowHint(bd.Bead{ID: "x-1", Status: "open", Kind: contract.KindSignal}, nil, "", nil)
	if !strings.Contains(got, "press enter") || strings.Contains(got, "press l") || strings.Contains(got, ", not t") {
		t.Fatalf("hint = %q, want enter and neither l nor t", got)
	}
}

// A long pause note stays inside the dialog instead of overflowing it.
func TestPausedLaunchDialogFitsPauseLine(t *testing.T) {
	m, _ := actModel(t)
	beads := actionBeads()
	for i := range beads {
		if beads[i].ID == "pz" {
			beads[i].Labels = []string{"paused:" + strings.Repeat("x", 200)}
		}
	}
	m, _ = send(t, m, loadedMsg{beads: beads, sessions: actionSessions()})
	m, _ = at(t, m, "pz", enterKey())
	for _, run := range regexp.MustCompile(`x+`).FindAllString(ansi.Strip(m.View()), -1) {
		if max := m.dialogWidth() - 4; len(run) > max {
			t.Errorf("pause run of %d columns > %d inner", len(run), max)
		}
	}
}

// The detail pane and the Unparented text name enter, never t (ADR 0164, FR25/FR26).
func TestGraphDetailNamesEnterNeverT(t *testing.T) {
	m, _ := actModel(t)
	m, _ = send(t, m, loadedMsg{beads: actionBeads(), live: map[string]bool{"live": true}, sessions: actionSessions()})
	if d := ansi.Strip(selectBead(t, m, "res").tabAt(m.tab).(graphTab).detailText()); strings.Contains(d, "Press enter") || strings.Contains(d, "AFK —") {
		t.Errorf("res: AFK preview should be gone:\n%s", d)
	}
	for id, want := range map[string]string{
		"sess": "SESSION — enter attaches (resumes if exited).",
		"live": "build is running. x aborts it.",
		"dead": "build's effort is dead. x recovers it.",
		"gone": "gone's claim has no session here — x recovers it if it's stranded",
	} {
		d := ansi.Strip(selectBead(t, m, id).tabAt(m.tab).(graphTab).detailText())
		if !strings.Contains(d, want) {
			t.Errorf("%s: detail lacks %q:\n%s", id, want, d)
		}
		if strings.Contains(d, "Press t") || strings.Contains(d, " with t") {
			t.Errorf("%s: detail names t:\n%s", id, d)
		}
	}
}

// abortDone names the three outcomes: pids signalled, a dispatcher-only hold (ADR
// 0155), and no live process.
func TestAbortDoneStatus(t *testing.T) {
	for _, c := range []struct {
		pids []int
		held bool
		want string
	}{
		{[]int{41, 42}, false, "aborted run: signalled 41 42"},
		{nil, true, "aborted run: " + dispatch.AbortNote(true)},
		{nil, false, "aborted run: no live process; paused only"},
	} {
		next, _ := Model{}.abortDone("run", c.pids, c.held, nil)
		if s := next.(Model).status; s != c.want {
			t.Errorf("status = %q, want %q", s, c.want)
		}
	}
}

// armedModel is actModel with "sess" armed (an Auto-exit deadline set) and the
// finish seam stubbed to report armed, logging to the returned call log.
func armedModel(t *testing.T, armed bool) (Model, *[]string) {
	t.Helper()
	m, calls := actModel(t)
	old := finishSession
	t.Cleanup(func() { finishSession = old })
	finishSession = func(b bd.Bead) (bool, error) { *calls = append(*calls, "finishSession "+b.ID); return armed, nil }
	s := actionSessions()
	s["sess"] = session.Info{State: session.Alive, Session: "s1", AutoExit: time.Now().Add(time.Minute)}
	m, _ = send(t, m, loadedMsg{beads: actionBeads(), live: map[string]bool{"live": true}, sessions: s})
	return m, calls
}

// x on an armed session asks finish or drop: y finishes, d drops the same session,
// n and esc change nothing (FR1-4, FR11).
func TestArmedSessionXOffersFinishOrDrop(t *testing.T) {
	for _, c := range []struct {
		k          tea.KeyMsg
		finish, dr bool
	}{{runeKey("y"), true, false}, {runeKey("d"), false, true}, {runeKey("n"), false, false}, {escKey(), false, false}} {
		m, calls := armedModel(t, true)
		m, _ = at(t, m, "sess", runeKey("x"))
		if len(m.modals) != 1 || m.modals[0].title != "Finish session?" {
			t.Fatalf("modals = %+v, want one Finish session?", m.modals)
		}
		wide, _ := send(t, m, tea.WindowSizeMsg{Width: 300, Height: 60}) // wide: no wrapping
		if b := ansi.Strip(wide.View()); !strings.Contains(b, `finish "`) || !strings.Contains(b, `y finish (clean exit, publishes) · d drop (discards) · n cancel`) {
			t.Errorf("body = %q", b)
		}
		m, cmd := send(t, m, c.k)
		m = settle(t, m, cmd)
		if has(*calls, "finishSession sess") != c.finish || has(*calls, "dropSession sess") != c.dr || len(m.modals) != 0 {
			t.Errorf("%s: calls %v modals %d", c.k, *calls, len(m.modals))
		}
	}
}

// A finish that finds the session disarmed says so and touches nothing.
func TestFinishNotArmedSaysSo(t *testing.T) {
	m, calls := armedModel(t, false)
	m, _ = at(t, m, "sess", runeKey("x"))
	m, cmd := send(t, m, runeKey("y"))
	m = settle(t, m, cmd)
	if s := statusOf(m); !strings.Contains(s, "sess is no longer armed — not finished (x again to drop)") || has(*calls, "dropSession") {
		t.Errorf("status %q calls %v", s, *calls)
	}
}

// An unarmed row keeps Drop session?, and only an armed one hints finish/drop.
func TestUnarmedXStaysDropOnly(t *testing.T) {
	m, _ := armedModel(t, true)
	if h := hintsOf(selectBead(t, m, "sess")); !strings.Contains(h, "x finish/drop") {
		t.Errorf("armed hints = %s", h)
	}
	m, _ = actModel(t)
	if h := hintsOf(selectBead(t, m, "sess")); strings.Contains(h, "finish") {
		t.Errorf("unarmed hints = %s", h)
	}
	if m, _ = at(t, m, "sess", runeKey("x")); m.modals[0].title != "Drop session?" {
		t.Errorf("title = %q", m.modals[0].title)
	}
	if !strings.Contains(keyDropKey.Help().Desc, "finish or drop the session") {
		t.Errorf("help x = %q", keyDropKey.Help().Desc)
	}
}

// Run-now answers every refusal it can decide without I/O before the blocked y/n,
// so no dialog opens and no run-now seam is called (Spec D2, FR13–FR21).
func TestRunNowRefusesBeforeDialog(t *testing.T) {
	beads := append(actionBeads(),
		bd.Bead{ID: "pblk", Kind: contract.KindResearch, Status: "open", Title: "Paused blocked", Labels: []string{"paused:manual"}, BlockedBy: []string{"dep"}, Created: epoch},
		bd.Bead{ID: "chart", Kind: contract.KindChart, Container: true, Status: "open", Title: "A chart", Created: epoch},
		bd.Bead{ID: "kid", Kind: contract.KindResearch, Parent: "chart", Status: "open", Title: "Kid", Created: epoch},
		bd.Bead{ID: "hcp", Kind: contract.KindCheckpoint, Container: true, Status: "open", Title: "Held checkpoint", BlockedBy: []string{"dep"}, Created: epoch},
		bd.Bead{ID: "happr", Kind: contract.KindApproval, Parent: "hcp", Status: "closed", CloseReason: bd.ApprovedReason, Title: "Approved", Created: epoch},
	)
	for _, c := range []struct{ id, say string }{
		{"hcp", "land held by open blocker: Dependency"},
		{"dead", "dead's effort is not running — x recovers it"},
		{"done", "done is closed — nothing to launch or run"},
		{"pblk", "pblk is paused by hand"},
		{"chart", "chart: Chart not drained — 0/1 children closed"},
	} {
		t.Run(c.id, func(t *testing.T) {
			m, calls := actModel(t)
			m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{"live": true}, sessions: actionSessions()})
			m = pick(t, m, "s", "any") // done is closed
			m, cmd := at(t, m, c.id, enterKey())
			if cmd != nil || len(m.modals) != 0 || !strings.Contains(statusOf(m), c.say) || has(*calls, "readyFrontier") {
				t.Errorf("cmd=%v modals=%d status=%q calls=%v, want %q", cmd != nil, len(m.modals), statusOf(m), *calls, c.say)
			}
		})
	}
	t.Run("pending write", func(t *testing.T) {
		m, calls := actModel(t)
		m, _ = at(t, m, "resblk", runeKey("p")) // starts a write on it
		m, cmd := send(t, m, enterKey())
		if cmd != nil || len(m.modals) != 0 || !strings.Contains(statusOf(m), "write in flight") || has(*calls, "readyFrontier") {
			t.Errorf("cmd=%v modals=%d status=%q", cmd != nil, len(m.modals), statusOf(m))
		}
	})
}

// enter is the one action key: it runs AFK rows (retry, spec, land) (Spec D1, D4).
func TestEnterRunsAFKRows(t *testing.T) {
	beads := append(actionBeads(),
		bd.Bead{ID: "fail", Kind: contract.KindResearch, Status: "open", Title: "Failing research", Labels: []string{"paused:failing"}, Created: epoch},
		bd.Bead{ID: "chart", Kind: contract.KindChart, Container: true, Status: "open", Title: "A chart", Created: epoch},
		bd.Bead{ID: "cp", Kind: contract.KindCheckpoint, Container: true, Status: "open", Title: "A checkpoint", Created: epoch},
		bd.Bead{ID: "appr", Kind: contract.KindApproval, Parent: "cp", Status: "closed", CloseReason: bd.ApprovedReason, Title: "Approved", Created: epoch},
	)
	load := func(t *testing.T) Model {
		m, _ := actModel(t)
		m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{"live": true}, sessions: actionSessions()})
		return m
	}
	for _, c := range []struct{ id, verb string }{{"fail", "retry"}, {"chart", "spec"}, {"cp", "land"}} {
		m := load(t)
		if h := hintsOf(selectBead(t, m, c.id)); !strings.HasPrefix(h, "enter "+c.verb+" · ") {
			t.Errorf("%s: hints %q lack enter %s", c.id, h, c.verb)
		}
		m, cmd := at(t, m, c.id, enterKey())
		if cmd == nil || m.pending[c.id] == "" {
			t.Errorf("%s: enter did not dispatch: pending=%v status=%q", c.id, m.pending, statusOf(m))
		}
	}
	m, _ := actModel(t)
	if m, cmd := at(t, m, "res", runeKey("b")); cmd != nil || statusOf(m) != "b is HITL-only — enter runs it" {
		t.Errorf("b on AFK: cmd=%v status=%q", cmd != nil, statusOf(m))
	}
}

// An exited session's row hints enter resume; live and dead AFK rows hint no enter.
func TestGraphHintsEnterResume(t *testing.T) {
	m, _ := actModel(t)
	m, _ = send(t, m, loadedMsg{beads: actionBeads(), live: map[string]bool{"live": true}, sessions: map[string]session.Info{"sess": {State: session.Exited, Session: "s1"}}})
	if h := hintsOf(selectBead(t, m, "sess")); !strings.HasPrefix(h, "enter resume · ") {
		t.Errorf("hints %q", h)
	}
}

// The AFK preview line is gone (ADR 0265), so a retry row no longer names the verb.
func TestPreviewNamesRetryVerb(t *testing.T) {
	m, _ := actModel(t)
	m, _ = send(t, m, loadedMsg{beads: append(actionBeads(),
		bd.Bead{ID: "fail", Kind: contract.KindResearch, Status: "open", Title: "Failing", Labels: []string{"paused:failing"}, Created: epoch}),
		sessions: actionSessions()})
	d := ansi.Strip(selectBead(t, m, "fail").tabAt(m.tab).(graphTab).detailText())
	if strings.Contains(d, "Press enter") {
		t.Errorf("detail has an AFK preview:\n%s", d)
	}
}

// A launch that loses the claim race refuses in one line and reloads; nothing starts
// and nothing is unclaimed. Any other claim error stays a launch failure.
func TestLaunchClaimRefusals(t *testing.T) {
	for _, c := range []struct {
		name    string
		err     error
		want    string
		refused bool
	}{
		{"held", &bd.HeldError{Holder: "A", Err: errors.New("cannot reassign sig: held by \"A\"\nmore")}, "sig is claimed by A — not launched", true},
		{"since", fmt.Errorf("%w: raw\nstderr", bd.ErrClaimedSince), "sig was claimed since the last refresh — not launched", true},
		{"other", errors.New("boom"), "launch: claim: boom", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, calls := actModel(t)
			orig := hitlClaim
			t.Cleanup(func() { hitlClaim = orig })
			hitlClaim = func(string) error { return c.err }
			m, cmd := at(t, m, "sig", enterKey())
			var done launchDoneMsg
			var walk func(tea.Cmd)
			walk = func(c tea.Cmd) {
				if c == nil {
					return
				}
				switch msg := c().(type) {
				case tea.BatchMsg:
					for _, sub := range msg {
						walk(sub)
					}
				case launchDoneMsg:
					done = msg
				}
			}
			walk(cmd)
			if done.b.ID != "sig" {
				t.Fatalf("no launchDoneMsg")
			}
			nm, reload := m.launchDone(done)
			s := statusOf(nm.(Model))
			if !strings.Contains(s, c.want) || (c.refused && (strings.Contains(s, "claim:") || strings.Contains(s, "\n"))) {
				t.Errorf("status = %q, want %q", s, c.want)
			}
			if (reload != nil) != c.refused || has(*calls, "startSession") || has(*calls, "hitlUnclaim") {
				t.Errorf("reload=%v calls=%v", reload != nil, *calls)
			}
		})
	}
}
