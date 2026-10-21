package nami

import (
	"fmt"
	"slices"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// RenumberSeams are the graph and git legs MintRenumber runs over, so Jinbe (and
// Nami's Chart arm) call it over their own seams, the FlagStalePin precedent.
type RenumberSeams struct {
	SpawnSuccessor func(pred string, s bd.Successor) (string, bool, error)
	CutBranch      func(name, base string) (bool, error)
	BuildChildren  func(checkpoint string) (int, error) // Builds open or closed
	SpawnBuild     func(parent string, s bd.Successor) (string, bool, error)
	InitiativeOf   func(bd.Bead) (string, error)
	SubtreeIDs     func(id string) ([]string, error)
	CausedBy       func(id string, want contract.Kind) (string, bool, error)
}

// RenumberPred is the Chart or Checkpoint id caused-by points at: found only for a
// renumber Checkpoint, so it is the cheap "is this one?" test.
func RenumberPred(id string, causedBy func(string, contract.Kind) (string, bool, error)) (pred string, kind contract.Kind, found bool, err error) {
	for _, k := range []contract.Kind{contract.KindChart, contract.KindCheckpoint} {
		if pred, found, err = causedBy(id, k); err != nil || found {
			return pred, k, found, err
		}
	}
	return "", "", false, nil
}

// RenumberScope is the bead ids a renumber of container's numbers sweeps (ADR 0225 §5):
// container's own bead and its subtree, plus, while it is a renumber Checkpoint, the subtree of its
// caused-by predecessor, walked to the first container that is not one. A renumber
// Checkpoint is a kind:checkpoint caused-by a Chart or Checkpoint (an ordinary one is
// caused-by a Spec, a Rollback successor by a Rollback), so no marker is needed.
func RenumberScope(container bd.Bead, subtree func(string) ([]string, error), causedBy func(string, contract.Kind) (string, bool, error)) (ids []string, widened bool, err error) {
	sub, err := subtree(container.ID)
	if err != nil {
		return nil, false, err
	}
	out := append([]string{container.ID}, sub...)
	for id, kind, seen := container.ID, container.Kind, map[string]bool{}; kind == contract.KindCheckpoint && !seen[id]; {
		seen[id] = true
		pred, predKind, found, err := RenumberPred(id, causedBy)
		if err != nil || !found {
			return out, widened, err
		}
		kind = predKind
		sub, err := subtree(pred)
		if err != nil {
			return nil, false, err
		}
		widened = true
		out, id = append(append(out, pred), sub...), pred
	}
	return out, widened, nil
}

// MintRenumber files the Spec-less renumber Checkpoint for landed, a container whose
// squash commit sha added numbered files that clashes show it is the later side of
// (ADR 0225 §2-3): one Checkpoint caused-by landed carrying every clash, its branch cut
// at the fetched Forge main, and exactly one Build to renumber and sweep. Idempotent
// at every leg, so a pass that crashed anywhere resumes with no duplicate. Returns the
// Checkpoint's id.
func MintRenumber(landed bd.Bead, sha string, clashes []git.PostLandClash, s RenumberSeams) (string, error) {
	if len(clashes) == 0 {
		return "", nil
	}
	parent, err := s.InitiativeOf(landed)
	if err != nil {
		return "", err
	}
	added := make([]string, len(clashes))
	for i, c := range clashes {
		added[i] = c.Added
	}
	slices.Sort(added)
	cp, _, err := s.SpawnSuccessor(landed.ID, bd.Successor{
		Kind:        contract.KindCheckpoint,
		Container:   true,
		Title:       "Renumber " + added[0] + " from " + landed.ID,
		Description: renumberBody(landed.ID, sha, clashes),
		Parent:      parent,
		Context:     bd.ContextOf(landed),
		Repo:        bd.RepoStampOf(landed),
	})
	if err != nil {
		return cp, err
	}
	if _, err := s.CutBranch(git.BranchName(string(contract.KindCheckpoint), cp), clashes[0].Mainline); err != nil {
		return cp, err
	}
	// Any Build, open or closed: Franky may finish the Build before a crashed pass resumes.
	if n, err := s.BuildChildren(cp); err != nil || n > 0 {
		return cp, err
	}
	scope, _, err := RenumberScope(landed, s.SubtreeIDs, s.CausedBy)
	if err != nil {
		return cp, err
	}
	_, _, err = s.SpawnBuild(cp, bd.Successor{
		Kind:        contract.KindBuild,
		Title:       "Renumber " + added[0] + " and sweep its references",
		Description: renumberBuildBody(landed.ID, sha, clashes, scope),
	})
	return cp, err
}

// renumberBody is the renumber Checkpoint's body: every clashing path against the
// file(s) it clashes with, and the landed sha. It carries no Spec: line.
func renumberBody(landedID, sha string, clashes []git.PostLandClash) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Renumber Checkpoint for %s, landed at %s (ADR 0225 (mugicrew-a-forge-merged…)). Its added numbered file(s) clash with earlier ones on mainline:\n", landedID, sha)
	for _, c := range clashes {
		others := make([]string, len(c.Others))
		for i, o := range c.Others {
			others[i] = o
			if i < len(c.LandedBy) && c.LandedBy[i] != "" {
				others[i] += " (landed by " + c.LandedBy[i] + ")"
			}
		}
		fmt.Fprintf(&b, "- %s clashes with %s\n", c.Added, strings.Join(others, ", "))
	}
	return b.String()
}

// renumberBuildBody is the Build's brief: the number-clash wording (crew doc
// sequence-renumber.md, already-landed case), the clashing paths, the file sweep
// source and the bead sweep scope.
func renumberBuildBody(landedID, sha string, clashes []git.PostLandClash, scope []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Number clash: %s landed numbered file(s) whose number mainline already holds, and the landed side moves. "+
		"Read crew doc sequence-renumber.md (the already-landed case): renumber each file below to the next free number on current mainline and fix the references that mean it.\n\n", landedID)
	for _, c := range clashes {
		fmt.Fprintf(&b, "Clashing path: %s (clashes with %s)\n", c.Added, strings.Join(c.Others, ", "))
	}
	fmt.Fprintf(&b, "\nFile sweep: the files %s changed (git diff --name-only %s^1 %s).\n", sha, sha, sha)
	fmt.Fprintf(&b, "Bead sweep: %s.\n", strings.Join(scope, ", "))
	return b.String()
}
