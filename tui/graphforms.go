package tui

// The Graph tab's graph-shaping verbs (FR34–36, ADR 0144 §2): e E z a A, as
// modal forms with no y/n step. Each dialog pins its bead id when it opens;
// writes run off the event loop under the per-key guard (ADR 0098).

import (
	"cmp"
	"errors"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/rebase"
)

var (
	keyEdit    = key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit"))
	keyReject  = key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "reject"))
	keyAdopt   = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "adopt"))
	keyRelease = key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "release"))
	keyBlock   = key.NewBinding(key.WithKeys("B"), key.WithHelp("B", "add blocker"))
	keyUnblock = key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "unblock"))
)

// The write seams, vars so tests file nothing against a live bd.
var (
	newSignal        = bd.NewSignal
	newInitiative    = bd.NewInitiative
	initiativesByRef = bd.InitiativesByRef
	newSurgery       = bd.NewSurgery
	newSurgeryOn     = bd.NewSurgeryOn
	updateBead       = bd.Update
	closeBead        = bd.Close
	adoptChain       = bd.AdoptChain
	releaseChain     = bd.ReleaseChain
	addBlocker       = bd.AddBlocker
	removeBlocker    = bd.RemoveBlocker
	requestRebase    = func(id string) (rebase.Outcome, error) { return rebase.Request(rebase.Real, id) }
)

// rejectDefault is the close reason a blank z reason falls back to.
const rejectDefault = "rejected from TUI"

// rejectable is z's gate: an open, unclaimed Signal. A claimed or in-progress one is
// a live session's or another actor's to close.
func rejectable(b bd.Bead) bool {
	return b.Kind == contract.KindSignal && b.Status == "open" && b.Assignee == ""
}

// canAdopt is a's gate: an unclosed non-Initiative with an open Initiative to join.
func canAdopt(beads []bd.Bead, b *bd.Bead) bool {
	return b != nil && b.Unclosed() && b.Kind != contract.KindInitiative && len(adoptTargets(beads)) > 0
}

// canRelease is A's gate: an unclosed member of an Initiative.
func canRelease(beads []bd.Bead, b *bd.Bead) bool {
	return b != nil && b.Unclosed() && graphData{beads: beads}.beadByID(b.Parent).Kind == contract.KindInitiative
}

// canBlock is B's gate: an open, non-Draft bead.
func canBlock(b *bd.Bead) bool { return b != nil && b.Unclosed() && b.Status != "deferred" }

// canRebase is r's gate: an open Chart or Checkpoint (ADR 0262).
func canRebase(b *bd.Bead) bool {
	return b != nil && b.Open() && (b.Kind == contract.KindChart || b.Kind == contract.KindCheckpoint)
}

// rebaseFlash renders a Request outcome as the verb prints it (FR 12).
func rebaseFlash(out rebase.Outcome, err error) string {
	if err != nil {
		return "rebase: " + err.Error()
	}
	var s string
	switch out.Result {
	case rebase.Filed:
		s = "filed: " + out.Title + " (" + out.ID + ")"
	case rebase.AlreadyRequested:
		s = "already requested: " + out.Title + " (" + out.ID + ")"
	case rebase.OpenConflict:
		s = "already being rebased by open conflict: " + out.Title + " (" + out.ID + ") Base: " + out.Base
	case rebase.AlreadyOnMain:
		s = "already on mainline: " + out.Base
	default:
		s = "rebase: refused: " + out.Reason
	}
	if out.Warning != "" {
		s += " — warning: " + out.Warning
	}
	return s
}

// canUnblock is U's gate: a bead with at least one open blocker.
func canUnblock(beads []bd.Bead, b *bd.Bead) bool {
	return b != nil && len(graphData{beads: beads}.openBlockers(b)) > 0
}

// blockTargets are the beads B picks from: neither closed nor deferred, minus b and
// its current blockers, in beadCmp order.
func blockTargets(beads []bd.Bead, b *bd.Bead) []bd.Bead {
	var out []bd.Bead
	for _, t := range beads {
		if t.Unclosed() && t.Status != "deferred" && t.ID != b.ID && !slices.Contains(b.BlockedBy, t.ID) {
			out = append(out, t)
		}
	}
	slices.SortFunc(out, beadCmp)
	return out
}

// adoptTargets are the open Initiatives a picks from, in beadCmp order.
func adoptTargets(beads []bd.Bead) []bd.Bead {
	var out []bd.Bead
	for _, b := range beads {
		if b.Kind == contract.KindInitiative && b.Unclosed() {
			out = append(out, b)
		}
	}
	slices.SortFunc(out, beadCmp)
	return out
}

// formBindings are the Graph's form verbs that apply to b (order on the bar is
// set centrally, ADR 0229).
func formBindings(beads []bd.Bead, b *bd.Bead) []key.Binding {
	var bs []key.Binding
	if b != nil && rejectable(*b) {
		bs = append(bs, keyReject)
	}
	if b != nil {
		bs = append(bs, keyEdit)
	}
	if canCheckup(b) {
		bs = append(bs, keyCheckup)
	}
	if canAdopt(beads, b) {
		bs = append(bs, keyAdopt)
	}
	if canRelease(beads, b) {
		bs = append(bs, keyRelease)
	}
	if canBlock(b) {
		bs = append(bs, keyBlock)
	}
	if canUnblock(beads, b) {
		bs = append(bs, keyUnblock)
	}
	return bs
}

// formRefusal says why a, A, B, U or z does nothing on b,
// or "" for any other key.
func formRefusal(k tea.KeyMsg, b *bd.Bead) string {
	if b == nil {
		if key.Matches(k, keyAdopt) || key.Matches(k, keyRelease) || key.Matches(k, keyReject) || key.Matches(k, keyBlock) || key.Matches(k, keyUnblock) || key.Matches(k, keyCheckup) {
			return "nothing selected"
		}
		return ""
	}
	switch {
	case key.Matches(k, keyAdopt):
		switch {
		case b.Kind == contract.KindInitiative:
			return "Initiatives never nest"
		case b.Status == "closed":
			return b.ID + " is closed"
		}
		return "no open Initiative to adopt " + b.ID + " into"
	case key.Matches(k, keyRelease):
		return b.ID + " is not in an Initiative"
	case key.Matches(k, keyReject):
		return b.ID + " is not an open, unclaimed Signal"
	case key.Matches(k, keyBlock):
		if b.Status == "closed" {
			return "closed beads can't be blocked"
		}
		return "Drafts can't be blocked"
	case key.Matches(k, keyUnblock):
		return "no open blockers"
	}
	return ""
}

// graphKey opens the form for a Graph form key bound on the selection; ok is false
// for any other key. Gating through formBindings keeps handled == bound.
func (m Model) graphKey(k tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	b := m.selection()
	var open key.Binding
	for _, kb := range formBindings(m.beads, b) {
		if key.Matches(k, kb) {
			open = kb
		}
	}
	if len(open.Keys()) == 0 {
		if why := formRefusal(k, b); why != "" {
			m.setStatus(why)
			return m, nil, true
		}
		return m, nil, false
	}
	switch open.Keys()[0] {
	case "A":
		if why := bd.ReleaseRequiredWhy(*b); m.initiatives.Required && why != "" {
			m.setStatus(why)
			return m, nil, true
		}
		id, from := b.ID, b.Parent
		next, cmd := m.startAct(id, "releasing", func() writeDoneMsg {
			err := releaseChain(id)
			return writeDoneMsg{id, func(m Model) (tea.Model, tea.Cmd) {
				return m.doneOrErr("release", "released "+id+" from "+from, err)
			}}
		})
		return next, cmd, true
	}
	if m.refusePending(b.ID) {
		return m, nil, true
	}
	switch open.Keys()[0] {
	case "c":
		next, cmd := m.checkupKey(*b)
		return next, cmd, true
	case "z":
		d := inputDialog("Reject "+cmp.Or(b.Title, b.ID), []string{"reason (blank: " + rejectDefault + ")"}, b.ID, reject)
		return m.push(d), nil, true
	case "e":
		return m.push(editSignalForm(*b)), nil, true
	case "a":
		var ids []string
		cur := 0
		ts := adoptTargets(m.beads)
		for i, t := range ts {
			ids = append(ids, t.ID)
			if t.ID == b.Parent {
				cur = i
			}
		}
		items := m.pickerRows(ts)
		d := pickerDialog("Adopt "+cmp.Or(b.Title, b.ID)+" into", items, b.ID, adopt(ids))
		d.cur = cur
		return m.push(d), nil, true
	case "B":
		ts := blockTargets(m.beads, b)
		ids := make([]string, len(ts))
		for i, t := range ts {
			ids[i] = t.ID
		}
		d := pickerDialog("Block "+cmp.Or(b.Title, b.ID)+" on", m.pickerRows(ts), b.ID, addBlockerFrom(ids))
		return m.push(d), nil, true
	case "U":
		var ts []bd.Bead
		for _, t := range (graphData{beads: m.beads}).openBlockers(b) {
			ts = append(ts, *t)
		}
		slices.SortFunc(ts, beadCmp)
		ids := make([]string, len(ts))
		for i, t := range ts {
			ids[i] = t.ID
		}
		d := pickerDialog("Unblock "+cmp.Or(b.Title, b.ID)+" from", m.pickerRows(ts), b.ID, removeBlockerFrom(ids))
		return m.push(d), nil, true
	}
	return m, nil, false
}

// pickerRows draws bs as picker items: kind-tag column, id, title truncated to the
// picker width. Shared by the Adopt and blocker pickers.
func (m Model) pickerRows(bs []bd.Bead) []string {
	g, tagW := m.gateData(), 0
	for i := range bs {
		tagW = max(tagW, lipgloss.Width(kindTag(&bs[i])))
	}
	inner := m.pickerInner()
	items := make([]string, len(bs))
	for i, t := range bs {
		fixed := lipgloss.Width(g.sbrn(&t, "", false, tagW))
		items[i] = g.sbrn(&t, ansi.Truncate(t.Title, max(inner-fixed, 1), "…"), false, tagW)
	}
	return items
}

// addBlockerFrom makes the pinned bead wait on the bead the picker chose (bd refuses a cycle).
func addBlockerFrom(ids []string) func(Model, dialog) (Model, tea.Cmd) {
	return func(m Model, d dialog) (Model, tea.Cmd) {
		if len(ids) == 0 {
			return m, nil
		}
		picked := ids[d.cur]
		_, selOK := m.pinned(d.id)
		p, pickOK := m.pinned(picked)
		if !selOK || !pickOK || p.Status == "closed" {
			m.setStatus("add blocker cancelled: " + d.id + " or " + picked + " is gone")
			return m, nil
		}
		next, cmd := m.startAct(d.id, "blocking", func() writeDoneMsg {
			err := addBlocker(d.id, picked)
			return writeDoneMsg{d.id, func(m Model) (tea.Model, tea.Cmd) {
				return m.doneOrErr("add blocker", d.id+" now waits on "+picked, err)
			}}
		})
		return into(next, cmd)
	}
}

// removeBlockerFrom drops the blocks edge from the pinned bead to the blocker the picker chose.
func removeBlockerFrom(ids []string) func(Model, dialog) (Model, tea.Cmd) {
	return func(m Model, d dialog) (Model, tea.Cmd) {
		if len(ids) == 0 {
			return m, nil
		}
		picked := ids[d.cur]
		_, selOK := m.pinned(d.id)
		p, pickOK := m.pinned(picked)
		if !selOK || !pickOK || p.Status == "closed" {
			m.setStatus("unblock cancelled: " + d.id + " or " + picked + " is gone")
			return m, nil
		}
		next, cmd := m.startAct(d.id, "unblocking", func() writeDoneMsg {
			err := removeBlocker(d.id, picked)
			return writeDoneMsg{d.id, func(m Model) (tea.Model, tea.Cmd) {
				return m.doneOrErr("unblock", d.id+" no longer waits on "+picked, err)
			}}
		})
		return into(next, cmd)
	}
}

// openNewSignal opens the empty Signal form, refused while a create is in flight.
func (m Model) openNewSignal() (tea.Model, tea.Cmd, bool) {
	return m.openNew(newSignalForm())
}

// openNew opens the empty form d, refused while a create of its noun is in flight.
func (m Model) openNew(d dialog) (tea.Model, tea.Cmd, bool) {
	if m.refusePending(slot("new " + d.sf.noun)) {
		return m, nil, true
	}
	return m.push(d), nil, true
}

// retry reopens d after a failed write with the error shown, so the human's
// typing survives.
func (m Model) retry(d dialog, prefix string, err error) (tea.Model, tea.Cmd) {
	if len(m.modals) > 0 { // never stack the form on a dialog the human opened meanwhile
		m.setStatus(prefix + ": " + err.Error())
		return m, nil
	}
	switch d.kind {
	case form:
		d.sf.err = prefix + ": " + err.Error()
	case surgery:
		d.sg.err = prefix + ": " + err.Error()
	default:
		d.body = []string{warnSt.Render(prefix + ": " + err.Error())}
	}
	return m.push(d), nil
}

// enterResume rewrites the shared refusal's resume hint to the single enter token.
func enterResume(why string) string { return strings.Replace(why, "l to resume", "enter to resume", 1) }

// fileSurgery files the Surgery, on a fresh Checkpoint (in Initiative ini, if any) or (join != "") as a member of
// that one, then launches it (ADR 0107). A failure after a bead exists
// names the orphan; a launch failure leaves the Surgery idle.
func (m Model) fileSurgery(pend, title string, d dialog, join, ini string) (tea.Model, tea.Cmd) {
	return m.startWrite(pend, "filing", func() writeDoneMsg {
		var id string
		var err error
		cp, filed := join, ""
		if join == "" {
			id, cp, err = newSurgery(title, "", ini, m.initiatives.Required)
			filed = cp // a fresh Checkpoint is an orphan if the Surgery failed
		} else {
			id, err = newSurgeryOn(join, title, "")
		}
		return writeDoneMsg{pend, func(m Model) (tea.Model, tea.Cmd) {
			var jr bd.JoinRefusal
			switch {
			case errors.As(err, &jr):
				m.setStatus(enterResume(string(jr)))
				return m, m.load()
			case err != nil && id == "" && filed == "":
				return m.retry(d, "create", err)
			case err != nil:
				m.setStatus("create: " + err.Error() + " (filed " + strings.TrimSpace(id+" "+filed) + ")")
				return m, m.load()
			}
			return m.launchNow(bd.Bead{ID: id, Kind: contract.KindSurgery, Status: "in_progress", Title: title, Parent: cp}, false)
		}}
	})
}

// reject closes the pinned Signal with the typed reason, re-checking the gate on the
// current snapshot so a stale dialog can't close one someone just claimed.
func reject(m Model, d dialog) (Model, tea.Cmd) {
	b, ok := m.pinned(d.id)
	if !ok || !rejectable(b) {
		m.setStatus(d.id + " not rejected: no longer an open, unclaimed Signal")
		return m, m.load()
	}
	reason := cmp.Or(strings.TrimSpace(d.inputs[0].text), rejectDefault)
	next, cmd := m.startAct(b.ID, "rejecting", func() writeDoneMsg {
		err := closeBead(b.ID, reason)
		return writeDoneMsg{b.ID, func(m Model) (tea.Model, tea.Cmd) {
			return m.doneOrErr("reject", "rejected "+b.ID, err)
		}}
	})
	return into(next, cmd)
}

// adopt moves the pinned head into the Initiative the picker chose (bd re-checks).
func adopt(ids []string) func(Model, dialog) (Model, tea.Cmd) {
	return func(m Model, d dialog) (Model, tea.Cmd) { return adoptInto(m, d, ids[d.cur]) }
}

func adoptInto(m Model, d dialog, ini string) (Model, tea.Cmd) {
	_, headOK := m.pinned(d.id)
	target, iniOK := m.pinned(ini)
	if !headOK || !iniOK || target.Status == "closed" {
		m.setStatus("adopt cancelled: " + d.id + " or its target is gone")
		return m, nil
	}
	next, cmd := m.startAct(d.id, "adopting", func() writeDoneMsg {
		err := adoptChain(d.id, ini)
		return writeDoneMsg{d.id, func(m Model) (tea.Model, tea.Cmd) {
			return m.doneOrErr("adopt", "adopted "+d.id+" into "+ini, err)
		}}
	})
	return into(next, cmd)
}

// bdUpdate runs `bd update <id> args` under the guard; a failure reopens d.
func bdUpdate(m Model, d dialog, args []string, ok string) (Model, tea.Cmd) {
	next, cmd := m.startAct(d.id, "updating", func() writeDoneMsg {
		err := updateBead(d.id, args)
		return writeDoneMsg{d.id, func(m Model) (tea.Model, tea.Cmd) {
			if err != nil {
				return m.retry(d, "edit", err)
			}
			return m.doneOrErr("edit", ok, nil)
		}}
	})
	return into(next, cmd)
}

// rebaseRequest is the New menu's r row: refuse a pending bead, else run the rebase
// request with Request's outcome flashes (ADR 0262).
func (m Model) rebaseRequest(id string) (Model, tea.Cmd) {
	if m.refusePending(id) {
		return m, nil
	}
	next, cmd := m.startAct(id, "rebasing", func() writeDoneMsg {
		out, err := requestRebase(id)
		return writeDoneMsg{id, func(m Model) (tea.Model, tea.Cmd) {
			if err != nil || out.Result == rebase.Refused {
				m.setStatus(rebaseFlash(out, err))
				return m, nil
			}
			return m.doneOrErr("rebase", rebaseFlash(out, nil), nil)
		}}
	})
	return next.(Model), cmd
}
