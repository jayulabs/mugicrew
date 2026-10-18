package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
)

// The edge rules (ADR 0099 §4, Spec D4): the hook event and the current
// bit give the new bit; bad JSON and unknown events leave it as it is.
func TestWaitEdge(t *testing.T) {
	for _, tc := range []struct {
		event     string
		cur, want Waiting
	}{
		{`{"hook_event_name":"Stop"}`, NotWaiting, WaitTurn},
		{`{"hook_event_name":"Stop","background_tasks":[{"id":"b1"}]}`, NotWaiting, WaitTurn},
		{`{"hook_event_name":"StopFailure"}`, WaitAsk, WaitTurn},
		{`{"hook_event_name":"PermissionRequest"}`, NotWaiting, WaitAsk},
		{`{"hook_event_name":"PermissionRequest","agent_id":"sub-1"}`, WaitTurn, WaitAsk},
		{`{"hook_event_name":"Notification","notification_type":"elicitation_dialog"}`, NotWaiting, WaitAsk},
		{`{"hook_event_name":"Notification","notification_type":"elicitation_url_dialog"}`, NotWaiting, WaitAsk},
		{`{"hook_event_name":"Notification","notification_type":"idle_prompt"}`, WaitTurn, WaitTurn},
		{`{"hook_event_name":"Notification","notification_type":"permission_prompt"}`, NotWaiting, NotWaiting},
		{`{"hook_event_name":"UserPromptSubmit"}`, WaitTurn, NotWaiting},
		{`{"hook_event_name":"UserPromptSubmit"}`, WaitAsk, NotWaiting},
		{`{"hook_event_name":"PostToolUse"}`, WaitAsk, NotWaiting},
		{`{"hook_event_name":"PostToolUseFailure"}`, WaitAsk, NotWaiting},
		{`{"hook_event_name":"PostToolUse"}`, WaitTurn, WaitTurn},
		{`{"hook_event_name":"PostToolUseFailure"}`, WaitTurn, WaitTurn},
		{`{"hook_event_name":"SubagentStop"}`, WaitTurn, WaitTurn},
		{`{"hook_event_name":"SubagentStop"}`, NotWaiting, NotWaiting},
		{`{"hook_event_name":"PreToolUse"}`, WaitAsk, WaitAsk},
		{`not json`, WaitTurn, WaitTurn},
	} {
		if got := waitEdge([]byte(tc.event), tc.cur); got != tc.want {
			t.Errorf("waitEdge(%s, %q) = %q, want %q", tc.event, tc.cur, got, tc.want)
		}
	}
}

// The Auto-exit edges (Spec D2, ADR 0111): a Stop showing the closing
// phrase with no background work arms for the setting read from the event's cwd;
// every other hooked edge disarms; bad JSON and unhooked events keep.
func TestAutoExitEdge(t *testing.T) {
	const line = `"last_assistant_message":"done.\n━━━\n✅ **The work for this session is finished.** You may close this chat.\n━━━"`
	const (
		keep   = -1
		disarm = 0
	)
	for _, tc := range []struct {
		name, event string
		setting     time.Duration
		want        time.Duration
	}{
		{"closing-line Stop arms", `{"hook_event_name":"Stop","cwd":"/wt",` + line + `}`, 90 * time.Second, 90 * time.Second},
		{"empty arrays still arm", `{"hook_event_name":"Stop","cwd":"/wt","background_tasks":[],"session_crons":[],` + line + `}`, time.Minute, time.Minute},
		{"background task pending", `{"hook_event_name":"Stop","cwd":"/wt","background_tasks":[{"id":"b1"}],` + line + `}`, time.Minute, disarm},
		{"session cron pending", `{"hook_event_name":"Stop","cwd":"/wt","session_crons":[{"id":"c1"}],` + line + `}`, time.Minute, disarm},
		{"off never arms", `{"hook_event_name":"Stop","cwd":"/wt",` + line + `}`, 0, disarm},
		{"Stop without the line", `{"hook_event_name":"Stop","cwd":"/wt","last_assistant_message":"Want me to go on?"}`, time.Minute, disarm},
		{"StopFailure with the line", `{"hook_event_name":"StopFailure","cwd":"/wt",` + line + `}`, time.Minute, disarm},
		{"reply", `{"hook_event_name":"UserPromptSubmit"}`, time.Minute, disarm},
		{"permission", `{"hook_event_name":"PermissionRequest"}`, time.Minute, disarm},
		{"elicitation", `{"hook_event_name":"Notification","notification_type":"elicitation_dialog"}`, time.Minute, disarm},
		{"elicitation url", `{"hook_event_name":"Notification","notification_type":"elicitation_url_dialog"}`, time.Minute, disarm},
		{"tool run", `{"hook_event_name":"PostToolUse"}`, time.Minute, disarm},
		{"tool run failure", `{"hook_event_name":"PostToolUseFailure"}`, time.Minute, disarm},
		{"idle notification keeps", `{"hook_event_name":"Notification","notification_type":"idle_prompt"}`, time.Minute, keep},
		{"bad JSON keeps", `not json`, time.Minute, keep},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var read string
			got := time.Duration(keep)
			d, ok := autoExitEdge([]byte(tc.event), func(cwd string) time.Duration { read = cwd; return tc.setting })
			if ok {
				got = d
			}
			if got != tc.want {
				t.Errorf("autoExitEdge = %v (change %v), want %v", d, ok, tc.want)
			}
			if tc.want > 0 && read != "/wt" {
				t.Errorf("setting read from %q, want the event cwd /wt", read)
			}
		})
	}
}

// hitl-run exports the pane's address (socket, pane id) to the agent when it runs
// in a tmux pane, and nothing when TMUX or TMUX_PANE is absent (Spec D3).
func TestHitlRunExportsPane(t *testing.T) {
	origRun := runAgent
	defer func() { runAgent = origRun }()
	stubDrafts(t, nil)
	for _, tc := range []struct {
		name, tmux, pane, want string
	}{
		{"in a pane", "/r/.git/mugicrew/tmux.sock,4242,0", "%7", "/r/.git/mugicrew/tmux.sock,%7"},
		{"no TMUX", "", "%7", ""},
		{"no TMUX_PANE", "/s,1,0", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eventlogtest.Capture(t)
			for k, v := range map[string]string{"TMUX": tc.tmux, "TMUX_PANE": tc.pane, paneVar: "/stale,%1"} {
				t.Setenv(k, v)
				if v == "" {
					os.Unsetenv(k)
				}
			}
			got, seen := "", false
			runAgent = func([]string, <-chan struct{}) error {
				got, seen = os.LookupEnv(paneVar)
				return exec.Command("false").Run()
			}
			HitlRun([]string{"--bead=g", argvArg(t, "claude")}, meter(t))
			if got != tc.want || seen != (tc.want != "") {
				t.Errorf("agent %s = %q (set %v), want %q", paneVar, got, seen, tc.want)
			}
		})
	}
}

// hitl-hook against real tmux on a throwaway -f /dev/null server: each event sets
// or clears @waiting on the pane's session, and Sessions() reads it back.
func TestHitlHookRealTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	sock := filepath.Join(t.TempDir(), "s.sock")
	origSock := socket
	socket = func() (string, error) { return sock, nil }
	t.Cleanup(func() { socket = origSock; tmuxCmd(sock, "kill-server").Run() })
	pane, err := tmuxCmd(sock, "-f", "/dev/null", "new-session", "-d", "-s", "mc-w", "-P", "-F", "#{pane_id}",
		";", "set-option", "-t", "=mc-w:", "@bead", "mc-w").CombinedOutput()
	if err != nil {
		t.Fatalf("tmux new-session: %v: %s", err, pane)
	}
	t.Setenv(paneVar, sock+","+strings.TrimSpace(string(pane)))
	for _, step := range []struct {
		event string
		want  Waiting
	}{
		{"Stop", WaitTurn},
		{"PostToolUse", WaitTurn},
		{"UserPromptSubmit", NotWaiting},
		{"PermissionRequest", WaitAsk},
		{"PostToolUseFailure", NotWaiting},
	} {
		HitlHook(strings.NewReader(`{"hook_event_name":"`+step.event+`"}`), noAutoExit)
		s, err := Sessions()
		if err != nil {
			t.Fatal(err)
		}
		if got := s["mc-w"].Waiting; got != step.want {
			t.Fatalf("after %s: waiting = %q, want %q", step.event, got, step.want)
		}
	}
}

// noAutoExit is an auto_exit setting that is never read.
func noAutoExit(string) time.Duration { panic("setting read") }

// hitl-hook against real tmux: a closing-line Stop writes a deadline of now plus
// the setting to @autofinish, a reply unsets it, and Sessions() reads both.
func TestHitlHookAutoExitRealTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	sock := filepath.Join(t.TempDir(), "s.sock")
	origSock := socket
	socket = func() (string, error) { return sock, nil }
	t.Cleanup(func() { socket = origSock; tmuxCmd(sock, "kill-server").Run() })
	pane, err := tmuxCmd(sock, "-f", "/dev/null", "new-session", "-d", "-s", "mc-x", "-P", "-F", "#{pane_id}",
		";", "set-option", "-t", "=mc-x:", "@bead", "mc-x").CombinedOutput()
	if err != nil {
		t.Fatalf("tmux new-session: %v: %s", err, pane)
	}
	t.Setenv(paneVar, sock+","+strings.TrimSpace(string(pane)))
	deadline := func() time.Time {
		s, err := Sessions()
		if err != nil {
			t.Fatal(err)
		}
		return s["mc-x"].AutoExit
	}
	before := time.Now()
	HitlHook(strings.NewReader(`{"hook_event_name":"Stop","last_assistant_message":"**The work for this session is finished.**"}`),
		func(string) time.Duration { return time.Hour })
	if got := deadline(); got.Before(before.Add(time.Hour).Truncate(time.Second)) || got.After(time.Now().Add(time.Hour)) {
		t.Fatalf("armed deadline = %v, want about now+1h", got)
	}
	HitlHook(strings.NewReader(`{"hook_event_name":"UserPromptSubmit"}`), noAutoExit)
	if got := deadline(); !got.IsZero() {
		t.Errorf("deadline after a reply = %v, want disarmed", got)
	}
}

// Finish against real tmux (Spec FR 2, FR 8): an armed session's @autofinish moves
// to now and Finish reports armed; an unarmed one is left unset and reports not.
func TestFinishRealTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	sock := filepath.Join(t.TempDir(), "s.sock")
	origSock := socket
	socket = func() (string, error) { return sock, nil }
	t.Cleanup(func() { socket = origSock; tmuxCmd(sock, "kill-server").Run() })
	if out, err := tmuxCmd(sock, "-f", "/dev/null", "new-session", "-d", "-s", "mc-x",
		";", "new-session", "-d", "-s", "mc-y").CombinedOutput(); err != nil {
		t.Fatalf("tmux new-session: %v: %s", err, out)
	}
	opt := func(s string) string {
		out, _ := tmuxCmd(sock, "display-message", "-p", "-t", "="+s+":", "#{@autofinish}").Output()
		return strings.TrimSpace(string(out))
	}
	tmuxCmd(sock, "set-option", "-t", "=mc-x:", "@autofinish", "1999999999").Run()
	before := time.Now().Unix()
	if armed, err := Finish(bd.Bead{ID: "mc-x"}); err != nil || !armed {
		t.Fatalf("Finish(armed) = %v, %v; want true, nil", armed, err)
	}
	if n, _ := strconv.ParseInt(opt("mc-x"), 10, 64); n < before || n > time.Now().Unix() {
		t.Errorf("@autofinish = %q, want about now", opt("mc-x"))
	}
	if armed, err := Finish(bd.Bead{ID: "mc-y"}); err != nil || armed {
		t.Errorf("Finish(unarmed) = %v, %v; want false, nil", armed, err)
	}
	if got := opt("mc-y"); got != "" {
		t.Errorf("unarmed @autofinish = %q after Finish, want unset", got)
	}
}
