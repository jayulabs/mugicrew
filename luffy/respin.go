package luffy

import (
	"fmt"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/nami"
)

// The reads a respin's Packet inputs make, as seams so the render tests stay off a real bd and git.
var (
	listChildren = bd.ListChildren
	// isRespin reads D10's edge: the Checkpoint is caused-by a kind:remnant.
	isRespin = func(checkpoint string) (bool, error) {
		_, found, err := bd.CausedBy(checkpoint, contract.KindRemnant)
		return found, err
	}
	seedOf = func(checkpoint string) (string, error) {
		return git.Repo("").NewestByTrailerKey(git.BranchName(string(contract.KindCheckpoint), checkpoint), nami.SeedTrailerKey)
	}
)

// respinSection is the Packet inputs of a respin Checkpoint (ADR 0407 §5): its
// Landed-tip:, the seed commit and the re-filed members, with the instruction to apply
// approval-body.md's "A respin's Packet". "" for any other Checkpoint.
func respinSection(cp bd.Bead) (string, error) {
	if ok, err := isRespin(cp.ID); err != nil || !ok {
		return "", err
	}
	tip := bd.ParseRemnantBody(cp.Description).LandedTip()
	if tip == "" {
		tip = "unknown"
	}
	seed, err := seedOf(cp.ID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(seed) == "" {
		seed = "none (no seed: nothing was replayed, or the replay conflicted; read the replay Conflict member's close if one exists)"
	}
	kids, err := listChildren(cp.ID)
	if err != nil {
		return "", err
	}
	var members []string
	for _, k := range kids {
		if k.Kind == contract.KindBuild || k.Kind == contract.KindCritique {
			members = append(members, fmt.Sprintf("  - %s (%s)", k.Title, k.Kind))
		}
	}
	return fmt.Sprintf("\n\nThis Checkpoint is a respin: its body starts \"Respin of\". Apply the \"A respin's Packet\" paragraph of crew doc approval-body.md to every section it names. Its inputs:\n\n- Landed-tip: %s\n- seed commit (tag its diff rows replayed): %s\n- re-filed members (tag their rows respin):\n%s", strings.TrimSpace(tip), strings.TrimSpace(seed), strings.Join(members, "\n")), nil
}

// withRespin appends the parent Checkpoint's respin section to the prompt.
func withRespin(prompt string, parent bd.Bead) (string, error) {
	sec, err := respinSection(parent)
	return prompt + sec, err
}
