package session

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/git"
)

// BackstopEnv marks the backstop's own `mugicrew integrate` child, so the verb
// records by: backstop.
const BackstopEnv = "MUGICREW_BACKSTOP"

// Backstop is the Go integrate run after an effort on effort/<id> exits, with no
// agent (Spec D6, ADR 0102 §6): it catches a session that exited without
// integrating. It owns wt's teardown and maps the verb's exit:
//   - ExitIntegrated: the work is on the container; teardown, then delete
//     effort/<id>.
//   - ExitConflict (rebase, held, landed): abort the rebase, pause
//     integrate-conflict, reopen b if its tail already closed it, keep the branch
//     and the tree for a human, log effort.paused at Warn. Reports stranded. Any
//     exit over a rebase stopped in the tree maps here too.
//   - anything else (transient): teardown, keep the branch, and reopen b if its
//     tail already closed it, so the next tick re-dispatches it and retries.
//
// A failed effort skips the integrate (half-done work is never squashed): teardown,
// keep the branch. So does a swept one, closed out from under it by a confirm
// sweep (FR 19, FR 49), and it is never reopened or paused: its container is gone.
// A sweep that lands while the integrate runs is honoured the same way.
//
// kept says effort/<id> outlives the backstop: its bead must stay open (a closed
// bead is never re-dispatched, and its live ref holds the container undrained).
// attrs ride on effort.paused and the branch events.
func Backstop(b bd.Bead, wt string, failed bool, teardown func(), out io.Writer, attrs ...any) (stranded, kept bool) {
	branch := git.EffortBranch(b.ID)
	attrs = append(slices.Clip(attrs), "branch", branch)
	if failed || swept(b.ID) {
		teardown()
		return false, true
	}
	code := integrateIn(wt, out)
	// A held or landed refusal left no rebase to abort. One stopped in the tree
	// under a transient exit (an agent's, on a detached HEAD the verb can't read)
	// is a conflict too: tearing it down would drop that work mid-rebase.
	aborted := code != ExitIntegrated && abortRebase(wt) == nil
	switch {
	case code != ExitIntegrated && swept(b.ID):
		teardown()
		return false, true
	case code == ExitIntegrated:
		teardown()
		eventlog.RecordBranches([]bd.BranchOutcome{{Branch: branch, Err: deleteBranch(branch)}}, attrs...)
		return false, false
	case code == ExitConflict || aborted:
		err := errors.Join(pauseBead(b.ID, bd.PausedIntegrateConflict), reopenIfClosed(b.ID))
		attrs = append(attrs, "cause", bd.PausedIntegrateConflict, "tree", wt)
		if err != nil {
			attrs = append(attrs, "error", err.Error())
		}
		slog.Warn("effort.paused", attrs...)
		return true, true
	default:
		teardown()
		// ponytail: a failed reopen strands the bead as before; integrate.failed is its trace
		_ = reopenIfClosed(b.ID)
		return false, true
	}
}

// swept reports whether a confirm sweep closed id out: id is closed and it or its
// container carries the sweep's obsolete: close reason. Any other close of both
// (an empty Surgery's abandoned close-out) is no sweep, so the integrate runs.
// ponytail: a failed read reads as not swept, so the integrate runs as before
// ponytail: a container a human closed or landed by hand with an obsolete: reason
// reads as swept too, so its held or landed refusal goes unrecorded and unpaused
// (no integrate.conflict{reason: landed}). Reachable only off the drain gates;
// key the sweep on the confirm's own mark if it bites.
func swept(id string) bool {
	b, err := showBead(id)
	if err != nil || b.Status != "closed" || b.Parent == "" {
		return err == nil && obsolete(b)
	}
	if obsolete(b) {
		return true
	}
	cp, err := showBead(b.Parent)
	return err == nil && obsolete(cp)
}

func obsolete(b bd.Bead) bool {
	return b.Status == "closed" && strings.HasPrefix(b.CloseReason, bd.ObsoleteReason)
}

// reopenIfClosed reopens id if its tail already closed it.
func reopenIfClosed(id string) error {
	if cur, err := showBead(id); err == nil && cur.Status == "closed" {
		return reopenBead(id)
	}
	return nil
}

// integrateIn runs `mugicrew integrate` as the backstop with wt as its cwd (git
// is dir-less: the tree is the caller's cwd, ADR 0010) and returns its exit code;
// a child that never ran reads as transient. Backstop's other seams are bd's and
// git's (showBead is hitl-run's). Vars, so the outcome mapping is testable without a repo or a bd.
var (
	integrateIn = func(wt string, out io.Writer) int {
		self, err := os.Executable()
		if err != nil {
			return ExitTransient
		}
		cmd := exec.Command(self, "integrate")
		cmd.Dir = wt
		cmd.Env = append(os.Environ(), BackstopEnv+"=1")
		cmd.Stdout, cmd.Stderr = out, out
		var exit *exec.ExitError
		if err := cmd.Run(); errors.As(err, &exit) {
			return exit.ExitCode()
		} else if err != nil {
			return ExitTransient
		}
		return ExitIntegrated
	}
	abortRebase  = git.AbortRebase
	pauseBead    = bd.Pause
	reopenBead   = bd.Reopen
	deleteBranch = git.DeleteBranch
)
