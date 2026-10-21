package git

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- mugicrew integrate (Spec D3, ADR 0102 §3): real scratch repos ----

const integCP = "checkpoint/cp"

// integrateRepo is integrate's scene: main, a ref-only container checkpoint/cp
// cut from it, and an effort tree on effort/b1 cut from the container, which
// becomes the cwd (the verb runs in the effort's tree). It returns the main
// worktree's path.
func integrateRepo(t *testing.T) (root string) {
	t.Helper()
	newRepo(t)
	root, _ = os.Getwd()
	mustGit(t, "branch", integCP)
	wt := filepath.Join(t.TempDir(), "wt")
	mustGit(t, "worktree", "add", "-b", EffortBranch("b1"), wt, integCP)
	t.Chdir(wt)
	return root
}

func integrateB1() (string, int, error) {
	return Integrate(integCP, "b1", "feat: b1 work [b1]", "Build: b1")
}

// siblingMoves puts one commit on the container, ref-only, as a sibling's
// integrate would.
func siblingMoves(t *testing.T, file, content string) {
	t.Helper()
	tip := mustGit(t, "rev-parse", integCP)
	sib := filepath.Join(t.TempDir(), "sib")
	mustGit(t, "worktree", "add", "--detach", sib, tip)
	if err := os.WriteFile(filepath.Join(sib, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, "-C", sib, "add", ".")
	mustGit(t, "-C", sib, "commit", "-m", "sibling "+file)
	mustGit(t, "update-ref", "refs/heads/"+integCP, mustGit(t, "-C", sib, "rev-parse", "HEAD"), tip)
}

// postRewriteHook installs a post-rewrite hook (run after each rebase that
// rewrites commits): it moves the container under the integrate, between its
// rebase and its lock. once limits it to the first rebase.
func postRewriteHook(t *testing.T, once bool) {
	t.Helper()
	hooks := t.TempDir()
	guard := ""
	if once {
		mark := filepath.Join(t.TempDir(), "fired")
		guard = "[ -e " + mark + " ] && exit 0\ntouch " + mark + "\n"
	}
	script := "#!/bin/sh\n" + guard +
		"t=$(git rev-parse refs/heads/" + integCP + ")\n" +
		"c=$(git commit-tree \"$t^{tree}\" -p \"$t\" -m mover)\n" +
		"git update-ref refs/heads/" + integCP + " \"$c\" \"$t\"\n"
	if err := os.WriteFile(filepath.Join(hooks, "post-rewrite"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, "config", "core.hooksPath", hooks)
}

func wantIntegrateConflict(t *testing.T, err error, reason ConflictReason) {
	t.Helper()
	var c *IntegrateConflict
	if !errors.As(err, &c) || c.Reason != reason {
		t.Fatalf("Integrate err = %v, want *IntegrateConflict{Reason: %q}", err, reason)
	}
}

// The effort's commits land as exactly one squash on the container: subject
// as passed, the bead's trailer, the effort's tree, parented on the old
// tip. A re-run (the crash between CAS and close) finds the trailer and writes
// nothing, even though replaying the effort's commits onto the squash would
// conflict.
func TestIntegrateSquashesWithTrailerAndIsIdempotent(t *testing.T) {
	integrateRepo(t)
	before := mustGit(t, "rev-parse", integCP)
	commitFile(t, "work.txt", "one\n")
	commitFile(t, "work.txt", "two\n")
	siblingMoves(t, "sib.txt", "sib\n")
	sibTip := mustGit(t, "rev-parse", integCP)

	sha, retries, err := integrateB1()
	if err != nil || sha == "" || retries != 0 {
		t.Fatalf("Integrate = (%q, %d, %v), want a squash sha, 0 retries", sha, retries, err)
	}
	if tip := mustGit(t, "rev-parse", integCP); tip != sha {
		t.Errorf("container tip %s, want the squash %s", tip, sha)
	}
	if n := mustGit(t, "rev-list", "--count", before+".."+integCP); n != "2" {
		t.Errorf("container gained %s commits, want 2 (the sibling + one squash)", n)
	}
	if p := mustGit(t, "rev-parse", sha+"^"); p != sibTip {
		t.Errorf("squash parent %s, want the container tip %s", p, sibTip)
	}
	if msg := mustGit(t, "log", "-1", "--format=%B", sha); msg != "feat: b1 work [b1]\n\nBuild: b1" {
		t.Errorf("squash message = %q, want the passed subject, then the trailer \"Build: b1\"", msg)
	}
	for f, want := range map[string]string{"work.txt": "two", "sib.txt": "sib"} {
		if got := mustGit(t, "show", sha+":"+f); got != want {
			t.Errorf("container %s = %q, want %q", f, got, want)
		}
	}

	again, _, err := integrateB1()
	if err != nil || again != "" {
		t.Errorf("re-run Integrate = (%q, %v), want (\"\", nil): already integrated", again, err)
	}
	if tip := mustGit(t, "rev-parse", integCP); tip != sha {
		t.Errorf("re-run moved the container %s → %s", sha, tip)
	}
}

// A commit made after a bead's first integrate is not lost to the trailer: the
// re-run squashes the delta, and a further re-run writes nothing.
func TestIntegrateAgainCarriesLaterCommits(t *testing.T) {
	integrateRepo(t)
	commitFile(t, "work.txt", "one\n")
	commitFile(t, "work.txt", "two\n")
	first, _, err := integrateB1()
	if err != nil || first == "" {
		t.Fatalf("first Integrate = (%q, %v), want a squash", first, err)
	}
	siblingMoves(t, "sib.txt", "sib\n")
	commitFile(t, "work.txt", "three\n")

	sha, _, err := integrateB1()
	if err != nil || sha == "" {
		t.Fatalf("re-run Integrate = (%q, %v), want a second squash", sha, err)
	}
	for f, want := range map[string]string{"work.txt": "three", "sib.txt": "sib"} {
		if got := mustGit(t, "show", integCP+":"+f); got != want {
			t.Errorf("container %s = %q, want %q", f, got, want)
		}
	}
	if again, _, err := integrateB1(); err != nil || again != "" {
		t.Errorf("third Integrate = (%q, %v), want (\"\", nil)", again, err)
	}
}

// An effort recut from the container after its branch was deleted (a Surgery
// relaunch) integrates its new commits though the trailer is already there.
func TestIntegrateRecutEffortCarriesNewCommits(t *testing.T) {
	integrateRepo(t)
	commitFile(t, "work.txt", "one\n")
	if sha, _, err := integrateB1(); err != nil || sha == "" {
		t.Fatalf("first Integrate = (%q, %v), want a squash", sha, err)
	}
	siblingMoves(t, "sib.txt", "sib\n")
	mustGit(t, "checkout", "--detach")
	mustGit(t, "branch", "-D", EffortBranch("b1"))
	mustGit(t, "checkout", "-b", EffortBranch("b1"), integCP)
	commitFile(t, "work.txt", "two\n")

	if sha, _, err := integrateB1(); err != nil || sha == "" {
		t.Fatalf("recut Integrate = (%q, %v), want a squash", sha, err)
	}
	if got := mustGit(t, "show", integCP+":work.txt"); got != "two" {
		t.Errorf("container work.txt = %q, want %q", got, "two")
	}
}

// An effort with nothing ahead of the container integrates nothing: exit 0, no write.
func TestIntegrateNothingAheadWritesNothing(t *testing.T) {
	integrateRepo(t)
	siblingMoves(t, "sib.txt", "sib\n")
	before := mustGit(t, "rev-parse", integCP)

	sha, _, err := integrateB1()
	if err != nil || sha != "" {
		t.Fatalf("Integrate = (%q, %v), want (\"\", nil)", sha, err)
	}
	if tip := mustGit(t, "rev-parse", integCP); tip != before {
		t.Errorf("nothing-ahead integrate moved the container %s → %s", before, tip)
	}
}

// A container that moves between the rebase and the lock is re-read: the verb
// re-rebases and retries, and the sibling's commit survives under the squash.
func TestIntegrateRetriesWhenTheContainerMoves(t *testing.T) {
	integrateRepo(t)
	commitFile(t, "work.txt", "mine\n")
	siblingMoves(t, "sib.txt", "sib\n") // so the first rebase rewrites and fires the hook
	postRewriteHook(t, true)

	sha, retries, err := integrateB1()
	if err != nil || sha == "" || retries != 1 {
		t.Fatalf("Integrate = (%q, %d, %v), want a squash after 1 retry", sha, retries, err)
	}
	if subj := mustGit(t, "log", "-1", "--format=%s", sha+"^"); subj != "mover" {
		t.Errorf("squash parent %q, want the mover's commit kept under it", subj)
	}
	if got := mustGit(t, "show", integCP+":work.txt"); got != "mine" {
		t.Errorf("container work.txt = %q, want the effort's", got)
	}
}

// A container that moves on every attempt spends the 3 retries: transient
// ErrContended, and the effort's work never reaches the container.
func TestIntegrateSpentRetriesIsContended(t *testing.T) {
	integrateRepo(t)
	commitFile(t, "work.txt", "mine\n")
	siblingMoves(t, "sib.txt", "sib\n")
	postRewriteHook(t, false)

	_, retries, err := integrateB1()
	if !errors.Is(err, ErrContended) || retries != 3 {
		t.Fatalf("Integrate = (%d, %v), want ErrContended after 3 retries", retries, err)
	}
	if _, err := run("cat-file", "-e", integCP+":work.txt"); err == nil {
		t.Error("contended integrate still wrote the effort onto the container")
	}
}

// lockContainerRef leaves a stale ref lock on the container, so its update-ref
// fails while its tip stays put: a real ref failure, not a lost CAS.
func lockContainerRef(t *testing.T) {
	t.Helper()
	common := mustGit(t, "rev-parse", "--path-format=absolute", "--git-common-dir")
	writeFile(t, filepath.Join(common, "refs", "heads", integCP+".lock"), "")
}

// A ref that won't update for any reason but a moved tip (a stale lock, a
// permission error) surfaces as itself on the first attempt, never as spent
// retries (ErrContended).
func TestIntegrateRefFailureIsNotContention(t *testing.T) {
	integrateRepo(t)
	commitFile(t, "work.txt", "mine\n")
	lockContainerRef(t)

	_, retries, err := integrateB1()
	if err == nil || errors.Is(err, ErrContended) || retries != 0 {
		t.Fatalf("Integrate = (%d, %v), want the update-ref failure itself, no retry", retries, err)
	}
}

// A rebase conflict is left stopped in the tree for the agent to resolve; the
// container is untouched.
func TestIntegrateRebaseConflictStopsInTheTree(t *testing.T) {
	integrateRepo(t)
	commitFile(t, "README", "mine\n")
	siblingMoves(t, "README", "theirs\n")
	before := mustGit(t, "rev-parse", integCP)

	_, _, err := integrateB1()
	wantIntegrateConflict(t, err, "rebase")
	if !rebaseInProgress() {
		t.Error("rebase conflict was aborted; want it left stopped in the tree")
	}
	if tip := mustGit(t, "rev-parse", integCP); tip != before {
		t.Errorf("conflicted integrate moved the container %s → %s", before, tip)
	}
}

// A rebase left stopped in the tree (an agent that exited without --abort) still
// names its bead, and a re-run reads it as the same rebase conflict, never as
// nothing ahead or transient: the backstop pauses it.
func TestIntegrateReRunMidRebaseIsAConflict(t *testing.T) {
	integrateRepo(t)
	commitFile(t, "README", "mine\n")
	siblingMoves(t, "README", "theirs\n")
	if _, _, err := integrateB1(); err == nil {
		t.Fatal("want the rebase to conflict")
	}
	if id, err := CurrentEffort(); err != nil || id != "b1" {
		t.Errorf("CurrentEffort mid-rebase = (%q, %v), want b1", id, err)
	}
	before := mustGit(t, "rev-parse", integCP)

	_, _, err := integrateB1()
	wantIntegrateConflict(t, err, "rebase")
	if tip := mustGit(t, "rev-parse", integCP); tip != before {
		t.Errorf("re-run mid-rebase moved the container %s → %s", before, tip)
	}
}

// A worktree with the container checked out refuses the integrate (held): a CAS
// would strand that tree (C3).
func TestIntegrateRefusesAHeldContainer(t *testing.T) {
	integrateRepo(t)
	commitFile(t, "work.txt", "mine\n")
	mustGit(t, "worktree", "add", filepath.Join(t.TempDir(), "hand"), integCP)
	before := mustGit(t, "rev-parse", integCP)

	_, _, err := integrateB1()
	wantIntegrateConflict(t, err, "held")
	if tip := mustGit(t, "rev-parse", integCP); tip != before {
		t.Errorf("held integrate moved the container %s → %s", before, tip)
	}
}

// A container whose land trailer is already on main refuses the integrate
// (landed): the work would be stranded on a landed branch.
func TestIntegrateRefusesALandedContainer(t *testing.T) {
	root := integrateRepo(t)
	commitFile(t, "work.txt", "mine\n")
	mustGit(t, "-C", root, "commit", "--allow-empty", "-m", "land checkpoint/cp", "-m", "Checkpoint: cp")
	before := mustGit(t, "rev-parse", integCP)

	_, _, err := integrateB1()
	wantIntegrateConflict(t, err, "landed")
	if tip := mustGit(t, "rev-parse", integCP); tip != before {
		t.Errorf("landed integrate moved the container %s → %s", before, tip)
	}
}

// The verb reads its bead off the cwd's effort/<id> branch, and refuses any other.
func TestCurrentEffortReadsTheBranch(t *testing.T) {
	root := integrateRepo(t)
	if id, err := CurrentEffort(); err != nil || id != "b1" {
		t.Errorf("CurrentEffort on effort/b1 = (%q, %v), want b1", id, err)
	}
	t.Chdir(root)
	if id, err := CurrentEffort(); err == nil {
		t.Errorf("CurrentEffort on main = %q, want an error", id)
	}
}

// EffortRefs is the set of bead ids holding an effort/<id> branch, read in one
// for-each-ref (Spec D7); other branches never appear.
func TestEffortRefsListsEffortBranches(t *testing.T) {
	newRepo(t)
	if got, err := EffortRefs(); err != nil || len(got) != 0 {
		t.Fatalf("EffortRefs with none = %v, %v; want empty", got, err)
	}
	mustGit(t, "branch", EffortBranch("mc-a.1"))
	mustGit(t, "branch", EffortBranch("mc-a.2"))
	mustGit(t, "branch", "chart/mc-a")
	got, err := EffortRefs()
	if err != nil || len(got) != 2 || !got["mc-a.1"] || !got["mc-a.2"] {
		t.Errorf("EffortRefs = %v, %v; want {mc-a.1, mc-a.2}", got, err)
	}
}

// An effort tree is cut on effort/<id> at the container's tip when the branch is
// new, and reuses the branch, prior commits and all, when it already exists (Spec
// D2, FR 2–3): the roll forward after a failed or abandoned effort.
func TestNewEffortWorktreeCutsOrReuses(t *testing.T) {
	newRepo(t)
	mustGit(t, "branch", integCP)
	tip := mustGit(t, "rev-parse", integCP)

	wt, teardown, err := NewEffortWorktree("b1", integCP, "")
	if err != nil {
		t.Fatalf("fresh cut: %v", err)
	}
	if got := mustGit(t, "-C", wt, "symbolic-ref", "--short", "HEAD"); got != EffortBranch("b1") {
		t.Fatalf("fresh cut on %q, want %s", got, EffortBranch("b1"))
	}
	if got := mustGit(t, "-C", wt, "rev-parse", "HEAD"); got != tip {
		t.Fatalf("fresh cut at %s, want the container tip %s", got, tip)
	}
	if err := os.WriteFile(filepath.Join(wt, "work.txt"), []byte("w"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, "-C", wt, "add", ".")
	mustGit(t, "-C", wt, "commit", "-m", "prior run")
	prior := mustGit(t, "-C", wt, "rev-parse", "HEAD")
	teardown()

	siblingMoves(t, "sib.txt", "s") // the container moves on; the reuse must not recut
	wt, teardown, err = NewEffortWorktree("b1", integCP, "")
	if err != nil {
		t.Fatalf("reuse: %v", err)
	}
	defer teardown()
	if got := mustGit(t, "-C", wt, "rev-parse", "HEAD"); got != prior {
		t.Fatalf("reuse at %s, want the prior run's commit %s", got, prior)
	}
}

// A Build's whole effort round trip at the git seam (Spec D2, D3, D6): cut
// effort/<id> at the Checkpoint, commit twice, integrate from the tree, then the
// backstop's 0 path (teardown, delete the branch). The Checkpoint gains exactly one
// integrate commit and no effort ref is left.
func TestBuildEffortEndsAsOneIntegrateCommit(t *testing.T) {
	newRepo(t)
	mustGit(t, "branch", integCP)
	tip := mustGit(t, "rev-parse", integCP)
	wt, teardown, err := NewEffortWorktree("b1", integCP, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(wt, f), []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
		mustGit(t, "-C", wt, "add", ".")
		mustGit(t, "-C", wt, "commit", "-m", "work "+f)
	}
	root, _ := os.Getwd()
	t.Chdir(wt)
	if _, _, err := integrateB1(); err != nil {
		t.Fatalf("Integrate: %v", err)
	}
	t.Chdir(root)
	teardown()
	if err := DeleteBranch(EffortBranch("b1")); err != nil {
		t.Fatalf("delete effort branch: %v", err)
	}
	if got := mustGit(t, "log", "--format=%s", tip+".."+integCP); got != "feat: b1 work [b1]" {
		t.Errorf("container gained %q, want one \"feat: b1 work [b1]\" commit", got)
	}
	if refs, err := EffortRefs(); err != nil || len(refs) != 0 {
		t.Errorf("effort refs left = %v (%v), want none", refs, err)
	}
}

// A prototype's effort at the git seam (Spec FR 36): its ADR commit on
// effort/<id> integrates onto the Chart, while the throwaway, built in a detached
// tree and published to the side ref as sanji's prompt does, never reaches it.
func TestPrototypeEffortIntegratesADRNotThrowaway(t *testing.T) {
	newRepo(t)
	const chart, side = "chart/c", "refs/mugicrew/prototypes/c"
	mustGit(t, "branch", chart)
	wt, teardown, err := NewEffortWorktree("p1", chart, "")
	if err != nil {
		t.Fatal(err)
	}
	defer teardown()
	commitIn := func(dir, file string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, file), []byte(file), 0o644); err != nil {
			t.Fatal(err)
		}
		mustGit(t, "-C", dir, "add", ".")
		mustGit(t, "-C", dir, "commit", "-m", "add "+file)
	}
	commitIn(wt, "adr.md")
	scratch := filepath.Join(t.TempDir(), "proto")
	mustGit(t, "-C", wt, "worktree", "add", "--detach", scratch, chart)
	commitIn(scratch, "throwaway.go")
	mustGit(t, "-C", scratch, "update-ref", side, "HEAD")
	mustGit(t, "-C", wt, "worktree", "remove", "--force", scratch)
	t.Chdir(wt)
	if _, _, err := Integrate(chart, "p1", "docs: p1 [p1]", "Prototype: p1"); err != nil {
		t.Fatalf("Integrate: %v", err)
	}
	files := mustGit(t, "ls-tree", "--name-only", chart)
	if !strings.Contains(files, "adr.md") || strings.Contains(files, "throwaway.go") {
		t.Errorf("chart tree = %q, want adr.md and no throwaway.go", files)
	}
	if got := mustGit(t, "ls-tree", "--name-only", side); !strings.Contains(got, "throwaway.go") {
		t.Errorf("side ref tree = %q, want throwaway.go", got)
	}
}

// ---- replace mode (Spec D3, FR 13–14, ADR 0102 §4) ----

// replaceRepo is a history-rewriting leaf's scene: the container holds cp.txt at
// its pinned Tip, main moved on (main.txt), and the effort tree, cut at the pin,
// has rebased the container onto main, as a Conflict's resolve does. It returns
// the pin.
func replaceRepo(t *testing.T) (pin string) {
	t.Helper()
	root := integrateRepo(t)
	commitFile(t, "cp.txt", "cp\n")
	pin = mustGit(t, "rev-parse", "HEAD")
	mustGit(t, "update-ref", "refs/heads/"+integCP, pin)
	writeFile(t, filepath.Join(root, "main.txt"), "main\n")
	mustGit(t, "-C", root, "add", ".")
	mustGit(t, "-C", root, "commit", "-m", "main moves")
	mustGit(t, "rebase", "main")
	return pin
}

func replaceB1(pin string) (string, int, error) {
	return IntegrateReplace(integCP, pin)
}

// An unmoved pin: the container becomes the rewritten tip, no squash, and a re-run
// (the crash between CAS and close) writes nothing.
func TestIntegrateReplacePublishesTheRewrite(t *testing.T) {
	pin := replaceRepo(t)
	head := mustGit(t, "rev-parse", "HEAD")

	sha, _, err := replaceB1(pin)
	if err != nil || sha != head {
		t.Fatalf("IntegrateReplace = (%q, %v), want the rewritten tip %s", sha, err, head)
	}
	if tip := mustGit(t, "rev-parse", integCP); tip != head {
		t.Errorf("container tip %s, want the rewritten tip %s", tip, head)
	}
	if again, _, err := replaceB1(pin); err != nil || again != "" {
		t.Errorf("re-run = (%q, %v), want (\"\", nil): already integrated", again, err)
	}
}

// As for a squash, a replace whose update-ref fails without the tip moving
// surfaces the failure, never spent retries.
func TestIntegrateReplaceRefFailureIsNotContention(t *testing.T) {
	pin := replaceRepo(t)
	lockContainerRef(t)

	_, retries, err := replaceB1(pin)
	if err == nil || errors.Is(err, ErrContended) || retries != 0 {
		t.Fatalf("IntegrateReplace = (%d, %v), want the update-ref failure itself, no retry", retries, err)
	}
}

// A container moved past the pin: the sibling's commit is replayed on top of the
// rewrite, so it survives the replace.
func TestIntegrateReplaceKeepsSiblingWorkPastThePin(t *testing.T) {
	pin := replaceRepo(t)
	siblingMoves(t, "sib.txt", "sib\n")

	sha, _, err := replaceB1(pin)
	if err != nil || sha == "" {
		t.Fatalf("IntegrateReplace = (%q, %v), want the rewrite plus the replayed sibling", sha, err)
	}
	if tip := mustGit(t, "rev-parse", integCP); tip != sha {
		t.Errorf("container tip %s, want %s", tip, sha)
	}
	for f, want := range map[string]string{"main.txt": "main", "cp.txt": "cp", "sib.txt": "sib"} {
		if got := mustGit(t, "show", integCP+":"+f); got != want {
			t.Errorf("container %s = %q, want %q", f, got, want)
		}
	}
	if !IsAncestor("main", integCP) {
		t.Error("container does not sit on main: the rewrite was lost")
	}
	if again, _, err := replaceB1(pin); err != nil || again != "" {
		t.Errorf("re-run = (%q, %v), want (\"\", nil): already integrated", again, err)
	}
}

// A sibling commit that conflicts with the rewrite stops the replay in the tree as
// a rebase conflict; an abort leaves the effort branch at the rewrite.
func TestIntegrateReplaceReplayConflictStopsInTheTree(t *testing.T) {
	pin := replaceRepo(t)
	commitFile(t, "README", "mine\n")
	head := mustGit(t, "rev-parse", "HEAD")
	siblingMoves(t, "README", "theirs\n")
	before := mustGit(t, "rev-parse", integCP)

	_, _, err := replaceB1(pin)
	wantIntegrateConflict(t, err, "rebase")
	if !rebaseInProgress() {
		t.Fatal("replay conflict was aborted; want it left stopped in the tree")
	}
	if tip := mustGit(t, "rev-parse", integCP); tip != before {
		t.Errorf("conflicted replace moved the container %s → %s", before, tip)
	}
	_, _, err = replaceB1(pin)
	wantIntegrateConflict(t, err, "rebase") // a re-run mid-replay
	mustGit(t, "rebase", "--abort")
	if got := mustGit(t, "rev-parse", EffortBranch("b1")); got != head {
		t.Errorf("abort left effort/b1 at %s, want the rewrite %s", got, head)
	}
}

// After the agent resolves a replay conflict and continues, the re-run publishes
// the resolution without replaying the sibling a second time.
func TestIntegrateReplaceReRunAfterResolveDoesNotReplayAgain(t *testing.T) {
	pin := replaceRepo(t)
	commitFile(t, "README", "mine\n")
	siblingMoves(t, "README", "theirs\n")
	if _, _, err := replaceB1(pin); err == nil {
		t.Fatal("want the replay to conflict")
	}
	writeFile(t, "README", "both\n")
	mustGit(t, "add", "README")
	mustGit(t, "-c", "core.editor=true", "rebase", "--continue")

	sha, _, err := replaceB1(pin)
	if err != nil || sha == "" {
		t.Fatalf("re-run IntegrateReplace = (%q, %v), want the resolved rewrite", sha, err)
	}
	if got := mustGit(t, "show", integCP+":README"); got != "both" {
		t.Errorf("container README = %q, want the resolution %q", got, "both")
	}
}

// A resolution that only drops commits (a reset behind the pin) is a rewrite to
// publish, not nothing ahead: the container takes it.
func TestIntegrateReplacePublishesADropOnlyRewrite(t *testing.T) {
	pin := replaceRepo(t)
	mustGit(t, "reset", "--hard", pin+"^")
	head := mustGit(t, "rev-parse", "HEAD")

	if sha, _, err := replaceB1(pin); err != nil || sha != head {
		t.Fatalf("IntegrateReplace = (%q, %v), want the dropped-to %s", sha, err, head)
	}
	if tip := mustGit(t, "rev-parse", integCP); tip != head {
		t.Errorf("container tip %s, want %s", tip, head)
	}
	if again, _, err := replaceB1(pin); err != nil || again != "" {
		t.Errorf("re-run = (%q, %v), want (\"\", nil): already integrated", again, err)
	}
}

// A replace published but its bead left open: the backstop deleted
// effort/<id>, and the recut at the now-stale pin holds nothing past it, so the
// re-run is nothing ahead, not a "rewritten off its pinned tip" refusal.
func TestIntegrateReplaceRecutAtAStalePinIsNothingAhead(t *testing.T) {
	pin := replaceRepo(t)
	rewrite, _, err := replaceB1(pin)
	if err != nil || rewrite == "" {
		t.Fatalf("first replace = (%q, %v)", rewrite, err)
	}
	mustGit(t, "checkout", "-B", EffortBranch("b1"), pin)

	if sha, _, err := replaceB1(pin); err != nil || sha != "" {
		t.Fatalf("re-run at the pin = (%q, %v), want (\"\", nil): nothing ahead", sha, err)
	}
	if tip := mustGit(t, "rev-parse", integCP); tip != rewrite {
		t.Errorf("re-run moved the container %s → %s", rewrite, tip)
	}
}

// The replace-mode cut is at the pin while the container still descends from it,
// at the container tip once a replace rewrote it off the pin; work
// on that recut publishes without replaying the stale pin's history.
func TestEffortCutAfterAReplaceStartsAtTheContainer(t *testing.T) {
	pin := replaceRepo(t)
	siblingMoves(t, "sib.txt", "sib\n")
	wt, teardown, err := NewEffortWorktree("b2", integCP, pin)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, "-C", wt, "rev-parse", "HEAD"); got != pin {
		t.Errorf("cut before the replace at %s, want the pin %s", got, pin)
	}
	teardown()
	mustGit(t, "branch", "-D", EffortBranch("b2"))
	rewrite, _, err := replaceB1(pin)
	if err != nil || rewrite == "" {
		t.Fatalf("first replace = (%q, %v)", rewrite, err)
	}

	wt, teardown, err = NewEffortWorktree("b2", integCP, pin)
	if err != nil {
		t.Fatal(err)
	}
	defer teardown()
	if got := mustGit(t, "-C", wt, "rev-parse", "HEAD"); got != rewrite {
		t.Fatalf("recut at %s, want the container tip %s", got, rewrite)
	}
	t.Chdir(wt)
	commitFile(t, "more.txt", "more\n")
	head := mustGit(t, "rev-parse", "HEAD")
	if sha, _, err := IntegrateReplace(integCP, pin); err != nil || sha != head {
		t.Fatalf("IntegrateReplace on the recut = (%q, %v), want %s", sha, err, head)
	}
}

// raceHelperEnv marks a re-exec of this test binary as one racer of
// TestSiblingIntegratesRaceALand, which needs a process per tree: Integrate and
// Land run in the cwd.
const raceHelperEnv = "MUGICREW_GIT_RACE_HELPER"

// TestRaceHelper is the racer's body, a no-op unless re-exec'd with raceHelperEnv
// set to "land" or an effort id. It prints its outcome as one line.
func TestRaceHelper(t *testing.T) {
	who := os.Getenv(raceHelperEnv)
	if who == "" {
		t.Skip("re-exec'd racer only")
	}
	var sha string
	var err error
	if who == "land" {
		// Land once the first sibling has integrated (the tree is detached at the
		// pre-race tip), so the race has integrates on both sides of it.
		for start := mustGit(t, "rev-parse", "HEAD"); mustGit(t, "rev-parse", integCP) == start; {
			time.Sleep(5 * time.Millisecond)
		}
		sha, err = Land(integCP, "Checkpoint: cp", LandLocal)
	} else {
		sha, _, err = Integrate(integCP, who, "feat: "+who+" ["+who+"]", "Build: "+who)
	}
	var c *IntegrateConflict
	switch {
	case errors.As(err, &c):
		fmt.Println("conflict", c.Reason)
	case errors.Is(err, ErrContended):
		fmt.Println("contended")
	case err != nil:
		fmt.Println("error", err)
	default:
		fmt.Println("ok", sha)
	}
}

// N sibling integrates race a Land, all under the land lock (Spec Testing
// Decisions, the slimmed prototype race): every integrated effort's work
// is on the container and on main, so nothing integrated is lost or reverted; the
// container is never checked out (no held refusal, no holder after); and every
// effort that did not integrate keeps its branch and its work.
func TestSiblingIntegratesRaceALand(t *testing.T) {
	newRepo(t)
	root, _ := os.Getwd()
	mustGit(t, "branch", integCP)
	commitFile(t, "main.txt", "m\n") // main moves, so the Land really rebases
	const n = 6
	dirs := map[string]string{}
	for i := range n {
		id := fmt.Sprintf("b%d", i)
		wt, _, err := NewEffortWorktree(id, integCP, "")
		if err != nil {
			t.Fatal(err)
		}
		t.Chdir(wt)
		commitFile(t, id+".txt", id+"\n")
		dirs[id] = wt
	}
	t.Chdir(root)
	land, _, err := NewDetachedWorktree(integCP)
	if err != nil {
		t.Fatal(err)
	}
	dirs["land"] = land

	out := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for who, dir := range dirs {
		wg.Go(func() {
			cmd := exec.Command(os.Args[0], "-test.run=^TestRaceHelper$")
			cmd.Dir, cmd.Env = dir, append(os.Environ(), raceHelperEnv+"="+who)
			b, err := cmd.Output()
			mu.Lock()
			defer mu.Unlock()
			out[who] = strings.TrimSpace(strings.Split(string(b), "\n")[0])
			if err != nil {
				out[who] = "error " + err.Error()
			}
		})
	}
	wg.Wait()

	if !strings.HasPrefix(out["land"], "ok ") {
		t.Fatalf("Land = %q, want ok", out["land"])
	}
	for i := range n {
		id := fmt.Sprintf("b%d", i)
		switch o := out[id]; {
		case strings.HasPrefix(o, "ok "):
			for _, ref := range []string{integCP, Mainline()} {
				if got, err := run("show", ref+":"+id+".txt"); err != nil || got != id {
					t.Errorf("%s integrated (%s) but %s:%s.txt = %q, %v", id, o, ref, id, got, err)
				}
			}
		case o == "conflict landed" || o == "contended":
			if got, err := run("show", EffortBranch(id)+":"+id+".txt"); err != nil || got != id {
				t.Errorf("%s did not integrate (%s) and lost its work: %q, %v", id, o, got, err)
			}
		default:
			t.Errorf("%s: %q, want ok, conflict landed or contended", id, o)
		}
	}
	if h, ok, _ := WorktreeOn(integCP); ok {
		t.Errorf("the container is checked out in %s", h.Path)
	}
}

// Under a TMPDIR that runs through a symlink (macOS /var, /tmp), a tree is minted in
// the resolved spelling, so it equals what git's porcelain reports for its branch,
// and TempWorktreePath names the same resolved parent.
func TestTempTreesAreBornSymlinkResolved(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatal(err)
	}
	newRepo(t)
	t.Setenv("TMPDIR", link)
	mustGit(t, "branch", integCP)

	wt, teardown, err := NewEffortWorktree("sym", integCP, "")
	if err != nil {
		t.Fatal(err)
	}
	defer teardown()
	h, ok, err := WorktreeOn(EffortBranch("sym"))
	if err != nil || !ok || h.Path != wt {
		t.Errorf("git lists %q (ok %v, err %v), NewEffortWorktree returned %q", h.Path, ok, err, wt)
	}
	if got := filepath.Dir(filepath.Dir(TempWorktreePath("k"))); got != resolved {
		t.Errorf("TempWorktreePath under %q, want the resolved %q", got, resolved)
	}
}

// ---- cut-point replay (ADR 0264): squash integrate replays from the cut ref ----

// cutRepo is integrateRepo with the container holding one commit a.txt="old" and
// the effort cut by NewEffortWorktree, so refs/mugicrew/cut/b1 exists; cwd is its tree.
func cutRepo(t *testing.T) (oldA string) {
	oldA, _, _ = cutRepoAt(t)
	return oldA
}

// cutRepoAt is cutRepo that also returns the main tree and the effort tree.
func cutRepoAt(t *testing.T) (oldA, root, wt string) {
	t.Helper()
	newRepo(t)
	root, _ = os.Getwd()
	mustGit(t, "branch", integCP)
	siblingMoves(t, "a.txt", "old\n")
	oldA = mustGit(t, "rev-parse", integCP)
	wt, teardown, err := NewEffortWorktree("b1", integCP, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(teardown)
	t.Chdir(wt)
	return oldA, root, wt
}

// rewriteContainer replaces the container's top commit with one rewriting a.txt to
// content ("" drops it), as an IntegrateReplace publish would.
func rewriteContainer(t *testing.T, content string) {
	t.Helper()
	old := mustGit(t, "rev-parse", integCP)
	parent := mustGit(t, "rev-parse", old+"^")
	if content == "" {
		mustGit(t, "update-ref", "refs/heads/"+integCP, parent, old)
		return
	}
	sib := filepath.Join(t.TempDir(), "rw")
	mustGit(t, "worktree", "add", "--detach", sib, parent)
	if err := os.WriteFile(filepath.Join(sib, "a.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, "-C", sib, "add", ".")
	mustGit(t, "-C", sib, "commit", "-m", "rewritten a")
	mustGit(t, "update-ref", "refs/heads/"+integCP, mustGit(t, "-C", sib, "rev-parse", "HEAD"), old)
}

func TestCutRefWrittenAtTheCutAndKeptOnReuse(t *testing.T) {
	oldA, root, wt := cutRepoAt(t)
	if got := mustGit(t, "rev-parse", cutRefPrefix+"b1"); got != oldA {
		t.Fatalf("cut ref %s, want the container tip %s", got, oldA)
	}
	commitFile(t, "own.txt", "own\n") // own work keeps the branch, and its cut ref, as is
	t.Chdir(root)
	mustGit(t, "worktree", "remove", "--force", wt)
	siblingMoves(t, "s.txt", "s\n")
	_, teardown, err := NewEffortWorktree("b1", integCP, "") // reuses the branch
	if err != nil {
		t.Fatal(err)
	}
	defer teardown()
	if got := mustGit(t, "rev-parse", cutRefPrefix+"b1"); got != oldA {
		t.Errorf("reused branch's cut ref %s, want it kept at %s", got, oldA)
	}
}

func TestIntegrateAfterContainerRewriteIgnoresChangedOldCommit(t *testing.T) {
	cutRepo(t)
	commitFile(t, "w.txt", "w\n")
	rewriteContainer(t, "new\n")

	sha, _, err := integrateB1()
	if err != nil || sha == "" {
		t.Fatalf("Integrate = (%q, %v), want a clean squash", sha, err)
	}
	if got := mustGit(t, "show", integCP+":a.txt"); got != "new" {
		t.Errorf("a.txt = %q, want the rewritten container's", got)
	}
	if got := mustGit(t, "show", integCP+":w.txt"); got != "w" {
		t.Errorf("w.txt = %q, want the effort's work", got)
	}
}

func TestIntegrateAfterContainerRewriteKeepsADroppedCommitDropped(t *testing.T) {
	cutRepo(t)
	commitFile(t, "w.txt", "w\n")
	rewriteContainer(t, "")

	if sha, _, err := integrateB1(); err != nil || sha == "" {
		t.Fatalf("Integrate = (%q, %v), want a squash", sha, err)
	}
	if _, err := run("cat-file", "-e", integCP+":a.txt"); err == nil {
		t.Error("dropped commit's a.txt came back on the container")
	}
}

func TestIntegrateTwiceReplaysOnlyNewCommitsAndMovesTheCutRef(t *testing.T) {
	cutRepo(t)
	commitFile(t, "w.txt", "one\n")
	if sha, _, err := integrateB1(); err != nil || sha == "" {
		t.Fatalf("first Integrate = (%q, %v)", sha, err)
	}
	if got, head := mustGit(t, "rev-parse", cutRefPrefix+"b1"), mustGit(t, "rev-parse", "HEAD"); got != head {
		t.Errorf("cut ref %s, want the integrated HEAD %s", got, head)
	}
	if sha, _, err := integrateB1(); err != nil || sha != "" {
		t.Fatalf("re-run = (%q, %v), want nothing", sha, err)
	}
	commitFile(t, "w.txt", "two\n")
	commitFile(t, "v.txt", "v\n")
	if sha, _, err := integrateB1(); err != nil || sha == "" {
		t.Fatalf("second Integrate = (%q, %v)", sha, err)
	}
	if got := mustGit(t, "show", integCP+":w.txt"); got != "two" {
		t.Errorf("w.txt = %q, want two", got)
	}
	if sha, _, err := integrateB1(); err != nil || sha != "" {
		t.Fatalf("final re-run = (%q, %v), want nothing", sha, err)
	}
}

func TestLostCASLeavesTheCutRefUnmoved(t *testing.T) {
	oldA := cutRepo(t)
	commitFile(t, "w.txt", "w\n")
	siblingMoves(t, "s.txt", "s\n") // so the rebase rewrites and the hook fires
	postRewriteHook(t, false)

	if _, _, err := integrateB1(); !errors.Is(err, ErrContended) {
		t.Fatalf("Integrate err = %v, want ErrContended", err)
	}
	if got := mustGit(t, "rev-parse", cutRefPrefix+"b1"); got != oldA {
		t.Errorf("cut ref moved to %s on a lost CAS, want %s", got, oldA)
	}
}

// An attempt that rebased but did not publish (here a held refusal) leaves the
// effort where it was, so a later integrate replays only its own work and not the
// container commits the first rebase already carried.
func TestIntegrateAfterAnUnpublishedAttemptReplaysOnlyTheEffortsWork(t *testing.T) {
	cutRepo(t)
	commitFile(t, "w.txt", "w\n")
	siblingMoves(t, "a.txt", "x\n")
	hand := filepath.Join(t.TempDir(), "hand")
	mustGit(t, "worktree", "add", hand, integCP)
	_, _, err := integrateB1()
	wantIntegrateConflict(t, err, "held")
	mustGit(t, "worktree", "remove", "--force", hand)
	siblingMoves(t, "a.txt", "y\n")

	sha, _, err := integrateB1()
	if err != nil || sha == "" {
		t.Fatalf("re-Integrate = (%q, %v), want a clean squash", sha, err)
	}
	if got := mustGit(t, "show", integCP+":a.txt"); got != "y" {
		t.Errorf("container a.txt = %q, want the sibling's y kept", got)
	}
	if got := mustGit(t, "show", integCP+":w.txt"); got != "w" {
		t.Errorf("container w.txt = %q, want the effort's", got)
	}
}

func TestDeleteBranchDeletesTheCutRef(t *testing.T) {
	_, root, wt := cutRepoAt(t)
	t.Chdir(root)
	mustGit(t, "worktree", "remove", "--force", wt)
	if err := DeleteBranch(EffortBranch("b1")); err != nil {
		t.Fatal(err)
	}
	if _, err := run("rev-parse", "--verify", "--quiet", cutRefPrefix+"b1"); err == nil {
		t.Error("cut ref outlived its branch")
	}
}

func TestIntegrateWithoutCutRefFallsBackToForkPoint(t *testing.T) {
	cutRepo(t)
	mustGit(t, "update-ref", "-d", cutRefPrefix+"b1") // a branch cut before cut refs
	commitFile(t, "w.txt", "w\n")
	siblingMoves(t, "s.txt", "s\n")

	sha, _, err := integrateB1()
	if err != nil || sha == "" {
		t.Fatalf("Integrate = (%q, %v), want a squash via --fork-point", sha, err)
	}
	if got := mustGit(t, "show", integCP+":w.txt"); got != "w" {
		t.Errorf("w.txt = %q, want the effort's work", got)
	}
	if _, err := run("rev-parse", "--verify", "--quiet", cutRefPrefix+"b1"); err != nil {
		t.Error("fallback path left no cut ref after a won CAS")
	}
}

func TestIntegrateWithoutCutRefAfterRewriteRefusesNoCutPoint(t *testing.T) {
	cutRepo(t)
	mustGit(t, "update-ref", "-d", cutRefPrefix+"b1")
	commitFile(t, "w.txt", "w\n")
	// an unrelated container history with its reflog gone: no fork point survives
	orphan := mustGit(t, "commit-tree", mustGit(t, "hash-object", "-t", "tree", "/dev/null"), "-m", "rewritten")
	mustGit(t, "update-ref", "refs/heads/"+integCP, orphan)
	mustGit(t, "reflog", "expire", "--expire=now", "--all")

	sha, _, err := integrateB1()
	if !errors.Is(err, ErrNoCutPoint) || sha != "" {
		t.Fatalf("Integrate = (%q, %v), want ErrNoCutPoint", sha, err)
	}
	if got := mustGit(t, "rev-parse", integCP); got != orphan {
		t.Errorf("container moved to %s, want it untouched at %s", got, orphan)
	}
}

// CommitsSinceCut counts an effort's own commits from its recorded cut point, and
// reports ok false when there is no cut ref.
func TestCommitsSinceCut(t *testing.T) {
	newRepo(t)
	mustGit(t, "branch", "effort/b1")
	if _, ok, err := Repo("").CommitsSinceCut("b1", "effort/b1"); ok || err != nil {
		t.Fatalf("no cut ref: ok = %v, err = %v", ok, err)
	}
	if err := recordCut(Repo(""), "b1", "effort/b1"); err != nil {
		t.Fatal(err)
	}
	mustGit(t, "checkout", "-q", "effort/b1")
	mustGit(t, "commit", "-q", "--allow-empty", "-m", "one")
	mustGit(t, "commit", "-q", "--allow-empty", "-m", "two")
	if n, ok, err := (Repo("")).CommitsSinceCut("b1", "effort/b1"); n != 2 || !ok || err != nil {
		t.Errorf("CommitsSinceCut = %d, %v, %v; want 2, true, nil", n, ok, err)
	}
}

func TestIntegrateSubject(t *testing.T) {
	cases := []struct{ name, kind, title, context, want string }{
		{"survey", "survey", "Map it", "app", "docs(app): Map it [b1]"},
		{"grilling", "grilling", "Q", "app", "docs(app): Q [b1]"},
		{"research", "research", "R", "app", "docs(app): R [b1]"},
		{"prototype", "prototype", "P", "app", "docs(app): P [b1]"},
		{"build", "build", "Add It", "app", "feat(app): Add It [b1]"},
		{"critique", "critique", "Bad thing.", "app", "fix(app): Bad thing. [b1]"},
		{"surgery", "surgery", "Fix", "app", "fix(app): Fix [b1]"},
		{"other kind", "widget", "W", "app", "chore(app): W [b1]"},
		{"no context", "build", "Add", "", "feat: Add [b1]"},
		{"empty title", "build", "", "app", "integrate b1"},
		{"blank title", "build", " \t", "", "integrate b1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IntegrateSubject(c.kind, "b1", c.title, c.context); got != c.want {
				t.Errorf("IntegrateSubject = %q, want %q", got, c.want)
			}
		})
	}
}
