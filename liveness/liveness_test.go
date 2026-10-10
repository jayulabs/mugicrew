package liveness

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeProc lays a /proc-shaped tree: one <pid>/cmdline per argv (NUL-joined, with
// the kernel's trailing NUL), and points procDir at it for the test.
func fakeProc(t *testing.T, argvs ...[]string) {
	t.Helper()
	dir := t.TempDir()
	for i, argv := range argvs {
		pid := filepath.Join(dir, string(rune('1'+i)))
		if err := os.Mkdir(pid, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pid, "cmdline"), []byte(strings.Join(argv, "\x00")+"\x00"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	orig := procDir
	procDir = dir
	t.Cleanup(func() { procDir = orig })
}

func TestAlive(t *testing.T) {
	reconcile := []string{"/usr/local/bin/mugicrew", "reconcile", "ws-a13.4"}
	claude := []string{"claude", "-p", "do the thing", "--output-format", "json", "--name", "ws-a13.4"}
	cases := []struct {
		name  string
		procs [][]string
		want  bool
	}{
		{"reconcile only", [][]string{reconcile}, true},
		{"claude only", [][]string{claude}, true},
		{"both", [][]string{reconcile, claude}, true},
		{"neither", [][]string{{"bash"}}, false},
		{"prefix collision", [][]string{{"mugicrew", "reconcile", "ws-a13.40"}}, false},
		{"id inside a longer token", [][]string{{"claude", "-p", "Bead to implement: ws-a13.4 (read it)"}}, false},
		{"id on a foreign argv[0]", [][]string{{"/usr/bin/zsh", "-c", "ws-a13.4"}, {"bd", "show", "ws-a13.4"}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeProc(t, c.procs...)
			if got := Alive([]string{"ws-a13.4"})["ws-a13.4"]; got != c.want {
				t.Errorf("alive = %v, want %v", got, c.want)
			}
		})
	}
}

// A vanished pid (no cmdline) or an unreadable one is skipped, never an error,
// and doesn't hide the live processes beside it.
func TestAliveSkipsVanishedAndUnreadable(t *testing.T) {
	fakeProc(t, []string{"mugicrew", "reconcile", "mc-x"})
	if err := os.Mkdir(filepath.Join(procDir, "98"), 0o755); err != nil { // vanished: no cmdline
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(procDir, "99", "cmdline"), 0o755); err != nil { // unreadable: cmdline is a dir
		t.Fatal(err)
	}
	got := Alive([]string{"mc-x", "mc-y"})
	if !got["mc-x"] || got["mc-y"] {
		t.Errorf("Alive = %v, want mc-x alive, mc-y dead", got)
	}
	if _, ok := got["mc-y"]; !ok {
		t.Error("every queried id gets a verdict, dead included")
	}
}

// Pids are the pids of the bead's live mugicrew/claude processes, on the same
// exact-token match as Alive: a prefix collision and a foreign argv[0] are left out.
func TestPids(t *testing.T) {
	fakeProc(t,
		[]string{"/usr/local/bin/mugicrew", "reconcile", "ws-a13.4"}, // pid 1
		[]string{"mugicrew", "reconcile", "ws-a13.40"},               // 2: prefix collision
		[]string{"bd", "show", "ws-a13.4"},                           // 3: foreign argv[0]
		[]string{"claude", "-p", "x", "--name", "ws-a13.4"},          // 4
	)
	if got := Pids("ws-a13.4"); !slices.Equal(got, []int{1, 4}) {
		t.Errorf("Pids = %v, want [1 4]", got)
	}
	if got := Pids("ws-none"); len(got) != 0 {
		t.Errorf("Pids of a dead bead = %v, want none", got)
	}
}
