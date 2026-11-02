package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/selfexe"
)

// SpawnFollower (the CLI triage path's spawn too) starts exactly when a
// destination is named, with D4's argv (Spec mc-11b8 FR21).
func TestSpawnFollower(t *testing.T) {
	orig := startFollower
	defer func() { startFollower = orig }()
	var got [][]string
	startFollower = func(_ string, args []string) error { got = append(got, args); return nil }
	attrs := []any{"bead", "b", "session", "s", "profile", "p"}
	SpawnFollower(attrs, contract.Metered{}, "/log/events.jsonl")
	if len(got) != 0 {
		t.Fatalf("spawned without a destination: %v", got)
	}
	SpawnFollower(attrs, contract.Metered{ContinuedIn: "d", Last: contract.Cost{Known: true, USD: 2}}, "/log/events.jsonl")
	if len(got) != 1 {
		t.Fatalf("spawns = %v", got)
	}
	argv := strings.Join(got[0], " ")
	for _, want := range []string{"hitl-follow", "--bead=b", "--session=s", "--profile=p", "--continued_in=d", `"USD":2`, "--exited_at=", "--events=/log/events.jsonl"} {
		if !strings.Contains(argv, want) {
			t.Errorf("argv %q lacks %q", argv, want)
		}
	}
}

// The real startFollower execs the startup path with the follower's argv, its
// output landing in the .out file.
func TestStartFollowerRunsStartupPath(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "mugicrew")
	os.WriteFile(stub, []byte("#!/bin/sh\necho \"$@\"\n"), 0o755)
	defer selfexe.Stub(stub)()
	outPath := filepath.Join(dir, "hitl-follow.out")
	if err := startFollower(outPath, []string{"hitl-follow", "--bead=b"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, _ := os.ReadFile(outPath); strings.TrimSpace(string(b)) == "hitl-follow --bead=b" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	b, _ := os.ReadFile(outPath)
	t.Fatalf("out = %q; want the stub's argv", b)
}

// No startup path (an error or empty) is an error and spawns nothing.
func TestStartFollowerWithoutStartupPath(t *testing.T) {
	orig := selfexe.Path
	defer func() { selfexe.Path = orig }()
	for name, path := range map[string]func() (string, error){
		"error": func() (string, error) { return "", errors.New("gone") },
		"empty": func() (string, error) { return "", nil },
	} {
		selfexe.Path = path
		outPath := filepath.Join(t.TempDir(), "out")
		if err := startFollower(outPath, []string{"hitl-follow"}); err == nil {
			t.Errorf("%s: want an error", name)
		}
		if _, err := os.Stat(outPath); err == nil {
			t.Errorf("%s: spawned (out file created)", name)
		}
	}
}
