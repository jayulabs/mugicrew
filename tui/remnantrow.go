package tui

// A Remnant's rows on the Execute tab (ADR 0409 (mugicrew-a-remnant-row…)). The landed
// Checkpoint it is related to is closed and has no tree row, so the waiter scan keys on
// that relation: the Remnant is a side row in the landed Checkpoint's Initiative and, while
// takeable, an Actionable row. X on it opens the drop dialog.

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/remnant"
	"jayulabs.org/mugicrew/session"
)

// remnantRel is the relation of a Remnant row to its landed Checkpoint.
const remnantRel = "stranded"

var (
	keyDropRemnant = verbKey("X", "drop")
	// remnantSeams and runRemnantDrop are the drop's act, vars so the tests stub them.
	remnantSeams    = remnant.Real
	runRemnantDrop  = func(id string) (remnant.Result, error) { return remnant.Drop(remnantSeams, id) }
	readStrandRange = git.CommitsAhead
)

// remnantRow is a live Remnant with the landed Checkpoint it is related to.
type remnantRow struct{ b, landed *bd.Bead }

// remnants are the live Remnants whose landed Checkpoint is in the snapshot, in beadCmp order.
func (g graphData) remnants() (out []remnantRow) {
	for i, b := range g.beads {
		if b.Kind != contract.KindRemnant || !execLive(b) {
			continue
		}
		for _, id := range b.Related {
			if j := g.beadIndex(id); j >= 0 && g.beads[j].Kind == contract.KindCheckpoint {
				out = append(out, remnantRow{&g.beads[i], &g.beads[j]})
				break
			}
		}
	}
	slices.SortStableFunc(out, func(a, b remnantRow) int { return beadCmp(*a.b, *b.b) })
	return out
}

// readStrandCommits is the commit count of each open Remnant's Stranded: range, read with the slow tier.
func readStrandCommits(beads []bd.Bead) map[string]int {
	out := map[string]int{}
	for _, b := range beads {
		if b.Kind != contract.KindRemnant || !b.Unclosed() {
			continue
		}
		if tip, head, ok := strings.Cut(bd.ParseRemnantBody(b.Description).StrandedRange(), ".."); ok {
			if n, err := readStrandRange(tip, head); err == nil {
				out[b.ID] = n
			}
		}
	}
	return out
}

// strandSummary is the row's right-edge count summary: 3 commits · 2 kept · 5 children. The
// commit count rides out when its range is unknown or unread.
func (g graphData) strandSummary(b *bd.Bead) string {
	var parts []string
	rb := bd.ParseRemnantBody(b.Description)
	if n, ok := g.strandCommits[b.ID]; ok {
		parts = append(parts, fmt.Sprintf("%d %s", n, commitNoun(n)))
	} else if rb.StrandedLine() == "" {
		parts = append(parts, "0 commits")
	}
	parts = append(parts, fmt.Sprintf("%d kept", len(rb.Kept())), fmt.Sprintf("%d children", len(rb.Children())))
	return strings.Join(parts, " · ")
}

// heldHint is why a held Remnant's tree row is held: the running children and the pivots
// that block it, e.g. `held by 2 running · pivot <title>`. "" when nothing blocks it.
func (g graphData) heldHint(b *bd.Bead) string {
	var running int
	var parts []string
	for _, h := range g.openBlockers(b) {
		if h.Kind == contract.KindPivot {
			parts = append(parts, "pivot "+h.Title)
		} else {
			running++
		}
	}
	if running > 0 {
		parts = append([]string{fmt.Sprintf("held by %d running", running)}, parts...)
	} else if len(parts) > 0 {
		parts[0] = "held by " + parts[0]
	}
	return strings.Join(parts, " · ")
}

// respinWait is the title of the open Renumber Checkpoint caused-by the Remnant b's
// landed Checkpoint, "" when none holds its respin (ADR 0408 §1). It is no blocker: the
// Remnant stays takeable.
func (g graphData) respinWait(b *bd.Bead) string {
	for _, id := range b.Related {
		for _, c := range g.beads {
			if c.Kind == contract.KindCheckpoint && c.CausedBy == id && c.Unclosed() {
				return c.Title
			}
		}
	}
	return ""
}

// respinWaitText is the dim amber suffix the hold renders as, "" when it does not apply.
func (g graphData) respinWaitText(b *bd.Bead) string {
	if t := g.respinWait(b); t != "" {
		return "respin waits on Renumber " + t
	}
	return ""
}

// remnantXWhy is why X refuses the Remnant b, "" when it may open the drop dialog.
func (g graphData) remnantXWhy(b *bd.Bead) string {
	switch bl := g.heldBy(b); {
	case !b.Unclosed():
		return b.ID + " is closed — nothing to drop"
	case len(bl) > 0:
		return fmt.Sprintf("%s is blocked by %s (%s) — not dropping", b.ID, bl[0].Title, bl[0].ID)
	case g.sessions[b.ID].State == session.Alive:
		return b.ID + " has a live session — finish it before dropping"
	}
	return ""
}

// dropRemnantKey is X on a Remnant row: the drop dialog, listing what it deletes.
func (m Model) dropRemnantKey(g graphData, b bd.Bead) (tea.Model, tea.Cmd) {
	if why := g.remnantXWhy(&b); why != "" {
		m.setStatus(why)
		return m, nil
	}
	body := []string{"Drop deletes these refs and re-files nothing:", ""}
	var refs []string
	for _, id := range b.Related {
		if g.beadByID(id).Kind == contract.KindCheckpoint {
			refs = append(refs, git.BranchName(string(contract.KindCheckpoint), id))
		}
	}
	refs = append(refs, bd.ParseRemnantBody(b.Description).KeptBranches()...)
	for _, ref := range refs { // Drop skips a missing ref, so the dialog doesn't name one
		if ok, err := remnantSeams.HasBranch(ref); err != nil || ok {
			body = append(body, "  "+ref)
		}
	}
	body = append(body, "", "The stranded work is gone for good; nothing is offshot or respun.")
	return m.push(confirmDialog("Drop "+b.Title+"?", body, b.ID, func(m Model, d dialog) (Model, tea.Cmd) {
		id := d.id
		return into(m.startAct(id, "dropping", func() writeDoneMsg {
			_, err := runRemnantDrop(id)
			return writeDoneMsg{id, func(m Model) (tea.Model, tea.Cmd) { return m.doneOrErr("drop", "dropped "+b.Title, err) }}
		}))
	})), nil
}
