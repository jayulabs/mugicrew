package tui

import (
	"slices"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/loop"
)

// snapView is the eligibility verdict's View over the TUI's loaded graph (ADR 0379): it does
// no I/O and never errors, so the row reads what the press reads, one refresh stale at worst.
type snapView struct{ g graphData }

func (v snapView) Children(id string) ([]bd.Bead, error) {
	idx := v.g.children(id)
	out := make([]bd.Bead, len(idx))
	for i, j := range idx {
		out[i] = v.g.beads[j]
	}
	return out, nil
}

func (v snapView) Efforts() (*git.EffortSet, error) {
	if v.g.efforts == nil {
		return git.NewEffortSet(), nil
	}
	return v.g.efforts, nil
}

// Landed is the refresh's batched trailer map; the foreign sha was dropped there, so the
// row never sees a foreign landing.
func (v snapView) Landed(b bd.Bead) (loop.Trailer, error) {
	sha, ok := v.g.landed[b.ID]
	if !ok {
		sha, ok = v.g.blockLanded[b.ID]
	}
	return loop.Trailer{Sha: sha, Landed: ok}, nil
}

// Contains answers from the refresh's ancestry facts; an unread pair counts as contained, so
// the row never holds on a guess (the press-time read is the authoritative gate).
func (v snapView) Contains(_ bd.Bead, tip, sha string) (bool, error) {
	in, read := v.g.contains[loop.ContainsKey(tip, sha)]
	return in || !read, nil
}

// SameRepo is always true: the snapshot does no repo resolution, and an ancestry fact it
// never read counts as contained, so a cross-repo pair never holds a row.
func (v snapView) SameRepo(_, _ bd.Bead) bool { return true }

// CheckpointTip is "" for an unknown tip, which pins nothing.
func (v snapView) CheckpointTip(id string) (string, error) { return v.g.cpTips[id], nil }

// Blockers is b's Ordering targets in edge order, each with its edge type; a target missing
// from the snapshot counts as closed (the default set loads every non-closed bead).
func (v snapView) Blockers(b bd.Bead) ([]loop.Blocker, error) {
	var out []loop.Blocker
	for _, id := range b.BlockedBy {
		var t bd.Bead
		if i := v.g.beadIndex(id); i >= 0 {
			t = v.g.beads[i]
		} else if x, ok := v.g.behind[id]; ok {
			t = x
		} else {
			continue
		}
		edge := loop.EdgeBlocks
		if slices.Contains(b.CondBlockedBy, id) {
			edge = loop.EdgeConditional
		}
		out = append(out, loop.Blocker{Bead: t, Edge: edge})
	}
	return out, nil
}

// Bead resolves a walk hop from the snapshot, else the refresh's walk-miss read; an id in
// neither is a stub, as beadByID returns it, so the walk reads it as unpaused.
func (v snapView) Bead(id string) (bd.Bead, error) {
	if i := v.g.beadIndex(id); i >= 0 {
		return v.g.beads[i], nil
	}
	if b, ok := v.g.ancestors[id]; ok {
		return b, nil
	}
	if b, ok := v.g.behind[id]; ok {
		return b, nil
	}
	return bd.Bead{ID: id, Title: id}, nil
}

// eligibility is b's run-now verdict over the loaded graph: the refresh's exact frontier widened
// with loop.Readmit, as the press widens its fresh one. It never errors (snapView can't).
func (g graphData) eligibility(b *bd.Bead) loop.Verdict {
	v, _ := loop.Judge(*b, loop.ModeRunNow, g.widened(), loop.Inputs{}, snapView{g})
	return v
}

// widened is the row's frontier: bd ready -n 0 plus loop.Readmit over the snapshot, per call: one pass over beads, cheap beside a frame.
func (g graphData) widened() []bd.Bead {
	return loop.Readmit(g.frontier, g.beads)
}
