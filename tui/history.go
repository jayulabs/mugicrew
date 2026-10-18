package tui

// The History tab (FR45–48; ADR 0233 (the-history-tab-lists-every-mainline-commit…)): the mainline log — main's newest
// first-parent commits, one row each, newest first: a land (Checkpoint, Chart or Spec
// anchor) with a kind badge, a revert, or a dimmed plain commit; Checkpoint lands carry
// rollback badges. enter reveals a land's or revert's bead in Graph, u (undo) files a Rollback
// of a Checkpoint land through a form dialog (the tab never reverts).

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// historyCap is how many mainline commits the tab reads: no paging.
const historyCap = 200

// readMainline is the History tab's git read, a seam so tests run without a repo.
// ponytail: re-read every tick; cache on the mainline tip if a Build measures it.
var readMainline = func() ([]git.MainlineCommit, error) {
	return git.MainlineLogAcross(historyCap, cfglayer.Repos("."))
}

var (
	newRollback = bd.NewRollback

	keyHistReveal = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "reveal"))
	keyRollback   = key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "undo"))
)

// rollbackMsg is r on a rollbackable land: open the Rollback form for Checkpoint id.
type rollbackMsg struct{ id, title, sha string }

type histKind int

const (
	histLand   histKind = iota // a land: Checkpoint, Chart or Spec trailer
	histRevert                 // a revert: Reverts-Checkpoint: <id>
	histPlain                  // any other commit
)

type histRow struct {
	kind  histKind
	sha   string
	time  time.Time
	id    string // the bead landed or reverted
	title string // its bead title; "" when the bead is missing from the snapshot
	lkind string // histLand: the land trailer's kind
	subj  string // histPlain: the commit's subject
	ctx   string // the declared repo context it landed in; "" for the Workspace
	badge string // histLand of a Checkpoint: its Rollback state, "" for none
}

// The Rollback badges, in precedence order.
const (
	badgeRolledBack   = "rolled back"
	badgeFixedForward = "fixed forward"
	badgePending      = "rollback pending"
)

// historyRows turns mainline commits (newest first) into one row each: a land, else
// a revert, else a plain commit.
func historyRows(cs []git.MainlineCommit, beads []bd.Bead) []histRow {
	titles := map[string]string{}
	kinds := map[string]contract.Kind{}
	for _, b := range beads {
		titles[b.ID], kinds[b.ID] = b.Title, b.Kind
	}
	badges := rollbackBadges(cs, beads, kinds)
	var rows []histRow
	for _, c := range cs {
		r := histRow{sha: c.SHA, time: c.Time, id: c.Lands, ctx: c.Context}
		switch {
		case c.Kind != "":
			r.lkind = c.Kind
			if c.Kind == string(contract.KindCheckpoint) {
				r.badge = badges[c.Lands]
			}
		case c.Reverts != "":
			r.kind, r.id = histRevert, c.Reverts
		default:
			r.kind, r.subj = histPlain, c.Subject
		}
		r.title = titles[r.id]
		rows = append(rows, r)
	}
	return rows
}

// rollbackBadges is each Checkpoint's badge: rolled back when the window holds its
// revert, else fixed forward when a related Rollback closed "fix-forward:…", else
// rollback pending when a related Rollback is still open.
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

// histData is the slice of the shell's data History reads, handed over before every
// update and view.
type histData struct {
	commits []git.MainlineCommit
	err     error
	beads   []bd.Bead
	w, h    int // the body's size
}

// histTab's state: the selected commit, kept by sha.
type histTab struct {
	histData
	sel string
}

func newHistory() histTab { return histTab{} }

func (t histTab) count() int { return 0 }
func (t histTab) actN() int  { return 0 }

func (t histTab) rows() []histRow { return historyRows(t.commits, t.beads) }

// selIndex is the selected row's index, -1 when nothing is selectable.
func (t histTab) selIndex(rows []histRow) int {
	return slices.IndexFunc(rows, func(r histRow) bool { return r.sha == t.sel })
}

func (t histTab) selection() (histRow, bool) {
	rows := t.rows()
	if i := t.selIndex(rows); i >= 0 {
		return rows[i], true
	}
	return histRow{}, false
}

// selected is the selected commit's bead, nil when the snapshot lacks it.
func (t histTab) selected() *bd.Bead {
	if r, ok := t.selection(); ok {
		return beadByID(t.beads, r.id)
	}
	return nil
}

// fix keeps the selection by sha across a reload, else the top row.
func (t histTab) fix() histTab {
	rows := t.rows()
	if t.selIndex(rows) >= 0 {
		return t
	}
	t.sel = ""
	if len(rows) > 0 {
		t.sel = rows[0].sha
	}
	return t
}

// refusal is why r cannot roll r back, "" when it can.
func refusal(r histRow, ok bool) string {
	switch {
	case !ok:
		return "nothing to roll back"
	case r.kind == histPlain:
		return "not a land"
	case r.kind == histRevert:
		return "a revert can't be rolled back"
	case r.lkind == string(contract.KindChart):
		return "a Chart land can't be rolled back"
	case r.lkind == string(contract.KindSpec):
		return "a Spec anchor can't be rolled back"
	case r.badge == badgePending:
		return r.id + ": rollback pending"
	case r.badge != "":
		return r.id + ": already " + r.badge
	}
	return ""
}

func (t histTab) bindings() []key.Binding {
	r, ok := t.selection()
	if !ok {
		return nil
	}
	var bs []key.Binding
	if r.kind != histPlain {
		bs = append(bs, keyHistReveal)
	}
	if refusal(r, ok) == "" {
		bs = append(bs, keyRollback)
	}
	return bs
}

// extra is moving, and u when the selection can't roll back (it answers with a
// refusal): shown in ? alone.
func (t histTab) extra() []key.Binding {
	var out []key.Binding
	if len(t.rows()) >= 2 {
		out = append(out, keyMove)
	}
	if r, ok := t.selection(); refusal(r, ok) != "" {
		out = append(out, keyRollback)
	}
	return out
}

func (t histTab) update(k tea.KeyMsg) (component, tea.Cmd) {
	rows := t.rows()
	r, ok := t.selection()
	switch {
	case key.Matches(k, keyMove):
		d := 1
		if s := k.String(); s == "k" || s == "up" {
			d = -1
		}
		i := t.selIndex(rows)
		if len(rows) > 0 {
			t.sel = rows[clampWrap(i+d, len(rows))].sha
		}
	case key.Matches(k, keyHistReveal) && ok && r.kind != histPlain:
		return t, func() tea.Msg { return revealMsg{r.id} }
	case key.Matches(k, keyRollback):
		if why := refusal(r, ok); why != "" {
			return t, func() tea.Msg { return statusMsg(why) }
		}
		return t, func() tea.Msg { return rollbackMsg{r.id, r.title, r.sha} }
	}
	return t, nil
}

// histCtxWidth is the context column's width: 0 (no column) when no row is nested.
func histCtxWidth(rows []histRow) int {
	w := 0
	for _, r := range rows {
		w = max(w, len(r.ctx))
	}
	return w
}

// histLine renders one row: `sha · [context ·] age · title (id)`, a revert dimmed with ↺, a land ending in its kind badge, a plain commit dimmed.
func histLine(r histRow, ctxW int) string {
	head := r.sha[:min(7, len(r.sha))] + " · "
	if ctxW > 0 { // the context column, blank for Workspace rows
		head += fmt.Sprintf("%-*s · ", ctxW, r.ctx)
	}
	head += HumanAge(r.time) + " · "
	if r.kind == histPlain {
		return dimSt.Render(head + r.subj)
	}
	name := r.id
	if r.title != "" {
		name = r.title + " (" + r.id + ")"
	}
	if r.kind == histRevert {
		return dimSt.Render("↺ " + head + "revert of " + name)
	}
	if r.badge != "" {
		name += " " + warnSt.Render(r.badge)
	}
	name += " " + kindTag(&bd.Bead{Kind: contract.Kind(r.lkind), Container: r.lkind != string(contract.KindSpec)})
	return dimSt.Render(head) + name
}

// view is a header naming the cap, then the rows full width.
func (t histTab) view(w, h int) string {
	head := dimSt.Render(fmt.Sprintf("main · newest %d first-parent commits", historyCap))
	rows := t.rows()
	sel := t.selIndex(rows)
	var lines []string
	switch {
	case t.err != nil:
		lines = append(lines, warnSt.Render("git log failed: "+t.err.Error()))
	case len(rows) == 0:
		lines = append(lines, dimSt.Render("no commits on main"))
	}
	at := len(lines) + max(sel, 0) // past any error line
	ctxW := histCtxWidth(rows)
	for i, r := range rows {
		line := ansi.Truncate("  "+histLine(r, ctxW), w, "…")
		if i == sel {
			line = selectRow("❯ "+ansi.TruncateLeft(line, 2, ""), w)
		}
		lines = append(lines, line)
	}
	off := followOffset(len(lines), h-1, at)
	return " " + ansi.Truncate(head, w-1, "") + "\n" + strings.Join(lines[off:min(off+h-1, len(lines))], "\n")
}

// rollbackForm opens the Rollback form (title, evidence): the dialog is the
// rollback's guard, and submitting files it per ADR 0047. Submit rechecks the gate,
// as a refresh may have made a Rollback pending under the open form.
func (m Model) rollbackForm(msg rollbackMsg) Model {
	d := inputDialog("Rollback of "+msg.id, []string{"title", "evidence"}, msg.id, func(m Model, d dialog) (Model, tea.Cmd) {
		title := strings.TrimSpace(d.inputs[0].text)
		if title == "" {
			d.body = []string{warnSt.Render("create: title required")}
			return m.push(d), nil
		}
		h := m.tabAt(tabHistory).(histTab)
		for _, r := range h.rows() {
			if r.sha == msg.sha {
				if why := refusal(r, true); why != "" {
					m.setStatus(why)
					return m, nil
				}
			}
		}
		target := bd.Bead{ID: msg.id}
		if i := beadIndex(h.beads, msg.id); i >= 0 {
			target = h.beads[i]
		}
		next, cmd := m.startWrite(msg.id, "filing", func() writeDoneMsg {
			id, err := newRollback(title, d.inputs[1].text, target)
			return writeDoneMsg{msg.id, func(m Model) (tea.Model, tea.Cmd) {
				switch {
				case id == "" && err != nil:
					return m.retry(d, "create", err)
				case err != nil: // filed, but unrelated: no badge until the edge is added by hand
					m.setStatus("filed Rollback " + id + " without its related edge: " + err.Error())
					return m, m.load()
				}
				return m.doneOrErr("create", "filed Rollback "+id+" of "+msg.id, nil)
			}}
		})
		return into(next, cmd)
	})
	d.inputs[0] = lineAt("Roll back " + cmp.Or(msg.title, msg.id))
	return m.push(d)
}
