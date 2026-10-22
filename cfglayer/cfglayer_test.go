package cfglayer

import (
	"fmt"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"slices"
	"strings"
	"time"

	"jayulabs.org/mugicrew/git"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func workspace(t *testing.T, shared, local string) string {
	t.Helper()
	root := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	for dir, body := range map[string]string{filepath.Join(root, ".mugicrew"): shared, filepath.Join(root, ".git", "mugicrew"): local} {
		if body == "" {
			continue
		}
		os.MkdirAll(dir, 0o755)
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestReposAndContexts(t *testing.T) {
	for _, tc := range []struct {
		name, shared, local string
		repos               map[string]string
		declared            []string
	}{
		{"none", "concurrency = 2\n", "", nil, nil},
		{"subtree only has no repos", "[context.a]\npath = \"apps/a\"\n", "", nil, []string{"a"}},
		{"repo decoded", "[context.a]\npath = \"apps/a\"\n[context.c]\nrepo = \"vendor/c\"\ngate = \"x\"\n", "",
			map[string]string{"c": "vendor/c"}, []string{"a", "c"}},
		{"local wins as a whole table", "[context.c]\nrepo = \"vendor/c\"\n", "[context.d]\nrepo = \"vendor/d\"\n",
			map[string]string{"d": "vendor/d"}, []string{"d"}},
		{"repo and path refused, undeclared", "[context.a]\npath = \"apps/a\"\n[context.c]\nrepo = \"vendor/c\"\npath = \"x\"\n", "",
			nil, []string{"a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := workspace(t, tc.shared, tc.local)
			got := Repos(root)
			if !maps.Equal(got, tc.repos) || (tc.repos == nil) != (got == nil) {
				t.Errorf("Repos = %v, want %v", got, tc.repos)
			}
			var names []string
			for n := range Contexts(root) {
				names = append(names, n)
			}
			if len(names) != len(tc.declared) {
				t.Errorf("Contexts names = %v, want %v", names, tc.declared)
			}
		})
	}
}

func TestContextGateDecoded(t *testing.T) {
	c := Contexts(workspace(t, "[context.c]\nrepo = \"v/c\"\ngate = \"build\"\n", ""))["c"]
	if c.Gate != "build" || c.Repo != "v/c" {
		t.Errorf("entry = %+v", c)
	}
}

// The top-level gate never leaks into a context, and a context's never into the top.
func TestGateTopLevelAndPerContext(t *testing.T) {
	root := workspace(t, "gate = \"top\"\n[context.a]\npath = \"a\"\ngate = \"ga\"\n[context.b]\npath = \"b\"\n", "")
	if g := Gate(root); g != "top" {
		t.Errorf("Gate = %q, want top", g)
	}
	for name, want := range map[string]string{"a": "ga", "b": "", "missing": ""} {
		if g := ContextGate(root, name); g != want {
			t.Errorf("ContextGate(%s) = %q, want %q", name, g, want)
		}
	}
	if g := Gate(workspace(t, "[context.a]\npath = \"a\"\ngate = \"ga\"\n", "")); g != "" {
		t.Errorf("Gate with only a context key = %q, want empty", g)
	}
}

// countGit puts a logging git first on PATH and returns the log's path: one line
// per git call the package under test makes.
func countGit(t *testing.T) string {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	dir, log := t.TempDir(), filepath.Join(t.TempDir(), "calls")
	script := "#!/bin/sh\necho \"$@\" >> " + log + "\nexec " + real + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// A Workspace's config reads cost no git call, from the root, a subdirectory and a
// linked worktree (ADR 0157: no-context Workspaces pay nothing).
func TestReposMakeNoGitCall(t *testing.T) {
	root := t.TempDir()
	for _, a := range [][]string{{"init", "-q", root}, {"-C", root, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "x"}, {"-C", root, "worktree", "add", "-q", filepath.Join(root, "wt")}} {
		if out, err := exec.Command("git", a...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(root, ".git", "mugicrew")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "config.toml"), []byte("[context.c]\nrepo = \"child\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	log := countGit(t)
	for _, dir := range []string{root, filepath.Join(root, "sub"), filepath.Join(root, "wt")} {
		if got := Repos(dir); got["c"] != "child" {
			t.Fatalf("Repos(%s) = %v; want c=child", dir, got)
		}
	}
	if b, _ := os.ReadFile(log); len(b) != 0 {
		t.Fatalf("Repos made git calls:\n%s", b)
	}
}

func templates(rules []git.Rule) []string {
	var out []string
	for _, r := range rules {
		out = append(out, r.Template)
	}
	return out
}

func TestSequenceRules(t *testing.T) {
	const flyway = "db/migration/V{n}__*.sql"
	adr := git.ADRRule.Template
	for _, tc := range []struct {
		name, shared, local string
		want                []string
		errHas              string
	}{
		{"missing file is built-in only", "", "", []string{adr}, ""},
		{"top level", "sequences = [\"" + flyway + "\"]\n", "", []string{adr, flyway}, ""},
		{"local layer ignored", "", "sequences = [\"" + flyway + "\"]\n", []string{adr}, ""},
		{"unparseable file", "sequences = [", "", nil, "config.toml"},
		{"no {n}", "sequences = [\"db/*.sql\"]\n", "", nil, `"db/*.sql"`},
		{"two {n}", "sequences = [\"{n}_{n}.sql\"]\n", "", nil, "config.toml"},
		{"n outside file name", "sequences = [\"v{n}/x.sql\"]\n", "", nil, "file name"},
		{"bad width", "sequences = [\"{n:x}.sql\"]\n", "", nil, "config.toml"},
		{"table in a rule's place", "sequences = [{ glob = \"x\" }]\n", "", nil, "not a template"},
		{"sequences in a path context", "[context.a]\npath = \"apps/a\"\nsequences = [\"" + flyway + "\"]\n", "", nil, "context.a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rules, err := SequenceRules(workspace(t, tc.shared, tc.local), "")
			if tc.errHas != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errHas) || !strings.Contains(err.Error(), "config.toml") {
					t.Fatalf("err = %v, want one naming %q and the file", err, tc.errHas)
				}
				return
			}
			if err != nil || !slices.Equal(templates(rules), tc.want) {
				t.Errorf("rules = %v, %v; want %v", templates(rules), err, tc.want)
			}
		})
	}
}

// A child repo takes its own [context.<name>] sequences, never the top level's; the
// Workspace repo takes the top level's, never a child's.
func TestSequenceRulesSelectByRepo(t *testing.T) {
	root := workspace(t, "sequences = [\"top/{n}.sql\"]\n[context.c]\nrepo = \"vendor/c\"\nsequences = [\"c/{n}.sql\"]\n", "")
	child := filepath.Join(root, "vendor", "c")
	os.MkdirAll(child, 0o755)
	if out, err := exec.Command("git", "init", "-q", child).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	adr := git.ADRRule.Template
	for _, tc := range []struct {
		repo git.Repo
		want []string
	}{
		{"", []string{adr, "top/{n}.sql"}},
		{git.Repo(root), []string{adr, "top/{n}.sql"}},
		{git.Repo(child), []string{adr, "c/{n}.sql"}},
	} {
		rules, err := SequenceRules(root, tc.repo)
		if err != nil || !slices.Equal(templates(rules), tc.want) {
			t.Errorf("repo %q: rules = %v, %v; want %v", tc.repo, templates(rules), err, tc.want)
		}
	}
}

func TestJiraSiteLocalTableWins(t *testing.T) {
	for _, tc := range []struct{ name, shared, local, want, src string }{
		{"none", "gate = \"x\"\n", "", "", "shared"},
		{"shared", "[jira]\nsite = \"a.atlassian.net\"\n", "", "a.atlassian.net", "shared"},
		{"local overrides shared", "[jira]\nsite = \"a.atlassian.net\"\n", "[jira]\nsite = \"b.atlassian.net\"\n", "b.atlassian.net", "local"},
		{"local table without site masks shared", "[jira]\nsite = \"a.atlassian.net\"\n", "[jira]\n", "", "local"},
		{"local without a table falls through", "[jira]\nsite = \"a.atlassian.net\"\n", "gate = \"x\"\n", "a.atlassian.net", "shared"},
	} {
		root := workspace(t, tc.shared, tc.local)
		got, path := JiraSite(root)
		wantPath := SharedPath(root)
		if tc.src == "local" {
			wantPath = LocalPath(root)
		}
		if got != tc.want || path != wantPath {
			t.Errorf("%s: JiraSite = %q, %q; want %q, %q", tc.name, got, path, tc.want, wantPath)
		}
	}
}

func TestInitiativesSetting(t *testing.T) {
	for _, tc := range []struct {
		name, shared, local string
		want                Initiatives
	}{
		{"absent", "gate = \"x\"\n", "", Initiatives{}},
		{"required only", "[initiatives]\nrequired = true\n", "", Initiatives{Required: true}},
		{"both on", "[initiatives]\nrequired = true\nexternal_ref = true\n", "", Initiatives{Required: true, ExternalRef: true}},
		{"external_ref without required", "[initiatives]\nexternal_ref = true\n", "", Initiatives{Unusable: true}},
		{"mistyped", "[initiatives]\nrequired = \"yes\"\n", "", Initiatives{Unusable: true}},
		{"local layer ignored", "", "[initiatives]\nrequired = true\n", Initiatives{}},
	} {
		root := workspace(t, tc.shared, tc.local)
		tc.want.Path = SharedPath(root)
		if got := InitiativesSetting(root); got != tc.want {
			t.Errorf("%s: InitiativesSetting = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestAutoApproveSetting(t *testing.T) {
	for _, tc := range []struct {
		name, shared, local string
		want                Approvals
	}{
		{"absent", "gate = \"x\"\n", "", Approvals{}},
		{"empty table", "[approvals]\n", "", Approvals{}},
		{"on", "[approvals]\nauto_approve = true\n", "", Approvals{AutoApprove: true}},
		{"off", "[approvals]\nauto_approve = false\n", "", Approvals{}},
		{"decode error", "[approvals\nauto_approve = true\n", "", Approvals{Unusable: true}},
		{"mistyped value", "[approvals]\nauto_approve = \"yes\"\n", "", Approvals{Unusable: true}},
		{"mistyped table", "approvals = true\n", "", Approvals{Unusable: true}},
		{"local layer ignored", "", "[approvals]\nauto_approve = true\n", Approvals{}},
	} {
		root := workspace(t, tc.shared, tc.local)
		tc.want.Path = SharedPath(root)
		if got := AutoApproveSetting(root); got != tc.want {
			t.Errorf("%s: AutoApproveSetting = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestJiraPollInterval(t *testing.T) {
	for _, tc := range []struct {
		name, shared, local string
		want                time.Duration
	}{
		{"unset", "[jira]\nsite = \"a.atlassian.net\"\n", "", DefaultJiraPollInterval},
		{"no table", "gate = \"x\"\n", "", DefaultJiraPollInterval},
		{"parses", "[jira]\npoll_interval = \"5m\"\n", "", 5 * time.Minute},
		{"under minimum", "[jira]\npoll_interval = \"30s\"\n", "", DefaultJiraPollInterval},
		{"unparseable", "[jira]\npoll_interval = \"soon\"\n", "", DefaultJiraPollInterval},
		{"local beats shared", "[jira]\npoll_interval = \"5m\"\n", "[jira]\npoll_interval = \"2m\"\n", 2 * time.Minute},
		{"local table without key masks shared", "[jira]\npoll_interval = \"5m\"\n", "[jira]\n", DefaultJiraPollInterval},
	} {
		if got := JiraPollInterval(workspace(t, tc.shared, tc.local)); got != tc.want {
			t.Errorf("%s: JiraPollInterval = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestGitlabPollInterval(t *testing.T) {
	for _, tc := range []struct {
		name, shared, local string
		want                time.Duration
	}{
		{"unset", "[gitlab]\nsite = \"a.atlassian.net\"\n", "", DefaultJiraPollInterval},
		{"no table", "gate = \"x\"\n", "", DefaultJiraPollInterval},
		{"parses", "[gitlab]\npoll_interval = \"5m\"\n", "", 5 * time.Minute},
		{"under minimum", "[gitlab]\npoll_interval = \"30s\"\n", "", DefaultJiraPollInterval},
		{"unparseable", "[gitlab]\npoll_interval = \"soon\"\n", "", DefaultJiraPollInterval},
		{"local beats shared", "[gitlab]\npoll_interval = \"5m\"\n", "[gitlab]\npoll_interval = \"2m\"\n", 2 * time.Minute},
		{"local table without key masks shared", "[gitlab]\npoll_interval = \"5m\"\n", "[gitlab]\n", DefaultJiraPollInterval},
	} {
		if got := GitlabPollInterval(workspace(t, tc.shared, tc.local)); got != tc.want {
			t.Errorf("%s: GitlabPollInterval = %v, want %v", tc.name, got, tc.want)
		}
	}
}

const pTable = "[profiles.p]\nharness = \"claudecode\"\nmodel = \"%s\"\n"

func writeAt(t *testing.T, path, body string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Profiles merges four files first-wins: mugicrew local, OE local, mugicrew
// shared, OE shared; the winning table replaces the lower ones whole.
func TestProfilesPrecedence(t *testing.T) {
	t.Setenv("OE_ROOT", "")
	files := []string{".git/mugicrew/config.toml", ".git/agent-profiles.toml", ".mugicrew/config.toml", "agent-profiles.toml"}
	for n := range files {
		root := workspace(t, "", "")
		for i := n; i < len(files); i++ {
			writeAt(t, filepath.Join(root, files[i]), fmt.Sprintf(pTable, files[i]))
		}
		e := Profiles(root).Winners["p"]
		if e.Profile.Model != files[n] || e.Path != filepath.Join(root, files[n]) || e.Err != nil {
			t.Errorf("with %v winner = %+v, want %s", files[n:], e, files[n])
		}
	}
}

func TestProfilesDefects(t *testing.T) {
	t.Setenv("OE_ROOT", "")
	root := workspace(t, "[profiles.p]\nharness = \"claudecode\"\n[profiles.q]\nharness = \"claudecode\"\nmodle = \"x\"\n", "[profiles.p]\nmodel = \"x\"\n")
	writeAt(t, filepath.Join(root, "agent-profiles.toml"), "[profiles.q]\nbogus = 1\n")
	evs := eventlogtest.Capture(t)
	c := Profiles(root)
	if e := c.Winners["p"]; e.Err == nil || e.Path != LocalPath(root) || e.Profile.Name != "p" {
		t.Errorf("p = %+v, want local's bad table shadowing the good shared one", e)
	}
	if e := c.Winners["q"]; e.Err == nil || e.Path != SharedPath(root) {
		t.Errorf("q = %+v, want the shared bad table", e)
	}
	var got []string
	for _, d := range c.Unusable {
		got = append(got, filepath.Base(d.Path)+":"+d.Name)
	}
	if want := []string{"config.toml:p", "config.toml:q", "agent-profiles.toml:"}; !slices.Equal(got, want) {
		t.Errorf("Unusable = %v, want %v", got, want)
	}
	if names := eventlogtest.Names(*evs); !slices.Equal(names, []string{"config.unusable", "config.unusable", "agent-profiles.unusable"}) {
		t.Fatalf("events = %v", names)
	}
	if a := (*evs)[0].Attrs; a["path"] != LocalPath(root) || a["key"] != "profiles.p" || a["reason"] != "no harness" {
		t.Errorf("first attrs = %v", a)
	}
	if a := (*evs)[2].Attrs; a["file"] != filepath.Join(root, "agent-profiles.toml") || a["reason"] == "" {
		t.Errorf("dropped layer attrs = %v", a)
	}
}

func TestProfilesNonTableAndMalformed(t *testing.T) {
	t.Setenv("OE_ROOT", "")
	root := workspace(t, "profiles = 3\n", "[profiles.p\n")
	writeAt(t, filepath.Join(root, "agent-profiles.toml"), "[profiles.o]\nharness = \"claudecode\"\n")
	evs := eventlogtest.Capture(t)
	c := Profiles(root)
	if len(c.Winners) != 1 || c.Winners["o"].Path != filepath.Join(root, "agent-profiles.toml") {
		t.Errorf("Winners = %+v, want only the OE one", c.Winners)
	}
	if len(c.Unusable) != 1 || c.Unusable[0].Name != "" || c.Unusable[0].Path != SharedPath(root) {
		t.Errorf("Unusable = %+v, want the non-table profiles file", c.Unusable)
	}
	if a := (*evs)[0].Attrs; (*evs)[0].Name != "config.unusable" || a["path"] != LocalPath(root) || a["err"] == nil {
		t.Errorf("malformed event = %+v", (*evs)[0])
	}
	if a := (*evs)[1].Attrs; a["path"] != SharedPath(root) || a["key"] != "profiles" {
		t.Errorf("non-table event = %+v", (*evs)[1])
	}
}

// From a linked worktree the local layers resolve to the main repo's common dir.
func TestProfilesFromLinkedWorktree(t *testing.T) {
	t.Setenv("OE_ROOT", "")
	root := t.TempDir()
	for _, a := range [][]string{{"init", "-q", root}, {"-C", root, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "x"}, {"-C", root, "worktree", "add", "-q", filepath.Join(root, "wt")}} {
		if out, err := exec.Command("git", a...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", a, err, out)
		}
	}
	writeAt(t, filepath.Join(root, ".git", "mugicrew", "config.toml"), fmt.Sprintf(pTable, "m"))
	writeAt(t, filepath.Join(root, ".git", "agent-profiles.toml"), "[profiles.oel]\nharness = \"claudecode\"\n")
	c := Profiles(filepath.Join(root, "wt"))
	if c.Winners["p"].Path != filepath.Join(root, ".git", "mugicrew", "config.toml") || c.Winners["oel"].Path != filepath.Join(root, ".git", "agent-profiles.toml") {
		t.Errorf("Winners = %+v", c.Winners)
	}
}

func TestLandingPollInterval(t *testing.T) {
	for _, tc := range []struct {
		name, shared, local string
		want                time.Duration
	}{
		{"unset", "[gitlab]\npoll_interval = \"5m\"\n", "", DefaultLandingPollInterval},
		{"no table", "gate = \"x\"\n", "", DefaultLandingPollInterval},
		{"parses", "[gitlab]\nlanding_poll_interval = \"2m\"\n", "", 2 * time.Minute},
		{"at minimum", "[gitlab]\nlanding_poll_interval = \"15s\"\n", "", MinLandingPollInterval},
		{"under minimum", "[gitlab]\nlanding_poll_interval = \"10s\"\n", "", DefaultLandingPollInterval},
		{"unparseable", "[gitlab]\nlanding_poll_interval = \"soon\"\n", "", DefaultLandingPollInterval},
		{"local beats shared", "[gitlab]\nlanding_poll_interval = \"5m\"\n", "[gitlab]\nlanding_poll_interval = \"30s\"\n", 30 * time.Second},
		{"local table without key masks shared", "[gitlab]\nlanding_poll_interval = \"5m\"\n", "[gitlab]\n", DefaultLandingPollInterval},
	} {
		if got := LandingPollInterval(workspace(t, tc.shared, tc.local)); got != tc.want {
			t.Errorf("%s: LandingPollInterval = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestLandingPollIntervalWarnsUnusable(t *testing.T) {
	for _, body := range []string{"landing_poll_interval = \"soon\"", "landing_poll_interval = \"10s\""} {
		evs := eventlogtest.Capture(t)
		if got := LandingPollInterval(workspace(t, "[gitlab]\n"+body+"\n", "")); got != DefaultLandingPollInterval {
			t.Errorf("%s: got %v, want default", body, got)
		}
		if len(*evs) != 1 || (*evs)[0].Name != "config.unusable" || (*evs)[0].Attrs["key"] != "gitlab.landing_poll_interval" {
			t.Errorf("%s: events = %v, want one config.unusable key=gitlab.landing_poll_interval", body, *evs)
		}
	}
}
