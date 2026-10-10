// Package loop is the outer dispatch loop (spec §6): a stateless activation
// substrate that polls bd every tick, detects ready AFK work fenced to the
// registry, claims a candidate synchronously, then runs the owner's Run in a
// goroutine gated by the AFK pool. It owns no kind (GLOSSARY.md: substrate,
// not a crewmate); all coordination is re-derived from bd every tick (ADR 0003).
// A stop drains: polling ends and every in-flight effort runs to completion,
// releasing its own claim (ADR 0050). Nothing self-heals a claim left by a crash,
// SIGKILL or force stop: it stays in_progress until graph repair (the Chopper
// Signal) or a manual bd unclaim.
package loop

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/dispatch"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/liveness"
	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/registry"
	"jayulabs.org/mugicrew/route"
)

// DefaultPool is the AFK pool size when the Workspace sets none (Spec FR3;
// Pool reads concurrency from .mugicrew/config.toml): at most that many AFK efforts
// in flight at once across all crewmates, in any mix of owners. HITL sessions take
// no slot. The loop and the TUI run-now both compare CountInflight against Pool, so
// the two launch paths gate identically. The pool is a throughput/cost budget, not
// a safety rule — the land lock and ref-only containers (ADR 0102) are — so any
// value is safe; it may overshoot by one (ADR 0070 §4), and a lowered pool drains
// naturally.
const DefaultPool = 3

// DefaultInterval is the blind-poll cadence (spec §6.2); fork-per-poll via bd
// shell-out is negligible at 15s.
const DefaultInterval = 15 * time.Second

// Loop is the dispatch substrate. It holds the assembled crewmate ctx (the
// harness lives here — the "wired in main" clause deferred from the harness backend, §5.2),
// the owner→crewmate table, and the poll interval. It keeps NO coordination
// state across ticks: its in-flight registry is per-process bookkeeping that only
// shutdown reads (ADR 0050 §2), and its hold and failure counts only decide when
// to write a pause to bd (ADR 0055 §4, ADR 0078); every tick still re-derives from bd.
type Loop struct {
	Ctx      contract.Ctx
	Crew     map[string]contract.Crewmate
	Interval time.Duration
	Rev      string // the build this image runs (ADR 0060), reported in every State; rewritten under mu
	// Bin is this image's store binary, SourceDir the source repo's git common dir
	// and CommonDir the Workspace's: a watched pinned loop (Rev set) follows the
	// source's mainline through the first two and roots its builds under the third
	// (ADR 0062 §1, ADR 0090 §2, §6). The Workspace's own mainline is never read.
	Bin, SourceDir, CommonDir string
	Watch                     bool // --watch: follow source lands; unwatched never checks for a build (ADR 0090 §4)

	wg        sync.WaitGroup
	mu        sync.Mutex
	inflight  map[string]bd.Bead // effort id → bead
	held      map[string]int     // bead id → consecutive effort.held skips (a restart loses at most PauseAfter-1)
	failing   map[string]int     // bead id → consecutive failed efforts (ADR 0078; same restart loss)
	onState   func(State)        // Run's callback; nil-safe
	draining  bool               // set once a stop drains; onState is re-told the in-flight set as it shrinks
	stop      chan struct{}      // closed by the first stop signal
	checking  bool               // a mainline eval/build is in flight (at most one)
	failed    string             // main rev whose eval/build failed; never retried (ADR 0062 §3)
	restart   *Restart           // the build of main made and not yet exec'd
	built     chan struct{}      // closed once restart is set: polling ends for the restart drain
	restartTo string             // set while draining for a restart; a stop clears it (ADR 0062 §6)
}

// State is what the loop reports through Run's onState (ADR 0064 §3), and main
// maps onto the lock body: the transitions, not a snapshot.
type State struct {
	State     string // looplock.On | looplock.Draining
	Inflight  []bd.Bead
	Rev       string // the build this image runs / rewrote to
	RestartTo string // set only on a restart drain
	FailedRev string // set only on an on loop whose build/eval failed
}

// Restart is Run's return after a restart drain: exec onto Bin, built from Rev
// (ADR 0064 §2).
type Restart struct{ Rev, Bin string }

// ErrForced is Run's return after a second stop signal abandoned in-flight efforts.
var ErrForced = errors.New("loop: force-stopped")

// Run polls until the first stop signal on sig (spec §6), then drains (ADR 0050):
// no new tick or dispatch (a tick under way dispatches nothing more), a draining
// State with the in-flight beads and loop.draining, then a wait with no timeout
// for every in-flight effort to finish; a draining State is reported again each
// time one finishes. A second signal forces: per in-flight effort a bounded
// paused:abandoned write then its effort.abandoned (claim not released, ADR 0082),
// then loop.forced and ErrForced; effort processes are never signalled. Each tick
// is best-effort: a tick error is recorded, not fatal. onState (may be nil) is
// first told on. A watched pinned loop also follows main each tick (checkMain); a stop
// cancels a running eval/build and doesn't wait for it (ADR 0064 §5). A finished
// build of main ends polling the same way and drains for a restart: the draining
// States carry RestartTo, and once the drain finishes Run returns the *Restart to
// exec (ADR 0062 §5). A stop returns nil, nil, including a stop that turned a
// restart drain into a stop drain (§6).
func (l *Loop) Run(sig <-chan os.Signal, onState func(State)) (*Restart, error) {
	if err := chdirRepoRoot(); err != nil {
		return nil, err
	}
	l.onState = onState
	l.report(State{State: looplock.On, Rev: l.Rev})
	interval := l.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	l.stop, l.built = make(chan struct{}), make(chan struct{})
	force := make(chan struct{})
	go func() { <-sig; close(l.stop); <-sig; close(force) }()
	ctx, cancel := context.WithCancel(context.Background())
	for !l.leaving() {
		l.checkMain(ctx)
		l.poll()
		select {
		case <-l.stop:
		case <-l.built:
		case <-t.C:
		}
	}
	cancel()
	return l.drain(force)
}

// report hands s to onState, if any.
func (l *Loop) report(s State) {
	if l.onState != nil {
		l.onState(s)
	}
}

// stopped reports whether the first stop signal has arrived.
func (l *Loop) stopped() bool { return closed(l.stop) }

// leaving reports whether polling is over: a stop, or a build to restart onto.
func (l *Loop) leaving() bool { return closed(l.stop) || closed(l.built) }

// closed reports whether c is closed; a nil c never is.
func closed(c chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// drain waits out the in-flight efforts, or abandons them once force closes. A
// loop that left polling for a build, not a stop, drains for a restart and
// returns it; a stop during that drain clears RestartTo and the drain ends as a
// stop's (ADR 0062 §6). A stop drain never becomes a restart.
func (l *Loop) drain(force <-chan struct{}) (*Restart, error) {
	l.mu.Lock()
	l.draining = true
	r, stop := l.restart, l.stop
	if l.stopped() {
		r = nil
	}
	if r != nil {
		l.restartTo = r.Rev
	}
	in := l.snapshotLocked()
	l.tellDrainingLocked(in)
	l.mu.Unlock()
	attrs := []any{"inflight", len(in), "beads", bd.IDs(dispatch.Beads(in))}
	if r != nil {
		attrs = append(attrs, "restart_to", r.Rev)
	}
	slog.Info("loop.draining", attrs...)
	if r == nil {
		stop = nil // a stop drain's first stop is spent: only force ends it early
	}
	done := make(chan struct{})
	go func() { l.wg.Wait(); close(done) }()
wait:
	for {
		select {
		case <-done:
			if l.stopped() { // a stop racing the last effort still wins
				return nil, nil
			}
			// ponytail: a stop landing after this check, before the exec, is lost: the
			// new image comes up on and `loop stop` must be re-run. Closing it means
			// handing the stop to the exec's caller; do that if the window ever bites.
			return r, nil
		case <-stop:
			l.mu.Lock()
			r, stop, l.restartTo = nil, nil, ""
			l.tellDrainingLocked(l.snapshotLocked())
			l.mu.Unlock()
		case <-force:
			break wait
		}
	}
	l.mu.Lock()
	in = l.snapshotLocked()
	l.mu.Unlock()
	dispatch.Abandon(in, pauseBead)
	slog.Warn("loop.forced", "inflight", len(in))
	return nil, ErrForced
}

// tellDrainingLocked reports a draining loop's in-flight beads. Under l.mu, so
// successive calls never land out of order.
// ponytail: onState runs under l.mu, so one that calls back into the Loop
// deadlocks; today's (a lockfile write) doesn't. Queue the calls if one must.
func (l *Loop) tellDrainingLocked(in []dispatch.Inflight) {
	if l.draining {
		l.report(State{State: looplock.Draining, Inflight: dispatch.Beads(in), Rev: l.Rev, RestartTo: l.restartTo})
	}
}

// snapshotLocked is the in-flight registry, ordered by effort id. Caller holds l.mu.
func (l *Loop) snapshotLocked() []dispatch.Inflight {
	var out []dispatch.Inflight
	for _, id := range slices.Sorted(maps.Keys(l.inflight)) {
		out = append(out, dispatch.Inflight{ID: id, Bead: l.inflight[id]})
	}
	return out
}

// chdirRepoRoot resolves the Workspace root (git.RepoRoot, the main worktree) and makes
// it the loop's process cwd (FR8/FR21), so the in-process fallthrough crew.Run
// resolves git/doc paths from the root regardless of the invocation dir — not
// merely when a human launches mugicrew from the root. Done once at Run start
// while still single-threaded, before dispatch spawns goroutines, so the
// process-global chdir never races a live effort. Worktree efforts set their
// subprocess cwd explicitly (reconcileInWorktree), so only the in-process path
// depends on this. Reuses the git.RepoRoot var seam session.Placement already
// consumes.
func chdirRepoRoot() error {
	root, err := git.RepoRoot()
	if err != nil {
		return err
	}
	return os.Chdir(root)
}

// Seams over the mainline check (ADR 0064 §1): main's SHA, the outPath eval and
// the rev-pinned build, separate so tests fail or block eval and build apart.
var (
	mainRev     = git.MainlineAt
	evalOutPath = looplock.OutPathAt
	buildRev    = looplock.BuildAt
)

// checkMain is a watched pinned loop's per-tick mainline check (ADR 0062 §1–4, ADR
// 0090 §4): when the source's main has moved off the loop's rev to a rev not
// already failed, nothing is built yet, and no check is in flight, it starts one
// (follow) off the tick goroutine, so dispatch never waits for nix. An unwatched
// or unpinned loop does nothing. A failed main lookup is loop.tick_failed.
func (l *Loop) checkMain(ctx context.Context) {
	l.mu.Lock()
	skip, rev, failed, built := l.checking, l.Rev, l.failed, l.restart != nil
	l.mu.Unlock()
	if !l.Watch || skip || rev == "" || l.SourceDir == "" {
		return
	}
	main, err := mainRev(l.SourceDir)
	if err != nil {
		slog.Error("loop.tick_failed", "err", err.Error())
		return
	}
	if main == rev || main == failed || built { // a made build is never chased (ADR 0062 §4)
		return
	}
	l.mu.Lock()
	l.checking = true
	l.mu.Unlock()
	go l.follow(ctx, main)
}

// follow checks main@rev, never chasing a newer one: the same outPath as this
// image's store dir rewrites Rev and clears the failure, silently (ADR 0062 §2);
// a different one is built (build.started → build.done) and ends polling for a
// restart drain onto it (§5); a failed eval or build is build.failed and
// remembered, reported as an on State with FailedRev (§3).
func (l *Loop) follow(ctx context.Context, rev string) {
	defer func() { l.mu.Lock(); l.checking = false; l.mu.Unlock() }()
	start := time.Now()
	out, err := evalOutPath(ctx, l.SourceDir, rev)
	if err == nil && out == filepath.Dir(filepath.Dir(l.Bin)) {
		l.settle(ctx, func() { l.Rev, l.failed = rev, "" })
		return
	}
	if err == nil {
		if ctx.Err() != nil {
			return
		}
		slog.Info("build.started", "rev", rev)
		start = time.Now()
		var bin string
		if bin, err = buildRev(ctx, l.SourceDir, l.CommonDir, rev); err == nil {
			l.mu.Lock()
			defer l.mu.Unlock()
			if ctx.Err() == nil {
				slog.Info("build.done", "rev", rev, "store_path", filepath.Dir(filepath.Dir(bin)), "dur_ms", time.Since(start).Milliseconds())
				l.restart = &Restart{Rev: rev, Bin: bin}
				close(l.built)
			}
			return
		}
	}
	l.settle(ctx, func() {
		slog.Error("build.failed", "rev", rev, "dur_ms", time.Since(start).Milliseconds(), "err", tail(err.Error(), 4<<10))
		l.failed = rev
	})
}

// settle applies a finished eval or failed build under l.mu (a made build ends
// polling in follow instead) and reports the on State, unless a
// stop cancelled ctx: a stopping loop drops its check, unlogged.
func (l *Loop) settle(ctx context.Context, apply func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ctx.Err() != nil {
		return
	}
	apply()
	l.report(State{State: looplock.On, Rev: l.Rev, FailedRev: l.failed})
}

// tail is s's last n bytes, cut forward to a line boundary (ADR 0062 §8).
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n-1:] // one byte more, so a cut right after a newline keeps its line
	if _, after, ok := strings.Cut(s, "\n"); ok {
		return after
	}
	return s[1:]
}

// poll is one best-effort tick: a failure goes to the event log as
// loop.tick_failed, never stderr (ADR 0029 §5).
func (l *Loop) poll() {
	if err := l.tick(); err != nil {
		slog.Error("loop.tick_failed", "err", err.Error())
	}
}

// tick is one poll (spec §6.4): re-derive inflight + candidates from bd, then
// for each candidate the AFK pool admits, claim synchronously and dispatch Run.
func (l *Loop) tick() error {
	ready, err := readyBeads()
	if err != nil {
		return err
	}
	inProgress, err := inProgressBeads()
	if err != nil {
		return err
	}
	paused, err := pausedBeads()
	if err != nil {
		return err
	}
	open, err := openBeads()
	if err != nil {
		return err
	}
	resume(ready)
	cands, err := l.candidates(Readmit(ready, open), slices.Concat(paused, inProgress))
	if err != nil {
		return err
	}
	held := map[string]bool{}
	l.mu.Lock()
	for _, b := range l.inflight {
		if br := containerStep(b); br != "" {
			held[br] = true
		}
	}
	l.mu.Unlock()
	// "." is the Workspace root: Run chdirs there (chdirRepoRoot) before any tick.
	for _, b := range selectDispatch(cands, Pool("."), CountInflight(inProgress), held) {
		if l.leaving() { // a stop (or a restart) mid-tick claims nothing more
			break
		}
		l.dispatch(b)
	}
	return nil
}

// Readmit widens the bd ready frontier by the beads hidden only by open kind:task
// blockers (Spec D6): bd ready hides a blocked Checkpoint AND its members,
// so a before-land Task would freeze the whole Checkpoint. It re-admits an open
// Checkpoint whose every open blocker is a Task, and that Checkpoint's open members
// with no open blocker of their own; the land guard in CheckpointReady then holds
// only the land (FR 16). Any other open blocker (a pivot's gate) re-admits nothing.
// open is every Open bead (closed rows may ride along: the TUI passes its full
// snapshot); a blocker absent from it is closed. Pure; the TUI's t shares it.
func Readmit(ready, open []bd.Bead) []bd.Bead {
	byID := map[string]bd.Bead{}
	for _, b := range open {
		byID[b.ID] = b
	}
	blockers := func(b bd.Bead) (n, tasks int) {
		for _, id := range b.BlockedBy {
			if x, ok := byID[id]; ok && x.Open() {
				n++
				if x.Kind == contract.KindTask {
					tasks++
				}
			}
		}
		return n, tasks
	}
	held := map[string]bool{}
	for _, b := range open {
		if n, tasks := blockers(b); b.Kind == contract.KindCheckpoint && b.Status == "open" && n > 0 && n == tasks {
			held[b.ID] = true
		}
	}
	in := map[string]bool{}
	for _, b := range ready {
		in[b.ID] = true
	}
	out := slices.Clone(ready)
	for _, b := range open {
		n, _ := blockers(b)
		if !in[b.ID] && (held[b.ID] || (held[b.Parent] && b.Status == "open" && n == 0)) {
			out = append(out, b)
		}
	}
	return out
}

// InheritedPause is the inherited-pause check candidates() runs, shared with TUI
// run-now so the two keep parity (Spec FR5, B): it returns a func
// naming b's nearest paused:manual ancestor ("" for none). Ids resolve off known
// and ready, then miss (cached); when neither holds a manual pause, nothing is
// inherited-paused and no walk runs.
func InheritedPause(ready, known []bd.Bead, miss func(id string) (bd.Bead, error)) func(bd.Bead) (string, error) {
	byID := map[string]bd.Bead{}
	manual := false
	for _, b := range slices.Concat(known, ready) {
		byID[b.ID] = b
		manual = manual || bd.PausedBy(b, bd.PausedManual)
	}
	lookup := func(id string) (bd.Bead, error) {
		if b, ok := byID[id]; ok {
			return b, nil
		}
		// ponytail: one miss per non-paused intermediate hop outside known and
		// ready, only while some manual pause exists; batch-read if that gets hot.
		b, err := miss(id)
		if err == nil {
			byID[id] = b
		}
		return b, err
	}
	return func(b bd.Bead) (string, error) {
		if !manual {
			return "", nil
		}
		return bd.PausedAncestor(b, lookup)
	}
}

// candidates classifies the ready frontier into AFK dispatch candidates in
// bd-ready order (spec §6.4): a research leaf is always a candidate; a published
// Chart is a candidate when drained (0 open children, Drafts included, §8.4) — it
// no longer gates on chart:defogged, and reconcileChart discriminates
// promote-or-stamp vs land downstream. Non-mugicrew
// beads (no kind) and known-but-unowned kinds (e.g. kind:spec) fall through —
// left sitting open (§6.1).
//
// Chart candidates come off the same bd.Ready() poll rather than a second
// open-containers list query: parent-child edges never gate `bd ready` (bd
// command mapping §3, verified), so an open Chart appears throughout its life —
// with open children, with all children closed, or with none. The OpenChildren
// call below is §6.4's second detection (the children + label check).
//
// A paused bead (any paused:<cause> label on the bead itself, ADR 0055 §3) is
// never a candidate, whatever its kind; nor is one under a manual-paused ancestor
// (inherited pause, ADR 0081). known is the tick's paused list (closed included)
// plus its in-progress rows: when it holds no manual pause, nothing is inherited-
// paused and no walk runs. Otherwise the walk resolves ids off known and ready, then
// bd show, and a failed lookup fails the tick, never a silent "not paused".
func (l *Loop) candidates(ready, known []bd.Bead) ([]bd.Bead, error) {
	inherited := InheritedPause(ready, known, showBead)
	var out []bd.Bead
	for _, b := range ready {
		if bd.Paused(b) { // own pause: skipped, no walk
			continue
		}
		anc, err := inherited(b)
		if err != nil {
			return nil, err
		}
		switch {
		case anc != "": // inherited pause: skipped
		case IsAFKLeaf(b):
			out = append(out, b)
		case IsDrainedChartCandidate(b):
			n, err := openChildren(b.ID)
			if err != nil {
				return nil, err
			}
			if n == 0 {
				out = append(out, b)
			}
		case IsCheckpointCandidate(b):
			ready, err := CheckpointReady(b)
			if err != nil {
				return nil, err
			}
			if ready {
				out = append(out, b)
			}
		}
	}
	return out, nil
}

// dispatch claims the bead in_progress synchronously (spec §6.5: before the
// goroutine, so the next tick's busy-count is truthful and re-dispatch is
// prevented), then runs the effort in a goroutine so an agentic (minutes-long)
// Run never freezes polling. dispatch.Dispatch — the shared entry point the TUI
// run-now trigger also calls — mutates the graph itself afterward (close +
// spawn); the loop never touches the bead again. The loop passes os.Stdout: it
// owns no alt-screen, so subprocess output goes straight to its log stream. Its
// own outcomes are events: loop.no_owner, claim.taken/.failed (via dispatch.Claim);
// a failed Run is Dispatch's effort.failed, which the loop only counts toward a
// failing pause (countFailure). The loop mints the effort id and holds it in the
// in-flight registry until Dispatch returns, so a stop can wait on the effort and
// a force can name it (ADR 0050).
func (l *Loop) dispatch(b bd.Bead) {
	crew, ok := l.Crew[ownerOf(b)]
	if !ok {
		slog.Warn("loop.no_owner", eventlog.BeadAttrs(b)...)
		return
	}
	if claimBead(b) != nil {
		return
	}
	id := dispatch.NewEffortID()
	l.mu.Lock()
	if l.inflight == nil {
		l.inflight = map[string]bd.Bead{}
	}
	l.inflight[id] = b
	l.mu.Unlock()
	l.wg.Add(1)
	run, live := runEffort, liveAFK
	go func() {
		defer l.wg.Done()
		o, err := run(l.Ctx, crew, b, os.Stdout, id, live)
		l.countHold(b, id, o)
		l.countFailure(b, id, o, err)
		l.mu.Lock()
		defer l.mu.Unlock()
		delete(l.inflight, id)
		l.tellDrainingLocked(l.snapshotLocked())
	}()
}

// PauseAfter is how many consecutive foreign holds, or failed efforts, pause a
// bead (ADR 0055 §2, ADR 0078).
const PauseAfter = 3

// countHold keeps b's consecutive effort.held count from its cut outcome o: a hold
// adds one, any other outcome resets it (ADR 0055 §4). At PauseAfter it writes
// paused:foreign-hold and logs effort.paused.
func (l *Loop) countHold(b bd.Bead, id string, o dispatch.Outcome) {
	if o.Cut != dispatch.CutHeld {
		l.resetCount(&l.held, b.ID)
		return
	}
	l.tally(&l.held, b, bd.PausedForeignHold, "skips", "effort", id, "branch", o.Branch, "holder", o.Holder.Path)
}

// countFailure keeps b's consecutive failed-effort count (ADR 0078 §2): an effort
// failed when Dispatch errored and the cut was neither busy nor held, so an
// unclaim error on a skip where nothing ran doesn't count. A failure adds one, a
// clean return resets it, and busy/held and a stranded integrate leave it alone,
// as does a failure whose release re-read showed paused:manual (an abort, ADR
// 0083 §8). At PauseAfter it writes paused:failing and logs effort.paused with the
// last error.
func (l *Loop) countFailure(b bd.Bead, id string, o dispatch.Outcome, err error) {
	switch {
	case o.Cut == dispatch.CutBusy || o.Cut == dispatch.CutHeld: // nothing ran
	case o.Stranded: // a backstop conflict is paused integrate-conflict, not failing (Spec FR 43)
	case err == nil:
		l.resetCount(&l.failing, b.ID)
	case o.Aborted: // the human's stop, not the bead's failure
	default:
		l.tally(&l.failing, b, bd.PausedFailing, "failures", "effort", id, "error", err.Error())
	}
}

// resetCount resets id's count in *m.
func (l *Loop) resetCount(m *map[string]int, id string) {
	l.mu.Lock()
	delete(*m, id)
	l.mu.Unlock()
}

// tally adds one to b's count in *m. At PauseAfter it writes paused:<cause> and
// logs effort.paused at Warn {cause, countKey: n, attrs…}; a failed write restores
// the count, so the next one retries it. The count resets under the same lock it
// reaches PauseAfter in, so a racing re-dispatch never writes the pause twice.
func (l *Loop) tally(m *map[string]int, b bd.Bead, cause, countKey string, attrs ...any) {
	l.mu.Lock()
	if *m == nil {
		*m = map[string]int{}
	}
	(*m)[b.ID]++
	n := (*m)[b.ID]
	if n >= PauseAfter {
		delete(*m, b.ID)
	}
	l.mu.Unlock()
	if n < PauseAfter {
		return
	}
	if err := pauseBead(b.ID, cause); err != nil {
		slog.Error("loop.tick_failed", "err", err.Error())
		l.mu.Lock()
		(*m)[b.ID] = max((*m)[b.ID], n)
		l.mu.Unlock()
		return
	}
	slog.Warn("effort.paused", append(append(eventlog.BeadAttrs(b), attrs...), "cause", cause, countKey, n)...)
}

// resume lifts paused:foreign-hold off each ready bead whose routed branch is free
// or held only by own contention, logging effort.resumed {cause foreign-hold}
// (ADR 0055 §5); the bead is a candidate again next tick. A still-foreign hold, or
// a failed lookup, leaves the pause; paused:manual and paused:failing are never
// touched (ADR 0078), so a bead carrying either too stays paused. Not lease
// self-heal: no claim is taken or released and no worktree is touched. A live TUI
// run-now tree on the branch is own contention through liveAFK, so it lifts the
// pause too.
func resume(ready []bd.Bead) {
	for _, b := range ready {
		if !bd.PausedBy(b, bd.PausedForeignHold) {
			continue
		}
		branch, foreign, err := foreignHeld(b, liveAFK)
		if err == nil && foreign {
			continue
		}
		if err == nil {
			err = unpauseBead(b.ID, bd.PausedForeignHold)
		}
		if err != nil {
			slog.Error("loop.tick_failed", "err", err.Error())
			continue
		}
		slog.Info("effort.resumed", append(eventlog.BeadAttrs(b), "cause", bd.PausedForeignHold, "branch", branch)...)
	}
}

// Seams over the bd reads and writes, the claim, the effort run and the hold
// lookup, so drain/force and the pause are testable with no live bd or subprocess.
var (
	readyBeads, inProgressBeads = bd.Ready, bd.InProgress
	pausedBeads, showBead       = bd.PausedList, bd.Show
	openBeads                   = bd.OpenList
	claimBead, runEffort        = dispatch.Claim, dispatch.Dispatch
	pauseBead, unpauseBead      = bd.Pause, bd.Unpause
	foreignHeld                 = dispatch.ForeignHeld
	liveAFK                     = LiveAFK(bd.ListAll, liveness.Alive)
)

// LiveAFK is the shared live-AFK reader (ADR 0068 §5): every IsActiveAFKEffort
// bead from a fresh list that the liveness probe finds alive, the loop's or a TUI
// run-now's. The loop and the TUI both pass it to Dispatch, and the loop to
// ForeignHeld, so a run-now tree in another process is own contention for both.
func LiveAFK(list func() ([]bd.Bead, error), alive func([]string) map[string]bool) dispatch.InflightFunc {
	return func() ([]bd.Bead, error) {
		beads, err := list()
		if err != nil {
			return nil, err
		}
		var active []bd.Bead
		var ids []string
		for _, b := range beads {
			if IsActiveAFKEffort(b) {
				active = append(active, b)
				ids = append(ids, b.ID)
			}
		}
		live := alive(ids)
		return slices.DeleteFunc(active, func(b bd.Bead) bool { return !live[b.ID] }), nil
	}
}

// IsAFKLeaf reports whether a ready bead is an AFK leaf the loop dispatches via
// Run (spec §6.4): a registry kind with mode:afk and container:false (MVP:
// research). Unknown, unowned, HITL, and container kinds are not leaves. Exported
// because it is the parity contract the TUI run-now trigger reuses to admit only
// what the loop itself would dispatch (Spec FR5) — never re-spelled.
func IsAFKLeaf(b bd.Bead) bool {
	e, err := registry.Lookup(b.Kind)
	return err == nil && e.Mode == registry.AFK && !e.Container
}

// IsActiveAFKEffort reports whether a bead is an active AFK effort — the rows the
// effort liveness view gives a verdict (ADR 0032 §4): in_progress and a registry
// mode:afk kind, container or leaf. Broader than IsAFKLeaf because AFK containers
// (Chart drain, Checkpoint land) run as efforts too. HITL claims are excluded —
// a different process boundary, never given a fabricated alive/dead. LiveAFK reads
// it to classify a refused cut's holder and a paused bead's (ADR 0054/0068).
func IsActiveAFKEffort(b bd.Bead) bool {
	e, err := registry.Lookup(b.Kind)
	return err == nil && e.Mode == registry.AFK && b.Status == "in_progress"
}

// IsDrainedChartCandidate reports whether a ready bead is a Chart the loop should
// drain-check (spec §6.4/§8.4): a registry AFK-container Chart. It NO LONGER gates
// on chart:defogged — a drained Chart reaches Run regardless of the label,
// and reconcileChart discriminates promote-fog vs stamp-defogged
// downstream; both flows route through this one candidate path. Nor on the
// placeholder body: a freshly cut Chart is a Draft until its charting session
// publishes it with its fog, so Drafts are the one gate (ADR 0059 §7). It stays
// PURE (kind, container, registry mode only); the 0-open-children half is the I/O
// check the caller adds. The kind:chart guard is what separates a Chart from a
// Checkpoint (both AFK containers), the mirror of IsCheckpointCandidate's kind
// guard. Exported alongside IsAFKLeaf as the parity contract the TUI run-now
// trigger reuses (Spec FR3/FR5).
func IsDrainedChartCandidate(b bd.Bead) bool {
	e, err := registry.Lookup(b.Kind)
	return err == nil && e.Mode == registry.AFK && e.Container && b.Kind == contract.KindChart
}

// IsCheckpointCandidate reports whether a ready bead is the Checkpoint container the
// loop should hand to Jinbe (spec §6.4, FR43): a registry AFK container of kind
// checkpoint. It is the pure twin of IsDrainedChartCandidate — a Checkpoint has no
// chart:defogged-style label, so its readiness (approvedAtHead || landed-trailer ||
// reviewDue) is the I/O check CheckpointReady adds. Exported alongside IsDrainedChartCandidate as
// the parity contract the TUI run-now trigger reuses.
func IsCheckpointCandidate(b bd.Bead) bool {
	e, err := registry.Lookup(b.Kind)
	return err == nil && e.Mode == registry.AFK && e.Container && b.Kind == contract.KindCheckpoint
}

// Seams over the git/bd legs CheckpointReady reads, so the drain-candidacy arm
// (approvedAtHead || landed-trailer || reviewDue) is testable with no live bd/git
// (mirrors the worktreeRun var seam). These alias the real package funcs; the loop test swaps them.
var (
	closedApprovalsOf = bd.ClosedChildrenOfKindBeads
	openChildrenOf    = bd.OpenChildrenOfKind
	openChildren      = bd.OpenChildren // candidates()' drained-Chart count
	approvedAtHead    = git.ApprovedAtHead
	landedBase        = git.LandedBase
	reviewDue         = bd.ReviewDue
	membersDrained    = bd.MembersDrained
	openBlockers      = bd.OpenBlockers
)

// CheckpointReady is the Checkpoint drain-candidacy I/O check (FR43/FR44): an open
// Checkpoint is a candidate when a closed Approval's Commit: pin still equals its
// branch tip (approvedAtHead), OR the land trailer is already on mainline (a roll-
// forward after a post-land crash, so Jinbe rolls the close forward) — the former only
// once its members are drained (bd.MembersDrained) — OR its Review
// is due (bd.ReviewDue: drained, no Approval at head), so Jinbe re-derives the
// Review spawn every tick and a crash between a member's close and the spawn can't
// strand it (Spec FR 30). Mainline drift never re-reviews; a tip moved
// past a stale Approval does (FR 31).
//
// First, though, an open kind:conflict or kind:critique member undrains the Checkpoint:
// Jinbe files a conflict on a rebase collision without moving the tip, so
// approvedAtHead stays true — without this gate the loop would re-dispatch Jinbe every
// tick (Land→abort→no-op) until the fix moves HEAD. Excluding an open conflict/critique
// child restores the undrain property the drained-Chart 0-open-children gate has (FR40).
//
// The at-head arm also holds while the Checkpoint has any open incoming blocker
// (bd.OpenBlockers, Spec FR 14): a before-land Task holds only the land, so
// the Review arm still runs (FR 16), and the Task's close releases it (FR 17).
//
// Exported alongside IsCheckpointCandidate as the parity contract the TUI run-now
// trigger reuses (Spec FR5): the checkpoint readiness I/O is the git/bd half
// of that arm, and the TUI runs it verbatim rather than re-spelling it — mirroring
// how the drained-Chart arm reuses IsDrainedChartCandidate + OpenChildren.
func CheckpointReady(cp bd.Bead) (bool, error) {
	ready, _, err := CheckpointLandHold(cp)
	return ready, err
}

// CheckpointLandHold is CheckpointReady that also returns the open blockers holding an
// approved-at-head, drained Checkpoint's land (nil otherwise), so the TUI's t
// refusal names them (Spec FR 45).
func CheckpointLandHold(cp bd.Bead) (ready bool, held []bd.Bead, err error) {
	for _, k := range []contract.Kind{contract.KindConflict, contract.KindCritique} {
		open, err := openChildrenOf(cp.ID, k)
		if err != nil {
			return false, nil, err
		}
		if open > 0 {
			return false, nil, nil
		}
	}
	approvals, err := closedApprovalsOf(cp.ID, contract.KindApproval)
	if err != nil {
		return false, nil, err
	}
	// ANY at-head closed Approval lands, not the first: a stale-Approval
	// self-heal leaves the stale obsoleted Approval ahead of the real at-head
	// one in creation order, so a first-match read strands the land.
	// An at-head Approval still waits on drained members (Spec FR 28), as
	// reconcileCheckpoint's land does; otherwise fall through, where reviewDue is
	// false for it. A drained one still waits on the land hold.
	for _, a := range approvals {
		if approvedAtHead(a.Description, git.BranchName(string(contract.KindCheckpoint), cp.ID)) {
			drained, err := membersDrained(cp.ID)
			if err != nil {
				return false, nil, err
			}
			if !drained {
				break
			}
			// The land hold (Spec FR 14): any open incoming blocker.
			held, err := openBlockers(cp.ID)
			return err == nil && len(held) == 0, held, err
		}
	}
	if _, err := landedBase(git.TrailerFor(string(contract.KindCheckpoint), cp.ID)); err == nil {
		return true, nil, nil
	}
	ready, err = reviewDue(cp.ID, approvedAtHead)
	return ready, nil, err
}

// ownerOf resolves a bead's owning crewmate name via the registry, or "" for an
// unregistered kind (defensive; candidates are already registry-fenced).
func ownerOf(b bd.Bead) string {
	e, err := registry.Lookup(b.Kind)
	if err != nil {
		return ""
	}
	return e.Owner
}

// CountInflight re-derives the AFK pool's in-flight total from the in_progress
// beads, whatever their owner (spec §6.4 step 1; ADR 0003: busy re-derived from
// bd, never held in memory). Only mode:afk beads count: a human's in-progress HITL
// bead (grilling/task/signal) takes no pool slot (§6.3, Spec FR10).
// Exported because the TUI run-now trigger reuses it (never re-spelled) to honor
// the pool before claiming, so the two launch paths count inflight identically.
func CountInflight(inProgress []bd.Bead) int {
	n := 0
	for _, b := range inProgress {
		if e, err := registry.Lookup(b.Kind); err == nil && e.Mode == registry.AFK {
			n++
		}
	}
	return n
}

// selectDispatch fills the AFK pool (Spec FR6/FR8) from candidates in
// order: it stops once inflight plus picks this tick reach pool, so an over-full
// pool (a lowered setting) picks nothing and drains. One owner may take several
// slots. An unowned candidate is skipped, as is a container-level step whose
// container is in held (this loop's in-flight container steps) or was picked
// earlier this tick, falling through to the next without spending a slot: one
// container step per container, whatever the pool. Leaves never skip: each works
// its own effort/<id> and integrates under the land lock, so siblings in one
// container run concurrently (Spec D11, ADR 0102). The skip is loop-local,
// never bd's in_progress list, so a stranded claim still surfaces as effort.held.
// Pure: it copies the held set and mutates only its copy.
func selectDispatch(cands []bd.Bead, pool, inflight int, held map[string]bool) []bd.Bead {
	branches := maps.Clone(held)
	if branches == nil {
		branches = map[string]bool{}
	}
	var picked []bd.Bead
	for _, b := range cands {
		if inflight+len(picked) >= pool {
			break
		}
		br := containerStep(b)
		if ownerOf(b) == "" || branches[br] {
			continue
		}
		picked = append(picked, b)
		if br != "" {
			branches[br] = true
		}
	}
	return picked
}

// containerStep is the container b's step works on when b is a container-level
// step (a Chart's charting or land, a Checkpoint's land), else "": a leaf, a
// repo-root route or a routing error (Dispatch surfaces that error itself). A land
// step's detached tree holds no branch but still names its container in Branch.
func containerStep(b bd.Bead) string {
	e, err := registry.Lookup(b.Kind)
	if err != nil || !e.Container {
		return ""
	}
	if d, err := route.Route(b); err == nil && d.Placement != route.RepoRoot {
		return cmp.Or(d.Container, d.Branch)
	}
	return ""
}
