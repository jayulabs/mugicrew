package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// beadCmp: open before closed, then priority ascending, then newest-created first
// (Spec FR2).
func TestBeadCmp(t *testing.T) {
	now := time.Now()
	older, newer := now.Add(-time.Hour), now
	for _, c := range []struct {
		name string
		a, b bd.Bead
		want int
	}{
		{"open before closed", bd.Bead{Status: "open", Priority: 4}, bd.Bead{Status: "closed", Priority: 0}, -1},
		{"in_progress counts as open", bd.Bead{Status: "in_progress"}, bd.Bead{Status: "closed"}, -1},
		{"P0 before P2", bd.Bead{Status: "open", Priority: 0, Created: older}, bd.Bead{Status: "open", Priority: 2, Created: newer}, -1},
		{"P3 after P1", bd.Bead{Status: "open", Priority: 3}, bd.Bead{Status: "open", Priority: 1}, 1},
		{"same priority: newest first", bd.Bead{Status: "open", Created: newer}, bd.Bead{Status: "open", Created: older}, -1},
		{"same priority: older after", bd.Bead{Status: "open", Created: older}, bd.Bead{Status: "open", Created: newer}, 1},
		{"closed sort by priority too", bd.Bead{Status: "closed", Priority: 1}, bd.Bead{Status: "closed", Priority: 2}, -1},
		{"equal", bd.Bead{Status: "open", Created: older}, bd.Bead{Status: "open", Created: older}, 0},
	} {
		if got := beadCmp(c.a, c.b); got != c.want {
			t.Errorf("%s: beadCmp = %d, want %d", c.name, got, c.want)
		}
	}
}

// buildGraph orders members within a group and Chart groups by their head with
// beadCmp; Unparented stays first whatever its members' order.
func TestBuildGraphSorted(t *testing.T) {
	now := time.Now()
	at := func(h int) time.Time { return now.Add(time.Duration(-h) * time.Hour) }
	groups := buildGraph([]bd.Bead{
		{ID: "chA", Kind: contract.KindChart, Container: true, Status: "open", Priority: 2, Created: at(1)},
		{ID: "chB", Kind: contract.KindChart, Container: true, Status: "open", Priority: 1, Created: at(5)},
		{ID: "chC", Kind: contract.KindChart, Container: true, Status: "closed", Priority: 0, Created: at(0)},
		{ID: "chD", Kind: contract.KindChart, Container: true, Status: "open", Priority: 2, Created: at(0)},
		{ID: "a1", Kind: contract.KindGrilling, Parent: "chA", Status: "closed", Priority: 0},
		{ID: "a2", Kind: contract.KindGrilling, Parent: "chA", Status: "open", Priority: 3, Created: at(2)},
		{ID: "a3", Kind: contract.KindGrilling, Parent: "chA", Status: "open", Priority: 3, Created: at(1)},
		{ID: "a4", Kind: contract.KindGrilling, Parent: "chA", Status: "open", Priority: 1},
		{ID: "s1", Kind: contract.KindSignal, Status: "open", Priority: 4},
		{ID: "s2", Kind: contract.KindSignal, Status: "open", Priority: 0},
	})
	var keys []string
	for _, g := range groups {
		keys = append(keys, g.key)
	}
	if want := []string{"", "chB", "chD", "chA", "chC"}; !slices.Equal(keys, want) {
		t.Errorf("group order = %q, want %q", keys, want)
	}
	ids := func(bs []bd.Bead) (out []string) {
		for _, b := range bs {
			out = append(out, b.ID)
		}
		return out
	}
	if got, want := ids(groups[0].members), []string{"s2", "s1"}; !slices.Equal(got, want) {
		t.Errorf("unparented members = %v, want %v", got, want)
	}
	if got, want := ids(groups[3].members), []string{"a4", "a3", "a2", "a1"}; !slices.Equal(got, want) {
		t.Errorf("chA members = %v, want %v", got, want)
	}
}

// +/= raise and - lower the selected bead's priority one step, one bd write per
// press through updateBead, on Graph and Inbox; P0/P4 and closed beads write
// nothing and say why (Spec FR1).
func TestPriorityKeys(t *testing.T) {
	var writes [][]string
	orig := updateBead
	defer func() { updateBead = orig }()
	updateBead = func(id string, args []string) error {
		writes = append(writes, append([]string{id}, args...))
		return nil
	}
	key := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
	newModel := func(tb tab, status string, pri int) Model {
		m := New(nil, nil)
		m.beads = []bd.Bead{
			{ID: "chart1", Kind: contract.KindChart, Container: true, Title: "Chart: X", Status: "open"},
			{ID: "grl1", Kind: contract.KindGrilling, Parent: "chart1", Status: status, Priority: pri, Title: "Grill me"},
		}
		m.rebuildGroups()
		m.tab, m.selGraph, m.selInbox = tb, "grl1", "grl1"
		return m
	}
	for _, c := range []struct {
		name, key, status string
		tab               tab
		pri               int
		write             []string // nil: no write
		hint              string
	}{
		{"graph + raises", "+", "open", tabGraph, 2, []string{"grl1", "--priority", "1"}, "grl1 → P1"},
		{"graph = raises", "=", "open", tabGraph, 2, []string{"grl1", "--priority", "1"}, "grl1 → P1"},
		{"graph - lowers", "-", "open", tabGraph, 2, []string{"grl1", "--priority", "3"}, "grl1 → P3"},
		{"inbox + raises", "+", "open", tabInbox, 1, []string{"grl1", "--priority", "0"}, "grl1 → P0"},
		{"inbox - lowers", "-", "in_progress", tabInbox, 3, []string{"grl1", "--priority", "4"}, "grl1 → P4"},
		{"P0 limit", "+", "open", tabGraph, 0, nil, "grl1 is already P0"},
		{"P4 limit", "-", "open", tabInbox, 4, nil, "grl1 is already P4"},
		{"closed refused", "+", "closed", tabGraph, 2, nil, "Grill me is closed — priority unchanged"},
	} {
		writes = nil
		next, cmd := newModel(c.tab, c.status, c.pri).Update(key(c.key))
		m := next.(Model)
		if c.write == nil {
			if len(writes) != 0 || cmd != nil {
				t.Errorf("%s: writes = %v (cmd %v), want none", c.name, writes, cmd != nil)
			}
		} else {
			if cmd == nil || m.status != "updating grl1…" {
				t.Fatalf("%s: press cmd %v status %q; want a Cmd and updating", c.name, cmd != nil, m.status)
			}
			next, cmd = m.Update(cmd())
			m = next.(Model)
			if len(writes) != 1 || !slices.Equal(writes[0], c.write) || cmd == nil {
				t.Errorf("%s: writes = %v (reload %v), want one %v and a reload", c.name, writes, cmd != nil, c.write)
			}
		}
		if m.status != c.hint {
			t.Errorf("%s: status = %q, want %q", c.name, m.status, c.hint)
		}
	}

	// Logs selects events, not beads: +/- does nothing there.
	writes = nil
	newModel(tabLogs, "open", 2).Update(key("+"))
	if len(writes) != 0 {
		t.Errorf("logs: writes = %v, want none", writes)
	}
}

// ? help and the footer list +/- on Graph and Inbox, not on Logs; a closed bead
// gets no footer hint.
func TestPriorityKeysHelpAndFooter(t *testing.T) {
	for _, tb := range []tab{tabGraph, tabInbox, tabLogs} {
		listed := slices.ContainsFunc(helpKeys(tb, "ctrl+b"), func(k footerKey) bool { return k.k == "+/=/-" })
		if listed != (tb != tabLogs) {
			t.Errorf("tab %d: help lists +/=/- = %v", tb, listed)
		}
	}
	m := New(nil, nil)
	m.w = 200
	m.beads = []bd.Bead{
		{ID: "chart1", Kind: contract.KindChart, Container: true, Title: "Chart: X", Status: "open"},
		{ID: "grl1", Kind: contract.KindGrilling, Parent: "chart1", Status: "open", Title: "T"},
		{ID: "done", Kind: contract.KindGrilling, Parent: "chart1", Status: "closed", Title: "D"},
	}
	m.rebuildGroups()
	for _, c := range []struct {
		tab  tab
		sel  string
		want bool
	}{{tabGraph, "grl1", true}, {tabInbox, "grl1", true}, {tabGraph, "done", false}} {
		m.tab, m.selGraph, m.selInbox = c.tab, c.sel, c.sel
		if got := strings.Contains(ansi.Strip(m.footer()), "+/- pri"); got != c.want {
			t.Errorf("tab %d sel %s: footer %q has +/- = %v, want %v", c.tab, c.sel, ansi.Strip(m.footer()), got, c.want)
		}
	}
	// Too narrow for the whole footer: +/- yields before any view key does.
	m.tab, m.selGraph, m.w = tabGraph, "grl1", 84
	if f := ansi.Strip(m.footer()); strings.Contains(f, "+/-") || !strings.Contains(f, "o loop") {
		t.Errorf("84 cols: footer = %q, want +/- dropped and o kept", f)
	}
}

// The P<n> echo lands only on a successful done message, on the bead found by id
// at that moment; a failed one shows the error and leaves the row (ADR 0098 §4).
func TestPriorityEchoOnSuccessOnly(t *testing.T) {
	orig := updateBead
	t.Cleanup(func() { updateBead = orig })
	key := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
	m := New(nil, nil)
	m.beads = []bd.Bead{{ID: "a", Kind: contract.KindSignal, Status: "open", Priority: 2, Title: "A"}}
	m.rebuildGroups()
	m.selGraph = "a"
	pri := func(m Model) int {
		return m.beads[slices.IndexFunc(m.beads, func(b bd.Bead) bool { return b.ID == "a" })].Priority
	}

	updateBead = func(string, []string) error { return errors.New("dolt locked") }
	next, cmd := m.Update(key("+"))
	if pri(next.(Model)) != 2 {
		t.Fatal("press must not echo before the write lands")
	}
	next, _ = next.Update(cmd())
	if got := next.(Model); got.status != "priority: dolt locked" || pri(got) != 2 {
		t.Errorf("failed write: status %q P%d; want the error and P2", got.status, pri(got))
	}

	updateBead = func(string, []string) error { return nil }
	_, cmd = m.Update(key("+"))
	msg := cmd()
	// A reload landed meanwhile: the echo finds the bead in the current beads.
	m.beads = []bd.Bead{{ID: "z", Status: "open"}, {ID: "a", Kind: contract.KindSignal, Status: "open", Priority: 2, Title: "A"}}
	next, _ = m.Update(msg)
	if got := next.(Model); got.status != "a → P1" || pri(got) != 1 {
		t.Errorf("echo: status %q P%d; want a → P1", got.status, pri(got))
	}
}

// A second +/- on a bead with a write in flight is refused, not queued; another
// bead proceeds (ADR 0098 §3, Spec FR20-22).
func TestPriorityGuardRefusesPendingBead(t *testing.T) {
	calls := stubSeams(t)
	key := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
	m := New(nil, nil)
	m.beads = []bd.Bead{
		{ID: "a", Kind: contract.KindSignal, Status: "open", Priority: 2},
		{ID: "b", Kind: contract.KindSignal, Status: "open", Priority: 2},
	}
	m.rebuildGroups()
	m.selGraph = "a"
	next, first := m.Update(key("+"))
	next, cmd := next.Update(key("+"))
	m = next.(Model)
	if cmd != nil || m.status != "a: write in flight, wait" {
		t.Errorf("second press: cmd %v status %q; want refused", cmd != nil, m.status)
	}
	m.selGraph = "b"
	next, cmd = m.Update(key("-"))
	if m = next.(Model); cmd == nil || m.pending["a"] == "" || m.pending["b"] == "" {
		t.Fatalf("other bead: cmd %v pending %v; want it to proceed", cmd != nil, m.pending)
	}
	next, _ = m.Update(first())
	if m = next.(Model); m.pending["a"] != "" || m.pending["b"] == "" || len(*calls) != 1 {
		t.Errorf("after a's done: pending %v calls %q", m.pending, *calls)
	}
}
