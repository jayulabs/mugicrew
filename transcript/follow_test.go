package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type safeBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *safeBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *safeBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

const (
	tPrompt = `{"type":"user","timestamp":"2026-10-15T08:00:00Z","message":{"content":"go"}}` + "\n"
	tCall   = `{"type":"assistant","timestamp":"2026-10-15T08:00:01Z","message":{"content":[{"type":"tool_use","id":"a","name":"Bash","input":{"command":"ls"}}]}}` + "\n"
	tResult = `{"type":"user","timestamp":"2026-10-15T08:00:02Z","message":{"content":[{"type":"tool_result","tool_use_id":"a","content":"x"}]}}` + "\n"
)

// follow runs Follow on a live session whose transcript is steps appended in turn
// (nil file step = not yet written); each step runs after the output satisfied
// its wait. It returns the output.
func follow(t *testing.T, redraw bool, steps []string, waits []string, mid ...func(string)) string {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	os.MkdirAll(filepath.Join(cfg, "projects", "p"), 0o755)
	path := filepath.Join(cfg, "projects", "p", "sess.jsonl")
	var out safeBuf
	var mu sync.Mutex
	stop := false
	done := make(chan struct{})
	s := &Session{ID: "sess", Mode: "AFK", Kind: "build", Outcome: "unended", Start: time.Now()}
	go func() {
		Follow(s, FollowOpts{Opts: Opts{Wrap: redraw, Width: 80}, Redraw: redraw, Poll: 5 * time.Millisecond,
			Stop: func() bool { mu.Lock(); defer mu.Unlock(); return stop }}, &out)
		close(done)
	}()
	waitFor := func(w string) {
		for i := 0; i < 400 && !strings.Contains(out.String(), w); i++ {
			time.Sleep(5 * time.Millisecond)
		}
		if !strings.Contains(out.String(), w) {
			t.Fatalf("never saw %q in %q", w, out.String())
		}
	}
	for i, st := range steps {
		if st != "" {
			f, _ := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			f.WriteString(st)
			f.Close()
		}
		if waits[i] != "" {
			waitFor(waits[i])
		}
	}
	for _, m := range mid {
		time.Sleep(30 * time.Millisecond)
		m(out.String())
	}
	mu.Lock()
	stop = true
	mu.Unlock()
	<-done
	return out.String()
}

func TestFollowWaitsThenPicksUp(t *testing.T) {
	out := follow(t, false, []string{"", tPrompt}, []string{"waiting for transcript…", "launch prompt"})
	if strings.Count(out, "waiting for transcript…") != 1 || strings.Contains(out, "\n\n\n") {
		t.Errorf("out = %q", out)
	}
}

func TestFollowTornLineWaits(t *testing.T) {
	half := tPrompt[:len(tPrompt)/2]
	out := follow(t, false, []string{tPrompt + half, tPrompt[len(half):] + ""}, []string{"launch prompt", ""})
	if strings.Count(out, "launch prompt") != 1 {
		t.Errorf("out = %q", out)
	}
}

func TestFollowPipedPrintsOpenRunOnlyAtEnd(t *testing.T) {
	// the call is open while following: nothing printed for it until the follow ends
	out := follow(t, false, []string{tPrompt + tCall}, []string{"launch prompt"}, func(mid string) {
		if strings.Contains(mid, "Bash") {
			t.Errorf("open run printed while piped: %q", mid)
		}
	})
	if strings.Count(out, "Bash ls") != 1 || !strings.Contains(out, "· no result") || strings.Contains(out, "\x1b") {
		t.Errorf("out = %q", out)
	}
}

func TestFollowRedrawsPendingCall(t *testing.T) {
	out := follow(t, true, []string{tPrompt + tCall, tResult}, []string{"· no result", "✓"})
	if !strings.Contains(out, "\x1b[1A\r\x1b[J") || strings.Count(out, "Bash") != 2 {
		t.Errorf("out = %q", out)
	}
}

const (
	tCost = `{"type":"cost-state"}` + "\n"
	tCont = `{"type":"continued-in","continuedInSessionId":"dest"}` + "\n"
)

// followHandoff follows a session that hands off to dest, whose file is written
// after destDelay. Stop is true from the start (hitl.exited already logged), so
// the follow can only end through the handoff.
func followHandoff(t *testing.T, destDelay time.Duration, destBody string) string {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	dir := filepath.Join(cfg, "projects", "p")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "sess.jsonl"), []byte(
		`{"type":"user","uuid":"u1","timestamp":"2026-10-15T08:00:00Z","message":{"content":"go"}}`+"\n"+tCost+tCont), 0o644)
	if destBody != "" {
		go func() {
			time.Sleep(destDelay)
			os.WriteFile(filepath.Join(dir, "dest.jsonl"), []byte(destBody), 0o644)
		}()
	}
	var out safeBuf
	s := &Session{ID: "sess", Mode: "HITL", Kind: "build", Outcome: "unended", Start: time.Now()}
	done := make(chan struct{})
	go func() {
		Follow(s, FollowOpts{Poll: 5 * time.Millisecond, Stop: func() bool { return true }}, &out)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("follow never ended")
	}
	return out.String()
}

const destTail = `{"type":"user","uuid":"u1","message":{"content":"go"}}` + "\n" +
	`{"type":"assistant","uuid":"u2","message":{"content":[{"type":"text","text":"the tail"}]}}` + "\n"

func TestFollowSwitchesToDestinationDespiteExit(t *testing.T) {
	out := followHandoff(t, 0, destTail+tCost)
	if strings.Count(out, "launch prompt") != 1 || strings.Count(out, "↪ continued in dest (bg)") != 1 || !strings.Contains(out, "the tail") {
		t.Errorf("out = %q", out)
	}
}

func TestFollowWaitsForLateDestination(t *testing.T) {
	out := followHandoff(t, 40*time.Millisecond, destTail+tCost)
	if !strings.Contains(out, "the tail") {
		t.Errorf("out = %q", out)
	}
}

func TestFollowMissingDestinationEndsWithMarker(t *testing.T) {
	out := followHandoff(t, 0, "")
	if !strings.Contains(out, "↪ continued in dest (bg)") || strings.Contains(out, "the tail") {
		t.Errorf("out = %q", out)
	}
}

func tText(n int, s string) string {
	return `{"type":"assistant","uuid":"t` + string(rune('a'+n)) + `","timestamp":"2026-10-15T08:00:01Z","message":{"content":[{"type":"text","text":"` + s + `"}]}}` + "\n"
}

// A width change redraws only the open run: settled lines already printed are
// neither repeated (narrower) nor make later lines vanish (wider).
func TestFollowWidthChangeKeepsSettledLinesExactlyOnce(t *testing.T) {
	long := strings.Repeat("word ", 40)
	var width atomic.Int32
	width.Store(60)
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	os.MkdirAll(filepath.Join(cfg, "projects", "p"), 0o755)
	path := filepath.Join(cfg, "projects", "p", "sess.jsonl")
	var out safeBuf
	var stop atomic.Bool
	done := make(chan struct{})
	s := &Session{ID: "sess", Mode: "AFK", Kind: "build", Outcome: "unended", Start: time.Now()}
	go func() {
		Follow(s, FollowOpts{Opts: Opts{Wrap: true, Width: 60}, Redraw: true, Poll: 5 * time.Millisecond,
			Width: func() int { return int(width.Load()) }, Stop: stop.Load}, &out)
		close(done)
	}()
	add := func(body, wait string) {
		f, _ := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		f.WriteString(body)
		f.Close()
		for i := 0; i < 400 && !strings.Contains(out.String(), wait); i++ {
			time.Sleep(5 * time.Millisecond)
		}
		time.Sleep(30 * time.Millisecond)
	}
	add(tPrompt+tText(0, long)+tText(1, long)+tText(4, long)+tText(5, long)+tText(6, long)+tText(7, "FIRST-DONE"), "FIRST-DONE")
	width.Store(200)
	time.Sleep(30 * time.Millisecond)
	add(tText(2, "AFTER-WIDEN"), "AFTER-WIDEN")
	if !strings.Contains(out.String(), "AFTER-WIDEN") {
		t.Error("block appended after widening never appeared")
	}
	words := strings.Count(out.String(), "word word")
	width.Store(40)
	time.Sleep(30 * time.Millisecond)
	add(tText(3, "AFTER-NARROW"), "AFTER-NARROW")
	stop.Store(true)
	<-done
	got := out.String()
	for _, w := range []string{"AFTER-WIDEN", "AFTER-NARROW"} {
		if n := strings.Count(got, w); n != 1 {
			t.Errorf("%s appears %d times in %q", w, n, got)
		}
	}
	if n := strings.Count(got, "word word"); n != words {
		t.Errorf("settled lines repeated after narrowing: %d lines, was %d", n, words)
	}
	if n := strings.Count(got, "▍launch prompt"); n != 1 {
		t.Errorf("launch prompt printed %d times", n)
	}
}
