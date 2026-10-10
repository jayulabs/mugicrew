package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
)

// launcherSock, set, makes this test binary the launcher on that socket
// (TestLauncherHelper), with launcherExe as its TUI pane's program.
const (
	launcherSock = "MUGICREW_TEST_LAUNCHER_SOCK"
	launcherExe  = "MUGICREW_TEST_LAUNCHER_EXE"
)

// TestLauncherHelper is the launcher process TestLaunchTUIRealTmux runs in an
// outer tmux pane — its "terminal".
func TestLauncherHelper(t *testing.T) {
	sock := os.Getenv(launcherSock)
	if sock == "" {
		t.Skip("launcher helper process only")
	}
	launchTUI(sock, t.TempDir(), os.Getenv(launcherExe))
	os.Exit(0)
}

// The launcher against real tmux on throwaway -f /dev/null servers (ADR 0065
// §1–4): three terminals (outer panes) get independent tui-<pid> sessions, each
// with status and remain-on-exit off; one client leaving (<prefix> d) kills only
// its own session, a closed terminal (its pane hung up) kills another, and a
// TERM to the launcher kills the third.
func TestLaunchTUIRealTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.sock"), filepath.Join(dir, "out.sock")
	tm := func(sock string, args ...string) string {
		t.Helper()
		b, err := tmuxCmd(sock, append([]string{"-f", "/dev/null"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("tmux %v: %v: %s", args, err, b)
		}
		return strings.TrimSpace(string(b))
	}
	t.Cleanup(func() {
		tmuxCmd(in, "kill-server").Run()
		tmuxCmd(out, "kill-server").Run()
	})
	tm(in, "new-session", "-d", "-s", "keep") // the private server, started without ~/.tmux.conf
	exe := filepath.Join(dir, "tui")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nexec sleep 300\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var tuis []string
	for _, term := range []string{"t1", "t2", "t3"} {
		tm(out, "new-session", "-d", "-s", term, "-x", "80", "-y", "24", "env",
			launcherSock+"="+in, launcherExe+"="+exe, "XDG_RUNTIME_DIR="+dir,
			os.Args[0], "-test.run=^TestLauncherHelper$")
		tuis = append(tuis, "tui-"+tm(out, "display-message", "-p", "-t", "="+term+":", "#{pane_pid}"))
	}
	clients := func() []string {
		b, _ := tmuxCmd(in, "list-clients", "-F", "#{session_name}").Output()
		s := strings.Fields(string(b))
		slices.Sort(s)
		return s
	}
	sessions := func() []string {
		b, _ := tmuxCmd(in, "list-sessions", "-F", "#{session_name}").Output()
		return strings.Fields(string(b))
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); !ok(); time.Sleep(50 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s: sessions %v, clients %v", what, sessions(), clients())
			}
		}
	}
	want := slices.Sorted(slices.Values(tuis))
	waitFor("one client on each TUI session", func() bool { return slices.Equal(clients(), want) })
	for _, s := range tuis {
		if got := tm(in, "show-options", "-v", "-t", "="+s+":", "status"); got != "off" {
			t.Errorf("%s status = %q, want off", s, got)
		}
		if got := tm(in, "show-options", "-wv", "-t", "="+s+":", "remain-on-exit"); got != "off" {
			t.Errorf("%s remain-on-exit = %q, want off", s, got)
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, "tui.out")); err != nil || len(b) != 0 {
		t.Errorf("tui.out = %q, %v; want made and empty", b, err)
	}

	tm(in, "detach-client", "-s", "="+tuis[0])
	waitFor(tuis[0]+" killed after its client left", func() bool { return !slices.Contains(sessions(), tuis[0]) })
	if !slices.Contains(sessions(), tuis[1]) {
		t.Fatalf("the other terminal's %s went too: %v", tuis[1], sessions())
	}
	tm(out, "kill-session", "-t", "=t2") // the terminal closes: its pane hangs up
	waitFor(tuis[1]+" killed after its terminal closed", func() bool { return !slices.Contains(sessions(), tuis[1]) })
	pid, _ := strconv.Atoi(strings.TrimPrefix(tuis[2], "tui-"))
	syscall.Kill(pid, syscall.SIGTERM)
	waitFor(tuis[2]+" killed after its launcher got TERM", func() bool { return !slices.Contains(sessions(), tuis[2]) })
	if got := sessions(); !slices.Equal(got, []string{"keep"}) {
		t.Errorf("sessions = %v, want only keep", got)
	}
}

// switchClient against real tmux (ADR 0065 §1): with two terminals' clients on
// the server, each on its own TUI session, a switch from one TUI's pane moves only
// that TUI's client to the HITL session.
func TestSwitchClientRealTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.sock"), filepath.Join(dir, "out.sock")
	tm := func(sock string, args ...string) string {
		t.Helper()
		b, err := tmuxCmd(sock, append([]string{"-f", "/dev/null"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("tmux %v: %v: %s", args, err, b)
		}
		return strings.TrimSpace(string(b))
	}
	t.Cleanup(func() {
		tmuxCmd(in, "kill-server").Run()
		tmuxCmd(out, "kill-server").Run()
	})
	for _, s := range []string{"tui-1", "tui-2", "mc-a_1"} {
		tm(in, "new-session", "-d", "-s", s)
	}
	for _, term := range []string{"tui-1", "tui-2"} { // each terminal (an outer pane) attaches to its TUI
		tm(out, "new-session", "-d", "-s", term, "-x", "80", "-y", "24",
			"env", "-u", "TMUX", "-u", "TMUX_PANE", "tmux", "-S", in, "attach-session", "-t", "="+term)
	}
	clients := func() []string {
		b, _ := tmuxCmd(in, "list-clients", "-F", "#{session_name}").Output()
		s := strings.Fields(string(b))
		slices.Sort(s)
		return s
	}
	waitFor := func(what string, want []string) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); !slices.Equal(clients(), want); time.Sleep(50 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s: clients %v", what, clients())
			}
		}
	}
	waitFor("a client on each TUI", []string{"tui-1", "tui-2"})
	pane := tm(in, "display-message", "-p", "-t", "=tui-2:", "#{pane_id}")
	if err := switchClient(in, pane, "mc-a_1"); err != nil {
		t.Fatal(err)
	}
	waitFor("tui-2's client on the HITL session, tui-1's left", []string{"mc-a_1", "tui-1"})
	if err := switchClient(in, pane, "mc-a_1"); err == nil {
		t.Error("a switch from a TUI no client views must fail")
	}
}

// F12 against real tmux (ADR 0065 §5): with the preamble's binding and each TUI's
// recorded @tty, F12 in a HITL session returns the pressing terminal to its own
// TUI, after hopping sessions and with two terminals on one HITL session; it does
// nothing on a TUI session; a hand-attached terminal with no TUI detaches. The bar
// (and key table) is set after the clients are on the sessions, so this also
// shows a key table set on a viewed session takes effect.
func TestF12RealTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.sock"), filepath.Join(dir, "out.sock")
	tm := func(sock string, args ...string) string {
		t.Helper()
		b, err := tmuxCmd(sock, append([]string{"-f", "/dev/null"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("tmux %v: %v: %s", args, err, b)
		}
		return strings.TrimSpace(string(b))
	}
	t.Cleanup(func() {
		tmuxCmd(in, "kill-server").Run()
		tmuxCmd(out, "kill-server").Run()
	})
	tm(in, append(preamble(), ";", "new-session", "-d", "-s", "tui-1")...) // chained, or the empty server exits
	for _, s := range []string{"tui-2", "mc-a_1", "mc-b_1"} {
		tm(in, "new-session", "-d", "-s", s)
	}
	for _, term := range []string{"tui-1", "tui-2", "mc-a_1"} { // t3 attaches to mc-a_1 by hand
		tm(out, "new-session", "-d", "-s", term, "-x", "80", "-y", "24",
			"env", "-u", "TMUX", "-u", "TMUX_PANE", "tmux", "-S", in, "attach-session", "-t", "="+term)
	}
	clients := func() []string {
		b, _ := tmuxCmd(in, "list-clients", "-F", "#{session_name}").Output()
		s := strings.Fields(string(b))
		slices.Sort(s)
		return s
	}
	waitFor := func(what string, want ...string) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); !slices.Equal(clients(), want); time.Sleep(50 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s: clients %v", what, clients())
			}
		}
	}
	f12 := func(term string) { tm(out, "send-keys", "-t", "="+term+":", "F12") }
	waitFor("the three terminals attached", "mc-a_1", "tui-1", "tui-2")
	for _, s := range []string{"mc-a_1", "mc-b_1"} {
		tm(in, setArgs(s, bar(bd.Bead{ID: s, Title: "x"}))...)
	}
	var panes []string
	for _, s := range []string{"tui-1", "tui-2"} {
		pane := tm(in, "display-message", "-p", "-t", "="+s+":", "#{pane_id}")
		if err := recordClient(in, pane); err != nil {
			t.Fatal(err)
		}
		panes = append(panes, pane)
	}
	tty1 := tm(in, "show-options", "-v", "-t", "=tui-1:", ttyOption)
	if tty1 == "" || tty1 == tm(in, "show-options", "-v", "-t", "=tui-2:", ttyOption) {
		t.Fatalf("@tty not recorded per TUI: %q", tty1)
	}

	// Terminal 1 goes to mc-b_1, then hops to mc-a_1 with tmux; terminal 2 joins it.
	if err := switchClient(in, panes[0], "mc-b_1"); err != nil {
		t.Fatal(err)
	}
	waitFor("t1 on mc-b_1", "mc-a_1", "mc-b_1", "tui-2")
	tm(in, "switch-client", "-c", tty1, "-t", "=mc-a_1")
	if err := switchClient(in, panes[1], "mc-a_1"); err != nil {
		t.Fatal(err)
	}
	waitFor("all three on mc-a_1", "mc-a_1", "mc-a_1", "mc-a_1")
	f12("tui-1")
	waitFor("t1 back on its TUI", "mc-a_1", "mc-a_1", "tui-1")
	f12("tui-2")
	waitFor("t2 back on its TUI", "mc-a_1", "tui-1", "tui-2")
	f12("tui-1") // on a TUI session: nothing
	time.Sleep(300 * time.Millisecond)
	waitFor("F12 on a TUI doing nothing", "mc-a_1", "tui-1", "tui-2")
	f12("mc-a_1") // hand attach, no mapping: detach
	waitFor("the hand-attached terminal detached", "tui-1", "tui-2")
}

// A clean HITL exit against real tmux (ADR 0065 §6): the wrapper's return moves
// the viewer with a TUI back to it before the session goes; a hand-attached
// viewer with no TUI is left there and detached when the session is destroyed.
func TestReturnViewersRealTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.sock"), filepath.Join(dir, "out.sock")
	tm := func(sock string, args ...string) string {
		t.Helper()
		b, err := tmuxCmd(sock, append([]string{"-f", "/dev/null"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("tmux %v: %v: %s", args, err, b)
		}
		return strings.TrimSpace(string(b))
	}
	t.Cleanup(func() {
		tmuxCmd(in, "kill-server").Run()
		tmuxCmd(out, "kill-server").Run()
	})
	tm(in, append(preamble(), ";", "new-session", "-d", "-s", "tui-1")...)
	tm(in, "new-session", "-d", "-s", "mc-a_1")
	for _, term := range []string{"tui-1", "mc-a_1"} { // t2 attaches to mc-a_1 by hand
		tm(out, "new-session", "-d", "-s", term, "-x", "80", "-y", "24",
			"env", "-u", "TMUX", "-u", "TMUX_PANE", "tmux", "-S", in, "attach-session", "-t", "="+term)
	}
	clients := func() []string {
		b, _ := tmuxCmd(in, "list-clients", "-F", "#{session_name}").Output()
		s := strings.Fields(string(b))
		slices.Sort(s)
		return s
	}
	waitFor := func(what string, want ...string) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); !slices.Equal(clients(), want); time.Sleep(50 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s: clients %v", what, clients())
			}
		}
	}
	waitFor("both terminals attached", "mc-a_1", "tui-1")
	tui := tm(in, "display-message", "-p", "-t", "=tui-1:", "#{pane_id}")
	if err := recordClient(in, tui); err != nil {
		t.Fatal(err)
	}
	if err := switchClient(in, tui, "mc-a_1"); err != nil {
		t.Fatal(err)
	}
	waitFor("both on mc-a_1", "mc-a_1", "mc-a_1")

	hitl := tm(in, "display-message", "-p", "-t", "=mc-a_1:", "#{pane_id}")
	returnViewers(in+",1,0", hitl)
	waitFor("t1 back on its TUI", "mc-a_1", "tui-1")
	tm(in, "kill-session", "-t", "=mc-a_1") // the wrapper's exit 0 ends the session
	waitFor("the hand-attached viewer detached", "tui-1")
}

// F11 against real tmux (Spec D5): pressed in an attached HITL session it
// unsets that session's @autofinish, leaves another armed session's alone, keeps
// the session alive, and on an unarmed session is a harmless no-op.
func TestF11RealTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.sock"), filepath.Join(dir, "out.sock")
	tm := func(sock string, args ...string) string {
		t.Helper()
		b, err := tmuxCmd(sock, append([]string{"-f", "/dev/null"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("tmux %v: %v: %s", args, err, b)
		}
		return strings.TrimSpace(string(b))
	}
	t.Cleanup(func() {
		tmuxCmd(in, "kill-server").Run()
		tmuxCmd(out, "kill-server").Run()
	})
	tm(in, append(preamble(), ";", "new-session", "-d", "-s", "mc-a_1")...) // chained, or the empty server exits
	tm(in, "new-session", "-d", "-s", "mc-b_1")
	for _, s := range []string{"mc-a_1", "mc-b_1"} {
		tm(in, setArgs(s, append(bar(bd.Bead{ID: s, Title: "x"}), [2]string{"@autofinish", "1999999999"}))...)
	}
	tm(out, "new-session", "-d", "-s", "term", "-x", "80", "-y", "24",
		"env", "-u", "TMUX", "-u", "TMUX_PANE", "tmux", "-S", in, "attach-session", "-t", "=mc-a_1")
	armed := func(s string) string { return tm(in, "display-message", "-p", "-t", "="+s+":", "#{@autofinish}") }
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		if b, _ := tmuxCmd(in, "list-clients").Output(); len(b) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the terminal to attach")
		}
	}
	for range 2 { // the second press finds it unarmed: a no-op
		tm(out, "send-keys", "-t", "=term:", "F11")
		for deadline := time.Now().Add(10 * time.Second); armed("mc-a_1") != ""; time.Sleep(50 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatal("F11 left mc-a_1 armed")
			}
		}
	}
	if got := armed("mc-b_1"); got != "1999999999" {
		t.Errorf("mc-b_1 @autofinish = %q after F11 in mc-a_1, want it still armed", got)
	}
	if got := tm(in, "list-clients", "-F", "#{session_name}"); got != "mc-a_1" {
		t.Errorf("clients %q after F11, want still on mc-a_1", got)
	}
}
