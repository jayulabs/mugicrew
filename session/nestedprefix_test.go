package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestChooseChord(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		outer, outer2, private2, want string
		bound                         []string
	}{
		{name: "outer C-b", outer: "C-b", outer2: "None", private2: "None", want: "C-a"},
		{name: "outer prefix C-a", outer: "C-a", outer2: "None", private2: "None", want: "C-Space"},
		{name: "outer prefix2 C-a", outer: "C-b", outer2: "C-a", private2: "None", want: "C-Space"},
		{name: "outer C-a, private C-Space bound", outer: "C-a", outer2: "None", private2: "None", bound: []string{"C-Space"}, want: ""},
		{name: "private prefix2 C-x", outer: "C-b", outer2: "None", private2: "C-x", want: ""},
		{name: "private prefix2 from an earlier launch", outer: "C-b", outer2: "None", private2: "C-a", want: ""},
		{name: "unknown outer", private2: "None", want: "C-a"},
		{name: "private C-a bound", outer: "C-b", outer2: "None", private2: "None", bound: []string{"c", "C-a"}, want: "C-Space"},
		{name: "outer C-@ takes neither", outer: "C-@", outer2: "C-A", private2: "None", want: "C-a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := chooseChord(tc.outer, tc.outer2, tc.private2, tc.bound); got != tc.want {
				t.Errorf("chooseChord = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParsePrefixKeys(t *testing.T) {
	out := "bind-key    -T prefix C-a     send-prefix -2\n" +
		"bind-key -r -T prefix Up      select-pane -U\n" +
		"bind-key    -T prefix C-Space next-layout\n" +
		"bind-key -T prefix c new-window\n"
	if got, want := parsePrefixKeys(out), []string{"C-a", "Up", "C-Space", "c"}; !slices.Equal(got, want) {
		t.Errorf("parsePrefixKeys = %v, want %v", got, want)
	}
	if got := parsePrefixKeys("table prefix doesn't exist\n"); len(got) != 0 {
		t.Errorf("parsePrefixKeys(missing table) = %v, want none", got)
	}
}

// A launcher against real tmux (the TestLaunchTUIRealTmux setup, whose launcher is
// already nested): what the private server holds once a client is attached.
func TestNestedLaunchPrefix2RealTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	for _, tc := range []struct {
		name         string
		outerPrefix  string // "" leaves the outer at C-b
		privateSet   string // a prefix2 set on the private server before launch
		unnested     bool
		launches     int
		want         string
		wantBindings int
	}{
		{name: "nested", launches: 1, want: "C-a", wantBindings: 1},
		{name: "outer prefix C-a", outerPrefix: "C-a", launches: 1, want: "C-Space", wantBindings: 1},
		{name: "preset private prefix2", privateSet: "C-x", launches: 1, want: "C-x"},
		{name: "second launcher", launches: 2, want: "C-a", wantBindings: 1},
		{name: "not nested", unnested: true, launches: 1, want: "None"},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			exe := filepath.Join(dir, "tui")
			if err := os.WriteFile(exe, []byte("#!/bin/sh\nexec sleep 300\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			tm(in, "new-session", "-d", "-s", "keep")
			if tc.privateSet != "" {
				tm(in, "set", "-g", "prefix2", tc.privateSet)
			}
			for i := 0; i < tc.launches; i++ {
				term := "t" + strconv.Itoa(i)
				tm(out, "new-session", "-d", "-s", term, "-x", "80", "-y", "24")
				if tc.outerPrefix != "" {
					tm(out, "set", "-g", "prefix", tc.outerPrefix)
				}
				cmd := []string{"env"}
				if tc.unnested {
					cmd = append(cmd, "-u", "TMUX", "-u", "TMUX_PANE")
				}
				cmd = append(cmd, launcherSock+"="+in, launcherExe+"="+exe, "XDG_RUNTIME_DIR="+dir,
					os.Args[0], "-test.run=^TestLauncherHelper$")
				tm(out, "respawn-pane", "-k", "-t", "="+term+":", strings.Join(cmd, " "))
				want := i + 1
				for deadline := time.Now().Add(10 * time.Second); len(strings.Fields(tm(in, "list-clients", "-F", "#{session_name}"))) != want; time.Sleep(50 * time.Millisecond) {
					if time.Now().After(deadline) {
						t.Fatalf("launcher %d never attached", i)
					}
				}
			}
			if got := tm(in, "show", "-gv", "prefix2"); got != tc.want {
				t.Errorf("prefix2 = %q, want %q", got, tc.want)
			}
			files, _ := filepath.Glob(filepath.Join(dir, "mugicrew-env-*"))
			if len(files) != tc.launches {
				t.Fatalf("env handoff files = %v, want %d", files, tc.launches)
			}
			for _, f := range files {
				b, _ := os.ReadFile(f)
				if has := slices.Contains(strings.Split(string(b), "\x00"), NestedEnv+"=1"); has == tc.unnested {
					t.Errorf("%s carries %s = %v, want %v", filepath.Base(f), NestedEnv, has, !tc.unnested)
				}
			}
			bindings := 0
			if b, err := tmuxCmd(in, "list-keys", "-T", "prefix").Output(); err == nil {
				for _, l := range strings.Split(string(b), "\n") {
					if f := strings.Fields(l); slices.Contains(f, "send-prefix") && slices.Contains(f, "-2") {
						bindings++
					}
				}
			}
			if bindings != tc.wantBindings {
				t.Errorf("send-prefix -2 bindings = %d, want %d", bindings, tc.wantBindings)
			}
		})
	}
}

// A probe or set that fails (no private server to answer) neither panics nor
// prints: setNestedPrefix2 has nothing to write to.
func TestSetNestedPrefix2FailureIsSilent(t *testing.T) {
	setNestedPrefix2(filepath.Join(t.TempDir(), "none.sock"))
}
