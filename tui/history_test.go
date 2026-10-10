package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/session"
)

// FR3–5: lands and reverts are rows in place; every maximal run of other commits,
// leading and trailing included, folds to one hidden line; a missing bead has no title.
func TestFoldHistory(t *testing.T) {
	beads := []bd.Bead{{ID: "cp-a", Title: "Alpha"}, {ID: "cp-b", Title: "Beta"}}
	land := func(sha, id string) git.MainlineCommit { return git.MainlineCommit{SHA: sha, Lands: id} }
	rev := func(sha, id string) git.MainlineCommit { return git.MainlineCommit{SHA: sha, Reverts: id} }
	other := func(sha string) git.MainlineCommit { return git.MainlineCommit{SHA: sha} }
	cp := func(sha, id, title string) histRow {
		return histRow{kind: histCheckpoint, sha: sha, id: id, title: title}
	}
	rolledBack := func(r histRow) histRow { r.badge = badgeRolledBack; return r }
	rv := func(sha, id, title string) histRow { return histRow{kind: histRevert, sha: sha, id: id, title: title} }
	hid := func(n int) histRow { return histRow{kind: histHidden, hidden: n} }
	for _, c := range []struct {
		name string
		in   []git.MainlineCommit
		want []histRow
	}{
		{"empty", nil, nil},
		{"only others", []git.MainlineCommit{other("1"), other("2")}, []histRow{hid(2)}},
		{"leading and trailing runs", []git.MainlineCommit{other("1"), other("2"), land("3", "cp-a"), other("4")},
			[]histRow{hid(2), cp("3", "cp-a", "Alpha"), hid(1)}},
		{"mixed, revert in place", []git.MainlineCommit{rev("1", "cp-a"), other("2"), land("3", "cp-b"), land("4", "cp-a"), other("5"), other("6"), other("7")},
			[]histRow{rv("1", "cp-a", "Alpha"), hid(1), cp("3", "cp-b", "Beta"), rolledBack(cp("4", "cp-a", "Alpha")), hid(3)}},
		{"missing bead", []git.MainlineCommit{land("1", "cp-gone"), rev("2", "cp-gone")},
			[]histRow{rolledBack(cp("1", "cp-gone", "")), rv("2", "cp-gone", "")}},
	} {
		if got := foldHistory(c.in, beads); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

// FR6: a Checkpoint row's one badge — a revert trailer beats fix-forward beats
// pending; a closed Rollback without the fix-forward: prefix counts for nothing;
// only a related edge to a Checkpoint is a Rollback's target; revert rows carry none.
func TestFoldHistoryBadges(t *testing.T) {
	cp := bd.Bead{ID: "cp", Kind: contract.KindCheckpoint, Title: "C"}
	other := bd.Bead{ID: "sig", Kind: contract.KindSignal}
	rb := func(id, status, reason string, related ...string) bd.Bead {
		return bd.Bead{ID: id, Kind: contract.KindRollback, Status: status, CloseReason: reason, Related: related}
	}
	pending := rb("rb1", "open", "", "cp")
	fixed := rb("rb2", "closed", "fix-forward: revert conflicted", "cp")
	reverted := rb("rb3", "closed", "reverted on main", "cp")
	landed := []git.MainlineCommit{{SHA: "1", Lands: "cp"}}
	revertedToo := []git.MainlineCommit{{SHA: "0", Reverts: "cp"}, {SHA: "1", Lands: "cp"}}
	for _, c := range []struct {
		name  string
		in    []git.MainlineCommit
		beads []bd.Bead
		want  string
	}{
		{"no rollback", landed, nil, ""},
		{"pending", landed, []bd.Bead{pending}, badgePending},
		{"blocked rollback is pending", landed, []bd.Bead{rb("rb1", "blocked", "", "cp")}, badgePending},
		{"draft rollback is pending", landed, []bd.Bead{rb("rb1", "deferred", "", "cp")}, badgePending},
		{"fixed forward", landed, []bd.Bead{fixed}, badgeFixedForward},
		{"fix-forward beats pending", landed, []bd.Bead{pending, fixed}, badgeFixedForward},
		{"closed without fix-forward: prefix", landed, []bd.Bead{reverted}, ""},
		{"revert trailer beats all", revertedToo, []bd.Bead{pending, fixed}, badgeRolledBack},
		{"revert trailer alone", revertedToo, nil, badgeRolledBack},
		{"related to a non-Checkpoint first", landed, []bd.Bead{other, rb("rb1", "open", "", "sig", "cp")}, badgePending},
		{"related only to a non-Checkpoint", landed, []bd.Bead{other, rb("rb1", "open", "", "sig")}, ""},
	} {
		rows := foldHistory(c.in, append([]bd.Bead{cp}, c.beads...))
		last := rows[len(rows)-1]
		if last.kind != histCheckpoint || last.badge != c.want {
			t.Errorf("%s: row %+v, want badge %q", c.name, last, c.want)
		}
		if rows[0].kind == histRevert && rows[0].badge != "" {
			t.Errorf("%s: revert row badged %q", c.name, rows[0].badge)
		}
	}
}

// FR6 in the model: an open Rollback related to cp-b in the snapshot badges its row.
func TestHistoryRollbackPendingBadge(t *testing.T) {
	m := historyModel(t, 80)
	m.beads = append(m.beads, bd.Bead{ID: "rb", Kind: contract.KindRollback, Status: "open", Title: "Roll back Beta", Related: []string{"cp-b"}})
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "0000005 · 2d · Beta (cp-b) rollback pending") {
		t.Errorf("cp-b row lacks rollback pending:\n%s", v)
	}
	if !strings.Contains(v, "0000007 · 0m · Alpha (cp-a) rolled back") {
		t.Errorf("reverted cp-a row lacks rolled back:\n%s", v)
	}
}

// historyModel is a Model on the History tab: two Checkpoints in the Graph (one
// closed, so h hides it), a revert of one, a missing bead's land, and hidden runs.
func historyModel(t *testing.T, w int) Model {
	t.Helper()
	m := New(nil, nil)
	m.w, m.h = w, 24
	now := time.Now()
	m.beads = []bd.Bead{
		{ID: "cp-a", Kind: contract.KindCheckpoint, Container: true, Status: "closed", Title: "Alpha"},
		{ID: "cp-b", Kind: contract.KindCheckpoint, Container: true, Status: "open", Title: "Beta"},
	}
	m.histCommits = []git.MainlineCommit{
		{SHA: "0000001aaaa", Time: now}, // leading hidden
		{SHA: "0000002aaaa", Time: now.Add(-time.Hour), Reverts: "cp-a"},
		{SHA: "0000003aaaa", Time: now}, // hidden
		{SHA: "0000004aaaa", Time: now}, // hidden
		{SHA: "0000005aaaa", Time: now.Add(-48 * time.Hour), Lands: "cp-b"},
		{SHA: "0000006aaaa", Time: now, Lands: "cp-gone"},
		{SHA: "0000007aaaa", Time: now, Lands: "cp-a"},
	}
	m.rebuildGroups()
	m.fixSelection()
	return press(t, m, "H")
}

// FR1–5: H lands on History with the first selectable row (past a leading hidden
// line) selected; rows read sha · age · title (id); j/k skip hidden lines and wrap.
func TestHistoryTab(t *testing.T) {
	m := historyModel(t, 80)
	if m.tab != tabHistory || m.selHist != "0000002aaaa" {
		t.Fatalf("H: tab=%v sel=%q, want History on the revert", m.tab, m.selHist)
	}
	var v []string
	for _, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		v = append(v, strings.TrimSpace(l))
	}
	for _, want := range []string{"main · newest 200 first-parent commits", "+1 hidden commit", "❯ ↺ 0000002 · 1h · revert of Alpha (cp-a)", "+2 hidden commits", "0000005 · 2d · Beta (cp-b)", "0000006 · 0m · cp-gone"} {
		if !slices.Contains(v, want) {
			t.Errorf("view lacks line %q:\n%s", want, strings.Join(v, "\n"))
		}
	}
	var got []string
	for range 5 {
		m = press(t, m, "j")
		got = append(got, m.selHist)
	}
	if want := []string{"0000005aaaa", "0000006aaaa", "0000007aaaa", "0000002aaaa", "0000005aaaa"}; !slices.Equal(got, want) {
		t.Errorf("j walk = %v, want %v (hidden lines skipped, wrapping)", got, want)
	}
	if m = press(t, m, "k"); m.selHist != "0000002aaaa" {
		t.Errorf("k: sel=%q, want the revert", m.selHist)
	}
	for _, k := range []string{"l", "t", "p", "e", "E", "n", "x", "/", "f", "h"} {
		if next, cmd := m.updateKey(runeKey(k)); cmd != nil || next.(Model).editMode != editNone || next.(Model).status != "" {
			t.Errorf("%s acted on History", k)
		}
	}
}

// FR7: enter and G reveal the selected row's Checkpoint in the Graph — a revert
// row's target too, showing closed roots if h hid it.
func TestHistoryReveal(t *testing.T) {
	m := historyModel(t, 80)
	got, _ := send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if got.tab != tabGraph || got.selGraph != "cp-a" || got.hideClosedRoots {
		t.Errorf("enter on revert: tab=%v sel=%q hide=%v, want Graph on cp-a shown", got.tab, got.selGraph, got.hideClosedRoots)
	}
	m = press(t, m, "j")
	if got = press(t, m, "G"); got.tab != tabGraph || got.selGraph != "cp-b" {
		t.Errorf("G on cp-b: tab=%v sel=%q", got.tab, got.selGraph)
	}
	if got.selHist != "0000005aaaa" {
		t.Errorf("reveal moved History's selection to %q", got.selHist)
	}
}

// FR10: a reload keeps the selection by sha as newer commits land above it, and
// falls back to the top row once its commit leaves the window.
func TestHistoryKeepsSelectionBySha(t *testing.T) {
	m := press(t, historyModel(t, 80), "j") // cp-b's land
	commits := append([]git.MainlineCommit{{SHA: "000000faaaa", Lands: "cp-new"}}, m.histCommits...)
	next, _ := m.Update(loadedMsg{beads: m.beads, commits: commits})
	if m = next.(Model); m.selHist != "0000005aaaa" {
		t.Errorf("after a new land: sel=%q, want it kept by sha", m.selHist)
	}
	next, _ = m.Update(loadedMsg{beads: m.beads, commits: commits[:3]})
	if m = next.(Model); m.selHist != "000000faaaa" {
		t.Errorf("selection's commit gone: sel=%q, want the top row", m.selHist)
	}
}

// The refresh carries the mainline read; a failed read shows on History alone,
// leaving the bead load standing.
func TestLoadReadsMainline(t *testing.T) {
	stubLoopSeams(t)
	orig := readMainline
	t.Cleanup(func() { readMainline = orig })
	listAll = func() ([]bd.Bead, error) { return []bd.Bead{{ID: "cp-b", Kind: contract.KindCheckpoint}}, nil }
	readSessions = func() (map[string]session.Info, error) { return nil, nil }
	readLoop = func() (looplock.Status, error) { return looplock.Status{}, nil }
	readMainline = func() ([]git.MainlineCommit, error) {
		return []git.MainlineCommit{{SHA: "0000005aaaa", Lands: "cp-b"}}, nil
	}
	next, _ := New(nil, nil).Update(New(nil, nil).load()())
	if m := next.(Model); m.selHist != "0000005aaaa" || len(m.beads) != 1 {
		t.Errorf("loaded: sel=%q beads=%d, want the land selected beside the beads", m.selHist, len(m.beads))
	}
	readMainline = func() ([]git.MainlineCommit, error) { return nil, errors.New("no main") }
	next, _ = New(nil, nil).Update(New(nil, nil).load()())
	m := press(t, next.(Model), "H")
	if m.loadErr != nil || len(m.beads) != 1 || !strings.Contains(ansi.Strip(m.View()), "git log failed: no main") {
		t.Errorf("failed read: loadErr=%v beads=%d view:\n%s", m.loadErr, len(m.beads), ansi.Strip(m.View()))
	}
}

// At the 40-col floor every History line fits, and the footer offers reveal.
func TestHistoryFloor(t *testing.T) {
	m := historyModel(t, floorCols)
	for _, l := range strings.Split(m.View(), "\n") {
		if lipgloss.Width(l) > floorCols {
			t.Errorf("line %q is %d wide", ansi.Strip(l), lipgloss.Width(l))
		}
	}
	if f := ansi.Strip(m.footer()); !strings.Contains(f, "enter/G reveal") || strings.Contains(f, "tab") {
		t.Errorf("footer %q lacks enter/G reveal", f)
	}
}

// FR8–9: R refuses a revert row, a badged row and an empty selection with a
// reason; on an unbadged Checkpoint it opens the prefilled form, esc cancels, an
// empty title is refused, and enter files the stubbed Rollback of that
// Checkpoint, whose reload then badges the row pending.
func TestHistoryRollbackForm(t *testing.T) {
	orig := newRollback
	t.Cleanup(func() { newRollback = orig })
	var got []string
	newRollback = func(title, body, target string) (string, error) {
		got = append(got, title, body, target)
		return "rb-new", nil
	}
	m := historyModel(t, 80) // on the revert of cp-a
	for _, c := range []struct{ name, want string }{
		{"revert row", "a revert can't be rolled back"},
		{"pending", "cp-b: rollback pending"},
		{"rolled back", "cp-a: already rolled back"},
	} {
		if c.name == "pending" {
			m.beads = append(m.beads, bd.Bead{ID: "rb", Kind: contract.KindRollback, Status: "open", Related: []string{"cp-b"}})
			m = press(t, m, "j")
		}
		if c.name == "rolled back" {
			m = press(t, press(t, m, "j"), "j")
		}
		if r := press(t, m, "R"); r.editMode != editNone || r.status != c.want {
			t.Errorf("%s: R mode=%d status=%q, want refusal %q", c.name, r.editMode, r.status, c.want)
		}
	}
	fixed := historyModel(t, 80)
	fixed.beads = append(fixed.beads, bd.Bead{ID: "rb", Kind: contract.KindRollback, Status: "closed", CloseReason: bd.FixForwardReason + " x", Related: []string{"cp-b"}})
	if r := press(t, press(t, fixed, "j"), "R"); r.editMode != editNone || r.status != "cp-b: already fixed forward" {
		t.Errorf("fixed forward: R mode=%d status=%q", r.editMode, r.status)
	}
	empty := New(nil, nil)
	empty.tab = tabHistory
	if r := press(t, empty, "R"); r.editMode != editNone || r.status != "nothing to roll back" {
		t.Errorf("no selection: R mode=%d status=%q", r.editMode, r.status)
	}

	m = press(t, historyModel(t, 80), "j") // cp-b, unbadged
	if f := ansi.Strip(m.footer()); !strings.Contains(f, "R roll back") {
		t.Errorf("footer %q lacks R roll back", f)
	}
	m = press(t, m, "R")
	if m.editMode != editCreate || m.createTitle.text != "Roll back Beta" || !strings.Contains(ansi.Strip(m.View()), "Rollback of cp-b") {
		t.Fatalf("R: mode=%d title=%q, want the prefilled Rollback form", m.editMode, m.createTitle.text)
	}
	if c, _ := send(t, m, tea.KeyMsg{Type: tea.KeyEsc}); c.editMode != editNone || got != nil {
		t.Errorf("esc: mode=%d filed=%v, want cancelled", c.editMode, got)
	}
	blank := m
	blank.createTitle = lineAt("  ")
	if b, _ := send(t, blank, tea.KeyMsg{Type: tea.KeyEnter}); b.editMode != editCreate || got != nil {
		t.Errorf("empty title: mode=%d filed=%v, want refused", b.editMode, got)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m, _ = send(t, m, runeKey("x"))
	m, cmd := settleWrite(send(t, m, tea.KeyMsg{Type: tea.KeyEnter}))
	if want := []string{"Roll back Beta", "x", "cp-b"}; !slices.Equal(got, want) || m.editMode != editNone || cmd == nil {
		t.Fatalf("submit: filed %v mode=%d cmd=%v, want %v and a reload", got, m.editMode, cmd != nil, want)
	}
	beads := append(m.beads, bd.Bead{ID: "rb-new", Kind: contract.KindRollback, Status: "open", Related: []string{"cp-b"}})
	next, _ := m.Update(loadedMsg{beads: beads, commits: m.histCommits})
	if v := ansi.Strip(next.(Model).View()); !strings.Contains(v, "Beta (cp-b) rollback pending") {
		t.Errorf("after reload cp-b lacks rollback pending:\n%s", v)
	}
}
