package tui

// The History tab: main's newest first-parent commits as landed
// Checkpoints, newest first. enter/G reveal a row's Checkpoint; R files a
// Rollback of it, which Jinbe runs (the tab never reverts itself).

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// historyCap is how many mainline commits the tab reads (FR2): no paging.
const historyCap = 200

// readMainline is the History tab's git read, a var so tests run without a repo.
var readMainline = func() ([]git.MainlineCommit, error) {
	return git.MainlineLog(string(contract.KindCheckpoint), historyCap)
}

// histKind is a History row's kind (FR3–5).
type histKind int

const (
	histCheckpoint histKind = iota // a land: Checkpoint: <id>
	histRevert                     // a revert: Reverts-Checkpoint: <id>
	histHidden                     // a folded run of other commits
)

// histRow is one History line; callers read its fields, so a later field (the FR6 badge) needs no rework.
type histRow struct {
	kind   histKind
	sha    string
	time   time.Time
	id     string // the Checkpoint landed or reverted
	title  string // its bead title; "" when the bead is missing from the snapshot
	hidden int    // histHidden: how many commits it folds
	badge  string // histCheckpoint: its Rollback state (FR6), "" for none
}

// The FR6 Rollback badges, in precedence order.
const (
	badgeRolledBack   = "rolled back"
	badgeFixedForward = "fixed forward"
	badgePending      = "rollback pending"
)

// foldHistory turns mainline commits (newest first) into History rows: a land
// or revert is a row, each maximal run of other commits one hidden line.
func foldHistory(cs []git.MainlineCommit, beads []bd.Bead) []histRow {
	titles := map[string]string{}
	kinds := map[string]contract.Kind{}
	for _, b := range beads {
		titles[b.ID], kinds[b.ID] = b.Title, b.Kind
	}
	badges := rollbackBadges(cs, beads, kinds)
	var rows []histRow
	for _, c := range cs {
		r := histRow{sha: c.SHA, time: c.Time, id: c.Lands}
		switch {
		case c.Lands != "":
			r.badge = badges[c.Lands]
		case c.Reverts != "":
			r.kind, r.id = histRevert, c.Reverts
		default:
			if n := len(rows); n > 0 && rows[n-1].kind == histHidden {
				rows[n-1].hidden++
			} else {
				rows = append(rows, histRow{kind: histHidden, hidden: 1})
			}
			continue
		}
		r.title = titles[r.id]
		rows = append(rows, r)
	}
	return rows
}

// rollbackBadges is each Checkpoint's FR6 badge: rolled back when the window
// holds its revert, else fixed forward when a related Rollback closed
// "fix-forward:…", else rollback pending when a related Rollback is still open.
func rollbackBadges(cs []git.MainlineCommit, beads []bd.Bead, kinds map[string]contract.Kind) map[string]string {
	badges := map[string]string{}
	for _, b := range beads {
		target, ok := bd.RollbackTargetIn(b, kinds)
		if b.Kind != contract.KindRollback || !ok {
			continue
		}
		switch {
		case b.Status == "closed" && strings.HasPrefix(b.CloseReason, bd.FixForwardReason):
			badges[target] = badgeFixedForward
		case b.Status != "closed" && badges[target] == "": // any non-closed status, blocked included
			badges[target] = badgePending
		}
	}
	for _, c := range cs {
		if c.Reverts != "" {
			badges[c.Reverts] = badgeRolledBack
		}
	}
	return badges
}

// historyRows is the tab's rows over the last load.
func (m Model) historyRows() []histRow { return foldHistory(m.histCommits, m.beads) }

// selectedHist is the selected row's index, -1 when nothing is selectable.
func (m Model) selectedHist(rows []histRow) int {
	return slices.IndexFunc(rows, func(r histRow) bool { return r.kind != histHidden && r.sha == m.selHist })
}

// histSelection is the selected row, false when nothing is selected.
func (m Model) histSelection() (histRow, bool) {
	rows := m.historyRows()
	if i := m.selectedHist(rows); i >= 0 {
		return rows[i], true
	}
	return histRow{}, false
}

// moveHist steps the selection d rows, skipping hidden lines (FR5), wrapping.
func (m *Model) moveHist(d int) {
	rows := m.historyRows()
	i := m.selectedHist(rows)
	for range rows {
		i = clampWrap(i+d, len(rows))
		if rows[i].kind != histHidden {
			m.selHist = rows[i].sha
			return
		}
	}
}

// fixHist keeps the selection by sha across a reload (FR10), else the top row.
func (m *Model) fixHist() {
	rows := m.historyRows()
	if m.selectedHist(rows) >= 0 {
		return
	}
	m.selHist = ""
	if i := slices.IndexFunc(rows, func(r histRow) bool { return r.kind != histHidden }); i >= 0 {
		m.selHist = rows[i].sha
	}
}

// histLine renders one row: `sha · age · title (id)`, a revert dimmed with ↺.
func histLine(r histRow) string {
	if r.kind == histHidden {
		s := "s"
		if r.hidden == 1 {
			s = ""
		}
		return dimmer.Render(fmt.Sprintf("+%d hidden commit%s", r.hidden, s))
	}
	name := r.id
	if r.title != "" {
		name = r.title + " (" + r.id + ")"
	}
	head := r.sha[:min(7, len(r.sha))] + " · " + HumanAge(r.time) + " · "
	if r.kind == histRevert {
		return dim.Render("↺ " + head + "revert of " + name)
	}
	if r.badge != "" {
		name += " " + warn.Render(r.badge)
	}
	return dim.Render(head) + name
}

// historyView is the History body: a header naming the cap, then the rows full width.
func (m Model) historyView(height int) string {
	head := dim.Render(fmt.Sprintf("main · newest %d first-parent commits", historyCap))
	rows := m.historyRows()
	sel := m.selectedHist(rows)
	var lines []string
	switch {
	case m.histErr != nil:
		lines = append(lines, warn.Render("git log failed: "+m.histErr.Error()))
	case len(rows) == 0:
		lines = append(lines, dim.Render("no commits on main"))
	}
	selLine := len(lines) + max(sel, 0) // past any error line
	for i, r := range rows {
		marker := "  "
		if i == sel {
			marker = selMarker.Render("❯ ")
		}
		lines = append(lines, trunc(marker+histLine(r), m.w))
	}
	out := windowRows(strings.Join(lines, "\n"), m.w, height-1, followOffset(len(lines), height-1, selLine))
	return trunc(head, m.w) + "\n" + strings.Join(out, "\n")
}

// historyKeys are the keys History handles: the global ones plus j/k, reveal and R.
var historyKeys = []string{"q", "ctrl+c", "left", "shift+tab", "right", "G", "I", "S", "H", "L", "#", "*", "j", "down", "k", "up", "enter", "?", "o", "f5", "R"}

// openRollback is R (FR8–9): on an unbadged Checkpoint row it opens the create
// form aimed at that Checkpoint; anywhere else it only says why not.
func (m *Model) openRollback() {
	r, ok := m.histSelection()
	switch {
	case !ok:
		m.status = "nothing to roll back"
	case r.kind == histRevert:
		m.status = "a revert can't be rolled back"
	case r.badge == badgePending:
		m.status = r.id + ": rollback pending"
	case r.badge != "":
		m.status = r.id + ": already " + r.badge
	default:
		m.editMode, m.createFor, m.createField, m.createSurgery = editCreate, r.id, 0, false
		m.createTitle, m.createDesc, m.status = lineAt("Roll back "+cmp.Or(r.title, r.id)), line{}, ""
	}
}
