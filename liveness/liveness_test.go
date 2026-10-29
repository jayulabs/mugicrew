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
	"time"
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
	// A pending timer keeps the runtime's deadlock check quiet (select {} would
	// crash the helper and free the lock); the bound matches the sleep child.
	time.Sleep(time.Minute)
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
	t.Cleanup(func() { cmd.Process.Kill() })
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	childPid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || childPid <= 0 {
		// Never reach the kill cleanup with pid 0: it would signal our process group.
		t.Fatalf("helper's first line %q is not a positive pid (err %v)", line, err)
	}
	t.Cleanup(func() { syscall.Kill(childPid, syscall.SIGKILL) })
	if !Held("e1") {
		t.Fatal("helper's hold must read alive")
	}
	cmd.Process.Kill()
	cmd.Wait()
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Fatalf("helper must end by the test's SIGKILL, got %v", cmd.ProcessState)
	}
	if Held("e1") {
		t.Error("lock still held after holder SIGKILL; orphaned child inherited it")
	}
}

// cloexec reports whether fd carries FD_CLOEXEC.
func cloexec(t *testing.T, fd int) bool {
	t.Helper()
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_GETFD, 0)
	if errno != 0 {
		t.Fatal(errno)
	}
	return flags&syscall.FD_CLOEXEC != 0
}

// Adopt re-arms O_CLOEXEC on an inherited (non-CLOEXEC) fd, and Release on the
// adopted Hold frees the lock.
func TestAdopt(t *testing.T) {
	fakeLocks(t)
	h, err := Acquire("e1")
	if err != nil {
		t.Fatal(err)
	}
	fd, err := syscall.Dup(int(h.File().Fd()))
	if err != nil {
		t.Fatal(err)
	}
	if cloexec(t, fd) {
		t.Fatal("dup'd fd should model an inherited one: no CLOEXEC")
	}
	a := Adopt(fd, "e1")
	if !cloexec(t, int(a.File().Fd())) {
		t.Error("Adopt must set FD_CLOEXEC")
	}
	h.Release()
	if !Held("e1") {
		t.Error("adopted hold must keep the lock after the original closes")
	}
	a.Release()
	if Held("e1") {
		t.Error("Release on an adopted Hold must free the lock")
	}
}

// A hold handed to a child through ExtraFiles outlives the parent's copy and
// frees when the child exits.
func TestHoldHandedToChild(t *testing.T) {
	fakeLocks(t)
	h, err := Acquire("e1")
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command("sleep", "60")
	child.ExtraFiles = []*os.File{h.File()}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { child.Process.Kill(); child.Wait() })
	h.Release()
	if !Held("e1") || !HeldIn(os.Getenv("BEADS_DIR"), "e1") {
		t.Fatal("child's inherited fd must keep the effort held")
	}
	child.Process.Kill()
	child.Wait()
	if Held("e1") {
		t.Error("lock still held after the child exited")
	}
}

func TestHoldsEffort(t *testing.T) {
	for _, c := range []struct {
		argv []string
		want bool
	}{
		{[]string{"/bin/mugicrew", "reconcile", "b1"}, true},
		{[]string{"/bin/mugicrew", "-C", "/x", "run", "b1", "--lock-fd=3"}, true},
		{[]string{"/bin/mugicrew", "transcript", "b1"}, false},
		{[]string{"/bin/mugicrew", "reconcile", "b10"}, false},
		{[]string{"claude", "--name", "b1"}, true},
		{nil, false},
	} {
		if got := HoldsEffort(c.argv, "b1"); got != c.want {
			t.Errorf("HoldsEffort(%v) = %v, want %v", c.argv, got, c.want)
		}
	}
}

// Holding is true from Acquire (or Adopt) until Release, and only for the hold's own
// id: a write serialized by the dispatcher hold names it through this.
func TestHoldingSpansTheHold(t *testing.T) {
	t.Setenv("BEADS_DIR", t.TempDir())
	if Holding("mc-h") {
		t.Fatal("Holding before Acquire")
	}
	h, err := Acquire("mc-h")
	if err != nil {
		t.Fatal(err)
	}
	if !Holding("mc-h") || Holding("mc-other") {
		t.Errorf("Holding(mc-h) = %v, Holding(mc-other) = %v; want true, false", Holding("mc-h"), Holding("mc-other"))
	}
	h.Release()
	if Holding("mc-h") {
		t.Error("Holding after Release")
	}
}
