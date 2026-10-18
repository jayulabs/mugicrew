// Package robin holds Robin, the crewmate who explains the Workspace read-only
// and drafts Signals from what a conversation finds (ADR 0193, ADR 0194). She
// owns the one kind `discussion`, a free-standing HITL bead: the loop never
// dispatches it, a human launches it, and Run is a loud error. Her session runs
// at RepoRoot (route's default): no worktree, commits or integrate. Launch marks
// the invocation read-only from the registry row, the one source of truth
// launch and resume both read (ADR 0191).
package robin

import (
	_ "embed"
	"fmt"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/registry"
)

// Robin is the concrete discussion crewmate. It carries no state; the harness
// arrives on the ctx, mirroring Brook.
type Robin struct{}

// Robin satisfies the crewmate contract.
var _ contract.Crewmate = Robin{}

// Run is the AFK path, never reached: a discussion needs a human in the room.
func (Robin) Run(_ contract.Ctx, bead contract.Bead) error {
	return fmt.Errorf("robin.Run: kind %q is HITL (discussion), not a loop kind — open it via Launch", bead.Kind)
}

// Launch composes the one HITL command a human opens for a discussion. A
// non-discussion kind is a loud error, as in Brook.
func (Robin) Launch(ctx contract.Ctx, bead contract.Bead) (contract.LaunchCommand, error) {
	if bead.Kind != contract.KindDiscussion {
		return nil, fmt.Errorf("robin.Launch: kind %q is not a launchable HITL kind", bead.Kind)
	}
	e, err := registry.Lookup(bead.Kind)
	if err != nil {
		return nil, err
	}
	prompt, err := launchPrompt(bead.ID)
	if err != nil {
		return nil, err
	}
	inv, err := contract.NewInvocation(bead.ID, prompt)
	if err != nil {
		return nil, err
	}
	inv.ReadOnly = e.ReadOnly
	return ctx.Harness().LaunchCommand(inv)
}

//go:embed prompts/discussion.md
var discussionTmpl string

// discussionPrompt is Robin's procedure over one discussion bead (Spec D11).
// Pure — asserted through the Launch seam test.
func discussionPrompt(id string) string {
	return fmt.Sprintf(discussionTmpl, id, contract.DraftInstruction(id))
}

// retrospectiveTmpl is the retrospective section placed before the
// discussion prompt (ADR 0280). It carries no ids: Robin reads Target: and
// Spine: from her own bead, so prompt size is independent of spine size.
//
//go:embed prompts/retrospective.md
var retrospectiveTmpl string

// showBead is the body-read seam, swapped in tests.
var showBead = bd.Show

// launchPrompt reads the bead body: no Target: line is the plain discussion
// prompt; Target: with a Spine: line is the framing in front of it. Target:
// without a Spine:, or a failed read, is a loud error, never a fallback. A
// quoted "> Target:" never starts a line with the key, so it never matches.
func launchPrompt(id string) (string, error) {
	b, err := showBead(id)
	if err != nil {
		return "", fmt.Errorf("robin.Launch: read %s: %w", id, err)
	}
	if _, ok := git.ParseLine(b.Description, "Target:"); !ok {
		return discussionPrompt(id), nil
	}
	if spine, _ := git.ParseLine(b.Description, "Spine:"); strings.TrimSpace(spine) == "" {
		return "", fmt.Errorf("robin.Launch: retrospective %s has a Target: line but no Spine: line", id)
	}
	return retrospectiveTmpl + "\n" + discussionPrompt(id), nil
}
