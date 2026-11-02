package zoro

import (
	"errors"
	"slices"
	"strings"

	"jayulabs.org/mugicrew/bd"
)

// amendmentPrefix opens an AMENDMENT comment; readers of the effective Spec look for it.
const amendmentPrefix = "AMENDMENT ("

// amendmentComment is the comment Amend writes on the Spec and each Ask Critique.
func amendmentComment(pivotID, amendment string) string {
	return amendmentPrefix + pivotID + "): " + strings.TrimSpace(amendment)
}

// Amend is the amend sweep (`mugicrew pivot amend`, ADR 0414 §1–3): the triage session decided
// with the human that the FR's mechanism changes and stays on this Checkpoint. In order,
// each leg guarded so a re-run after a partial failure converges:
//
//  1. append AMENDMENT (<pivot>): <amendment> to the pivot's Spec, skipped when the Spec
//     already has a comment with that prefix (the Spec body is never edited, ADR 0044 §5);
//  2. write the same comment on each Ask Critique, with the same skip;
//  3. with no Ask, refile the FR as dismiss does;
//  4. comment on each raising Errand (errandComment);
//  5. close the pivot 'amended: <gist>', last, so a closed pivot means a finished amend.
//
// A closed pivot is a no-op. critique follows Dismiss's rule: required with no Ask Critique,
// refused with one. Everything is checked before any write.
func Amend(pivotID, gist, amendment, critique, draftedBy string) error {
	cp, spec, open, err := openTriage("amend", pivotID, gist, draftedBy)
	if err != nil || !open {
		return err
	}
	if strings.TrimSpace(amendment) == "" {
		return errors.New("pivot amend: empty --amendment for " + pivotID)
	}
	r, err := pivotRaisers(pivotID)
	if err != nil {
		return err
	}
	if err := checkRefile("amend", pivotID, critique, r); err != nil {
		return err
	}
	text := amendmentComment(pivotID, amendment)
	for _, id := range append([]string{spec}, beadIDs(r.Asks)...) {
		if err := commentOnce(id, amendmentPrefix+pivotID+")", text); err != nil {
			return err
		}
	}
	refiled, err := refile(pivotID, cp, critique, draftedBy, r)
	if err != nil {
		return err
	}
	err = errandComment(pivotID, r.Errands, func(e bd.Bead) string {
		carrying := r.ErrandAsks[e.ID] // this Errand's own Asks
		if len(carrying) == 0 {
			carrying = beadIDs(r.Asks)
		}
		if refiled != "" {
			carrying = append(slices.Clone(carrying), refiled)
		}
		return "pivot " + pivotID + " amended: " + gist + ". Wait for " + strings.Join(carrying, ", ") + "."
	})
	if err != nil {
		return err
	}
	return bd.Close(pivotID, bd.AmendedCloseReason(gist))
}

// commentOnce writes text as a comment on id unless one already starts with prefix.
func commentOnce(id, prefix, text string) error {
	cs, err := bd.Comments(id)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(cs, func(c bd.Comment) bool { return strings.HasPrefix(c.Text, prefix) }) {
		return nil
	}
	return bd.AddComment(id, text)
}
