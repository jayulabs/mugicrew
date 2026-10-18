package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/jira"
)

// doctorWS is a temp git Workspace, chdir'd into, with PATH lookup and bd stubbed.
func doctorWS(t *testing.T, missing ...string) string {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	for _, a := range [][]string{{"init", "-q"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "i"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, a...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	t.Setenv("MUGICREW_MD_RENDERER", "")
	t.Setenv("BROWSER", "wslview")
	t.Chdir(dir)
	oldLook, oldBD, oldVer, oldExe, oldOS := lookPath, doctorBD, doctorToolVersion, doctorExe, doctorOS
	t.Cleanup(func() {
		lookPath, doctorBD, doctorToolVersion, doctorExe, doctorOS = oldLook, oldBD, oldVer, oldExe, oldOS
	})
	doctorExe = func() (string, error) { return "/bin/mugicrew", nil }
	doctorToolVersion = func(bin string, _ ...string) (string, error) { return healthyVersions[bin], nil }
	lookPath = func(bin string) (string, error) {
		for _, m := range missing {
			if m == bin {
				return "", errors.New("missing")
			}
		}
		return "/bin/" + bin, nil
	}
	doctorBD = func(_ string, args ...string) (string, error) {
		if args[0] == "where" {
			return whereJSON(filepath.Join(dir, ".beads")), nil
		}
		return "mc", nil
	}
	t.Setenv("BEADS_DIR", "")
	return dir
}

// healthyVersions are real version outputs, all at or above their floors.
var healthyVersions = map[string]string{
	"bd":     "bd version 1.3.0 (dev)",
	"tmux":   "tmux 3.7c",
	"claude": "2.1.301 (Claude Code)",
	"dolt":   "dolt version 2.2.0",
}

func whereJSON(path string) string {
	b, _ := json.Marshal(map[string]string{"path": path})
	return string(b)
}

func doctorRun(t *testing.T) (string, int) {
	var b bytes.Buffer
	code := runDoctor(&b)
	return b.String(), code
}

func TestDoctorAllHealthy(t *testing.T) {
	dir := doctorWS(t)
	out, code := doctorRun(t)
	if code != 0 || strings.Contains(out, "FAIL") || strings.Contains(out, "warn") || !strings.Contains(out, "ok  bead graph: issue prefix mc · in-tree · "+dir) {
		t.Errorf("code %d out %q", code, out)
	}
}

func TestDoctorMissingTool(t *testing.T) {
	for tool, bin := range map[string]string{"bd": "bd", "git": "git", "tmux": "tmux", "claude": "claude"} {
		doctorWS(t, bin)
		out, code := doctorRun(t)
		if code != 1 || !strings.Contains(out, "FAIL  "+tool+":") {
			t.Errorf("%s: code %d out %q", tool, code, out)
		}
	}
}

func TestDoctorMissingRendererOnlyWarns(t *testing.T) {
	doctorWS(t, "nosuchmd")
	t.Setenv("MUGICREW_MD_RENDERER", "nosuchmd -x")
	out, code := doctorRun(t)
	if code != 0 || !strings.Contains(out, "warn  md renderer: nosuchmd not on PATH") || !strings.Contains(out, "set MUGICREW_MD_RENDERER to another renderer") {
		t.Errorf("code %d out %q", code, out)
	}
}

func TestDoctorMDRendererFollowsEnv(t *testing.T) {
	for _, c := range []struct{ env, want string }{
		{"", "ok  md renderer: built-in (mugicrew doc-pane)"},
		{"  ", "ok  md renderer: built-in (mugicrew doc-pane)"},
		{"bat -l md", "ok  md renderer: bat on PATH"},
	} {
		doctorWS(t)
		t.Setenv("MUGICREW_MD_RENDERER", c.env)
		out, _ := doctorRun(t)
		if !strings.Contains(out, c.want) {
			t.Errorf("env %q: out %q", c.env, out)
		}
	}
	doctorWS(t, "bat")
	t.Setenv("MUGICREW_MD_RENDERER", "bat -l md")
	out, code := doctorRun(t)
	if code != 0 || !strings.Contains(out, "warn  md renderer: bat not on PATH") {
		t.Errorf("code %d out %q", code, out)
	}
}

func TestDoctorNotInGitRepo(t *testing.T) {
	doctorWS(t)
	t.Chdir(t.TempDir())
	out, code := doctorRun(t)
	if code != 1 || !strings.Contains(out, "FAIL  git repo:") || !strings.Contains(out, "skip  bead graph:") || !strings.Contains(out, "skip  agent profiles:") {
		t.Errorf("code %d out %q", code, out)
	}
}

func TestDoctorBeadGraphFailure(t *testing.T) {
	doctorWS(t, "tmux")
	doctorBD = func(string, ...string) (string, error) { return "", errors.New("exit 1") }
	out, code := doctorRun(t)
	if code != 1 || !strings.Contains(out, "FAIL  bead graph:") || !strings.Contains(out, "mugicrew init") {
		t.Errorf("code %d out %q", code, out)
	}
	if !strings.Contains(out, "FAIL  tmux:") || !strings.Contains(out, "ok  md renderer: built-in (mugicrew doc-pane)") {
		t.Errorf("early failure stopped later checks: %q", out)
	}
}

func TestDoctorUnrunnableProfile(t *testing.T) {
	dir := doctorWS(t)
	os.WriteFile(filepath.Join(dir, "agent-profiles.toml"), []byte("[profiles.p]\nharness = \"nope\"\n"), 0o644)
	t.Setenv("OE_ROOT", "")
	out, code := doctorRun(t)
	if code != 0 || !strings.Contains(out, "warn  agent profile p: unsupported harness nope") {
		t.Errorf("code %d out %q", code, out)
	}
}

func TestDoctorFromLinkedWorktreeSameWorkspace(t *testing.T) {
	dir := doctorWS(t)
	wt := filepath.Join(t.TempDir(), "wt")
	if out, err := exec.Command("git", "-C", dir, "worktree", "add", "-q", "-b", "x", wt).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	want, _ := doctorRun(t)
	t.Chdir(wt)
	if got, _ := doctorRun(t); got != want {
		t.Errorf("worktree out %q, want %q", got, want)
	}
}

func snapshot(t *testing.T, dir string) string {
	var b strings.Builder
	filepath.WalkDir(dir, func(p string, d os.DirEntry, _ error) error {
		if d.IsDir() && d.Name() == "objects" {
			return filepath.SkipDir
		}
		info, _ := d.Info()
		b.WriteString(p + " " + info.ModTime().String() + "\n")
		return nil
	})
	return b.String()
}

func TestDoctorWritesNothing(t *testing.T) {
	dir := doctorWS(t)
	writeCfg(t, filepath.Join(dir, ".mugicrew", "config.toml"), "gate = \"x\"\n[context.a]\npath = \"nope\"\n[jira]\nsite = \"acme.atlassian.net\"\n")
	doctorACLISite = func() (string, error) { return "acme.atlassian.net", nil }
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	oStrat := git.LandStrategyName
	t.Cleanup(func() { git.LandStrategyName = oStrat })
	git.LandStrategyName = func() (string, error) { return git.LandStrategyGitlab, nil }
	stubGitlab(t, nil, nil, nil)
	before := snapshot(t, dir)
	doctorRun(t)
	if after := snapshot(t, dir); after != before {
		t.Errorf("workspace changed:\n%s\n--\n%s", before, after)
	}
	if _, err := os.Stat(os.Getenv("XDG_STATE_HOME")); err == nil {
		t.Error("event log dir created")
	}
}

func writeCfg(t *testing.T, path, body string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDoctorUnparsableLayersFailNamingPaths(t *testing.T) {
	dir := doctorWS(t)
	shared, local := filepath.Join(dir, ".mugicrew", "config.toml"), filepath.Join(dir, ".git", "mugicrew", "config.toml")
	writeCfg(t, shared, "= bad")
	writeCfg(t, local, "= bad")
	out, code := doctorRun(t)
	if code != 1 || !strings.Contains(out, "FAIL  config shared: "+shared) || !strings.Contains(out, "FAIL  config local: "+local) {
		t.Errorf("code %d out %q", code, out)
	}
	if !strings.Contains(out, "ok  md renderer: built-in") {
		t.Errorf("config failure stopped later checks: %q", out)
	}
}

func TestDoctorMissingLayersOK(t *testing.T) {
	doctorWS(t)
	out, code := doctorRun(t)
	if code != 0 || !strings.Contains(out, "ok  config shared: not present") || !strings.Contains(out, "ok  config local: not present") || !strings.Contains(out, "ok  contexts: none declared") || strings.Contains(out, "nix") {
		t.Errorf("code %d out %q", code, out)
	}
}

func TestDoctorLinkedWorktreeReadsMainLocalLayer(t *testing.T) {
	dir := doctorWS(t)
	writeCfg(t, filepath.Join(dir, ".git", "mugicrew", "config.toml"), "= bad")
	wt := filepath.Join(t.TempDir(), "wt")
	if out, err := exec.Command("git", "-C", dir, "worktree", "add", "-q", "-b", "x", wt).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	t.Chdir(wt)
	if out, _ := doctorRun(t); !strings.Contains(out, "FAIL  config local: "+filepath.Join(dir, ".git", "mugicrew", "config.toml")) {
		t.Errorf("out %q", out)
	}
}

func TestDoctorContexts(t *testing.T) {
	dir := doctorWS(t)
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	child := filepath.Join(dir, "child")
	os.Mkdir(child, 0o755)
	exec.Command("git", "-C", child, "init", "-q").Run()
	os.Mkdir(filepath.Join(dir, "plain"), 0o755)
	writeCfg(t, filepath.Join(dir, ".mugicrew", "config.toml"), `
[context.both]
path = "sub"
repo = "child"
[context.nopath]
path = "gone"
[context.notrepo]
repo = "plain"
[context.goodpath]
path = "sub"
[context.goodrepo]
repo = "child"
`)
	out, code := doctorRun(t)
	if code != 1 {
		t.Errorf("code %d", code)
	}
	for _, w := range []string{"FAIL  context both:", "FAIL  context nopath:", "FAIL  context notrepo:", "ok  context goodpath:", "ok  context goodrepo:"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in %q", w, out)
		}
	}
	if n := strings.Count(out, "FAIL  context"); n != 3 {
		t.Errorf("%d context FAILs in %q", n, out)
	}
}

func TestDoctorGateNeedsNix(t *testing.T) {
	dir := doctorWS(t, "nix")
	out, code := doctorRun(t)
	if code != 0 || strings.Contains(out, "nix") {
		t.Errorf("no gate: code %d out %q", code, out)
	}
	cfg := filepath.Join(dir, ".mugicrew", "config.toml")
	writeCfg(t, cfg, "gate = \"top\"\n")
	out, code = doctorRun(t)
	if code != 1 || !strings.Contains(out, "FAIL  nix:") || !strings.Contains(out, "top-level gate") {
		t.Errorf("top gate: code %d out %q", code, out)
	}
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	writeCfg(t, cfg, "[context.c]\npath = \"sub\"\ngate = \"g\"\n")
	out, code = doctorRun(t)
	if code != 1 || !strings.Contains(out, "FAIL  nix:") || !strings.Contains(out, "context c") || strings.Contains(out, "top-level") {
		t.Errorf("context gate: code %d out %q", code, out)
	}
	lookPath = func(string) (string, error) { return "/bin/x", nil }
	if out, code = doctorRun(t); code != 0 || !strings.Contains(out, "ok  nix:") {
		t.Errorf("nix present: code %d out %q", code, out)
	}
}

func TestDoctorBdStore(t *testing.T) {
	const hint = `the TUI and loop need server mode: on a fresh clone run mugicrew init; else see README "Bd store mode" to migrate`
	ctx := func(mode string) func(string, ...string) (string, error) {
		return func(_ string, args ...string) (string, error) {
			if strings.Join(args, " ") != "context --json" {
				return "mc", nil
			}
			return mode, nil
		}
	}
	for _, tc := range []struct {
		name         string
		bd           func(string, ...string) (string, error)
		concurrency  string
		noRepo       bool
		st           status
		detail, hint string
	}{
		{"server", ctx(`{"dolt_mode":"server"}`), "", false, stOK, "server", ""},
		{"embedded pool 1", ctx(`{"dolt_mode":"embedded"}`), "concurrency = 1\n", false, stFail, "embedded, AFK pool 1", hint},
		{"embedded default pool", ctx(`{"dolt_mode":"embedded"}`), "", false, stFail, "embedded, AFK pool 3", hint},
		{"embedded explicit pool", ctx(`{"dolt_mode":"embedded"}`), "concurrency = 5\n", false, stFail, "embedded, AFK pool 5", hint},
		{"bd error", func(string, ...string) (string, error) { return "", errors.New("exit 1") }, "", false, stSkip, "mode unknown", ""},
		{"unreadable json", ctx(`nope`), "", false, stSkip, "mode unknown", ""},
		{"missing dolt_mode", ctx(`{}`), "", false, stSkip, "mode unknown", ""},
		{"unknown dolt_mode", ctx(`{"dolt_mode":"sqlite"}`), "", false, stSkip, "mode unknown", ""},
		{"no git repo", ctx(`{"dolt_mode":"server"}`), "", true, stSkip, "no git repo", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := doctorWS(t)
			doctorBD = tc.bd
			if tc.concurrency != "" {
				writeCfg(t, filepath.Join(dir, ".mugicrew", "config.toml"), tc.concurrency)
			}
			root := dir
			if tc.noRepo {
				root = ""
			}
			got := checkBdStore(root)
			want := []result{{tc.st, "bd store", tc.detail, tc.hint}}
			if len(got) != 1 || got[0] != want[0] {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

func TestDoctorBdStoreWarnKeepsExitCodeZero(t *testing.T) {
	doctorWS(t)
	doctorBD = func(_ string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "context" {
			return `{"dolt_mode":"embedded"}`, nil
		}
		if args[0] == "where" {
			return whereJSON("/x"), nil
		}
		return "mc", nil
	}
	out, code := doctorRun(t)
	bg, st := strings.Index(out, "bead graph:"), strings.Index(out, "bd store:")
	if code != 1 || !strings.Contains(out, "FAIL  bd store: embedded, AFK pool 3") || st < bg || bg < 0 {
		t.Errorf("code %d out %q", code, out)
	}
}

func TestDoctorBeadGraphSource(t *testing.T) {
	link := func(t *testing.T, target string) string {
		l := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(target, l); err != nil {
			t.Fatal(err)
		}
		return l
	}
	for _, tc := range []struct {
		name, source string
		setup        func(t *testing.T, root string) (path string) // returns bd where's path; may set BEADS_DIR
	}{
		{"BEADS_DIR", "BEADS_DIR", func(t *testing.T, root string) string {
			g := t.TempDir()
			t.Setenv("BEADS_DIR", g)
			return g
		}},
		{"BEADS_DIR via symlink", "BEADS_DIR", func(t *testing.T, root string) string {
			g := t.TempDir()
			t.Setenv("BEADS_DIR", link(t, g))
			return g
		}},
		{"in-tree", "in-tree", func(t *testing.T, root string) string { return filepath.Join(root, ".beads") }},
		{"in-tree via symlinked path", "in-tree", func(t *testing.T, root string) string {
			os.Mkdir(filepath.Join(root, ".beads"), 0o755)
			return filepath.Join(link(t, root), ".beads")
		}},
		{"redirect", "redirect", func(t *testing.T, root string) string {
			writeCfg(t, filepath.Join(root, ".beads", "redirect"), "../elsewhere\n")
			return t.TempDir()
		}},
		{"routed", "routed", func(t *testing.T, root string) string { return t.TempDir() }},
		{"other BEADS_DIR is not BEADS_DIR", "routed", func(t *testing.T, root string) string {
			t.Setenv("BEADS_DIR", t.TempDir())
			return t.TempDir()
		}},
		{"other BEADS_DIR falls to in-tree", "in-tree", func(t *testing.T, root string) string {
			t.Setenv("BEADS_DIR", t.TempDir())
			return filepath.Join(root, ".beads")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := doctorWS(t)
			path := tc.setup(t, dir)
			doctorBD = func(_ string, args ...string) (string, error) {
				if args[0] == "where" {
					return whereJSON(path), nil
				}
				return "mc", nil
			}
			got := checkBeadGraph(dir)
			want := result{stOK, "bead graph", "issue prefix mc · " + tc.source + " · " + path, ""}
			if len(got) != 1 || got[0] != want {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

func TestDoctorBeadGraphUnresolvable(t *testing.T) {
	for name, bd := range map[string]func(string, ...string) (string, error){
		"where fails": func(_ string, a ...string) (string, error) {
			if a[0] == "where" {
				return "", errors.New("x")
			}
			return "mc", nil
		},
		"where empty": func(_ string, a ...string) (string, error) {
			if a[0] == "where" {
				return "{}", nil
			}
			return "mc", nil
		},
		"where junk": func(_ string, a ...string) (string, error) {
			if a[0] == "where" {
				return "nope", nil
			}
			return "mc", nil
		},
		"prefix fails": func(_ string, a ...string) (string, error) {
			if a[0] == "where" {
				return whereJSON("/x"), nil
			}
			return "", errors.New("x")
		},
		"prefix empty": func(_ string, a ...string) (string, error) {
			if a[0] == "where" {
				return whereJSON("/x"), nil
			}
			return "", nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := doctorWS(t)
			doctorBD = bd
			got := checkBeadGraph(dir)
			want := result{stFail, "bead graph", "not initialized or not resolvable", "run mugicrew init"}
			if len(got) != 1 || got[0] != want {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

func TestDoctorSequences(t *testing.T) {
	dir := doctorWS(t)
	cfg := filepath.Join(dir, ".mugicrew", "config.toml")
	for _, f := range []string{"rfc/0001-a.md", "rfc/0002-b.md"} {
		writeCfg(t, filepath.Join(dir, f), "x")
	}
	for _, a := range [][]string{{"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "f"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, a...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	for _, tc := range []struct{ name, cfg, want string }{
		{"builtin only", "", ""},
		{"pass", "sequences = [\"rfc/{n:4}-*.md\"]\n", "ok  sequences workspace:"},
		{"unusable rule", "sequences = [\"rfc/{x}-*.md\"]\n", "FAIL  sequences workspace:"},
		{"unparseable", "= bad", "FAIL  sequences workspace:"},
		{"overlap", "sequences = [\"rfc/{n:4}-*.md\", \"**/{n}-a.md\"]\n", "FAIL  sequences workspace: git: numbered-file rules overlap on rfc/0001-a.md"},
	} {
		os.Remove(cfg)
		if tc.cfg != "" {
			writeCfg(t, cfg, tc.cfg)
		}
		out, _ := doctorRun(t)
		if tc.want == "" && strings.Contains(out, "sequences") || !strings.Contains(out, tc.want) {
			t.Errorf("%s: want %q in %q", tc.name, tc.want, out)
		}
	}
}

func TestDoctorDoltRemoteInfo(t *testing.T) {
	oStrat, oHas := git.LandStrategyName, doctorForgeHasRef
	t.Cleanup(func() { git.LandStrategyName, doctorForgeHasRef = oStrat, oHas })
	for _, tc := range []struct {
		name, strategy string
		has            bool
		want, not      string
	}{
		{"present", git.LandStrategyGitlab, true, "warn  graph ref: refs/heads/__dolt_remote_info__ on the Forge — set DOLT_REMOTE_INFO_BRANCH= in .beads/.env (run mugicrew init)", ""},
		{"absent", git.LandStrategyGitlab, false, "ok  graph ref:", "warn"},
		{"local", git.LandStrategyLocal, true, "", "graph ref"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doctorWS(t)
			git.LandStrategyName = func() (string, error) { return tc.strategy, nil }
			stubGitlab(t, nil, nil, nil)
			doctorForgeHasRef = func(ref string) (bool, error) { return tc.has && ref == bd.DoltInfoRef, nil }
			out, code := doctorRun(t)
			if code != 0 || !strings.Contains(out, tc.want) || (tc.not != "" && strings.Contains(out, tc.not)) {
				t.Errorf("code %d out %q", code, out)
			}
		})
	}
}

func TestDoctorBrowserWarnsNeverFails(t *testing.T) {
	for _, c := range []struct {
		env     string
		missing []string
		want    string
	}{
		{"", nil, "warn  browser: BROWSER unset"},
		{"  ", nil, "warn  browser: BROWSER unset"},
		{"wslview", nil, "ok  browser: wslview on PATH"},
		{"wslview", []string{"wslview"}, "warn  browser: wslview not on PATH"},
		{"firefox --new-tab", nil, "ok  browser: firefox on PATH"},
		{"firefox --new-tab", []string{"firefox"}, "warn  browser: firefox not on PATH"},
	} {
		doctorWS(t, c.missing...)
		t.Setenv("BROWSER", c.env)
		out, code := doctorRun(t)
		if code != 0 || !strings.Contains(out, c.want) || strings.Contains(out, "FAIL") {
			t.Errorf("env %q: code %d out %q", c.env, code, out)
		}
		if strings.HasPrefix(c.want, "warn") && !strings.Contains(out, "prototype hand-over prints the target only; set BROWSER") {
			t.Errorf("env %q: no hint in %q", c.env, out)
		}
	}
}

func TestDoctorTrackedBeads(t *testing.T) {
	dir := doctorWS(t)
	out, code := doctorRun(t)
	if code != 0 || !strings.Contains(out, "ok  beads untracked") {
		t.Errorf("untracked: code %d out %q", code, out)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".beads", "metadata.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if o, err := exec.Command("git", "-C", dir, "add", "-f", ".beads/metadata.json").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, o)
	}
	out, code = doctorRun(t)
	for _, want := range []string{"FAIL  beads tracked", ".beads/metadata.json", trackedBeadsFix} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	if code != 1 {
		t.Errorf("code %d", code)
	}
}

// stubGitlab stubs checkGitlab's seams: two projects (the Workspace's and context c's) unless
// projs is given, auth error authErr, and settings refusals by project ID.
func stubGitlab(t *testing.T, projs []git.GitlabProject, authErr error, refuse map[string]error) {
	t.Helper()
	oP, oC, oU, oS := doctorGitlabProjects, doctorForgeCheck, doctorForgeUser, doctorForgeSettings
	t.Cleanup(func() { doctorGitlabProjects, doctorForgeCheck, doctorForgeUser, doctorForgeSettings = oP, oC, oU, oS })
	if projs == nil {
		projs = []git.GitlabProject{{Project: forge.Project{Host: "gl.example.com", ID: "ns/p"}}}
	}
	doctorGitlabProjects = func() ([]git.GitlabProject, error) { return projs, nil }
	oF, oK := doctorForgeURL, doctorCredentialFill
	t.Cleanup(func() { doctorForgeURL, doctorCredentialFill = oF, oK })
	doctorForgeURL = func() (string, error) { return "https://gl.example.com/ns/p.git", nil }
	doctorCredentialFill = func(string, string, []string) error { return nil }
	doctorForgeCheck = func(string) error { return authErr }
	doctorForgeUser = func(forge.Project) (string, error) { return "ana", nil }
	doctorForgeSettings = func(p forge.Project) error { return refuse[p.ID] }
}

func TestDoctorGitlabGroup(t *testing.T) {
	oStrat := git.LandStrategyName
	t.Cleanup(func() { git.LandStrategyName = oStrat })
	projs := []git.GitlabProject{
		{Project: forge.Project{Host: "gl.example.com", ID: "ns/p"}},
		{Context: "c", Project: forge.Project{Host: "gl.example.com", ID: "ns/c"}},
	}
	refuse := map[string]error{"ns/c": &forge.ProjectRefusal{Field: "squash_option", Value: "default_off", Fix: "set Always"}}
	for _, tc := range []struct {
		name, strategy string
		authErr        error
		want, not      []string
	}{
		{"ok", git.LandStrategyGitlab, nil, []string{"ok  glab user: ana", "ok  squash settings ns/p: ok", "ok  git credential: credential available", "FAIL  squash settings ns/c (context c): forge: project squash_option is \"default_off\""}, nil},
		{"auth", git.LandStrategyGitlab, errors.New("no login"), []string{"FAIL  glab user: not authenticated for gl.example.com: no login — run one interactive web glab auth login (docs/install.md"}, []string{"squash settings"}},
		{"local", git.LandStrategyLocal, nil, nil, []string{"glab user", "squash settings", "git credential"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doctorWS(t)
			git.LandStrategyName = func() (string, error) { return tc.strategy, nil }
			stubGitlab(t, projs, tc.authErr, refuse)
			out, _ := doctorRun(t)
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("missing %q in %q", w, out)
				}
			}
			for _, n := range tc.not {
				if strings.Contains(out, n) {
					t.Errorf("unexpected %q in %q", n, out)
				}
			}
		})
	}
}

func TestDoctorGitlabNoGlab(t *testing.T) {
	oStrat := git.LandStrategyName
	t.Cleanup(func() { git.LandStrategyName = oStrat })
	doctorWS(t)
	git.LandStrategyName = func() (string, error) { return git.LandStrategyGitlab, nil }
	stubGitlab(t, nil, errors.New("exec: glab not found"), nil)
	look := lookPath
	lookPath = func(bin string) (string, error) {
		if bin == "glab" {
			return "", errors.New("missing")
		}
		return look(bin)
	}
	out, _ := doctorRun(t)
	for _, n := range []string{"glab user", "squash settings"} {
		if strings.Contains(out, n) {
			t.Errorf("unexpected %q in %q", n, out)
		}
	}
}

func TestDoctorJiraSite(t *testing.T) {
	for _, c := range []struct {
		name, cfg, active string
		err               error
		want              string
	}{
		{"match", "acme.atlassian.net", "acme.atlassian.net", nil, "ok  acli site: acme.atlassian.net"},
		{"mismatch", "acme.atlassian.net", "other.atlassian.net", nil, "FAIL  acli site: acli is on other.atlassian.net, [jira] site is acme.atlassian.net — acli jira auth switch --site acme.atlassian.net"},
		{"not logged in", "acme.atlassian.net", "", jira.ErrNotLoggedIn, "FAIL  acli site: not logged in — acli jira auth login"},
		{"malformed", "https://acme.atlassian.net", "", nil, "FAIL  jira site: https://acme.atlassian.net"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := doctorWS(t)
			old := doctorACLISite
			t.Cleanup(func() { doctorACLISite = old })
			doctorACLISite = func() (string, error) { return c.active, c.err }
			writeCfg(t, filepath.Join(dir, ".mugicrew", "config.toml"), "[jira]\nsite = \""+c.cfg+"\"\n")
			out, _ := doctorRun(t)
			if !strings.Contains(out, c.want) {
				t.Errorf("out %q", out)
			}
		})
	}
	t.Cleanup(func(old func() (string, error)) func() { return func() { doctorACLISite = old } }(doctorACLISite))
	doctorWS(t)
	doctorACLISite = func() (string, error) { t.Error("acli read without [jira]"); return "", nil }
	if out, _ := doctorRun(t); strings.Contains(out, "acli") {
		t.Errorf("acli line without [jira]: %q", out)
	}
}

func TestDoctorGitlabProjectsErrKeepsCredential(t *testing.T) {
	oStrat := git.LandStrategyName
	t.Cleanup(func() { git.LandStrategyName = oStrat })
	doctorWS(t)
	git.LandStrategyName = func() (string, error) { return git.LandStrategyGitlab, nil }
	stubGitlab(t, nil, nil, nil)
	doctorGitlabProjects = func() ([]git.GitlabProject, error) { return nil, errors.New("no remote") }
	out, _ := doctorRun(t)
	if !strings.Contains(out, "FAIL  gitlab projects: no remote") || !strings.Contains(out, "git credential") || strings.Contains(out, "glab user") {
		t.Errorf("out %q", out)
	}
}

func TestDoctorJiraNoAcliSingleLine(t *testing.T) {
	dir := doctorWS(t)
	old := doctorACLISite
	t.Cleanup(func() { doctorACLISite = old })
	doctorACLISite = func() (string, error) { return "", jira.ErrNotFound }
	writeCfg(t, filepath.Join(dir, ".mugicrew", "config.toml"), "[jira]\nsite = \"acme.atlassian.net\"\n")
	if out, _ := doctorRun(t); strings.Contains(out, "acli site") {
		t.Errorf("duplicate acli site line: %q", out)
	}
}

func TestDoctorToolVersions(t *testing.T) {
	for _, c := range []struct{ tool, out, want string }{
		{"bd", "bd version 1.3.0 (dev)", "ok  bd: 1.3.0"},
		{"bd", "bd version 1.2.1 (dev)", "FAIL  bd: 1.2.1 is below 1.3.0"},
		{"tmux", "tmux 3.7c", "ok  tmux: 3.7c"},
		{"tmux", "tmux 3.4", "FAIL  tmux: 3.4 is below 3.5"},
		{"tmux", "tmux 3.5a", "ok  tmux: 3.5a"},
		{"claude", "2.1.301 (Claude Code)", "ok  claude: 2.1.301"},
		{"claude", "2.1.77 (Claude Code)", "FAIL  claude: 2.1.77 is below 2.1.78"},
		{"dolt", "dolt version 2.2.0", "ok  dolt: 2.2.0"},
		{"dolt", "dolt version 2.4.2", "warn  dolt: 2.4.2"},
		{"bd", "garbage", "warn  bd: on PATH, version unreadable"},
	} {
		t.Run(c.tool+" "+c.out, func(t *testing.T) {
			doctorWS(t)
			old := doctorToolVersion
			t.Cleanup(func() { doctorToolVersion = old })
			doctorToolVersion = func(bin string, _ ...string) (string, error) {
				if bin == c.tool {
					return c.out, nil
				}
				return old(bin)
			}
			out, code := doctorRun(t)
			if !strings.Contains(out, c.want) {
				t.Errorf("want %q in %q", c.want, out)
			}
			if failing := strings.HasPrefix(c.want, "FAIL"); failing != (code == 1) {
				t.Errorf("code %d", code)
			}
		})
	}
}

func TestDoctorAbsentToolHintHasFloor(t *testing.T) {
	for goos, hint := range map[string]string{"linux": "distro package may be older than 3.5", "darwin": "brew install tmux"} {
		t.Run(goos, func(t *testing.T) {
			doctorWS(t, "tmux", "dolt")
			doctorOS = goos
			out, code := doctorRun(t)
			if code != 1 || !strings.Contains(out, "FAIL  tmux: not on PATH — need ≥ 3.5: install ") || !strings.Contains(out, hint) || !strings.Contains(out, "FAIL  dolt: not on PATH — install pinned 2.2.0 tarball, see docs/install.md#dolt") {
				t.Errorf("code %d out %q", code, out)
			}
		})
	}
}

func TestDoctorGlabAcliGated(t *testing.T) {
	doctorWS(t, "glab", "acli")
	out, code := doctorRun(t)
	if code != 0 || strings.Contains(out, "glab") || strings.Contains(out, "acli") {
		t.Errorf("ungated: code %d out %q", code, out)
	}
	dir := doctorWS(t, "glab", "acli")
	oldLS := git.LandStrategyName
	git.LandStrategyName = func() (string, error) { return git.LandStrategyGitlab, nil }
	t.Cleanup(func() { git.LandStrategyName = oldLS })
	os.MkdirAll(filepath.Join(dir, ".mugicrew"), 0o755)
	os.WriteFile(filepath.Join(dir, ".mugicrew", "config.toml"), []byte("[jira]\nsite = \"x.atlassian.net\"\n"), 0o644)
	out, code = doctorRun(t)
	if code != 1 || !strings.Contains(out, "FAIL  glab: not on PATH") || !strings.Contains(out, "FAIL  acli: not on PATH") {
		t.Errorf("gated: code %d out %q", code, out)
	}
}

func TestDoctorBinaryLine(t *testing.T) {
	doctorWS(t)
	out, _ := doctorRun(t)
	if !strings.Contains(out, "ok  binary: /bin/mugicrew (version ") {
		t.Errorf("same file: %q", out)
	}
	real, _ := filepath.EvalSymlinks(t.TempDir())
	a, b := filepath.Join(real, "a"), filepath.Join(real, "b")
	os.WriteFile(a, nil, 0o755)
	os.WriteFile(b, nil, 0o755)
	link := filepath.Join(real, "link")
	os.Symlink(a, link)
	doctorExe = func() (string, error) { return a, nil }
	for path, warn := range map[string]bool{b: true, link: false} {
		lookPath = func(bin string) (string, error) { return path, nil }
		out, code := doctorRun(t)
		if got := strings.Contains(out, "warn  binary: "+a); got != warn || code != 0 {
			t.Errorf("PATH %s: warn %v code %d out %q", path, got, code, out)
		}
	}
}

func TestDoctorGitCredential(t *testing.T) {
	oStrat := git.LandStrategyName
	t.Cleanup(func() { git.LandStrategyName = oStrat })
	hint := "glab cannot answer git for gl.example.com: set glab's user (glab config set user <name> --host gl.example.com) or re-run glab auth login (docs/install.md, GitLab section)"
	for _, tc := range []struct {
		name, want string
		err        error
	}{
		{"ok", "ok  git credential: credential available", nil},
		{"fail", "FAIL  git credential: git credential fill: exit status 128: no user — " + hint, errors.New("git credential fill: exit status 128: no user")},
		{"skip", "skip  git credential: ssh remote", git.ErrCredentialNA},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doctorWS(t)
			git.LandStrategyName = func() (string, error) { return git.LandStrategyGitlab, nil }
			stubGitlab(t, nil, nil, nil)
			inner := doctorBD
			doctorBD = func(d string, args ...string) (string, error) {
				if args[0] == "sql" || args[0] == "dolt" { // the probe must not touch bd's sql-server
					t.Errorf("bd server call: bd %v", args)
				}
				return inner(d, args...)
			}
			var dir string
			var env []string
			doctorCredentialFill = func(_, d string, e []string) error { dir, env = d, e; return tc.err }
			out, code := doctorRun(t)
			if !strings.Contains(out, tc.want) {
				t.Errorf("missing %q in %q", tc.want, out)
			}
			if strings.Contains(out, "graph push credential") {
				t.Errorf("graph push credential line remains: %q", out)
			}
			if (tc.err != nil && !errors.Is(tc.err, git.ErrCredentialNA)) != (code != 0) {
				t.Errorf("code %d for err %v", code, tc.err)
			}
			if dir != os.TempDir() || !slices.Equal(env, git.GlabHelperEnv("https://gl.example.com/ns/p.git")) {
				t.Errorf("fill ran in %q with env %v", dir, env)
			}
		})
	}
}
