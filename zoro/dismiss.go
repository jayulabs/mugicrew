package zoro

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// Dismiss is the dismiss sweep (`mugicrew pivot dismiss`, ADR 0414 §1–3, §6): the
// triage session decided with the human that the pivot's FR stands as specced, so the
// unfinished work carries on as a Critique. In order, each leg guarded so a re-run after
// a partial failure converges:
//
//  1. refile the FR as a kind:critique Draft by draftedBy, parented to the Checkpoint and
//     discovered-from the pivot — the re-run key — unless one already is;
//  2. rewrite each Ask Critique named in rewrites (title = the file's first line, body = the
//     rest), skipped when both already match, then close every other open Ask
//     'dismissed: <pivot>'; the Signal behind a reroute pivot stays closed;
//  3. comment on each raising Errand (errandComment);
//  4. close the pivot 'dismissed: <gist>', last, so a closed pivot means a finished dismiss.
//
// A closed pivot is a no-op. critique is the refile's body; it is required when the pivot
// has no Ask Critique and refused when it has one (the Ask is the refile). rewrites maps an
// Ask id to its new text; a key that is not an Ask of this pivot is refused. All of it is
// checked before any write.
func Dismiss(pivotID, gist, critique string, rewrites map[string]string, draftedBy string) error {
	cp, _, open, err := openTriage("dismiss", pivotID, gist, draftedBy)
	if err != nil || !open {
		return err
	}
	r, err := pivotRaisers(pivotID)
	if err != nil {
		return err
	}
	if err := checkRefile("dismiss", pivotID, critique, r); err != nil {
		return err
	}
	for id, text := range rewrites {
		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("pivot dismiss: --rewrite %s has an empty file", id)
		}
		if !slices.ContainsFunc(r.Asks, func(a bd.Bead) bool { return a.ID == id }) {
			return fmt.Errorf("pivot dismiss: --rewrite %s is not an Ask Critique of %s", id, pivotID)
		}
	}
	refiled, err := refile(pivotID, cp, critique, draftedBy, r)
	if err != nil {
		return err
	}
	kept := map[string]bool{} // the Asks rewritten, which stay open to carry the work on
	for _, ask := range r.Asks {
		text, rewrite := rewrites[ask.ID]
		if rewrite {
			if err := rewriteAsk(ask.ID, pivotID, text); err != nil {
				return err
			}
			kept[ask.ID] = true
		} else if err := closeAsk(ask.ID, pivotID); err != nil {
			return err
		}
	}
	err = errandComment(pivotID, r.Errands, func(e bd.Bead) string {
		var carrying []string // the open Critiques that carry this Errand's work on
		for _, id := range r.ErrandAsks[e.ID] {
			if kept[id] {
				carrying = append(carrying, id)
			}
		}
		if refiled != "" {
			carrying = append(carrying, refiled)
		}
		if len(carrying) > 0 {
			return dismissedCarried(pivotID, gist, carrying)
		}
		return dismissedDropped(pivotID, gist)
	})
	if err != nil {
		return err
	}
	return bd.Close(pivotID, bd.DismissedCloseReason(gist))
}

// dismissedCarried is the Errand comment when an open Critique carries the work.
func dismissedCarried(pivotID, gist string, critiques []string) string {
	return "pivot " + pivotID + " dismissed: " + gist + ". Wait for " + strings.Join(critiques, ", ") + "."
}

// dismissedDropped is the Errand comment when dismiss closed the Errand's Ask instead.
func dismissedDropped(pivotID, gist string) string {
	return "pivot " + pivotID + " dismissed: " + gist + ". The step may be wrong; amend it on resume."
}

// errandComment writes textFor(Errand) as one comment on each raising Errand, skipping an Errand that
// already has a comment starting "pivot <pivot> " (a re-run, or the amend verb's own). The
// Errands stay open.
func errandComment(pivotID string, errands []bd.Bead, textFor func(bd.Bead) string) error {
	for _, e := range errands {
		if err := commentOnce(e.ID, "pivot "+pivotID+" ", textFor(e)); err != nil {
			return err
		}
	}
	return nil
}

// rewriteAsk gives an Ask Critique the rewrite's title (its first line) and body (the
// rest), each under the pivot's claim, skipping a field that already matches.
func rewriteAsk(id, pivotID, text string) error {
	title, body, _ := strings.Cut(strings.TrimSpace(text), "\n")
	title, body = strings.TrimSpace(title), strings.TrimSpace(body)
	ask, err := bd.Show(id)
	if err != nil {
		return err
	}
	if ask.Title != contract.StripKindPrefix(title) {
		if err := bd.SetField(id, bd.TitleField, title, bd.UnderClaim(pivotID)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(ask.Description) == body {
		return nil
	}
	return bd.SetDescription(id, body, bd.UnderClaim(pivotID))
}

// closeAsk closes an open Ask Critique 'dismissed: <pivot>'; a closed one is skipped.
func closeAsk(id, pivotID string) error {
	ask, err := bd.Show(id)
	if err != nil {
		return err
	}
	if !ask.Open() {
		return nil
	}
	return bd.Close(id, bd.DismissedCloseReason(pivotID))
}

// openTriage is dismiss's and amend's preamble: openSweep's (a closed pivot is a finished
// sweep, a non-pivot is refused, the gist is non-empty) plus draftedBy non-empty and the
// Checkpoint still open — a closed one means a confirm or escalate sweep ran.
func openTriage(verb, pivotID, gist, draftedBy string) (cp, spec string, open bool, err error) {
	cp, spec, open, err = openSweep(verb, "gist", pivotID, gist)
	if err != nil || !open {
		return "", "", false, err
	}
	if strings.TrimSpace(draftedBy) == "" {
		return "", "", false, fmt.Errorf("pivot %s: empty --drafted-by for %s", verb, pivotID)
	}
	head, err := bd.Show(cp)
	if err != nil {
		return "", "", false, err
	}
	if !head.Open() {
		return "", "", false, fmt.Errorf("pivot %s: Checkpoint %s is closed (a confirm or escalate sweep ran)", verb, cp)
	}
	return cp, spec, true, nil
}

// raisers is what a pivot's raiser left in the graph (pivotRaisers).
type raisers struct {
	Asks    []bd.Bead // the Critiques the raiser filed asking for the work (a reroute Signal's or an Errand's)
	Errands []bd.Bead // the Errands that raised the pivot
	// ErrandAsks maps a raising Errand to its own Asks: those discovered-from it.
	ErrandAsks map[string][]string
	Refile     string // the Critique a Zoro triage already refiled, "" when none
}

// pivotRaisers reads the pivot's raiser through either provenance edge. An Ask is a
// kind:critique discovered-from the pivot that is also caused-by its Signal (reroute) or
// discovered-from its Errand (pivot raise); a Critique discovered-from the pivot with
// neither is Zoro's refile. drafted-by: labels can't tell them apart, since publish strips
// them. The raising Errands are the kind:errand beads the pivot is discovered-from.
func pivotRaisers(pivotID string) (raisers, error) {
	var r raisers
	up, err := bd.DiscoveredByTargets(pivotID)
	if err != nil {
		return r, err
	}
	for _, c := range up {
		if c.Kind != contract.KindCritique {
			continue
		}
		ask, errands, err := hasRaiser(c.ID)
		if err != nil {
			return r, err
		}
		if ask {
			r.Asks = append(r.Asks, c)
			for _, e := range errands {
				if r.ErrandAsks == nil {
					r.ErrandAsks = map[string][]string{}
				}
				r.ErrandAsks[e] = append(r.ErrandAsks[e], c.ID)
			}
		} else if r.Refile == "" {
			r.Refile = c.ID
		}
	}
	down, err := bd.DiscoveredFromTargets(pivotID)
	if err != nil {
		return r, err
	}
	for _, b := range down {
		if b.Kind == contract.KindErrand {
			r.Errands = append(r.Errands, b)
		}
	}
	return r, nil
}

// hasRaiser reports whether the Critique is caused-by a Signal or discovered-from an Errand,
// and the ids of the Errands it is discovered-from.
func hasRaiser(critique string) (bool, []string, error) {
	var errands []string
	ask := false
	for _, read := range []struct {
		targets func(string) ([]bd.Bead, error)
		kind    contract.Kind
	}{{bd.CausedByTargets, contract.KindSignal}, {bd.DiscoveredFromTargets, contract.KindErrand}} {
		down, err := read.targets(critique)
		if err != nil {
			return false, nil, err
		}
		for _, b := range down {
			if b.Kind == read.kind {
				ask = true
				if read.kind == contract.KindErrand {
					errands = append(errands, b.ID)
				}
			}
		}
	}
	return ask, errands, nil
}

// checkRefile is the --critique versus Ask rule: required with no Ask, refused with one.
func checkRefile(verb, pivotID, critique string, r raisers) error {
	switch {
	case len(r.Asks) > 0 && strings.TrimSpace(critique) != "":
		return fmt.Errorf("pivot %s: %s has Ask Critiques (%s), which carry the FR: drop --critique", verb, pivotID, strings.Join(beadIDs(r.Asks), ", "))
	case len(r.Asks) == 0 && strings.TrimSpace(critique) == "":
		return errors.New("pivot " + verb + ": " + pivotID + " has no Ask Critique, so --critique <file> is required to carry the FR")
	}
	return nil
}

// refile files the unfinished FR as a Critique Draft by draftedBy, parented to the
// Checkpoint, discovered-from the pivot, with the Checkpoint's context and repo stamp. It
// is skipped when one already is, or when an Ask Critique carries the FR. It runs under the
// pivot's claim and returns the refiled Critique's id, "" when an Ask carries the FR.
func refile(pivotID, cp, critique, draftedBy string, r raisers) (string, error) {
	if len(r.Asks) > 0 {
		return "", nil
	}
	if r.Refile != "" {
		return r.Refile, nil
	}
	head, err := bd.Show(cp)
	if err != nil {
		return "", err
	}
	pivot, err := bd.Show(pivotID)
	if err != nil {
		return "", err
	}
	id, _, err := bd.SpawnDiscoveredIfAbsent(pivotID, "", bd.Successor{
		Kind: contract.KindCritique, Title: contract.DraftTitle(critique, pivot.Title), Description: critique,
		DraftedBy: draftedBy, Parent: cp, Context: bd.ContextOf(head), Repo: bd.RepoStampOf(head),
		Serializer: bd.UnderClaim(pivotID),
	})
	return id, err
}

func beadIDs(bs []bd.Bead) []string {
	ids := make([]string, len(bs))
	for i, b := range bs {
		ids[i] = b.ID
	}
	return ids
}
