package looplock

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func tempPath(t *testing.T) string {
	return filepath.Join(t.TempDir(), "mugicrew", "loop.lock")
}

func TestAcquireRefusesSecondHolderThenReadsOff(t *testing.T) {
	p := tempPath(t)
	if st, err := Read(p); err != nil || st.State != Off {
		t.Fatalf("no file: Read = %+v, %v; want off", st, err)
	}
	lk, err := Acquire(p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	st, err := Read(p)
	if err != nil || !reflect.DeepEqual(st, Status{State: On, Pid: os.Getpid()}) {
		t.Fatalf("held: Read = %+v, %v; want on (pid %d)", st, err, os.Getpid())
	}
	_, err = Acquire(p, "", "", "") // a second fd conflicts even in one process (flock is per open file)
	var held *HeldError
	if !errors.As(err, &held) || !reflect.DeepEqual(held.Status, st) {
		t.Fatalf("second Acquire = %v; want HeldError %+v", err, st)
	}
	if want := "loop is already on (pid " + strconv.Itoa(os.Getpid()) + ")"; err.Error() != want {
		t.Fatalf("refusal = %q; want %q", err, want)
	}
	lk.Close()
	if st, err := Read(p); err != nil || st.State != Off {
		t.Fatalf("released: Read = %+v, %v; want off", st, err)
	}
}

// Write rewrites the body a reader sees: a draining loop reports its in-flight
// titles, and a refused second loop is told it is draining.
func TestWriteDraining(t *testing.T) {
	p := tempPath(t)
	lk, err := Acquire(p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer lk.Close()
	titles := []string{"Build: a", "Build: b", strings.Repeat("long title ", 100)} // past any small read buffer
	if err := lk.Write(Status{State: Draining, Inflight: titles}); err != nil {
		t.Fatal(err)
	}
	want := Status{State: Draining, Pid: os.Getpid(), Inflight: titles}
	if st, err := Read(p); err != nil || !reflect.DeepEqual(st, want) {
		t.Fatalf("Read = %+v, %v; want %+v", st, err, want)
	}
	if _, err := Acquire(p, "", "", ""); err == nil || err.Error() != "loop is draining (pid "+strconv.Itoa(os.Getpid())+")" {
		t.Fatalf("Acquire while draining = %v; want a draining refusal", err)
	}
}

// A shrinking body never leaves a torn tail: the rewrite covers every old byte,
// so a reader at any instant decodes a whole body, not on/pid 0.
func TestWriteShrinkCoversOldBody(t *testing.T) {
	p := tempPath(t)
	lk, err := Acquire(p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer lk.Close()
	lk.Write(Status{State: Draining, Inflight: []string{"Build: a", "Build: b", "Build: c"}})
	before, _ := os.ReadFile(p)
	if err := lk.Write(Status{State: Draining, Inflight: []string{"Build: a"}}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(p)
	if len(after) < len(before) {
		t.Fatalf("body shrank %d -> %d: a reader between write and trim sees a torn tail", len(before), len(after))
	}
	want := Status{State: Draining, Pid: os.Getpid(), Inflight: []string{"Build: a"}}
	if st, err := Read(p); err != nil || !reflect.DeepEqual(st, want) {
		t.Fatalf("Read = %+v, %v; want %+v", st, err, want)
	}
}

// A lockfile left by a killed loop still says on, but nobody holds the lock.
func TestStaleLockfileReadsOff(t *testing.T) {
	p := tempPath(t)
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(`{"pid":4242,"state":"on"}`), 0o644)
	if st, err := Read(p); err != nil || st.State != Off {
		t.Fatalf("Read = %+v, %v; want off", st, err)
	}
	lk, err := Acquire(p, "", "", "")
	if err != nil {
		t.Fatalf("Acquire over a stale file = %v", err)
	}
	lk.Close()
}

// The lock fd is close-on-exec: a child spawned while the lock is held does not
// keep it held once the loop lets go.
func TestChildDoesNotInheritLock(t *testing.T) {
	p := tempPath(t)
	lk, err := Acquire(p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command("sleep", "5")
	if err := child.Start(); err != nil {
		t.Skip("no sleep binary:", err)
	}
	defer child.Process.Kill()
	lk.Close()
	if st, err := Read(p); err != nil || st.State != Off {
		t.Fatalf("Read with child alive = %+v, %v; want off", st, err)
	}
}

// Pid reads the body's pid without taking the lock: the holder's pid while held,
// 0 with no file.
func TestPidReadsBodyWithoutProbing(t *testing.T) {
	p := tempPath(t)
	if got := Pid(p); got != 0 {
		t.Fatalf("no file: Pid = %d; want 0", got)
	}
	lk, err := Acquire(p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer lk.Close()
	if got := Pid(p); got != os.Getpid() {
		t.Fatalf("held: Pid = %d; want %d", got, os.Getpid())
	}
}

// The body carries the pinned build (ADR 0060 §5): Acquire writes rev and
// store_path, every Write keeps the store path and writes the rev, restart_to and
// failed_rev it is given (ADR 0063), and Read returns them; empty ones are omitted.
func TestAcquireWritesBuildAndWriteRoundTrips(t *testing.T) {
	p := tempPath(t)
	lk, err := Acquire(p, "0123456789abcdef", "/nix/store/x-mugicrew/bin/mugicrew", "")
	if err != nil {
		t.Fatal(err)
	}
	defer lk.Close()
	want := Status{State: On, Pid: os.Getpid(), Rev: "0123456789abcdef", StorePath: "/nix/store/x-mugicrew/bin/mugicrew"}
	if st, err := Read(p); err != nil || !reflect.DeepEqual(st, want) {
		t.Fatalf("on: Read = %+v, %v; want %+v", st, err, want)
	}
	for _, w := range []Status{
		{State: Draining, Rev: "0123456789abcdef", Inflight: []string{"Build: a"}, RestartTo: "fedcba9876543210"},
		{State: On, Rev: "0123456789abcdef", FailedRev: "fedcba9876543210"},
		{State: On, Rev: "fedcba9876543210"},
	} {
		if err := lk.Write(w); err != nil {
			t.Fatal(err)
		}
		w.Pid, w.StorePath = os.Getpid(), want.StorePath
		if st, err := Read(p); err != nil || !reflect.DeepEqual(st, w) {
			t.Fatalf("Read = %+v, %v; want %+v", st, err, w)
		}
	}
	body, _ := os.ReadFile(p)
	for _, k := range []string{"restart_to", "failed_rev", "inflight"} {
		if strings.Contains(string(body), k) {
			t.Fatalf("body %q; want empty %s omitted", body, k)
		}
	}
}

// A watched loop's body says watch: true and names its source dir from Acquire
// on, through every Write (Spec FR23, FR26); an unwatched one omits both.
func TestBodyCarriesWatch(t *testing.T) {
	p := tempPath(t)
	lk, err := Acquire(p, "0123456789abcdef", "/nix/store/x/bin/mugicrew", "/src/oe/.git")
	if err != nil {
		t.Fatal(err)
	}
	defer lk.Close()
	if st, _ := Read(p); !st.Watch || st.Source != "/src/oe/.git" {
		t.Fatalf("Acquire: Read = %+v; want watch from /src/oe/.git", st)
	}
	lk.Write(Status{State: Draining, Rev: "0123456789abcdef"})
	if st, _ := Read(p); !st.Watch || st.Source != "/src/oe/.git" {
		t.Fatalf("Write: Read = %+v; want watch and source kept", st)
	}
	if body, _ := os.ReadFile(p); !strings.Contains(string(body), `"watch":true,"source":"/src/oe/.git"`) {
		t.Fatalf("body %q; want watch: true and its source", body)
	}
	q := tempPath(t)
	un, err := Acquire(q, "0123456789abcdef", "/nix/store/x/bin/mugicrew", "")
	if err != nil {
		t.Fatal(err)
	}
	defer un.Close()
	if body, _ := os.ReadFile(q); strings.Contains(string(body), "watch") || strings.Contains(string(body), "source") {
		t.Fatalf("unwatched body %q; want watch and source omitted", body)
	}
}

// A holder that has locked but not yet written its body reads on, pid 0, no build.
func TestUnwrittenBodyReadsOnWithoutBuild(t *testing.T) {
	p := tempPath(t)
	os.MkdirAll(filepath.Dir(p), 0o755)
	f, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if st, err := Read(p); err != nil || !reflect.DeepEqual(st, Status{State: On}) {
		t.Fatalf("Read = %+v, %v; want on, pid 0, no build", st, err)
	}
}

// TestMain runs the fd-handoff helper images (see TestExecHandsLockToNewImage)
// instead of the tests when its env marker is set.
func TestMain(m *testing.M) {
	switch p := os.Getenv("LOOPLOCK_TEST_PATH"); {
	case p == "":
	case len(os.Args) == 1: // the old image: take the lock, exec onto the new one
		lk, err := Acquire(p, "old", "/old", "")
		if err != nil {
			panic(err)
		}
		fd, err := lk.InheritFd()
		if err != nil {
			panic(err)
		}
		panic(syscall.Exec(os.Args[0], []string{os.Args[0], "--lock-fd=" + strconv.Itoa(fd)}, os.Environ()))
	default: // the new image: adopt the fd, rewrite the body, hold until stdin closes
		fd, _ := strconv.Atoi(strings.TrimPrefix(os.Args[1], "--lock-fd="))
		if _, err := Adopt(fd, p, "new", "/new", ""); err != nil {
			panic(err)
		}
		os.Stdout.WriteString("adopted\n")
		io.ReadAll(os.Stdin)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// The lock survives an exec (ADR 0062 §5): the holder clears close-on-exec and
// execs a new image that adopts the fd. The lock never lapses, so it reads as held
// by the same pid with the new body, and a racing Acquire is refused.
func TestExecHandsLockToNewImage(t *testing.T) {
	p := tempPath(t)
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "LOOPLOCK_TEST_PATH="+p)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	if line, err := bufio.NewReader(stdout).ReadString('\n'); line != "adopted\n" {
		t.Fatalf("new image said %q, %v; want adopted", line, err)
	}
	want := Status{State: On, Pid: cmd.Process.Pid, Rev: "new", StorePath: "/new"}
	if st, err := Read(p); err != nil || !reflect.DeepEqual(st, want) {
		t.Fatalf("Read = %+v, %v; want %+v", st, err, want)
	}
	var held *HeldError
	if _, err := Acquire(p, "", "", ""); !errors.As(err, &held) {
		t.Fatalf("racing Acquire = %v; want HeldError", err)
	}
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if st, err := Read(p); err != nil || st.State != Off {
		t.Fatalf("after exit: Read = %+v, %v; want off", st, err)
	}
}

// Adopt refuses an fd that does not hold the flock on loop.lock: an unlocked fd
// on the file, a locked fd on some other file, and an unlocked fd while another
// fd holds it.
func TestAdoptRefusesFdNotHoldingLock(t *testing.T) {
	p := tempPath(t)
	os.MkdirAll(filepath.Dir(p), 0o755)
	f, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := Adopt(int(f.Fd()), p, "", "", ""); err == nil {
		t.Fatal("Adopt of an unlocked fd: want a refusal")
	}
	other, err := Acquire(tempPath(t), "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := Adopt(int(other.f.Fd()), p, "", "", ""); err == nil {
		t.Fatal("Adopt of another file's locked fd: want a refusal")
	}
	if st, err := Read(p); err != nil || st.State != Off {
		t.Fatalf("after refusals: Read = %+v, %v; want off", st, err)
	}
	lk, err := Acquire(p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer lk.Close()
	if _, err := Adopt(int(f.Fd()), p, "", "", ""); err == nil {
		t.Fatal("Adopt of an unlocked fd beside the holder: want a refusal")
	}
}

// The lock is keyed on the Bead Graph (Spec FR10): two Workspaces whose
// BEADS_DIR names one graph, even by different paths, contend on one lock;
// a distinct graph takes its own.
func TestResolveKeysTheLockOnTheBeadGraph(t *testing.T) {
	graph := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(graph, alias); err != nil {
		t.Fatal(err)
	}
	resolveIn := func(beadsDir string) string {
		t.Helper()
		ws := t.TempDir()
		if out, err := exec.Command("git", "init", "-q", ws).CombinedOutput(); err != nil {
			t.Fatalf("git init: %v %s", err, out)
		}
		t.Chdir(ws)
		t.Setenv("BEADS_DIR", beadsDir)
		p, err := Resolve(filepath.Join(ws, ".git"))
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	lk, err := Acquire(resolveIn(graph), "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer lk.Close()
	var held *HeldError
	if _, err := Acquire(resolveIn(alias), "", "", ""); !errors.As(err, &held) {
		t.Fatalf("second Workspace on the same graph: Acquire = %v; want held", err)
	}
	other, err := Acquire(resolveIn(t.TempDir()), "", "", "")
	if err != nil {
		t.Fatalf("a distinct graph: Acquire = %v; want its own lock", err)
	}
	other.Close()
	t.Setenv("BEADS_DIR", "")
	if _, err := Resolve(t.TempDir()); err == nil {
		t.Fatal("no BEADS_DIR: Resolve = nil; want an error")
	}
}

// A pre-ADR-0089 loop holds <common>/mugicrew/loop.lock and exports no BEADS_DIR:
// while it holds that lock Resolve names it, so status and start
// see the live loop, and its restart onto this image adopts the fd it inherited.
// Once unheld, the lock is the graph's again.
func TestResolveFollowsAHeldLegacyLock(t *testing.T) {
	common, graph := t.TempDir(), t.TempDir()
	legacy := filepath.Join(common, "mugicrew", "loop.lock")
	t.Setenv("BEADS_DIR", "")
	old, err := Acquire(legacy, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if p, err := Resolve(common); err != nil || p != legacy {
		t.Fatalf("legacy held, no BEADS_DIR: Resolve = %q, %v; want %q", p, err, legacy)
	}
	fd, err := old.InheritFd() // the restart's exec hands this to the new image
	if err != nil {
		t.Fatal(err)
	}
	p, _ := Resolve(common)
	lk, err := Adopt(fd, p, "abc", "", "")
	if err != nil {
		t.Fatalf("restart onto this image: Adopt = %v; want the legacy lock adopted", err)
	}
	lk.Close()
	t.Setenv("BEADS_DIR", graph)
	if p, err := Resolve(common); err != nil || p != filepath.Join(graph, "mugicrew-loop.lock") {
		t.Fatalf("legacy unheld: Resolve = %q, %v; want the graph's lock", p, err)
	}
}
