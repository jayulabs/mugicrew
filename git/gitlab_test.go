package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/forge"
)

// gitlabForge wires newRepo's repo to a bare Forge (its remote URL spelled as a
// GitLab ssh URL, rewritten to the bare repo for transport) and a stateful fake glab
// on PATH: POST creates MR !5 holding the sent description, GET lists it, PUT replaces
// the description. It returns the bare repo and the fake's argv log.
func gitlabForge(t *testing.T) (bare, argvLog string) {
	t.Helper()
	newRepo(t)
	defer SetGraphID("g00")()
	bare = wireForge(t, "", "ns/p")
	return bare, filepath.Join(installGlab(t), "argv.log")
}

// wireForge points r (the empty Repo is the Workspace) at a fresh bare Forge: origin
// is spelled as the GitLab ssh URL for path, rewritten to the bare repo for transport.
func wireForge(t *testing.T, r Repo, path string) (bare string) {
	t.Helper()
	bare = filepath.Join(t.TempDir(), "forge.git")
	mustGit(t, "init", "--bare", "-b", "main", bare)
	url := "git@gl.example.com:" + path + ".git"
	for _, a := range [][]string{{"remote", "add", "origin", url}, {"config", "url." + bare + ".insteadOf", url}} {
		if _, err := r.run(a...); err != nil {
			t.Fatal(err)
		}
	}
	return bare
}

// installGlab puts the fake glab on PATH (process-wide, so once per test) and returns
// its state dir. State is keyed by the projects/<ID> in argv: ns%2Fp keeps the dir
// itself (desc, project), any other project uses <dir>/<ID>. Every project has !5.
func installGlab(t *testing.T) (dir string) {
	t.Helper()
	dir = t.TempDir()
	script := `#!/bin/sh
d=` + dir + `
echo "$*" >> $d/argv.log
pid=$(echo "$*" | sed -n 's|.*projects/\([^/ ?]*\).*|\1|p')
path=$(echo "$pid" | sed 's|%2F|/|g')
if [ "$pid" = ns%2Fp ]; then s=$d; else s=$d/$pid; mkdir -p $s; fi
p=; for a in "$@"; do case "$a" in description=@*) if [ "$p" = -F ] || [ "$p" = --field ]; then cat "${a#description=@}" > $s/desc.tmp; else printf %s "$a" > $s/desc.tmp; fi;; esac; p=$a; done
case "$*" in
auth*) exit 0;;
*" user") printf 'HTTP/2.0 200 OK\n\n{"username":"ann","id":42}';;
*"projects/$pid") if [ -f $s/project ]; then cat $s/project; else printf 'HTTP/2.0 200 OK\n\n{"squash_commit_template":"%%{title}\\n\\n%%{description}","squash_option":"always"}'; fi;;
*"-X POST"*) if [ -f $s/dup ]; then cp $s/dup $s/desc; printf 'HTTP/2.0 409 Conflict\n\n{"message":"already exists"}'; else mv $s/desc.tmp $s/desc; printf 'HTTP/2.0 201 Created\n\n{"iid":5,"web_url":"https://gl.example.com/%s/-/merge_requests/5"}' "$path"; fi;;
*"-X PUT"*) mv $s/desc.tmp $s/desc; printf 'HTTP/2.0 200 OK\n\n{"iid":5,"web_url":"https://gl.example.com/%s/-/merge_requests/5"}' "$path";;
*) if [ -f $s/desc ]; then b=; while IFS= read -r l; do b="$b$l\\n"; done < $s/desc; printf 'HTTP/2.0 200 OK\n\n[{"iid":5,"web_url":"https://gl.example.com/%s/-/merge_requests/5","description":"%s"}]' "$path" "$b"; else printf 'HTTP/2.0 200 OK\n\n[]'; fi;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "glab"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return dir
}

// A gitlab Land pushes the pin to the container branch on the Forge, opens the
// branch's MR carrying the trailer block, and is pending with the MR iid; a second
// tick pushes nothing and reuses the MR (Spec FR 9–12, 14, 15).
func TestLandGitlabPushesPinOpensMRAndIsPending(t *testing.T) {
	bare, argv := gitlabForge(t)
	defer SetGraphID("g00")()
	mustGit(t, "checkout", "-b", "chart/x")
	writeFile(t, "a", "a\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "work")
	pin := mustGit(t, "rev-parse", "HEAD")
	mustGit(t, "checkout", "main")

	_, err := Land("chart/x", "Chart: x", landGitlab)
	var p *LandPending
	if !errors.Is(err, ErrLandPending) || !errors.As(err, &p) || p.MR != 5 || !p.Pushed || !p.Opened || p.WebURL != "https://gl.example.com/ns/p/-/merge_requests/5" {
		t.Fatalf("Land = %v (%+v), want pending !5 pushed+opened with its web URL", err, p)
	}
	if got := mustGit(t, "-C", bare, "rev-parse", "refs/heads/chart/x"); got != pin {
		t.Errorf("Forge branch = %s, want the pin %s", got, pin)
	}
	b, _ := os.ReadFile(filepath.Join(filepath.Dir(argv), "desc"))
	if want := "Tip: " + pin + "\nChart: x\nGraph: g00"; strings.TrimSpace(string(b)) != want {
		t.Errorf("MR description = %q, want %q", b, want)
	}
	if a, _ := os.ReadFile(argv); !strings.Contains(string(a), "title=land chart/x") || !strings.Contains(string(a), "target_branch=main") {
		t.Errorf("create argv = %s", a)
	}
	if a, _ := os.ReadFile(argv); strings.Count(string(a), "assignee_id=42") != 1 || strings.Count(string(a), " user\n") != 1 {
		t.Errorf("create must send assignee_id=42 once after one /user read: %s", a)
	}

	_, err = Land("chart/x", "Chart: x", landGitlab)
	if !errors.As(err, &p) || p.MR != 5 || p.Pushed || p.Opened || p.WebURL != "https://gl.example.com/ns/p/-/merge_requests/5" {
		t.Errorf("second Land = %v (%+v), want pending !5 with its web URL, nothing pushed or opened", err, p)
	}
	if a, _ := os.ReadFile(argv); strings.Count(string(a), "assignee_id") != 1 || strings.Count(string(a), " user\n") != 1 {
		t.Errorf("reuse must not read /user or send an assignee: %s", a)
	}
}

// An unpinned gitlab Land titles its MR with the subject its caller supplies, never "land <branch>".
func TestLandGitlabTitlesTheMRWithTheCallerSubject(t *testing.T) {
	_, argv := gitlabForge(t)
	defer SetGraphID("g00")()
	mustGit(t, "branch", "chart/x")
	st, err := LandStrategyFor(LandStrategyGitlab, "", "docs(mugicrew): Chart it (MC-2)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Land("chart/x", "Chart: x", st); !errors.Is(err, ErrLandPending) {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(argv)
	if !strings.Contains(string(a), "title=docs(mugicrew): Chart it (MC-2)") || strings.Contains(string(a), "title=land chart/x") {
		t.Errorf("create argv = %s", a)
	}
}

// A reused MR whose description lost the trailer block gets it rewritten.
func TestLandGitlabRewritesABrokenDescription(t *testing.T) {
	_, argv := gitlabForge(t)
	defer SetGraphID("g00")()
	mustGit(t, "branch", "chart/x")
	if _, err := Land("chart/x", "Chart: x", landGitlab); !errors.Is(err, ErrLandPending) {
		t.Fatal(err)
	}
	desc := filepath.Join(filepath.Dir(argv), "desc")
	os.WriteFile(desc, []byte("a human edited this\n"), 0o644)
	Land("chart/x", "Chart: x", landGitlab)
	if b, _ := os.ReadFile(desc); !strings.Contains(string(b), "\nChart: x\nGraph: g00") {
		t.Errorf("description = %q, want the trailer block back", b)
	}
}

// Under local, a full Land never invokes glab (Spec FR 2).
func TestLandLocalNeverInvokesGlab(t *testing.T) {
	newRepo(t)
	defer SetGraphID("g00")()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "glab"), []byte("#!/bin/sh\ntouch "+dir+"/invoked\nexit 1\n"), 0o755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	mustGit(t, "checkout", "-b", "chart/x")
	writeFile(t, "a", "a\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "work")
	mustGit(t, "checkout", "main")
	if _, err := Land("chart/x", "Chart: x", LandLocal); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "invoked")); err == nil {
		t.Error("glab was invoked under local")
	}
}

// badProject makes the fake Forge's project carry a template without %{description}.
func badProject(t *testing.T, argv string) {
	t.Helper()
	os.WriteFile(filepath.Join(filepath.Dir(argv), "project"), []byte(`HTTP/2.0 200 OK

{"squash_commit_template":"%{title}","squash_option":"always"}`), 0o644)
}

func projectReads(t *testing.T, argv string) int {
	b, _ := os.ReadFile(argv)
	return strings.Count(string(b), "projects/ns%2Fp\n")
}

// The write leg refuses a bad template before pushing or creating; a pending tick
// (pin already pushed, MR open and right) never reads the project (Spec FR 37).
func TestLandGitlabChecksProjectBeforeWriteOnly(t *testing.T) {
	bare, argv := gitlabForge(t)
	defer SetGraphID("g00")()
	mustGit(t, "checkout", "-b", "chart/x")
	writeFile(t, "a", "a\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "work")
	mustGit(t, "checkout", "main")

	badProject(t, argv)
	var ref *forge.ProjectRefusal
	if _, err := Land("chart/x", "Chart: x", landGitlab); !errors.As(err, &ref) || ref.Field != "squash_commit_template" {
		t.Fatalf("Land = %v, want a template refusal", err)
	}
	if out, _ := run("ls-remote", bare, "refs/heads/chart/x"); out != "" {
		t.Error("pushed despite a bad template")
	}
	if a, _ := os.ReadFile(argv); strings.Contains(string(a), "POST") {
		t.Error("created an MR despite a bad template")
	}

	os.Remove(filepath.Join(filepath.Dir(argv), "project"))
	if _, err := Land("chart/x", "Chart: x", landGitlab); !errors.Is(err, ErrLandPending) {
		t.Fatal(err)
	}
	badProject(t, argv)
	before := projectReads(t, argv)
	if _, err := Land("chart/x", "Chart: x", landGitlab); !errors.Is(err, ErrLandPending) {
		t.Fatalf("pending tick = %v, want pending despite a bad template", err)
	}
	if got := projectReads(t, argv); got != before {
		t.Errorf("pending tick read the project %d times", got-before)
	}
}

// loop start's preflight starts on a clean project, refuses a bad template and a
// foreign-graph trailer carrying our issue_prefix, and under local calls nothing.
func TestPreflightGitlab(t *testing.T) {
	bare, argv := gitlabForge(t)
	defer SetGraphID("g00")()
	defer func(f func() (string, error)) { readIssuePrefix = f }(readIssuePrefix)
	readIssuePrefix = func() (string, error) { return "mc", nil }
	mustGit(t, "push", bare, "main")

	if err := PreflightGitlab(); err != nil {
		t.Fatalf("local preflight = %v", err)
	}
	if _, err := os.Stat(argv); err == nil {
		t.Fatal("glab invoked under local")
	}

	defer func(f func() (string, error)) { LandStrategyName = f }(LandStrategyName)
	LandStrategyName = func() (string, error) { return LandStrategyGitlab, nil }
	if err := PreflightGitlab(); err != nil {
		t.Fatalf("clean project = %v", err)
	}

	badProject(t, argv)
	var ref *forge.ProjectRefusal
	if err := PreflightGitlab(); !errors.As(err, &ref) {
		t.Errorf("bad template = %v, want a refusal", err)
	}
	os.Remove(filepath.Join(filepath.Dir(argv), "project"))

	mustGit(t, "commit", "--allow-empty", "-m", "land\n\nChart: other-1\nGraph: g00")
	mustGit(t, "commit", "--allow-empty", "-m", "land\n\nChart: mc-abc\nGraph: g99")
	mustGit(t, "push", bare, "main")
	var foreign *ForeignLanding
	if err := PreflightGitlab(); !errors.As(err, &foreign) || foreign.Theirs != "g99" || foreign.Trailer != "Chart: mc-abc" {
		t.Errorf("foreign trailer = %v, want ForeignLanding g99", err)
	}

	// anchors and reverts carry Graph: too (FR 38, FR 44)
	for _, trailer := range []string{"Spec: mc-s1", "Reverts-Checkpoint: mc-s1"} {
		mustGit(t, "reset", "-q", "--hard", "HEAD~2")
		mustGit(t, "commit", "--allow-empty", "-m", "x\n\n"+trailer+"\nGraph: g77")
		mustGit(t, "push", "-f", bare, "main")
		if err := PreflightGitlab(); !errors.As(err, &foreign) || foreign.Theirs != "g77" || foreign.Trailer != trailer {
			t.Errorf("%s = %v, want ForeignLanding g77", trailer, err)
		}
		mustGit(t, "commit", "--allow-empty", "-m", "pad")
		mustGit(t, "commit", "--allow-empty", "-m", "pad")
	}
}

// An unusable land_strategy refuses LandedBase and the preflight
// rather than behaving as local (ADR 0147 §3).
func TestUnusableLandStrategyRefuses(t *testing.T) {
	bad := errors.New("land_strategy \"GitLab\" is unusable")
	defer func(f func() (string, error)) { LandStrategyName = f }(LandStrategyName)
	LandStrategyName = func() (string, error) { return "", bad }
	if _, err := LandedBase("Checkpoint: x"); !errors.Is(err, bad) {
		t.Errorf("LandedBase = %v, want %v", err, bad)
	}
	if err := PreflightGitlab(); !errors.Is(err, bad) {
		t.Errorf("PreflightGitlab = %v, want %v", err, bad)
	}
}

// Repo.Head maps a missing ref to ErrMissingRef, as package Head does.
func TestRepoHeadMissingRef(t *testing.T) {
	newRepo(t)
	r := Repo(".")
	if _, err := r.Head("main"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Head("nope"); !MissingRef(err) {
		t.Errorf("err = %v, want MissingRef", err)
	}
}

// The revert entry point pushes the pin to rollback/<id> and opens an MR with no Tip:
// titled with the revert's own subject; a second call reuses it; a bad project refuses
// before any push.
func TestPushRevertMRPushesPinOpensTiplessMR(t *testing.T) {
	bare, argv := gitlabForge(t)
	defer SetGraphID("g00")()
	squash := landCheckpoint(t, "a", "a.txt")
	pin, err := RevertOnto(mustGit(t, "rev-parse", Mainline()), squash, "Reverts-Checkpoint: a")
	if err != nil {
		t.Fatal(err)
	}

	p, err := Repo("").PushRevertMR("rb1", "Reverts-Checkpoint: a", pin)
	if err != nil || p.MR != 5 || !p.Pushed || !p.Opened {
		t.Fatalf("PushRevertMR = %+v, %v; want !5 pushed+opened", p, err)
	}
	if got := mustGit(t, "-C", bare, "rev-parse", "refs/heads/rollback/rb1"); got != pin {
		t.Errorf("Forge branch = %s, want %s", got, pin)
	}
	b, _ := os.ReadFile(filepath.Join(filepath.Dir(argv), "desc"))
	if want := "Reverts-Checkpoint: a\nGraph: g00"; strings.TrimSpace(string(b)) != want {
		t.Errorf("description = %q, want %q", b, want)
	}
	if a, _ := os.ReadFile(argv); !strings.Contains(string(a), "title=revert: "+mustGit(t, "log", "-1", "--format=%s", squash)) {
		t.Errorf("create argv = %s", a)
	}
	p, err = Repo("").PushRevertMR("rb1", "Reverts-Checkpoint: a", pin)
	if err != nil || p.MR != 5 || p.Pushed || p.Opened {
		t.Errorf("second = %+v, %v; want reuse", p, err)
	}

	badProject(t, argv)
	if _, err := Repo("").PushRevertMR("rb2", "Reverts-Checkpoint: a", pin); err == nil {
		t.Error("bad project must refuse")
	}
	if out, _ := run("ls-remote", "origin", "refs/heads/rollback/rb2"); out != "" {
		t.Errorf("pushed before the project check: %s", out)
	}
}

// A tip-less assert leaves a right block alone and rewrites a broken one.
func TestAssertMRDescriptionTiplessBlock(t *testing.T) {
	_, argv := gitlabForge(t)
	defer SetGraphID("g00")()
	block := "Reverts-Checkpoint: a\nGraph: g00\n"
	if err := Repo("").AssertMRDescription(&forge.MR{IID: 5, Description: block}, "", "Reverts-Checkpoint: a"); err != nil {
		t.Fatal(err)
	}
	if a, _ := os.ReadFile(argv); strings.Contains(string(a), "PUT") {
		t.Errorf("right block was rewritten: %s", a)
	}
	if err := Repo("").AssertMRDescription(&forge.MR{IID: 5, Description: "edited"}, "", "Reverts-Checkpoint: a"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(filepath.Dir(argv), "desc")); string(b) != "edited\n\n"+block {
		t.Errorf("description = %q, want %q", b, "edited\n\n"+block)
	}
}

// A pending repair drops only the stale trailer lines at the body's end and keeps the
// human's text byte-for-byte; a trailer-less body gets the block after a blank line.
func TestAssertMRDescriptionRepairsInPlace(t *testing.T) {
	_, argv := gitlabForge(t)
	defer SetGraphID("g00")()
	desc := filepath.Join(filepath.Dir(argv), "desc")
	for _, c := range []struct{ name, have, want string }{
		{"broken", "  human  \n\nTip: old\nChart: x\nGraph: stale\n", "  human  \n\nTip: abc\nChart: x\nGraph: g00\n"},
		{"deleted", "human\nGraph: mid\nmore", "human\nGraph: mid\nmore\n\nTip: abc\nChart: x\nGraph: g00\n"},
		{"only trailer", "Graph: stale", "Tip: abc\nChart: x\nGraph: g00\n"},
	} {
		os.Remove(desc)
		if err := Repo("").AssertMRDescription(&forge.MR{IID: 5, Description: c.have}, "abc", "Chart: x"); err != nil {
			t.Fatal(c.name, err)
		}
		if b, _ := os.ReadFile(desc); string(b) != c.want {
			t.Errorf("%s: description = %q, want %q", c.name, b, c.want)
		}
	}
}

// RevertOnto builds on the given base and moves no ref; a conflict is *RevertConflict.
func TestRevertOntoMovesNoRef(t *testing.T) {
	newRepo(t)
	defer SetGraphID("g00")()
	squash := landCheckpoint(t, "a", "a.txt")
	tip := mustGit(t, "rev-parse", Mainline())
	sha, err := RevertOnto(tip, squash, "Reverts-Checkpoint: a")
	if err != nil {
		t.Fatal(err)
	}
	if mustGit(t, "rev-parse", Mainline()) != tip || mustGit(t, "rev-parse", sha+"^") != tip {
		t.Error("revert must be a child of base and leave main alone")
	}
	if msg := mustGit(t, "log", "-1", "--format=%B", sha); !strings.Contains(msg, "Reverts-Checkpoint: a\nGraph: g00") {
		t.Errorf("message = %q", msg)
	}
	writeFile(t, "a.txt", "later\n")
	mustGit(t, "commit", "-am", "later")
	tip = mustGit(t, "rev-parse", Mainline())
	var c *RevertConflict
	if _, err := RevertOnto(tip, squash, "Reverts-Checkpoint: a"); !errors.As(err, &c) {
		t.Errorf("err = %v, want *RevertConflict", err)
	}
}

// FirstParentOnMain is X on a first-parent line, else the merge that brought X in.
func TestFirstParentOnMain(t *testing.T) {
	newRepo(t)
	x := mustGit(t, "rev-parse", "HEAD")
	writeFile(t, "b", "b\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "b")
	if got, err := FirstParentOnMain(x); err != nil || got != x {
		t.Errorf("ff history = %s, %v; want %s", got, err, x)
	}
	mustGit(t, "checkout", "-b", "side")
	writeFile(t, "s", "s\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "squash")
	sq := mustGit(t, "rev-parse", "HEAD")
	mustGit(t, "checkout", "main")
	mustGit(t, "merge", "--no-ff", "-m", "merge", "side")
	m := mustGit(t, "rev-parse", "HEAD")
	writeFile(t, "c", "c\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "c")
	if got, err := FirstParentOnMain(sq); err != nil || got != m {
		t.Errorf("merge history = %s, %v; want %s", got, err, m)
	}
}

// lineageGitlab is a gitlab Forge holding main, a lineage branch of one commit on it,
// and the commit's sha.
func lineageGitlab(t *testing.T) (bare, argv, sha string) {
	t.Helper()
	bare, argv = gitlabForge(t)
	t.Cleanup(func(f func() (string, error)) func() { return func() { LandStrategyName = f } }(LandStrategyName))
	LandStrategyName = func() (string, error) { return LandStrategyGitlab, nil }
	mustGit(t, "push", bare, "main")
	base := mustGit(t, "rev-parse", "main")
	return bare, argv, lineageCommit(t, "lin", "Lineage: stock/x "+base, base)
}

// Under gitlab LandLineage opens an MR whose title and description are the commit's
// own subject and body, with no Tip:/Graph:, and moves nothing local; a re-run reuses
// the open MR and pushes nothing (Spec D4, FR 47–54).
func TestLandLineageGitlabOpensExactMessageMR(t *testing.T) {
	bare, argv, sha := lineageGitlab(t)
	main := mustGit(t, "rev-parse", "main")

	url, err := LandLineage("lin")
	if err != nil || url != "https://gl.example.com/ns/p/-/merge_requests/5" {
		t.Fatalf("LandLineage = (%q, %v), want the MR's URL", url, err)
	}
	if got := mustGit(t, "-C", bare, "rev-parse", "refs/heads/lin"); got != sha {
		t.Errorf("Forge branch = %s, want %s", got, sha)
	}
	a, _ := os.ReadFile(argv)
	if !strings.Contains(string(a), "title=lineage: sync stock/x") {
		t.Errorf("create argv = %s, want the commit subject as title", a)
	}
	b, _ := os.ReadFile(filepath.Join(filepath.Dir(argv), "desc"))
	if want := "Lineage: stock/x " + main; strings.TrimSpace(string(b)) != want {
		t.Errorf("MR description = %q, want the commit body %q", b, want)
	}
	if mustGit(t, "rev-parse", "main") != main || !branchExists("lin") {
		t.Error("local main or the lineage branch moved")
	}

	mustGit(t, "-C", bare, "update-ref", "-d", "refs/heads/lin") // a second push would put it back
	if _, err := LandLineage("lin"); err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, "ls-remote", bare, "refs/heads/lin"); got != "" {
		t.Error("re-run pushed again instead of reusing the open MR")
	}
}

// A branch cut from a main the Forge has since moved past is refused before any push.
func TestLandLineageGitlabRefusesMainMovedOnForge(t *testing.T) {
	bare, _, _ := lineageGitlab(t)
	base := mustGit(t, "rev-parse", "main")
	moved := mustGit(t, "commit-tree", base+"^{tree}", "-p", base, "-m", "forge moved")
	mustGit(t, "push", bare, moved+":refs/heads/main")

	var e *MainMoved
	if _, err := LandLineage("lin"); !errors.As(err, &e) || e.Main != moved {
		t.Errorf("LandLineage = %v, want MainMoved against the Forge main %s", err, moved)
	}
	if got := mustGit(t, "ls-remote", bare, "refs/heads/lin"); got != "" {
		t.Error("pushed despite the refusal")
	}
}

// An invalid project setting refuses before any push.
func TestLandLineageGitlabChecksProjectBeforePush(t *testing.T) {
	bare, argv, _ := lineageGitlab(t)
	badProject(t, argv)
	var ref *forge.ProjectRefusal
	if _, err := LandLineage("lin"); !errors.As(err, &ref) {
		t.Errorf("LandLineage = %v, want a project refusal", err)
	}
	if got := mustGit(t, "ls-remote", bare, "refs/heads/lin"); got != "" {
		t.Error("pushed despite the refusal")
	}
}

// wiredChild is a newChild wired to its own bare Forge under ns/child, after the
// Workspace was wired to ns/p.
func wiredChild(t *testing.T) (child Repo, wsBare, childBare string) {
	t.Helper()
	child = newChild(t)
	wsBare = wireForge(t, "", "ns/p")
	childBare = wireForge(t, child, "ns/child")
	installGlab(t)
	return child, wsBare, childBare
}

// A wired child names its own Forge project; the Workspace's is untouched.
func TestRepoForgeProjectIsTheChildsOwn(t *testing.T) {
	child, _, _ := wiredChild(t)
	if got, err := child.ForgeProject(); err != nil || got.ID != "ns%2Fchild" {
		t.Errorf("child = %+v, %v; want ns%%2Fchild", got, err)
	}
	if got, err := ForgeProject(); err != nil || got.ID != "ns%2Fp" {
		t.Errorf("workspace = %+v, %v; want ns%%2Fp", got, err)
	}
	if _, err := child.run("config", "branch.main.remote", "up"); err != nil {
		t.Fatal(err)
	}
	if got := child.ForgeRemote(); got != "up" {
		t.Errorf("ForgeRemote = %q, want the branch's remote", got)
	}
	if got := ForgeRemote(); got != "origin" {
		t.Errorf("Workspace ForgeRemote = %q, want origin", got)
	}
}

// A child's FetchMainline fast-forwards only the child's main, and the fetched sha is
// in the child's object store.
func TestRepoFetchMainlineMovesOnlyTheChildsMain(t *testing.T) {
	child, _, childBare := wiredChild(t)
	wsMain, _ := run("rev-parse", "main")
	clone := filepath.Join(t.TempDir(), "c")
	mustGit(t, "clone", "-q", string(child), clone)
	mustGit(t, "-C", clone, "-c", "user.email=t@e.com", "-c", "user.name=t", "commit", "--allow-empty", "-m", "forge move")
	tip, _ := run("-C", clone, "rev-parse", "HEAD")
	mustGit(t, "-C", clone, "push", "-q", childBare, "HEAD:main")

	got, err := child.FetchMainline()
	if err != nil || got != tip {
		t.Fatalf("FetchMainline = %q, %v; want %s", got, err, tip)
	}
	if main, _ := child.run("rev-parse", "main"); main != tip {
		t.Errorf("child main = %s, want %s", main, tip)
	}
	if _, err := child.run("cat-file", "-e", tip+"^{commit}"); err != nil {
		t.Errorf("fetched sha missing from the child's store: %v", err)
	}
	if now, _ := run("rev-parse", "main"); now != wsMain {
		t.Errorf("Workspace main moved to %s", now)
	}
	if _, err := os.Stat(filepath.Join(string(child), ".git", "mugicrew", "land.lock")); err == nil {
		t.Error("land.lock created in the child")
	}
}

// Under gitlab a child's LandedBase accepts ours or a legacy trailer and refuses a
// foreign Graph: (ADR 0187).
func TestRepoLandedBaseRefusesForeignGraphUnderGitlab(t *testing.T) {
	child, _, _ := wiredChild(t)
	defer SetGraphID("g00")()
	defer func(f func() (string, error)) { LandStrategyName = f }(LandStrategyName)
	LandStrategyName = func() (string, error) { return LandStrategyGitlab, nil }
	for _, m := range []string{"a\n\nChart: mc-ours\nGraph: g00", "b\n\nChart: mc-old", "c\n\nChart: mc-theirs\nGraph: g99"} {
		if _, err := child.run("commit", "--allow-empty", "-m", m); err != nil {
			t.Fatal(err)
		}
	}
	for _, tr := range []string{"Chart: mc-ours", "Chart: mc-old"} {
		if sha, err := child.LandedBase(tr); err != nil || sha == "" {
			t.Errorf("LandedBase(%s) = %q, %v; want landed", tr, sha, err)
		}
	}
	var foreign *ForeignLanding
	if _, err := child.LandedBase("Chart: mc-theirs"); !errors.As(err, &foreign) || foreign.Theirs != "g99" {
		t.Errorf("foreign = %v, want ForeignLanding g99", err)
	}
	if _, err := child.LandedBase("Chart: nope"); !errors.Is(err, ErrNotLanded) {
		t.Errorf("absent = %v, want ErrNotLanded", err)
	}
}

func TestCheckSameHost(t *testing.T) {
	ws := forge.Project{Host: "gl.example.com", ID: "ns%2Fp"}
	if err := CheckSameHost(ws, forge.Project{Host: "gl.example.com", ID: "ns%2Fc"}); err != nil {
		t.Errorf("same host = %v", err)
	}
	var dh *DifferentHost
	if err := CheckSameHost(ws, forge.Project{Host: "other.example.com", ID: "ns%2Fc"}); !errors.As(err, &dh) {
		t.Errorf("other host = %v, want *DifferentHost", err)
	}
}

// armNestedPreflight wires a gitlab preflight over the Workspace (ns/p) and one
// declared nested repo (context "kid", ns/child), both mainlines on their Forges.
// It returns the fake glab's argv log.
func armNestedPreflight(t *testing.T) (child Repo, argv string) {
	t.Helper()
	child, wsBare, childBare := wiredChild(t)
	t.Cleanup(SetGraphID("g00"))
	mustGit(t, "push", wsBare, "main")
	if _, err := child.run("push", childBare, "main"); err != nil {
		t.Fatal(err)
	}
	argv = filepath.Join(strings.SplitN(os.Getenv("PATH"), ":", 2)[0], "argv.log")
	f, g, h := readIssuePrefix, LandStrategyName, DeclaredRepos
	t.Cleanup(func() { readIssuePrefix, LandStrategyName, DeclaredRepos = f, g, h })
	readIssuePrefix = func() (string, error) { return "mc", nil }
	LandStrategyName = func() (string, error) { return LandStrategyGitlab, nil }
	DeclaredRepos = func() map[string]string { return map[string]string{"kid": "child"} }
	return child, argv
}

// The preflight hits the Workspace's and each child's project once, and a child
// failing its squash settings, fetch or foreign-Graph scan names its context.
func TestPreflightGitlabCoversEachProjectAndNamesTheContext(t *testing.T) {
	child, argv := armNestedPreflight(t)
	if err := PreflightGitlab(); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(argv)
	for _, p := range []string{"projects/ns%2Fp\n", "projects/ns%2Fchild\n"} {
		if n := strings.Count(string(a), p); n != 1 {
			t.Errorf("%q read %d times, want once", p, n)
		}
	}

	badProject(t, filepath.Join(filepath.Dir(argv), "ns%2Fchild", "x")) // child's project file
	var ref *forge.ProjectRefusal
	if err := PreflightGitlab(); !errors.As(err, &ref) || !strings.Contains(err.Error(), "context kid: ") {
		t.Errorf("bad child template = %v, want a refusal naming kid", err)
	}
	os.Remove(filepath.Join(filepath.Dir(argv), "ns%2Fchild", "project"))

	if _, err := child.run("commit", "--allow-empty", "-m", "land\n\nChart: mc-abc\nGraph: g99"); err != nil {
		t.Fatal(err)
	}
	if _, err := child.run("push", "origin", "main"); err != nil {
		t.Fatal(err)
	}
	var foreign *ForeignLanding
	if err := PreflightGitlab(); !errors.As(err, &foreign) || !strings.Contains(err.Error(), "context kid: ") {
		t.Errorf("foreign child land = %v, want ForeignLanding naming kid", err)
	}

	if _, err := child.run("remote", "remove", "origin"); err != nil {
		t.Fatal(err)
	}
	if err := PreflightGitlab(); err == nil || !strings.Contains(err.Error(), "context kid: ") {
		t.Errorf("no remote = %v, want a refusal naming kid", err)
	}
}

// A child on another host is refused with the typed error, naming its context; a
// child sharing the Workspace's project is preflighted once.
func TestPreflightGitlabRefusesAnotherHostAndDedupes(t *testing.T) {
	child, argv := armNestedPreflight(t)
	if _, err := child.run("config", "remote.origin.url", "git@other.example.com:ns/child.git"); err != nil {
		t.Fatal(err)
	}
	var dh *DifferentHost
	if err := PreflightGitlab(); !errors.As(err, &dh) || !strings.Contains(err.Error(), "context kid: ") {
		t.Errorf("other host = %v, want DifferentHost naming kid", err)
	}

	if _, err := child.run("config", "remote.origin.url", "git@gl.example.com:ns/p.git"); err != nil {
		t.Fatal(err)
	}
	os.Remove(argv)
	if err := PreflightGitlab(); err != nil {
		t.Fatal(err)
	}
	if a, _ := os.ReadFile(argv); strings.Count(string(a), "projects/ns%2Fp\n") != 1 {
		t.Errorf("shared project read more than once:\n%s", a)
	}
}

// childCommit is a commit on top of the child's main, held by no branch.
func childCommit(t *testing.T, child Repo, msg string) string {
	t.Helper()
	sha, err := child.run("commit-tree", child.Mainline()+"^{tree}", "-p", child.Mainline(), "-m", msg)
	if err != nil {
		t.Fatal(err)
	}
	return sha
}

// PinnedGitlab on a child pushes its pin to the child's Forge and opens its MR; every
// glab call names the child's project, and the Workspace's MR state is untouched.
func TestPinnedGitlabOnAChildActsOnlyOnTheChildProject(t *testing.T) {
	child, wsBare, childBare := wiredChild(t)
	defer SetGraphID("g00")()
	pin := childCommit(t, child, "work")
	if _, err := child.run("branch", "chart/x", pin); err != nil {
		t.Fatal(err)
	}
	_, err := child.Land("chart/x", "Chart: x", child.PinnedGitlab(pin, MRPresentation{Title: "land chart/x"}))
	var p *LandPending
	if !errors.As(err, &p) || p.MR != 5 || !p.Pushed || !p.Opened || p.WebURL != "https://gl.example.com/ns/child/-/merge_requests/5" {
		t.Fatalf("Land = %v (%+v), want pending !5 pushed+opened with the child project's absolute web URL", err, p)
	}
	if got := mustGit(t, "-C", childBare, "rev-parse", "refs/heads/chart/x"); got != pin {
		t.Errorf("child Forge branch = %s, want %s", got, pin)
	}
	if out := mustGit(t, "ls-remote", wsBare, "refs/heads/chart/x"); out != "" {
		t.Errorf("pushed to the Workspace's Forge: %s", out)
	}
	dir := filepath.Dir(argvPath(t))
	if a, _ := os.ReadFile(filepath.Join(dir, "argv.log")); strings.Contains(string(a), "projects/ns%2Fp") || !strings.Contains(string(a), "projects/ns%2Fchild") || !strings.Contains(string(a), "target_branch=main") {
		t.Errorf("argv = %s, want only the child's project", a)
	}
	if _, err := os.Stat(filepath.Join(dir, "desc")); err == nil {
		t.Error("the Workspace project's MR was written")
	}
	if _, err := os.Stat(filepath.Join(dir, "ns%2Fchild", "desc")); err != nil {
		t.Errorf("child MR description missing: %v", err)
	}
}

// PushRevertMR and AssertMRDescription on a child talk only to the child project.
func TestRevertMRSeamsOnAChildActOnlyOnTheChildProject(t *testing.T) {
	child, _, childBare := wiredChild(t)
	defer SetGraphID("g00")()
	tip := childCommit(t, child, "x")
	pin, err := child.RevertOnto(tip, tip, "Reverts-Checkpoint: a")
	if err != nil {
		t.Fatal(err)
	}
	p, err := child.PushRevertMR("rb1", "Reverts-Checkpoint: a", pin)
	if err != nil || p.MR != 5 || !p.Pushed || !p.Opened {
		t.Fatalf("PushRevertMR = %+v, %v; want !5 pushed+opened", p, err)
	}
	if got := mustGit(t, "-C", childBare, "rev-parse", "refs/heads/rollback/rb1"); got != pin {
		t.Errorf("child Forge branch = %s, want %s", got, pin)
	}
	if err := child.AssertMRDescription(&forge.MR{IID: 5, Description: "edited"}, "", "Reverts-Checkpoint: a"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(argvPath(t))
	if a, _ := os.ReadFile(filepath.Join(dir, "argv.log")); strings.Contains(string(a), "projects/ns%2Fp") {
		t.Errorf("argv names the Workspace project: %s", a)
	}
	if _, err := os.Stat(filepath.Join(dir, "desc")); err == nil {
		t.Error("the Workspace project's MR was written")
	}
}

// RevertOnto and FirstParentOnMain on a child read and write only the child.
func TestRevertOntoAndFirstParentOnAChildStayInTheChild(t *testing.T) {
	child, _, _ := wiredChild(t)
	defer SetGraphID("g00")()
	wsRefs := mustGit(t, "for-each-ref")
	base, _ := child.run("rev-parse", "main")
	x := childCommit(t, child, "x")
	if _, err := child.run("update-ref", "refs/heads/main", x, base); err != nil {
		t.Fatal(err)
	}
	if got, err := child.FirstParentOnMain(x); err != nil || got != x {
		t.Errorf("FirstParentOnMain = %q, %v; want %s", got, err, x)
	}
	if _, err := child.FirstParentOnMain(mustGit(t, "rev-parse", "HEAD")); err == nil {
		t.Error("a Workspace sha must not be on the child's mainline")
	}
	if sha, err := child.RevertOnto(x, x, "Reverts-Checkpoint: a"); err != nil || sha == "" {
		t.Errorf("RevertOnto = %q, %v", sha, err)
	}
	if now := mustGit(t, "for-each-ref"); now != wsRefs {
		t.Errorf("Workspace refs changed:\n%s\nwant\n%s", now, wsRefs)
	}
}

// argvPath is the fake glab's argv log: the dir of the glab found first on PATH.
func argvPath(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("glab")
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(filepath.Dir(p), "argv.log")
}

func TestRecordedMRRoundTripsBothForms(t *testing.T) {
	for _, tc := range []struct {
		line, path string
		iid        int
	}{{"MR: !5", "", 5}, {"MR: group/proj!5", "group/proj", 5}, {"MR: a/b/c!12", "a/b/c", 12}, {"", "", 0}} {
		body := "Commit: x\n" + tc.line + "\n"
		if path, iid := RecordedMRPath(body); path != tc.path || iid != tc.iid || RecordedMR(body) != tc.iid {
			t.Errorf("%q: path %q iid %d, want %q %d", tc.line, path, iid, tc.path, tc.iid)
		}
		if tc.iid != 0 && MRLine(tc.path, tc.iid) != tc.line {
			t.Errorf("MRLine(%q, %d) = %q, want %q", tc.path, tc.iid, MRLine(tc.path, tc.iid), tc.line)
		}
	}
}

// A presented Land MR is created with its title and a description of the human body, a
// blank line and the trailer block last; a reuse rewrites both in one PUT.
func TestPinnedGitlabPresentsTitleAndBody(t *testing.T) {
	_, argv := gitlabForge(t)
	mustGit(t, "branch", "chart/x")
	pin := mustGit(t, "rev-parse", "chart/x")
	strategy := Repo("").PinnedGitlab(pin, MRPresentation{Title: "Ship it", Body: "## Why\n\nbecause\n"})
	if _, err := Land("chart/x", "Chart: x", strategy); !errors.Is(err, ErrLandPending) {
		t.Fatal(err)
	}
	desc := filepath.Join(filepath.Dir(argv), "desc")
	b, _ := os.ReadFile(desc)
	if want := "## Why\n\nbecause\n\nTip: " + pin + "\nChart: x\nGraph: testgraphroot\n"; string(b) != want {
		t.Errorf("created description = %q, want %q", b, want)
	}
	if a, _ := os.ReadFile(argv); !strings.Contains(string(a), "title=Ship it") {
		t.Errorf("create argv = %s", a)
	}

	os.WriteFile(argv, nil, 0o644)
	strategy = Repo("").PinnedGitlab(pin, MRPresentation{Title: "Ship it v2"})
	if _, err := Land("chart/x", "Chart: x", strategy); !errors.Is(err, ErrLandPending) {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(desc)
	if want := "Tip: " + pin + "\nChart: x\nGraph: testgraphroot\n"; string(b) != want {
		t.Errorf("reused description = %q, want trailer only %q", b, want)
	}
	a, _ := os.ReadFile(argv)
	if n := strings.Count(string(a), "-X PUT"); n != 1 || !strings.Contains(string(a), "title=Ship it v2") {
		t.Errorf("reuse argv = %s, want one PUT carrying the title", a)
	}
}

// A Land that loses the create race (409) re-lists and rewrites the reused MR's title
// and body like any reuse: one PUT, no assignee.
func TestPinnedGitlabDuplicateRaceRewritesTitleAndBody(t *testing.T) {
	_, argv := gitlabForge(t)
	dir := filepath.Dir(argv)
	os.WriteFile(filepath.Join(dir, "dup"), []byte("stale\n"), 0o644) // the racing creator's MR, listed only after the 409
	mustGit(t, "branch", "chart/x")
	pin := mustGit(t, "rev-parse", "chart/x")
	strategy := Repo("").PinnedGitlab(pin, MRPresentation{Title: "Ship it", Body: "## Why\n\nbecause\n"})
	_, err := Land("chart/x", "Chart: x", strategy)
	var p *LandPending
	if !errors.As(err, &p) || p.WebURL != "https://gl.example.com/ns/p/-/merge_requests/5" {
		t.Fatalf("Land = %v (%+v), want pending carrying the re-listed MR's web URL", err, p)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "desc"))
	if want := "## Why\n\nbecause\n\nTip: " + pin + "\nChart: x\nGraph: testgraphroot\n"; string(b) != want {
		t.Errorf("description = %q, want %q", b, want)
	}
	a, _ := os.ReadFile(argv)
	if n := strings.Count(string(a), "-X PUT"); n != 1 || !strings.Contains(string(a), "title=Ship it") || strings.Contains(string(a[strings.Index(string(a), "-X PUT"):]), "assignee") {
		t.Errorf("argv = %s, want one PUT with the title and no assignee", a)
	}
}

func TestRecordedMRLinkRoundTrips(t *testing.T) {
	const url = "https://gl.example.com/ns/child/-/merge_requests/5"
	if got := RecordedMRLink("MR: ns/child!5\n" + MRLinkLine(url) + "\n"); got != url {
		t.Errorf("RecordedMRLink = %q, want %q", got, url)
	}
	if MRLinkLine(url) != "MR link: "+url {
		t.Errorf("MRLinkLine = %q", MRLinkLine(url))
	}
	if got := RecordedMRLink("MR: !5\n"); got != "" {
		t.Errorf("RecordedMRLink without a line = %q, want empty", got)
	}
}

func TestLandedBaseOnAndForeignScanReadTheGivenRef(t *testing.T) {
	gitlabForge(t)
	defer SetGraphID("g00")()
	defer func(f func() (string, error)) { readIssuePrefix = f }(readIssuePrefix)
	readIssuePrefix = func() (string, error) { return "mc", nil }
	defer func(f func() (string, error)) { LandStrategyName = f }(LandStrategyName)
	LandStrategyName = func() (string, error) { return LandStrategyGitlab, nil }

	mustGit(t, "branch", "ahead")
	mustGit(t, "switch", "-q", "ahead")
	mustGit(t, "commit", "--allow-empty", "-m", "land\n\nCheckpoint: mc-ok\nGraph: g00")
	mustGit(t, "commit", "--allow-empty", "-m", "land\n\nCheckpoint: mc-fx\nGraph: g99")
	mustGit(t, "switch", "-q", "-")

	if _, err := Repo("").LandedBaseOn("ahead", "Checkpoint: mc-ok"); err != nil {
		t.Errorf("ours on ref = %v", err)
	}
	if _, err := Repo("").LandedBase("Checkpoint: mc-ok"); !errors.Is(err, ErrNotLanded) {
		t.Errorf("local main reads the ref's land: %v", err)
	}
	var foreign *ForeignLanding
	if _, err := Repo("").LandedBaseOn("ahead", "Checkpoint: mc-fx"); !errors.As(err, &foreign) {
		t.Errorf("foreign on ref = %v", err)
	}
	if err := Repo("").ForeignScan("main"); err != nil {
		t.Errorf("clean main = %v", err)
	}
	if err := Repo("").ForeignScan("ahead"); !errors.As(err, &foreign) || foreign.Theirs != "g99" {
		t.Errorf("scan of ahead = %v", err)
	}
}
