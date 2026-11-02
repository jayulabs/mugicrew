package loop

import (
	"fmt"
	"log/slog"
	"time"

	"jayulabs.org/mugicrew/release"
	"jayulabs.org/mugicrew/selfexe"
	"jayulabs.org/mugicrew/session"
)

// Upgrader is the loop's narrow seam onto an unattended Upgrade (Spec mc-d0jkl
// §4), wired in by main so this package knows nothing of npm. Check answers from
// the cached release check: the safe target ("" for none), the first marked
// release blocking it, and ok=false when the cache cannot say. Install runs the
// exact-version npm plan and returns its exit code. Doctor runs the new binary's
// doctor and returns its exit code and To-fix count.
type Upgrader interface {
	Check() (target, blockedBy string, ok bool)
	Install(target string) (npmCode int)
	Doctor() (code, fixes int)
}

// sessionTable is the Workspace's live session table, a seam for tests.
var sessionTable = session.Sessions

// naturallyIdle is whether no effort is in flight and no session is live in the
// Workspace's tmux server. A table read error is not idle: never install on a guess.
func (l *Loop) naturallyIdle() bool {
	if l.running.Load() != 0 {
		return false
	}
	sessions, err := sessionTable()
	return err == nil && session.Live(sessions) == 0
}

// checkUpgrade is an opted-in loop's per-tick unattended Upgrade step: arm a wait
// on the cached safe target, and install it at natural idle. An npm failure is
// remembered and never retried until Check offers a newer target; a clean install
// leaves the swapped binary for checkSwap's restart. Runs on the tick goroutine,
// before poll, so nothing dispatches while npm runs.
func (l *Loop) checkUpgrade() {
	if l.Upgrade == nil || l.UpgradeGate == nil || !l.UpgradeGate() {
		return
	}
	l.mu.Lock()
	leaving := l.restart != nil
	l.mu.Unlock()
	if leaving {
		return
	}
	target, blockedBy, ok := l.Upgrade.Check()
	if !ok {
		return
	}
	if blockedBy != "" && blockedBy != l.skipped {
		l.skipped = blockedBy
		slog.Info("upgrade.skipped", "version", blockedBy, "reason", "human-step")
	}
	if target == "" {
		l.waiting = ""
		return
	}
	l.mu.Lock()
	if l.upgradeFailed != "" {
		if !release.Newer(target, l.upgradeFailed) {
			l.mu.Unlock()
			return
		}
		l.upgradeFailed = ""
		l.report(l.liveLocked(l.snapshotLocked()))
	}
	l.mu.Unlock()
	if l.waiting != target {
		if l.waiting == "" {
			l.waitSince = now() // a change of target keeps the clock running
		}
		l.waiting = target
		slog.Info("upgrade.waiting", "target", target)
	}
	if !l.naturallyIdle() {
		if l.SwapPath != "" && now().Sub(l.waitSince) >= upgradeWaitCap {
			l.drainForUpgrade(target)
		}
		return
	}
	l.install(target)
}

// upgradeWaitCap is how long an armed wait for natural idle lasts before the loop
// pauses dispatch and drains for the Upgrade (Spec mc-d0jkl FR 17).
const upgradeWaitCap = 24 * time.Hour

// drainForUpgrade ends polling for a restart drain that installs target once the
// efforts and live sessions end, however long that takes. It is a swap restart whose
// binary is not yet swapped: the lock reads Draining with RestartTo = target.
func (l *Loop) drainForUpgrade(target string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.restart, l.swapping, l.upgradeTo = &Restart{Rev: target, Bin: l.SwapPath}, true, target
	slog.Info("upgrade.draining", "target", target)
	close(l.built)
}

// install runs the exact-version npm install of target and the new binary's doctor.
// It reports whether to go on to the restart-or-halt check: true after a clean install
// and after a tripped doctor too (which halts the loop instead); false only when npm
// failed, which is remembered so target is not retried unattended, and the loop keeps
// running.
func (l *Loop) install(target string) bool {
	if l.superseded(target) {
		l.checkSwap() // natural idle: no restart yet, so the swap takes over; a drain already has one
		return true
	}
	if code := l.Upgrade.Install(target); code != 0 {
		slog.Error("upgrade.failed", "to", target, "npm_code", code)
		l.mu.Lock()
		l.upgradeFailed, l.waiting = target, ""
		l.report(l.liveLocked(l.snapshotLocked()))
		l.mu.Unlock()
		return false
	}
	code, fixes := l.Upgrade.Doctor()
	if code == 0 && fixes == 0 {
		l.checkSwap() // clean: end polling now, so poll dispatches nothing under the old binary
		return true
	}
	slog.Error("upgrade.doctor", "to", target, "code", code, "fixes", fixes)
	l.halt(fmt.Sprintf("upgraded to %s but its doctor tripped; run `mugicrew doctor fix`, then start the loop", target))
	return true
}

// superseded is whether the startup binary was swapped since this loop began: another
// loop or a human upgraded the shared npm global meanwhile, so installing target could
// downgrade the machine. It skips the install and turns a drain's restart into a plain
// swap restart, whose Rev swapReady fills from the binary now on disk.
func (l *Loop) superseded(target string) bool {
	if l.SwapPath == "" || !l.swapKnown || !selfexe.Swapped(l.SwapPath, l.swapID) {
		return false
	}
	slog.Info("upgrade.superseded", "to", target)
	l.mu.Lock()
	if l.restart != nil {
		l.restart.Rev = ""
	}
	l.mu.Unlock()
	return true
}

// halt stops the loop without a restart for reason: polling ends, the drain
// returns no Restart, and every report from here carries the reason into the lock
// body, where it outlives the loop until the next start.
func (l *Loop) halt(reason string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stoppedFor != "" {
		return
	}
	l.stoppedFor = reason
	if l.halted != nil {
		close(l.halted)
	}
	l.report(l.liveLocked(l.snapshotLocked()))
}

// upgradeStep is the upgrade drain's step once no effort is in flight, for drain: it
// installs when no session is live and reports ready, waiting while one is. A target
// the binary on disk already supersedes skips both. An npm
// failure reverts the drain to polling, ready false and resumed true.
func (l *Loop) upgradeStep() (ready, resumed bool) {
	l.mu.Lock()
	target := l.upgradeTo
	l.mu.Unlock()
	if target == "" {
		return true, false
	}
	if l.superseded(target) { // a plain swap restart: nothing to install, so no wait for sessions
		l.mu.Lock()
		l.upgradeTo, l.restartTo = "", swapRestartTo
		l.tellLocked(l.snapshotLocked())
		l.mu.Unlock()
		return true, false
	}
	if !l.naturallyIdle() {
		return false, false
	}
	if l.install(target) {
		l.mu.Lock()
		l.upgradeTo = ""
		l.mu.Unlock()
		return true, false
	}
	l.mu.Lock()
	l.restart, l.swapping, l.upgradeTo, l.restartTo = nil, false, "", ""
	l.resumeCause = "upgrade-failed" // upgrade.failed already said why; not a cancel
	l.built = make(chan struct{})
	l.mu.Unlock()
	return false, true
}
