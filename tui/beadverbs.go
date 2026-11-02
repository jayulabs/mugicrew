package tui

// The bead verbs (Spec mc-w2a8c): one rule, a verb is offered wherever its bead's row
// appears, gated by the bead's state and never by the tab. This file holds the verb
// list, the row-to-bead resolver, the router the shell calls once, and the shared hint
// list every bead-row surface splices in.

import (
	"slices"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/git"
)

// beadVerbs are the twelve bead verbs: z e a A c B U +/- p x X w. The router's key match,
// the shared hint list and the parity test all read it. keyPrio binds four keys, so a
// consumer iterates Keys(). t, y, r, enter, b, d, space and i are not bead verbs.
var beadVerbs = []key.Binding{keyReject, keyEdit, keyAdopt, keyRelease, keyCheckup, keyBlock, keyUnblock, keyPrio, keyPauseKey, keyDropKey, keyWithdrawKey, keyOpenMR}

// beadVerbKey is whether k is one of beadVerbs' keys.
func beadVerbKey(k tea.KeyMsg) bool {
	for _, v := range beadVerbs {
		if key.Matches(k, v) {
			return true
		}
	}
	return false
}

// rowBead is the bead the cursor row stands for: the selection, else the Initiative a
// header row names; nil on a row with no bead behind it.
func (m Model) rowBead() *bd.Bead {
	if b := m.selection(); b != nil {
		return b
	}
	return m.initRow()
}

// routesBeadVerbs is whether the shell's bead-verb router claims keys on the active
// surface: the focus view, or a tab up to Initiatives (Ideate, Defog, Execute, Graph,
// Initiatives). History, Stats and Logs keep their own keys.
func (m Model) routesBeadVerbs() bool {
	return m.focus.id != "" || m.tab <= tabInits
}

// beadVerbRoute is the one router for bead-verb keys: every beadVerbs key on an admitted
// surface is handled, so none fails silently. No row bead answers "nothing selected"; a
// write in flight answers before any verb's own gate.
func (m Model) beadVerbRoute(k tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if !m.routesBeadVerbs() || !beadVerbKey(k) {
		return m, nil, false
	}
	b := m.rowBead()
	if b == nil {
		m.setStatus("nothing selected")
		if gt, isGraph := m.tabAt(m.tab).(graphTab); isGraph && gt.sel == unparentedSel {
			m.setStatus("Unparented is a group header — select a Signal and press enter to launch Luffy to triage it, or a Rollback and press enter to run it")
		}
		return m, nil, true
	}
	if m.refusePending(b.ID) {
		return m, nil, true
	}
	g := m.gateData()
	switch {
	case key.Matches(k, keyOpenMR):
		next, cmd := m.openMRBead(b)
		return next, cmd, true
	case key.Matches(k, keyPauseKey):
		next, cmd := m.togglePause(g, *b)
		return next, cmd, true
	case key.Matches(k, keyDropKey):
		next, cmd := m.dropKey(g, *b)
		return next, cmd, true
	case key.Matches(k, keyWithdrawKey):
		next, cmd := m.xPress(g, *b)
		return next, cmd, true
	case key.Matches(k, keyPrio):
		next, cmd := m.bumpPriority(*b, prioDir(k))
		return next, cmd, true
	}
	return m.graphKey(k)
}

// rowRole is how much of the state-gated verb set a row hints (ADR 0360): the rest is
// bound, so ? lists it and the key still answers.
type rowRole int

const (
	roleFull   rowRole = iota // every verb the bead's state offers
	roleMR                    // an MR row: w, plus x while a session runs
	roleClosed                // a closed row: c, plus x while a session runs
)

// beadVerbBindings are the bead verbs on b's row as hinted and extra (the hinted/extra
// split): hinted by role, in bar order, and extra — every other verb, bound so ? lists
// it under "this tab". A nil b hints nothing and binds them all, for "nothing selected".
func beadVerbBindings(g graphData, b *bd.Bead, role rowRole) (hinted, extra []key.Binding) {
	shown := map[string]bool{}
	if b != nil && g.draftWhy(b) != "" { // a Draft row hints no verb: all are refused, so ? files them under "this tab"
		b = nil
	}
	if b != nil {
		offered := map[string]key.Binding{}
		add := func(kb key.Binding) { offered[kb.Keys()[0]] = kb }
		if b.Unclosed() {
			add(verbKey("p", pauseLabel(b)))
		}
		if l := g.xHint(b); l != "" {
			add(verbKey("x", l))
		}
		if t := g.xTargetOf(b); t != xNone {
			add(xBinding(t))
		}
		for _, kb := range formBindings(g.beads, b) {
			add(kb)
		}
		if b.Unclosed() {
			add(keyPrio)
		}
		if git.RecordedMRLink(b.Description) != "" {
			add(keyOpenMR)
		}
		live := g.sessions[b.ID].Has()
		for _, v := range []key.Binding{keyPauseKey, keyDropKey, keyWithdrawKey, keyReject, keyEdit, keyPrio, keyCheckup, keyAdopt, keyRelease, keyBlock, keyUnblock, keyOpenMR} {
			kb, ok := offered[v.Keys()[0]]
			if !ok {
				continue
			}
			k := kb.Keys()[0]
			if role == roleMR && k != "w" && !(k == "x" && live) || role == roleClosed && k != "c" && !(k == "x" && live) {
				continue
			}
			hinted = append(hinted, kb)
			shown[k] = true
		}
	}
	for _, v := range beadVerbs {
		if !shown[v.Keys()[0]] {
			extra = append(extra, v)
		}
	}
	return hinted, extra
}

// spliceVerbs appends the hinted bead verbs to bs, the transcript key after the leading p, x
// and X. hintOf (ADR 0229) picks each verb's section; this order is the in-section order on
// the bar and in ?.
func spliceVerbs(bs, hinted []key.Binding) []key.Binding {
	at := slices.IndexFunc(hinted, func(kb key.Binding) bool { return !slices.Contains([]string{"p", "x", "X"}, kb.Keys()[0]) })
	if at < 0 {
		at = len(hinted)
	}
	bs = append(bs, hinted[:at]...)
	bs = append(bs, keyTranscript)
	return append(bs, hinted[at:]...)
}
