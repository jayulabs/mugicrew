package bd

import (
	"slices"

	"jayulabs.org/mugicrew/contract"
)

// IsWithdrawable reports whether withdraw takes b: an IsChainHead, or an unclosed
// Task that is not an unpublished Draft (Spec mc-xw3ez D1). IsChainHead stays
// unchanged: a cancel's chain list and a release by chain head never see a Task, which
// cancel and release handle as a member (ADR 0349).
func IsWithdrawable(b Bead, all []Bead) bool {
	return IsChainHead(b, all) || (b.Kind == contract.KindTask && b.Unclosed() && !IsDraft(b))
}

// Sweep is the Tasks a withdraw closes beyond its chain, the ones it spares and
// the land holds it lets go (Spec mc-xw3ez D2, mc-d5zdj D1).
type Sweep struct {
	Swept    []Swept        // forward rule: a Task after an in-set bead, in sweep order
	Before   []BeforeClosed // backward rule: a Task before the set, in close order
	LeftOpen []Held         // Tasks blocking an in-set bead that stay open
	Releases []string       // unclosed Checkpoints outside the final set blocked by a closing Task
}

// Swept is a Task the withdraw closes with the beads in closing.
type Swept struct {
	Bead  Bead
	After string   // the closing or swept bead it hangs under (first in-set blocker)
	Also  []string // unclosed blockers outside the final set
}

// BeforeClosed is a Task the withdraw closes because everything it blocks closes too.
type BeforeClosed struct {
	Bead   Bead
	Before string // the first in-set bead it blocks: the row it hangs under
}

// Held is a Task that blocks an in-set bead but stays open, because it also blocks
// something outside the final set.
type Held struct {
	Bead   Bead
	Before string   // the first in-set bead it blocks
	Also   []string // unclosed beads it blocks outside the final set; never empty for a chain head
}

// SweepTasks computes, over the snapshot all, what withdrawing closing sweeps: every
// unclosed, published Task outside closing blocked by a bead in closing or already
// swept (a fixed point). With backward on (a chain head's withdraw, ADR 0351) it
// also before-closes every unclosed, published Task whose unclosed dependents are
// non-empty and all in the set, the two rules alternating until neither grows. The
// Tasks that block an in-set bead and stay open are left open; with backward off
// those are only the direct blockers of a closing Checkpoint. It also returns the
// unclosed Checkpoints outside the final set whose in-set blockers are all Tasks,
// which the withdraw releases rather than refuses. Every other bead ordered after
// the final set still refuses, through OrderedAfter.
func SweepTasks(closing, all []Bead, backward bool) Sweep {
	in := map[string]bool{}
	for _, b := range closing {
		in[b.ID] = true
	}
	byID := map[string]Bead{}
	dependents := map[string][]Bead{} // unclosed beads whose BlockedBy names the key, in snapshot order
	for _, b := range all {
		byID[b.ID] = b
		if b.Unclosed() {
			for _, id := range b.BlockedBy {
				dependents[id] = append(dependents[id], b)
			}
		}
	}
	eligible := func(b Bead) bool { return b.Kind == contract.KindTask && b.Unclosed() && !IsDraft(b) && !in[b.ID] }
	var s Sweep
	for grew := true; grew; {
		grew = false
		for _, b := range all {
			if !eligible(b) {
				continue
			}
			i := slices.IndexFunc(b.BlockedBy, func(id string) bool { return in[id] })
			if i < 0 {
				continue
			}
			in[b.ID] = true
			s.Swept = append(s.Swept, Swept{Bead: b, After: b.BlockedBy[i]})
			grew = true
		}
		if !backward {
			continue
		}
		for _, b := range all {
			ds := dependents[b.ID]
			if !eligible(b) || len(ds) == 0 || slices.ContainsFunc(ds, func(d Bead) bool { return !in[d.ID] }) {
				continue
			}
			in[b.ID] = true
			s.Before = append(s.Before, BeforeClosed{Bead: b, Before: ds[0].ID})
			grew = true
		}
	}
	for i, w := range s.Swept {
		for _, id := range w.Bead.BlockedBy {
			if x, ok := byID[id]; ok && x.Unclosed() && !in[id] {
				s.Swept[i].Also = append(s.Swept[i].Also, id)
			}
		}
	}
	alsoOf := func(t Bead) []string {
		var out []string
		for _, d := range dependents[t.ID] {
			if !in[d.ID] {
				out = append(out, d.ID)
			}
		}
		return out
	}
	if backward {
		for _, t := range all {
			if !eligible(t) {
				continue
			}
			i := slices.IndexFunc(dependents[t.ID], func(d Bead) bool { return in[d.ID] })
			if i < 0 {
				continue
			}
			s.LeftOpen = append(s.LeftOpen, Held{Bead: t, Before: dependents[t.ID][i].ID, Also: alsoOf(t)})
		}
	} else {
		for _, c := range closing {
			if c.Kind != contract.KindCheckpoint {
				continue
			}
			for _, id := range c.BlockedBy {
				t, ok := byID[id]
				if !ok || !eligible(t) || slices.ContainsFunc(s.LeftOpen, func(h Held) bool { return h.Bead.ID == id }) {
					continue
				}
				s.LeftOpen = append(s.LeftOpen, Held{Bead: t, Before: c.ID, Also: alsoOf(t)})
			}
		}
	}
	for _, b := range all {
		if b.Kind != contract.KindCheckpoint || !b.Unclosed() || in[b.ID] {
			continue
		}
		var held bool
		allTasks := true
		for _, id := range b.BlockedBy {
			if !in[id] {
				continue
			}
			held = true
			allTasks = allTasks && byID[id].Kind == contract.KindTask
		}
		if held && allTasks {
			s.Releases = append(s.Releases, b.ID)
		}
	}
	return s
}
