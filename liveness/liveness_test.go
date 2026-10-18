package liveness

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// fakeLocks points lockDir at a fresh temp dir for the test.
func fakeLocks(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "mugicrew-efforts")
	orig := lockDir
	lockDir = dir
	t.Cleanup(func() { lockDir = orig })
	return dir
}

func TestHold(t *testing.T) {
	dir := fakeLocks(t)
	if Held("e1") {
		t.Fatal("no lock file must read dead")
	}
	h, err := Acquire("e1")
	if err != nil {
		t.Fatal(err)
	}
	if !Held("e1") {
		t.Error("held lock must read alive")
	}
	if Held("e2") {
		t.Error("another id must read dead")
	}
	if _, err := Acquire("e1"); err == nil {
		t.Error("second Acquire while held must fail")
	}
	h.Release()
	if Held("e1") {
		t.Error("released lock must read dead")
	}
	if _, err := os.Stat(filepath.Join(dir, "e1.lock")); err != nil {
		t.Errorf("Release must leave the file: %v", err)
	}
	h2, err := Acquire("e1")
	if err != nil {
		t.Fatalf("re-Acquire after Release: %v", err)
	}
	h2.Release()
}

// TestHoldHelper is the holder process of TestSigkilledHolderFreesLockDespiteOrphan, not a test.
func TestHoldHelper(t *testing.T) {
	dir := os.Getenv("LIVENESS_HELPER_DIR")
	if dir == "" {
		t.Skip("helper process only")
	}
	lockDir = dir
	if _, err := Acquire("e1"); err != nil {
		t.Fatal(err)
	}
	child := exec.Command("sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	os.Stdout.WriteString(strconv.Itoa(child.Process.Pid) + "\n")
	select {}
}

// A SIGKILLed holder frees its lock at once, and the child it spawned (which
// would carry an inherited fd) does not keep it held: O_CLOEXEC.
func TestSigkilledHolderFreesLockDespiteOrphan(t *testing.T) {
	dir := fakeLocks(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestHoldHelper$")
	cmd.Env = append(os.Environ(), "LIVENESS_HELPER_DIR="+dir)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	childPid, _ := strconv.Atoi(strings.TrimSpace(line))
	t.Cleanup(func() { syscall.Kill(childPid, syscall.SIGKILL) })
	if !Held("e1") {
		t.Fatal("helper's hold must read alive")
	}
	cmd.Process.Kill()
	cmd.Wait()
	if Held("e1") {
		t.Error("lock still held after holder SIGKILL; orphaned child inherited it")
	}
}
