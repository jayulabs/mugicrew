package locate

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
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

func sh(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@x", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@x")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, dir, msg string) {
	t.Helper()
	sh(t, dir, "add", "-A")
	sh(t, dir, "commit", "-qm", msg)
}

// synthetic is the prototype's synthetic Workspace (mc-1ysqn.4 mksyn.sh): main has
// idx.py; checkpoint/mc-a edits it and adds arm.py; checkpoint/mc-b touches an
// unrelated file; chart/mc-c and effort/mc-a.1 are decoys at mc-a's tip; mc-l is
// squash-landed with its trailer on main and its branch kept.
func synthetic(t *testing.T) git.Repo {
	t.Helper()
	d := t.TempDir()
	sh(t, d, "init", "-q", "-b", "main")
	write(t, d, "src/idx.py", "def most_cited(q):\n    return q.order_by(\"citation_count\")\n")
	write(t, d, "src/score.py", "def decay(age):\n    return 1.0 / (1 + age)\n")
	commit(t, d, "base")
	sh(t, d, "checkout", "-qb", "checkpoint/mc-l")
	write(t, d, "src/boost.py", "def legacy_boost(s): return s * 1.2\n")
	commit(t, d, "add legacy_boost")
	sh(t, d, "checkout", "-q", "main")
	sh(t, d, "merge", "-q", "--squash", "checkpoint/mc-l")
	sh(t, d, "commit", "-qm", "land checkpoint/mc-l\n\nCheckpoint: mc-l\n")
	write(t, d, "src/util.py", "def noop(): pass\n")
	commit(t, d, "main moves on")
	sh(t, d, "checkout", "-qb", "checkpoint/mc-a", "main")
	write(t, d, "src/idx.py", "def most_cited(q):\n    return q.using(\"prod_citation_count_idx\").order_by(\"citation_count\")\n")
	write(t, d, "src/arm.py", "from idx import most_cited\nCITE_WINDOW = 30\ndef arm(q): return most_cited(q)[:CITE_WINDOW]\n")
	commit(t, d, "most_cited arm on prod index")
	sh(t, d, "branch", "effort/mc-a.1")
	sh(t, d, "branch", "chart/mc-c")
	sh(t, d, "checkout", "-qb", "checkpoint/mc-b", "main")
	write(t, d, "src/window.py", "CITE_WINDOW = 14\n")
	commit(t, d, "window for b")
	sh(t, d, "checkout", "-q", "chart/mc-c")
	write(t, d, "docs/notes.md", "hot_shard_cache is suspected\n")
	commit(t, d, "research note")
	sh(t, d, "checkout", "-q", "main")
	return git.Repo(d)
}

func deps(r git.Repo, ids ...string) Deps {
	var beads []bd.Bead
	for _, id := range ids {
		beads = append(beads, bd.Bead{ID: id, Kind: contract.KindCheckpoint, Status: "open"})
	}
	return Deps{
		Checkpoints: func() ([]bd.Bead, error) { return beads, nil },
		RepoOf:      func(bd.Bead) git.Repo { return r },
		Repos:       func() ([]git.Repo, error) { return []git.Repo{r}, nil },
		Strategy:    func() (string, error) { return git.LandStrategyLocal, nil },
	}
}

func run(t *testing.T, d Deps, tokens, paths []string) Result {
	t.Helper()
	res, err := d.Locate(Query{Tokens: tokens, Paths: paths})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestMotivatingCaseIsOnlyMcA(t *testing.T) {
	d := deps(synthetic(t), "mc-a", "mc-b")
	for _, tokens := range [][]string{{"prod_citation_count_idx"}, {"prod_citation_count_idx", "most_cited"}} {
		res := run(t, d, tokens, []string{"src/idx.py"})
		if res.Verdict != "only" || res.Checkpoint != "mc-a" {
			t.Errorf("tokens %v: verdict %q %q, want only mc-a", tokens, res.Verdict, res.Checkpoint)
		}
	}
	res := run(t, d, []string{"most_cited"}, nil)
	if h := res.Hits[0]; !h.OnMain || !slices.Equal(h.Checkpoints, []string{"mc-a"}) {
		t.Errorf("most_cited hit = %+v, want on_main and on mc-a", h)
	}
}

func TestPathOnlyChangedByOneCheckpointIsOnly(t *testing.T) {
	res := run(t, deps(synthetic(t), "mc-a", "mc-b"), nil, []string{"src/idx.py"})
	if res.Verdict != "only" || res.Checkpoint != "mc-a" {
		t.Errorf("verdict %q %q, want only mc-a", res.Verdict, res.Checkpoint)
	}
}

func TestDefectTokenOnlyOnMainIsMain(t *testing.T) {
	res := run(t, deps(synthetic(t), "mc-a", "mc-b"), []string{"prod_citation_count_idx", "decay"}, nil)
	if res.Verdict != "main" {
		t.Errorf("verdict %q, want main", res.Verdict)
	}
}

func TestUnchangedPathOnMainIsMain(t *testing.T) {
	res := run(t, deps(synthetic(t), "mc-a", "mc-b"), nil, []string{"src/score.py"})
	if res.Verdict != "main" || !res.Hits[0].OnMain {
		t.Errorf("verdict %q hit %+v, want main", res.Verdict, res.Hits[0])
	}
}

func TestTokensOnTwoCheckpointsAreSeveral(t *testing.T) {
	res := run(t, deps(synthetic(t), "mc-a", "mc-b"), []string{"CITE_WINDOW"}, nil)
	if res.Verdict != "several" || res.Checkpoint != "" {
		t.Errorf("verdict %q %q, want several", res.Verdict, res.Checkpoint)
	}
}

func TestTermNowhereIsUnknownAndVerdictNone(t *testing.T) {
	res := run(t, deps(synthetic(t), "mc-a"), []string{"mistyped_tokn"}, []string{"src/nope.py"})
	if res.Verdict != "none" {
		t.Errorf("verdict %q, want none", res.Verdict)
	}
	for _, h := range res.Hits {
		if !h.Unknown {
			t.Errorf("hit %+v, want unknown", h)
		}
	}
}

func TestLandedCheckpointIsSkipped(t *testing.T) {
	res := run(t, deps(synthetic(t), "mc-a", "mc-l"), []string{"legacy_boost"}, nil)
	if res.Verdict == "only" || slices.Contains(res.Considered, "mc-l") {
		t.Errorf("verdict %q considered %v: landed mc-l must not count", res.Verdict, res.Considered)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].ID != "mc-l" || !strings.Contains(res.Skipped[0].Why, "landed") {
		t.Errorf("skipped = %+v, want mc-l landed", res.Skipped)
	}
}

func TestBeadWithoutBranchIsSkipped(t *testing.T) {
	res := run(t, deps(synthetic(t), "mc-a", "mc-x"), []string{"prod_citation_count_idx"}, nil)
	if len(res.Skipped) != 1 || res.Skipped[0].ID != "mc-x" || !strings.Contains(res.Skipped[0].Why, "no checkpoint/mc-x branch") {
		t.Errorf("skipped = %+v, want mc-x with no branch", res.Skipped)
	}
	if !slices.Equal(res.Considered, []string{"mc-a"}) {
		t.Errorf("considered %v, want [mc-a]", res.Considered)
	}
}

func TestDecoyBranchesAreNotCheckpointCode(t *testing.T) {
	// hot_shard_cache lives only on chart/mc-c
	res := run(t, deps(synthetic(t), "mc-a", "mc-b"), []string{"hot_shard_cache"}, nil)
	if res.Verdict != "none" || !res.Hits[0].Unknown {
		t.Errorf("verdict %q hit %+v, want none/unknown", res.Verdict, res.Hits[0])
	}
}

func TestGitlabReadsTrackingRefAndFallsBackWithWarning(t *testing.T) {
	r := synthetic(t)
	// a Forge whose mainline already carries a defect token local main lacks
	forge := t.TempDir()
	sh(t, forge, "clone", "-q", "--bare", string(r), ".")
	clone := t.TempDir()
	sh(t, clone, "clone", "-q", string(r), ".")
	sh(t, clone, "checkout", "-q", "main")
	write(t, clone, "src/merged.py", "merged_on_forge = 1\n")
	commit(t, string(clone), "mr merged on the forge")
	sh(t, clone, "push", "-q", forge, "main")
	sh(t, string(r), "remote", "add", "origin", forge)

	d := deps(r, "mc-a")
	d.Strategy = func() (string, error) { return git.LandStrategyGitlab, nil }
	res := run(t, d, []string{"merged_on_forge"}, nil)
	if res.Verdict != "main" || len(res.Warnings) != 0 {
		t.Errorf("verdict %q warnings %v, want main from the fetched tracking ref", res.Verdict, res.Warnings)
	}
	if err := exec.Command("git", "-C", string(r), "show-ref", "--verify", "-q", "refs/remotes/origin/main").Run(); err != nil {
		t.Error("tracking ref not written")
	}
	if err := exec.Command("git", "-C", string(r), "grep", "-q", "merged_on_forge", "main").Run(); err == nil {
		t.Error("local main must not be fast-forwarded")
	}

	sh(t, string(r), "remote", "set-url", "origin", filepath.Join(forge, "gone"))
	res = run(t, d, []string{"prod_citation_count_idx"}, nil)
	if res.Verdict != "only" || len(res.Warnings) != 1 || !strings.HasPrefix(res.Warnings[0], "mainline: local, fetch failed: ") {
		t.Errorf("verdict %q warnings %v, want local read with the fetch warning", res.Verdict, res.Warnings)
	}
}

func TestTextAndJSONCarryVerdictHitsSkippedConsidered(t *testing.T) {
	res := run(t, deps(synthetic(t), "mc-a", "mc-x"), []string{"prod_citation_count_idx"}, nil)
	var text bytes.Buffer
	res.WriteText(&text)
	for _, want := range []string{"verdict: only mc-a", "token prod_citation_count_idx", "skipped mc-x", "considered: mc-a"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("text output lacks %q:\n%s", want, text.String())
		}
	}
	var js bytes.Buffer
	if err := res.WriteJSON(&js); err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(js.Bytes(), &back); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"verdict", "checkpoint", "hits", "skipped", "considered"} {
		if _, ok := back[k]; !ok {
			t.Errorf("json lacks %q: %s", k, js.String())
		}
	}
}

func TestCheckpointsErrorPropagates(t *testing.T) {
	d := deps(synthetic(t))
	d.Checkpoints = func() ([]bd.Bead, error) { return nil, errors.New("bd down") }
	if _, err := d.Locate(Query{Tokens: []string{"x"}}); err == nil {
		t.Error("want the bd error")
	}
}

func TestQueryNeedsATerm(t *testing.T) {
	if _, err := deps(synthetic(t)).Locate(Query{}); err == nil {
		t.Error("want an error for no --token or --path")
	}
}

func TestTrailerReadFailureWarnsInsteadOfPassingSilently(t *testing.T) {
	d := t.TempDir()
	sh(t, d, "init", "-q", "-b", "main") // no commits: main does not resolve
	res := run(t, deps(git.Repo(d), "mc-x"), []string{"anything"}, nil)
	if !slices.ContainsFunc(res.Warnings, func(w string) bool { return strings.Contains(w, "mc-x") && strings.Contains(w, "trailer") }) {
		t.Errorf("warnings = %v, want one naming mc-x and the failed trailer read", res.Warnings)
	}
}

func TestTokenWithRegexMetacharactersReadsLiterally(t *testing.T) {
	d := t.TempDir()
	sh(t, d, "init", "-q", "-b", "main")
	write(t, d, "a.py", "x = idx(citation_count)\n")
	commit(t, d, "base")
	sh(t, d, "checkout", "-qb", "checkpoint/mc-a")
	write(t, d, "b.py", "y = idx(citation_count) + a.b\n")
	commit(t, d, "add token")
	sh(t, d, "checkout", "-qb", "checkpoint/mc-b", "main")
	write(t, d, "c.py", "z = idxXcitation_count\n")
	commit(t, d, "regex lookalike")
	sh(t, d, "checkout", "-q", "main")
	dd := deps(git.Repo(d), "mc-a", "mc-b")
	for _, tok := range []string{"idx(citation_count)", "a.b"} {
		res := run(t, dd, []string{tok}, nil)
		if res.Verdict != "only" || res.Checkpoint != "mc-a" {
			t.Errorf("token %q: verdict %q %q, want only mc-a (warnings %v)", tok, res.Verdict, res.Checkpoint, res.Warnings)
		}
	}
}

func TestInvalidTokenOrGitFailureWarnsInsteadOfPassingSilently(t *testing.T) {
	d := t.TempDir()
	sh(t, d, "init", "-q", "-b", "main")
	write(t, d, "a.py", "x\n")
	commit(t, d, "base")
	sh(t, d, "checkout", "-qb", "checkpoint/mc-a")
	write(t, d, "a.py", "y\n")
	commit(t, d, "change")
	sh(t, d, "checkout", "-q", "main")
	dd := deps(git.Repo(d), "mc-a")
	dd.Repos = func() ([]git.Repo, error) { return []git.Repo{git.Repo(d)}, nil }
	// A pathspec outside the repository makes git diff fail.
	res := run(t, dd, nil, []string{"../outside"})
	if !slices.ContainsFunc(res.Warnings, func(w string) bool { return strings.Contains(w, "../outside") }) {
		t.Errorf("warnings = %v, want one naming the failed path read", res.Warnings)
	}
}

// A failed repo-root read is a Warning, not a silent drop of the declared repos.
func TestLocateWarnsWhenDeclaredReposCannotBeResolved(t *testing.T) {
	r := synthetic(t)
	d := deps(r)
	d.Repos = func() ([]git.Repo, error) { return []git.Repo{r}, errors.New("not a git repo") }
	res := run(t, d, []string{"nothing"}, nil)
	if !slices.ContainsFunc(res.Warnings, func(w string) bool { return strings.Contains(w, "not a git repo") }) {
		t.Errorf("warnings = %q, want one naming the repo-root failure", res.Warnings)
	}
}
