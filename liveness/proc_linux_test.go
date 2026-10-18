package liveness

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
		{"nix-wrapped reconcile", [][]string{{"/nix/store/abc-mugicrew/bin/.mugicrew-wrapped", "reconcile", "ws-a13.4"}}, true},
		{"nix-wrapped claude", [][]string{{".claude-wrapped", "-p", "x", "--name", "ws-a13.4"}}, true},
		{"nix-wrapped prefix collision", [][]string{{".mugicrew-wrapped", "reconcile", "ws-a13.40"}}, false},
		{"near-miss wrapped names", [][]string{{".bash-wrapped", "-c", "ws-a13.4"}, {"mugicrew-wrapped-x", "reconcile", "ws-a13.4"}}, false},
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
		[]string{"/usr/local/bin/mugicrew", "reconcile", "ws-a13.4"},                       // pid 1
		[]string{"mugicrew", "reconcile", "ws-a13.40"},                                     // 2: prefix collision
		[]string{"bd", "show", "ws-a13.4"},                                                 // 3: foreign argv[0]
		[]string{"claude", "-p", "x", "--name", "ws-a13.4"},                                // 4
		[]string{"/nix/store/abc-mugicrew/bin/.mugicrew-wrapped", "reconcile", "ws-a13.4"}, // 5: Nix-wrapped
	)
	if got := Pids("ws-a13.4"); !slices.Equal(got, []int{1, 4, 5}) {
		t.Errorf("Pids = %v, want [1 4 5]", got)
	}
	if got := Pids("ws-none"); len(got) != 0 {
		t.Errorf("Pids of a dead bead = %v, want none", got)
	}
}

// fakeCwd lays procDir/<pid>/cwd as a symlink to target, as the kernel shows a
// process's working directory.
func fakeCwd(t *testing.T, pid int, target string) {
	t.Helper()
	dir := filepath.Join(procDir, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "cwd")); err != nil {
		t.Fatal(err)
	}
}

// InTree is every pid whose cwd lies in the tree, whatever its argv: the tree
// itself or below it, reached through a symlinked tree path, or a cwd the kernel
// marks " (deleted)". A sibling sharing the tree's prefix, a vanished pid, an
// unreadable (EACCES) cwd and an excluded pid are not hits.
func TestInTree(t *testing.T) {
	fakeProc(t)
	root := t.TempDir()
	tree := filepath.Join(root, "mugicrew-wt-1", "wt")
	if err := os.MkdirAll(filepath.Join(tree, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Join(root, "mugicrew-wt-1"), link); err != nil {
		t.Fatal(err)
	}
	fakeCwd(t, 10, tree)                                                  // the tree itself
	fakeCwd(t, 11, filepath.Join(tree, "sub"))                            // below it
	fakeCwd(t, 12, tree+"/sub (deleted)")                                 // a removed cwd
	fakeCwd(t, 13, tree+"2")                                              // a prefix sibling
	fakeCwd(t, 14, root)                                                  // outside
	fakeCwd(t, 15, tree)                                                  // excluded
	if err := os.Mkdir(filepath.Join(procDir, "16"), 0o755); err != nil { // vanished: no cwd
		t.Fatal(err)
	}
	fakeCwd(t, 17, tree) // unreadable below
	if err := os.Chmod(filepath.Join(procDir, "17"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(procDir, "17"), 0o755) })
	want := []int{10, 11, 12}
	if os.Geteuid() == 0 { // root reads through a 0 mode dir
		want = append(want, 17)
	}
	for _, path := range []string{tree, filepath.Join(link, "wt")} {
		got, err := InTree(path, []int{15})
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("InTree(%s) = %v, want %v", path, got, want)
		}
	}
}

// Lineage is pid and its ancestors, read off each /proc/<pid>/stat's ppid (a comm
// with spaces and parens included), stopping at pid 1's parent 0 or an unreadable stat.
func TestLineage(t *testing.T) {
	fakeProc(t)
	for pid, ppid := range map[int]int{1: 0, 40: 1, 41: 40, 42: 41} {
		dir := filepath.Join(procDir, strconv.Itoa(pid))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		stat := strconv.Itoa(pid) + " (go test) x) S " + strconv.Itoa(ppid) + " 1 1 0"
		if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := Lineage(42); !slices.Equal(got, []int{42, 41, 40, 1}) {
		t.Errorf("Lineage(42) = %v, want [42 41 40 1]", got)
	}
	if got := Lineage(99); !slices.Equal(got, []int{99}) {
		t.Errorf("Lineage of an unreadable pid = %v, want itself only", got)
	}
}

func TestAliveByLock(t *testing.T) {
	argv := [][]string{{"mugicrew", "reconcile", "e1"}}
	cases := []struct {
		name string
		argv [][]string
		lock bool
		want bool
	}{
		{"argv only", argv, false, true},
		{"lock only", nil, true, true},
		{"neither", nil, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeProc(t, c.argv...)
			fakeLocks(t)
			if c.lock {
				h, err := Acquire("e1")
				if err != nil {
					t.Fatal(err)
				}
				defer h.Release()
			}
			if got := Alive([]string{"e1"})["e1"]; got != c.want {
				t.Errorf("alive = %v, want %v", got, c.want)
			}
		})
	}
}
