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
	"sync/atomic"
	"syscall"
	"time"

	"jayulabs.org/mugicrew/backup"
	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/brook"
	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/dispatch"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/liveness"
	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/registry"
	"jayulabs.org/mugicrew/release"
	"jayulabs.org/mugicrew/route"
	"jayulabs.org/mugicrew/selfexe"
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
	JiraPoll                  time.Duration // [jira] poll_interval, read by Run once per loop start: a jira latch checked sooner is not a candidate (Spec mc-8ymw.9 D5)
	GitlabPoll                time.Duration // [gitlab] poll_interval, read with JiraPoll: the same for an mr latch (Spec mc-zfce.5 FR 19)
	LandingPoll               time.Duration // [gitlab] landing_poll_interval, read with JiraPoll: a Landing whose effort ended sooner is not a candidate
	Watch                     bool          // --watch: follow source lands; unwatched never checks for a build (ADR 0090 §4)
	Upgrade                   Upgrader      // unattended Upgrade's seam; nil for none (Spec mc-d0jkl §4)
	UpgradeGate               func() bool   // asked each tick: Upgrade runs only when it says opted in
	SwapPath                  string        // an installed loop's startup path: a swap of the file there drains for a restart onto it (ADR 0406 §3); empty for none

	wg            sync.WaitGroup
	mu            sync.Mutex
	inflight      map[string]bd.Bead  // effort id → bead
	held          map[string]int      // bead id → consecutive effort.held skips (a restart loses at most PauseAfter-1)
	failing       map[string]int      // bead id → consecutive failed efforts (ADR 0078; same restart loss)
	onState       func(State)         // Run's callback; nil-safe
	draining      bool                // set once a stop drains; onState is re-told the in-flight set as it shrinks
	gen           atomic.Pointer[gen] // the current signal generation (ADR 0236); swapped under mu, read anywhere
	running       atomic.Int32        // efforts in flight; drain waits on it, not on wg (ADR 0236 §5)
	notify        chan struct{}       // non-blocking send after each effort ends: wakes drain
	checking      bool                // a mainline eval/build is in flight (at most one)
	skipped       string              // the last human-step release logged as upgrade.skipped, once per value
	waiting       string              // the target an upgrade.waiting was logged for
	waitSince     time.Time           // when waiting was armed: the 24h cap counts from it
	resumeCause   string              // overrides "cancel" in the next loop.resumed, set by an upgrade drain that reverted
	upgradeTo     string              // set while the restart drain is an Upgrade's, until its install
	upgradeFailed string              // the target whose npm install failed: not retried unattended until a newer one
	failed        string              // main rev whose eval/build failed; never retried (ADR 0062 §3)
	restart       *Restart            // the build of main made and not yet exec'd
	built         chan struct{}       // closed once restart is set: polling ends for the restart drain
	asked         bool                // a restart request is pending on the check in flight: its same-outPath ending logs restart.current (ADR 0289)
	asks          chan struct{}       // SIGUSR2 requests, one slot so repeats merge; answered by request
	swapID        selfexe.Identity    // SwapPath's identity at startup
	swapKnown     bool                // swapID was read: without it nothing is compared
	swapWaitedOn  string              // the last answer swapReady logged as swap.waiting, so a stall logs once
	swapping      bool                // restart is a swap restart: Rev is filled in once the new binary answers --version
	halted        chan struct{}       // closed once an unattended Upgrade's tripped doctor stops the loop: polling ends, no restart
	stoppedFor    string              // why halted closed: the stop reason the lock body carries
	restartTo     string              // set while draining for a restart; a stop clears it (ADR 0062 §6), a cancel of that stop drain sets it again (ADR 0236 §3)

	holdChecked map[string]time.Time // Hold Errand id → last hold check end (ADR 0393); pruned when the Errand leaves the open set, memory only

	checked map[string]time.Time // latch or Landing id → last effort end (Spec mc-8ymw.9 D5); memory only, so a restart makes every latch due once

	limitedUntil time.Time      // a Plan usage limit hit holds claude dispatch until then (reset + 1m); zero: no hold. Memory only
	limit        contract.Limit // the hold's latest reset and where it came from
}

// now is the loop's clock, a seam for the limited hold's tests.
var now = time.Now

// limitSlack is how long past the reset the hold lasts, absorbing a truncated
// minute in the reset text.
const limitSlack = time.Minute

// State is what the loop reports through Run's onState (ADR 0064 §3), and main
// maps onto the lock body: the transitions, not a snapshot.
type State struct {
	State         string // looplock.On | looplock.Limited | looplock.Draining
	Inflight      []bd.Bead
	Rev           string // the build this image runs / rewrote to
	RestartTo     string // set only on a restart drain
	FailedRev     string // set only on an on loop whose build/eval failed
	UpgradeFailed string // set only on an on loop whose unattended Upgrade's npm install failed
	StoppedFor    string // set only once a tripped doctor stopped the loop: the reason, naming mugicrew doctor fix

	ResetsAt    time.Time // set only on a limited loop: when the Plan usage window resets
	LimitSource string    // set only on a limited loop: message | usage | fallback
}

// Restart is Run's return after a restart drain: exec onto Bin, built from Rev
// (ADR 0064 §2).
type Restart struct{ Rev, Bin string }

// StopWindow is how long after the first stop signal a repeat is taken as the
// same stop, not a force: npx forwards a terminal's Ctrl-C to the loop on top of
// the terminal's own delivery. A var so tests shrink it (both packages' TestMain
// zero it; a window test sets and restores its own); `loop stop --force` waits it out.
var StopWindow = time.Second

// BackupInterval is how often the loop pushes the Bead Graph to its backup remote
// (ADR 0370), in every state but off. Not config; a var so tests shrink it.
var BackupInterval = time.Hour

// backupSync is the push the backup job runs, the same code path as `mugicrew
// backup push`; a var so tests stub it.
var backupSync = backup.Sync

// runBackups pushes once at start, then every BackupInterval until quit closes.
// The start push keeps a loop that restarts onto each landed build from never
// reaching its first tick; Sync is a cheap no-op with nothing unpushed. Independent of the
// tick and of dispatch, through on, limited and draining. Sync records its own
// backup.pushed and backup.failed, and a failure is never the loop's: the job
// ignores what it returns. done closes on return, so Run joins an in-flight push
// before it returns a Restart to the exec.
func runBackups(quit <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	push, every := backupSync, BackupInterval
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		push(os.Getenv("BEADS_DIR"), eventlog.BackupLoop)
		select {
		case <-quit:
			return
		case <-t.C:
		}
	}
}

// ErrForced is Run's return after a second stop signal abandoned in-flight efforts.
var ErrForced = errors.New("loop: force-stopped")

// Run polls until the first stop signal on sig (spec §6), then drains (ADR 0050):
// no new tick or dispatch (a tick under way dispatches nothing more), a draining
// State with the in-flight beads and loop.draining, then a wait with no timeout
// for every in-flight effort to finish; a draining State is reported again each
// time one finishes. A second signal at least StopWindow after the first forces
// (one sooner is ignored): per in-flight effort a bounded paused:abandoned write
// then its effort.abandoned (claim not released, ADR 0082), then loop.forced and
// ErrForced; effort processes are never signalled. Each tick is best-effort: a
// tick error is recorded, not fatal. onState (may be nil) is first told on. A
// watched pinned loop also follows main each tick (checkMain); a stop
// cancels a running eval/build and doesn't wait for it (ADR 0064 §5). A finished
// build of main ends polling the same way and drains for a restart: the draining
// States carry RestartTo, and once the drain finishes Run returns the *Restart to
// exec (ADR 0062 §5). A stop returns nil, nil, including a stop that turned a
// restart drain into a stop drain (§6). SIGUSR1 on sig during a stop drain
// cancels it: Run logs loop.resumed {cause: cancel}, reports on (or limited) and
// polls again with nothing abandoned, or drains for the restart again when the
// stop had converted a restart drain; the next stop is a fresh drain (ADR 0236).
func (l *Loop) Run(sig <-chan os.Signal, onState func(State)) (r *Restart, err error) {
	if err := chdirRepoRoot(); err != nil {
		return nil, err
	}
	// Old beads must not fail silently under this binary: refuse before taking work,
	// and relabel nothing unasked (ADR 0355).
	if err := refuseLegacyTasks("."); err != nil {
		return nil, err
	}
	l.onState = onState
	l.JiraPoll = cfglayer.JiraPollInterval(".")
	l.GitlabPoll = cfglayer.GitlabPollInterval(".")
	l.LandingPoll = cfglayer.LandingPollInterval(".")
	l.report(State{State: looplock.On, Rev: l.Rev})
	if l.SwapPath != "" {
		l.swapID, err = selfexe.Identify(l.SwapPath)
		l.swapKnown = err == nil
	}
	t := time.NewTicker(l.pollEvery())
	defer t.Stop()
	l.gen.Store(newGen())
	l.built, l.halted, l.notify, l.asks = make(chan struct{}), make(chan struct{}), make(chan struct{}, 1), make(chan struct{}, 1)
	quit, stopped, backedUp := make(chan struct{}), make(chan struct{}), make(chan struct{})
	// joined, so nothing reads StopWindow after Run returns and no push outlives it into the exec
	defer func() { close(quit); <-stopped; <-backedUp }()
	go func() { defer close(stopped); l.signals(sig, quit) }()
	go runBackups(quit, backedUp)
	for {
		g := l.gen.Load()
		ctx, cancel := context.WithCancel(context.Background()) // a stop cancels it, so each polling phase gets its own
		for !l.leavingGen(g) {
			l.checkMain(ctx, false)
			l.checkSwap()
			l.checkUpgrade()
			l.poll()
			select {
			case <-l.asks:
				l.request(ctx)
			case <-g.stop:
			case <-l.built:
			case <-l.halted:
			case <-t.C:
			}
		}
		cancel()
		var resumed bool
		r, resumed, err = l.drain(g)
		if !resumed {
			return r, err
		}
		l.mu.Lock()
		l.draining = false
		if l.restart == nil { // a reverted restart drain reports draining next, not on
			l.report(l.liveLocked(l.snapshotLocked()))
		}
		l.mu.Unlock()
		cause := "cancel"
		if l.resumeCause != "" {
			cause, l.resumeCause = l.resumeCause, ""
		}
		slog.Info("loop.resumed", "cause", cause)
	}
}

// gen is one generation of signal channels, each close-once and closed only by
// signals (ADR 0236 §1): stop on the first stop signal, force on a repeat past
// StopWindow, cancel when SIGUSR1 ends the stop. A cancel installs a fresh one.
type gen struct{ stop, force, cancel chan struct{} }

func newGen() *gen {
	return &gen{make(chan struct{}), make(chan struct{}), make(chan struct{})}
}

// signals is Run's one signal goroutine, alive until quit: a stop signal closes
// stop, a repeat inside StopWindow is the same stop (npx forwards Ctrl-C on top
// of the terminal's), one after it closes force and ends the goroutine, and
// SIGUSR1 while stopped installs a fresh generation and closes the old cancel,
// so the next stop drains afresh. SIGUSR1 otherwise is dropped.
func (l *Loop) signals(sig <-chan os.Signal, quit <-chan struct{}) {
	g, stopped, first := l.gen.Load(), false, time.Time{}
	for {
		var s os.Signal
		select {
		case s = <-sig:
		case <-quit:
			return
		}
		switch {
		case s == syscall.SIGUSR2:
			if stopped {
				slog.Info("restart.requested", "outcome", "stopping")
				continue
			}
			select {
			case l.asks <- struct{}{}:
			default: // one already queued: repeats merge
			}
		case s == syscall.SIGUSR1:
			if stopped {
				old := g
				g, stopped = newGen(), false
				l.mu.Lock()
				l.gen.Store(g)
				l.mu.Unlock()
				close(old.cancel)
			}
		case !stopped:
			stopped, first = true, time.Now()
			close(g.stop)
		case time.Since(first) >= StopWindow:
			close(g.force)
			return
		}
	}
}

// report hands s to onState, if any.
func (l *Loop) report(s State) {
	if l.onState != nil {
		l.onState(s)
	}
}

// leaving reports whether polling is over: a stop in the current generation, or
// a build to restart onto.
func (l *Loop) leaving() bool {
	g := l.gen.Load()
	return g != nil && l.leavingGen(g) // nil before Run: ticks driven directly never leave
}

// leavingGen is leaving for the generation g: Run polls on the one its phase
// began with, so a stop cancelled before drain starts is still seen as one.
func (l *Loop) leavingGen(g *gen) bool {
	return closed(g.stop) || closed(l.built) || closed(l.halted)
}

// closed reports whether c is closed; a nil c never is.
func closed(c chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// drain waits out the in-flight efforts, or abandons them once g.force closes. A
// loop that left polling for a build, not a stop, drains for a restart and
// returns it; a stop during that drain clears RestartTo and the drain ends as a
// stop's (ADR 0062 §6). A SIGUSR1 cancel of a stop drain (g.cancel) leaves it
// with resumed true and nothing abandoned: Run polls again, or, when the stop had
// converted a restart drain, drains for that restart again (ADR 0236).
func (l *Loop) drain(g *gen) (r *Restart, resumed bool, err error) {
	l.mu.Lock()
	l.draining = true
	r, stop := l.restart, g.stop
	if closed(g.stop) || l.stoppedFor != "" {
		r = nil
	}
	if r != nil {
		l.restartTo = restartTo(r)
	}
	in := l.snapshotLocked()
	l.tellLocked(in)
	l.mu.Unlock()
	attrs := []any{"inflight", len(in), "beads", bd.IDs(dispatch.Beads(in))}
	if r != nil {
		attrs = append(attrs, "restart_to", restartTo(r))
	}
	slog.Info("loop.draining", attrs...)
	select { // a request queued as the drain starts is answered by it
	case <-l.asks:
		l.refuse(r != nil)
	default:
	}
	if r == nil {
		stop = nil // a stop drain's first stop is spent: only force or cancel ends it early
	}
	var recheck <-chan time.Time // armed while a swap restart waits for the new binary
wait:
	for {
		if l.running.Load() == 0 {
			switch {
			case closed(g.cancel):
				return nil, true, nil
			case closed(l.gen.Load().stop): // a stop racing the last effort still wins
				return nil, false, nil
			}
			if r != nil {
				ready, back := l.upgradeStep()
				if back {
					return nil, true, nil
				}
				if !ready { // a live session may last hours: look at the poll's cadence
					recheck = time.After(l.pollEvery())
					goto sleep
				}
				l.mu.Lock()
				halted := l.stoppedFor != ""
				l.mu.Unlock()
				if halted { // the install's doctor tripped: stop without the restart
					return nil, false, nil
				}
			}
			if r != nil && !l.swapReady(r) {
				recheck = time.After(swapRecheck)
				goto sleep
			}
			if r != nil { // a stop sent during the install, doctor or probe wins: no restart
				switch {
				case closed(g.cancel):
					return nil, true, nil
				case closed(l.gen.Load().stop):
					return nil, false, nil
				}
			}
			// ponytail: a stop landing after this check, before the exec, is lost: the
			// new image comes up on and `loop stop` must be re-run. Closing it means
			// handing the stop to the exec's caller; do that if the window ever bites.
			if r != nil {
				l.mu.Lock()
				r = l.restart
				l.mu.Unlock()
			}
			return r, false, nil
		}
	sleep:
		select {
		case <-l.notify:
		case <-recheck:
		case <-l.asks:
			l.mu.Lock()
			restarting := l.restartTo != ""
			l.mu.Unlock()
			l.refuse(restarting)
		case <-stop:
			l.mu.Lock()
			r, stop, l.restartTo = nil, nil, ""
			l.tellLocked(l.snapshotLocked())
			l.mu.Unlock()
		case <-g.cancel:
			return nil, true, nil
		case <-g.force:
			break wait
		}
	}
	l.mu.Lock()
	in = l.snapshotLocked()
	l.mu.Unlock()
	dispatch.Abandon(in, pauseBead)
	slog.Warn("loop.forced", "inflight", len(in))
	return nil, false, ErrForced
}

// tellLocked reports the in-flight beads of a draining or limited loop. Under
// l.mu, so successive calls never land out of order.
// ponytail: onState runs under l.mu, so one that calls back into the Loop
// deadlocks; today's (a lockfile write) doesn't. Queue the calls if one must.
func (l *Loop) tellLocked(in []dispatch.Inflight) {
	switch {
	case l.draining:
		l.report(State{State: looplock.Draining, Inflight: dispatch.Beads(in), Rev: l.Rev, RestartTo: l.restartTo, StoppedFor: l.stoppedFor})
	case !l.limitedUntil.IsZero():
		l.report(l.liveLocked(in))
	}
}

// liveLocked is the State of a polling loop: limited while a hold is set, else
// on. Caller holds l.mu.
func (l *Loop) liveLocked(in []dispatch.Inflight) State {
	if l.limitedUntil.IsZero() {
		return State{State: looplock.On, Rev: l.Rev, FailedRev: l.failed, UpgradeFailed: l.upgradeFailed, StoppedFor: l.stoppedFor}
	}
	return State{State: looplock.Limited, Inflight: dispatch.Beads(in), Rev: l.Rev, FailedRev: l.failed, UpgradeFailed: l.upgradeFailed, StoppedFor: l.stoppedFor, ResetsAt: l.limit.ResetsAt, LimitSource: l.limit.Source}
}

// holdLocked extends the limited hold to lim's reset + 1m if that is later than the
// hold's, keeping the Limit with the latest reset (Spec D5). Caller holds l.mu.
func (l *Loop) holdLocked(lim contract.Limit) {
	if until := lim.ResetsAt.Add(limitSlack); until.After(l.limitedUntil) {
		l.limitedUntil, l.limit = until, lim
	}
}

// limitHeld reports whether claude dispatch is held. Past the hold's end it clears
// the hold, writes on and logs loop.resumed {cause limit}, with no ReadUsage
// re-check: an early resume costs at most one round of the pool (Pool) of limited
// efforts, which re-hold, so a wrong reset time corrects itself (Spec D5, FR 19).
func (l *Loop) limitHeld() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.limitedUntil.IsZero() {
		return false
	}
	if now().Before(l.limitedUntil) {
		return true
	}
	l.limitedUntil, l.limit = time.Time{}, contract.Limit{}
	slog.Info("loop.resumed", "cause", "limit")
	l.report(l.liveLocked(nil))
	return false
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
// or unpinned loop does nothing unless asked (a SIGUSR2 request, ADR 0289), which lifts the
// watch gate. A failed main lookup is loop.tick_failed.
func (l *Loop) checkMain(ctx context.Context, asked bool) {
	l.mu.Lock()
	skip, rev, failed, built := l.checking, l.Rev, l.failed, l.restart != nil
	l.mu.Unlock()
	if !(l.Watch || asked) || skip || rev == "" || l.SourceDir == "" {
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
	l.checking, l.asked = true, asked
	l.mu.Unlock()
	go l.follow(ctx, main)
}

// refuse answers a request that arrives in a drain: draining on a restart drain,
// stopping on a stop drain (ADR 0289).
func (l *Loop) refuse(restarting bool) {
	outcome := "stopping"
	if restarting {
		outcome = "draining"
	}
	slog.Info("restart.requested", "outcome", outcome)
}

// request answers a SIGUSR2 while polling, in order (ADR 0289): the failed rev is
// forgotten, then a check in flight is joined, a loop without a source dir is
// unsourced, main on the running rev is current, else main is built through the
// follow path, past the watch gate.
// ponytail: a failed main lookup is only loop.tick_failed, so the CLI's ack wait times out.
func (l *Loop) request(ctx context.Context) {
	l.mu.Lock()
	if l.failed != "" {
		l.failed = ""
		l.report(l.liveLocked(l.snapshotLocked()))
	}
	if l.checking {
		l.asked = true
		l.mu.Unlock()
		slog.Info("restart.requested", "outcome", "building", "joined", true)
		return
	}
	rev, src := l.Rev, l.SourceDir
	l.mu.Unlock()
	if src == "" || rev == "" {
		slog.Info("restart.requested", "outcome", "unsourced")
		return
	}
	main, err := mainRev(src)
	if err != nil {
		slog.Error("loop.tick_failed", "err", err.Error())
		return
	}
	if main == rev {
		slog.Info("restart.requested", "outcome", "current")
		return
	}
	slog.Info("restart.requested", "outcome", "building", "joined", false)
	l.checkMain(ctx, true)
}

// follow checks main@rev, never chasing a newer one: the same outPath as this
// image's store dir rewrites Rev and clears the failure, silently (ADR 0062 §2);
// a different one is built (build.started → build.done) and ends polling for a
// restart drain onto it (§5); a failed eval or build is build.failed and
// remembered, reported as an on State with FailedRev (§3).
func (l *Loop) follow(ctx context.Context, rev string) {
	start := time.Now()
	out, err := evalOutPath(ctx, l.SourceDir, rev)
	if err == nil && out == filepath.Dir(filepath.Dir(l.Bin)) {
		l.settle(ctx, func() {
			l.Rev, l.failed = rev, ""
			if l.asked {
				slog.Info("restart.current", "rev", rev)
			}
		})
		return
	}
	if err == nil {
		if ctx.Err() != nil {
			l.settle(ctx, nil)
			return
		}
		slog.Info("build.started", "rev", rev)
		start = time.Now()
		var bin string
		if bin, err = buildRev(ctx, l.SourceDir, l.CommonDir, rev); err == nil {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.checking, l.asked = false, false // same critical section as the build, so a request never joins a finished check
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
// stop cancelled ctx: a stopping loop drops its check, unlogged. It ends the check
// in the same critical section (apply sees asked), so a request never joins a
// finished check.
func (l *Loop) settle(ctx context.Context, apply func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	defer func() { l.checking, l.asked = false, false }()
	if ctx.Err() != nil {
		return
	}
	apply()
	l.report(l.liveLocked(l.snapshotLocked()))
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
	StampModeSnapshots() // live: an Approval's mode follows this tick's setting (ADR 0346)
	effortSet.Store(memoEfforts(effortRefsAcross))
	reapIntegrated(tickEfforts())
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
	retired, err := retireStragglers(open)
	if err != nil {
		return err
	}
	gone := func(b bd.Bead) bool { return retired[b.ID] }
	ready, open = slices.DeleteFunc(ready, gone), slices.DeleteFunc(open, gone)
	resume(ready)
	resumeProfiles(l.Ctx, ready)
	pauseContexts(ready, open, paused, inProgress)
	l.clearHolds(open)
	cands, err := l.candidates(Readmit(ready, open), slices.Concat(paused, inProgress))
	if err != nil {
		return err
	}
	cands = byEffectivePriority(cands, open)
	held := map[string]bool{}
	l.mu.Lock()
	for _, b := range l.inflight {
		if br := containerStep(b); br != "" {
			held[br] = true
		}
	}
	l.mu.Unlock()
	if l.limitHeld() { // housekeeping above has run; claude dispatch waits for the reset
		cands = slices.DeleteFunc(cands, func(b bd.Bead) bool { // Mechanical kinds run no session (FR 17 amendment)
			e, err := registry.Lookup(b.Kind)
			return err != nil || !e.Mechanical
		})
	}
	// "." is the Workspace root: Run chdirs there (chdirRepoRoot) before any tick.
	for _, b := range selectDispatch(cands, Pool("."), CountInflight(inProgress, Orphans(inProgress)), held) {
		if l.leaving() { // a stop (or a restart) mid-tick claims nothing more
			break
		}
		l.dispatch(b)
	}
	return nil
}

// retireStragglers is the sweep-side backstop (ADR 0124, Spec D4): every
// listed child (but an open MR Watch) of an obsolete:-closed Checkpoint (a Draft born after the sweep's
// listing, Usopp's Approval, Jinbe's spawns) is force-closed with that
// Checkpoint's own reason, one sweep.straggler each, before the tick dispatches.
// A parent absent from open is closed, so only those are read, once each; a
// quiet tick reads nothing. Any other close reason (a land) is left alone. A
// live pivot whose every gate target is closed is closed superseded:, as
// closeOut does for siblings. It returns the ids it closed.
func retireStragglers(open []bd.Bead) (map[string]bool, error) {
	listed := map[string]bool{}
	for _, b := range open {
		listed[b.ID] = true
	}
	kids := map[string][]string{}
	var parents []string
	for _, b := range open {
		if b.Parent == "" || listed[b.Parent] {
			continue
		}
		if kids[b.Parent] == nil {
			parents = append(parents, b.Parent)
		}
		if b.Kind == contract.KindMRWatch && b.Status != "deferred" { // an open Watch outlives the sweep (it closes itself)
			continue
		}
		kids[b.Parent] = append(kids[b.Parent], b.ID)
	}
	retired := map[string]bool{}
	for _, p := range parents {
		cp, err := showBead(p)
		if err != nil {
			return nil, err
		}
		if cp.Status != "closed" || !strings.HasPrefix(cp.CloseReason, bd.ObsoleteReason) {
			continue
		}
		if len(kids[p]) == 0 { // only a spared Watch listed
			continue
		}
		if err := forceCloseBeads(kids[p], cp.CloseReason); err != nil {
			return nil, err
		}
		slog.Warn("sweep.straggler", "checkpoint", p, "ids", kids[p])
		for _, id := range kids[p] {
			retired[id] = true
		}
	}
	// Pivot stragglers (W3): a pivot has no parent, so the walk above never sees
	// one published late against a swept Checkpoint. A listed bead naming it in
	// BlockedBy is open and still gated; only the rest have their targets read.
	gating := map[string]bool{}
	for _, b := range open {
		for _, id := range b.BlockedBy {
			gating[id] = true
		}
	}
	for _, b := range open {
		// Status "open" only: a deferred Draft has no gate yet, and an in_progress
		// pivot is the lead mid-sweep, closed by its own caller.
		if b.Kind != contract.KindPivot || b.Status != "open" || gating[b.ID] {
			continue
		}
		targets, err := gateTargets(b.ID)
		if err != nil {
			return nil, err
		}
		// ponytail: an orphan pivot (no gate, only discovered-from a swept member)
		// stays live; pivotTargets refuses it and a human dismisses it once. Retire
		// orphans here if sweep.straggler counts show the ceiling bites (Spec D6).
		if len(targets) == 0 || slices.ContainsFunc(targets, func(t bd.Bead) bool { return t.Status != "closed" }) {
			continue
		}
		cps := strings.Join(bd.IDs(targets), ", ")
		if err := closeBead(b.ID, "superseded: gated Checkpoint "+cps+" closed"); err != nil {
			return nil, err
		}
		slog.Warn("sweep.straggler", "checkpoint", cps, "ids", []string{b.ID})
		retired[b.ID] = true
	}
	return retired, nil
}

// Readmit widens the bd ready frontier by the beads hidden only by land-only
// blockers (Spec D6, ADR 0269): bd ready hides a blocked Checkpoint AND its members,
// so a before-land prerequisite would freeze the whole Checkpoint. It re-admits an open
// Checkpoint whose every open blocker's kind admits a land-only hold
// (contract.Kind.AdmitsLandOnlyHold), and that Checkpoint's open members
// with no open blocker of their own; the verdict's Checkpoint arm then holds
// only the land (FR 16). An open Rollback held only on Errands is re-admitted too, so
// Jinbe still sees a human's merge or close of its revert MR. Any other open blocker
// (a pivot's gate) re-admits nothing.
// open is every Open bead (closed rows may ride along: the TUI passes its full
// snapshot); a blocker absent from it is closed. Pure; the TUI's t shares it.
func Readmit(ready, open []bd.Bead) []bd.Bead {
	byID := map[string]bd.Bead{}
	for _, b := range open {
		byID[b.ID] = b
	}
	blockers := func(b bd.Bead) (n, errands, landOnly int) {
		for _, id := range b.BlockedBy {
			if x, ok := byID[id]; ok && x.Open() {
				n++
				if x.Kind == contract.KindErrand {
					errands++
				}
				if x.Kind.AdmitsLandOnlyHold() {
					landOnly++
				}
			}
		}
		return n, errands, landOnly
	}
	held := map[string]bool{}
	for _, b := range open {
		if n, _, landOnly := blockers(b); b.Kind == contract.KindCheckpoint && b.Status == "open" && n > 0 && n == landOnly {
			held[b.ID] = true
		}
	}
	in := map[string]bool{}
	for _, b := range ready {
		in[b.ID] = true
	}
	out := slices.Clone(ready)
	for _, b := range open {
		n, errands, _ := blockers(b)
		heldRollback := b.Kind == contract.KindRollback && b.Status == "open" && n > 0 && n == errands
		if !in[b.ID] && (held[b.ID] || heldRollback || (held[b.Parent] && b.Status == "open" && n == 0)) {
			out = append(out, b)
		}
	}
	return out
}

// candidates admits the ready frontier's AFK dispatch candidates in bd-ready order
// (spec §6.4): each ready bead gets the eligibility verdict in loop mode over one live view
// built for the tick, and the runnable ones are admitted (a review-due Checkpoint
// included; Jinbe spawns the Review). Non-mugicrew beads (no kind) and known-but-unowned
// kinds (e.g. kind:spec) are not candidates, left sitting open (§6.1).
//
// known is the tick's paused list (closed included) plus its in-progress rows, which seed
// the view so the inherited-pause walk (ADR 0081) resolves them without a bd show. A
// foreign-landing verdict files its hold Errand here and skips that bead; a failed read
// or filing fails the tick, never a silent "not paused" or "not landed".
func (l *Loop) candidates(ready, known []bd.Bead) ([]bd.Bead, error) {
	view := newLiveView(tickEfforts, known, ready)
	// known holds every paused bead, so without a manual or context pause there, no
	// bead inherits one and the ancestor walk is skipped.
	in := Inputs{Paced: l.paceNotDue, NoInheritable: !slices.ContainsFunc(slices.Concat(known, ready), func(b bd.Bead) bool {
		return bd.PausedBy(b, bd.PausedManual) || bd.PausedBy(b, bd.PausedContext)
	})}
	var out []bd.Bead
	l.pruneChecked(ready)
	for _, b := range ready {
		v, err := Judge(b, ModeLoop, ready, in, view)
		if err != nil {
			return nil, err
		}
		if v.Cause == CauseForeignLanding {
			if err := holdForeign(b, v.Foreign); err != nil {
				return nil, err
			}
			continue
		}
		if v.Runnable {
			out = append(out, b)
		}
	}
	return out, nil
}

// ClearHolds is the hold clearing step the tick runs as housekeeping (ADR 0393): Jinbe
// owns it, main wires it, and nil is no step. due and checked carry the loop's pacing.
var ClearHolds func(open []bd.Bead, due func(id string) bool, checked func(id string))

// clearHolds runs the hold clearing step over the tick's open list, paced by
// LandingPoll from the end of each hold's last check, pass or fail, and prunes the
// stamps of Errands that left the open set. It runs under the claude-limit hold.
func (l *Loop) clearHolds(open []bd.Bead) {
	l.mu.Lock()
	for id := range l.holdChecked {
		if !slices.ContainsFunc(open, func(b bd.Bead) bool { return b.ID == id }) {
			delete(l.holdChecked, id)
		}
	}
	l.mu.Unlock()
	if ClearHolds == nil {
		return
	}
	ClearHolds(open, func(id string) bool {
		l.mu.Lock()
		defer l.mu.Unlock()
		at, ok := l.holdChecked[id]
		return !ok || now().Sub(at) >= l.LandingPoll
	}, func(id string) {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.holdChecked == nil {
			l.holdChecked = map[string]time.Time{}
		}
		l.holdChecked[id] = now()
	})
}

// pruneChecked drops last-checked stamps of ids no longer in the tick's ready set.
func (l *Loop) pruneChecked(ready []bd.Bead) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for id := range l.checked {
		if !slices.ContainsFunc(ready, func(b bd.Bead) bool { return b.ID == id }) {
			delete(l.checked, id)
		}
	}
}

// paceNotDue is whether b is a latch, Landing, MR Watch or Rollback with a recorded revert MR whose last effort ended within its
// poll interval: a latch by its system's (GitlabPoll for mr, else JiraPoll,
// including an unparseable body, whose check then errors and pauses), a Landing or MR Watch by
// LandingPoll. Decided only here, never in Run: a skip must not reset the
// consecutive-failure count.
func (l *Loop) paceNotDue(b bd.Bead) bool {
	var poll time.Duration
	switch b.Kind {
	case contract.KindLatch:
		poll = l.JiraPoll
		if sys, _, err := brook.Watches(b.Description); err == nil && sys == "mr" {
			poll = l.GitlabPoll
		}
	case contract.KindLanding, contract.KindMRWatch:
		poll = l.LandingPoll
	case contract.KindRollback:
		if git.RecordedMR(b.Description) == 0 {
			return false // still building or pushing its revert, closing invalid, or local: due every tick
		}
		poll = l.LandingPoll
	default:
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	at, ok := l.checked[b.ID]
	return ok && now().Sub(at) < poll
}

// holdForeign files the hold Errand for a container whose trailer another Bead Graph
// landed: that one container is refused (FR 42, ADR 0187), never the whole tick, and since
// a skipped container is never dispatched to a reconcile the loop files the Errand here.
// The blocks edge takes a Chart out of bd ready, but Readmit re-admits a Checkpoint whose
// only open blockers admit a land-only hold, so a held Checkpoint reruns this (warn +
// idempotent Errand query) every tick. An Errand-filing error fails the tick.
func holdForeign(b bd.Bead, foreign *git.ForeignLanding) error {
	slog.Warn("loop.foreign_landing", append(eventlog.BeadAttrs(b), "err", foreign.Error())...)
	if err := bd.HoldForeign(b, foreign); err != error(foreign) {
		return err // HoldForeign returns the refusal itself on the call that files the Errand
	}
	return nil
}

// dispatch first passes dispatch.Admit: a bead whose Agent Profile can't run is
// paused paused:profile and never claimed (ADR 0123 §3). Else it
// claims the bead in_progress synchronously (spec §6.5: before the
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
	refused, err := dispatch.Admit(l.Ctx, b, pauseBead)
	if err != nil {
		slog.Error("loop.tick_failed", "err", err.Error())
	}
	if refused != nil {
		return
	}
	hold, err := claimBead(b)
	if err != nil {
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
	l.running.Add(1)
	run, live := runEffort, liveAFK
	go func() {
		defer func() {
			l.running.Add(-1)
			select {
			case l.notify <- struct{}{}:
			default:
			}
			l.wg.Done()
		}()
		defer hold.Release() // after every bd write on b (ADR 0154)
		o, err := run(l.Ctx, crew, b, os.Stdout, id, live)
		l.stampChecked(b)
		l.countHold(b, id, o)
		l.countFailure(b, id, o, err)
		l.mu.Lock()
		defer l.mu.Unlock()
		delete(l.inflight, id)
		if o.Limit != nil {
			l.holdLocked(*o.Limit)
		}
		l.tellLocked(l.snapshotLocked())
	}()
}

// stampChecked records a latch's, Landing's, MR Watch's or Rollback's effort end, pass or
// fail, as its last check. A Rollback is stamped before its MR is recorded too: the effort
// that writes the MR: line started without it, and paceNotDue decides from the current body.
func (l *Loop) stampChecked(b bd.Bead) {
	if b.Kind != contract.KindLatch && b.Kind != contract.KindRollback && !b.Kind.PacedLeaf() {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.checked == nil {
		l.checked = map[string]time.Time{}
	}
	l.checked[b.ID] = now()
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
	l.tally(&l.held, b, bd.PausedForeignHold, pauseBead, "skips", "effort", id, "branch", o.Branch, "holder", o.Holder.Path)
}

// countFailure keeps b's consecutive failed-effort count (ADR 0078 §2): an effort
// failed when Dispatch errored and the cut was neither busy nor held, so an
// unclaim error on a skip where nothing ran doesn't count. A failure adds one, a
// clean return resets it, and busy/held and a stranded integrate leave it alone,
// as does a limited effort and a failure whose release re-read showed paused:manual (an abort, ADR
// 0083 §8). At PauseAfter it writes paused:failing and logs effort.paused with the
// last error; an Approval instead gets the sticky auto-approve:failed marker that
// hands it back to the human (Spec D6, ADR 0341 §1) and logs effort.fell_back with
// the last error.
func (l *Loop) countFailure(b bd.Bead, effortID string, o dispatch.Outcome, err error) {
	switch {
	case o.Cut == dispatch.CutBusy || o.Cut == dispatch.CutHeld: // nothing ran
	case o.Limit != nil: // a Plan usage limit hit blames no bead (Spec D4)
	case o.Stranded: // a backstop conflict is paused integrate-conflict, not failing (Spec FR 43)
	case err == nil:
		l.resetCount(&l.failing, b.ID)
	case o.Aborted: // the human's stop, not the bead's failure
	case b.Kind == contract.KindApproval:
		fellBack := func(id, cause string) error {
			if merr := markBead(id, cause); merr != nil {
				return merr
			}
			slog.Warn("effort.fell_back", append(eventlog.BeadAttrs(b), "cause", cause, "failures", PauseAfter, "effort", effortID, "error", err.Error())...)
			return nil
		}
		l.tally(&l.failing, b, registry.AutoApproveFailedLabel, fellBack, "failures")
	default:
		l.tally(&l.failing, b, bd.PausedFailing, pauseBead, "failures", "effort", effortID, "error", err.Error())
	}
}

// resetCount resets id's count in *m.
func (l *Loop) resetCount(m *map[string]int, id string) {
	l.mu.Lock()
	delete(*m, id)
	l.mu.Unlock()
}

// tally adds one to b's count in *m. At PauseAfter it calls write(b.ID, cause) (a
// pause, or the Approval fallback marker, which logs its own event) and otherwise
// logs effort.paused at Warn {cause, countKey: n, attrs…}; a failed write restores
// the count, so the next one retries it. The count resets under the same lock it
// reaches PauseAfter in, so a racing re-dispatch never writes the pause twice.
func (l *Loop) tally(m *map[string]int, b bd.Bead, cause string, write func(id, cause string) error, countKey string, attrs ...any) {
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
	if err := write(b.ID, cause); err != nil {
		slog.Error("loop.tick_failed", "err", err.Error())
		l.mu.Lock()
		(*m)[b.ID] = max((*m)[b.ID], n)
		l.mu.Unlock()
		return
	}
	if len(attrs) == 0 {
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

// resumeProfiles lifts paused:profile off each ready bead whose kind now resolves
// to a runnable Agent Profile, logging effort.resumed {cause profile} (ADR 0123
// §3): fixing the config is the whole fix. Other causes are never touched, so a
// bead carrying one too stays paused.
func resumeProfiles(ctx contract.Ctx, ready []bd.Bead) {
	for _, b := range ready {
		if !bd.PausedBy(b, bd.PausedProfile) {
			continue
		}
		if _, reason := dispatch.Unrunnable(ctx, b); reason != nil {
			continue
		}
		if err := unpauseBead(b.ID, bd.PausedProfile); err != nil {
			slog.Error("loop.tick_failed", "err", err.Error())
			continue
		}
		slog.Info("effort.resumed", append(eventlog.BeadAttrs(b), "cause", bd.PausedProfile)...)
	}
}

// pauseContexts holds each container whose context: label names an undeclared
// context, or whose repo: stamp disagrees with the live repo = (paused:context), and
// lifts the hold once the label, the stamp or the config is fixed,
// logging effort.resumed {cause context} (ADR 0158). It scans only beads the tick
// already fetched; with no context: label among them it reads no config and writes
// nothing. A fresh hold is also stamped onto the passed beads' labels (the slices
// share their elements with the caller). In-flight efforts are untouched.
func pauseContexts(lists ...[]bd.Bead) {
	var cs []bd.Bead
	seen := map[string]bool{}
	for _, b := range slices.Concat(lists...) {
		if b.Status != "closed" && !seen[b.ID] && (bd.ContextOf(b) != "" || bd.PausedBy(b, bd.PausedContext)) {
			seen[b.ID] = true
			cs = append(cs, b)
		}
	}
	if len(cs) == 0 {
		return
	}
	declared := declaredContexts(".")
	// the repo = table, read once and only for a labelled container that needs it (ADR 0167)
	liveRepos := sync.OnceValue(declaredRepos)
	for _, b := range cs {
		name := bd.ContextOf(b)
		_, ok := declared[name]
		hold := name != "" && !ok
		if name != "" && ok && bd.RepoStampOf(b) != "" {
			_, _, hold = contexts.Drift(b, liveRepos())
		} else if name != "" && ok && contexts.CutKind(b) && bd.RepoStampOf(b) == "" {
			// cut in the Workspace repo, then relabelled to a repo = context: the root
			// probe, not the child's, since the child also lacks it before StampCut
			if dir := liveRepos()[name]; dir != "" {
				hold = rootHasBranch(git.BranchName(string(b.Kind), b.ID))
			}
		}
		switch held := bd.PausedBy(b, bd.PausedContext); {
		case hold && !held:
			if err := pauseBead(b.ID, bd.PausedContext); err != nil {
				slog.Error("loop.tick_failed", "err", err.Error())
				continue
			}
			// The hold is effective this tick: label the tick's own copies so
			// Paused and InheritedPause see it (FR 30, no unfocused dispatch).
			for _, l := range lists {
				for i := range l {
					if l[i].ID == b.ID {
						l[i].Labels = append(slices.Clone(l[i].Labels), "paused:"+bd.PausedContext)
					}
				}
			}
		case !hold && held:
			if err := unpauseBead(b.ID, bd.PausedContext); err != nil {
				slog.Error("loop.tick_failed", "err", err.Error())
				continue
			}
			slog.Info("effort.resumed", append(eventlog.BeadAttrs(b), "cause", bd.PausedContext)...)
		}
	}
}

// Seams over the bd reads and writes, the claim, the effort run and the hold
// lookup, so drain/force and the pause are testable with no live bd or subprocess.
var (
	readyBeads, inProgressBeads = bd.Ready, bd.InProgress
	pausedBeads, showBead       = bd.PausedList, bd.Show
	openBeads, forceCloseBeads  = bd.OpenList, bd.ForceCloseAll
	gateTargets, closeBead      = bd.GateTargets, bd.Close
	claimBead, runEffort        = dispatch.Claim, dispatch.Dispatch
	pauseBead, unpauseBead      = bd.Pause, bd.Unpause
	markBead                    = bd.AddLabel
	declaredContexts            = contexts.Contexts
	declaredRepos               = contexts.Repos
	rootHasBranch               = git.Repo("").HasBranch
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
// An Approval waiting on its MR's un-draft is no leaf.
func IsAFKLeaf(b bd.Bead) bool {
	if bd.AwaitingUndraft(b) {
		return false // waiting on the human's un-draft: only the MR Watch closes it (Spec FR 9)
	}
	e, err := registry.Lookup(b.Kind)
	return err == nil && registry.ModeOf(b.Kind, b.Labels) == registry.AFK && !e.Container
}

// IsActiveAFKEffort reports whether a bead is an active AFK effort — the rows the
// effort liveness view gives a verdict (ADR 0032 §4): in_progress and a registry
// mode:afk kind, container or leaf. Broader than IsAFKLeaf because AFK containers
// (Chart drain, Checkpoint land) run as efforts too. HITL claims are excluded —
// a different process boundary, never given a fabricated alive/dead. LiveAFK reads
// it to classify a refused cut's holder and a paused bead's (ADR 0054/0068).
func IsActiveAFKEffort(b bd.Bead) bool {
	return registry.IsMode(b.Kind, b.Labels, registry.AFK) && b.Status == "in_progress"
}

// IsDrainedChartCandidate reports whether a ready bead is a Chart the loop should
// drain-check (spec §6.4/§8.4): a registry AFK-container Chart. It NO LONGER gates
// on chart:defogged — a drained Chart reaches Run regardless of the label,
// and reconcileChart discriminates promote-fog vs stamp-defogged
// downstream; both flows route through this one candidate path. Nor on the
// placeholder body: a fresh Chart is born with an open Survey, which counts as an
// open child, so the open-children count is the one gate (ADR 0140). It stays
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
// reviewDue) is the verdict's Checkpoint arm. Exported alongside IsDrainedChartCandidate as
// the parity contract the TUI run-now trigger reuses.
func IsCheckpointCandidate(b bd.Bead) bool {
	e, err := registry.Lookup(b.Kind)
	return err == nil && e.Mode == registry.AFK && e.Container && b.Kind == contract.KindCheckpoint
}

// refuseLegacyTasks is Run's start check, the predicate doctor and the TUI share; a
// var so tests never reach a live bd.
var refuseLegacyTasks = bd.RefuseLegacyTasks

// effortSet is the current tick's effort-ref set, resolved lazily and at most once per tick
// (Spec FR 46): the Workspace's refs plus each declared child's, so a nested leaf's
// unintegrated effort holds its container. Until the first tick installs it (the TUI's
// run-now reuse can run before then) tickEfforts is nil and the bd gates read the set
// themselves.
var (
	effortSet        atomic.Pointer[func() *git.EffortSet]
	effortRefsAcross = bd.EffortRefsAcross
)

// memoEfforts wraps read so the first call per tick pays for the git and config
// reads; a failed read yields nil, which the gates retry themselves.
func memoEfforts(read func() (*git.EffortSet, error)) *func() *git.EffortSet {
	once := sync.OnceValues(read)
	f := func() *git.EffortSet {
		set, err := once()
		if err != nil {
			return nil
		}
		return set
	}
	return &f
}

func tickEfforts() *git.EffortSet {
	if f := effortSet.Load(); f != nil {
		return (*f)()
	}
	return nil
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
// bead (grilling/errand/signal) takes no pool slot (§6.3, Spec FR10).
// Exported because the TUI run-now trigger reuses it (never re-spelled) to honor
// the pool before claiming, so the two launch paths count inflight identically.
// orphaned is the shared Orphaned-claim reading (Orphans, ADR 0391): an AFK bead it
// marks orphaned holds no slot, since no live host holds it. Nothing is written for
// an orphan; repair stays with the human's recover.
func CountInflight(inProgress []bd.Bead, orphaned map[string]bool) int {
	return countAFK(inProgress, orphaned, false)
}

// CountOrphaned is CountInflight's other half: the AFK beads in inProgress that
// orphaned marks, the ones CountInflight leaves out.
func CountOrphaned(inProgress []bd.Bead, orphaned map[string]bool) int {
	return countAFK(inProgress, orphaned, true)
}

// countAFK is the one filter behind both counts: the AFK beads of inProgress whose
// orphaned mark equals wantOrphaned.
func countAFK(inProgress []bd.Bead, orphaned map[string]bool, wantOrphaned bool) int {
	n := 0
	for _, b := range inProgress {
		if registry.IsMode(b.Kind, b.Labels, registry.AFK) && orphaned[b.ID] == wantOrphaned {
			n++
		}
	}
	return n
}

// byEffectivePriority stably orders candidates by (effective priority, own
// priority), both ascending, so bd ready's order (then readmits') breaks full
// ties (ADR 0410 §5). Effective priority is derived over open, the beads the
// tick already read; a chain that loops or leaves open just stops the walk.
func byEffectivePriority(cands, open []bd.Bead) []bd.Bead {
	byID := make(map[string]bd.Bead, len(open))
	for _, b := range open {
		byID[b.ID] = b
	}
	lookup := func(id string) (bd.Bead, bool) { b, ok := byID[id]; return b, ok }
	eff := make(map[string]int, len(cands))
	for _, b := range cands {
		eff[b.ID], _ = bd.EffectivePriority(b, lookup)
	}
	return slices.SortedStableFunc(slices.Values(cands), func(a, b bd.Bead) int {
		return cmp.Or(cmp.Compare(eff[a.ID], eff[b.ID]), cmp.Compare(a.Priority, b.Priority))
	})
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

// Seams under the reap, so the loop's tests run with no live bd/git.
var (
	reapBeads      = bd.ListByID
	reapCheckedOut = func(r git.Repo) (map[string]bool, error) { return r.CheckedOut() }
	reapDelete     = func(r git.Repo, branch string) error { return r.DeleteBranch(branch) }
)

// reapIntegrated deletes effort/<id> and its cut ref for each closed bead in the
// tick's integrated set that no worktree has checked out (ADR 0394
// (a-closed-effort-whose-tip-is-its-cut-ref…)), each in the repo that owns it. It
// removes no worktree: once the human does, the next tick reaps the branch. A failure
// is logged and never fails the tick; the drain gates never depend on this having run.
func reapIntegrated(set *git.EffortSet) {
	if set == nil || len(set.Integrated) == 0 {
		return
	}
	ids := slices.Sorted(maps.Keys(set.Integrated))
	beads, err := reapBeads(ids)
	if err != nil {
		slog.Warn("branch.reap_failed", "err", err.Error())
		return
	}
	checked := map[git.Repo]map[string]bool{}
	for _, b := range beads {
		if b.Status != "closed" || !set.Integrated[b.ID] {
			continue
		}
		repo := set.Owner[b.ID]
		out, ok := checked[repo]
		if !ok {
			if out, err = reapCheckedOut(repo); err != nil {
				slog.Warn("branch.reap_failed", "err", err.Error(), "repo", string(repo))
			}
			checked[repo] = out
		}
		branch := git.EffortBranch(b.ID)
		if out == nil || out[branch] { // unread tree list, or a tree has it: keep
			continue
		}
		eventlog.RecordBranches([]bd.BranchOutcome{{Branch: branch, Err: reapDelete(repo, branch)}}, eventlog.BeadAttrs(b)...)
	}
}

// swapRecheck is how often a swap restart drain, with nothing in flight, looks
// again for the new binary.
var swapRecheck = time.Second

// swapStat is the regular-file check's stat, a seam for tests.
var swapStat = os.Stat

// swapVersion is `path --version`'s answer, a seam for tests.
var swapVersion = func(path string) (string, error) { return selfexe.Version(path) }

// swapRestartTo is the lock's RestartTo while a swap restart does not yet know
// the version it restarts onto.
const swapRestartTo = "new-binary" // looplock.swapSentinel

// restartTo is what a restart drain reports as its target.
func restartTo(r *Restart) string {
	if r.Rev == "" {
		return swapRestartTo
	}
	return r.Rev
}

// checkSwap is an installed loop's per-tick binary check (ADR 0406 §3): the file
// at SwapPath changing identity, or vanishing, ends polling for a restart drain
// onto that path. Stat only. Any loop with a SwapPath does it, opted in or not.
func (l *Loop) checkSwap() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.SwapPath == "" || !l.swapKnown || l.restart != nil || !selfexe.Swapped(l.SwapPath, l.swapID) {
		return
	}
	slog.Info("swap.detected", "path", l.SwapPath)
	l.restart, l.swapping = &Restart{Bin: l.SwapPath}, true
	close(l.built)
}

// swapReady is whether a swap restart may exec: always for any other restart; for
// a swap, once the startup path is a regular file whose --version answers a strict
// release, which becomes the restart's Rev (for logging). A missing or half-written
// binary is not ready yet.
func (l *Loop) swapReady(r *Restart) bool {
	l.mu.Lock()
	swapping := l.swapping
	l.mu.Unlock()
	if !swapping {
		return true
	}
	fi, err := swapStat(r.Bin)
	if err != nil || !fi.Mode().IsRegular() {
		l.swapWaiting(r.Bin, "not a regular file")
		return false
	}
	v, err := swapVersion(r.Bin)
	if err != nil || !release.Strict(v) {
		answer := v
		if err != nil {
			answer = err.Error()
		}
		l.swapWaiting(r.Bin, answer)
		return false
	}
	l.mu.Lock()
	r.Rev = v
	l.mu.Unlock()
	slog.Info("swap.ready", "rev", v)
	return true
}

// swapWaiting logs why a swap restart is not ready, once per distinct answer, so the
// event log explains a drain that never ends (a pre-release or a non-npm build at the path).
func (l *Loop) swapWaiting(path, answer string) {
	l.mu.Lock()
	again := l.swapWaitedOn == answer
	l.swapWaitedOn = answer
	l.mu.Unlock()
	if !again {
		slog.Info("swap.waiting", "path", path, "answer", answer)
	}
}

// pollEvery is the loop's poll interval.
func (l *Loop) pollEvery() time.Duration {
	if l.Interval <= 0 {
		return DefaultInterval
	}
	return l.Interval
}
