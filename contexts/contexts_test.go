package contexts

import (
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts/contextstest"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

func repo(t *testing.T, shared, local string) string {
	t.Helper()
	root := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	write := func(dir, body string) {
		if body == "" {
			return
		}
		os.MkdirAll(dir, 0o755)
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, ".mugicrew"), shared)
	write(filepath.Join(root, ".git", "mugicrew"), local)
	return root
}

func TestContexts(t *testing.T) {
	for _, tc := range []struct {
		name, shared, local string
		want                map[string]string
	}{
		{"no file", "", "", nil},
		{"unusable file", "[context.a\n", "", nil},
		{"no table", "concurrency = 2\n", "", nil},
		{"shared only", "concurrency = 2\n[context.a]\npath = \"apps/a\"\n[context.b]\npath = \"libs/b\"\nfuture = 1\n", "",
			map[string]string{"a": "apps/a", "b": "libs/b"}},
		{"local wins as a whole table", "[context.a]\npath = \"apps/a\"\n[context.b]\npath = \"libs/b\"\n", "[context.c]\npath = \"x\"\n",
			map[string]string{"c": "x"}},
		{"unusable local falls to shared", "[context.a]\npath = \"apps/a\"\n", "[context.c\n", map[string]string{"a": "apps/a"}},
		{"local without table falls to shared", "[context.a]\npath = \"apps/a\"\n", "concurrency = 4\n", map[string]string{"a": "apps/a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Contexts(repo(t, tc.shared, tc.local))
			if !maps.Equal(got, tc.want) || (tc.want == nil) != (got == nil) {
				t.Errorf("Contexts = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFocusLine(t *testing.T) {
	cs := map[string]string{"mugicrew": "apps/mugicrew"}
	if got := FocusLine(cs, bd.Bead{Labels: []string{"kind:chart"}}); got != "" {
		t.Errorf("unlabelled = %q", got)
	}
	got := FocusLine(cs, bd.Bead{Labels: []string{"context:mugicrew"}})
	for _, w := range []string{"mugicrew", "apps/mugicrew/", "apps/mugicrew/GLOSSARY.md", "apps/mugicrew/docs/adr/", "apps/mugicrew/.mugicrew/CREW.md", "after the Workspace", "repo root"} {
		if !strings.Contains(got, w) {
			t.Errorf("focus %q lacks %q", got, w)
		}
	}
}

func TestOutOfScope(t *testing.T) {
	in := []string{
		"apps/a/x.go", "apps/a", "apps/ab/y.go", "libs/l/z.go",
		"GLOSSARY.md", "apps/b/GLOSSARY-MAP.md", "docs/adr/0001-x.md", "apps/b/docs/adr/0002-y.md",
		".mugicrew/config.toml", "pnpm-lock.yaml", "flake.nix", "other/.mugicrew/config.toml",
		".mugicrew/CREW.md", "apps/lineage/.mugicrew/CREW.md",
	}
	want := []string{"apps/ab/y.go", "libs/l/z.go", "pnpm-lock.yaml", "flake.nix", "other/.mugicrew/config.toml", "apps/lineage/.mugicrew/CREW.md"}
	if got := OutOfScope(in, "apps/a/"); !slices.Equal(got, want) {
		t.Errorf("OutOfScope = %v, want %v", got, want)
	}
}

func TestFocusHelpersThreeWayAndGate(t *testing.T) {
	labelled := bd.Bead{ID: "c", Labels: []string{"context:a"}}
	show := func(id string) (bd.Bead, error) {
		if id == "leaf" {
			return bd.Bead{ID: id, Parent: "c"}, nil
		}
		return labelled, nil
	}
	const prompt = "base"
	t.Chdir(repo(t, "", ""))
	boom := func(string) (bd.Bead, error) { t.Fatal("show called with no contexts"); return bd.Bead{}, nil }
	if got := FocusUnder(prompt, "leaf", boom); got != prompt {
		t.Errorf("no table = %q", got)
	}
	t.Chdir(repo(t, "[context.a]\npath = \"apps/a\"\n", ""))
	if got := FocusUnder(prompt, "leaf", func(id string) (bd.Bead, error) {
		if id == "leaf" {
			return bd.Bead{ID: id, Parent: "u"}, nil
		}
		return bd.Bead{ID: id}, nil
	}); got != prompt {
		t.Errorf("unlabelled container = %q", got)
	}
	want := prompt + "\n\n" + FocusLine(map[string]string{"a": "apps/a"}, labelled)
	if got := FocusUnder(prompt, "leaf", show); got != want {
		t.Errorf("FocusUnder = %q, want %q", got, want)
	}
	if got := FocusByID(prompt, "c", show); got != want {
		t.Errorf("FocusByID = %q, want %q", got, want)
	}
}

const repoCfg = "[context.c]\nrepo = \"vendor/c\"\n"

// nestedWorkspace is a Workspace declaring c = repo vendor/c, with a child repo there
// whose origin/HEAD names trunk "tr"; the cwd is the Workspace.
func nestedWorkspace(t *testing.T, cfg string) {
	t.Helper()
	root := repo(t, cfg, "")
	child := filepath.Join(root, "vendor", "c")
	os.MkdirAll(child, 0o755)
	for _, args := range [][]string{
		{"init", "-q", "-b", "tr"}, {"-c", "user.email=a@b", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "r"},
		{"update-ref", "refs/remotes/origin/tr", "HEAD"}, {"symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/tr"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = child
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	t.Chdir(root)
}

func TestRepoContextMapsToItsDirAndPromptCarriesTheAddendum(t *testing.T) {
	nestedWorkspace(t, repoCfg)
	if got := Contexts("."); !maps.Equal(got, map[string]string{"c": "vendor/c"}) {
		t.Fatalf("Contexts = %v", got)
	}
	labelled := bd.Bead{ID: "k", Labels: []string{"context:c"}}
	if RepoOf(labelled) == "" || RepoOf(bd.Bead{ID: "k", Labels: []string{"context:nope"}}) != "" {
		t.Errorf("RepoOf labelled = %q", RepoOf(labelled))
	}
	got := Focus("base", labelled)
	for _, w := range []string{`"c" context (vendor/c/)`, "own git repo", "git -C vendor/c", "trunk is tr.", "vendor/c/.mugicrew/CREW.md"} {
		if !strings.Contains(got, w) {
			t.Errorf("prompt %q lacks %q", got, w)
		}
	}
}

func TestSubtreeOnlyPromptHasNoRepoAddendum(t *testing.T) {
	t.Chdir(repo(t, "[context.a]\npath = \"apps/a\"\n", ""))
	b := bd.Bead{Labels: []string{"context:a"}}
	if got, want := Focus("base", b), "base\n\n"+FocusLine(map[string]string{"a": "apps/a"}, b); got != want {
		t.Errorf("subtree prompt = %q, want %q", got, want)
	}
}

func TestRepoOfReadsNoConfigForAnUnlabelledBead(t *testing.T) {
	old := repos
	t.Cleanup(func() { repos = old })
	repos = func(string) map[string]string { t.Fatal("config read for an unlabelled bead"); return nil }
	if RepoOf(bd.Bead{Labels: []string{"kind:chart"}}) != "" {
		t.Error("unlabelled bead resolved a repo")
	}
}

func TestStampCutStampsBeforeTheCutAndRetries(t *testing.T) {
	oR, oS := repos, addStamp
	t.Cleanup(func() { repos, addStamp = oR, oS })
	root := repo(t, "", "")
	os.MkdirAll(filepath.Join(root, "vendor/x/.git"), 0o755)
	t.Chdir(root)
	repos = func(string) map[string]string { return map[string]string{"x": "vendor/x"} }
	child := git.Repo(root)
	if out, err := exec.Command("git", "-C", root, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "i").CombinedOutput(); err != nil {
		t.Fatalf("commit: %v: %s", err, out)
	}
	var got []string
	fail := true
	addStamp = func(id, dir string) error {
		if fail {
			return errors.New("bd down")
		}
		got = append(got, id+" "+dir)
		return nil
	}
	oB := ShowBead
	t.Cleanup(func() { ShowBead = oB })
	ShowBead = func(string) (bd.Bead, error) { return bd.Bead{}, nil }
	src := bd.Bead{Labels: []string{"context:x"}}
	if err := StampCut(src, "c1", child, "chart/c1"); err == nil {
		t.Fatal("a failed stamp write must surface")
	}
	fail = false // the re-run: the branch was never cut, so it stamps again
	if err := StampCut(src, "c1", child, "chart/c1"); err != nil || len(got) != 1 || got[0] != "c1 vendor/x" {
		t.Fatalf("re-run stamped %v, %v; want c1 vendor/x", got, err)
	}
	got = nil
	ShowBead = func(string) (bd.Bead, error) { return bd.Bead{Labels: []string{"repo:vendor/x"}}, nil }
	if err := StampCut(src, "c1", child, "chart/c1"); err != nil || len(got) != 0 {
		t.Errorf("stamped %v on an already-stamped target: %v", got, err)
	}
	if _, err := child.CutBranch("chart/c1", "HEAD"); err != nil {
		t.Fatal(err)
	}
	for _, s := range []bd.Bead{{Labels: []string{"context:x", "repo:old"}}, {}, {Labels: []string{"context:y"}}} {
		if err := StampCut(s, "c1", child, "chart/c2"); err != nil {
			t.Fatal(err)
		}
	}
	if err := StampCut(src, "c1", child, "chart/c1"); err != nil || len(got) != 0 {
		t.Errorf("stamped %v on an existing branch (would record a drifted dir): %v", got, err)
	}
}

func TestDrift(t *testing.T) {
	live := map[string]string{"x": "vendor/y/"}
	for _, c := range []struct {
		labels  []string
		drifted bool
	}{
		{[]string{"context:x", "repo:vendor/y"}, false},
		{[]string{"context:x", "repo:vendor/x"}, true},
		{[]string{"context:x"}, false},
		{[]string{"repo:vendor/x"}, false},
		{[]string{"context:z", "repo:vendor/x"}, true},
	} {
		if _, _, d := Drift(bd.Bead{Labels: c.labels}, live); d != c.drifted {
			t.Errorf("Drift(%v) = %v, want %v", c.labels, d, c.drifted)
		}
	}
}

func TestPauseNoteNamesBothDirs(t *testing.T) {
	old := repos
	t.Cleanup(func() { repos = old })
	root := repo(t, "", "")
	os.MkdirAll(filepath.Join(root, "vendor/new/.git"), 0o755)
	t.Chdir(root)
	repos = func(string) map[string]string { return map[string]string{"x": "vendor/new"} }
	got := PauseNote(bd.Bead{Labels: []string{"context:x", "repo:vendor/old"}}, bd.PausedContext)
	if !strings.Contains(got, "vendor/old") || !strings.Contains(got, "vendor/new") {
		t.Errorf("PauseNote = %q, want both dirs", got)
	}
}

// An unstamped container labelled with a repo = context reads as cut-before-labelled,
// not undeclared; an unlabelled or path = one keeps the generic note.
func TestPauseNoteNamesRelabelledUnstamped(t *testing.T) {
	old := repos
	t.Cleanup(func() { repos = old })
	root := repo(t, "", "")
	os.MkdirAll(filepath.Join(root, "vendor/x/.git"), 0o755)
	t.Chdir(root)
	repos = func(string) map[string]string { return map[string]string{"x": "vendor/x"} }
	got := PauseNote(bd.Bead{ID: "cp1", Kind: contract.KindCheckpoint, Labels: []string{"context:x"}}, bd.PausedContext)
	for _, w := range []string{"cut in the Workspace repo before it was labelled context:x", "remove the label", "checkpoint/cp1", "vendor/x", "repo:vendor/x"} {
		if !strings.Contains(got, w) {
			t.Errorf("PauseNote = %q, missing %q", got, w)
		}
	}
	if got := PauseNote(bd.Bead{ID: "cp1", Kind: contract.KindCheckpoint, Labels: []string{"context:y"}}, bd.PausedContext); strings.Contains(got, "Workspace repo") {
		t.Errorf("undeclared context got the relabel note: %q", got)
	}
}

// A subtree-only Workspace resolves no Workspace root (git worktree list) for a
// context-labelled bead's repo, nor for the live repo table (Spec FR 4, ADR 0157).
func TestSubtreeOnlyContextsResolveNoRoot(t *testing.T) {
	t.Chdir(repo(t, "[context.a]\npath = \"apps/a\"\n", ""))
	old := git.RepoRoot
	t.Cleanup(func() { git.RepoRoot = old })
	git.RepoRoot = func() (string, error) { t.Error("RepoRoot called"); return "", errors.New("x") }
	if RepoOf(bd.Bead{Labels: []string{"context:a"}}) != "" || Repos() != nil {
		t.Error("subtree context resolved a repo")
	}
}

// A leaf carries no context:, so Owner hands back its container's bead, and reads
// nothing when no repo = is declared.
func TestOwnerResolvesThroughTheContainer(t *testing.T) {
	contextstest.Workspace(t)
	old := ShowBead
	t.Cleanup(func() { ShowBead = old })
	ShowBead = func(id string) (bd.Bead, error) { return bd.Bead{ID: id, Labels: []string{contextstest.Label}}, nil }
	if got := Owner(bd.Bead{ID: "leaf"}, "checkpoint/cp"); got.ID != "cp" {
		t.Errorf("Owner = %q, want the container cp", got.ID)
	}
	if got := Owner(bd.Bead{ID: "leaf"}, ""); got.ID != "leaf" {
		t.Errorf("no container: Owner = %q, want the leaf", got.ID)
	}
	t.Chdir(t.TempDir())
	ShowBead = func(string) (bd.Bead, error) { t.Fatal("read a bead with no repo declared"); return bd.Bead{}, nil }
	Owner(bd.Bead{ID: "leaf"}, "checkpoint/cp")
}

// A repo = dir that holds no .git of its own would make git -C walk up into the
// Workspace repo, so it is unusable: no repo, no live entry, not a declared context.
func TestPlainDirRepoIsUnusable(t *testing.T) {
	root := repo(t, "[context.x]\nrepo = \"vendor/x\"\n", "")
	os.MkdirAll(filepath.Join(root, "vendor/x"), 0o755)
	t.Chdir(root)
	b := bd.Bead{Labels: []string{"context:x"}}
	if r, dir := RepoDirOf(b); r != "" || dir != "" {
		t.Errorf("RepoDirOf = %q, %q; want none", r, dir)
	}
	if got := Repos(); len(got) != 0 {
		t.Errorf("Repos = %v; want none", got)
	}
	if got := Contexts(root); len(got) != 0 {
		t.Errorf("Contexts = %v; want none (held like undeclared)", got)
	}
	os.Mkdir(filepath.Join(root, "vendor/x/.git"), 0o755)
	if r, _ := RepoDirOf(b); r == "" {
		t.Error("a dir with its own .git is not usable")
	}
}

func TestLandedMapsOnlyForeignToError(t *testing.T) {
	b := bd.Bead{ID: "x"}
	ws := func(err error, sha string) func(string) (string, error) {
		return func(string) (string, error) { return sha, err }
	}
	if sha, ok, err := Landed(b, "t", ws(nil, "abc")); sha != "abc" || !ok || err != nil {
		t.Errorf("landed: %q %v %v", sha, ok, err)
	}
	if _, ok, err := Landed(b, "t", ws(git.ErrNotLanded, "")); ok || err != nil {
		t.Errorf("miss: %v %v", ok, err)
	}
	if _, ok, err := Landed(b, "t", ws(&git.ForeignLanding{}, "")); ok || err == nil {
		t.Errorf("foreign: %v %v", ok, err)
	}
	if _, ok, err := Landed(b, "t", ws(errors.New("git log failed"), "")); ok || err == nil {
		t.Errorf("transient: %v %v", ok, err)
	}
}

// A nested container's landed Tip: is read from the child repo (the sha is absent
// from the Workspace repo); a Workspace bead reads through ws.
func TestLandedTipReadsTheChild(t *testing.T) {
	_, kid := contextstest.Workspace(t)
	contextstest.Git(t, kid, "commit", "-q", "--allow-empty", "-m", "land\n\nTip: oldtip")
	sha := contextstest.Git(t, kid, "rev-parse", "HEAD")
	ws := func(string) (string, bool) { return "ws", true }
	if tip, ok := LandedTip(bd.Bead{Labels: []string{contextstest.Label}}, sha, ws); !ok || tip != "oldtip" {
		t.Errorf("nested LandedTip = %q, %v; want oldtip from the child", tip, ok)
	}
	if tip, _ := LandedTip(bd.Bead{}, sha, ws); tip != "ws" {
		t.Errorf("Workspace LandedTip = %q, want ws", tip)
	}
}
