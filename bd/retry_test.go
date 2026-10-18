package bd

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"
	"time"
)

// exitErr fakes a failed bd exec whose stderr is folded into the text, as runOnce does.
func exitErr(stderr string) error {
	return fmt.Errorf("bd x: %w: %s", &exec.ExitError{}, stderr)
}

func TestRunInRetry(t *testing.T) {
	var slept []time.Duration
	sleep = func(d time.Duration) { slept = append(slept, d) }
	orig := runOnce
	defer func() { runOnce, sleep = orig, time.Sleep }()

	tests := []struct {
		name      string
		fail      error // returned for the first failures attempts
		failures  int
		wantCalls int
		wantErr   bool
	}{
		{"1213", exitErr("Error 1213 (40001): serialization failure"), 2, 3, false},
		{"already exists", exitErr("already exists"), 1, 2, false},
		{"connection refused", exitErr("dial: connection refused"), 1, 2, false},
		{"breaker", exitErr("circuit breaker is open"), 2, 3, false},
		{"indeterminate+1213", exitErr("1213 (40001) indeterminate"), 1, 1, true},
		{"stage and commit", exitErr("stage and commit after regular SQL commit"), 1, 1, true},
		{"other exit", exitErr("exit status 13"), 1, 1, true},
		{"non-exit", errors.New("bd: not found"), 1, 1, true},
		{"persistent", exitErr("1213 (40001)"), 99, 3, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls, slept = 0, nil
			runOnce = func(string, ...string) (string, error) {
				calls++
				if calls <= tt.failures {
					return "", tt.fail
				}
				return "ok", nil
			}
			out, err := runIn("", "show")
			if calls != tt.wantCalls || (err != nil) != tt.wantErr {
				t.Fatalf("calls=%d err=%v, want calls=%d err=%v", calls, err, tt.wantCalls, tt.wantErr)
			}
			if err != nil && err != tt.fail {
				t.Fatalf("last error changed: %v", err)
			}
			if err == nil && out != "ok" {
				t.Fatalf("out=%q", out)
			}
			if len(slept) != tt.wantCalls-1 {
				t.Fatalf("slept %d times, want %d", len(slept), tt.wantCalls-1)
			}
		})
	}
}

var calls int

func TestBackoffNearSchedule(t *testing.T) {
	for i, base := range []time.Duration{200 * time.Millisecond, 500 * time.Millisecond, time.Second} {
		for range 50 {
			if d := backoff(i); d < base/2 || d >= base*3/2 {
				t.Fatalf("backoff(%d)=%v outside ±50%% of %v", i, d, base)
			}
		}
	}
}

func TestRunBatchRetry(t *testing.T) {
	sleep = func(time.Duration) {}
	orig := runBatchOnce
	defer func() { runBatchOnce, sleep = orig, time.Sleep }()

	tests := []struct {
		name      string
		fail      error
		failures  int
		wantCalls int
		wantErr   bool
	}{
		{"1213", exitErr("Error 1213 (40001): serialization failure"), 2, 3, false},
		{"already exists", exitErr("already exists"), 1, 2, false},
		{"connection refused", exitErr("dial: connection refused"), 1, 2, false},
		{"breaker", exitErr("circuit breaker is open"), 2, 3, false},
		{"indeterminate+1213", exitErr("1213 (40001) indeterminate"), 1, 1, true},
		{"stage and commit", exitErr("stage and commit after regular SQL commit"), 1, 1, true},
		{"other exit", exitErr("exit status 13"), 1, 1, true},
		{"non-exit", errors.New("bd: not found"), 1, 1, true},
		{"persistent", exitErr("1213 (40001)"), 99, 3, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var scripts []string
			runBatchOnce = func(script string) error {
				scripts = append(scripts, script)
				if len(scripts) <= tt.failures {
					return tt.fail
				}
				return nil
			}
			err := runBatch("open mc-a\n")
			if len(scripts) != tt.wantCalls || (err != nil) != tt.wantErr {
				t.Fatalf("calls=%d err=%v, want calls=%d err=%v", len(scripts), err, tt.wantCalls, tt.wantErr)
			}
			if err != nil && err != tt.fail {
				t.Fatalf("last error changed: %v", err)
			}
			for _, s := range scripts {
				if s != "open mc-a\n" {
					t.Fatalf("replayed script %q differs", s)
				}
			}
		})
	}
}

func TestRunEnvRetry(t *testing.T) {
	sleep = func(time.Duration) {}
	orig := runEnvOnce
	defer func() { runEnvOnce, sleep = orig, time.Sleep }()
	calls := 0
	runEnvOnce = func(env []string, args ...string) (string, error) {
		calls++
		if len(env) != 1 {
			t.Fatalf("env dropped: %v", env)
		}
		if calls == 1 {
			return "", exitErr("dial: connection refused")
		}
		return "ok", nil
	}
	if out, err := runEnv([]string{"A=b"}, "sql"); err != nil || out != "ok" || calls != 2 {
		t.Fatalf("out=%q err=%v calls=%d", out, err, calls)
	}
}
