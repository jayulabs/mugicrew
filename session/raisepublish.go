package session

import (
	"fmt"
	"slices"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/luffy"
)

// Seams over the reads a raise's publish plan makes, so it is testable without a
// live bd or repo.
var (
	specOf           = bd.SpecOf
	childrenOfKind   = bd.ChildrenOfKind
	gateEdges        = bd.GateEdges
	hasEdge          = bd.HasEdge
	commentsOf       = bd.Comments
	addComment       = bd.AddComment
	clearMetadata    = bd.ClearMetadata
	landedOnMainline = luffy.HostLanded
)

// raiseSession is a Signal's or Task's publish plan when it has raise Drafts (pivot or
// Critique Drafts); recorded merges alone are a leftover and are ignored unless their
// lineage edges already exist (mergesOnlyPlan) (Spec decisions 4 and 6, ADR 0323 §2, §3,
// ADR 0326 §4):
// it re-reads each host, re-checks it, and hands the gate edges of every pivot Draft
// carrying raise_checkpoint, whoever drafted it, and each merge's lineage edge (all
// filtered by HasEdge). A Signal also gets the close rerouted: <ids>, handed only when
// it is not already closed, so a re-publish converges; a Task is never closed (ADR 0326
// §6). ok is false when the creator has no raise Draft.
func raiseSession(creator bd.Bead) (plan publishPlan, ok bool, err error) {
	drafts, err := listDrafts(creator.ID)
	if err != nil {
		return plan, false, err
	}
	pivots, critiques := luffy.RaiseDrafts(creator, drafts)
	merges, err := luffy.RaiseMergesOf(creator)
	if err != nil {
		return plan, false, err
	}
	if len(pivots)+len(critiques) == 0 {
		return mergesOnlyPlan(creator, merges)
	}
	plan.merges = merges

	hosts, err := luffy.RaiseHosts(pivots, critiques)
	if err != nil {
		return publishPlan{}, false, fmt.Errorf("raise of %s: %w", creator.ID, err)
	}
	if creator.Kind == contract.KindSignal && len(hosts) > 1 {
		return publishPlan{}, false, fmt.Errorf("raise of %s: Drafts name more than one host (%s)", creator.ID, strings.Join(hosts, ", "))
	}
	for _, h := range hosts {
		_, spec, err := recheckHost(h)
		if err != nil {
			return publishPlan{}, false, fmt.Errorf("raise of %s: %w", creator.ID, err)
		}
		var ids []string
		for _, p := range pivots {
			if luffy.PivotHost(p) == h {
				ids = append(ids, p.ID)
			}
		}
		if len(ids) == 0 {
			continue
		}
		edges, err := gateEdges(h, spec, ids)
		if err != nil {
			return publishPlan{}, false, err
		}
		plan.edges = append(plan.edges, edges...)
	}
	ids := append(bd.IDs(pivots), bd.IDs(critiques)...)
	closing := creator.Kind == contract.KindSignal && creator.Status != "closed"
	for _, m := range merges {
		ids = append(ids, m.Pivot)
		have, err := hasEdge(m.Pivot, creator.ID, lineage(creator))
		if err != nil {
			return publishPlan{}, false, err
		}
		if !have {
			plan.edges = append(plan.edges, bd.Edge{From: m.Pivot, To: creator.ID, Type: lineage(creator)})
		}
	}
	if closing {
		slices.Sort(ids)
		ids = slices.Compact(ids)
		plan.closing = &bd.Closing{ID: creator.ID, Reason: "rerouted: " + strings.Join(ids, ", ")}
	}
	return plan, true, nil
}

// lineage is the edge type a merged-into pivot carries to its raise's creator: caused-by
// a Signal, discovered-from a Task (ADR 0326 §5).
func lineage(creator bd.Bead) string {
	if creator.Kind == contract.KindSignal {
		return "caused-by"
	}
	return "discovered-from"
}

// mergesOnlyPlan is the re-publish after a batch that opened the Drafts but lost a
// merge's evidence comment: no Drafts are left, yet every recorded merge's lineage edge
// is already on the creator, so the plan carries only the merges for postMerges (its
// marker check keeps that idempotent), with no edge and no close. Merges without their
// edge are a failed raise's leftover and are ignored (Spec decision 4); ok is false for
// them.
func mergesOnlyPlan(creator bd.Bead, merges []luffy.RaiseMerge) (publishPlan, bool, error) {
	if len(merges) == 0 {
		return publishPlan{}, false, nil
	}
	for _, m := range merges {
		have, err := hasEdge(m.Pivot, creator.ID, lineage(creator))
		if err != nil {
			return publishPlan{}, false, err
		}
		if !have {
			return publishPlan{}, false, nil
		}
	}
	return publishPlan{merges: merges}, true, nil
}

// recheckHost repeats reconcile's refusals on a raise's Checkpoint through
// luffy.CheckHost (Spec FR 38, ADR 0324 §6). Any hit fails the publish.
func recheckHost(id string) (host bd.Bead, spec string, err error) {
	return luffy.CheckHost(id, luffy.NewHostReads(showBead, specOf, childrenOfKind, landedOnMainline))
}

// postMerges posts each merge's evidence as a comment on its pivot once the batch has
// landed. The first line is the marker "Rerouted from <signal>" or "Raised by Task
// <task>"; a pivot already carrying it is skipped, so a re-publish re-posts a lost
// comment and never doubles one.
func postMerges(creator bd.Bead, merges []luffy.RaiseMerge) error {
	marker := "Rerouted from " + creator.ID
	if creator.Kind == contract.KindTask {
		marker = "Raised by Task " + creator.ID
	}
	for _, m := range merges {
		cs, err := commentsOf(m.Pivot)
		if err != nil {
			return err
		}
		if slices.ContainsFunc(cs, func(c bd.Comment) bool { return firstLine(c.Text) == marker }) {
			continue
		}
		if err := addComment(m.Pivot, marker+"\n\n"+m.Evidence); err != nil {
			return err
		}
	}
	return nil
}

// firstLine is text up to its first newline.
func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return line
}
