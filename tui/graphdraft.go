package tui

import (
	"cmp"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// creatorState is where a Draft's creator stands, read off the snapshot alone (Spec
// mc-pyamf D4): xLabel's arms, not Chopper's Judge, which needs process, cwd, route
// and lock reads that render can't make (ADR 0101).
type creatorState int

const (
	creatorNone     creatorState = iota // the Draft has no drafted-by: label
	creatorStarting                     // its launch succeeded; no snapshot shows it alive yet
	creatorClaimed                      // claimed by another actor: not ours to call gone
	creatorGone                         // no creator bead, or a HITL creator with no session
	creatorUnknown                      // a HITL creator while the session table is unreadable
	creatorPublishing
	creatorLive
	creatorExited // a HITL session exited: resumable
	creatorFenced // a HITL creator held by a Checkup
	creatorStranded
	creatorReleased // an AFK creator no longer in_progress, its Drafts left behind
)

// word is the drawer's name for the state.
func (s creatorState) word(c *bd.Bead) string {
	switch s {
	case creatorStarting:
		return "starting"
	case creatorClaimed:
		return "claimed by " + c.Assignee
	case creatorGone:
		return "gone"
	case creatorUnknown:
		return "unknown (session table unreadable)"
	case creatorPublishing:
		return "publishing"
	case creatorLive:
		return "live"
	case creatorExited:
		return "exited, resumable"
	case creatorFenced:
		return "Checkup-fenced"
	case creatorStranded:
		return "stranded"
	case creatorReleased:
		return "stranded (released)"
	}
	return ""
}

// draftCreator is the creator bead id named by b's drafted-by: label, "" when it has none.
func draftCreator(b *bd.Bead) string {
	prefix := contract.DraftedBy("")
	for _, l := range b.Labels {
		if id, ok := strings.CutPrefix(l, prefix); ok && id != "" {
			return id
		}
	}
	return ""
}

// creatorStateOf classifies Draft b's creator. It is the one classification the drawer's
// drafted by line and the refusal wording share, so the two never disagree.
func (g graphData) creatorStateOf(b *bd.Bead) (c *bd.Bead, s creatorState) {
	id := draftCreator(b)
	if id == "" {
		return nil, creatorNone
	}
	i := g.beadIndex(id)
	if i >= 0 {
		c = &g.beads[i]
	} else {
		c = &bd.Bead{ID: id, Title: id}
	}
	switch {
	case !g.launched[id].IsZero():
		return c, creatorStarting
	case i < 0:
		return c, creatorGone
	case bd.ClaimedElsewhere(c.Assignee):
		return c, creatorClaimed
	case isHITL(c):
		switch sess := g.sessions[id]; {
		case g.sessErr != nil:
			return c, creatorUnknown
		case sess.State == session.Alive && sess.Closing:
			return c, creatorPublishing
		case sess.State == session.Alive:
			return c, creatorLive
		case sess.State == session.Exited:
			return c, creatorExited
		case bd.PausedBy(*c, bd.PausedCheckup):
			return c, creatorFenced
		}
		return c, creatorGone // no session, whatever its kind: strandable's at-rest kinds don't apply
	}
	switch {
	case g.liveOf(c) == alive:
		return c, creatorLive
	case g.liveOf(c) == dead, g.liveOf(c) == orphan:
		return c, creatorStranded
	case c.Status == "closed":
		return c, creatorGone // closed reads as gone for HITL and AFK alike
	case c.Status != "in_progress":
		return c, creatorReleased
	}
	return c, creatorLive // in_progress, but not an active AFK effort: nothing says it stopped
}

// draftedByLine is the drawer's drafted by value for Draft b.
func (g graphData) draftedByLine(b *bd.Bead) string {
	c, s := g.creatorStateOf(b)
	if s == creatorNone {
		return dimSt.Render("no creator")
	}
	who := c.Title
	if who == "" {
		who = c.ID
	}
	return who + softSt.Render(" · "+s.word(c))
}

// draftRefused are the keys a Draft row refuses (Spec mc-pyamf D6): every mutating key
// the Graph routes to the selection, enter included. d, t, R, y and g are not here.
var draftRefused = []string{"enter", "b", "p", "x", "X", "+", "=", "-", "_", "z", "e", "c", "a", "A", "B", "U"}

// draftRefuses is whether k is a key a Draft row refuses.
func draftRefuses(k tea.KeyMsg) bool { return slices.Contains(draftRefused, k.String()) }

// draftWhy is the one Draft predicate (Spec mc-pyamf D5): "" unless b's own status is
// Draft (nothing is inherited from an ancestor), else why no mutating key acts on it,
// worded by the creator's state. The hints bar, the drawer and the key guard all read it.
func (g graphData) draftWhy(b *bd.Bead) string {
	if b == nil || !b.Draft() {
		return ""
	}
	c, s := g.creatorStateOf(b)
	if s == creatorNone {
		return "Drafts publish when their creator exits"
	}
	who := cmp.Or(c.Title, c.ID)
	if c.Title != "" && c.Title != c.ID {
		who += " (" + c.ID + ")"
	}
	switch s {
	case creatorStarting, creatorLive, creatorPublishing:
		return "Drafts publish when " + who + " exits"
	case creatorExited:
		return who + " exited — resume it (enter) or drop it (x) on its row; its Drafts publish when it exits again"
	case creatorGone, creatorStranded, creatorReleased:
		if g.xLabel(c) == "recover" {
			return who + " is " + s.word(c) + " — x recovers it on its row, and its Drafts publish"
		}
		return who + " is " + s.word(c) + " — recover it (mugicrew recover " + c.ID + ") to publish its Drafts"
	case creatorFenced:
		return who + " is held by an open Checkup — finish that Checkup to publish these Drafts"
	case creatorUnknown:
		return "the session table is unreadable, so the state of " + who + " is unknown"
	case creatorClaimed:
		return who + " is claimed by " + c.Assignee + " — Drafts publish when they finish"
	}
	return "Drafts publish when their creator exits"
}
