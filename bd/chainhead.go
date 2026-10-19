package bd

import (
	"slices"
	"strings"

	"jayulabs.org/mugicrew/contract"
)

// IsDraft reports whether b is an unpublished Draft: status deferred with a
// drafted-by: label (ADR 0059).
func IsDraft(b Bead) bool {
	return b.Status == "deferred" && slices.ContainsFunc(b.Labels, func(l string) bool {
		return strings.HasPrefix(l, contract.DraftedBy(""))
	})
}

// IsChainHead reports whether b is an unclosed chain head over the snapshot all
// (Spec mc-5rttm D1, ADR 0310 §1): unclosed; a signal, chart, spec, checkpoint
// or rollback; unparented or a child of an Initiative (open or closed); not an
// unpublished Draft; and with no unclosed bead caused-by it. The TUI runs it
// over its own snapshot, so there is one copy.
func IsChainHead(b Bead, all []Bead) bool {
	switch b.Kind {
	case contract.KindSignal, contract.KindChart, contract.KindSpec, contract.KindCheckpoint, contract.KindRollback:
	default:
		return false
	}
	if !b.Unclosed() || IsDraft(b) {
		return false
	}
	if b.Parent != "" {
		i := slices.IndexFunc(all, func(x Bead) bool { return x.ID == b.Parent })
		if i < 0 || all[i].Kind != contract.KindInitiative {
			return false
		}
	}
	return !slices.ContainsFunc(all, func(x Bead) bool { return x.CausedBy == b.ID && x.Unclosed() })
}

// WithdrawSet is what withdrawing head closes (Spec D1): head, every unclosed
// bead reached by walking CausedBy back from it, and the unclosed subtree of each,
// leaves first, head last. A caused-by cycle ends the walk.
func WithdrawSet(head string, all []Bead) []Bead {
	in, kids := index(all)
	var chain []Bead // head first, then its predecessors
	for id := head; ; {
		b, ok := in[id]
		if !ok || slices.ContainsFunc(chain, func(x Bead) bool { return x.ID == id }) {
			break
		}
		chain = append(chain, b)
		id = b.CausedBy
	}
	var out []Bead
	for _, b := range slices.Backward(chain) { // oldest predecessor first, head last
		if b.Unclosed() { // a closed predecessor's stragglers are not this chain's
			out = append(append(out, unclosedLeavesFirst(b.ID, kids)...), b)
		}
	}
	return out
}

// CancelSet is what cancelling the Initiative closes: each unclosed member
// expanded (as a withdraw set when it is a chain head, else its unclosed subtree),
// then the Initiative itself. A chain whose head is named in released is kept out
// whole (its caused-by predecessors too): it is released, not closed.
func CancelSet(initiative string, all []Bead, released ...string) []Bead {
	in, kids := index(all)
	var out []Bead
	seen := map[string]bool{}
	add := func(bs []Bead) {
		for _, b := range bs {
			if !seen[b.ID] {
				seen[b.ID] = true
				out = append(out, b)
			}
		}
	}
	kept := map[string]bool{}
	for _, h := range released {
		for _, b := range WithdrawSet(h, all) {
			kept[b.ID] = true
		}
	}
	for _, m := range kids[initiative] {
		switch {
		case !m.Unclosed(), kept[m.ID]:
		case IsChainHead(m, all):
			add(WithdrawSet(m.ID, all))
		default:
			add(unclosedLeavesFirst(m.ID, kids))
			add([]Bead{m})
		}
	}
	if ini, ok := in[initiative]; ok {
		add([]Bead{ini})
	}
	return out
}

// OrderedAfter is the unclosed beads outside closing, anywhere in the snapshot,
// that are blocked by (blocks/conditional-blocks) a bead in it: closing would
// make bd read the blocker as satisfied.
func OrderedAfter(closing, all []Bead) []Bead {
	in := map[string]bool{}
	for _, b := range closing {
		in[b.ID] = true
	}
	return Filter(all, func(b Bead) bool {
		return b.Unclosed() && !in[b.ID] && slices.ContainsFunc(b.BlockedBy, func(id string) bool { return in[id] })
	})
}
