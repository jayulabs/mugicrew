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
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/route"
)

// BackstopEnv marks the backstop's own `mugicrew integrate` child, so the verb
// records by: backstop.
const BackstopEnv = "MUGICREW_BACKSTOP"

// Backstop is the Go integrate run after an effort on effort/<id> exits, with no
// agent (Spec D6, ADR 0102 §6): it catches a session that exited without
// integrating. It owns wt's teardown and maps the verb's exit:
//   - ExitIntegrated: the work is on the container; teardown, then delete
//     effort/<id>. A closed, unswept Build whose Build: <id> trailer is missing
//     from its Checkpoint (or whose count failed) built nothing: teardown, keep
//     effort/<id>, pause not-built, reopen, log effort.paused at Warn with the
//     agent's "Not built:" note (ADR 0248). Reports stranded and kept. A Build
//     carrying paused:not-built whose count is above zero is unpaused first,
//     logging effort.resumed {cause: not-built}.
//   - ExitLanded: its container already landed, so b's commits can never integrate.
//     File them as an offshoot Signal for intake, close b obsolete: container
//     landed (so the container still drains), then teardown, keep effort/<id> and
//     log effort.stranded at Warn. Not stranded. If the facts, the Signal or the
//     close fail, it falls to the conflict arm below.
//   - ExitNoCutPoint: no cut ref and no fork point after a rewrite; nothing was
//     integrated and a retry cannot help. Pause integrate-conflict naming it
//     (reason no-cut-point), keep the branch and tree for a human.
//   - ExitOffPin: a replace bead refused off its pinned Tip. If the container
//     already descends from its Base: close it moot; else, with no Re-pinned: line
//     yet, re-pin it (Tip: to the container's tip, Re-pinned: the old one), drop
//     effort/<id> and its cut ref and leave it open to re-dispatch; else pause
//     integrate-conflict (reason repeat-off-pin).
//   - ExitConflict (rebase, held): abort the rebase, pause
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
	if failed || Swept(b.ID) {
		teardown()
		return false, true
	}
	// a nested effort's rebase and branch live in the child (Spec FR 25)
	treeDir, del := wt, deleteBranch
	var kid *Child
	if kids := probeChildren(wt); len(kids) == 1 {
		kid = &kids[0]
		treeDir, del = string(kid.Repo), kid.Main.DeleteBranch
	}
	code := integrateIn(wt, out)
	// A held or landed refusal left no rebase to abort. One stopped in the tree
	// under a transient exit (an agent's, on a detached HEAD the verb can't read)
	// is a conflict too: tearing it down would drop that work mid-rebase.
	aborted := code != ExitIntegrated && abortRebase(treeDir) == nil
	switch {
	case code != ExitIntegrated && Swept(b.ID):
		teardown()
		return false, true
	case code == ExitIntegrated:
		// counted before the teardown: the host's count runs in the tree
		var missing, built bool
		var cerr error
		if b.Kind == contract.KindBuild {
			missing, built, cerr = notBuilt(b, kid, treeDir)
		}
		teardown()
		if missing {
			return pauseNotBuilt(b, attrs, cerr)
		}
		// a retry that integrated lifts the pause; a clean return alone never does (ADR 0104)
		if built && bd.PausedBy(b, bd.PausedNotBuilt) {
			if err := unpause(b.ID, bd.PausedNotBuilt); err != nil {
				slog.Warn("effort.resume_failed", append(attrs, "cause", bd.PausedNotBuilt, "error", err.Error())...)
			} else {
				slog.Info("effort.resumed", append(eventlog.BeadAttrs(b), "cause", bd.PausedNotBuilt)...)
			}
		}
		eventlog.RecordBranches([]bd.BranchOutcome{{Branch: branch, Err: del(branch)}}, attrs...)
		return false, false
	case code == ExitLanded:
		signal, info, err := carryStranded(b, kid)
		if err == nil {
			teardown()
			slog.Warn("effort.stranded", append(attrs, "signal", signal, "container", info.Container, "landed", info.Landed, "range", info.Range)...)
			return false, true
		}
		return pauseConflict(b, wt, attrs, err)
	case code == ExitNoCutPoint:
		return pauseConflict(b, wt, append(attrs, "reason", "no-cut-point"), nil)
	case code == ExitOffPin:
		repo := git.Repo(treeDir)
		if kid != nil {
			repo = kid.Main
		}
		return offPin(b, wt, repo, teardown, del, branch, attrs)
	case code == ExitConflict || aborted:
		return pauseConflict(b, wt, attrs, nil)
	default:
		teardown()
		// ponytail: a failed reopen strands the bead as before; integrate.failed is its trace
		_ = reopenIfClosed(b.ID)
		return false, true
	}
}

// offPin settles a replace bead whose integrate was refused off its pinned Tip
// (Spec FR 33–35). kept is true on every settled path: a moot close is done, a
// re-pinned bead must stay open, and either way the handed close must not run.
func offPin(b bd.Bead, wt string, repo git.Repo, teardown func(), del func(string) error, branch string, attrs []any) (stranded, kept bool) {
	pause := func(err error) (bool, bool) {
		return pauseConflict(b, wt, append(attrs, "reason", "repeat-off-pin"), err)
	}
	cur, err := showBead(b.ID)
	if err != nil {
		return pause(err)
	}
	d, err := routeBead(cur)
	if err != nil {
		return pause(err)
	}
	tip, err := containerTip(repo, d.Container)
	if err != nil {
		return pause(err)
	}
	finish := func() {
		teardown()
		eventlog.RecordBranches([]bd.BranchOutcome{{Branch: branch, Err: del(branch)}}, attrs...)
	}
	if base, ok := git.ParseBase(cur.Description); ok && isAncestor(repo, base, tip) {
		if err := closeBead(b.ID, "moot: container already on base "+base); err != nil {
			return pause(err)
		}
		finish()
		return false, true
	}
	if _, ok := git.ParseTip(cur.Description); !ok {
		return pauseConflict(b, wt, append(attrs, "reason", "no-pinned-tip"), nil) // nothing to re-pin
	}
	body, ok := repin(cur.Description, tip)
	if !ok {
		return pause(nil) // already re-pinned once: a human decides
	}
	if err := setDescription(b.ID, body); err != nil {
		return pause(err)
	}
	finish()
	// ponytail: a failed reopen strands the bead; effort.paused is not logged for it
	_ = reopenIfClosed(b.ID)
	return false, true
}

// repin is body with its first Tip: line replaced by tip and a Re-pinned: line
// (the old tip) appended; ok is false when it was already re-pinned (offPin has checked for a Tip: already).
func repin(body, tip string) (string, bool) {
	if _, done := git.ParseLine(body, "Re-pinned:"); done {
		return "", false
	}
	old, ok := git.ParseTip(body)
	if !ok {
		return "", false
	}
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "Tip:") {
			lines[i] = git.TipPin(tip)
			break
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n" + "Re-pinned: " + old, true
}

// notBuilt reports whether b, a Build the integrate just exited 0 for, never
// reached its Checkpoint: it re-reads closed (a pivoted Build is still open), is
// not Swept (a sweep may have landed during the integrate) and no Build: <id>
// trailer is on mainline..checkpoint/<parent>, or counting it failed. built says
// the count was above zero, so a paused:not-built b is lifted. The count
// runs in the repo the integrate squashed into, not contexts.RepoOf (ADR 0166).
func notBuilt(b bd.Bead, kid *Child, treeDir string) (missing, built bool, err error) {
	if cur, err := showBead(b.ID); err != nil || cur.Status != "closed" || Swept(b.ID) {
		return false, false, nil
	}
	repo := git.Repo(treeDir)
	if kid != nil {
		repo = kid.Main
	}
	n, err := trailerCount(repo, b.Parent, git.TrailerFor(string(b.Kind), b.ID))
	return err != nil || n == 0, err == nil && n > 0, err
}

// pauseNotBuilt pauses b not-built and reopens it, keeping effort/<id> so its live
// ref holds the Checkpoint undrained and a re-run rolls forward (ADR 0248). The
// tree is already torn down.
func pauseNotBuilt(b bd.Bead, attrs []any, cause error) (stranded, kept bool) {
	attrs = append(attrs, "cause", bd.PausedNotBuilt)
	if note := notBuiltNote(b.ID); note != "" {
		attrs = append(attrs, "note", note)
	}
	if err := errors.Join(cause, pauseBead(b.ID, bd.PausedNotBuilt), reopenBead(b.ID)); err != nil {
		attrs = append(attrs, "error", err.Error())
	}
	slog.Warn("effort.paused", attrs...)
	return true, true
}

// pauseConflict pauses b integrate-conflict, reopens it if its tail closed it and
// keeps the branch and tree for a human; cause, if any, is the landed arm's failure.
func pauseConflict(b bd.Bead, wt string, attrs []any, cause error) (stranded, kept bool) {
	err := errors.Join(cause, pauseBead(b.ID, bd.PausedIntegrateConflict), reopenIfClosed(b.ID))
	attrs = append(attrs, "cause", bd.PausedIntegrateConflict, "tree", wt)
	if err != nil {
		attrs = append(attrs, "error", err.Error())
	}
	slog.Warn("effort.paused", attrs...)
	return true, true
}

// Swept reports whether a confirm sweep closed id out: id is closed and it or its
// container carries the sweep's obsolete: close reason. Any other close of both
// (an empty Surgery's abandoned close-out) is no sweep, so the integrate runs.
// Dispatch's publish tail asks it too, for every placement (ADR 0124).
// ponytail: a failed read reads as not swept, so the integrate (and publish) runs as before
// ponytail: a container a human closed or landed by hand with an obsolete: reason
// reads as swept too, so its held or landed refusal goes unrecorded and unpaused
// (no integrate.conflict{reason: landed}). Reachable only off the drain gates;
// key the sweep on the confirm's own mark if it bites.
func Swept(id string) bool {
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
	// trailerCount counts trailer on mainline..checkpoint/<parent> in repo; notBuiltNote
	// is bd's read of the agent's reason.
	trailerCount = func(repo git.Repo, parent, trailer string) (int, error) {
		return repo.CommitsByTrailer(repo.Mainline(), git.BranchName("checkpoint", parent), trailer)
	}
	notBuiltNote   = bd.NotBuiltNote
	probeChildren  = ProbeChildren
	abortRebase    = git.AbortRebase
	pauseBead      = bd.Pause
	landedFacts    = landedFactsFor
	fileStranded   = bd.SpawnOffshootSignalTitledIfAbsent
	closeBead      = bd.Close
	reopenBead     = bd.Reopen
	deleteBranch   = git.DeleteBranch
	routeBead      = route.Route
	isAncestor     = git.Repo.IsAncestor
	containerTip   = func(r git.Repo, container string) (string, error) { return r.Head("refs/heads/" + container) }
	setDescription = func(id, body string) error { return bd.Update(id, []string{"--description", body}) }
)
