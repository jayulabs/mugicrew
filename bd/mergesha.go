package bd

import (
	"fmt"

	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// MergeShaOf is the merge sha a container's trailerless hold Task recorded, or ""
// with the reason none was found (ResolveMergeSha). Offline: it reads only the graph.
func MergeShaOf(containerID string) (sha, why string, err error) {
	landings, err := LandingsOf(containerID)
	if err != nil {
		return "", "", err
	}
	// every blocker, closed ones too: a human closes the hold once the trailer is stamped
	blockers, err := depTargets(containerID, "down", "blocks")
	if err != nil {
		return "", "", err
	}
	sha, why = ResolveMergeSha(landings, blockers)
	return sha, why, nil
}

// ResolveMergeSha is the pure core of MergeShaOf: the newest Landing's recorded MR
// iid, the blocker keyed "Merged MR: !<iid>" in any status, and that Task's "Merge
// sha:" line. "" with a reason when a step finds nothing.
func ResolveMergeSha(landings, blockers []Bead) (sha, why string) {
	l, ok := NewestLanding(landings)
	if !ok {
		return "", "no Landing"
	}
	iid := git.RecordedMR(l.Description)
	if iid == 0 {
		return "", fmt.Sprintf("Landing %s records no MR", l.ID)
	}
	key := fmt.Sprintf("Merged MR: !%d", iid)
	for _, t := range blockers {
		if t.Kind != contract.KindTask || !HasBodyLine(t.Description, key) {
			continue
		}
		if v, ok := ParseMergeShaLine(t.Description); ok {
			return v, ""
		}
		return "", fmt.Sprintf("hold Task %s has no Merge sha: line", t.ID)
	}
	return "", fmt.Sprintf("no hold Task keyed %q", key)
}

// ParseMergeShaLine is the value of a hold Task body's "Merge sha:" line.
func ParseMergeShaLine(body string) (string, bool) {
	v, ok := git.ParseLine(body, "Merge sha:")
	return v, ok && v != ""
}
