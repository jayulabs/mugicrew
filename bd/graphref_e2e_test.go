package bd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"jayulabs.org/mugicrew/git"
)

// TestGraphRefRoundTrip is the opt-in end-to-end (MUGICREW_BD_TEST=1, bd on
// PATH): Graph push to a bare repo standing in for the Forge, Graph
// fetch into a Scratch graph, root and head match, bd reads it; sync.remote
// and refs/dolt/data untouched; an unrelated graph's push refused. Prior art:
// refs/mugicrew/prototypes/mc-t72m.16.
func TestGraphRefRoundTrip(t *testing.T) {
	if os.Getenv("MUGICREW_BD_TEST") != "1" {
		t.Skip("set MUGICREW_BD_TEST=1 to drive a real bd server")
	}
	for _, b := range []string{"bd", "git"} {
		if _, err := exec.LookPath(b); err != nil {
			t.Skip("no " + b + " on PATH")
		}
	}
	S := t.TempDir()
	t.Cleanup(func() { // registered first, so it runs after every stop
		pids, _ := exec.Command("pgrep", "-f", "sql-server").Output()
		for _, pid := range strings.Fields(string(pids)) {
			if cwd, err := os.Readlink("/proc/" + pid + "/cwd"); err == nil && strings.HasPrefix(cwd, S) {
				syscall.Kill(mustAtoi(pid), syscall.SIGTERM)
				t.Errorf("sql-server %s left running in %s", pid, cwd)
			}
		}
	})
	sh := func(dir, name string, args ...string) string {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		cmd.Env = append(withoutEnv(os.Environ(), "BEADS_DIR", "DOLT_REMOTE_INFO_BRANCH"), "DOLT_REMOTE_INFO_BRANCH=")
		if dir != "" && name == "bd" {
			cmd.Env = append(cmd.Env, "BEADS_DIR="+filepath.Join(dir, ".beads"))
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	forge, seed := filepath.Join(S, "forge.git"), filepath.Join(S, "seed")
	sh(S, "git", "init", "-q", "--bare", forge)
	sh(S, "git", "init", "-q", seed)
	sh(seed, "git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "seed")
	sh(seed, "git", "push", "-q", forge, "HEAD:refs/heads/main")

	graph := func(name, prefix string) string { // a git repo with a server-mode bd, forge as origin
		d := filepath.Join(S, name)
		sh(S, "git", "init", "-q", d)
		sh(d, "git", "remote", "add", "origin", "file://"+forge)
		sh(d, "bd", "init", "--server", "--non-interactive", "-p", prefix)
		t.Cleanup(func() { // aimed at this scratch graph, never the caller's BEADS_DIR
			cmd := exec.Command("bd", "dolt", "stop")
			cmd.Dir = d
			cmd.Env = append(withoutEnv(os.Environ(), "BEADS_DIR"), "BEADS_DIR="+filepath.Join(d, ".beads"))
			cmd.Run()
		})
		if got := sh(d, "bd", "config", "get", "issue_prefix"); got != prefix {
			t.Fatalf("scratch BEADS_DIR guard: prefix %q", got)
		}
		return d
	}
	push := func(d, name string) error {
		t.Chdir(d)
		t.Setenv("BEADS_DIR", filepath.Join(d, ".beads"))
		root, err := readGraphRoot()
		if err != nil {
			t.Fatal(err)
		}
		defer git.SetGraphID(root)()
		_, err = GraphPush(name)
		return err
	}
	a := graph("a", "alice")
	sh(a, "bd", "create", "--title", "first")
	refs0 := sh(forge, "git", "for-each-ref")
	if err := push(a, "alice"); err != nil {
		t.Fatal(err)
	}
	if got := sh(forge, "git", "for-each-ref", "refs/mugicrew/graphs/"); !strings.Contains(got, "refs/mugicrew/graphs/alice") {
		t.Fatalf("no Graph ref: %s", got)
	}
	if got := sh(forge, "git", "for-each-ref", "refs/dolt/data", "refs/heads/__dolt_remote_info__"); got != "" || refs0 != sh(forge, "git", "for-each-ref", "refs/heads", "refs/dolt") {
		t.Errorf("push touched refs/dolt/data or the info branch: %q", got)
	}

	dir := filepath.Join(S, "scratch")
	t.Chdir(a)
	if err := FetchGraph("alice", dir, a); err != nil {
		t.Fatal(err)
	}
	sh(dir, "bd", "dolt", "start") // read-only bd does not auto-start the server
	root := "SELECT commit_hash FROM dolt_commit_ancestors WHERE parent_hash IS NULL"
	head := "SELECT commit_hash FROM dolt_log LIMIT 1"
	for _, q := range []string{root, head} {
		if x, y := sh(a, "bd", "sql", "--csv", q), sh(dir, "bd", "sql", "--csv", q); x != y {
			t.Errorf("%s: pushed %q fetched %q", q, x, y)
		}
	}
	if out := sh(dir, "bd", "list", "--all"); !strings.Contains(out, "first") {
		t.Errorf("bd list = %s", out)
	}
	wr := exec.Command("bd", "create", "--title", "nope")
	wr.Dir, wr.Env = dir, append(withoutEnv(os.Environ(), "BEADS_DIR", "DOLT_REMOTE_INFO_BRANCH"), "DOLT_REMOTE_INFO_BRANCH=", "BEADS_DIR="+filepath.Join(dir, ".beads"))
	if out, err := wr.CombinedOutput(); err == nil || !strings.Contains(string(out), "read-only mode") {
		t.Errorf("bd create on Scratch graph: %v %s", err, out)
	}
	sh(dir, "bd", "dolt", "stop")

	c := graph("c", "carol")
	sh(c, "bd", "create", "--title", "carol one")
	before := sh(forge, "git", "rev-parse", "refs/mugicrew/graphs/alice")
	err := push(c, "alice")
	if _, ok := err.(*ForeignGraphRef); !ok {
		t.Fatalf("unrelated push: %v", err)
	}
	if after := sh(forge, "git", "rev-parse", "refs/mugicrew/graphs/alice"); after != before {
		t.Error("foreign push moved the ref")
	}
}

func mustAtoi(s string) int { n, _ := strconv.Atoi(s); return n }
