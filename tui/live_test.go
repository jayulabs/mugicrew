package tui

import (
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/session"
)

// TestMain keeps every test off the real bd, tmux, git and loop lock.
func TestMain(m *testing.M) {
	eventlogtest.Discard()
	listAll = func() ([]bd.Bead, error) { return mirrorBeads, nil }
	listOpen = func() ([]bd.Bead, error) { return nil, nil }
	defaultIDs = func() ([]string, error) { return ids(mirrorBeads), nil }
	listByID = func(want []string) ([]bd.Bead, error) { return withIDs(mirrorBeads, want), nil }
	listComments = func(string) ([]bd.Comment, error) { return nil, nil }
	rollupLineage = func() (*bd.Lineage, error) { return bd.NewLineage(nil), nil }
	rollupEvents = func() ([]eventlog.Event, error) { return nil, nil }
	listChildren = func(string) ([]bd.Bead, error) { return nil, nil }
	listDependents = func(string) ([]string, error) { return nil, nil }
	listWithAncestors = func(string) ([]bd.Bead, error) { return nil, nil }
	searchClosed = func(string) ([]bd.Bead, error) { return nil, nil }
	readKinds = func() (map[string]contract.Kind, error) { return nil, nil }
	readSessions = func() (map[string]session.Info, error) { return nil, nil }
	readLoop = func() (looplock.Status, error) { return looplock.Status{State: looplock.Off}, nil }
	commonDir = func() (string, error) { return "", errors.New("commonDir not stubbed") }
	dir, _ := os.MkdirTemp("", "recents")
	recentsPath = func() string { return filepath.Join(dir, "recents.json") } // every test's acts land in one throwaway file
	readRefs = func() (map[string]bool, map[string]string, error) { return nil, nil, nil }
	recordClient = func() error { return nil }
	setBars = func(map[string]bool, map[string]string, map[string]session.Info) error { return nil }
	loopVerb = func(...string) (string, error) { return "", errors.New("loopVerb not stubbed") }
	startHost = func(*exec.Cmd) error { return errors.New("startHost not stubbed") } // no test forks tui.test
	pauseBead = func(string, string) error { return errors.New("pauseBead not stubbed") }
	newSignal = func(string, string) (string, error) { return "", errors.New("newSignal not stubbed") }
	newInitiative = func(string, string, string) (string, error) { return "", errors.New("newInitiative not stubbed") }
	newSurgery = func(string, string, string, bool) (string, string, error) {
		return "", "", errors.New("newSurgery not stubbed")
	}
	showBead = func(string) (bd.Bead, error) { return bd.Bead{}, errors.New("showBead not stubbed") }
	updateBead = func(string, []string) error { return errors.New("updateBead not stubbed") }
	closeBead = func(string, string) error { return errors.New("closeBead not stubbed") }
	adoptChain = func(string, string) error { return errors.New("adoptChain not stubbed") }
	releaseChain = func(string) error { return errors.New("releaseChain not stubbed") }
	addBlocker = func(string, string) error { return errors.New("addBlocker not stubbed") }
	removeBlocker = func(string, string) error { return errors.New("removeBlocker not stubbed") }
	startSession = func(bd.Bead, contract.AgentCommand) error { return errors.New("startSession not stubbed") }
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

var (
	loopOff      = looplock.Status{State: looplock.Off}
	loopOn       = looplock.Status{State: looplock.On, Pid: 7}
	loopDraining = looplock.Status{State: looplock.Draining, Pid: 7, Inflight: []string{"Build the lock", "Wire the TUI toggle"}}
)

func fakeLoopVerb(t *testing.T) *[][]string {
	t.Helper()
	return fakeLoopVerbOut(t, "", nil)
}

// fakeLoopVerbOut is fakeLoopVerb returning out and err from every verb.
func fakeLoopVerbOut(t *testing.T, out string, err error) *[][]string {
	t.Helper()
	orig := loopVerb
	t.Cleanup(func() { loopVerb = orig })
	var calls [][]string
	loopVerb = func(args ...string) (string, error) { calls = append(calls, args); return out, err }
	return &calls
}

// settle runs a command's message back through Update, so a verb's Cmd lands.
func settle(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok { // a verb's write rides with its Recents save
		for _, c := range batch {
			m = settle(t, m, c)
		}
		return m
	}
	m, next := send(t, m, msg)
	if _, ok := msg.(pickLogKindMsg); ok { // the picker waits on the kinds read
		return settle(t, m, next)
	}
	if m.tabAt(tabLogs).(logsTab).scan.searching { // a filter scan streams until done
		return settle(t, m, next)
	}
	return m
}

func header(m Model) string { return lines(m.View())[0] }

// A load folds the sessions, beads and loop into the header's right side (FR6).
func TestHeaderCountsFromLoad(t *testing.T) {
	m := sized(t, 120, 30)
	if h := header(m); !strings.Contains(h, dimSt.Render("0 HITL")) || !strings.Contains(h, dimSt.Render("0 AFK")) || !strings.HasSuffix(strings.TrimRight(h, " "), "loop off") {
		t.Errorf("before any load: header = %q, want dim 0 HITL, 0 AFK and loop off", h)
	}
	sessions := map[string]session.Info{
		"a": {State: session.Alive},
		"b": {State: session.Alive, Waiting: session.WaitTurn},
		"c": {State: session.Exited},
	}
	beads := []bd.Bead{
		{ID: "a", Status: "in_progress"},
		{ID: "b", Status: "in_progress"},
		{ID: "p", Status: "open", Labels: []string{"paused:manual"}},
		{ID: "ev", Event: true, Labels: []string{"paused:manual"}},
	}
	m, _ = send(t, m, loadedMsg{beads: beads, sessions: sessions, live: map[string]bool{"a": true, "b": true, "c": false}, loop: looplock.Status{State: looplock.On, Pid: 7}})
	h := header(m)
	for _, want := range []string{"mugicrew", aliveSt.Render("2 HITL"), afkSt.Render("2 AFK"), "1 waiting", "1‖", "loop on"} {
		if !strings.Contains(h, want) {
			t.Errorf("header %q lacks %q", h, want)
		}
	}
	if lipgloss.Width(h) > 120 {
		t.Errorf("header %q is %d wide", h, lipgloss.Width(h))
	}
	m, _ = send(t, m, loadedMsg{beads: beads, sessions: sessions, loop: loopDraining})
	if h := header(m); !strings.Contains(h, "loop draining") {
		t.Errorf("draining header = %q", h)
	}
}

// A dead AFK verdict is not counted, and a narrow header drops both counts but keeps the loop.
func TestHeaderAFKCount(t *testing.T) {
	m := sized(t, 120, 30)
	m, _ = send(t, m, loadedMsg{live: map[string]bool{"a": true, "c": false}})
	if h := header(m); !strings.Contains(h, afkSt.Render("1 AFK")) || !strings.Contains(h, dimSt.Render("0 HITL")) {
		t.Errorf("header = %q, want 1 AFK and dim 0 HITL", h)
	}
	m = sized(t, floorCols, 30)
	st := loopDraining
	st.Rev = "abc1234def5678"
	m, _ = send(t, m, loadedMsg{live: map[string]bool{"a": true}, loop: st})
	if h := header(m); strings.Contains(h, "AFK") || strings.Contains(h, "HITL") || !strings.Contains(h, "loop draining") {
		t.Errorf("narrow header = %q, want no counts and the loop segment", h)
	}
}

// The header fits every width from the floor up, dropping detail before clipping.
func TestHeaderFitsNarrow(t *testing.T) {
	st := loopDraining
	st.Rev = "abc1234def5678"
	for w := floorCols; w <= 100; w++ {
		m := sized(t, w, 20)
		m, _ = send(t, m, loadedMsg{loop: st, sessions: map[string]session.Info{"a": {State: session.Alive}}})
		if got := lipgloss.Width(header(m)); got > w {
			t.Fatalf("w=%d: header is %d wide", w, got)
		}
	}
}

// A load error renders the notice in place of the frame, and a good load clears it (FR15).
func TestLoadErrorNotice(t *testing.T) {
	m := sized(t, 80, 24)
	m, _ = send(t, m, errMsg{errors.New("dolt down")})
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "bd read failed: dolt down") || strings.Contains(v, "1 Ideate") || len(lines(v)) != 24 {
		t.Errorf("load error view:\n%s", v)
	}
	m, _ = send(t, m, loadedMsg{})
	if v := ansi.Strip(m.View()); strings.Contains(v, "bd read failed") || !strings.Contains(v, "1 Ideate") {
		t.Errorf("good load did not clear the notice:\n%s", v)
	}
}

// The status line shows the last action's result on the frame's fourth-from-last row (FR12).
func TestStatusLine(t *testing.T) {
	m := sized(t, 80, 24)
	m.status = "closed b1"
	ls := lines(m.View())
	if !strings.Contains(ls[len(ls)-2], "closed b1") {
		t.Errorf("status row = %q", ls[len(ls)-2])
	}
}

var (
	loopFromSource = looplock.Status{State: looplock.On, Pid: 7, Rev: "abc1234def5678"}
	loopRestart    = looplock.Status{State: looplock.Draining, Pid: 7, Rev: "abc1234def5678", RestartTo: "def5678abc1234", Inflight: []string{"A"}}
)

// o on any tab opens the Loop menu: the status text and the options for the
// loop's state, esc always last (ADR 0288 §4).
func TestOLoopMenuOptions(t *testing.T) {
	for _, c := range []struct {
		name     string
		st       looplock.Status
		starting bool
		want     string
		note     string
	}{
		{"off", loopOff, false, "s start · esc close", ""},
		{"on", loopFromSource, false, "r restart · s stop · esc close", ""},
		{"limited", looplock.Status{State: looplock.Limited, Pid: 7, Rev: "abc1234", ResetsAt: time.Now(), LimitSource: "usage"}, false, "r restart · s stop · esc close", ""},
		{"restarting", loopRestart, false, "s stop · esc close", ""},
		{"stop drain", loopDraining, false, "k keep on · f force stop · esc close", ""},
		{"installed", looplock.Status{State: looplock.On, Pid: 7, StorePath: "/usr/bin/mugicrew"}, false, "s stop · esc close", "not from source: stop and start it to change builds"},
		{"starting", loopOff, true, "esc close", "waiting for the loop to write its lock…"},
	} {
		for _, tab := range []string{"1", "$"} {
			m := sized(t, 100, 30)
			m, _ = send(t, m, runeKey(tab))
			m.loop, m.loopStarting = c.st, c.starting
			m, cmd := send(t, m, runeKey("o"))
			v := ansi.Strip(m.View())
			if cmd != nil || len(m.modals) != 1 {
				t.Fatalf("%s: o ran a verb or opened %d modals", c.name, len(m.modals))
			}
			for _, want := range []string{"Loop", c.st.State, c.want, c.note} {
				if !strings.Contains(v, want) {
					t.Errorf("%s: menu lacks %q:\n%s", c.name, want, v)
				}
			}
		}
	}
}

// The menu body is the loop status fields: dim labels, the state value colored as the
// tab bar colors its word, a blank row between sections, long values hanging under the
// value column (ADR 0295 §4).
func TestOLoopMenuStatusFields(t *testing.T) {
	store := "/nix/store/" + strings.Repeat("a", 70) + "/bin/mugicrew"
	for _, c := range []struct {
		name  string
		st    looplock.Status
		style lipgloss.Style
		state string
	}{
		{"off", loopOff, dimSt, "off"},
		{"on", looplock.Status{State: looplock.On, Pid: 7, Rev: "abc1234def5678", StorePath: store}, aliveSt, "on"},
		{"on stale", looplock.Status{State: looplock.On, Pid: 7, Rev: "abc1234def5678", StorePath: store, FailedRev: "def5678abc1234"}, warnSt, "on (stale)"},
		{"restart drain", loopRestart, warnSt, "draining"},
	} {
		m := sized(t, 100, 30)
		m.loop = c.st
		m, _ = send(t, m, runeKey("o"))
		raw := m.View()
		if want := softSt.Render("state: ") + c.style.Render(c.state); !strings.Contains(raw, want) {
			t.Errorf("%s: state row not styled as %q:\n%s", c.name, want, raw)
		}
		v := ansi.Strip(raw)
		if !strings.Contains(v, "state: "+c.state) {
			t.Errorf("%s: menu lacks the state field:\n%s", c.name, v)
		}
		if c.st.StorePath == "" {
			continue
		}
		ls := lines(v)
		i := slices.IndexFunc(ls, func(l string) bool { return strings.Contains(l, "pid: 7") })
		if i < 0 || !strings.Contains(ls[i+1], "│"+strings.Repeat(" ", 58)+"│") {
			t.Fatalf("%s: no blank row after the state section:\n%s", c.name, v)
		}
		j := slices.IndexFunc(ls, func(l string) bool { return strings.Contains(l, "store: /nix") })
		if j < 0 || !strings.Contains(ls[j+1], "│        aaaa") {
			t.Errorf("%s: store path not wrapped under the value column:\n%s", c.name, v)
		}
	}
}

// Each option closes the menu and runs its verb, watched when the TUI is.
func TestOLoopMenuRunsVerb(t *testing.T) {
	for _, c := range []struct {
		st    looplock.Status
		k     string
		verb  []string
		pend  string
		watch bool
	}{
		{loopOff, "s", []string{"start"}, startingLoop, false},
		{loopOff, "s", []string{"start", "--watch"}, startingLoop, true},
		{loopFromSource, "s", []string{"stop"}, stoppingLoop, false},
		{loopRestart, "s", []string{"stop"}, stoppingLoop, false},
		{loopFromSource, "r", []string{"restart"}, restartingLoop, true},
		{loopDraining, "k", []string{"start"}, resumingLoop, false},
		{loopDraining, "k", []string{"start", "--watch"}, resumingLoop, true},
		{loopDraining, "f", []string{"stop", "--force"}, stoppingLoop, false}, // f is its own confirmation
	} {
		calls := fakeLoopVerb(t)
		m := sized(t, 100, 30).WithWatch(c.watch)
		m.loop = c.st
		m, _ = send(t, m, runeKey("o"))
		m, cmd := send(t, m, runeKey(c.k))
		if len(m.modals) != 0 || cmd == nil || !strings.Contains(ansi.Strip(m.View()), c.pend) {
			t.Fatalf("%s %s: modals=%d cmd=%v status %q", c.st.State, c.k, len(m.modals), cmd != nil, m.status)
		}
		settle(t, m, cmd)
		if len(*calls) != 1 || !slices.Equal((*calls)[0], c.verb) {
			t.Errorf("%s %s: ran %v, want %v", c.st.State, c.k, *calls, c.verb)
		}
	}
	// a key the state does not offer, and esc, run nothing
	for _, c := range []struct {
		st looplock.Status
		k  tea.KeyMsg
	}{{loopOff, runeKey("r")}, {loopFromSource, runeKey("f")}, {loopDraining, runeKey("s")}, {loopOn, tea.KeyMsg{Type: tea.KeyEsc}}} {
		calls := fakeLoopVerb(t)
		m := sized(t, 100, 30)
		m.loop = c.st
		m, _ = send(t, m, runeKey("o"))
		m, cmd := send(t, m, c.k)
		if cmd != nil || len(*calls) != 0 {
			t.Errorf("%s %s ran a verb", c.st.State, c.k)
		}
		if esc := c.k.Type == tea.KeyEsc; esc != (len(m.modals) == 0) {
			t.Errorf("%s %s: modals=%d", c.st.State, c.k, len(m.modals))
		}
	}
}

// r shows the pending footer, then the verb's stdout on exit 0 and its error on exit 1.
func TestORestartFooter(t *testing.T) {
	for _, c := range []struct {
		out  string
		err  error
		want string
	}{
		{"loop is already on main abc1234\n", nil, "loop is already on main abc1234"},
		{"", errors.New("build of main def5678 failed (see build.failed in events)"), "build of main def5678 failed"},
	} {
		fakeLoopVerbOut(t, c.out, c.err)
		m := sized(t, 100, 30)
		m.loop = loopFromSource
		m, _ = send(t, m, runeKey("o"))
		m, cmd := send(t, m, runeKey("r"))
		if !strings.Contains(ansi.Strip(m.View()), restartingLoop) || !m.loopRestarting {
			t.Fatalf("no pending footer:\n%s", ansi.Strip(m.View()))
		}
		m = settle(t, m, cmd)
		v := ansi.Strip(m.View())
		if m.loopRestarting || strings.Contains(v, restartingLoop) || !strings.Contains(v, c.want) {
			t.Errorf("footer after %q/%v:\n%s", c.out, c.err, v)
		}
	}
	// other verbs flash only on failure
	fakeLoopVerbOut(t, "loop stopping\n", nil)
	m := sized(t, 100, 30)
	m.loop = loopFromSource
	m, _ = send(t, m, runeKey("o"))
	m, cmd := send(t, m, runeKey("s"))
	if m = settle(t, m, cmd); strings.Contains(ansi.Strip(m.View()), "loop stopping") {
		t.Error("stop flashed its stdout")
	}
}

// o while a restart is out shows the options dimmed under restart in progress, and
// its keys do nothing; the return clears it.
func TestOMenuMidRestart(t *testing.T) {
	calls := fakeLoopVerb(t)
	m := sized(t, 100, 30)
	m.loop = loopFromSource
	m, _ = send(t, m, runeKey("o"))
	m, cmd := send(t, m, runeKey("r"))
	m, _ = send(t, m, runeKey("o"))
	v := ansi.Strip(m.View())
	if len(m.modals) != 1 || !strings.Contains(v, "restart in progress") || !strings.Contains(v, "r restart · s stop · esc close") {
		t.Fatalf("mid-restart menu:\n%s", v)
	}
	m, cmd2 := send(t, m, runeKey("s"))
	if cmd2 != nil || len(m.modals) != 1 {
		t.Error("s acted during a restart")
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = settle(t, m, cmd)
	if m.loopRestarting || len(*calls) != 1 {
		t.Errorf("restarting=%v calls=%v", m.loopRestarting, *calls)
	}
}

// f5 refreshes through the load seam.
func TestF5Refreshes(t *testing.T) {
	seam(t, &defaultIDs, func() ([]string, error) { return []string{"x"}, nil })
	seam(t, &listByID, func([]string) ([]bd.Bead, error) { return []bd.Bead{{ID: "x"}}, nil })
	m := sized(t, 80, 24)
	m, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyF5})
	if cmd == nil {
		t.Fatal("f5 returned no command")
	}
	var got *loadedMsg
	for _, msg := range flatten(cmd()) {
		if l, ok := msg.(loadedMsg); ok {
			got = &l
		}
	}
	if got == nil || len(got.beads) != 1 {
		t.Fatalf("f5 did not load: %+v", got)
	}
	_ = m
}

// flatten runs a possibly batched command's messages (log reads included).
func flatten(msg tea.Msg) []tea.Msg {
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			if c != nil {
				out = append(out, flatten(c())...)
			}
		}
		return out
	}
	return []tea.Msg{msg}
}

// q with nothing in flight quits; with a pending write or a run-now effort it
// opens a dialog and the dialog decides (FR20, 25).
func TestQuitGuard(t *testing.T) {
	_, cmd := send(t, sized(t, 80, 24), runeKey("q"))
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q with nothing in flight did not quit")
	}
	inflight := map[string]Model{}
	pending := sized(t, 100, 30)
	pending.pending = map[string]string{"b1": "closing"}
	inflight["pending"] = pending
	for name, m := range inflight {
		for _, key := range []tea.KeyMsg{runeKey("q"), {Type: tea.KeyCtrlC}} {
			open, cmd := send(t, m, key)
			if cmd != nil || len(open.modals) != 1 {
				t.Fatalf("%s %s: no dialog (cmd=%v modals=%d)", name, key, cmd != nil, len(open.modals))
			}
			for _, no := range []tea.KeyMsg{runeKey("n"), {Type: tea.KeyEsc}} {
				if closed, cmd := send(t, open, no); cmd != nil || len(closed.modals) != 0 {
					t.Errorf("%s: %s did not cancel", name, no)
				}
			}
		}
	}
	open, _ := send(t, pending, runeKey("q"))
	if v := ansi.Strip(open.View()); strings.Contains(v, "run-now") || !strings.Contains(v, "1 write in flight: b1") {
		t.Errorf("quit dialog does not name the write:\n%s", v)
	}
	_, cmd = send(t, open, runeKey("y"))
	if cmd == nil {
		t.Fatal("y did not quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("y did not quit")
	}
}

// A run-now effort is a detached host once its press has returned: with nothing
// else in flight q and ctrl+c quit at once and write no pause (ADR 0332, FR 2, 4).
func TestQuitIgnoresRunNowHosts(t *testing.T) {
	pauses := 0
	orig := pauseBead
	t.Cleanup(func() { pauseBead = orig })
	pauseBead = func(string, string) error { pauses++; return nil }
	for _, key := range []tea.KeyMsg{runeKey("q"), {Type: tea.KeyCtrlC}} {
		m, cmd := send(t, sized(t, 100, 30), key)
		if cmd == nil || len(m.modals) != 0 {
			t.Fatalf("%s: no immediate quit (modals=%d)", key, len(m.modals))
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%s did not quit", key)
		}
	}
	if pauses != 0 {
		t.Errorf("quit wrote %d pauses", pauses)
	}
}

// The press itself (reads, claim, spawn) is the pending "dispatching" key, which
// still guards quit.
func TestQuitGuardsARunNowPress(t *testing.T) {
	m := sized(t, 100, 30)
	m.pending = map[string]string{"b1": "dispatching"}
	m, cmd := send(t, m, runeKey("q"))
	if cmd != nil || len(m.modals) != 1 {
		t.Fatalf("no dialog for a run-now press in flight (modals=%d)", len(m.modals))
	}
}

func testConfirm(ran *bool) dialog {
	return confirmDialog("Drop session?", []string{"kills the session"}, "b1", func(m Model, d dialog) (Model, tea.Cmd) {
		*ran = d.id == "b1"
		return m, nil
	})
}

func testPicker(picked *string) dialog {
	return pickerDialog("Status", []string{"any", "open", "closed"}, "", func(m Model, d dialog) (Model, tea.Cmd) {
		*picked = d.items[d.cur]
		return m, nil
	})
}

// A confirm dialog runs its action on the pinned id only after y/enter (FR24).
func TestConfirmDialog(t *testing.T) {
	for _, c := range []struct {
		k   tea.KeyMsg
		run bool
	}{{runeKey("y"), true}, {tea.KeyMsg{Type: tea.KeyEnter}, true}, {runeKey("n"), false}, {tea.KeyMsg{Type: tea.KeyEsc}, false}, {runeKey("x"), false}} {
		var ran bool
		m := sized(t, 80, 24).push(testConfirm(&ran))
		if v := ansi.Strip(m.View()); !strings.Contains(v, "Drop session?") || !strings.Contains(v, "y/enter") || !strings.Contains(v, "n/esc") {
			t.Fatalf("dialog view:\n%s", v)
		}
		m, _ = send(t, m, c.k)
		if ran != c.run {
			t.Errorf("%s: ran=%v, want %v", c.k, ran, c.run)
		}
		if open := len(m.modals) == 1; open != (c.k.String() == "x") {
			t.Errorf("%s: dialog open=%v", c.k, open)
		}
	}
}

// An input dialog edits the focused field, tab moves between a form's two, enter
// submits both and esc cancels (FR21).
func TestInputDialog(t *testing.T) {
	var got []string
	d := inputDialog("File Rollback", []string{"title", "evidence"}, "c1", func(m Model, d dialog) (Model, tea.Cmd) {
		for _, in := range d.inputs {
			got = append(got, in.text)
		}
		return m, nil
	})
	m := sized(t, 80, 24).push(d)
	for _, k := range []tea.KeyMsg{runeKey("1"), runeKey("a"), {Type: tea.KeyTab}, runeKey("q"), runeKey("?")} {
		m, _ = send(t, m, k)
	}
	if m.tab != 0 || len(m.modals) != 1 {
		t.Fatalf("typing leaked to the shell: tab %d modals %d", m.tab, len(m.modals))
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !slices.Equal(got, []string{"1a", "q?"}) || len(m.modals) != 0 {
		t.Errorf("submitted %q, modals %d", got, len(m.modals))
	}
	got = nil
	m = sized(t, 80, 24).push(d)
	m, _ = send(t, m, runeKey("z"))
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if got != nil || len(m.modals) != 0 {
		t.Errorf("esc submitted %q", got)
	}
}

// A picker moves with j/k, picks with enter and cancels with esc.
func TestPickerDialog(t *testing.T) {
	var picked string
	m := sized(t, 80, 24).push(testPicker(&picked))
	for _, k := range []tea.KeyMsg{runeKey("j"), runeKey("j"), runeKey("k")} {
		m, _ = send(t, m, k)
	}
	if _, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc}); picked != "" {
		t.Fatal("esc picked")
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if picked != "open" || len(m.modals) != 0 {
		t.Errorf("picked %q, modals %d", picked, len(m.modals))
	}
}

// While a dialog or picker is open no digit or tab key reaches the shell (FR21).
func TestModalBlocksShellKeys(t *testing.T) {
	var ran bool
	var picked string
	for name, d := range map[string]dialog{"dialog": testConfirm(&ran), "picker": testPicker(&picked)} {
		m := sized(t, 80, 24).push(d)
		for _, k := range []tea.KeyMsg{runeKey("1"), runeKey("5"), runeKey("*"), {Type: tea.KeyTab}, {Type: tea.KeyShiftTab}, {Type: tea.KeyLeft}, {Type: tea.KeyRight}, runeKey("o"), runeKey("q"), runeKey("?"), {Type: tea.KeyF5}} {
			next, cmd := send(t, m, k)
			if next.tab != 0 || cmd != nil || len(next.modals) == 0 {
				t.Errorf("%s: %s reached the shell (tab %d)", name, k, next.tab)
			}
		}
	}
}

// A write that lands under an open modal is held: the modal stays untouched and
// the result shows once the stack empties (the flash waits, FR12).
func TestFlashWaitsBehindModal(t *testing.T) {
	var ran bool
	m := sized(t, 80, 24).push(testConfirm(&ran))
	m.pending = map[string]string{"b1": "closing"}
	m, _ = send(t, m, writeDoneMsg{key: "b1", apply: func(m Model) (tea.Model, tea.Cmd) {
		m.setStatus("closed b1")
		return m, nil
	}})
	if m.status == "closed b1" || len(m.modals) != 1 || len(m.pending) != 0 {
		t.Fatalf("flash showed under a modal: status %q modals %d pending %v", m.status, len(m.modals), m.pending)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.status != "closed b1" {
		t.Errorf("after the modal closed status = %q", m.status)
	}
}

// The ✕ close-failed flash seeds on the first read, then waits behind a modal
// rather than consuming itself under it; ✓ finished comes from the event log.
func TestFlashCloseFailedAndFinished(t *testing.T) {
	m := sized(t, 120, 30)
	closing := map[string]session.Info{"h1": {State: session.Alive, Closing: true}}
	m, _ = send(t, m, loadedMsg{sessions: closing})
	var ran bool
	m = m.push(testConfirm(&ran))
	m, _ = send(t, m, loadedMsg{sessions: map[string]session.Info{"h1": {State: session.Exited}}})
	if m.status != "" {
		t.Fatalf("flash fired under a modal: %q", m.status)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m, _ = send(t, m, loadedMsg{sessions: map[string]session.Info{"h1": {State: session.Exited}}})
	if want := "✕ h1 close failed — resume or drop"; m.status != want {
		t.Errorf("status = %q, want %q", m.status, want)
	}
	m.status = ""
	t0 := time.Unix(100, 0)
	seed := logsMsg{path: "p", changed: true, events: []eventlog.Event{{Msg: "hitl.exited", Bead: "old", Time: t0, Raw: map[string]any{}}}, digest: []eventlog.Event{{Msg: "hitl.exited", Bead: "old", Time: t0, Raw: map[string]any{}}}}
	m, _ = send(t, m, seed)
	if m.status != "" {
		t.Fatalf("history flashed: %q", m.status)
	}
	next := logsMsg{path: "p", changed: true, events: []eventlog.Event{{Msg: "hitl.exited", Bead: "old", Time: t0, Raw: map[string]any{}}, {Msg: "hitl.exited", Bead: "h2", Time: t0.Add(1), Raw: map[string]any{}}}, digest: []eventlog.Event{{Msg: "hitl.exited", Bead: "old", Time: t0, Raw: map[string]any{}}, {Msg: "hitl.exited", Bead: "h2", Time: t0.Add(1), Raw: map[string]any{}}}}
	m, _ = send(t, m, next)
	if m.status != "✓ h2 finished" {
		t.Errorf("status = %q, want ✓ h2 finished", m.status)
	}
	// hitl.continued is in the digest but never flashes finished (FR19).
	if !inDigest(eventlog.Event{Msg: "hitl.continued"}) {
		t.Error("hitl.continued not in the digest")
	}
	m.status = ""
	cont := eventlog.Event{Msg: "hitl.continued", Bead: "h3", Time: t0.Add(2), Raw: map[string]any{}}
	m, _ = send(t, m, logsMsg{path: "p", changed: true, events: []eventlog.Event{cont}, digest: []eventlog.Event{cont}})
	if m.status != "" {
		t.Errorf("hitl.continued flashed %q", m.status)
	}
}

// A second write on a key is refused, never queued (ADR 0098 §3).
func TestStartWriteRefusesSecond(t *testing.T) {
	m := sized(t, 80, 24)
	ran := 0
	run := func() writeDoneMsg { ran++; return writeDoneMsg{key: "b1"} }
	m1, cmd := m.startWrite("b1", "closing", run)
	if cmd == nil || m1.(Model).pending["b1"] != "closing" || m1.(Model).status != "closing b1…" {
		t.Fatalf("first write: %+v", m1.(Model).pending)
	}
	m2, cmd := m1.(Model).startWrite("b1", "closing", run)
	if cmd != nil || m2.(Model).status != "b1: write in flight, wait" {
		t.Errorf("second write: cmd=%v status %q", cmd != nil, m2.(Model).status)
	}
}

func openModes(t *testing.T) map[string]Model {
	t.Helper()
	var ran bool
	var picked string
	base := sized(t, 120, 30)
	return map[string]Model{
		"dialog": base.push(testConfirm(&ran)),
		"picker": base.push(testPicker(&picked)),
	}
}

// Bindings are the single source for the modals too: every bound key is handled,
// every handled key bound.
func TestModalBindingCoverage(t *testing.T) {
	for name, m := range openModes(t) {
		bound := map[string]bool{}
		for _, b := range m.modeBindings() {
			for _, k := range b.Keys() {
				bound[k] = true
			}
		}
		for _, k := range candidateKeys() {
			next, cmd := send(t, m, k)
			handled := cmd != nil || next.View() != m.View()
			if handled != bound[k.String()] {
				t.Errorf("%s: key %q handled=%v bound=%v", name, k, handled, bound[k.String()])
			}
			delete(bound, k.String())
		}
		if len(bound) > 0 {
			t.Errorf("%s: bound keys no candidate covers: %v", name, bound)
		}
	}
}

// Rendering a modal runs no subprocess (ADR 0101 §4).
func TestModalViewRunsNoSubprocess(t *testing.T) {
	log := fakeTools(t)
	modes := openModes(t)
	for name, m := range modes {
		m.View()
		if got, _ := os.ReadFile(log); len(got) > 0 {
			t.Fatalf("View of the %s started subprocesses:\n%s", name, got)
		}
	}
}

// Every modal frame fits the terminal, at the floor and above.
func TestModalFrameFits(t *testing.T) {
	var ran bool
	var picked string
	for _, sz := range [][2]int{{40, 12}, {80, 24}} {
		for _, d := range []dialog{testConfirm(&ran), testPicker(&picked), inputDialog("T", []string{"a", "b"}, "", nil)} {
			ls := lines(sized(t, sz[0], sz[1]).push(d).View())
			if len(ls) != sz[1] {
				t.Errorf("%v: %d lines", sz, len(ls))
			}
			for _, l := range ls {
				if lipgloss.Width(l) > sz[0] {
					t.Errorf("%v: %q too wide", sz, l)
				}
			}
		}
	}
}

// A binding with no help text is handled but never shown: no blank hint item.
func TestHintsHaveNoBlankItems(t *testing.T) {
	var picked string
	m := sized(t, 80, 24).push(testPicker(&picked))
	if v := ansi.Strip(m.View()); strings.Contains(v, "•   •") {
		t.Errorf("picker hints have a blank item:\n%s", v)
	}
	s, _ := settingsModel(t)
	for name, v := range map[string]string{"bar": s.View(), "ref": ansi.Strip(s.refView())} {
		if strings.Contains(ansi.Strip(v), "•   •") {
			t.Errorf("Settings %s has a blank item:\n%s", name, v)
		}
	}
}

// The tab bar's loop segment is one state word per state, watched or not; stale
// follows any on or limited loop with a failed rev; o stops a limited loop.
func TestLoopSegmentWord(t *testing.T) {
	rev, failed := "abc1234def5678abc1234def5678abc1234def56", "def5678abc1234def5678abc1234def5678abc12"
	for _, c := range []struct {
		name     string
		st       looplock.Status
		starting bool
		want     string
	}{
		{"off", looplock.Status{State: looplock.Off}, false, "loop off"},
		{"starting", looplock.Status{State: looplock.Off}, true, "loop starting…"},
		{"on", looplock.Status{State: looplock.On, Rev: rev, Inflight: []string{"a"}}, false, "loop on"},
		{"on stale unwatched", looplock.Status{State: looplock.On, FailedRev: failed}, false, "loop on stale"},
		{"on stale watched", looplock.Status{State: looplock.On, Watch: true, FailedRev: failed}, false, "loop on stale"},
		{"limited", looplock.Status{State: looplock.Limited, ResetsAt: time.Now()}, false, "loop limited"},
		{"limited stale", looplock.Status{State: looplock.Limited, FailedRev: failed}, false, "loop limited stale"},
		{"restarting unwatched", looplock.Status{State: looplock.Draining, RestartTo: failed, Rev: rev}, false, "loop restarting"},
		{"restarting watched", looplock.Status{State: looplock.Draining, Watch: true, RestartTo: failed}, false, "loop restarting"},
		{"stop drain", looplock.Status{State: looplock.Draining, Inflight: []string{"a", "b"}}, false, "loop draining"},
	} {
		for _, w := range []int{120, 30} {
			m := sized(t, w, 30)
			m.loop, m.loopStarting = c.st, c.starting
			if got := ansi.Strip(m.loopSegment()); got != c.want {
				t.Errorf("%s: segment = %q, want %q", c.name, got, c.want)
			}
			if got := ansi.Strip(m.headerRight(w)); !strings.Contains(got, c.want) {
				t.Errorf("%s at %d: header %q lacks %q", c.name, w, got, c.want)
			}
		}
	}
	if !stoppable(looplock.Status{State: looplock.Limited}) {
		t.Error("a limited loop must be stoppable from o")
	}
}

// The tick hands each alive session its bead's plain SBRN (no dot, no ANSI) as
// status-left; a discussion keeps its Robin form; a dead session gets nothing.
func TestBarStatesLeft(t *testing.T) {
	beads := []bd.Bead{
		{ID: "a", Kind: contract.KindTask, Title: "Fix it", Status: "in_progress", Priority: 1},
		{ID: "d", Kind: contract.KindDiscussion, Title: "Why?", Status: "open"},
		{ID: "x", Kind: contract.KindTask, Title: "Dead", Status: "open"},
	}
	sess := map[string]session.Info{"a": {State: session.Alive}, "d": {State: session.Alive}}
	_, left := barStates(beads, sess)
	want := map[string]string{"a": "◐ TASK P1 · Fix it · a", "d": "Why? · Robin discussion"}
	if !maps.Equal(left, want) {
		t.Errorf("left = %q, want %q", left, want)
	}
}

// withIDs is the beads of from whose id is in want, as bd list --id returns them.
func withIDs(from []bd.Bead, want []string) []bd.Bead {
	var out []bd.Bead
	for _, b := range from {
		if slices.Contains(want, b.ID) {
			out = append(out, b)
		}
	}
	return out
}

// An old log whose newest graph.push* line is graph.push_failed shows no warning and drops out of the digest.
func TestHeaderIgnoresOldGraphPushFailed(t *testing.T) {
	ev := eventlog.Event{Msg: "graph.push_failed", Time: time.Unix(1, 0), Raw: map[string]any{}}
	if inDigest(ev) {
		t.Error("graph.push_failed must not be in the digest")
	}
	m := sized(t, 160, 30)
	m.digest = []eventlog.Event{ev}
	if strings.Contains(header(m), "graph push failed") {
		t.Errorf("header %q warns of an old graph.push_failed", header(m))
	}
}
