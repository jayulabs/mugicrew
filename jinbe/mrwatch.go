package jinbe

import (
	"fmt"
	"slices"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
)

// spawnMRWatch gives a container its MR Watch (Spec D4) when recordMR first records an MR
// for it, copying the Landing's MR: line so the Watch knows the iid between Landings.
// SpawnChildIfAbsent keeps it to one open per container.
func spawnMRWatch(cp bd.Bead, lg legs, iid int) error {
	_, _, err := spawnChild(cp.ID, bd.Successor{
		Kind:        contract.KindMRWatch,
		Title:       fmt.Sprintf("Watch %s", git.MRRef(lg.mrPath, iid)),
		Description: git.MRLine(lg.mrPath, iid) + "\n",
	})
	return err
}

// closeMRWatches closes every open MR Watch of a landed container, before the container
// itself closes.
func closeMRWatches(containerID, reason string) error {
	watches, err := childrenOfKind(containerID, contract.KindMRWatch)
	if err != nil {
		return err
	}
	for _, w := range watches {
		if err := closeIfOpen(w, reason); err != nil {
			return err
		}
	}
	return nil
}

// reconcileMRWatch is the MR Watch's one read of its container's MR (Spec D4). It closes
// when its container has closed or its MR is merged or closed, skips while the container
// has a pending Landing (mirrorMR reads the MR then, once per period), and otherwise runs
// the Forge Review arm then the close-out arm over a still-open MR. A Forge 429 is held and logged once.
func reconcileMRWatch(w bd.Bead) error {
	if !w.Open() {
		return nil
	}
	cp, err := showBead(w.Parent)
	if err != nil {
		return err
	}
	if !cp.Open() {
		return closeIfOpen(w, "container closed")
	}
	if name, _, err := landStrategy(); err != nil || name != git.LandStrategyGitlab {
		return err
	}
	landings, err := landingsOf(cp.ID)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(landings, bd.Bead.Open) {
		return nil
	}
	lg, ok, err := legsFor(cp, true)
	if err != nil || !ok {
		return err
	}
	iid := git.RecordedMR(w.Description)
	if iid == 0 {
		return nil
	}
	mr, err := forgeMR(lg.proj, iid)
	if err == nil {
		switch mr.State {
		case forge.StateMerged:
			return closeIfOpen(w, "obsolete: merge request merged")
		case forge.StateClosed:
			return closeIfOpen(w, "obsolete: merge request closed")
		case forge.StateOpened:
			if err = forgeReviewArm(cp, nil, lg, mr); err == nil {
				err = closeOutArm(cp, lg, mr, git.BranchName(string(cp.Kind), cp.ID), closeOutTarget{head: mr.SHA})
			}
		}
	}
	return holdRateLimited(w, "mrwatch.rate_limited", err, lg.attrs)
}
