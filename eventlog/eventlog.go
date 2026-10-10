// Package eventlog is mugicrew's event log: the app's logging convention (ADR
// 0027/0028/0029). Every event is one slog JSONL record appended to
// $(git-common-dir)/mugicrew/events.jsonl, one file shared by every process and
// worktree. msg is the noun.verb event name; pid and proc ride on every record.
//
// main builds the process logger once per verb (Log.Logger) and installs it with
// slog.SetDefault; call sites emit with slog.Info/Warn/Error/Debug, and tests swap
// the default (Spec decision A: no global of our own).
//
// slog.SetDefault also routes stdlib log.Printf here, as an INFO record whose msg
// is free text, not a noun.verb: emit events with slog, never package log.
//
// The log is history (ADR 0033): non-authoritative, so an emit never fails the
// caller. An unopenable path discards the event with a one-time warning.
package eventlog

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/registry"
)

// MaxSize is the rotation cap (ADR 0028): at or past it the live file becomes
// the single .1 backup before the next write.
const MaxSize = 10 << 20

// EffortEnv carries the minted effort id from dispatch into a worktree child; a
// process running inside an effort stamps it on every event (ADR 0029 §2).
const EffortEnv = "MUGICREW_EFFORT"

// Files are the event log's files in append order: the .1 backup, then the live
// file (ADR 0028 §4).
func Files(path string) []string { return []string{path + ".1", path} }

// Log is the rotating writer under the process logger's JSON handler. The zero
// path discards (with the warning).
type Log struct {
	path    string
	why     error        // why path is empty, for the warning
	warn    func(string) // receives the one-time warning; nil drops it
	once    sync.Once
	warning string
	mu      sync.Mutex // guards warning
}

// New is a Log appending to path. warn receives the one-time "event log
// unavailable" warning: CLI verbs print it to stderr; the TUI passes nil (its
// alt-screen owns the terminal) and shows Warning() on its status line instead.
func New(path string, warn func(string)) *Log { return &Log{path: path, warn: warn} }

// Open is the Log for the cwd's repo (Resolve); an unresolvable repo discards.
func Open(warn func(string)) *Log {
	p, err := Resolve()
	l := New(p, warn)
	l.why = err
	return l
}

// Resolve is the event log path for the cwd's repo: absolute, so every linked
// worktree (reconcile children in /tmp/mugicrew-wt-*) resolves the same file.
func Resolve() (string, error) {
	dir, err := git.CommonDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mugicrew", "events.jsonl"), nil
}

// Logger is the process logger for proc (tui/loop/reconcile/signal): JSON records
// through l carrying pid and proc, plus effort inside a worktree child
// (EffortEnv). Records below Info are dropped unless MUGICREW_LOG_LEVEL=debug.
func (l *Log) Logger(proc string) *slog.Logger {
	level := slog.LevelInfo
	if os.Getenv("MUGICREW_LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}
	lg := slog.New(slog.NewJSONHandler(l, &slog.HandlerOptions{Level: level})).With("pid", os.Getpid(), "proc", proc)
	if id := os.Getenv(EffortEnv); id != "" {
		lg = lg.With("effort", id)
	}
	return lg
}

// Write appends one record: slog's handler renders each into one buffer and makes
// one Write, so concurrent O_APPEND writers never interleave (ADR 0027 research).
// Rotation, open and write errors are swallowed (it always reports success): the
// first failure warns once, later ones are silent.
func (l *Log) Write(p []byte) (int, error) {
	if l.path == "" {
		l.fail(l.why)
		return len(p), nil
	}
	// ponytail: two processes crossing the cap at once can both rename, the second
	// clobbering .1 with a near-empty file (ADR 0028). flock on a sibling .lock
	// around stat+rename is the upgrade if that lost backup ever matters.
	if fi, err := os.Stat(l.path); err == nil && fi.Size() >= MaxSize {
		os.Rename(l.path, Files(l.path)[0]) // fail-open: append to the live file anyway
	}
	os.MkdirAll(filepath.Dir(l.path), 0o755) // an error here surfaces as the open's
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		l.fail(err)
		return len(p), nil
	}
	defer f.Close()
	if _, err := f.Write(p); err != nil {
		l.fail(err)
	}
	return len(p), nil
}

func (l *Log) fail(err error) {
	l.once.Do(func() {
		msg := fmt.Sprintf("mugicrew: event log unavailable, events discarded: %v", err)
		l.mu.Lock()
		l.warning = msg
		l.mu.Unlock()
		if l.warn != nil {
			l.warn(msg)
		}
	})
}

// Warning is the one-time warning once it has fired, else "". A nil Log has none.
func (l *Log) Warning() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.warning
}

// Path is the file l appends to, and why it is "" (unresolvable). A nil Log has
// neither.
func (l *Log) Path() (string, error) {
	if l == nil {
		return "", nil
	}
	return l.path, l.why
}

// BeadAttrs are the common attrs naming a bead (ADR 0029 §3): bead, parent,
// crewmate.
func BeadAttrs(b bd.Bead) []any {
	return []any{"bead", b.ID, "parent", b.Parent, "crewmate", owner(b)}
}

// owner is b's registry owner, or "" for an unregistered kind.
func owner(b bd.Bead) string {
	if e, err := registry.Lookup(b.Kind); err == nil {
		return e.Owner
	}
	return ""
}

// Record records an outcome: done at Info, or failed at Error with err, and
// returns err (FR35: err on failures only).
func Record(done, failed string, err error, attrs ...any) error {
	if err != nil {
		slog.Error(failed, withErr(attrs, err)...)
		return err
	}
	slog.Info(done, attrs...)
	return nil
}

// RecordDrafts records an op over creator's Drafts (ADR 0059): done
// (draft.published / draft.deleted) with the ids, or draft.failed at Error, on
// attrs — the op's common attrs (ADR 0029 §3). An op that did nothing (nil ids:
// bd.Publish / bd.DeleteDrafts had no Draft and no handed close or edge) records
// nothing; a publish that landed only a handed close records empty ids. Returns err.
func RecordDrafts(done, creator string, ids []string, err error, attrs ...any) error {
	if err == nil && ids == nil {
		return nil
	}
	return Record(done, "draft.failed", err, append(slices.Clip(attrs), "creator", creator, "ids", ids)...)
}

// RecordBranches records each container-branch delete outcome on attrs
// (deleteLanded's shape): branch.deleted, or branch.kept at Warn with err.
func RecordBranches(outs []bd.BranchOutcome, attrs ...any) {
	for _, o := range outs {
		a := append(slices.Clip(attrs), "branch", o.Branch)
		if o.Err != nil {
			slog.Warn("branch.kept", append(a, "err", o.Err.Error())...)
			continue
		}
		slog.Info("branch.deleted", a...)
	}
}

// RecordClaim records a claim op's outcome err as event (claim.taken /
// claim.released), or claim.failed at Error, and returns err (ADR 0029 §5). by
// duplicates crewmate by design; extra attrs (the busy skip's effort id) ride
// along. bd stays event-blind: its callers record around it.
func RecordClaim(b bd.Bead, event string, err error, extra ...any) error {
	return Record(event, "claim.failed", err, append(append(BeadAttrs(b), extra...), "by", owner(b))...)
}

// RecordCarry records a bd.Carry of pred's Ordering edges to succ (Spec D5):
// carry.done with moved and skipped — at Warn when anything was skipped, the
// event being the per-pair warning — or carry.failed at Error with the moved that
// landed first. A carry that found nothing records nothing. Returns err.
func RecordCarry(pred bd.Bead, succ string, moved []string, skipped []bd.CarrySkip, err error) error {
	if err == nil && moved == nil && skipped == nil {
		return nil
	}
	attrs := append(BeadAttrs(pred), "successor", succ, "moved", moved, "skipped", skipped)
	if err == nil && skipped != nil {
		slog.Warn("carry.done", attrs...)
		return nil
	}
	return Record("carry.done", "carry.failed", err, attrs...)
}

// RecordAbandoned records an in-flight effort left behind by a force stop (loop)
// or a confirmed TUI quit (tui), its claim still in_progress (ADR 0029, 0050, 0051).
// paused says whether its paused:abandoned write landed; error is set when it
// didn't (ADR 0082). branch is its routed branch, kept for the next launch.
func RecordAbandoned(b bd.Bead, effort, branch string, pauseErr error) {
	attrs := append(BeadAttrs(b), "effort", effort, "branch", branch, "paused", pauseErr == nil)
	if pauseErr != nil {
		attrs = append(attrs, "error", pauseErr.Error())
	}
	slog.Warn("effort.abandoned", attrs...)
}

// RecordLand records a crewmate's git.Land of branch into mainline: land.done with the
// landed sha, or land.failed at Error (ADR 0029 §5). Called around the crewmates'
// landBranch seams so git stays event-blind.
// ponytail: effort reaches claim/land events only via MUGICREW_EFFORT (a worktree
// child); an in-process effort that landed or unclaimed would record them without
// it (FR18). No in-process route does today; thread e.attrs through if one ever does.
func RecordLand(b bd.Bead, branch, sha string, err error) {
	attrs := append(BeadAttrs(b), "branch", branch, "into", git.Mainline())
	if err == nil {
		attrs = append(attrs, "sha", sha)
	}
	Record("land.done", "land.failed", err, attrs...)
}

// RecordRevert records a Rollback's git.Revert of target's land (squash) on mainline:
// revert.done with the revert sha, or revert.failed at Error (ADR 0029 §5, ADR 0047).
// A re-run that finds the revert already on main records revert.done with its sha.
// A conflict is the planned fix-forward outcome (ADR 0049), not a failure:
// revert.conflict at Warn with the conflicted paths.
func RecordRevert(b bd.Bead, target, squash, sha string, err error) {
	attrs := append(BeadAttrs(b), "target", target, "squash", squash, "into", git.Mainline())
	var conflict *git.RevertConflict
	if errors.As(err, &conflict) {
		slog.Warn("revert.conflict", append(attrs, "paths", strings.Join(conflict.Paths, ", "))...)
		return
	}
	if err == nil {
		attrs = append(attrs, "sha", sha)
	}
	Record("revert.done", "revert.failed", err, attrs...)
}

// RecordIntegrate records an effort's git.Integrate onto container (Spec D13,
// FR55): integrate.done with the squash sha ("" when nothing was ahead), a
// conflict the caller surfaces as integrate.conflict at Warn with its reason
// (rebase|held|landed), or integrate.failed at Error, reason contended when the
// retries were spent.
func RecordIntegrate(b bd.Bead, container, sha string, retries int, by IntegrateBy, err error) {
	attrs := append(BeadAttrs(b), "container", container, "branch", git.EffortBranch(b.ID), "retries", retries, "by", string(by))
	var conflict *git.IntegrateConflict
	switch {
	case errors.As(err, &conflict):
		slog.Warn("integrate.conflict", append(attrs, "reason", string(conflict.Reason))...)
		return
	case errors.Is(err, git.ErrContended):
		attrs = append(attrs, "reason", git.ReasonContended)
	case err == nil:
		attrs = append(attrs, "sha", sha)
	}
	Record("integrate.done", "integrate.failed", err, attrs...)
}

// IntegrateBy is who ran an integrate, the by its integrate.* event carries
// (Spec FR 55).
type IntegrateBy string

const (
	ByAgent    IntegrateBy = "agent"
	ByBackstop IntegrateBy = "backstop"
	ByHuman    IntegrateBy = "human"
)

// RecordHandoff records a Rollback handing its reverted work to the successor
// Checkpoint Jinbe seeded (ADR 0048): handoff.done with the successor id, or
// handoff.failed at Error.
func RecordHandoff(b bd.Bead, successor string, err error) {
	Record("handoff.done", "handoff.failed", err, append(BeadAttrs(b), "successor", successor)...)
}

// RecordHitlExited records hitl.exited for the un-tmuxed CLI triage session's
// attrs, with its exit (the TUI's tmux sessions record through hitl-run, ADR 0029
// amendment). It stays Info on a non-zero exit or a launch error: the human
// ending, or failing to start, their own session is not a mugicrew failure. err
// still rides along to say why (FR35: err on failures only).
func RecordHitlExited(attrs []any, err error) {
	slog.Info("hitl.exited", withErr(append(attrs, ExitAttrs(err)...), err)...)
}

// RecordHitlDropped records hitl.dropped for b's session (its id and state,
// alive/exited) on the TUI's x drop — the only hitl event the TUI writes (ADR 0029
// amendment). Info even when the drop fails: err rides along to say why.
func RecordHitlDropped(b bd.Bead, session, state string, err error) {
	slog.Info("hitl.dropped", withErr(append(BeadAttrs(b), "session", session, "state", state), err)...)
}

// withErr appends err's attr to attrs when err is set (FR35: err on failures only).
func withErr(attrs []any, err error) []any {
	if err != nil {
		return append(attrs, "err", err.Error())
	}
	return attrs
}

// ExitAttrs are a finished child's exit attrs (Exit): exit, plus signal when one
// killed it.
func ExitAttrs(err error) []any {
	code, sig := Exit(err)
	if sig != "" {
		return []any{"exit", code, "signal", sig}
	}
	return []any{"exit", code}
}

// Exit decodes a finished child's Run/Wait error into its exit attrs: 0 for nil,
// the process's code (-1 when killed, with the signal named), or -1 when it never
// ran (any other error).
func Exit(err error) (code int, signal string) {
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0, ""
	case !errors.As(err, &ee):
		return -1, ""
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		signal = ws.Signal().String()
	}
	return ee.ExitCode(), signal
}
