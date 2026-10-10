package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// abortGraph has a live AFK row with a session, one without, and a dead one.
func abortGraph(t *testing.T) Model {
	t.Helper()
	m := New(nil, nil)
	m.w, m.h = 120, 40
	m.hideClosedRoots = false
	next, _ := m.Update(loadedMsg{
		beads: []bd.Bead{
			{ID: "ep", Kind: contract.KindChart, Container: true, Title: "Epic", Status: "open"},
			{ID: "hs", Kind: contract.KindBuild, Parent: "ep", Status: "in_progress", Title: "With session"},
			{ID: "run", Kind: contract.KindBuild, Parent: "ep", Status: "in_progress", Title: "Running build"},
			{ID: "dead", Kind: contract.KindBuild, Parent: "ep", Status: "in_progress", Title: "Dead build"},
		},
		live:     map[string]bool{"run": true, "hs": true},
		sessions: map[string]session.Info{"hs": {State: session.Alive, Session: "@1"}},
	})
	return next.(Model)
}

func stubAbort(t *testing.T, pids []int, err error) *[]string {
	t.Helper()
	orig, origList := abortEffort, listAll
	t.Cleanup(func() { abortEffort, listAll = orig, origList })
	listAll = func() ([]bd.Bead, error) { return nil, nil }
	var calls []string
	abortEffort = func(id, by string) ([]int, error) { calls = append(calls, id+" "+by); return pids, err }
	return &calls
}

// x: a session drops; no session and a live AFK effort opens the abort confirm;
// not live does nothing. The footer offers x abort only on the live AFK row.
func TestXRoutesAbort(t *testing.T) {
	calls := stubAbort(t, nil, nil)
	for _, c := range []struct {
		id, footer string
		mode       editMode
	}{
		{"hs", "drop", editConfirmDrop},
		{"run", "abort", editConfirmAbort},
		{"dead", "", editNone},
	} {
		m := abortGraph(t)
		m.selGraph = c.id
		var x string
		ks := m.beadKeys(m.selectedBead())
		if i := slices.IndexFunc(ks, func(k footerKey) bool { return k.k == "x" }); i >= 0 {
			x = ks[i].label
		}
		if x != c.footer {
			t.Errorf("%s: footer x = %q, want %q", c.id, x, c.footer)
		}
		m, cmd := send(t, m, runeKey("x"))
		if m.editMode != c.mode || cmd != nil {
			t.Errorf("%s: x gave mode %v cmd %v, want %v", c.id, m.editMode, cmd != nil, c.mode)
		}
		if c.mode == editConfirmAbort && (m.editID != "run" || !strings.Contains(m.status, `abort "Running build"`)) {
			t.Errorf("abort confirm = %q pinned %q", m.status, m.editID)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("x alone aborted: %v", *calls)
	}
}

func confirmAbort(t *testing.T, key string) (Model, tea.Cmd) {
	t.Helper()
	m := abortGraph(t)
	m.selGraph = "run"
	m, _ = send(t, m, runeKey("x"))
	return send(t, m, runeKey(key))
}

// n cancels with nothing written; y aborts by tui and shows the outcome, then reloads.
func TestAbortConfirm(t *testing.T) {
	calls := stubAbort(t, nil, nil)
	if m, cmd := confirmAbort(t, "n"); cmd != nil || m.editMode != editNone || m.status != "abort cancelled" || len(*calls) != 0 {
		t.Fatalf("n: cmd %v status %q calls %v", cmd != nil, m.status, *calls)
	}
	for _, c := range []struct {
		pids []int
		err  error
		want string
	}{
		{[]int{41, 42}, nil, "aborted run: signalled 41 42"},
		{nil, nil, "aborted run: no live process; paused only"},
		{nil, errors.New("bd update: boom"), "abort: bd update: boom"}, // a pause failure: Abort killed nothing (dispatch.Abort test pins the kill)
	} {
		got := stubAbort(t, c.pids, c.err)
		m, cmd := confirmAbort(t, "y")
		if cmd == nil || m.editMode != editNone {
			t.Fatalf("y: no abort cmd")
		}
		next, reload := m.Update(cmd())
		if s := next.(Model).status; s != c.want || reload == nil {
			t.Errorf("status = %q (reload %v), want %q", s, reload != nil, c.want)
		}
		if want := []string{"run tui"}; !slices.Equal(*got, want) {
			t.Errorf("abort calls = %v, want %v", *got, want)
		}
	}
}

// y holds the bead under the per-bead guard (ADR 0107 §2): the row reads
// aborting… until the done lands; meanwhile a second x/y and a p are refused.
func TestAbortInFlight(t *testing.T) {
	calls := stubAbort(t, []int{41}, nil)
	m, cmd := confirmAbort(t, "y")
	b := m.beadByID("run")
	if suf := ansi.Strip(m.rowSuffix(b)); !strings.HasSuffix(suf, "  aborting…") || m.status != "aborting run…" {
		t.Fatalf("y: row suffix %q status %q; want aborting… and aborting run…", suf, m.status)
	}
	for _, k := range []string{"x", "p"} {
		next, again := send(t, m, runeKey(k))
		if k == "x" && next.editMode == editConfirmAbort {
			next, again = send(t, next, runeKey("y"))
		}
		if again != nil || next.status != "run: write in flight, wait" {
			t.Errorf("%s during abort: cmd %v status %q; want refused", k, again != nil, next.status)
		}
	}
	next, reload := m.Update(cmd())
	m = next.(Model)
	if suf := ansi.Strip(m.rowSuffix(b)); strings.Contains(suf, "aborting") || m.status != "aborted run: signalled 41" || reload == nil {
		t.Errorf("done: row suffix %q status %q reload %v", suf, m.status, reload != nil)
	}
	if len(*calls) != 1 {
		t.Errorf("abort calls = %v; want one", *calls)
	}
}
