package session

import (
	"errors"
	"fmt"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/remnant"
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

// Seams of the Remnant carry.
var (
	remnantOf    = bd.RemnantOf
	appendLines  = bd.AppendLines
	keptLine     = remnant.KeptLiner(func(ref string) (string, error) { return headIn(ref) }) // late-bound, so tests can stub headIn
	landStrategy = git.LandStrategyName
	headIn       = git.Head
)

// carryToRemnant appends b's Kept: and Child: lines to its landed container's Remnant
// and closes b carried by it, keeping effort/<id>. carried is false when the container
// has no open Remnant (a closed one takes no more; a Chart member, a container landed before the Remnant, a nested
// container), leaving b for the per-child Signal.
func carryToRemnant(b bd.Bead, kid *Child) (remnantID string, carried bool, err error) {
	if kid != nil || b.Parent == "" {
		return "", false, nil
	}
	rem, ok, err := remnantOf(b.Parent)
	if err != nil {
		return "", false, err
	}
	if !ok {
		return "", false, remnantPending(b.Parent)
	}
	if !rem.Open() {
		return "", false, nil
	}
	reason := bd.CarriedByReason(rem.ID)
	lines, err := remnant.ChildLines(b, reason, keptLine)
	if err != nil {
		return "", false, err
	}
	if err = appendLines(rem.ID, lines...); err != nil {
		return "", false, err
	}
	if err = reopenIfClosed(b.ID); err != nil {
		return "", false, err
	}
	return rem.ID, true, closeBead(b.ID, reason)
}

// remnantPending is an error when container is an open Checkpoint on gitlab: its Remnant
// is filed by closeLanded, which may not have run yet (a held 429, a paused Checkpoint,
// tick order), so a per-child Signal now would strand the child outside the Remnant. The
// error pauses the child, which keeps it Running so fileRemnant makes it hold the
// Remnant. A Chart member, a closed container or another strategy gets nil.
func remnantPending(container string) error {
	cp, err := showBead(container)
	if err != nil || cp.Kind != contract.KindCheckpoint || !cp.Open() {
		return err
	}
	name, err := landStrategy()
	if err != nil || name != git.LandStrategyGitlab {
		return err
	}
	return fmt.Errorf("%s landed but its Remnant is not filed yet; holding for it: %w", container, errRemnantPending)
}

// errRemnantPending marks the hold remnantPending raises, so the pause can say it is no
// real conflict.
var errRemnantPending = errors.New("remnant pending")

// remnantPendingNote is the comment on a child held for its Remnant: its
// paused:integrate-conflict is a hold, not a conflict.
const remnantPendingNote = "Held, not conflicted: the container landed before its Remnant was filed. " +
	"Resume this child once the Remnant exists and its integrate joins it; nothing else is needed."

// carryStranded files b's stranded-commits Signal and closes b out of the way, marked
// so its landed container still drains (Spec D2). Any failure leaves b for the pause.
// A container with a Remnant takes b onto it instead (remnant set, no Signal).
func carryStranded(b bd.Bead, kid *Child) (signal, remnantID string, info strandedInfo, err error) {
	if remnantID, ok, err := carryToRemnant(b, kid); err != nil || ok {
		return "", remnantID, strandedInfo{Container: b.Parent}, err
	}
	if info, err = landedFacts(b, kid); err != nil {
		return "", "", info, err
	}
	if signal, _, err = fileStranded(b.ID, info.Title, info.Body); err != nil {
		return "", "", info, err
	}
	// bd close on a closed bead keeps its old reason, so reopen it for the marker to land.
	if err = reopenIfClosed(b.ID); err != nil {
		return "", "", info, err
	}
	return signal, "", info, closeBead(b.ID, bd.CarriedByReason(signal))
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
