// Package brook holds Brook, the crewmate whose domain is the external world:
// everything outside the bead graph and the code (ADR 0115). He owns the one
// kind `task`, a HITL bead (a direct Initiative child when ordered against a
// member, else outside) whose body is a numbered ## Steps list (ADR 0116). Task is
// HITL, so the loop never dispatches it: Launch composes the in-session guide a
// human starts with l/b (ADR 0114), and Run is a loud error. The Task body is
// never read by Go; the guide does. He also owns kind:latch, an
// AFK leaf Run checks in-process with no session: Go reads a latch's Watches:
// line and the Jira status. Both run at RepoRoot (route's default): no worktree,
// commits or integrate.
package brook

import (
	_ "embed"
	"fmt"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/jira"
)

// Seams over the bd/Jira legs of the latch check, swapped by the brook test.
var (
	showBead       = bd.Show
	closeBead      = bd.Close
	hasOpenBlocked = bd.OpenBlocked
	readJira       = jira.View
	readMR         = forge.GetMR
	jiraSite       = func() string {
		root, err := git.RepoRoot()
		if err != nil {
			return ""
		}
		site, _ := cfglayer.JiraSite(root)
		return site
	}
)

// Brook is the concrete external-world crewmate. It carries no state; the
// harness arrives on the ctx (spec §4), mirroring Sanji.
type Brook struct{}

// Brook satisfies the crewmate contract.
var _ contract.Crewmate = Brook{}

// Run is the AFK path the loop dispatches: a latch is checked in-process. A Task
// runs only when a human is there to do real-world steps (Spec FR19), so it is
// opened via Launch and Run on it (or any other kind) is a loud error.
func (Brook) Run(ctx contract.Ctx, bead contract.Bead) error {
	switch bead.Kind {
	case contract.KindLatch:
		return checkLatch(ctx, bead.ID)
	}
	return fmt.Errorf("brook.Run: kind %q is HITL (task), not a loop kind — open it via Launch", bead.Kind)
}

// checkLatch closes the latch orphaned when nothing open waits on it, else closes
// it once what it watches is done: a Jira ticket Done, a GitLab MR merged. Still
// waiting returns nil: the loop's release leaves it open. Errors count toward the
// loop's failure pause; this never pauses.
func checkLatch(ctx contract.Ctx, id string) error {
	b, err := showBead(id)
	if err != nil {
		return err
	}
	system, item, werr := Watches(b.Description)
	open, err := hasOpenBlocked(id)
	if err != nil {
		return err
	}
	if !open {
		if werr != nil {
			return closeLatch(ctx, id, "orphaned: no open bead waits on this latch")
		}
		if system == "mr" {
			// ponytail: a malformed mr value falls through to the check, which errors
			if p, iid, perr := forge.ParseMRCanonical(item); perr == nil {
				return closeLatch(ctx, id, fmt.Sprintf("orphaned: no open bead waits on mr %s!%d", p.Path(), iid))
			}
		}
		return closeLatch(ctx, id, "orphaned: no open bead waits on "+system+" "+item)
	}
	if werr != nil {
		return werr
	}
	if system == "mr" {
		return checkMR(ctx, id, item)
	}
	it, err := readJira(item, jiraSite())
	if err != nil {
		return err
	}
	if !it.Done() {
		return nil
	}
	return closeLatch(ctx, id, fmt.Sprintf("jira %s reached %s (Done)", item, it.Status))
}

// checkMR closes the latch once the merge request named by its canonical
// <host>/<path>!<iid> value is merged; closed unmerged is an error for the pause.
func checkMR(ctx contract.Ctx, id, canonical string) error {
	p, iid, err := forge.ParseMRCanonical(canonical)
	if err != nil {
		return err
	}
	disp := fmt.Sprintf("%s!%d", p.Path(), iid)
	mr, err := readMR(p, iid)
	if err != nil {
		return err
	}
	switch mr.State {
	case forge.StateMerged:
		return closeLatch(ctx, id, "mr "+disp+" merged")
	case forge.StateOpened:
		return nil
	case forge.StateClosed:
		return fmt.Errorf("mr %s is closed without merging; reopen the MR or bd close this latch", disp)
	}
	return fmt.Errorf("mr %s is in unexpected state %q", disp, mr.State)
}

// Watches parses the body's leading "Watches: <system> <item>" line. The system
// is jira or mr; the loop reads it to pick the latch's poll interval.
func Watches(body string) (system, item string, err error) {
	line, _, _ := strings.Cut(body, "\n")
	f := strings.Fields(strings.TrimSpace(line))
	if len(f) != 3 || f[0] != "Watches:" {
		return "", "", fmt.Errorf("latch body must start with 'Watches: <jira|mr> <item>', got %q", line)
	}
	if f[1] != "jira" && f[1] != "mr" {
		return "", "", fmt.Errorf("latch watches unknown system %q", f[1])
	}
	return f[1], f[2], nil
}

// closeLatch hands the close to the effort's publish batch when the ctx carries a
// Publisher, else closes the bead itself.
func closeLatch(ctx contract.Ctx, id, reason string) error {
	if p, ok := ctx.(contract.Publisher); ok {
		p.CloseOnPublish(reason)
		return nil
	}
	return closeBead(id, reason)
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
