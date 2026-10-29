package looplock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func shortGate(t *testing.T, d time.Duration) {
	old := gateTimeout
	gateTimeout = d
	t.Cleanup(func() { gateTimeout = old })
}

func TestRunGateCommandPass(t *testing.T) {
	if err := RunGateCommand(context.Background(), t.TempDir(), "exit 0"); err != nil {
		t.Fatal(err)
	}
}

func TestRunGateCommandBuilderError(t *testing.T) {
	err := RunGateCommand(context.Background(), t.TempDir(), "echo out; echo err >&2; exit 3")
	var be *BuilderError
	if !errors.As(err, &be) || be.Exit != 3 || be.Drv != "" {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(be.Log, "out") || !strings.Contains(be.Log, "err") {
		t.Errorf("log %q", be.Log)
	}
	if !strings.Contains(be.Error(), "exit 3") {
		t.Errorf("error %q", be.Error())
	}
}

func TestRunGateCommandInfra(t *testing.T) {
	for name, tc := range map[string]struct{ dir, cmd, want string }{
		"127":    {"", "exit 127", "127"},
		"126":    {"", "exit 126", "126"},
		"signal": {"", "kill -KILL $$", "killed"},
		"baddir": {"/nonexistent-dir-xyz", "true", ""},
	} {
		dir := tc.dir
		if dir == "" {
			dir = t.TempDir()
		}
		err := RunGateCommand(context.Background(), dir, tc.cmd)
		var ie *InfraError
		if !errors.As(err, &ie) || ie.Timeout || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestRunGateCommandParentCancelIsInfra(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	err := RunGateCommand(ctx, t.TempDir(), "sleep 30")
	var ie *InfraError
	if !errors.As(err, &ie) || ie.Timeout {
		t.Fatalf("got %v", err)
	}
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func readPid(t *testing.T, dir string) int {
	t.Helper()
	for range 50 {
		if b, err := os.ReadFile(filepath.Join(dir, "pid")); err == nil && strings.HasSuffix(string(b), "\n") {
			n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
			return n
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no pid written")
	return 0
}

func waitGone(t *testing.T, pid int) {
	t.Helper()
	for range 50 {
		if !alive(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("grandchild %d still alive", pid)
}

func TestRunGateCommandDeadlineKillsForegroundGrandchild(t *testing.T) {
	shortGate(t, 300*time.Millisecond)
	dir := t.TempDir()
	start := time.Now()
	err := RunGateCommand(context.Background(), dir, "sleep 30 & echo $! > pid; wait")
	var ie *InfraError
	if !errors.As(err, &ie) || !ie.Timeout {
		t.Fatalf("got %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("took %v", d)
	}
	waitGone(t, readPid(t, dir))
}

func TestRunGateCommandCleanExitSweepsBackgroundGrandchild(t *testing.T) {
	dir := t.TempDir()
	start := time.Now()
	err := RunGateCommand(context.Background(), dir, "sleep 30 & echo $! > pid; exit 0")
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > cmdWaitDelay+3*time.Second {
		t.Errorf("took %v", d)
	}
	waitGone(t, readPid(t, dir))
}

func TestRunGateCommandChattyOutputIsBounded(t *testing.T) {
	big := "i=0; while [ $i -lt 20000 ]; do echo line-$i-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx; i=$((i+1)); done"
	err := RunGateCommand(context.Background(), t.TempDir(), big+"; exit 1")
	var be *BuilderError
	if !errors.As(err, &be) {
		t.Fatalf("got %v", err)
	}
	lines := strings.Split(be.Log, "\n")
	if len(lines) != 80 || lines[79] != "line-19999-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" ||
		!strings.HasPrefix(lines[0], "line-") {
		t.Errorf("log: %d lines, first %q last %q", len(lines), lines[0], lines[len(lines)-1])
	}
	err = RunGateCommand(context.Background(), t.TempDir(), big+"; exit 127")
	var ie *InfraError
	if !errors.As(err, &ie) || len(ie.Tail) > maxTail || !strings.HasPrefix(ie.Tail, "line-") {
		t.Fatalf("infra tail %d: %.40q", len(ie.Tail), ie.Tail)
	}
}

func TestTailBufDropsPartialFirstLineOnceWrapped(t *testing.T) {
	tb := &tailBuf{cap: 10}
	fmt.Fprint(tb, "aaa\nbbb\nccc\n")
	if got := tb.String(); got != "bbb\nccc\n" {
		t.Errorf("got %q", got)
	}
}

func TestRunGateCommandInfraTailEndsWithStatus(t *testing.T) {
	chatty := "i=0; while [ $i -lt 2000 ]; do echo line-$i-xxxxxxxxxxxxxxxxxxxx; i=$((i+1)); done; "
	for _, c := range []struct{ name, cmd, end string }{
		{"127", chatty + "exit 127", "exit status 127"},
		{"kill", chatty + "kill -9 $$", "signal: killed"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var ie *InfraError
			if err := RunGateCommand(context.Background(), t.TempDir(), c.cmd); !errors.As(err, &ie) {
				t.Fatalf("err = %v, want *InfraError", err)
			}
			if !strings.HasSuffix(ie.Tail, "\n"+c.end) || len(ie.Tail) > maxTail || !strings.HasPrefix(ie.Tail, "line-") {
				t.Errorf("tail = %q (%d bytes), want line-start output ending %q within %d", ie.Tail, len(ie.Tail), c.end, maxTail)
			}
		})
	}
}
