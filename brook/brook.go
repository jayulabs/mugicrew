// Package brook holds Brook, the crewmate whose domain is the external world:
// everything outside the bead graph and the code (ADR 0115). He owns the one
// kind `task`, a free-standing HITL bead whose body is a numbered ## Steps list
// (ADR 0116). Task is HITL, so the loop never dispatches it: Launch composes the
// in-session guide a human starts with l/b (ADR 0114), and Run is a loud error.
// Brook is pure-prompt: Go never reads the Task body; the guide does. The
// session runs at RepoRoot (route's default): no worktree, commits or integrate.
package brook

import (
	_ "embed"
	"fmt"

	"jayulabs.org/mugicrew/contract"
)

// Brook is the concrete external-world crewmate. It carries no state; the
// harness arrives on the ctx (spec §4), mirroring Sanji.
type Brook struct{}

// Brook satisfies the crewmate contract.
var _ contract.Crewmate = Brook{}

// Run is the AFK path, never reached: a Task runs only when a human is there to
// do real-world steps (Spec FR19), so it is opened via Launch.
func (Brook) Run(_ contract.Ctx, bead contract.Bead) error {
	return fmt.Errorf("brook.Run: kind %q is HITL (task), not a loop kind — open it via Launch", bead.Kind)
}

// Launch composes the one HITL command a human opens for a Task: an
// AgentCommand wrapping taskPrompt. A non-task kind is a loud error, as in Franky.
func (Brook) Launch(ctx contract.Ctx, bead contract.Bead) (contract.LaunchCommand, error) {
	if bead.Kind != contract.KindTask {
		return nil, fmt.Errorf("brook.Launch: kind %q is not a launchable HITL kind", bead.Kind)
	}
	inv, err := contract.NewInvocation(bead.ID, taskPrompt(bead.ID))
	if err != nil {
		return nil, err
	}
	return ctx.Harness().LaunchCommand(inv)
}

//go:embed prompts/task.md
var taskTmpl string

// taskPrompt is the guide's procedure over one Task (Spec D3): resume
// by re-checking, walk each step by its markers, record value-free passes,
// amend on approval, exit stuck or close only after a final all-checks pass.
// Pure — asserted through the Launch seam test.
func taskPrompt(taskID string) string {
	return fmt.Sprintf(taskTmpl, taskID, contract.DraftInstruction(taskID))
}
