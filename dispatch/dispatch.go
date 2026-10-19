// Package dispatch is the single per-bead dispatch entry point both the loop and
// the TUI run-now trigger call, so AFK dispatch behavior cannot drift between the
// two launch paths (Spec FR19-21). It pulls the loop's old runEffort tail
// — route.Route(bead) -> run the effort in its routed tree (worktree subprocess
// or in-process crew.Run) -> tear the worktree down — out of the loop's poll
// machinery into a shared package neither the loop nor the TUI drags the other
// into. The effort's combined stdout/stderr goes to a caller-supplied io.Writer
// (the loop passes os.Stdout; the TUI passes a buffer so it never corrupts the
// bubbletea alt-screen).
//
// The bead's in_progress claim (spec §6.5) is the CALLER's synchronous step,
// done before it hands the bead to Dispatch's goroutine, so the next loop tick's
// busy-count is truthful and the bead is not double-dispatched. Dispatch is the
// route->run->teardown effort the goroutine runs; it does not claim.
//
// Dispatch is also the effort layer of the event log (ADR 0029 §1-3): under a
// caller-minted effort id it records effort.started then .ended or .failed, for
// both routes.
// It also hosts the session layer: Sessions (and the in-process route) wrap the
// ctx so every Harness.Run records session.* events inside the effort.
package dispatch

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"jayulabs.org/mugicrew/agentprofile"
	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/liveness"
	"jayulabs.org/mugicrew/route"
	"jayulabs.org/mugicrew/session"
)

// effort is one dispatch's event-log identity: the minted id plus the common
// attrs every effort.* event carries.
type effort struct {
	id       string
	attrs    []any
	start    time.Time // zero until started
	child    bool      // a reconcile child ran, so its exit is the effort's to report
	inflight InflightFunc
	out      Outcome // how the cut went, set by worktreeRun
	intent   intent  // what the effort's Go flow handed its publish
}

// intent is what an effort's Go flow handed its publish through contract.Publisher
// (Spec D6): final edges and the finishing close. JSON-encodable, so a worktree
// child hands it across the process boundary in a file (PublishEnv).
type intent struct {
	Edges []bd.Edge
	Close *bd.Closing
	// Limit is set on a failed run that hit the Plan usage limit (Spec D3): the
	// child's only channel to the parent for it.
	Limit *contract.Limit
}

func (in intent) empty() bool { return in.Close == nil && len(in.Edges) == 0 }

// publishCtx is an effort's Ctx with its contract.Publisher handle: what the Go
// flow hands over lands in in, for bead id's publish.
type publishCtx struct {
	contract.Ctx
	id string
	in *intent
}

func (c publishCtx) CloseOnPublish(reason string) {
	c.in.Close = &bd.Closing{ID: c.id, Reason: reason}
}

func (c publishCtx) EdgeOnPublish(e contract.Edge) { c.in.Edges = append(c.in.Edges, e) }

// Cut is how an effort's worktree cut went: what the loop's consecutive-hold count
// reads (ADR 0055 §4). An effort with no tree to cut reports CutOK.
type Cut int

const (
	CutOK     Cut = iota // cut, or nothing to cut
	CutBusy              // own contention (effort.busy)
	CutHeld              // foreign hold (effort.held)
	CutFailed            // unroutable, or the cut failed (effort.failed)
)

// Outcome is Dispatch's report of the cut, with the branch and, on a refusal, its
// holder, and of the effort's integrate backstop. Aborted says the post-effort
// release re-read showed paused:manual (an abort, ADR 0083 §8), so the loop
// doesn't count the failure.
type Outcome struct {
	Cut     Cut
	Branch  string
	Holder  git.Holder
	Aborted bool
	// Stranded says the integrate backstop hit a conflict and paused the bead
	// integrate-conflict (Spec D6), or found a closed Build's trailer missing from
	// its Checkpoint and paused it not-built (ADR 0248), so the loop's failure count is untouched.
	Stranded bool
	// Limit is set when the effort ended on a Plan usage limit hit (a limited
	// effort, Spec D3): the loop holds claude dispatch to its reset.
	Limit *contract.Limit
}

// InflightFunc reads the beads of live efforts in OTHER processes (the TUI and
// the loop pass loop.LiveAFK, the re-read live AFK efforts, ADR 0054/0106): each
// one's reconcile is running, so its cut succeeded and it holds its routed branch. Read only when a cut is refused
// by a temp-shaped holder this process didn't cut. nil means none.
type InflightFunc func() ([]bd.Bead, error)

// NewEffortID mints an effort id. The loop mints it before Dispatch so a force
// stop can name the efforts it abandons (ADR 0050).
func NewEffortID() string {
	var r [4]byte
	rand.Read(r[:]) // never fails (crypto/rand, go1.24+)
	return fmt.Sprintf("%x", r)
}

// Inflight is one dispatched, not-yet-returned effort: its id and claimed bead.
// The loop and the TUI each track theirs so a stop or quit can name what it waits
// on or abandons.
type Inflight struct {
	ID   string
	Bead bd.Bead
	// Hold is the dispatcher's per-effort lock (ADR 0154), taken by Claim and
	// released by the caller once its goroutine is done with the bead.
	Hold *liveness.Hold
}

// AbandonTimeout bounds each paused:abandoned write, so a hung bd never traps a
// force stop or a quit (ADR 0082 §3's non-blocking rule).
// ponytail: fixed and serial, so N hung writes cost N×timeout (N ≤ the AFK
// pool; the TUI freezes that long on quit). Write in parallel if that
// bites. A hung write's goroutine is left behind: the process is exiting.
var AbandonTimeout = 3 * time.Second

// Abandon writes paused:abandoned on each effort's bead through pause (the
// caller's bd.Pause seam), then records its effort.abandoned with the outcome and
// its effort/<id>, which it leaves for the next launch to reuse (ADR 0082 §2–3,
// Spec FR 48–49); a land step's (on its container) or a repo-root bead's
// names none. Best-effort: a failed or timed-out write is only recorded.
func Abandon(in []Inflight, pause func(id, cause string) error) {
	for _, e := range in {
		done := make(chan error, 1)
		go func() { done <- pause(e.Bead.ID, bd.PausedAbandoned) }()
		var err error
		select {
		case err = <-done:
		case <-time.After(AbandonTimeout):
			err = fmt.Errorf("pause timed out after %s", AbandonTimeout)
		}
		var branch string
		if d, ok, _ := route.OnEffort(e.Bead); ok {
			branch = d.Branch
		}
		eventlog.RecordAbandoned(e.Bead, e.ID, branch, err)
	}
}

// Beads is each effort's bead, in order.
func Beads(in []Inflight) []bd.Bead {
	out := make([]bd.Bead, len(in))
	for i, e := range in {
		out[i] = e.Bead
	}
	return out
}

// Abort stops everything in flight on bead id for the human (ADR 0083), by cli or
// tui: refused with "nothing in flight" unless in_progress; else paused:manual
// first — a failed write returns its error with nothing logged or signalled —
// then effort.aborted, then SIGTERM to each live process's group. Returns the
// pids signalled and whether a dispatcher holds the bead's liveness lock (ADR 0155):
// none and not held is paused only; the dispatcher's group is never signalled and
// the lock never refuses. A live host's failure path does the
// cleanup; abort never unclaims, removes a tree or deletes Drafts.
// ponytail: SIGTERM only; add SIGKILL escalation if aborted efforts survive.
func Abort(id, by string) ([]int, bool, error) {
	b, err := show(id)
	if err != nil {
		return nil, false, err
	}
	if b.Status != "in_progress" {
		return nil, false, fmt.Errorf("%s: nothing in flight", id)
	}
	if err := abortPause(id, bd.PausedManual); err != nil {
		return nil, false, err
	}
	// The scan also matches this process when it is `mugicrew abort <id>`;
	// signalling it would kill the caller's process group, not the effort.
	self := os.Getpid()
	pids := slices.DeleteFunc(abortPids(id), func(pid int) bool { return pid == self })
	held := abortHeld(id)
	slog.Warn("effort.aborted", append(eventlog.BeadAttrs(b), "by", by, "pids", pids, "held", held, "paused", true)...)
	var signalled []int
	var errs []error
	for _, pid := range pids {
		switch err := killGroup(pid); {
		case errors.Is(err, syscall.ESRCH): // vanished since the scan
		case err != nil:
			errs = append(errs, fmt.Errorf("pid %d: %w", pid, err))
		default:
			signalled = append(signalled, pid)
		}
	}
	return signalled, held, errors.Join(errs...)
}

// AbortNote is what abort tells the human when it signalled no pid: a dispatcher
// holding the bead with no child stops or finishes at its next step (ADR 0155).
func AbortNote(held bool) string {
	if held {
		return "dispatcher holds it with no child; paused, it stops or finishes at its next step"
	}
	return "no live process; paused only"
}

// FormatPids renders Abort's pids space-separated for the CLI and TUI ("" for none).
func FormatPids(pids []int) string { return strings.Trim(fmt.Sprint(pids), "[]") }

// abortPause, abortPids and killGroup are Abort's seams, vars so tests stub
// them. killGroup SIGTERMs pid's own process group (every spawn is Setpgid).
var (
	abortPause = bd.Pause
	abortPids  = liveness.Pids
	abortHeld  = liveness.Held
	killGroup  = func(pid int) error {
		pgid, err := syscall.Getpgid(pid)
		if err != nil {
			return err
		}
		return syscall.Kill(-pgid, syscall.SIGTERM)
	}
)

func newEffort(b bd.Bead, id string) *effort {
	return &effort{id: id, attrs: slices.Clip(append(eventlog.BeadAttrs(b), "effort", id))} // clipped: each event's append copies
}

func (e *effort) started() {
	e.start = time.Now()
	slog.Info("effort.started", e.attrs...)
}

// finish records how a started effort ended. A reconcile child's exit (and the
// signal that killed it) ride along when one ran.
func (e *effort) finish(err error) {
	attrs := append(e.attrs, "dur_ms", time.Since(e.start).Milliseconds())
	if e.child { // an in-process bd/git exit is not the effort's
		attrs = append(attrs, eventlog.ExitAttrs(err)...)
	}
	if err != nil && e.out.Limit != nil { // a limited effort is not a failure (Spec D3)
		slog.Warn("effort.limited", append(attrs, "err", err.Error(), "resets_at", e.out.Limit.ResetsAt.Format(time.RFC3339), "source", e.out.Limit.Source)...)
		return
	}
	eventlog.Record("effort.ended", "effort.failed", err, attrs...)
}

// Dispatch runs one already-claimed bead's owner effort in the git tree its kind
// routes to (route.Route): charting and a knowledge-bearing defog
// leaf (research) each operate in a worktree on their own effort/<id>, integrated
// onto the Chart branch so their ADR/CONTEXT/docs commits reach it (Spec
// D1), and everything else falls through to in-process crew.Run in the caller's
// cwd. out receives the worktree subprocess's combined output. crew is
// the resolved owning crewmate (the caller looks it up from its own crew table).
// id is the caller-minted effort id (NewEffortID), so the caller can name the
// effort while it runs. inflight tells a refused cut's own contention from a
// foreign hold (ADR 0054). The Outcome reports the cut, whatever the effort did after.
func Dispatch(ctx contract.Ctx, crew contract.Crewmate, b bd.Bead, out io.Writer, id string, inflight InflightFunc) (Outcome, error) {
	e := newEffort(b, id)
	e.inflight = inflight
	d, err := route.Route(b)
	if err != nil { // unroutable (no membership edge): a failed effort, so the loop's silence loses nothing
		e.started()
		e.out.Cut = CutFailed
		err = e.fail(b, err)
		return e.out, err
	}
	busy := false
	if d.Placement != route.RepoRoot {
		busy, err = worktreeRun(b, d, e, out)
	} else {
		e.started()
		var sctx contract.Ctx
		if err = preSpawn(b.ID); err == nil {
			sctx, err = assemble(ctx, b, e.attrs)
		}
		if err == nil {
			err = crew.Run(publishCtx{sctx, b.ID, &e.intent}, contract.Bead{ID: b.ID, Kind: b.Kind})
			var le *contract.LimitError
			if errors.As(err, &le) {
				e.out.Limit = &le.Limit
			}
		}
	}
	if busy { // nothing ran, so no started to leave dangling and no Drafts to touch; worktreeRun logged the skip
		return e.out, err
	}
	if err == nil {
		if swept(b.ID) { // every placement (ADR 0124)
			err = e.retire(b)
		}
		if err == nil {
			err = e.publish(b)
		}
		if err != nil {
			e.child = false // a failed retire or publish's bd exit is not the child's
		}
	}
	if err != nil {
		err = e.fail(b, err) // before reading e.out: release sets its Aborted
		return e.out, err
	}
	if d.Placement == route.Detached {
		e.deleteLanded(b, d.Branch) // worktreeRun's deferred teardown already freed the branch
	}
	e.finish(nil)
	e.release(b) // a non-terminal success (e.g. Jinbe filing a land conflict) must not strand
	return e.out, nil
}

// fail records a started effort's failure, deletes its Drafts so ADR 0034's retry
// doesn't adopt them (ADR 0059 §6), then releases the caller's claim. Returns the
// effort's err.
func (e *effort) fail(b bd.Bead, err error) error {
	e.finish(err)
	ids, branches, derr := deleteDrafts(b.ID)
	// ponytail: a failed delete leaves the Drafts stalling the container (D9), as a crash does
	eventlog.RecordDrafts("draft.deleted", b.ID, ids, derr, e.attrs...)
	eventlog.RecordBranches(branches, e.attrs...)
	e.release(b)
	return err
}

// publish publishes the Drafts b's effort created, in one bd batch with the edges
// and finishing close its Go flow handed over (ADR 0059 §4, Spec D5-D6), recording
// draft.published with the ids, or draft.failed. Nothing to publish records nothing.
func (e *effort) publish(b bd.Bead) error {
	ids, err := publish(b.ID, e.intent.Edges, e.intent.Close)
	return eventlog.RecordDrafts("draft.published", b.ID, ids, err, e.attrs...)
}

// retire readies a swept member's publish (ADR 0124, Spec D2): its handed edges
// and close are dropped (the sweep closed it, and its Checkpoint is retired),
// and every Draft but a kind:signal one is closed obsolete: in one bd call,
// recorded as draft.obsoleted, so the publish opens only its Signals. A failed
// read or close is the effort's error, so e.fail deletes and nothing publishes.
// ponytail: a Draft born between this read and the publish batch goes live, at
// most one tick until the loop's straggler backstop closes it (Spec D5); a bd
// batch status guard is the upgrade path.
func (e *effort) retire(b bd.Bead) error {
	e.intent = intent{}
	ds, err := drafts(b.ID)
	if err != nil {
		return err
	}
	var ids []string
	for _, d := range ds {
		if d.Kind != contract.KindSignal { // a whitelist, so a new kind is closed
			ids = append(ids, d.ID)
		}
	}
	if len(ids) > 0 {
		err = closeDrafts(ids, fmt.Sprintf("%s %s of %s was retired", bd.ObsoleteReason, b.ID, b.Parent))
	}
	return eventlog.RecordDrafts("draft.obsoleted", b.ID, ids, err, append(e.attrs, "container", b.Parent)...)
}

// publish and deleteDrafts are bd's, vars so tests observe them without a live
// bd; so are swept and the swept tail's Drafts read and close.
var (
	publish, deleteDrafts = bd.Publish, bd.DeleteDrafts
	swept                 = session.Swept
	drafts, closeDrafts   = bd.Drafts, bd.ForceCloseAll
)

// deleteLanded deletes a container's own branch once it is landed and closed
// (Spec FR30–32): the bead re-reads closed, and the branch tip is the tip
// its land squash pinned (git.LandedTip). Best-effort: an unmet gate keeps the
// branch silently, a refused delete (a human holds it checked out, ADR 0106 §3) is
// logged at Warn, and neither fails the effort. Not in Land: a landed container
// whose close failed re-dispatches, and its land tree is cut at the branch.
// refs/mugicrew/prototypes/* is a separate ref, never touched.
// ponytail: no sweep for containers closed outside the loop; a leftover branch is harmless.
func (e *effort) deleteLanded(b bd.Bead, branch string) {
	if cur, err := show(b.ID); err != nil || cur.Status != "closed" {
		return
	}
	landedBase, landedTip, head, del := git.LandedBase, git.LandedTip, git.Head, git.DeleteBranch
	if r := contexts.RepoOf(b); r != "" { // a nested container lands and keeps its branch in the child
		landedBase, landedTip, head, del = r.LandedBase, r.LandedTip, r.Head, r.DeleteBranch
	}
	landed, err := landedBase(git.TrailerFor(string(b.Kind), b.ID))
	if err != nil {
		return
	}
	pinned, ok := landedTip(landed)
	if tip, err := head("refs/heads/" + branch); err != nil || !ok || tip != pinned {
		return
	}
	eventlog.RecordBranches([]bd.BranchOutcome{{Branch: branch, Err: del(branch)}}, e.attrs...)
}

// release hands the caller's claim back once the effort has ended, failed or not,
// so the bead returns to bd ready instead of stranding in_progress with no process
// (ADR 0034). Guarded by a re-read: only a bead still in_progress is released — a
// handler's close stands — and a failed re-read records claim.failed and leaves the
// claim, never a blind release.
func (e *effort) release(b bd.Bead) {
	cur, serr := show(b.ID)
	// ponytail: a failed re-read leaves Aborted false, so an aborted effort then
	// counts toward paused:failing — accepted: it already carries paused:manual.
	e.out.Aborted = bd.PausedBy(cur, bd.PausedManual)
	if serr != nil { // a non-nil err records claim.failed: nothing was released
		eventlog.RecordClaim(b, "claim.released", serr, "effort", e.id)
	} else if cur.Status == "in_progress" {
		eventlog.RecordClaim(b, "claim.released", unclaim(b.ID), "effort", e.id)
	}
}

// worktreeRun runs an effort's reconcile inside a freshly-cut per-effort git
// worktree at Dispatch's routed d: checked out to effort/<id>, cut or reused at
// its container (route.Worktree: charting, a defog leaf, a Build, Critique or
// Conflict), or detached at the container for a land step
// (route.Detached, ADR 0102 §8), then tears it down; an effort tree's teardown is
// the integrate backstop's, which also says whether it stranded (Spec D6). The
// reconcile runs as a subprocess whose cwd IS that worktree — ADR 0010's
// "worktree is the caller's cwd" seam — so git's land/branch signatures stay
// dir-less and two concurrent efforts operate on separate trees and separate
// indexes (no index.lock race). The effort starts once the tree is cut; busy
// reports the skip, which records no started; a non-busy return must have
// called e.started(), and a busy one logged effort.busy or effort.held. A var so
// callers' tests stub the git+exec machinery.
// ponytail: create-and-remove per effort; a worktree pool/GC is explicitly OOS,
// add only if concurrent-Chart disk/add cost bites.
var worktreeRun = func(b bd.Bead, d route.Destination, e *effort, out io.Writer) (busy bool, err error) {
	branch := d.Branch
	e.out.Branch = branch
	effortTree := d.Placement == route.Worktree
	wt, teardown, err := session.CutTree(b.ID, d)
	if err != nil {
		// Contention interlock: git refuses a second checkout of a
		// branch a listed worktree holds (ADR 0056) — that refusal IS the
		// one-effort-per-effort-branch gate (ADR 0106). Skip this poll rather than crash; the next tick re-derives and
		// retries once the holder's worktree is torn down — which needs the
		// caller's claim released, or bd ready never offers it again and the bead
		// strands in_progress with no process (reads dead in the TUI).
		if h, busy := contexts.Busy(contexts.Owner(b, d.Container), err, branch); busy {
			err = eventlog.RecordClaim(b, "claim.released", unclaim(b.ID), "effort", e.id)
			e.out.Holder = h
			if ownContention(b, branch, h, e.inflight) {
				e.out.Cut = CutBusy
				slog.Debug("effort.busy", append(e.attrs, "branch", branch)...)
			} else {
				e.out.Cut = CutHeld
				slog.Warn("effort.held", append(e.attrs, "branch", branch, "holder", h.Path, "prunable", h.Prunable)...)
			}
			return true, err
		}
		e.out.Cut = CutFailed
		e.started() // a real cut failure (holderless exit 128 included) is a failed effort, not a skip
		return false, err
	}
	if !effortTree {
		defer teardown() // best-effort teardown; --force drops a dirty tree
	}
	cut.Store(wt, true)
	defer cut.Delete(wt)
	e.started()
	if err := preSpawn(b.ID); err != nil { // nothing spawned; an effort tree still needs the backstop's teardown
		if effortTree {
			backstop(b, wt, true, teardown, out, e.attrs...)
		}
		return false, err
	}
	e.child = true
	defer os.Remove(intentPath(e.id))
	err = reconcileInWorktree(wt, b.ID, e.id, out)
	if rerr := readIntent(intentPath(e.id), &e.intent); err == nil { // a failed child may still hand over its Limit
		err = rerr
	}
	e.out.Limit = e.intent.Limit
	if effortTree { // the backstop owns an effort tree's teardown (Spec D6)
		var kept bool
		e.out.Stranded, kept = backstop(b, wt, err != nil, teardown, out, e.attrs...)
		if kept { // a handed close (a pivoted: one) would strand the live ref on a closed bead; keep it open to retry
			e.intent.Close = nil
		}
	}
	return false, err
}

// errAborted is a pre-spawn re-read that found the bead paused by hand.
var errAborted = errors.New("aborted before spawn: paused:manual")

// preSpawn re-reads bead id just before its effort spawns (ADR 0155): a human's
// abort in the pre-cut window wrote paused:manual with no child yet to signal.
// A failed read errs too: never spawn on a guess. No re-read follows the child.
func preSpawn(id string) error {
	cur, err := show(id)
	if err != nil {
		return err
	}
	if bd.PausedBy(cur, bd.PausedManual) {
		return errAborted
	}
	return nil
}

// backstop is session.Backstop, a var so the cut-and-backstop wiring is testable
// without a subprocess.
var backstop = session.Backstop

// PublishEnv names the file a worktree child leaves its effort's intent in, for
// the parent's publish (Spec D6). Set only on the dispatch child, whose reconcile
// verb reads and unsets it once, so a nested reconcile has no Publisher.
const PublishEnv = "MUGICREW_PUBLISH"

// intentPath is effort id's intent file.
// ponytail: a force stop leaves the child running with no parent to read it, so
// the file leaks and the handed close is dropped (bead stays in_progress, Drafts
// stay — D9); Chopper's cleanup is the upgrade path.
func intentPath(effortID string) string {
	return filepath.Join(os.TempDir(), "mugicrew-publish-"+effortID+".json")
}

// readIntent reads a child's intent; no file means it handed nothing over.
func readIntent(path string, in *intent) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, in)
}

// cut is this process's in-flight efforts' worktrees (path → true): the trees own
// matches a refused cut's holder against exactly, so an effort whose own cut was
// refused never vouches for the holder.
var cut sync.Map

// ownContention reports whether h, the holder refusing b's cut on branch, is
// mugicrew's own contention (ADR 0054 §1): a live, temp-shaped tree that an
// in-flight effort (this process's, or a live one inflight names) or an alive
// session accounts for. Anything else (a human's tree, the main tree, a prunable
// entry, an orphan, an exited session's tree) is a foreign hold, and so is a holder
// a failed read leaves unexplained: not provably ours, so it surfaces. Only
// classifies; never removes or prunes anything.
func ownContention(b bd.Bead, branch string, h git.Holder, inflight InflightFunc) bool {
	if h.Prunable || !git.IsTempWorktree(h.Path) {
		return false
	}
	if _, ok := cut.Load(h.Path); ok {
		return true
	}
	if inflight != nil {
		if in, err := inflight(); err == nil {
			for _, o := range in {
				if d, rerr := route.Route(o); o.ID != b.ID && rerr == nil && d.Placement == route.Worktree && d.Branch == branch {
					return true // live, so cut: one checkout per branch makes it the holder
				}
			}
		}
	}
	ss, err := sessions() // one multiplexer query, only on the refusal path
	return err == nil && ss[session.HolderBead(h.Path, ss)].State == session.Alive
}

// ForeignHeld reports whether b's routed branch is foreign-held right now (ADR
// 0054): the loop's auto-clear check on a paused:foreign-hold bead (ADR 0055 §5).
// A free branch, a repo-root or detached (land step) route, or own contention is
// not foreign. Only reads; never removes or prunes anything.
func ForeignHeld(b bd.Bead, inflight InflightFunc) (branch string, foreign bool, err error) {
	d, ok, err := route.OnEffort(b)
	if !ok {
		return "", false, err
	}
	h, ok, err := contexts.WorktreeOn(contexts.Owner(b, d.Container), d.Branch)
	if err != nil || !ok {
		return d.Branch, false, err
	}
	return d.Branch, !ownContention(b, d.Branch, h, inflight), nil
}

// sessions is the detached-session table, a var so tests script it without tmux.
var sessions = session.Sessions

// claim, unclaim and show are bd's, vars so tests observe the claim ops without a live bd.
var claim, unclaim, show = bd.Claim, bd.Unclaim, bd.Show

// Claim takes b's in_progress claim for its owning crewmate and records
// claim.taken, or claim.failed at Error (ADR 0029 §5). bd stays event-blind: the
// loop and the TUI run-now claim through here. It takes the bead's per-effort
// hold first (ADR 0154) so the effort reads alive from claim to release; on
// success the caller owns the hold and releases it when done with the bead. A
// hold that can't be acquired means another dispatcher has the bead: claim.failed,
// no claim. A failed claim releases the hold.
func Claim(b bd.Bead) (*liveness.Hold, error) {
	h, err := liveness.Acquire(b.ID)
	if err != nil {
		return nil, eventlog.RecordClaim(b, "claim.taken", err)
	}
	if err := eventlog.RecordClaim(b, "claim.taken", claim(b.ID)); err != nil {
		h.Release()
		return nil, err
	}
	return h, nil
}

// Reconcile is the reconcile verb: crew's Run over b with session logging, recorded
// as reconcile.done, or reconcile.failed at Error, naming b's routed branch. As a
// dispatch child (path, PublishEnv's file, set) its Ctx carries the Publisher, and
// what the flow hands over is written to path for the parent's publish.
func Reconcile(ctx contract.Ctx, crew contract.Crewmate, b bd.Bead, path string) error {
	ctx, err := Sessions(ctx, b)
	var in intent
	if path != "" {
		ctx = publishCtx{ctx, b.ID, &in}
	}
	if err == nil {
		err = crew.Run(ctx, contract.Bead{ID: b.ID, Kind: b.Kind})
	}
	var le *contract.LimitError
	if errors.As(err, &le) && path != "" { // handed over even though the run failed: Limit only
		in = intent{Limit: &le.Limit}
		if werr := writeIntent(path, in); werr != nil {
			err = errors.Join(err, werr)
		}
	} else if err == nil && path != "" && !in.empty() {
		err = writeIntent(path, in)
	}
	d, _ := route.Route(b) // a route error leaves branch "": the event still records the outcome
	return eventlog.Record("reconcile.done", "reconcile.failed", err, append(eventlog.BeadAttrs(b), "branch", d.Branch)...)
}

// reconcileInWorktree runs `mugicrew reconcile <id>` with the worktree as cwd —
// re-entering this same binary's idempotent accept/land verb (the charting agent
// runs the same verb, §8.2). The subprocess's combined output goes to out — the
// loop passes os.Stdout, the TUI passes a buffer so a background effort never
// corrupts its alt-screen. A var so callers' tests avoid a real subprocess.
var reconcileInWorktree = func(worktree, beadID, effortID string, out io.Writer) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	return reconcileCmd(self, worktree, beadID, effortID, out).Run()
}

// reconcileCmd builds the child: cwd the worktree, output to out, and in its env
// the effort id (eventlog.EffortEnv) for the child's own events and its intent
// file (PublishEnv). It runs in its own process group, so a terminal Ctrl-C or
// hangup reaches only the loop (ADR 0050).
func reconcileCmd(self, worktree, beadID, effortID string, out io.Writer) *exec.Cmd {
	cmd := exec.Command(self, "reconcile", beadID)
	cmd.Dir = worktree
	cmd.Env = append(os.Environ(), eventlog.EffortEnv+"="+effortID, PublishEnv+"="+intentPath(effortID))
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

// writeIntent leaves a child's intent at path for the parent.
func writeIntent(path string, in intent) error {
	data, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Sessions wraps ctx so every Harness.Run a crewmate makes records session.*
// events stamped with b's common attrs — the reconcile child's wrap, where the
// effort id arrives via MUGICREW_EFFORT and the emitter stamps it (ADR 0029 §1-2).
// Dispatch's in-process route wraps the same way with the effort's attrs. A
// Profiled ctx is first assembled for b's Agent Profile; an unrunnable one is the
// error.
func Sessions(ctx contract.Ctx, b bd.Bead) (contract.Ctx, error) {
	return assemble(ctx, b, eventlog.BeadAttrs(b))
}

// Profiled is a Ctx that assembles each effort's ctx for its bead's kind (ADR
// 0125): main's resolves the kind's Agent Profile and builds a
// harness for it, returning the profile name ("" = the built-in), named beside
// the error too when it can't run. A Ctx that isn't one runs every effort on its
// own harness.
type Profiled interface {
	ForKind(kind contract.Kind) (ctx contract.Ctx, profile string, err error)
}

// Unrunnable resolves b's Agent Profile when ctx is Profiled: the profile name
// and, when it can't run, why (ADR 0123 §1). Any other ctx is always runnable.
func Unrunnable(ctx contract.Ctx, b bd.Bead) (profile string, reason error) {
	p, ok := ctx.(Profiled)
	if !ok {
		return "", nil
	}
	_, profile, reason = p.ForKind(b.Kind)
	return profile, reason
}

// Admit is the profile gate the loop and TUI run-now pass before claiming b or
// cutting its tree (ADR 0123 §3): an unrunnable profile logs profile.unusable at
// Warn {profile, reason, launch afk}, pauses b paused:profile through pause and
// logs effort.paused {cause profile}. refused is "profile <name>: <reason>", nil
// to go on; err is a failed pause write, which leaves b a candidate to retry.
func Admit(ctx contract.Ctx, b bd.Bead, pause func(id, cause string) error) (refused, err error) {
	profile, reason := Unrunnable(ctx, b)
	if reason == nil {
		return nil, nil
	}
	attrs := slices.Clip(append(eventlog.BeadAttrs(b), "profile", profile)) // clipped: each event's append copies
	slog.Warn("profile.unusable", append(attrs, agentprofile.ReasonAttrs(reason, "afk")...)...)
	refused = fmt.Errorf("profile %s: %w", profile, reason)
	if err = pause(b.ID, bd.PausedProfile); err != nil {
		return refused, err
	}
	slog.Warn("effort.paused", append(attrs, "cause", bd.PausedProfile)...)
	return refused, nil
}

// assemble is Sessions with the caller's attrs, plus profile when one is named,
// so it rides on every session.* event.
// ponytail: this resolves the profile a second time after Admit; a catalog or
// config edit in between (seconds on the worktree route, where the reconcile
// child re-resolves) fails the effort here as "agent profile: ..." and counts
// toward paused:failing instead of Admit's paused:profile refusal. Route this
// error through Admit's pause path if that window ever bites.
func assemble(ctx contract.Ctx, b bd.Bead, attrs []any) (contract.Ctx, error) {
	if p, ok := ctx.(Profiled); ok {
		pctx, name, err := p.ForKind(b.Kind)
		if err != nil {
			return ctx, fmt.Errorf("agent profile: %w", err)
		}
		ctx = pctx
		if name != "" {
			attrs = append(attrs, "profile", name)
		}
	}
	return sessionCtx{ctx, slices.Clip(attrs)}, nil
}

// sessionCtx is the one place every Harness.Run caller routes through, so the
// crewmates don't each grow session logging.
type sessionCtx struct {
	contract.Ctx
	attrs []any
}

func (c sessionCtx) Harness() contract.Harness { return sessionHarness{c.Ctx.Harness(), c.attrs} }

type sessionHarness struct {
	contract.Harness
	attrs []any
}

// Run pre-assigns the session id so session.started carries it (a killed session
// still leaves a pointer to its transcript), then records session.ended, or
// session.failed at Error, with the harness's evidence (ADR 0029 §4) and, when
// known, the Session cost as top-level attrs.
func (h sessionHarness) Run(ctx contract.Ctx, inv contract.Invocation) (contract.Result, error) {
	if inv.SessionID == "" {
		inv.SessionID = contract.NewSessionID()
	}
	attrs := append(h.attrs, "session", inv.SessionID)
	slog.Info("session.started", attrs...)
	start := time.Now()
	res, err := h.Harness.Run(ctx, inv)
	attrs = append(attrs, "exit", res.Exit, "dur_ms", time.Since(start).Milliseconds(), "result", res.Summary)
	if res.Signal != "" {
		attrs = append(attrs, "signal", res.Signal)
	}
	if res.Stderr != "" {
		attrs = append(attrs, "stderr", res.Stderr)
	}
	attrs = append(attrs, res.Cost.Attrs()...)
	return res, eventlog.Record("session.ended", "session.failed", err, attrs...)
}
