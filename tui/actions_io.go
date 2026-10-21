package tui

// The press-time I/O behind Graph's launch, drop, abort, recover and run-now keys
// (FR4): pure of the Model, each runs inside a tea.Cmd, never in
// render, and goes through the shared session/dispatch/chopper core (FR59).

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/agentprofile"
	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/dispatch"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/loop"
	"jayulabs.org/mugicrew/registry"
	"jayulabs.org/mugicrew/route"
	"jayulabs.org/mugicrew/session"
)

// launchFor returns the command the human runs to launch a bead (spec §9.3).
// Every HITL kind — a Signal included (ADR 0039 §6; the `signal triage` verb is
// CLI-only) — resolves to its registry owner and returns that crewmate's Launch command
// directly, so an agent launch carries the harness's pre-assigned
// session id; the owner-from-kind seam keeps the command with the kind's owner,
// not here.
func launchFor(ctx contract.Ctx, crew map[string]contract.Crewmate, b bd.Bead) (contract.LaunchCommand, error) {
	owner, err := launchOwner(crew, b.Kind)
	if err != nil {
		return nil, err
	}
	return crew[owner].Launch(contract.WithClosingLine(ctx), contract.Bead{ID: b.ID, Kind: b.Kind})
}

// dropRun is drop's I/O: it returns the status and whether to reload.
func dropRun(id, sess string, b *bd.Bead) (string, bool) {
	fresh, err := readSessions()
	if err != nil {
		return "drop: " + err.Error(), false
	}
	if b == nil {
		return "drop cancelled: " + id + " is no longer listed", true
	}
	s, has := fresh[id]
	if !has || s.Session != sess {
		return "drop cancelled: " + id + "'s session changed, look again", true
	}
	err = dropSession(*b, s)
	eventlog.RecordHitlDropped(*b, s.Session, string(s.State), err)
	if err != nil {
		return "drop: " + err.Error(), true
	}
	status := "dropped " + id + "'s session"
	// Release the launch's claim back to open (Spec FR12); a Surgery stays
	// in_progress, idle, since its filing holds the claim.
	// A discussion closes as dropped instead (ADR 0194 §4).
	if b.Kind == contract.KindDiscussion {
		if err := closeBead(id, "dropped"); err != nil {
			status += "; close: " + err.Error()
		}
	} else if b.Kind == contract.KindCheckup {
		if err := closeCheckup(*b); err != nil {
			status += "; close: " + err.Error()
		}
	} else if b.Status == "in_progress" && b.Kind != contract.KindSurgery {
		if err := hitlUnclaim(id); err != nil {
			status += "; unclaim: " + err.Error()
		}
	}
	return status, true
}

// launchFailed is a failed launch's status; a Surgery's names it idle, since l
// retries it (Spec FR3).
func launchFailed(b bd.Bead, err error) string {
	if b.Kind == contract.KindSurgery {
		return "launch: " + err.Error() + " — Surgery " + b.ID + " idle, l retries"
	}
	return "launch: " + err.Error()
}

// afkHolder is the live AFK effort whose tree is on branch, "" if none: the loop's
// tree is a mugicrew-wt-* worktree with no session, yet no orphan. It reads a fresh
// bead list, so an effort claimed since the last refresh still counts.
func afkHolder(branch string) (string, error) {
	live, err := liveAFK()
	if err != nil {
		return "", err
	}
	for _, b := range live {
		if d, err := route.Route(b); err == nil && d.Branch == branch {
			return b.ID, nil
		}
	}
	return "", nil
}

// liveAFK is loop's shared live-AFK reader over the TUI's list and probe seams:
// afkHolder's set, and run-now's in-flight set for Dispatch's own-contention test
// (ADR 0054).
func liveAFK() ([]bd.Bead, error) { return loop.LiveAFK(listAll, probeLiveness)() }

// runTailLines is how many trailing lines of a failed effort's captured output
// the status surfaces for diagnosis (Spec FR12) — enough for a stack/error
// context, not the whole run. ponytail: fixed; a scrollable log pane is OOS (Q5).
const runTailLines = 12

// claimRunNow is t's background half: the press-time eligibility re-check, the
// crewmate lookup, the AFK pool check, dispatch's profile gate (an unrunnable
// Agent Profile pauses b paused:profile, a runnable one lifts it, ADR 0123 §3)
// and the claim, in that order. It returns the crewmate and the minted Inflight
// once b is claimed, else a nil crewmate and the refusal or error status.
func claimRunNow(ctx contract.Ctx, b bd.Bead, crews map[string]contract.Crewmate, force bool) (string, contract.Crewmate, dispatch.Inflight) {
	fail := func(err error) (string, contract.Crewmate, dispatch.Inflight) {
		return "run-now: " + err.Error(), nil, dispatch.Inflight{}
	}
	refuse := func(s string) (string, contract.Crewmate, dispatch.Inflight) { return s, nil, dispatch.Inflight{} }
	// Press-time eligibility is the authoritative gate: bd ready is an I/O read
	// re-checked now, so a one-refresh-stale row can't run a since-drained bead.
	ready, err := readyFrontier()
	if err != nil {
		return fail(err)
	}
	// Widened as the loop's is (Spec D6): bd ready hides a Task-held Checkpoint.
	open, err := openBeads()
	if err != nil {
		return fail(err)
	}
	ready = loop.Readmit(ready, open)
	if force && !inFrontier(b.ID, ready) { // the human confirmed running it ahead of its open blockers (ADR 0142 §7)
		ready = append(slices.Clip(ready), b)
	}
	// The pause labels come from this fresh read too: a manual pause added, or a
	// foreign-hold lifted, since the last refresh is honoured now.
	if i := slices.IndexFunc(ready, func(r bd.Bead) bool { return r.ID == b.ID }); i >= 0 {
		b.Labels = ready[i].Labels
	}
	inProgress, err := inProgressBeads()
	if err != nil {
		return fail(err)
	}
	// Inherited pause off the loop's own reads — the paused list plus in-progress
	// rows (ADR 0081 §7) — through the loop's own check, for parity with candidates().
	paused, err := pausedBeads()
	if err != nil {
		return fail(err)
	}
	// A hop outside those reads goes to bd show, as the loop's does (Spec F:
	// parity); a failed show refuses the press, never a silent "not paused".
	anc, err := loop.InheritedPause(ready, slices.Concat(paused, inProgress), showBead)(b)
	if err != nil {
		return fail(err)
	}
	ok, held, err := runNowEligible(b, ready, anc)
	if err != nil {
		return fail(err)
	}
	if !ok {
		return refuse(runNowHint(b, ready, anc, held))
	}
	e, err := registry.Lookup(b.Kind)
	if err != nil {
		return fail(err)
	}
	crew, ok := crews[e.Owner]
	if !ok {
		return refuse(fmt.Sprintf("run-now: no crewmate %q wired for %s", e.Owner, b.ID))
	}
	// Re-derive the AFK pool's in-flight total from bd and honor the pool BEFORE
	// claiming (Spec FR9). The loop and TUI are separate processes coordinating
	// only through bd, so the trigger reuses the loop's own CountInflight and Pool —
	// the same sources the loop reads, never re-spelled — counting every owner's AFK
	// against the layered pool size (local override, then the Workspace's config.toml,
	// then 3; ADR 0110), re-read on every press.
	// ponytail: this is a check-then-claim across two processes, NOT atomic — bd has no
	// claim-if-under-pool primitive, so loop and TUI reading the same count on one tick
	// can overshoot the pool by one. Accepted: the land lock is the safety rule (ADR 0102), the
	// pool only a throughput/cost budget the next tick re-reads (Spec FR34); NO
	// cross-process lock is added (Spec FR22, ADR 0070 §4).
	root, _ := git.RepoRoot() // unresolved ⇒ "" ⇒ the layers read relative to cwd, never a refusal
	if n, pool := loop.CountInflight(inProgress), loop.Pool(root); n >= pool {
		return refuse(fmt.Sprintf("AFK pool full (%d/%d) — %s will run once an AFK effort finishes", n, pool, b.ID))
	}
	if refused, err := dispatch.Admit(ctx, b, pauseBead); err != nil {
		return fail(errors.Join(refused, err))
	} else if refused != nil {
		return refuse("run-now: " + refused.Error() + " — paused " + b.ID)
	}
	// The profile runs again, the loop's lift condition, so a retry lifts its pause now.
	if bd.PausedBy(b, bd.PausedProfile) {
		if err := unpauseBead(b.ID, bd.PausedProfile); err != nil {
			return fail(err)
		}
		slog.Info("effort.resumed", append(eventlog.BeadAttrs(b), "cause", bd.PausedProfile)...)
	}
	// Claim in_progress BEFORE the dispatch goroutine (spec §6.5/FR16), exactly as
	// the loop does: a concurrent loop tick then sees a truthful busy count and
	// does not double-dispatch. Claim is deliberately the caller's step, NOT inside
	// dispatch.Dispatch, to preserve that "claim before goroutine" invariant.
	if s := dropKeptTree(b); s != "" {
		return refuse(s)
	}
	hold, err := claimBead(b)
	if err != nil {
		return refuse("run-now: claim " + b.ID + ": " + err.Error())
	}
	return "", crew, dispatch.Inflight{ID: dispatch.NewEffortID(), Bead: b, Hold: hold}
}

// dropKeptTree is t's handling of an orphaned tree still holding b's effort/<id>
// before the cut (ADR 0104 §3, ADR 0105 §2): a backstop conflict's kept tree, or a
// stale loop tree no in-flight effort accounts for. A clean one is dropped so the
// retry recuts on the reused branch; a dirty one refuses, naming the tree. Any other
// holder (a session, a live effort, a human checkout) is left to Dispatch's held
// path. It returns the refusal status, "" to go on.
func dropKeptTree(b bd.Bead) string {
	d, ok, _ := route.OnEffort(b)
	if !ok {
		return ""
	}
	h, ok, err := contexts.WorktreeOn(contexts.Owner(b, d.Container), d.Branch)
	if err != nil {
		return "run-now: " + err.Error()
	}
	if !ok {
		return ""
	}
	sessions, err := readSessions()
	if err != nil || !session.Orphan(h.Path, sessions) {
		return ""
	}
	if id, err := afkHolder(d.Branch); err != nil || id != "" {
		return ""
	}
	dirty := false
	if !h.Prunable { // a prunable tree's directory is already gone
		if dirty, err = treeDirty(h.Path); err != nil {
			return "run-now: " + err.Error()
		}
		if c := contexts.ChildTree(contexts.Owner(b, d.Container), h.Path); c != "" && !dirty { // the child holds the nested effort's work
			if dirty, err = treeDirty(c); err != nil {
				return "run-now: " + err.Error()
			}
		}
	}
	if dirty {
		return b.ID + ": " + d.Branch + " is held by an orphaned worktree with uncommitted work, " + h.Path +
			" — commit or discard it there, or 'git worktree remove --force " + h.Path + "', then enter"
	}
	if err := dropOrphan(h.Path); err != nil {
		return "run-now: drop orphan: " + err.Error()
	}
	return ""
}

// runNowEligible is the strict-parity predicate (Spec FR5): a bead is
// run-now-eligible iff the loop would dispatch it this tick — it is in the bd
// ready frontier AND a loop-dispatched AFK kind. It reuses the loop's own
// classifiers (never re-spelled — they ARE the parity contract): a research leaf
// via loop.IsAFKLeaf, a drained defogged Chart via
// loop.IsDrainedChartCandidate + 0 open children, or a land-ready
// Checkpoint via loop.IsCheckpointCandidate + loop.CheckpointReady,
// mirroring the loop's candidates() three-arm classify. A Chart's closed children
// still holding effort/<id> count as open (Spec D7) and come back as held
// for the refusal to name. The child/git reads are
// I/O, so this press-time check is the authoritative gate — the row affordance may
// be one refresh stale.
// The AFK pool refusal is runNow's, after eligibility.
// A bead with paused:manual or paused:abandoned (ADR 0082), or under a manual-
// paused ancestor anc (inherited pause, ADR 0081/0084), is never eligible, whatever
// else it carries; one with only paused:foreign-hold is (run-now overrides it, ADR
// 0055 §6).
func runNowEligible(b bd.Bead, ready []bd.Bead, anc string) (ok bool, held []bd.Bead, err error) {
	if pauseRefusesRunNow(b, anc) || !inFrontier(b.ID, ready) {
		return false, nil, nil
	}
	switch {
	case loop.IsAFKLeaf(b):
		held, err := surgeryHold(b) // an open Surgery holds its Checkpoint's Critiques (ADR 0160)
		return len(held) == 0 && err == nil, held, err
	case loop.IsDrainedChartCandidate(b):
		open, held, err := drainBlockers(b.ID)
		return err == nil && open == 0 && len(held) == 0, held, err
	case loop.IsCheckpointCandidate(b):
		return checkpointLandHold(b)
	default:
		return false, nil, nil
	}
}

func inFrontier(id string, ready []bd.Bead) bool {
	for _, r := range ready {
		if r.ID == id {
			return true
		}
	}
	return false
}

// runNowHint explains why enter is a no-op on an ineligible row (Spec FR4),
// one message per row class — mirroring launch's non-HITL guard so a refused
// press says what the row IS and what to do instead, never a bare "not eligible".
// Reached only after the press-time gate (runNowEligible) has refused, so it
// classifies from the loop's own predicates: a closed bead, an open HITL bead
// (launch it), a not-drained Chart, or a blocked/not-ready bead. held is the
// Chart's closed children still holding effort/<id>, named with both ways out, or
// the open blockers holding an approved Checkpoint's land (Spec FR 45).
func runNowHint(b bd.Bead, ready []bd.Bead, anc string, held []bd.Bead) string {
	switch {
	case b.Status == "closed":
		return b.ID + " is closed — kept for lineage, nothing to run"
	case pauseHint(b) != "":
		return pauseHint(b)
	case anc != "":
		return inheritedPauseHint(b.ID, anc)
	case isHITL(b.Kind):
		return b.ID + " is an open HITL bead — press enter to launch it (b in the background)"
	case !inFrontier(b.ID, ready):
		// Frontier first: a drained, defogged Chart that is merely blocked (not in
		// bd ready) is NOT un-drained — reporting the drain/defog state here would
		// lie. The Chart arms below only speak to a Chart that IS in the frontier.
		return b.ID + " is blocked / not ready — run-now only drives a bead already in the bd ready frontier"
	case loop.IsDrainedChartCandidate(b) && len(held) > 0:
		return chartHeldHint(b.ID, held)
	case loop.IsDrainedChartCandidate(b):
		return b.ID + ": Chart not drained — its defog frontier hasn't cleared; run-now drives it once every child closes"
	case b.Kind == contract.KindCritique && len(held) > 0:
		return b.ID + ": held by open Surgery " + held[0].ID + " — ending the Surgery releases it"
	case loop.IsCheckpointCandidate(b) && len(held) > 0:
		return b.ID + ": " + landHeldNote(bd.Titles(held))
	case loop.IsCheckpointCandidate(b):
		return checkpointNotReadyHint(b.ID)
	default:
		return b.ID + " is not run-now eligible (only a ready AFK leaf — e.g. research or a Rollback — a drained Chart, or a land-ready Checkpoint runs via enter)"
	}
}

// inheritedPauseHint, chartHeldHint and checkpointNotReadyHint are the refusal words
// runNowHint and runNowWhy share, so the press-time and up-front gates can't drift.
func inheritedPauseHint(id, anc string) string {
	return id + " is paused via " + anc + " — select it and press p, or run 'bd label remove " + anc + " paused:manual'"
}

func chartHeldHint(id string, held []bd.Bead) string {
	return id + ": Chart not drained — " + bd.HeldHint(held)
}

func checkpointNotReadyHint(id string) string {
	return id + ": Checkpoint not ready to land — needs a closed Approval pinning the branch HEAD and no open conflict/critique children"
}

// landHeldNote is the held-land words the preview and t's refusal share (Spec
// FR 44–45).
func landHeldNote(titles []string) string {
	return "approved; land held by open blocker: " + strings.Join(titles, ", ")
}

// startHost starts the run-now host; a var, as loopVerb is, so the TUI's TestMain
// stubs it and no test forks the tui.test binary (apps/mugicrew/.mugicrew/CREW.md).
var startHost = func(c *exec.Cmd) error { return c.Start() }

// holdFd is the descriptor the host finds its hold on: ExtraFiles[0] is fd 3.
const holdFd = 3

// runHostCmd is the setsid-detached `<self> run <id> --lock-fd=3` (ADR 0330): the
// hold rides ExtraFiles, stdin is nil, and stdout and stderr go to out.
func runHostCmd(self, id string, hold, out *os.File) *exec.Cmd {
	c := exec.Command(self, "run", id, fmt.Sprintf("--lock-fd=%d", holdFd))
	c.ExtraFiles = []*os.File{hold}
	c.Stdout, c.Stderr = out, out
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return c
}

// spawnRunHost hands the claimed effort r to a detached host running the TUI's own
// binary (ADR 0330), output on <common>/mugicrew/run-<id>.out opened O_TRUNC. The
// TUI's copy of the hold closes once Start returns. On a failure the hold never
// left the TUI: run.failed, unclaim, release (ADR 0331).
func spawnRunHost(r dispatch.Inflight) (*exec.Cmd, string, error) {
	cmd, outPath, err := func() (*exec.Cmd, string, error) {
		self, err := os.Executable()
		if err != nil {
			return nil, "", err
		}
		common, err := commonDir()
		if err != nil {
			return nil, "", err
		}
		outPath := filepath.Join(common, "mugicrew", "run-"+r.Bead.ID+".out")
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return nil, "", err
		}
		out, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return nil, "", err
		}
		defer out.Close() // the host holds its own copy
		cmd := runHostCmd(self, r.Bead.ID, r.Hold.File(), out)
		return cmd, outPath, startHost(cmd)
	}()
	if err != nil {
		slog.Error("run.failed", append(eventlog.BeadAttrs(r.Bead), "effort", r.ID, "err", err.Error())...)
		if uerr := eventlog.RecordClaim(r.Bead, "claim.released", hitlUnclaim(r.Bead.ID), "effort", r.ID); uerr != nil {
			err = errors.Join(err, fmt.Errorf("unclaim: %w", uerr))
		}
		r.Hold.Release()
		return nil, "", err
	}
	r.Hold.Release() // the host owns the flock now
	return cmd, outPath, nil
}

// waitRunHost is the follow-up Cmd: it Waits on the host and reports from its exit
// status and the tail of its out file (ADR 0332). A TUI that quits first just exits.
func waitRunHost(id string, cmd *exec.Cmd, outPath string) tea.Cmd {
	return func() tea.Msg {
		err := cmd.Wait()
		out, _ := os.ReadFile(outPath)
		return runDoneMsg{status: runStatus(id, err, string(out))}
	}
}

var liftCause = regexp.MustCompile(`paused:[\w-]+`)

// runStatus is the status line for a host that exited with err (nil = 0), per
// ADR 0332's table; out is its run-<id>.out.
func runStatus(id string, err error, out string) string {
	tail := func(s string, n int) string {
		if t := lastLines(out, n); t != "" {
			s += "\n" + t
		}
		return s
	}
	if err == nil {
		return "run " + id + " done"
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return tail("run "+id+" failed: "+err.Error(), runTailLines)
	}
	switch code := ee.ExitCode(); code {
	case -1: // a signal the host did not absorb
		return tail("run "+id+" killed: "+err.Error(), runTailLines)
	case dispatch.RunPreDispatch:
		return tail("run "+id+" failed before dispatch", runTailLines)
	case dispatch.RunNotRun:
		return tail("run "+id+" not run", 1)
	case dispatch.RunLiftFailed:
		cause := liftCause.FindString(lastLines(out, 1))
		if cause == "" {
			cause = "its pause"
		}
		return tail("run "+id+" done; lifting "+cause+" failed — clear it by hand", 1)
	case dispatch.RunAbandoned:
		return tail("run "+id+" abandoned (paused:abandoned)", runTailLines)
	default: // 1, and anything else, 2 included
		return tail("run "+id+" failed: "+err.Error(), runTailLines)
	}
}

// lastLines returns the final n lines of s (fewer if s is shorter), the failure
// tail surfaced in the status.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// launchProfiled is main's Ctx (ADR 0125) as far as launching goes: a HITL launch
// resolves its kind's Agent Profile, a resume its session's recorded @profile,
// each with a harness built for it. A Ctx that isn't one runs everything on its
// own harness.
type launchProfiled interface {
	dispatch.Profiled
	ForProfile(name string) (contract.Ctx, error)
}

// launchCtx is the ctx b's HITL launch runs with and its profile name ("" = the
// built-in); an unrunnable profile is refused.
func launchCtx(ctx contract.Ctx, b bd.Bead) (contract.Ctx, string, error) {
	p, ok := ctx.(launchProfiled)
	if !ok {
		return ctx, "", nil
	}
	pctx, name, err := p.ForKind(b.Kind)
	return pctx, name, RefuseProfile(b, name, "hitl", err)
}

// resumeCtx is the ctx b's resume of a session recorded on profile runs with.
func resumeCtx(ctx contract.Ctx, b bd.Bead, profile string) (contract.Ctx, error) {
	p, ok := ctx.(launchProfiled)
	if !ok {
		return ctx, nil
	}
	pctx, err := p.ForProfile(profile)
	return pctx, RefuseProfile(b, profile, "resume", err)
}

// RefuseProfile logs an unrunnable profile's refusal once (ADR 0123) and returns
// the flash, "profile <name>: <reason>"; nil when err is.
func RefuseProfile(b bd.Bead, name, launch string, err error) error {
	if err == nil {
		return nil
	}
	slog.Warn("profile.unusable", append(append(eventlog.BeadAttrs(b), "profile", name), agentprofile.ReasonAttrs(err, launch)...)...)
	return fmt.Errorf("profile %s: %w", name, err)
}
