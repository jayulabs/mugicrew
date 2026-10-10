package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// TestRejectSignal drives z (Spec FR1–8): offered only on an open,
// unclaimed Signal, reason input then a y/n confirm, blank reason → default, a
// stale confirm refused, from Inbox, Graph and Search.
func TestRejectSignal(t *testing.T) {
	orig, origAll := closeBead, listAll
	defer func() { closeBead, listAll = orig, origAll }()
	listAll = func() ([]bd.Bead, error) { return nil, nil }
	var closed []string
	closeBead = func(id, reason string) error { closed = append(closed, id+": "+reason); return nil }
	key := func(s string) tea.KeyMsg {
		switch s {
		case "esc":
			return tea.KeyMsg{Type: tea.KeyEsc}
		case "enter":
			return tea.KeyMsg{Type: tea.KeyEnter}
		}
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	newModel := func(tb tab, sel string) Model {
		m := New(nil, nil)
		m.w, m.h = 160, 30
		m.beads = []bd.Bead{
			{ID: "sig", Kind: contract.KindSignal, Status: "open", Title: "Junk Signal"},
			{ID: "held", Kind: contract.KindSignal, Status: "open", Assignee: "nami", Title: "Claimed Signal"},
			{ID: "wip", Kind: contract.KindSignal, Status: "in_progress", Title: "Working Signal"},
			{ID: "done", Kind: contract.KindSignal, Status: "closed", Title: "Closed Signal"},
			{ID: "grl", Kind: contract.KindGrilling, Status: "open", Title: "A grilling"},
		}
		m.rebuildGroups()
		m.tab, m.selGraph, m.selInbox, m.selSearch = tb, sel, sel, sel
		return m
	}
	press := func(m Model, ks ...string) Model {
		for _, k := range ks {
			next, _ := m.Update(key(k))
			m = next.(Model)
		}
		return m
	}

	for _, id := range []string{"held", "wip", "done", "grl"} {
		m := newModel(tabGraph, id)
		if strings.Contains(m.footer(), "z reject") {
			t.Errorf("%s: footer offers z: %q", id, m.footer())
		}
		if m = press(m, "z"); m.editMode != editNone {
			t.Errorf("%s: z opened mode %v", id, m.editMode)
		}
	}

	for _, tb := range []tab{tabInbox, tabGraph, tabSearch} {
		for _, c := range []struct{ typed, want string }{{"", "rejected from TUI"}, {"dup of x", "dup of x"}} {
			closed = nil
			m := newModel(tb, "sig")
			if !strings.Contains(m.footer(), "z reject") {
				t.Errorf("tab %d: footer lacks z: %q", tb, m.footer())
			}
			m = press(m, "z")
			if m.editMode != editRejectReason {
				t.Fatalf("tab %d: z mode = %v", tb, m.editMode)
			}
			m.selGraph, m.selInbox, m.selSearch = "grl", "grl", "grl" // a drift must not retarget
			m.editBuf = lineAt(c.typed)
			m = press(m, "enter")
			if lines := strings.Split(m.helpLine(), "\n"); m.editMode != editConfirmReject || lines[0] != "y reject · n/esc cancel" {
				t.Fatalf("tab %d: confirm mode=%v footer=%q", tb, m.editMode, lines[0])
			}
			m, cmd := settleWrite(m.Update(key("y")))
			if len(closed) != 1 || closed[0] != "sig: "+c.want || cmd == nil || m.editMode != editNone {
				t.Errorf("tab %d: closed=%v reload=%v mode=%v", tb, closed, cmd != nil, m.editMode)
			}
		}
	}

	closed = nil
	for _, cancel := range [][]string{{"z", "esc"}, {"z", "enter", "n"}, {"z", "enter", "esc"}} {
		if m := press(newModel(tabInbox, "sig"), cancel...); m.editMode != editNone || len(closed) != 0 {
			t.Errorf("%v: mode=%v closed=%v", cancel, m.editMode, closed)
		}
	}

	// A stale confirm: the Signal was claimed after the flow opened.
	m := press(newModel(tabInbox, "sig"), "z", "enter")
	m.beads[0].Assignee = "someone"
	if m = press(m, "y"); len(closed) != 0 || !strings.Contains(m.status, "not rejected") {
		t.Errorf("stale: closed=%v status=%q", closed, m.status)
	}
}
