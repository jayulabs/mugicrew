package tui

import (
	"cmp"
	"strings"
	"sync"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/loop"
)

// memoEfforts wraps read so the first call per press pays for the git and config reads; a
// failed read yields nil, which the view reads afresh and so surfaces as an error.
func memoEfforts(read func() (*git.EffortSet, error)) func() *git.EffortSet {
	once := sync.OnceValues(read)
	return func() *git.EffortSet {
		set, err := once()
		if err != nil {
			return nil
		}
		return set
	}
}

// holderBeads is the verdict's holders as plain beads.
func holderBeads(hs []loop.Holder) []bd.Bead {
	out := make([]bd.Bead, len(hs))
	for i, h := range hs {
		out[i] = h.Bead
	}
	return out
}

// heldHolders is the held (closed, unintegrated) holders: those carrying an effort branch.
func heldHolders(hs []loop.Holder) []bd.Bead {
	var out []bd.Bead
	for _, h := range hs {
		if h.Branch != "" {
			out = append(out, h.Bead)
		}
	}
	return out
}

func holderIDs(hs []loop.Holder) string { return strings.Join(bd.IDs(holderBeads(hs)), ", ") }

// approvalsOnly is whether every holder is an open Approval: a Chart held by nothing else is
// waiting on a human, not on defog.
func approvalsOnly(hs []loop.Holder) bool {
	for _, h := range hs {
		if h.Bead.Kind != contract.KindApproval || !h.Bead.Open() {
			return false
		}
	}
	return len(hs) > 0
}

// causeHint is the TUI's words for a not-runnable verdict: one wording per loop.Cause, the
// single map the press-time gate reads (and the row hint, once it reads the verdict). The
// loop package returns causes and holders, never text.
func causeHint(b bd.Bead, v loop.Verdict) string {
	id := b.ID
	switch v.Cause {
	case loop.CauseNotCandidate:
		switch {
		case b.Status == "closed":
			return id + " is closed — kept for lineage, nothing to run"
		case b.Status == "blocked":
			return id + " is blocked — enter runs a bead only once its blockers clear"
		case isHITL(&b) && !bd.AwaitingUndraft(b): // a waiting fallback Approval's next step is the un-draft, not a session
			return id + " is an open HITL bead — press enter to launch it (b in the background)"
		}
		return id + " is not run-now eligible (only a ready AFK leaf — e.g. research or a Rollback — a drained Chart, or a land-ready Checkpoint runs via enter)"
	case loop.CausePaused:
		if h := pauseHint(b); h != "" {
			return h
		}
		return id + " is held by paused:" + v.PauseCause + " — run-now won't override it; clear the label first"
	case loop.CauseInheritedPause:
		return inheritedPauseHint(id, v.Holders[0].Bead.ID)
	case loop.CauseContainerFrozen:
		h := v.Holders[0].Bead
		return b.Parent + " is gated by " + cmp.Or(h.Title, h.ID) + " (" + h.ID + ") — not running"
	case loop.CauseBlocked:
		return id + " is blocked / not ready — run-now only drives a bead already in the bd ready frontier"
	case loop.CauseBehindMainline:
		h := v.Holders[0].Bead
		return id + ": " + bd.BehindMainlineRemedy(b.Parent) + " — " + cmp.Or(h.Title, h.ID) + " (" + h.ID + ") landed after the cut"
	case loop.CausePaced:
		return id + " was checked within its poll interval — the loop retries it when due"
	case loop.CauseSurgeryHold:
		return id + ": held by open Surgery " + v.Holders[0].Bead.ID + " — ending the Surgery releases it"
	case loop.CauseUndrained:
		if held := heldHolders(v.Holders); b.Kind == contract.KindChart {
			if len(held) > 0 {
				return chartHeldHint(id, held)
			}
			if approvalsOnly(v.Holders) {
				return id + ": Chart not ready to land — Approval " + holderIDs(v.Holders) + " is awaiting a human"
			}
			return id + ": Chart not drained — its defog frontier hasn't cleared; run-now drives it once every child closes"
		}
		if held := heldHolders(v.Holders); len(held) > 0 {
			return id + ": Checkpoint not drained — " + bd.HeldHint(held)
		}
		return id + ": Checkpoint not drained — member(s) still open: " + holderIDs(v.Holders)
	case loop.CauseLandingOpen:
		return id + ": a Landing is already open (" + holderIDs(v.Holders) + ") — one land attempt at a time"
	case loop.CauseForeignLanding:
		return v.Foreign.Error()
	case loop.CauseDefectOpen:
		return id + ": Checkpoint not ready to land — open conflict/critique: " + holderIDs(v.Holders)
	case loop.CausePivotOpen:
		return id + ": Checkpoint not ready — open Pivot " + holderIDs(v.Holders) + " freezes it"
	case loop.CauseAwaitingApproval:
		return id + ": Checkpoint not ready to land — Approval " + holderIDs(v.Holders) + " is awaiting a human"
	case loop.CauseNoMembers:
		return id + ": Checkpoint not ready to land — it has no members yet"
	case loop.CauseLandHeld:
		return id + ": " + landHeldNote(bd.Titles(holderBeads(v.Holders)))
	case loop.CauseReviewDue:
		return id + ": review due — the loop spawns it"
	}
	return id + " is not run-now eligible (" + string(v.Cause) + ")"
}
