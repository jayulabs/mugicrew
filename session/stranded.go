package session

import (
	"errors"
	"fmt"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/route"
)

// strandedInfo is a landed-refused child's offshoot Signal (Spec D4) and the landing
// facts the effort.stranded event carries.
type strandedInfo struct {
	Title, Body, Container, Landed, Range string
}

// landedReads are the git reads the facts take, so strandedSignal is testable
// without a repo (as nami.FlagStalePin's are).
type landedReads struct {
	landedBase func(trailer string) (string, error)
	head       func(ref string) (string, error)
	landedTip  func(sha string) (string, bool)
}

// strandedSignal derives the Signal for child b routed to d (Spec D3): the landing sha
// is the container's mainline trailer, never its branch. The squash range runs from the
// container ref now (the landed squash's Tip: when the ref is gone), the replace range
// from b's Base: pin. dir names a nested child's repo, "" otherwise.
func strandedSignal(b bd.Bead, d route.Destination, dir string, r landedReads) (strandedInfo, error) {
	kind, id := git.ParseBranchName(d.Container)
	landed, err := r.landedBase(git.TrailerFor(kind, id))
	if err != nil {
		return strandedInfo{}, err
	}
	branch := git.EffortBranch(b.ID)
	var base string
	if d.Mode == route.Replace {
		var ok bool
		if base, ok = git.ParseBase(b.Description); !ok {
			return strandedInfo{}, fmt.Errorf("stranded: %s carries no Base: pin", b.ID)
		}
	} else {
		base, err = r.head("refs/heads/" + d.Container)
		if git.MissingRef(err) {
			var ok bool
			if base, ok = r.landedTip(landed); !ok {
				return strandedInfo{}, fmt.Errorf("stranded: %s is gone and the landed squash %s carries no Tip:", d.Container, landed)
			}
		} else if err != nil {
			return strandedInfo{}, err
		}
	}
	rng := base + ".." + branch
	body := fmt.Sprintf("%s (%s) was open when %s landed at %s; its integrate was refused.\n", b.ID, b.Title, id, landed)
	if dir != "" {
		body += "Repo: " + dir + "\n"
	}
	body += fmt.Sprintf("Landed: %s\nBranch: %s\nRange: %s\nFix: these commits are not on mainline. Land them as new work.\n", landed, branch, rng)
	return strandedInfo{Title: b.Title + ": commits stranded after " + id + " landed", Body: body, Container: id, Landed: landed, Range: rng}, nil
}

// carryStranded files b's stranded-commits Signal and closes b out of the way, marked
// so its landed container still drains (Spec D2). Any failure leaves b for the pause.
func carryStranded(b bd.Bead, kid *Child) (signal string, info strandedInfo, err error) {
	if info, err = landedFacts(b, kid); err != nil {
		return "", info, err
	}
	if signal, _, err = fileStranded(b.ID, info.Title, info.Body); err != nil {
		return "", info, err
	}
	// bd close on a closed bead keeps its old reason, so reopen it for the marker to land.
	if err = reopenIfClosed(b.ID); err != nil {
		return "", info, err
	}
	return signal, info, closeBead(b.ID, bd.ContainerLandedReason+"; carried by "+signal)
}

// landedFactsFor is landedFacts over the real repos: kid's when nested, else the cwd's.
func landedFactsFor(b bd.Bead, kid *Child) (strandedInfo, error) {
	d, err := route.Route(b)
	if err == nil && d.Container == "" {
		err = errors.New("integrates onto no container")
	}
	if err != nil {
		return strandedInfo{}, fmt.Errorf("stranded: %s: %w", b.ID, err)
	}
	if kid == nil {
		return strandedSignal(b, d, "", landedReads{git.LandedBase, git.Head, git.LandedTip})
	}
	return strandedSignal(b, d, kid.Dir, landedReads{kid.Main.LandedBase, kid.Main.Head, kid.Main.LandedTip})
}
