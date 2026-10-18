package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/forge"
)

// initWS is a temp git repo, chdir'd into, with every init seam stubbed: bd calls
// are recorded and bd init "creates" <dir>/.beads.
type initEnv struct {
	dir         string
	calls       [][]string
	graph       bool // a graph already resolves
	tty         bool
	prompts     int
	storePrefix string // what bd config get issue_prefix reads
	storeErr    error
}

func initWS(t *testing.T) *initEnv {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	for _, a := range [][]string{{"init", "-q"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "i"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, a...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	t.Chdir(dir)
	e := &initEnv{dir: dir}
	oldBD, oldGraph, oldTTY, oldProj := initBD, graphDir, initIsTTY, forgeProject
	oldUser := forgeUserName
	t.Cleanup(func() {
		initBD, graphDir, initIsTTY, forgeProject, forgeUserName = oldBD, oldGraph, oldTTY, oldProj, oldUser
	})
	initBD = func(_ string, args ...string) (string, error) {
		if args[0] == "config" { // a read, not a store change
			return e.storePrefix, e.storeErr
		}
		e.calls = append(e.calls, args)
		if args[0] == "init" {
			e.graph = true
			os.MkdirAll(filepath.Join(dir, ".beads"), 0o755)
			os.WriteFile(filepath.Join(dir, ".git", "info", "exclude"), []byte(".beads/\n"), 0o644) // what --stealth does
		}
		return "", nil
	}
	graphDir = func(string) (string, error) {
		if !e.graph {
			return "", &bd.NoGraphError{}
		}
		return filepath.Join(dir, ".beads"), nil
	}
	initIsTTY = func() bool { return e.tty }
	forgeProject = func() (forge.Project, error) { return forge.Project{}, errors.New("no forge") }
	return e
}

func (e *initEnv) run(t *testing.T, in string, args ...string) (string, int) {
	var out bytes.Buffer
	code := runInit(args, strings.NewReader(in), &out)
	return out.String(), code
}

func (e *initEnv) status(t *testing.T) string {
	out, _ := exec.Command("git", "-C", e.dir, "status", "--porcelain").Output()
	return string(out)
}

func (e *initEnv) bdVerbs() []string {
	var v []string
	for _, c := range e.calls {
		v = append(v, strings.Join(c, " "))
	}
	return v
}

func TestInitRefusesTrackedBeadsWithoutRunningBd(t *testing.T) {
	e := initWS(t)
	os.MkdirAll(filepath.Join(e.dir, ".beads"), 0o755)
	os.WriteFile(filepath.Join(e.dir, ".beads", "metadata.json"), []byte("{}"), 0o644)
	exec.Command("git", "-C", e.dir, "add", "-f", ".beads/metadata.json").Run()
	out, code := e.run(t, "", "--prefix", "ab")
	if code == 0 || len(e.calls) != 0 {
		t.Fatalf("code=%d calls=%v", code, e.calls)
	}
	if !strings.Contains(out, ".beads/metadata.json") || !strings.Contains(out, "git rm -r --cached .beads") {
		t.Fatalf("guidance missing: %s", out)
	}
}

func TestInitRunsExactBdInitThenEnvThenStop(t *testing.T) {
	e := initWS(t)
	out, code := e.run(t, "", "--prefix", "ab")
	if code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	want := []string{"init --server --non-interactive --stealth --prefix ab", "dolt stop"}
	if got := e.bdVerbs(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("bd calls %v, want %v", got, want)
	}
	env, _ := os.ReadFile(filepath.Join(e.dir, ".beads", ".env"))
	if string(env) != "DOLT_REMOTE_INFO_BRANCH=\n" {
		t.Fatalf(".env = %q", env)
	}
	if s := e.status(t); s != "" {
		t.Fatalf("git status not clean: %q", s)
	}
	if !strings.Contains(out, "ab") || !strings.Contains(out, "server") || !strings.Contains(out, "mugicrew doctor") {
		t.Fatalf("report: %s", out)
	}
}

func TestInitEnvMergeKeepsLinesAndIsIdempotent(t *testing.T) {
	e := initWS(t)
	e.graph = true
	os.MkdirAll(filepath.Join(e.dir, ".beads"), 0o755)
	p := filepath.Join(e.dir, ".beads", ".env")
	os.WriteFile(p, []byte("BEADS_DOLT_PASSWORD=x"), 0o600) // no trailing newline
	if _, code := e.run(t, ""); code != 0 {
		t.Fatal(code)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "BEADS_DOLT_PASSWORD=x\nDOLT_REMOTE_INFO_BRANCH=\n" {
		t.Fatalf(".env = %q", b)
	}
	if got := e.bdVerbs(); len(got) != 1 || got[0] != "dolt stop" {
		t.Fatalf("calls %v", got) // bootstrapped: no bd init
	}
	e.calls = nil
	out, code := e.run(t, "")
	if code != 0 || len(e.calls) != 0 {
		t.Fatalf("rerun: code=%d calls=%v", code, e.calls)
	}
	if !strings.Contains(out, "left as is") {
		t.Fatalf("report: %s", out)
	}
	if b2, _ := os.ReadFile(p); string(b2) != string(b) {
		t.Fatalf("rerun changed .env: %q", b2)
	}
}

func TestInitExistingKeyWithValueIsKept(t *testing.T) {
	e := initWS(t)
	e.graph = true
	os.MkdirAll(filepath.Join(e.dir, ".beads"), 0o755)
	p := filepath.Join(e.dir, ".beads", ".env")
	os.WriteFile(p, []byte("DOLT_REMOTE_INFO_BRANCH=custom\n"), 0o600)
	e.run(t, "")
	if b, _ := os.ReadFile(p); string(b) != "DOLT_REMOTE_INFO_BRANCH=custom\n" || len(e.calls) != 0 {
		t.Fatalf(".env %q calls %v", b, e.calls)
	}
}

func TestInitNoTTYWithoutPrefixRefuses(t *testing.T) {
	e := initWS(t)
	out, code := e.run(t, "")
	if code == 0 || len(e.calls) != 0 || !strings.Contains(out, "--prefix") {
		t.Fatalf("code=%d calls=%v out=%s", code, e.calls, out)
	}
}

func TestInitTTYPromptsWithGlabDefault(t *testing.T) {
	e := initWS(t)
	e.tty = true
	forgeProject = func() (forge.Project, error) { return forge.Project{Host: "gl.example"}, nil }
	forgeUserName = func(forge.Project) (string, error) { return "Jane.Doe_X", nil }
	out, code := e.run(t, "\n") // accept the default
	if code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	if got := e.bdVerbs()[0]; got != "init --server --non-interactive --stealth --prefix jane-doe-x" {
		t.Fatalf("argv %q (prompt: %s)", got, out)
	}
	e2 := initWS(t)
	e2.tty = true
	e2.run(t, "mine\n")
	if got := e2.bdVerbs()[0]; !strings.HasSuffix(got, "--prefix mine") {
		t.Fatalf("argv %q", got)
	}
}

// TestInitRealBd drives a real bd in a scratch BEADS_DIR-free clone: server mode,
// dolt_database follows the prefix, and nothing is committed.
func TestInitRealBd(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd absent")
	}
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt absent")
	}
	e := initWS(t)
	initBD, graphDir = bd.RunIn, bd.GraphDir
	t.Setenv("BEADS_DIR", "") // restored on cleanup
	os.Unsetenv("BEADS_DIR")  // an ambient BEADS_DIR would aim bd at the real graph
	t.Cleanup(func() { exec.Command("bd", "-C", e.dir, "dolt", "stop").Run() })
	head := func() string {
		o, _ := exec.Command("git", "-C", e.dir, "rev-parse", "HEAD").Output()
		return string(o)
	}
	before := head()
	out, code := e.run(t, "", "--prefix", "zq")
	if code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	meta, _ := os.ReadFile(filepath.Join(e.dir, ".beads", "metadata.json"))
	if !strings.Contains(string(meta), `"server"`) || !strings.Contains(string(meta), `"dolt_database": "zq"`) {
		t.Fatalf("metadata: %s", meta)
	}
	if head() != before || e.status(t) != "" {
		t.Fatalf("committed or dirty: %q", e.status(t))
	}
}

func TestInitSamePrefixOnResolvingGraphIsARerun(t *testing.T) {
	e := initWS(t)
	e.graph, e.storePrefix = true, "p"
	os.MkdirAll(filepath.Join(e.dir, ".beads"), 0o755)
	out, code := e.run(t, "", "--prefix", "p")
	env, _ := os.ReadFile(filepath.Join(e.dir, ".beads", ".env"))
	if code != 0 || !strings.Contains(out, "left as is") || !strings.Contains(string(env), "DOLT_REMOTE_INFO_BRANCH=") {
		t.Fatalf("code=%d out=%s env=%q", code, out, env)
	}
	for _, c := range e.calls {
		if c[0] == "init" {
			t.Fatalf("bd init ran: %v", e.calls)
		}
	}
}

func TestInitDifferentOrUnreadablePrefixOnResolvingGraphRefuses(t *testing.T) {
	for name, set := range map[string]func(*initEnv){
		"different":  func(e *initEnv) { e.storePrefix = "other" },
		"unreadable": func(e *initEnv) { e.storeErr = errors.New("boom") },
	} {
		t.Run(name, func(t *testing.T) {
			e := initWS(t)
			e.graph = true
			set(e)
			os.MkdirAll(filepath.Join(e.dir, ".beads"), 0o755)
			out, code := e.run(t, "", "--prefix", "p")
			if code == 0 || len(e.calls) != 0 || !strings.Contains(out, "bd init --server --stealth --prefix p") {
				t.Fatalf("code=%d calls=%v out=%s", code, e.calls, out)
			}
		})
	}
}
