package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
)

// transcriptWorkspace is a git repo whose event log and claude config dir hold two
// sessions of mc-t (s1111111 ended, s1222222 pruned), with bd faked.
func transcriptWorkspace(t *testing.T) {
	t.Helper()
	ws := t.TempDir()
	if err := exec.Command("git", "init", "-q", ws).Run(); err != nil {
		t.Fatal(err)
	}
	t.Chdir(ws)
	t.Setenv("BEADS_DIR", filepath.Join(ws, ".beads"))
	os.MkdirAll(filepath.Join(ws, ".git", "mugicrew"), 0o755)
	os.WriteFile(filepath.Join(ws, ".git", "mugicrew", "events.jsonl"), []byte(
		`{"time":"2026-10-15T08:00:00Z","msg":"session.started","bead":"mc-t","session":"s1111111","kind":"build","crewmate":"franky"}
{"time":"2026-10-15T08:00:05Z","msg":"session.ended","bead":"mc-t","session":"s1111111","exit":0,"dur_ms":5000}
{"time":"2026-10-15T09:00:00Z","msg":"session.started","bead":"mc-t","session":"s1222222","kind":"build","crewmate":"franky"}
{"time":"2026-10-15T09:00:05Z","msg":"session.ended","bead":"mc-t","session":"s1222222","exit":0,"dur_ms":5000}
`), 0o644)
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	os.MkdirAll(filepath.Join(cfg, "projects", "p"), 0o755)
	os.WriteFile(filepath.Join(cfg, "projects", "p", "s1111111.jsonl"), []byte(
		`{"type":"user","timestamp":"2026-10-15T08:00:00Z","message":{"content":"go"}}
{"type":"assistant","timestamp":"2026-10-15T08:00:01Z","message":{"content":[{"type":"tool_use","id":"a","name":"Bash","input":{"command":"ls"}}]}}
{"type":"user","timestamp":"2026-10-15T08:00:02Z","message":{"content":[{"type":"tool_result","tool_use_id":"a","content":"x"}]}}
`), 0o644)
	old := showBead
	t.Cleanup(func() { showBead = old })
	showBead = func(id string) (bd.Bead, error) {
		if id == "mc-nope" {
			return bd.Bead{}, errors.New("not found")
		}
		return bd.Bead{ID: id}, nil
	}
}

func TestTranscriptVerb(t *testing.T) {
	transcriptWorkspace(t)
	run := func(args ...string) (string, error) {
		var b bytes.Buffer
		err := runTranscript(args, &b)
		return b.String(), err
	}
	out, err := run("mc-t")
	if err != nil || strings.Count(out, "AFK") != 2 || strings.Contains(out, "\x1b") || !strings.Contains(out, "Bash ls ✓ 1.0s") || !strings.Contains(out, "transcript pruned") {
		t.Errorf("piped all = %q, %v", out, err)
	}
	if strings.Index(out, "s1111111"[:8]) > strings.Index(out, "s1222222"[:8]) {
		t.Errorf("not oldest first: %q", out)
	}
	if out, err = run("--last", "--no-follow", "--no-pager", "mc-t"); err != nil || strings.Count(out, "AFK") != 1 || !strings.Contains(out, "s1222222") {
		t.Errorf("--last = %q, %v", out, err)
	}
	if out, err = run("--session", "s11", "mc-t"); err != nil || strings.Count(out, "AFK") != 1 || !strings.Contains(out, "Bash") {
		t.Errorf("--session s11 = %q, %v", out, err)
	}
	for name, args := range map[string][]string{
		"both flags":          {"--session", "s11", "--last", "mc-t"},
		"ambiguous":           {"--session", "s1", "mc-t"},
		"unknown":             {"--session", "zz", "mc-t"},
		"handoff destination": {"--session", "dest", "mc-t"}, // not a recorded id: read as unknown
		"no bead":             {"mc-nope"},
		"no arg":              {},
	} {
		if out, err := run(args...); err == nil || out != "" {
			t.Errorf("%s: out %q err %v, want an error and no output", name, out, err)
		}
	}
	if _, err := run("mc-nope"); err == nil || err.Error() != "mugicrew: no bead mc-nope" {
		t.Errorf("unknown bead err = %v", err)
	}
	if out, err = run("mc-quiet"); err != nil || out != "no sessions recorded for mc-quiet\n" {
		t.Errorf("no sessions = %q, %v", out, err)
	}
}

func TestTranscriptVerbFollowsLiveSession(t *testing.T) {
	transcriptWorkspace(t)
	old := followPoll
	followPoll = 5 * time.Millisecond
	t.Cleanup(func() { followPoll = old })
	logPath := filepath.Join(".git", "mugicrew", "events.jsonl")
	appendLog := func(s string) {
		f, _ := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
		f.WriteString(s)
		f.Close()
	}
	appendLog(`{"time":"2026-10-15T10:00:00Z","msg":"session.started","bead":"mc-l","session":"s1333333","kind":"build"}` + "\n")
	cfg := os.Getenv("CLAUDE_CONFIG_DIR")
	os.WriteFile(filepath.Join(cfg, "projects", "p", "s1333333.jsonl"), []byte(
		`{"type":"assistant","timestamp":"2026-10-15T10:00:01Z","message":{"content":[{"type":"tool_use","id":"a","name":"Bash","input":{"command":"ls"}}]}}`+"\n"), 0o644)

	var snap bytes.Buffer
	if err := runTranscript([]string{"--no-follow", "mc-l"}, &snap); err != nil || !strings.Contains(snap.String(), "Bash ls") || !strings.Contains(snap.String(), "· no result") || !strings.Contains(snap.String(), "no end recorded") {
		t.Fatalf("--no-follow = %q, %v", snap.String(), err)
	}

	done := make(chan string)
	go func() {
		var b bytes.Buffer
		runTranscript([]string{"mc-l"}, &b)
		done <- b.String()
	}()
	select {
	case <-done:
		t.Fatal("follow returned before the session ended")
	case <-time.After(100 * time.Millisecond):
	}
	appendLog(`{"time":"2026-10-15T10:00:09Z","msg":"session.ended","bead":"mc-l","session":"s1333333","exit":0,"dur_ms":9000}` + "\n")
	select {
	case out := <-done:
		if strings.Count(out, "Bash ls") != 1 || !strings.Contains(out, "· no result") {
			t.Errorf("follow = %q", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("follow did not stop at session.ended")
	}
}

func TestTranscriptPager(t *testing.T) {
	transcriptWorkspace(t)
	tty, err := os.Create(filepath.Join(t.TempDir(), "tty"))
	if err != nil {
		t.Fatal(err)
	}
	oldTTY, oldPager := isTerminal, startPager
	t.Cleanup(func() { isTerminal, startPager = oldTTY, oldPager })
	var cmds []string
	var piped bytes.Buffer
	startPager = func(cmd string, out *os.File) (io.Writer, func() error, error) {
		cmds = append(cmds, cmd)
		return &piped, func() error { return nil }, nil
	}
	isTerminal = func(*os.File) bool { return true }

	t.Setenv("PAGER", "")
	if err := runTranscript([]string{"mc-t"}, tty); err != nil || len(cmds) != 1 || cmds[0] != "less -FRX" {
		t.Fatalf("default pager: cmds %q, %v", cmds, err)
	}
	if !strings.Contains(piped.String(), "\x1b[") {
		t.Errorf("paged view lost its colour: %q", piped.String())
	}
	t.Setenv("PAGER", "more")
	cmds = nil
	if err := runTranscript([]string{"mc-t"}, tty); err != nil || len(cmds) != 1 || cmds[0] != "more" {
		t.Errorf("$PAGER: cmds %q, %v", cmds, err)
	}
	cmds = nil
	if err := runTranscript([]string{"--no-pager", "mc-t"}, tty); err != nil || len(cmds) != 0 {
		t.Errorf("--no-pager: cmds %q, %v", cmds, err)
	}
	isTerminal = func(*os.File) bool { return false }
	if err := runTranscript([]string{"mc-t"}, tty); err != nil || len(cmds) != 0 {
		t.Errorf("not a tty: cmds %q, %v", cmds, err)
	}
}

// In the Watch pane a finished view must not page through less -F, which quits on one
// screen and closes the pane; the no-sessions answer returns off a tty stdin (tests).
func TestTranscriptWatchPane(t *testing.T) {
	transcriptWorkspace(t)
	t.Setenv("PAGER", "")
	t.Setenv(watchPaneEnv, "1")
	if got := pagerCmd(); got != "less -RX" {
		t.Errorf("pane pager = %q, want less -RX", got)
	}
	var b bytes.Buffer
	if err := runTranscript([]string{"mc-quiet"}, &b); err != nil || !strings.Contains(b.String(), "no sessions recorded") {
		t.Errorf("no sessions = %q, %v", b.String(), err)
	}
	b.Reset()
	if err := runTranscript([]string{"mc-nope"}, &b); err == nil || !strings.Contains(b.String(), "mc-nope") {
		t.Errorf("error not shown in pane: %q, %v", b.String(), err)
	}
}

func TestTranscriptPrunedExitsZero(t *testing.T) {
	transcriptWorkspace(t)
	var b bytes.Buffer
	if err := runTranscript([]string{"--session", "s1222", "mc-t"}, &b); err != nil || !strings.Contains(b.String(), "transcript pruned (claude keeps ~30 days) · from the event log:") {
		t.Errorf("pruned = %q, %v", b.String(), err)
	}
}

func TestSessionEnded(t *testing.T) {
	log := filepath.Join(t.TempDir(), "events.jsonl")
	ev := func(msg, sess string) string {
		return `{"time":"2026-10-14T00:00:00Z","level":"INFO","msg":"` + msg + `","session":"` + sess + `"}` + "\n"
	}
	write := func(s string) { os.WriteFile(log, []byte(s), 0o644) }

	ended := sessionEnder(log, "a")
	if ended() {
		t.Error("a missing log stopped the watch")
	}
	write(ev("session.started", "a") + ev("session.ended", "b"))
	if ended() {
		t.Error("another session's end stopped the watch")
	}
	f, _ := os.OpenFile(log, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(strings.TrimSuffix(ev("session.ended", "a"), "\n"))
	if ended() {
		t.Error("a torn line stopped the watch")
	}
	f.WriteString("\n")
	if !ended() {
		t.Error("appended session.ended did not stop the watch")
	}
	write(ev("session.failed", "a")) // shorter than before: rotated, re-read whole
	if !sessionEnder(log, "a")() {
		t.Error("session.failed did not stop the watch")
	}
	write(ev("hitl.exited", "a"))
	if !sessionEnder(log, "a")() {
		t.Error("hitl.exited did not stop the watch")
	}
	write(ev("hitl.failed", "a") + ev("hitl.resumed", "a"))
	if sessionEnder(log, "a")() {
		t.Error("a session resumed after its end was taken as over")
	}
}

func TestTranscriptBdErrorPassesThrough(t *testing.T) {
	transcriptWorkspace(t)
	showBead = func(string) (bd.Bead, error) { return bd.Bead{}, errors.New("bd show: timeout") }
	err := runTranscript([]string{"mc-t"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "no bead") {
		t.Errorf("err = %v", err)
	}
}
