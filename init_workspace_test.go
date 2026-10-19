package main

import (
	"slices"

	"jayulabs.org/mugicrew/cfglayer"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// wsEnv is initWS on a repo whose trunk is main, run from a linked worktree.
func wsEnv(t *testing.T) (*initEnv, string) {
	t.Helper()
	e := initWS(t)
	wt := e.dir + "-wt"
	for _, a := range [][]string{{"branch", "-M", "main"}, {"worktree", "add", "-q", wt, "-b", "feat", "main"}} {
		if out, err := exec.Command("git", append([]string{"-C", e.dir}, a...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	t.Cleanup(func() { exec.Command("git", "-C", e.dir, "worktree", "remove", "--force", wt).Run() })
	t.Chdir(wt)
	return e, wt
}

func (e *initEnv) ws(t *testing.T, args ...string) (string, int) {
	return e.run(t, "", append([]string{"workspace"}, args...)...)
}

func cfgPath(wt string) string { return filepath.Join(wt, ".mugicrew", "config.toml") }

func readCfg(t *testing.T, wt string) string {
	b, err := os.ReadFile(cfgPath(wt))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func putCfg(t *testing.T, wt, src string) {
	os.MkdirAll(filepath.Dir(cfgPath(wt)), 0o755)
	if err := os.WriteFile(cfgPath(wt), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

var allFlags = []string{"--land-strategy", "local", "--require-initiatives", "false", "--external-ref", "false", "--jira-site", ""}

func flagsWith(set map[string]string, drop string) []string {
	var args []string
	for i := 0; i < len(allFlags); i += 2 {
		if allFlags[i] == drop {
			continue
		}
		v := allFlags[i+1]
		if s, ok := set[allFlags[i]]; ok {
			v = s
		}
		args = append(args, allFlags[i], v)
	}
	return args
}

func TestInitWorkspaceFirstRunWritesEveryOwnedKey(t *testing.T) {
	e, wt := wsEnv(t)
	out, code := e.ws(t, allFlags...)
	if code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	got := readCfg(t, wt)
	for _, want := range []string{`land_strategy = "local"`, "[initiatives]", "required = false", "external_ref = false"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "jira") {
		t.Fatalf("blank site wrote a jira key:\n%s", got)
	}
	if s := e.status(t); s != "" {
		t.Fatalf("main worktree status %q", s)
	}
	if st, _ := exec.Command("git", "-C", wt, "status", "--porcelain").Output(); strings.TrimSpace(string(st)) != "?? .mugicrew/" {
		t.Fatalf("worktree status %q", st)
	}
	if n, _ := exec.Command("git", "-C", wt, "rev-list", "--count", "main..HEAD").Output(); strings.TrimSpace(string(n)) != "0" {
		t.Fatalf("a commit was made: %s", n)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if last := lines[len(lines)-1]; !strings.Contains(last, "mugicrew doctor") {
		t.Fatalf("last line %q", last)
	}
	for _, want := range []string{".mugicrew/config.toml", "pull"} {
		if !strings.Contains(out, want) {
			t.Fatalf("landing steps lack %q: %s", want, out)
		}
	}
	if len(e.calls) != 0 {
		t.Fatalf("bd called: %v", e.calls)
	}
}

func TestInitWorkspaceWritesGivenValues(t *testing.T) {
	e, wt := wsEnv(t)
	out, code := e.ws(t, "--land-strategy", "gitlab", "--require-initiatives", "true", "--external-ref", "true", "--jira-site", "acme.atlassian.net")
	if code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	got := readCfg(t, wt)
	for _, want := range []string{`land_strategy = "gitlab"`, "required = true", "external_ref = true", `site = "acme.atlassian.net"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestInitWorkspaceRefusesOnMainline(t *testing.T) {
	e, _ := wsEnv(t)
	t.Chdir(e.dir)
	out, code := e.ws(t, allFlags...)
	if code == 0 || !strings.Contains(out, "git worktree add ../<dir> -b <branch> main") {
		t.Fatalf("code %d: %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(e.dir, ".mugicrew")); err == nil {
		t.Fatal("wrote on mainline")
	}
}

func TestInitWorkspaceNoTerminalMissingFlagRefusesNamingIt(t *testing.T) {
	for _, drop := range []string{"--land-strategy", "--require-initiatives", "--jira-site"} {
		e, wt := wsEnv(t)
		out, code := e.ws(t, flagsWith(nil, drop)...)
		if code == 0 || !strings.Contains(out, drop) {
			t.Fatalf("drop %s: code %d: %s", drop, code, out)
		}
		if _, err := os.Stat(cfgPath(wt)); err == nil {
			t.Fatalf("drop %s: wrote a file", drop)
		}
	}
}

func TestInitWorkspaceExternalRefAskedOnlyWhenRequired(t *testing.T) {
	e, wt := wsEnv(t)
	if out, code := e.ws(t, flagsWith(nil, "--external-ref")...); code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	if !strings.Contains(readCfg(t, wt), "external_ref = false") {
		t.Fatal("external_ref not written explicitly")
	}
	e2, _ := wsEnv(t)
	out, code := e2.ws(t, flagsWith(map[string]string{"--require-initiatives": "true"}, "--external-ref")...)
	if code == 0 || !strings.Contains(out, "--external-ref") {
		t.Fatalf("required with no external-ref answer: code %d: %s", code, out)
	}
}

func TestInitWorkspaceFormatRulesRefuseFromFlags(t *testing.T) {
	for _, c := range []struct{ flag, val, rule string }{
		{"--land-strategy", "svn", "local or gitlab"},
		{"--jira-site", "https://acme.atlassian.net", "bare host"},
		{"--require-initiatives", "maybe", "true or false"},
		{"--external-ref", "maybe", "true or false"},
	} {
		e, wt := wsEnv(t)
		out, code := e.ws(t, flagsWith(map[string]string{c.flag: c.val}, "")...)
		if code == 0 || !strings.Contains(out, c.flag) || !strings.Contains(out, c.rule) {
			t.Fatalf("%s=%s: code %d: %s", c.flag, c.val, code, out)
		}
		if _, err := os.Stat(cfgPath(wt)); err == nil {
			t.Fatalf("%s: wrote a file", c.flag)
		}
	}
}

func TestInitWorkspaceExternalRefNeedsRequired(t *testing.T) {
	e, _ := wsEnv(t)
	out, code := e.ws(t, flagsWith(map[string]string{"--external-ref": "true"}, "")...)
	if code == 0 || !strings.Contains(out, "--external-ref") || !strings.Contains(out, "requires") {
		t.Fatalf("code %d: %s", code, out)
	}
}

const handEdited = `# Workspace config
gate = "nix flake check"   # hand set

land_strategy   =   "gitlab" # keep me

[profiles.x]
model = "opus"

[initiatives]
required = true # trailing
external_ref = false

[jira]
poll_interval = "5m"
site = "old.atlassian.net"
`

func TestInitWorkspaceRerunKeepsUnspecifiedAndUnownedBytes(t *testing.T) {
	e, wt := wsEnv(t)
	putCfg(t, wt, handEdited)
	if out, code := e.ws(t, "--external-ref", "true"); code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	want := strings.Replace(handEdited, "external_ref = false", "external_ref = true", 1)
	if got := readCfg(t, wt); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if _, code := e.ws(t, "--jira-site", ""); code != 0 {
		t.Fatal(code)
	}
	if got := readCfg(t, wt); strings.Contains(got, "old.atlassian.net") || !strings.Contains(got, `poll_interval = "5m"`) {
		t.Fatalf("site not cleared cleanly:\n%s", got)
	}
}

func TestInitWorkspaceRerunDroppingRequiredResetsExternalRef(t *testing.T) {
	e, wt := wsEnv(t)
	putCfg(t, wt, "land_strategy = \"local\"\n[initiatives]\nrequired = true\nexternal_ref = true\n")
	if out, code := e.ws(t, "--require-initiatives", "false", "--jira-site", ""); code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	if got := readCfg(t, wt); !strings.Contains(got, "required = false") || !strings.Contains(got, "external_ref = false") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestInitWorkspaceRefusesEditByHandShape(t *testing.T) {
	e, wt := wsEnv(t)
	src := "initiatives = { required = true }\n"
	putCfg(t, wt, src)
	out, code := e.ws(t, allFlags...)
	if code == 0 || !strings.Contains(out, "edit by hand") {
		t.Fatalf("code %d: %s", code, out)
	}
	if got := readCfg(t, wt); got != src {
		t.Fatalf("file changed: %s", got)
	}
}

func TestInitWorkspaceUsageListsVerb(t *testing.T) {
	for _, want := range []string{
		"mugicrew init workspace [--land-strategy local|gitlab]",
		"[--require-initiatives <bool>]",
		"[--external-ref <bool>]",
		"[--jira-site <host>]",
		"[--context name=[repo:|path:]dir]...",
	} {
		if !strings.Contains(usage, want) {
			t.Fatalf("usage lacks %q", want)
		}
	}
}

func (e *initEnv) wsTTY(t *testing.T, in string, args ...string) (string, int) {
	e.tty = true
	return e.run(t, in, append([]string{"workspace"}, args...)...)
}

func TestInitWorkspaceTerminalBlankAnswersWriteDefaults(t *testing.T) {
	e, wt := wsEnv(t)
	out, code := e.wsTTY(t, "\n\n\n")
	if code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	got := readCfg(t, wt)
	for _, want := range []string{`land_strategy = "local"`, "required = false", "external_ref = false"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(out, "Enforce external ID") {
		t.Fatalf("external ID asked with required no:\n%s", out)
	}
}

func TestInitWorkspaceTerminalBadAnswersReprompt(t *testing.T) {
	e, wt := wsEnv(t)
	out, code := e.wsTTY(t, "svn\ngitlab\nyes\nyes\nhttps://acme.atlassian.net\nacme.atlassian.net\n")
	if code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	if !strings.Contains(out, "Enforce external ID") {
		t.Fatalf("external ID not asked with required yes:\n%s", out)
	}
	got := readCfg(t, wt)
	for _, want := range []string{`land_strategy = "gitlab"`, "required = true", "external_ref = true", `site = "acme.atlassian.net"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Count(out, "land strategy") < 2 || strings.Count(out, "Jira site") < 2 {
		t.Fatalf("bad answers did not re-ask:\n%s", out)
	}
}

func TestInitWorkspaceTerminalRerunEnterThroughIsByteIdentical(t *testing.T) {
	e, wt := wsEnv(t)
	if out, code := e.wsTTY(t, "gitlab\nyes\nno\nacme.atlassian.net\n"); code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	before := readCfg(t, wt)
	out, code := e.wsTTY(t, "\n\n\n\n\n")
	if code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	if got := readCfg(t, wt); got != before {
		t.Fatalf("Enter-through changed the file:\n%s\n--- was ---\n%s", got, before)
	}
	for _, want := range []string{"[gitlab]", "[yes]", "[no]", "[acme.atlassian.net]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("current value %s not the default:\n%s", want, out)
		}
	}
}

func TestInitWorkspaceTerminalFlagSkipsItsQuestion(t *testing.T) {
	e, wt := wsEnv(t)
	out, code := e.wsTTY(t, "\n\n", "--land-strategy", "gitlab", "--jira-site", "")
	if code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	if strings.Contains(out, "land strategy") || strings.Contains(out, "Jira site") {
		t.Fatalf("flagged questions were asked:\n%s", out)
	}
	if !strings.Contains(readCfg(t, wt), `land_strategy = "gitlab"`) {
		t.Fatal("flag value not written")
	}
}

func TestInitWorkspaceTerminalInputEndingOnBadAnswerFails(t *testing.T) {
	e, _ := wsEnv(t)
	if out, code := e.wsTTY(t, "svn"); code == 0 {
		t.Fatalf("expected failure, got:\n%s", out)
	}
}

// ctxEnv is wsEnv plus a path dir "pkg" and a nested ignored repo "child".
func ctxEnv(t *testing.T) (*initEnv, string) {
	t.Helper()
	e, wt := wsEnv(t)
	os.MkdirAll(filepath.Join(wt, "pkg"), 0o755)
	os.MkdirAll(filepath.Join(wt, "other"), 0o755)
	os.WriteFile(filepath.Join(wt, ".gitignore"), []byte("child/\nloose/\n"), 0o644)
	os.MkdirAll(filepath.Join(wt, "loose"), 0o755)
	for _, d := range []string{"child", "extra"} {
		os.MkdirAll(filepath.Join(wt, d), 0o755)
		if out, err := exec.Command("git", "-C", filepath.Join(wt, d), "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v %s", err, out)
		}
	}
	return e, wt
}

func ctxFlags(specs ...string) []string {
	args := slices.Clone(allFlags)
	for _, s := range specs {
		args = append(args, "--context", s)
	}
	return args
}

func TestInitWorkspaceContextFormsWritePathAndRepo(t *testing.T) {
	e, wt := ctxEnv(t)
	if out, code := e.ws(t, ctxFlags("a=pkg", "b=repo:child", "c=path:other")...); code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	got := readCfg(t, wt)
	for _, want := range []string{"[context.a]\npath = \"pkg\"", "[context.b]\nrepo = \"child\"", "[context.c]\npath = \"other\""} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestInitWorkspaceContextPathForcedWhenDirStartsWithRepoPrefix(t *testing.T) {
	e, wt := ctxEnv(t)
	os.MkdirAll(filepath.Join(wt, "repo:x"), 0o755)
	if out, code := e.ws(t, ctxFlags("a=path:repo:x")...); code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	if got := readCfg(t, wt); !strings.Contains(got, "[context.a]\npath = \"repo:x\"") {
		t.Fatalf("got:\n%s", got)
	}
}

const withContexts = `land_strategy = "local"
[initiatives]
required = false
external_ref = false

[context.keep]
path = "pkg"
gate = "checks.keep"

[context.drop]
path = "other"
gate = "checks.drop"
`

func TestInitWorkspaceContextFlagsReplaceSetKeepGateAndDropTable(t *testing.T) {
	e, wt := ctxEnv(t)
	putCfg(t, wt, withContexts)
	if out, code := e.ws(t, ctxFlags("keep=pkg", "new=repo:child")...); code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	want := strings.Replace(withContexts, "\n[context.drop]\npath = \"other\"\ngate = \"checks.drop\"\n", "", 1) +
		"\n[context.new]\nrepo = \"child\"\n"
	if got := readCfg(t, wt); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestInitWorkspaceNoContextFlagKeepsDeclaredSet(t *testing.T) {
	e, wt := ctxEnv(t)
	putCfg(t, wt, withContexts)
	if out, code := e.ws(t, allFlags...); code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	if got := readCfg(t, wt); got != withContexts {
		t.Fatalf("got:\n%s", got)
	}
}

func TestInitWorkspaceContextFormatRulesRefuseNamingFlag(t *testing.T) {
	for _, c := range []struct{ spec, rule string }{
		{"Bad_Name=pkg", "name"},
		{"noequals", "name=dir"},
		{"a=/abs/pkg", "relative"},
		{"a=../pkg", "inside"},
		{"a=pkg/../pkg", "cleaned"},
		{"a=.", "root"},
		{"a=missing", "exist"},
		{"a=repo:pkg", "git repo root"},
		{"a=repo:extra", "ignored"},
		{"a=pkg", ""}, // paired below with a duplicate
	} {
		if c.rule == "" {
			continue
		}
		e, wt := ctxEnv(t)
		out, code := e.ws(t, ctxFlags(c.spec)...)
		if code == 0 || !strings.Contains(out, "--context") || !strings.Contains(out, c.spec) || !strings.Contains(out, c.rule) {
			t.Fatalf("%s: code %d: %s", c.spec, code, out)
		}
		if _, err := os.Stat(cfgPath(wt)); err == nil {
			t.Fatalf("%s: wrote a file", c.spec)
		}
	}
	for _, specs := range [][]string{{"a=pkg", "a=other"}, {"a=pkg", "b=pkg"}} {
		e, _ := ctxEnv(t)
		out, code := e.ws(t, ctxFlags(specs...)...)
		if code == 0 || !strings.Contains(out, "--context") || !strings.Contains(out, "twice") {
			t.Fatalf("%v: code %d: %s", specs, code, out)
		}
	}
}

func TestInitWorkspaceLocalLayerShadowWarnsAndStillWrites(t *testing.T) {
	e, wt := ctxEnv(t)
	local := cfglayer.LocalPath(wt)
	os.MkdirAll(filepath.Dir(local), 0o755)
	os.WriteFile(local, []byte("[context.x]\npath = \"pkg\"\n"), 0o644)
	out, code := e.ws(t, ctxFlags("a=pkg")...)
	if code != 0 || !strings.Contains(out, "config.local.toml") {
		t.Fatalf("code %d: %s", code, out)
	}
	if !strings.Contains(readCfg(t, wt), "[context.a]") {
		t.Fatal("shared file not written")
	}
}

func TestInitWorkspaceNoTerminalPrintsNoCandidates(t *testing.T) {
	e, _ := ctxEnv(t)
	out, code := e.ws(t, allFlags...)
	if code != 0 || strings.Contains(out, "Declare which") || strings.Contains(out, "candidate") {
		t.Fatalf("code %d: %s", code, out)
	}
}

// candEnv is a repo with tracked manifests at several depths and nested repos.
func candEnv(t *testing.T) (*initEnv, string) {
	t.Helper()
	e, wt := wsEnv(t)
	for f, body := range map[string]string{
		"svc/go.mod": "m", "svc/inner/go.mod": "m", "web/ui/package.json": "{}", "Cargo.toml": "x", "My_App/flake.nix": "x",
	} {
		os.MkdirAll(filepath.Join(wt, filepath.Dir(f)), 0o755)
		os.WriteFile(filepath.Join(wt, f), []byte(body), 0o644)
	}
	os.WriteFile(filepath.Join(wt, ".gitignore"), []byte("deps/\nlibs/\nloose/\n"), 0o644)
	for _, d := range []string{"deps/a", "libs/x/y/z", "libs/x/y", "unignored", "loose/q"} {
		os.MkdirAll(filepath.Join(wt, d), 0o755)
	}
	for _, d := range []string{"deps/a", "libs/x/y", "unignored"} { // y is depth 3, z/w below would be depth 4
		if out, err := exec.Command("git", "-C", filepath.Join(wt, d), "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v %s", err, out)
		}
	}
	os.MkdirAll(filepath.Join(wt, "libs/x/y/z/w"), 0o755)
	exec.Command("git", "-C", filepath.Join(wt, "libs/x/y/z/w"), "init", "-q").Run()
	if out, err := exec.Command("git", "-C", wt, "add", "svc", "web", "Cargo.toml", "My_App").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	return e, wt
}

func TestDiscoverContextsOutermostManifestsAndRepoDepth(t *testing.T) {
	_, wt := candEnv(t)
	var got []string
	for _, c := range discoverContexts(wt, nil) {
		got = append(got, kindWord(c.Repo)+":"+c.Dir)
	}
	want := []string{"path:My_App", "path:svc", "path:web/ui", "repo:deps/a", "repo:libs/x/y"}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for _, c := range discoverContexts(wt, map[string]bool{"svc": true}) {
		if c.Dir == "svc" {
			t.Fatal("declared dir re-offered")
		}
	}
}

func TestInitWorkspaceTerminalDeclaresChosenCandidates(t *testing.T) {
	e, wt := candEnv(t)
	// scalar answers (land, required, site), then Declare which?: My_App (1), svc (2), deps/a (4)? list is sorted by kind scan: paths first
	out, code := e.wsTTY(t, "\n\n\n1 2 4\n\n")
	if code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	for _, want := range []string{"my-app  My_App  path  (tracked flake.nix)", "a  deps/a  repo  (ignored nested git repo)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	got := readCfg(t, wt)
	for _, want := range []string{"[context.my-app]\npath = \"My_App\"", "[context.svc]\npath = \"svc\"", "[context.a]\nrepo = \"deps/a\""} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestInitWorkspaceTerminalBlankDeclareWhichDeclaresNone(t *testing.T) {
	e, wt := candEnv(t)
	if out, code := e.wsTTY(t, "\n\n\n\n\n"); code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	if strings.Contains(readCfg(t, wt), "[context") {
		t.Fatalf("blank declared something:\n%s", readCfg(t, wt))
	}
}

func TestInitWorkspaceTerminalCollidingDerivedNameAsksForName(t *testing.T) {
	e, wt := candEnv(t)
	os.MkdirAll(filepath.Join(wt, "other/svc"), 0o755)
	os.WriteFile(filepath.Join(wt, "other/svc/go.mod"), []byte("m"), 0o644)
	exec.Command("git", "-C", wt, "add", "other").Run()
	// list: My_App, other/svc, svc, web/ui ...; pick other/svc then svc; second svc collides
	out, code := e.wsTTY(t, "\n\n\n2 3\nBad Name\nsvc2\n\n")
	if code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	if !strings.Contains(out, "Name for svc") || !strings.Contains(out, "matches") {
		t.Fatalf("no name prompt:\n%s", out)
	}
	got := readCfg(t, wt)
	if !strings.Contains(got, "[context.svc]\npath = \"other/svc\"") || !strings.Contains(got, "[context.svc2]\npath = \"svc\"") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestInitWorkspaceTerminalFreeLoopDefaultsKindAndReprompts(t *testing.T) {
	e, wt := ctxEnv(t)
	// name, dir (bad then good), kind default repo for the git root; then a path one with explicit kind; blank ends
	out, code := e.wsTTY(t, "\n\n\n\nBad\nchild-ctx\nnope\nextra\npath\nc2\nchild\n\n\n")
	if code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	got := readCfg(t, wt)
	if !strings.Contains(got, "[context.child-ctx]\npath = \"extra\"") || !strings.Contains(got, "[context.c2]\nrepo = \"child\"") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestInitWorkspaceTerminalRerunKeepAndDrop(t *testing.T) {
	e, wt := ctxEnv(t)
	putCfg(t, wt, withContexts)
	out, code := e.wsTTY(t, "\n\n\nn\n\n\n\n")
	if code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	if !strings.Contains(out, "Keep drop") && !strings.Contains(out, "Keep keep") {
		t.Fatalf("no Keep prompts:\n%s", out)
	}
	got := readCfg(t, wt)
	if !strings.Contains(got, `gate = "checks.keep"`) || strings.Contains(got, "context.drop") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestInitWorkspaceContextRedeclaredWithNewDirReplacesTable(t *testing.T) {
	e, wt := ctxEnv(t)
	putCfg(t, wt, withContexts)
	if out, code := e.ws(t, ctxFlags("keep=extra")...); code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	got := readCfg(t, wt)
	if !strings.Contains(got, "[context.keep]\npath = \"extra\"\n") || strings.Contains(got, "checks.keep") || strings.Contains(got, "context.drop") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestInitWorkspaceContextRedeclaredWithNewKindReplacesTable(t *testing.T) {
	e, wt := ctxEnv(t)
	putCfg(t, wt, withContexts)
	if out, code := e.ws(t, ctxFlags("keep=repo:child")...); code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	got := readCfg(t, wt)
	if !strings.Contains(got, "[context.keep]\nrepo = \"child\"\n") || strings.Contains(got, "checks.keep") || strings.Contains(got, "path = \"pkg\"") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestInitWorkspaceNoTerminalRerunWithoutJiraKeepsNoJira(t *testing.T) {
	e, wt := wsEnv(t)
	const src = "land_strategy = \"local\"\n\n[initiatives]\nrequired = false\nexternal_ref = false\n"
	putCfg(t, wt, src)
	if out, code := e.ws(t, "--land-strategy", "gitlab"); code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	want := strings.Replace(src, `"local"`, `"gitlab"`, 1)
	if got := readCfg(t, wt); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
