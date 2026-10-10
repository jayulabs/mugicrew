package eventlog

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/git"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func lines(t *testing.T, path string) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("unparseable record %q: %v", l, err)
		}
		out = append(out, m)
	}
	return out
}

// The path resolves to the git-common-dir from the main checkout AND from a linked
// worktree, so every process (reconcile children in /tmp worktrees included) hits
// one file (ADR 0027 §1); a record written through it lands there.
func TestResolveIsGitCommonDirFromAnyWorktree(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	gitRun(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init")
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-q", "-b", "side", wt)

	want := filepath.Join(repo, ".git", "mugicrew", "events.jsonl")
	if real, err := filepath.EvalSymlinks(repo); err == nil {
		want = filepath.Join(real, ".git", "mugicrew", "events.jsonl")
	}
	for _, dir := range []string{repo, wt} {
		t.Chdir(dir)
		got, err := Resolve()
		if err != nil || got != want {
			t.Fatalf("Resolve from %s = %q, %v; want %q", dir, got, err, want)
		}
	}
	// Still cwd'd in the linked worktree: the record lands in the common dir.
	p, _ := Resolve()
	New(p, nil).Logger("tui").Info("effort.started", "bead", "b1")
	got := lines(t, want)
	if len(got) != 1 || got[0]["msg"] != "effort.started" || got[0]["bead"] != "b1" ||
		got[0]["level"] != "INFO" || got[0]["proc"] != "tui" || got[0]["pid"] == nil {
		t.Errorf("record = %v", got)
	}
}

// Debug is dropped unless MUGICREW_LOG_LEVEL=debug (ADR 0027 §4).
func TestDebugGatedByEnv(t *testing.T) {
	p := filepath.Join(t.TempDir(), "events.jsonl")
	New(p, nil).Logger("loop").Debug("effort.busy")
	New(p, nil).Logger("loop").Info("effort.started")
	t.Setenv("MUGICREW_LOG_LEVEL", "debug")
	New(p, nil).Logger("loop").Debug("effort.busy")
	got := lines(t, p)
	if len(got) != 2 || got[0]["msg"] != "effort.started" || got[1]["msg"] != "effort.busy" {
		t.Errorf("records = %v, want [started, debug busy]", got)
	}
}

// At the cap the live file rotates into exactly one .1, clobbering the old backup
// (ADR 0028 §1-2); the new event starts a fresh live file.
func TestRotatesAtCapIntoOneBackup(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")
	os.WriteFile(p+".1", []byte("old backup\n"), 0o644)
	os.WriteFile(p, make([]byte, MaxSize), 0o644)

	New(p, nil).Logger("loop").Info("effort.started")
	if fi, err := os.Stat(p + ".1"); err != nil || fi.Size() != MaxSize {
		t.Errorf(".1 should be the rotated live file (size %d): %v", MaxSize, err)
	}
	if got := lines(t, p); len(got) != 1 || got[0]["msg"] != "effort.started" {
		t.Errorf("live file after rotation = %v", got)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 2 {
		t.Errorf("want exactly events.jsonl + .1, got %d entries", len(ents))
	}

	// Under the cap: no rotation.
	New(p, nil).Logger("loop").Info("effort.ended")
	if got := lines(t, p); len(got) != 2 {
		t.Errorf("under the cap must append, got %v", got)
	}
}

// An unwritable path discards with a single warning and never panics or errors
// (ADR 0027 §5); an unresolvable (empty) path does the same.
func TestUnwritableDiscardsWithOneWarning(t *testing.T) {
	var warned []string
	warn := func(msg string) { warned = append(warned, msg) }

	blocker := filepath.Join(t.TempDir(), "file")
	os.WriteFile(blocker, nil, 0o644)
	l := New(filepath.Join(blocker, "mugicrew", "events.jsonl"), warn) // parent is a file
	lg := l.Logger("loop")
	lg.Info("effort.started")
	lg.Error("effort.failed")
	if len(warned) != 1 || l.Warning() != warned[0] {
		t.Errorf("want one warning, got %v (Warning()=%q)", warned, l.Warning())
	}

	warned = nil
	New("", warn).Logger("loop").Info("effort.started")
	if len(warned) != 1 {
		t.Errorf("empty path: want one warning, got %v", warned)
	}
}

// A process running inside an effort (a worktree reconcile child) stamps the
// inherited effort id on its events (ADR 0029 §2).
func TestEffortEnvStampedOnEvents(t *testing.T) {
	p := filepath.Join(t.TempDir(), "events.jsonl")
	t.Setenv(EffortEnv, "abcd1234")
	New(p, nil).Logger("reconcile").Info("session.started")
	if got := lines(t, p); got[0]["effort"] != "abcd1234" {
		t.Errorf("record = %v, want effort=abcd1234", got[0])
	}
}

// RecordDrafts records nothing for an op that did nothing (nil ids), done with the
// common attrs for anything else — including a publish that landed only a handed
// close (empty ids) — and draft.failed on an error.
func TestRecordDrafts(t *testing.T) {
	evs := eventlogtest.Capture(t)
	RecordDrafts("draft.published", "mc-a", nil, nil, "bead", "mc-a")
	RecordDrafts("draft.published", "mc-a", []string{}, nil, "bead", "mc-a")
	RecordDrafts("draft.deleted", "mc-a", nil, errors.New("bd down"), "bead", "mc-a")
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"draft.published", "draft.failed"}) {
		t.Fatalf("events = %v", got)
	}
	for _, e := range *evs {
		if e.Attrs["bead"] != "mc-a" || e.Attrs["creator"] != "mc-a" {
			t.Errorf("%s attrs = %v, want bead and creator", e.Name, e.Attrs)
		}
	}
}

// RecordBranches records branch.deleted at Info, or branch.kept at Warn with err,
// per outcome on the common attrs; none records nothing.
func TestRecordBranches(t *testing.T) {
	evs := eventlogtest.Capture(t)
	RecordBranches(nil, "bead", "mc-a")
	RecordBranches([]bd.BranchOutcome{{Branch: "chart/mc-c"}, {Branch: "checkpoint/mc-k", Err: errors.New("has work")}}, "bead", "mc-a")
	if len(*evs) != 2 {
		t.Fatalf("events = %+v", *evs)
	}
	del, kept := (*evs)[0], (*evs)[1]
	if del.Name != "branch.deleted" || del.Level != slog.LevelInfo || del.Attrs["branch"] != "chart/mc-c" || del.Attrs["bead"] != "mc-a" {
		t.Errorf("branch.deleted = %+v", del)
	}
	if kept.Name != "branch.kept" || kept.Level != slog.LevelWarn || kept.Attrs["branch"] != "checkpoint/mc-k" || kept.Attrs["err"] != "has work" {
		t.Errorf("branch.kept = %+v", kept)
	}
}

// RecordLand records land.done with branch/into/sha, or land.failed at Error with err.
func TestRecordLandEvents(t *testing.T) {
	evs := eventlogtest.Capture(t)
	b := bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint}
	RecordLand(b, "checkpoint/ws-cp", "abc123", nil)
	RecordLand(b, "checkpoint/ws-cp", "", &git.RebaseConflict{Base: "base1"})
	if len(*evs) != 2 {
		t.Fatalf("events = %+v", *evs)
	}
	done, failed := (*evs)[0], (*evs)[1]
	if done.Name != "land.done" || done.Level != slog.LevelInfo || done.Attrs["branch"] != "checkpoint/ws-cp" ||
		done.Attrs["into"] != git.Mainline() || done.Attrs["sha"] != "abc123" {
		t.Errorf("land.done = %+v", done)
	}
	if failed.Name != "land.failed" || failed.Level != slog.LevelError || failed.Attrs["into"] != git.Mainline() ||
		failed.Attrs["err"] == nil || failed.Attrs["sha"] != nil {
		t.Errorf("land.failed = %+v", failed)
	}
}

// RecordIntegrate records integrate.done, a planned integrate.conflict at Warn
// with its reason, or integrate.failed at Error, reason contended on spent
// retries (Spec FR55).
func TestRecordIntegrateEvents(t *testing.T) {
	evs := eventlogtest.Capture(t)
	b := bd.Bead{ID: "mc-b", Kind: contract.KindBuild, Parent: "ws-cp"}
	RecordIntegrate(b, "checkpoint/ws-cp", "abc123", 1, "agent", nil)
	RecordIntegrate(b, "checkpoint/ws-cp", "", 0, "human", &git.IntegrateConflict{Reason: "held", Container: "checkpoint/ws-cp", Holder: "/h"})
	RecordIntegrate(b, "checkpoint/ws-cp", "", 3, "agent", git.ErrContended)
	RecordIntegrate(b, "checkpoint/ws-cp", "", 0, "agent", errors.New("boom"))
	if len(*evs) != 4 {
		t.Fatalf("events = %+v", *evs)
	}
	done, conflict, contended, failed := (*evs)[0], (*evs)[1], (*evs)[2], (*evs)[3]
	for _, e := range *evs {
		if e.Attrs["bead"] != "mc-b" || e.Attrs["container"] != "checkpoint/ws-cp" || e.Attrs["branch"] != "effort/mc-b" || e.Attrs["by"] == nil {
			t.Errorf("%s attrs = %+v", e.Name, e.Attrs)
		}
	}
	if done.Name != "integrate.done" || done.Level != slog.LevelInfo || done.Attrs["sha"] != "abc123" || done.Attrs["retries"] != 1 || done.Attrs["by"] != "agent" {
		t.Errorf("integrate.done = %+v", done)
	}
	if conflict.Name != "integrate.conflict" || conflict.Level != slog.LevelWarn || conflict.Attrs["reason"] != "held" || conflict.Attrs["by"] != "human" {
		t.Errorf("integrate.conflict = %+v", conflict)
	}
	if contended.Name != "integrate.failed" || contended.Level != slog.LevelError || contended.Attrs["reason"] != "contended" || contended.Attrs["retries"] != 3 {
		t.Errorf("contended integrate.failed = %+v", contended)
	}
	if failed.Name != "integrate.failed" || failed.Level != slog.LevelError || failed.Attrs["err"] == nil || failed.Attrs["reason"] != nil {
		t.Errorf("integrate.failed = %+v", failed)
	}
}

// RecordRevert records revert.done, a planned fix-forward revert.conflict at Warn
// with its paths (ADR 0049, not paged as a failure), or revert.failed at Error.
func TestRecordRevertEvents(t *testing.T) {
	evs := eventlogtest.Capture(t)
	b := bd.Bead{ID: "ws-rb", Kind: contract.KindRollback}
	RecordRevert(b, "ws-cp", "sq1", "abc123", nil)
	RecordRevert(b, "ws-cp", "sq1", "", &git.RevertConflict{Base: "base1", Paths: []string{"a.go", "b.go"}})
	RecordRevert(b, "ws-cp", "sq1", "", errors.New("boom"))
	if len(*evs) != 3 {
		t.Fatalf("events = %+v", *evs)
	}
	done, conflict, failed := (*evs)[0], (*evs)[1], (*evs)[2]
	for _, e := range *evs {
		if e.Attrs["squash"] != "sq1" {
			t.Errorf("%s squash = %v, want sq1", e.Name, e.Attrs["squash"])
		}
	}
	if done.Name != "revert.done" || done.Level != slog.LevelInfo || done.Attrs["target"] != "ws-cp" || done.Attrs["sha"] != "abc123" {
		t.Errorf("revert.done = %+v", done)
	}
	if conflict.Name != "revert.conflict" || conflict.Level != slog.LevelWarn || conflict.Attrs["paths"] != "a.go, b.go" ||
		conflict.Attrs["err"] != nil || conflict.Attrs["sha"] != nil {
		t.Errorf("revert.conflict = %+v", conflict)
	}
	if failed.Name != "revert.failed" || failed.Level != slog.LevelError || failed.Attrs["err"] == nil {
		t.Errorf("revert.failed = %+v", failed)
	}
}

// RecordClaim records the op's event by the owning crewmate, or claim.failed at
// Error with err, and hands the op's error back.
func TestRecordClaimEvents(t *testing.T) {
	evs := eventlogtest.Capture(t)
	b := bd.Bead{ID: "mc-r", Kind: contract.KindResearch, Parent: "mc-c"}
	RecordClaim(b, "claim.released", nil, "effort", "e1")
	bdErr := errors.New("bd down")
	if err := RecordClaim(b, "claim.released", bdErr); err != bdErr {
		t.Errorf("RecordClaim err = %v, want the op's error", err)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"claim.released", "claim.failed"}) {
		t.Fatalf("events = %v", got)
	}
	if a := (*evs)[0].Attrs; a["by"] != "nami" || a["crewmate"] != "nami" || a["parent"] != "mc-c" || a["effort"] != "e1" {
		t.Errorf("claim.released attrs = %v", a)
	}
	if f := (*evs)[1]; f.Level != slog.LevelError || f.Attrs["err"] != "bd down" {
		t.Errorf("claim.failed = %+v", f)
	}
}

// Exit decodes a child's error the one way every caller reports it: 0, the code,
// the killing signal, or -1 for a child that never ran.
func TestExit(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
		sig  string
	}{
		{nil, 0, ""},
		{exec.Command("sh", "-c", "exit 3").Run(), 3, ""},
		{exec.Command("sh", "-c", "kill -9 $$").Run(), -1, "killed"},
		{exec.Command("/nonexistent/claude").Run(), -1, ""},
	} {
		if code, sig := Exit(tc.err); code != tc.code || sig != tc.sig {
			t.Errorf("Exit(%v) = %d, %q; want %d, %q", tc.err, code, sig, tc.code, tc.sig)
		}
	}
}

// hitl.exited stays Info even for a failed launch, with the exit and err (FR35).
func TestRecordHitlExited(t *testing.T) {
	evs := eventlogtest.Capture(t)
	RecordHitlExited([]any{"session", "s1"}, exec.Command("sh", "-c", "exit 2").Run())
	RecordHitlExited([]any{"session", "s2"}, nil)
	if len(*evs) != 2 {
		t.Fatalf("events = %+v", *evs)
	}
	if e := (*evs)[0]; e.Name != "hitl.exited" || e.Level != slog.LevelInfo || e.Attrs["exit"] != 2 || e.Attrs["err"] != "exit status 2" {
		t.Errorf("failed exit = %+v", e)
	}
	if e := (*evs)[1]; e.Attrs["exit"] != 0 || e.Attrs["err"] != nil {
		t.Errorf("clean exit = %+v", e)
	}
}

// RecordCarry: carry.done at Info with moved only, Warn when anything was
// skipped, nothing for an empty carry, carry.failed at Error with err and the
// partial moved; it returns the err it was given (Spec D5).
func TestRecordCarry(t *testing.T) {
	evs := eventlogtest.Capture(t)
	sp := bd.Bead{ID: "ws-sp", Kind: contract.KindSpec}
	skip := []bd.CarrySkip{{Bead: "mc-y", Reason: "type-clash"}}
	boom := errors.New("bd down")
	if err := RecordCarry(sp, "ws-cp", nil, nil, nil); err != nil {
		t.Errorf("empty carry returned %v", err)
	}
	RecordCarry(sp, "ws-cp", []string{"mc-x"}, nil, nil)
	RecordCarry(sp, "ws-cp", []string{"mc-x"}, skip, nil)
	if err := RecordCarry(sp, "ws-cp", []string{"mc-x"}, nil, boom); err != boom {
		t.Errorf("failed carry returned %v, want its err", err)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"carry.done", "carry.done", "carry.failed"}) {
		t.Fatalf("events = %v", got)
	}
	info, warn, failed := (*evs)[0], (*evs)[1], (*evs)[2]
	for _, e := range *evs {
		if e.Attrs["bead"] != "ws-sp" || e.Attrs["successor"] != "ws-cp" || !reflect.DeepEqual(e.Attrs["moved"], []string{"mc-x"}) {
			t.Errorf("%s attrs = %v", e.Name, e.Attrs)
		}
	}
	if info.Level != slog.LevelInfo || warn.Level != slog.LevelWarn || failed.Level != slog.LevelError {
		t.Errorf("levels = %v %v %v", info.Level, warn.Level, failed.Level)
	}
	if !reflect.DeepEqual(warn.Attrs["skipped"], skip) {
		t.Errorf("skipped = %v, want %v", warn.Attrs["skipped"], skip)
	}
	if failed.Attrs["err"] != "bd down" || info.Attrs["err"] != nil {
		t.Errorf("err attr: failed=%v info=%v", failed.Attrs["err"], info.Attrs["err"])
	}
}
