package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/session"
)

var (
	loopOff      = looplock.Status{State: looplock.Off}
	loopOn       = looplock.Status{State: looplock.On, Pid: 7}
	loopDraining = looplock.Status{State: looplock.Draining, Pid: 7, Inflight: []string{"Build the lock", "Wire the TUI toggle"}}
	rev          = "abc1234def5678abc1234def5678abc1234def56"
	loopOnRev    = looplock.Status{State: looplock.On, Pid: 7, Rev: rev, StorePath: "/nix/store/x-mugicrew/bin/mugicrew"}
	loopStale    = looplock.Status{State: looplock.On, Pid: 7, Rev: rev, StorePath: loopOnRev.StorePath, Watch: true, FailedRev: "def5678abc1234def5678abc1234def5678abc12"}
	loopDrainRev = looplock.Status{State: looplock.Draining, Pid: 7, Inflight: loopDraining.Inflight, Rev: rev, StorePath: loopOnRev.StorePath}
	loopRestart  = looplock.Status{State: looplock.Draining, Pid: 7, Inflight: loopDraining.Inflight, Rev: rev, StorePath: loopOnRev.StorePath, Watch: true, RestartTo: loopStale.FailedRev}
	// ADR 0090 §7: an installed loop has no rev; stale and restarting are watched-only.
	loopInstalled   = looplock.Status{State: looplock.On, Pid: 7, StorePath: "/usr/local/bin/mugicrew"}
	loopPinnedStale = looplock.Status{State: looplock.On, Pid: 7, Rev: rev, StorePath: loopOnRev.StorePath, FailedRev: loopStale.FailedRev}
	loopPinnedDrain = looplock.Status{State: looplock.Draining, Pid: 7, Inflight: loopDraining.Inflight, Rev: rev, StorePath: loopOnRev.StorePath, RestartTo: loopStale.FailedRev}
	// Watched loops with a source to compare against its mainline tip.
	loopWatched      = looplock.Status{State: looplock.On, Pid: 7, Rev: rev, StorePath: loopOnRev.StorePath, Watch: true, Source: "/src/.git"}
	loopWatchedDrain = looplock.Status{State: looplock.Draining, Pid: 7, Inflight: loopDraining.Inflight, Rev: rev, StorePath: loopOnRev.StorePath, Watch: true, Source: "/src/.git"}
)

// The tab bar carries Inbox(N) and the loop segment in every state, and fits the
// 40-col floor even with wide counts; a pinned loop shows its short rev, and the
// floor drops it last. A watched loop at its source's mainline tip reads @latest.
func TestTabBarLoopSegment(t *testing.T) {
	hitl := bd.Bead{ID: "g1", Kind: contract.KindGrilling, Status: "open", Title: "q"}
	behind := "0123456789abcdef0123456789abcdef01234567"
	for _, c := range []struct {
		st          looplock.Status
		tip         string // the source mainline tip the refresh read; "" = unknown or failed
		want, roomy string
	}{
		{loopOff, "", "loop off", "loop off"},
		{loopOn, "", "loop on", "loop on"},
		{loopDraining, "", "loop draining 2", "loop draining 2"},
		{loopOnRev, "", "loop on", "loop on @abc1234"},
		{loopDrainRev, "", "loop draining 2", "loop draining 2 @abc1234"},
		{loopStale, "", "loop on stale", "loop on @abc1234 stale"},
		{loopRestart, "", "loop restarting 2", "loop restarting 2 @abc1234→def5678"},
		{loopInstalled, "", "loop on", "loop on"},
		{loopPinnedStale, "", "loop on", "loop on @abc1234"},
		{loopPinnedDrain, "", "loop draining 2", "loop draining 2 @abc1234"},
		{loopWatched, rev, "loop on", "loop on @latest"},
		{loopWatched, behind, "loop on", "loop on @abc1234"},
		{loopWatched, "", "loop on", "loop on @abc1234"},
		{loopWatchedDrain, rev, "loop draining 2", "loop draining 2 @latest"},
		{loopOnRev, rev, "loop on", "loop on @abc1234"},                               // unwatched: never latest (ADR 0090 §7)
		{loopInstalled, rev, "loop on", "loop on"},                                    // no rev, no suffix
		{loopStale, rev, "loop on stale", "loop on @abc1234 stale"},                   // stale keeps its hash
		{loopRestart, rev, "loop restarting 2", "loop restarting 2 @abc1234→def5678"}, // so does a restart drain
	} {
		m := New(nil, nil)
		m.beads, m.loop, m.loopTip = []bd.Bead{hitl}, c.st, c.tip
		m.w = 100 // roomy: the six-tab bar with the restart's rev fits untightened
		got := m.tabBar()
		if !strings.HasSuffix(got, "· 0 live · "+c.roomy) || !strings.Contains(got, "Inbox(1)") || strings.Contains(got, "open HITL") || strings.Contains(got, "/nix/store") {
			t.Errorf("%s: tab bar = %q, want Inbox(1) … · 0 live · %s", c.st.State, got, c.roomy)
		}
		if narrow := m.loopSegment(false); strings.Contains(narrow, "@") {
			t.Errorf("%s: narrow segment = %q, want no rev and no @latest", c.st.State, narrow)
		}
		// Worst case at the floor: two-digit Inbox and live counts while draining.
		beads := slices.Repeat([]bd.Bead{hitl}, 12)
		for i := range beads {
			beads[i].ID = string(rune('a' + i))
		}
		sessions := map[string]session.Info{}
		for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
			sessions[id] = session.Info{State: session.Alive}
		}
		m.beads, m.sessions, m.w = beads, sessions, floorCols
		got = m.tabBar()
		if lipgloss.Width(got) > floorCols || !strings.Contains(got, c.want) || !strings.Contains(got, "(12)") {
			t.Errorf("%s at %d cols: tab bar = %q (%d wide), want it to fit with %s", c.st.State, floorCols, got, lipgloss.Width(got), c.want)
		}
	}
}

// o's footer label tracks the loop state and sits just before ?, on every tab.
func TestFooterLoopKey(t *testing.T) {
	for _, c := range []struct {
		st   looplock.Status
		want string
	}{
		{loopOff, "o loop on · ? · q"},
		{loopOn, "o loop off · ? · q"},
		{loopDraining, "o force-stop · ? · q"},
		{loopRestart, "o loop off · ? · q"},    // a restart drain stops like on (ADR 0063 §5)
		{loopDrainRev, "o force-stop · ? · q"}, // …and once a stop clears restart_to, forces again
	} {
		for _, tb := range []tab{tabGraph, tabInbox, tabSearch, tabHistory, tabLogs} {
			m := New(nil, nil)
			m.loop, m.tab = c.st, tb
			// The Graph and Search put their filter keys between o and ? (they drop before o).
			if tb == tabSearch {
				m.w = 120 // room for all three pickers
			}
			got := strings.Replace(m.footer(), " · f status · c kind · s sort", "", 1)
			if got := strings.Replace(got, " · f kind · / search", "", 1); !strings.HasSuffix(got, c.want) {
				t.Errorf("%s tab %d: footer = %q, want suffix %q", c.st.State, tb, got, c.want)
			}
		}
	}
}

// stubLoopSeams restores every seam the loop tests swap.
func stubLoopSeams(t *testing.T) {
	t.Helper()
	l, s, r, tp, v := listAll, readSessions, readLoop, readTip, loopVerb
	t.Cleanup(func() { listAll, readSessions, readLoop, readTip, loopVerb = l, s, r, tp, v })
}

// The refresh reads the loop state through the seam, and a watched loop's source
// mainline tip alongside it.
func TestLoadReadsLoopState(t *testing.T) {
	stubLoopSeams(t)
	listAll = func() ([]bd.Bead, error) { return nil, nil }
	readSessions = func() (map[string]session.Info, error) { return nil, nil }
	readLoop = func() (looplock.Status, error) { return loopDraining, nil }
	var tipReads []string
	readTip = func(source string) (string, error) { tipReads = append(tipReads, source); return rev, nil }
	next, _ := New(nil, nil).Update(New(nil, nil).load()())
	if got := next.(Model); got.loop.State != looplock.Draining || got.loopTip != "" || len(tipReads) != 0 {
		t.Errorf("loaded loop = %+v tip %q reads %v, want draining, no tip read for an unwatched loop", got.loop, got.loopTip, tipReads)
	}
	readLoop = func() (looplock.Status, error) { return loopWatched, nil }
	next, _ = New(nil, nil).Update(New(nil, nil).load()())
	if m := next.(Model); m.loopTip != rev || !slices.Equal(tipReads, []string{"/src/.git"}) || !strings.HasSuffix(m.tabBar(), "loop on @latest") {
		t.Errorf("watched: tip %q reads %v bar %q, want the source's tip and @latest", m.loopTip, tipReads, m.tabBar())
	}
	readTip = func(string) (string, error) { return "", errors.New("no ref") }
	next, _ = New(nil, nil).Update(New(nil, nil).load()())
	if m := next.(Model); m.loopTip != "" || m.status != "" || m.loadErr != nil || !strings.HasSuffix(m.tabBar(), "loop on @abc1234") {
		t.Errorf("failed tip read: tip %q status %q err %v bar %q, want the hash, no error", m.loopTip, m.status, m.loadErr, m.tabBar())
	}
	readLoop = func() (looplock.Status, error) { return looplock.Status{}, errors.New("no repo") }
	if got := New(nil, nil).load()().(loadedMsg).loop; got.State != looplock.Off {
		t.Errorf("unreadable lock = %+v, want off", got)
	}
}

// fakeLoopVerb records each verb o runs and answers with err.
func fakeLoopVerb(t *testing.T, err error) *[][]string {
	t.Helper()
	stubLoopSeams(t)
	var calls [][]string
	loopVerb = func(args ...string) error { calls = append(calls, args); return err }
	listAll = func() ([]bd.Bead, error) { return nil, nil }
	readSessions = func() (map[string]session.Info, error) { return nil, nil }
	return &calls
}

// pressO presses o and runs the verb command it returns, if any.
func pressO(t *testing.T, m Model) (Model, tea.Msg) {
	t.Helper()
	next, cmd := m.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if cmd == nil {
		return next.(Model), nil
	}
	return next.(Model), cmd()
}

// o runs the verb for the state, shows pending until the next read confirms, and
// a CLI error verbatim.
func TestOTogglesLoop(t *testing.T) {
	for _, c := range []struct {
		st      looplock.Status
		pending string
		verb    []string
		after   looplock.Status
	}{
		{loopOff, "starting loop…", []string{"start"}, loopOn},
		{loopOn, "stopping loop…", []string{"stop"}, loopDraining},
	} {
		calls := fakeLoopVerb(t, nil)
		m := New(nil, nil)
		m.loop = c.st
		m, msg := pressO(t, m)
		if m.status != c.pending || len(*calls) != 1 || !slices.Equal((*calls)[0], c.verb) {
			t.Fatalf("%s: o → status %q calls %v, want %q %v", c.st.State, m.status, *calls, c.pending, c.verb)
		}
		// A second o while the verb runs is ignored.
		if again, _ := pressO(t, m); len(*calls) != 1 || again.status != c.pending {
			t.Errorf("%s: o while busy ran %v", c.st.State, *calls)
		}
		next, cmd := m.Update(msg)
		m = next.(Model)
		if cmd == nil || m.status != c.pending {
			t.Fatalf("%s: verb done must keep pending and re-read, status %q", c.st.State, m.status)
		}
		// A tick read begun before the verb returned still shows the old state.
		next, _ = m.Update(loadedMsg{loop: c.st})
		if m = next.(Model); m.status != c.pending {
			t.Errorf("%s: a stale read must keep pending, status %q", c.st.State, m.status)
		}
		readLoop = func() (looplock.Status, error) { return c.after, nil }
		next, _ = m.Update(cmd())
		if m = next.(Model); m.status != "" || m.loop.State != c.after.State {
			t.Errorf("%s: the read after o: status %q loop %q, want cleared, %q", c.st.State, m.status, m.loop.State, c.after.State)
		}
	}

	fakeLoopVerb(t, errors.New("mugicrew: loop is already on (pid 9)"))
	readLoop = func() (looplock.Status, error) { return loopOn, nil }
	m, msg := pressO(t, New(nil, nil))
	next, _ := m.Update(msg)
	if m = next.(Model); m.status != "mugicrew: loop is already on (pid 9)" {
		t.Errorf("CLI error must show verbatim, status %q", m.status)
	}
	next, _ = m.Update(m.load()())
	if m = next.(Model); m.status != "mugicrew: loop is already on (pid 9)" {
		t.Errorf("the read must not clear a CLI error, status %q", m.status)
	}
}

// A TUI opened with --watch forwards it on o: off starts `loop start --watch`
// (Spec FR18); a stop is unchanged.
func TestOForwardsWatch(t *testing.T) {
	calls := fakeLoopVerb(t, nil)
	m := New(nil, nil).WithWatch(true)
	pressO(t, m)
	m.loop = loopOn
	pressO(t, m)
	if want := [][]string{{"start", "--watch"}, {"stop"}}; !slices.EqualFunc(*calls, want, slices.Equal) {
		t.Fatalf("o with --watch ran %v, want %v", *calls, want)
	}
}

// A double-tap o on a running loop must not send a second stop (a force, FR38):
// o stays inert after the stop returns until a read confirms it left on.
func TestODoubleTapDoesNotForce(t *testing.T) {
	calls := fakeLoopVerb(t, nil)
	readLoop = func() (looplock.Status, error) { return loopOn, nil } // the loop hasn't written draining yet
	m := New(nil, nil)
	m.loop = loopOn
	m, msg := pressO(t, m)
	next, _ := m.Update(msg)
	m = next.(Model)
	next, _ = m.Update(loadedMsg{loop: loopOn}) // a stale tick read
	m = next.(Model)
	if m, msg = pressO(t, m); msg != nil || len(*calls) != 1 || m.editMode != editNone {
		t.Fatalf("o before the read confirmed: calls %v mode %v, want one stop and no-op", *calls, m.editMode)
	}
	next, _ = m.Update(loadedMsg{loop: loopDraining})
	if m, msg = pressO(t, next.(Model)); msg != nil || m.editMode != editConfirmForceStop || len(*calls) != 1 {
		t.Errorf("o after draining confirmed: calls %v mode %v, want the force confirm", *calls, m.editMode)
	}

	// A fast drain and a CLI restart before the next read: a new pid confirms too.
	m = New(nil, nil)
	m.loop = loopOn
	m, msg = pressO(t, m)
	next, _ = m.Update(msg)
	next, _ = next.(Model).Update(loadedMsg{loop: looplock.Status{State: looplock.On, Pid: 8}})
	if m = next.(Model); m.loopBusy || m.status != "" {
		t.Errorf("a restarted loop must confirm the stop: busy %v status %q", m.loopBusy, m.status)
	}
}

// o on a restart drain is a plain stop, busy until a read shows restart_to
// cleared; o then opens the force confirm (ADR 0063 §5).
func TestOStopsRestartDrain(t *testing.T) {
	calls := fakeLoopVerb(t, nil)
	readLoop = func() (looplock.Status, error) { return loopRestart, nil } // the stop hasn't landed yet
	m := New(nil, nil)
	m.loop, m.w = loopRestart, 200
	m, msg := pressO(t, m)
	if len(*calls) != 1 || !slices.Equal((*calls)[0], []string{"stop"}) || m.status != stoppingLoop {
		t.Fatalf("o on a restart drain: calls %v status %q; want one plain stop", *calls, m.status)
	}
	next, _ := m.Update(msg)
	next, _ = next.(Model).Update(loadedMsg{loop: loopRestart}) // a stale read
	if m, msg = pressO(t, next.(Model)); msg != nil || len(*calls) != 1 || m.editMode != editNone {
		t.Fatalf("o before the read confirmed: calls %v mode %v; want a no-op", *calls, m.editMode)
	}
	next, _ = m.Update(loadedMsg{loop: loopDrainRev})
	if m, msg = pressO(t, next.(Model)); msg != nil || m.editMode != editConfirmForceStop || len(*calls) != 1 {
		t.Errorf("o after the stop converted the drain: calls %v mode %v; want the force confirm", *calls, m.editMode)
	}
}

// A plain stop re-reads the lock: a loop a CLI stop already has draining (the
// tick hasn't shown it) opens the force confirm instead of sending a stop.
func TestOStopRereadsDraining(t *testing.T) {
	calls := fakeLoopVerb(t, nil)
	readLoop = func() (looplock.Status, error) { return loopDraining, nil }
	m := New(nil, nil)
	m.loop, m.w = loopOn, 200
	m, msg := pressO(t, m)
	next, _ := m.Update(msg)
	m = next.(Model)
	if len(*calls) != 0 || m.editMode != editConfirmForceStop || m.loopBusy || !strings.HasSuffix(m.status, "force stop?") {
		t.Errorf("stale on, lock draining: calls %v mode %v busy %v status %q, want the force confirm", *calls, m.editMode, m.loopBusy, m.status)
	}
}

// o while draining opens the force confirm; only y forces, n and esc cancel.
func TestOForceConfirm(t *testing.T) {
	for _, k := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("n")}, {Type: tea.KeyEsc}, {Type: tea.KeyRunes, Runes: []rune("o")}} {
		calls := fakeLoopVerb(t, nil)
		m := New(nil, nil)
		m.loop, m.w = loopDraining, 200
		m, msg := pressO(t, m)
		if msg != nil || m.editMode != editConfirmForceStop {
			t.Fatalf("o while draining must open the confirm, mode %v msg %v", m.editMode, msg)
		}
		want := "2 efforts in flight will be abandoned: Build the lock, Wire the TUI toggle — force stop?"
		if m.status != want || !strings.HasPrefix(m.helpLine(), renderKeys([]footerKey{{"y", "force"}, {"n/esc", "cancel"}})) {
			t.Errorf("confirm: status %q help %q", m.status, m.helpLine())
		}
		next, cmd := m.Update(k)
		if m = next.(Model); cmd != nil || m.editMode != editNone || len(*calls) != 0 {
			t.Errorf("%q must cancel without a verb: mode %v calls %v", k.String(), m.editMode, *calls)
		}
	}

	calls := fakeLoopVerb(t, nil)
	m := New(nil, nil)
	m.loop = loopDraining
	m, _ = pressO(t, m)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	cmd()
	if m = next.(Model); m.status != "stopping loop…" || len(*calls) != 1 || !slices.Equal((*calls)[0], []string{"stop", "--force"}) {
		t.Errorf("y: status %q calls %v, want loop stop --force", m.status, *calls)
	}
}

// The prompt folds titles that don't fit into +N.
func TestConfirmForcePromptFits(t *testing.T) {
	titles := []string{"a long effort title here", "another long effort title", "third"}
	if got := confirmForcePrompt(titles, 80); lipgloss.Width(got) > 80 || !strings.HasSuffix(got, " — force stop?") || !strings.Contains(got, "+") {
		t.Errorf("80 cols: %q (%d wide)", got, lipgloss.Width(got))
	}
	if got := confirmForcePrompt(titles, 200); !strings.Contains(got, "a long effort title here, another long effort title, third — ") {
		t.Errorf("wide: nothing is cut, got %q", got)
	}
}

// From the o that starts the loop, the tab bar reads loop starting… (warn) until
// a read shows the loop on, past the verb's return, even at the floor; a failed
// start drops it back to the real state with the error (Spec FR15–18).
func TestTabBarLoopStarting(t *testing.T) {
	calls := fakeLoopVerb(t, nil)
	readLoop = func() (looplock.Status, error) { return loopOff, nil } // the nix build is still running
	m := New(nil, nil)
	m.loop = loopOff
	m, msg := pressO(t, m)
	if got := m.tabBar(); len(*calls) != 1 || !strings.HasSuffix(got, "loop starting…") {
		t.Fatalf("o from off: tab bar = %q, want loop starting…", got)
	}
	next, cmd := m.Update(msg)
	next, _ = next.(Model).Update(cmd())
	m = next.(Model)
	if got := m.tabBar(); !strings.HasSuffix(got, "loop starting…") {
		t.Errorf("verb returned, read still off: tab bar = %q, want loop starting…", got)
	}
	m.w = floorCols
	if got := m.tabBar(); lipgloss.Width(got) > floorCols || !strings.Contains(got, "loop starting…") {
		t.Errorf("at %d cols: tab bar = %q, want loop starting…", floorCols, got)
	}
	next, _ = m.Update(loadedMsg{loop: loopOn})
	if got := next.(Model).tabBar(); !strings.HasSuffix(got, "loop on") {
		t.Errorf("a read showing on: tab bar = %q, want loop on", got)
	}

	fakeLoopVerb(t, errors.New("mugicrew: nix build failed"))
	m = New(nil, nil)
	m.loop = loopOff
	m, msg = pressO(t, m)
	next, _ = m.Update(msg)
	if m = next.(Model); !strings.HasSuffix(m.tabBar(), "loop off") || m.status != "mugicrew: nix build failed" {
		t.Errorf("a failed start: tab bar = %q status %q, want loop off and the error", m.tabBar(), m.status)
	}
}
