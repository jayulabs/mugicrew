package git

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// replayScene: base commit B, a stranded branch head H (two files changed) on B, and
// a mainline that moved on past B. conflict makes mainline edit a.txt too.
func replayScene(t *testing.T, conflict bool) (tip, mainline, head string) {
	t.Helper()
	newRepo(t)
	writeAll(t, map[string]string{"a.txt": "one\ntwo\nthree\n", "b.txt": "b\n"})
	tip = commitAll(t, "base")
	mustGit(t, "checkout", "-q", "-b", "stranded")
	edit(t, "a.txt", replaceOnce("one\n", "ONE\n"))
	writeAll(t, map[string]string{"new.txt": "new\n"})
	head = commitAll(t, "stranded work")
	mustGit(t, "checkout", "-q", "main")
	if conflict {
		edit(t, "a.txt", replaceOnce("one\n", "uno\n"))
	} else {
		edit(t, "b.txt", replaceOnce("b\n", "bb\n"))
	}
	mainline = commitAll(t, "mainline moves")
	return tip, mainline, head
}

func TestReplayOntoCleanYieldsTreeAndSeedCommit(t *testing.T) {
	tip, mainline, head := replayScene(t, false)
	before := mustGit(t, "for-each-ref")
	p, err := Repo("").ReplayOnto(tip, mainline, head)
	if err != nil || !p.Clean() {
		t.Fatalf("ReplayOnto = %+v, %v; want clean", p, err)
	}
	if got := mustGit(t, "for-each-ref"); got != before {
		t.Errorf("ReplayOnto wrote a ref:\n%s\nwas\n%s", got, before)
	}
	sha, err := Repo("").SeedCommit(mainline, p.Tree, "replay: stranded work of X", "Replayed: "+tip+".."+head, "Remnant: r1")
	if err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, "rev-parse", sha+"^{tree}"); got != p.Tree {
		t.Errorf("seed tree = %s, want %s", got, p.Tree)
	}
	if got := mustGit(t, "rev-parse", sha+"^"); got != mainline {
		t.Errorf("seed parent = %s, want mainline %s", got, mainline)
	}
	msg := mustGit(t, "log", "-1", "--format=%B", sha)
	for _, want := range []string{"replay: stranded work of X", "Replayed: " + tip + ".." + head, "Remnant: r1"} {
		if !strings.Contains(msg, want) {
			t.Errorf("seed message lacks %q:\n%s", want, msg)
		}
	}
	mustGit(t, "checkout", "-q", "--detach", sha)
	for name, want := range map[string]string{"a.txt": "ONE\ntwo\nthree\n", "b.txt": "bb\n", "new.txt": "new\n"} {
		if b, _ := os.ReadFile(name); string(b) != want {
			t.Errorf("%s = %q, want %q", name, b, want)
		}
	}
}

func TestReplayOntoConflictNamesPathsAndWritesNoRef(t *testing.T) {
	tip, mainline, head := replayScene(t, true)
	before := mustGit(t, "for-each-ref")
	p, err := Repo("").ReplayOnto(tip, mainline, head)
	if err != nil {
		t.Fatal(err)
	}
	if p.Clean() || p.Tree != "" || !slices.Equal(p.Conflicts, []string{"a.txt"}) {
		t.Errorf("ReplayOnto = %+v, want conflict on a.txt", p)
	}
	if got := mustGit(t, "for-each-ref"); got != before {
		t.Errorf("a conflicting replay wrote a ref:\n%s\nwas\n%s", got, before)
	}
}

// A head rewritten after its pin (tip not an ancestor of head) replays three-way.
func TestReplayOntoRewrittenBranchStillReplays(t *testing.T) {
	tip, mainline, _ := replayScene(t, false)
	mustGit(t, "checkout", "-q", "-b", "rewritten", "stranded")
	mustGit(t, "commit", "--amend", "-q", "-m", "stranded work, reworded")
	head := mustGit(t, "rev-parse", "HEAD")
	mustGit(t, "checkout", "-q", "main")
	// the pin is a sibling of the head's history, not an ancestor of it
	mustGit(t, "checkout", "-q", "-b", "pin", tip)
	writeAll(t, map[string]string{"pinned.txt": "p\n"})
	pin := commitAll(t, "pinned")
	mustGit(t, "checkout", "-q", "main")
	if Repo("").IsAncestor(pin, head) {
		t.Fatal("scene: pin must not be an ancestor of head")
	}
	p, err := Repo("").ReplayOnto(pin, mainline, head)
	if err != nil || p.Tree == "" {
		t.Fatalf("ReplayOnto = %+v, %v; want a tree", p, err)
	}
}

func adoptScene(t *testing.T) {
	t.Helper()
	newRepo(t)
	mustGit(t, "branch", integCP)
	wt := filepath.Join(t.TempDir(), "wt")
	mustGit(t, "worktree", "add", "-b", EffortBranch("old"), wt, integCP)
	if err := recordCut(Repo(""), "old", integCP); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(wt, "w.txt"), "work\n")
	mustGit(t, "-C", wt, "add", ".")
	mustGit(t, "-C", wt, "commit", "-m", "old work")
}

func TestAdoptEffortThenIntegrateReplaysTheOldEffortsOwnCommits(t *testing.T) {
	adoptScene(t)
	// a fresh container: main moved on, the new container cut from it
	writeFile(t, "m.txt", "m\n")
	commitAll(t, "mainline moves")
	mustGit(t, "branch", "checkpoint/new", "main")
	if err := Repo("").AdoptEffort("old", "new"); err != nil {
		t.Fatal(err)
	}
	if a, b := mustGit(t, "rev-parse", EffortBranch("new")), mustGit(t, "rev-parse", EffortBranch("old")); a != b {
		t.Errorf("effort/new = %s, want effort/old %s", a, b)
	}
	wt := filepath.Join(t.TempDir(), "wt2")
	mustGit(t, "worktree", "add", wt, EffortBranch("new"))
	t.Chdir(wt)
	sha, _, err := Integrate("checkpoint/new", "new", "feat: new [new]", "Build: new")
	if err != nil || sha == "" {
		t.Fatalf("Integrate = %q, %v", sha, err)
	}
	if got := mustGit(t, "diff", "--name-only", "checkpoint/new^", "checkpoint/new"); got != "w.txt" {
		t.Errorf("squash changed %q, want only the old effort's w.txt", got)
	}
	if got := mustGit(t, "log", "--format=%s", "main..checkpoint/new"); got != "feat: new [new]" {
		t.Errorf("container commits = %q, want one squash", got)
	}
}

func TestAdoptEffortWithoutOldCutRefRefusesAndCreatesNoRef(t *testing.T) {
	adoptScene(t)
	mustGit(t, "update-ref", "-d", cutRefPrefix+"old")
	before := mustGit(t, "for-each-ref")
	if err := Repo("").AdoptEffort("old", "new"); !errors.Is(err, ErrNoCutRef) {
		t.Fatalf("AdoptEffort without a cut ref = %v, want ErrNoCutRef", err)
	}
	if got := mustGit(t, "for-each-ref"); got != before {
		t.Errorf("refs changed on refusal:\n%s\nwas\n%s", got, before)
	}
}

func TestAdoptEffortWhenNewEffortExistsWritesNeitherRef(t *testing.T) {
	adoptScene(t)
	mustGit(t, "branch", EffortBranch("new"), "main")
	before := mustGit(t, "for-each-ref")
	if err := Repo("").AdoptEffort("old", "new"); err == nil {
		t.Fatal("AdoptEffort onto an existing effort succeeded")
	}
	if got := mustGit(t, "for-each-ref"); got != before {
		t.Errorf("refs changed on refusal:\n%s\nwas\n%s", got, before)
	}
}
