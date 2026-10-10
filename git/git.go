// Package git is the single shell-out choke point for the git-branch layer
// (ADR 0009/0010). Every git footgun is encapsulated in the private run(...),
// never at a call site, and the argv is split into pure helpers that are
// checkable without a real repo — mirroring bd/'s newSignalArgs /
// successorCreateArgs pattern.
//
// The core ops are CutBranch (idempotent branch-if-absent),
// Land (rebase+squash onto mainline, git-native idempotent), and LandedBase
// (re-derive a landed commit by its trailer), Anchor (the empty direct-Spec
// trailer commit, ADR 0035), Revert (the forward Rollback revert, ADR 0047),
// plus Land's in-lock ADR-number clash check (ADR 0030), *ADRClash. Every move of
// main (and Reapply) runs under the one land lock, and a dirty main is *MainDirty
// (ADR 0069). CheckResolved/RestoreResolved are the post-resolve check on a
// resolved Chart branch and its undo, *ResolveCheckFailed (ADR 0080).
package git

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// MainlineOf is the trunk of the repo whose git dir is gitDir (Spec
// FR11–12): origin/HEAD's branch with origin/ stripped, else main. Never current
// HEAD, an unreliable trunk pointer (ADR 0010 §3), and no setting.
func MainlineOf(gitDir string) string {
	ref, err := run("--git-dir", gitDir, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if b, ok := strings.CutPrefix(ref, "origin/"); err == nil && ok && b != "" {
		return b
	}
	return "main"
}

// mainlines caches MainlineOf per common dir, so each Workspace resolves once per
// process (a test process visits many scratch repos).
var mainlines sync.Map

// Mainline is the Workspace's trunk, the single pointer both the merge target and
// the cut base read (ADR 0010 §3): MainlineOf the cwd's common dir, resolved once.
func Mainline() string {
	dir, err := CommonDir()
	if err != nil {
		return "main"
	}
	if m, ok := mainlines.Load(dir); ok {
		return m.(string)
	}
	m, _ := mainlines.LoadOrStore(dir, MainlineOf(dir))
	return m.(string)
}

// RebaseConflict is Land's typed failure: the rebase onto mainline conflicted.
// It carries the target base SHA the branch must be rebased onto (mirroring the
// conflict bead's payload, ADR 0004 §4). git/ has already aborted the rebase and
// touched no beads — what a caller does with the conflict is its own policy
// (ADR 0010 consequences).
type RebaseConflict struct {
	Base string // mainline SHA to rebase onto
}

func (e *RebaseConflict) Error() string {
	return fmt.Sprintf("git: rebase conflict landing onto %s", e.Base)
}

// RevertConflict is Revert's typed failure (ADR 0049): reverting the squash
// conflicts with later lands. It carries the mainline SHA the revert was tried on
// (the fix-forward successor's cut base) and the conflicted paths, mirroring
// RebaseConflict.Base. Nothing was written to mainline.
type RevertConflict struct {
	Base  string   // mainline SHA the revert was tried on
	Paths []string // paths the revert conflicted on, sorted
}

func (e *RevertConflict) Error() string {
	return fmt.Sprintf("git: revert conflict onto %s in %s", e.Base, strings.Join(e.Paths, ", "))
}

// MainDirty is the typed refusal of every move of main (ADR 0069): the worktree
// holding main has a tracked change or a non-ignored untracked file. Nothing was
// touched; a human cleans main, then re-runs. It is raised on the write leg only,
// so an already-landed trailer still returns its SHA over a dirty main.
type MainDirty struct {
	Path string // the worktree that has main checked out
}

func (e *MainDirty) Error() string {
	return fmt.Sprintf("git: main worktree %s is dirty; clean main, then re-run", e.Path)
}

// ADRClash is Land's typed refusal (ADR 0030, ADR 0069): the branch adds an ADR
// number the locked base already holds under another filename — it rebases
// cleanly, so the rebase can't catch it. It carries the clashes (as adrClashes
// formats them) and the base checked against. Nothing was touched.
type ADRClash struct {
	Clashes []string
	Base    string // mainline SHA the clash check read
}

func (e *ADRClash) Error() string {
	return fmt.Sprintf("git: ADR-number clash landing onto %s: %s", e.Base, strings.Join(e.Clashes, "; "))
}

// mainClean is the write leg's guard: a *MainDirty if a worktree has main checked
// out with any tracked change or non-ignored untracked file (status --porcelain's
// default), nil if main is clean or checked out nowhere.
func mainClean() error {
	h, ok, err := WorktreeOn(Mainline())
	if err != nil || !ok {
		return err
	}
	if dirty, err := Dirty(h.Path); err != nil || !dirty {
		return err
	}
	return &MainDirty{Path: h.Path}
}

// Dirty reports whether the worktree at dir has any tracked change or
// non-ignored untracked file (status --porcelain's default).
func Dirty(dir string) (bool, error) {
	st, err := run("-C", dir, "status", "--porcelain")
	return st != "", err
}

// landLockTimeout bounds the wait for land.lock (ADR 0069): a hung hook can't wedge
// dispatch forever, and the next tick retries. A var so tests inject a short one.
var landLockTimeout = 60 * time.Second

// withLandLock runs fn under the blocking flock on <git-common-dir>/mugicrew/land.lock
// (beside events.jsonl), so two mugicrew moves of main never interleave, whichever
// process (loop, CLI, TUI's reconcile child) makes them. A wait past landLockTimeout
// is an untyped (transient) error. The fd is close-on-exec, so no git child keeps
// the lock; closing it releases the lock. Never nest it: each call opens its own
// fd, so a nested call waits on its own caller until the timeout.
//
// ponytail: LOCK_NB polled every 50ms up to the deadline, since a blocking flock
// can't be timed out without a signal; a waiter can lose to a newcomer, which only
// costs it time.
func withLandLock(fn func() (string, error)) (string, error) {
	dir, err := CommonDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "mugicrew", "land.lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	deadline := time.Now().Add(landLockTimeout)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EINTR {
			return "", fmt.Errorf("git: land lock %s: %w", path, err)
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("git: land lock %s still held after %s", path, landLockTimeout)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fn()
}

// WithLandLock runs fn under the land lock for a non-git caller: the Chart-body
// edits of `mugicrew chart append|drop` (ADR 0103 §4). Never nest it.
func WithLandLock(fn func() error) error {
	_, err := withLandLock(func() (string, error) { return "", fn() })
	return err
}

// conflictPaths parses merge-tree --write-tree --name-only's conflicted output: the
// lines between the tree OID and the first blank line, sorted and deduplicated.
func conflictPaths(out string) []string {
	lines := strings.Split(out, "\n")
	var paths []string
	for _, l := range lines[1:] {
		if l == "" {
			break
		}
		paths = append(paths, l)
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}

// run shells out to git and returns trimmed stdout, or "" on failure (mirrors
// bd.run). It is runOut with the failure output dropped.
func run(args ...string) (string, error) {
	out, err := runOut(args...)
	if err != nil {
		return "", err
	}
	return out, nil
}

// runOut is the one git shell-out every call routes through (directly, or via run),
// so the footguns are encapsulated in one place. It keeps stdout on failure, for the
// one call whose failure output is data (merge-tree's conflicted paths, Revert). The
// error still wraps the *exec.ExitError, so a caller can read the exit code.
func runOut(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // a loop's Ctrl-C drains; it must not kill an effort's git step (ADR 0050)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			err = fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(ee.Stderr)))
		} else {
			err = fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
	}
	return strings.TrimSpace(string(out)), err
}

// worktreeAddArgs is the pure argv that checks a branch out into a linked
// worktree at path (ADR 0009/0010): `git worktree add <path> <branch>`. A linked
// worktree has its OWN HEAD, index, and working files but shares the repo's
// object/ref store, so an effort's checkout+rebase+commit never touch the shared
// working tree — the per-effort isolation this layer's concurrent land needs.
func worktreeAddArgs(path, branch string) []string {
	return []string{"worktree", "add", path, branch}
}

// worktreeRemoveArgs is the pure argv that tears a per-effort worktree down:
// `git worktree remove --force <path>`. --force drops it even with a dirty tree
// left by a crashed effort, so teardown is unconditional.
func worktreeRemoveArgs(path string) []string {
	return []string{"worktree", "remove", "--force", path}
}

// AddWorktree checks branch out into a linked worktree at path so an effort's
// git working-tree ops run isolated from the shared tree (ADR 0009/0010). The
// caller (the loop's dispatch) then runs that effort with path as its cwd — the
// ADR 0010 "worktree is the caller's cwd" seam — so git/'s three land/branch
// signatures stay dir-less.
func AddWorktree(path, branch string) error {
	_, err := run(worktreeAddArgs(path, branch)...)
	return err
}

// RemoveWorktree tears a per-effort worktree down (best-effort teardown after the
// effort finishes or crashes).
func RemoveWorktree(path string) error {
	_, err := run(worktreeRemoveArgs(path)...)
	return err
}

// NewWorktree cuts a fresh linked worktree checked out to branch under a private
// temp dir and returns its path plus an unconditional teardown (RemoveWorktree's
// `worktree remove --force` drops a dirty/crashed tree, then the temp dir goes).
// The shared setup/teardown both HITL entry points wrap their launched session in,
// the same create-and-remove cycle the AFK loop's worktreeRun runs.
// A var so the placement decision is unit-testable without a real repo.
var NewWorktree = func(branch string) (path string, teardown func(), err error) {
	return newTree(func(wt string) error { return AddWorktree(wt, branch) })
}

// NewDetachedWorktree is NewWorktree with HEAD detached at ref's commit (`worktree
// add --detach`): the tree a land step runs in, holding no branch, so Land never
// writes its container (Spec D2, ADR 0102 §8). A var, as NewWorktree.
var NewDetachedWorktree = func(ref string) (path string, teardown func(), err error) {
	return newTree(func(wt string) error { _, err := run("worktree", "add", "--detach", wt, ref); return err })
}

// NewEffortWorktree is NewWorktree on bead id's effort/<id>, cut or reused (Spec
// D2, ADR 0105): an existing branch is checked out as is, so a re-dispatch
// rolls forward from a prior run's commits; else it is created at container's tip,
// or at pin, a replace-mode leaf's pinned Tip, while the container still descends
// from it. A container rewritten off the pin already holds an earlier replace
// (published, its bead left open), so the recut starts from it.
// A var, as NewWorktree.
var NewEffortWorktree = func(id, container, pin string) (path string, teardown func(), err error) {
	branch := EffortBranch(id)
	return newTree(func(wt string) error {
		if _, err := run("rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
			return AddWorktree(wt, branch)
		}
		at := container
		if pin != "" && IsAncestor(pin, container) {
			at = pin
		}
		_, err := run("worktree", "add", "-b", branch, wt, at)
		return err
	})
}

// newTree is NewWorktree's temp-parent cycle around add.
func newTree(add func(wt string) error) (path string, teardown func(), err error) {
	parent, err := os.MkdirTemp("", tempWorktreePrefix)
	if err != nil {
		return "", nil, err
	}
	wt := filepath.Join(parent, "wt")
	if err := add(wt); err != nil {
		os.RemoveAll(parent)
		return "", nil, err
	}
	return wt, func() { RemoveWorktree(wt); os.RemoveAll(parent) }, nil
}

// TempWorktreePath is the temp-shaped path, mugicrew-wt-<key>/wt, for a tree keyed
// on key: IsTempWorktree's shape, so a tree cut there reads as mugicrew's. Pure — it
// creates nothing (`git worktree add` makes the parents) — so a prompt preview can
// name it on every render. A prototype prompt is handed one for its detached
// throwaway (ADR 0071).
func TempWorktreePath(key string) string {
	return filepath.Join(os.TempDir(), tempWorktreePrefix+key, "wt")
}

// tempWorktreePrefix names NewWorktree's private temp parent dirs.
const tempWorktreePrefix = "mugicrew-wt-"

// IsTempWorktree reports whether wt has NewWorktree's shape, mugicrew-wt-*/wt: a
// tree mugicrew made, whose filepath.Dir is its own temp parent to remove with it.
func IsTempWorktree(wt string) bool {
	return filepath.Base(wt) == "wt" && strings.HasPrefix(filepath.Base(filepath.Dir(wt)), tempWorktreePrefix)
}

// Busy reports whether err, from cutting a worktree on branch, is git's refusal to
// check the branch out a second time, and names the holder (ADR 0056). That
// refusal IS the v1 contention interlock: git enforces one checkout per
// branch across the whole container, so a second effort on a live branch (AFK loop
// vs. HITL launch, or two of either) fails fast here instead of racing on a shared
// index. Both worktree-add call sites key "busy" on this so the loop skips and the
// TUI shows a status instead of crashing.
//
// Busy is exit 128 AND a holder listed by the porcelain worktree list. Exit 128 is
// git's generic fatal, so an exit 128 with no listed holder (bad ref, permissions,
// disk, or a holder stopped mid-rebase/bisect) is not busy: the caller fails it
// with git's own error, which still names any holder for the human. It never
// parses the fatal: string — that wording is git-version-dependent and
// translated. run() wraps the *exec.ExitError with %w, so errors.As reaches it.
// A failed list is not busy either: the cut error surfaces rather than a skip.
//
// ponytail: one effort per checked-out branch, enforced by git's own checkout lock
// — no lock or queue of our own. Every committing effort cuts its own effort/<id>,
// so this only guards an effort branch against a second tree (a human's, a stale
// one); siblings in one container never contend here, and their CAS-integrates
// are serialized by the land lock plus Land's pin check (ADR 0102). A land step's
// detached tree is never busy.
func Busy(err error, branch string) (Holder, bool) {
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 128 {
		return Holder{}, false
	}
	h, ok, lerr := WorktreeOn(branch)
	return h, ok && lerr == nil
}

// RepoRoot is the Workspace root: the main worktree's top-level dir, the same from
// the root, any subdirectory and any linked worktree (Spec FR3) — the first
// stanza of `git worktree list --porcelain`. It is the cwd a leaf session (signal
// triage, spec slicing) and the loop run from, so they operate on the shared
// checkout rather than the launcher's arbitrary cwd. A var so a leaf path's cwd
// seam is testable without a real repo.
// ponytail: a bare common dir's first stanza is the bare dir itself; handle it if a
// bare-repo Workspace ever appears.
var RepoRoot = func() (string, error) {
	out, err := run(worktreeListArgs()...)
	if err != nil {
		return "", err
	}
	first, _, _ := strings.Cut(out, "\n")
	root, ok := strings.CutPrefix(first, "worktree ")
	if !ok {
		return "", fmt.Errorf("git worktree list: no worktree in %q", first)
	}
	return root, nil
}

// CommonDir is the absolute git-common-dir: the main repo's .git from every
// linked worktree, so per-repo state there is shared by all of them (ADR 0027).
func CommonDir() (string, error) {
	return run("rev-parse", "--path-format=absolute", "--git-common-dir")
}

// CommonDirOf is the absolute git common dir of the repo containing dir: the
// source repo's, from its stamped root (ADR 0090 §2).
func CommonDirOf(dir string) (string, error) {
	return run("-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
}

// TopLevelOf is the root of the worktree containing dir.
func TopLevelOf(dir string) (string, error) {
	return run("-C", dir, "rev-parse", "--show-toplevel")
}

// MainlineAt is mainline's full SHA in the repo whose git dir is gitDir: pass the
// common dir, so every linked worktree resolves the shared mainline, never its own
// HEAD (ADR 0060 §5).
func MainlineAt(gitDir string) (string, error) {
	return run("--git-dir", gitDir, "rev-parse", "--verify", MainlineOf(gitDir)+"^{commit}")
}

// UserName is `git config user.name` — the middle rung of bd's actor resolution
// (ADR 0052 §2). An unset name is an error, which the caller treats as "fall through".
func UserName() (string, error) { return run("config", "user.name") }

// Head is a branch's live tip SHA (`git rev-parse <branch>`) — the commit a
// Checkpoint's approval is pinned against. A var so ApprovedAtHead's
// pin-vs-tip compare is testable without a real repo, mirroring RepoRoot.
var Head = func(branch string) (string, error) { return run("rev-parse", branch) }

// BranchName reconstructs a container's branch name from its kind and bead id
// (ADR 0010 §3): chart/<id>, checkpoint/<id>. Never stored — always rebuilt.
func BranchName(kind, id string) string { return kind + "/" + id }

// ParseBranchName is BranchName's inverse: a container branch's kind and bead id.
func ParseBranchName(branch string) (kind, id string) {
	kind, id, _ = strings.Cut(branch, "/")
	return kind, id
}

// IsAncestor reports whether commit a is an ancestor of (or equal to) b.
func IsAncestor(a, b string) bool {
	_, err := run("merge-base", "--is-ancestor", a, b)
	return err == nil
}

// DeleteBranch force-deletes a local branch (`git branch -D`); git refuses one a
// worktree still has checked out.
func DeleteBranch(name string) error {
	_, err := run("branch", "-D", name)
	return err
}

// AbortRebase runs `git rebase --abort` in the tree at dir: the backstop's undo
// of an integrate's stopped rebase (Spec D6). No rebase there is an error.
func AbortRebase(dir string) error {
	_, err := run("-C", dir, "rebase", "--abort")
	return err
}

// TrailerFor builds the land trailer that stamps the squash commit and that
// LandedBase greps back (ADR 0010 §2): "Chart: <id>", "Checkpoint: <id>". The
// key is the container kind title-cased here (chart -> Chart), so the caller
// passes the same kind constant BranchName takes and the two can't drift.
// Exported so a landing caller (reconcileChart) stamps the same trailer
// LandedBase greps, without re-implementing the format.
func TrailerFor(kind, id string) string {
	return strings.ToUpper(kind[:1]) + kind[1:] + ": " + id
}

// CommitPin is the body line a build/approval bead stamps to pin the commit it
// was cut against: "Commit: <sha>" — the bead-body twin of TrailerFor's
// commit-message trailer, ParseCommit greps it back. Pure, checkable without git.
func CommitPin(sha string) string { return "Commit: " + sha }

// BasePin is the body line a conflict bead stamps with the base SHA the tail must
// rebase onto: "Base: <sha>" — the writer twin of ParseBase, kept next
// to it so the two can't drift (mirrors CommitPin/ParseCommit). Jinbe stamps it on
// the kind:conflict member he raises; Franky's runConflict greps it back. Pure.
func BasePin(sha string) string { return "Base: " + sha }

// TipPin is the body line a chart-conflict bead stamps with the Chart branch tip
// the failed Land saw (D4), and a land squash with the container tip it
// landed (LandedTip): "Tip: <sha>" — ParseTip's writer twin. Pure.
func TipPin(sha string) string { return "Tip: " + sha }

// ParseCommit reads the "Commit: <sha>" pin out of a bead body (the build/approval
// pin CommitPin wrote); ParseBase reads the "Base: <sha>" pin a conflict bead body
// carries (the base SHA the tail rebases onto, mirroring RebaseConflict.Base);
// ParseTip reads a chart-conflict's "Tip: <sha>" pin. An absent pin is (,false).
// Pure, so all three are checkable without git or bd.
func ParseCommit(body string) (sha string, ok bool) { return parsePin(body, "Commit:") }
func ParseBase(body string) (sha string, ok bool)   { return parsePin(body, "Base:") }
func ParseTip(body string) (sha string, ok bool)    { return parsePin(body, "Tip:") }

// ConflictPin is the body pin a fix-forward successor carries: a
// "Fix-forward: <base>" line — its own key, so a Base: pin a Checkpoint body may
// carry one day never reads as a fix-forward decision — plus PathsPin's line;
// ParseConflict reads it back, absent a Fix-forward: pin (,false). Kept together so
// the two can't drift. Pure.
func ConflictPin(c *RevertConflict) string {
	return "Fix-forward: " + c.Base + "\n" + PathsPin(c.Paths)
}

// PathsPin is the "Conflicted paths: a, b" line ConflictPin writes and ParseConflict
// reads; a fix-forward Build body states its paths with it too.
func PathsPin(paths []string) string { return "Conflicted paths: " + strings.Join(paths, ", ") }

// ParseConflict reads ConflictPin back; no Fix-forward: pin is (nil, false).
func ParseConflict(body string) (*RevertConflict, bool) {
	base, ok := parsePin(body, "Fix-forward:")
	if !ok {
		return nil, false
	}
	c := &RevertConflict{Base: base}
	if paths, ok := parsePin(body, "Conflicted paths:"); ok && paths != "" {
		c.Paths = strings.Split(paths, ", ")
	}
	return c, true
}

// parsePin greps the first "<key> <value>" line out of a body, trimming the value.
func parsePin(body, key string) (string, bool) {
	for _, line := range strings.Split(body, "\n") {
		if v, found := strings.CutPrefix(strings.TrimSpace(line), key); found {
			return strings.TrimSpace(v), true
		}
	}
	return "", false
}

// ApprovedAtHead reports whether a closed Approval's Commit: pin still equals the
// live tip of its Checkpoint branch: the Checkpoint is done only while
// its approval pins the current HEAD — a post-approval commit moves HEAD and unpins
// it. checkpointBranch is BranchName(checkpoint, id) (caller keeps git contract-
// blind, per BranchName). A body with no pin, or a Head error, is not-at-head.
func ApprovedAtHead(approvalBody, checkpointBranch string) bool {
	pin, ok := ParseCommit(approvalBody)
	if !ok {
		return false
	}
	head, err := Head(checkpointBranch)
	return err == nil && pin == head
}

// CommitsAhead counts branch's commits since its merge-base with base (base..branch),
// so mainline moving on doesn't count: how surgery end tells a surgery with work
// from an empty one (Spec D5). A missing branch errors.
func CommitsAhead(base, branch string) (int, error) {
	out, err := run("rev-list", "--count", base+".."+branch)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(out)
}

// cutBranchArgs is the pure argv that creates a branch WITHOUT switching HEAD.
// The footgun it encapsulates: `git branch <name> <base>`, never `checkout -b`
// — cutting a branch must not move the current worktree's HEAD.
func cutBranchArgs(name, base string) []string { return []string{"branch", name, base} }

// landRebaseArgs is the pure argv for Land's rebase step. The invariant it
// encodes: the branch is rebased onto the mainline CONSTANT, never current HEAD
// (ADR 0010 §3).
func landRebaseArgs(base string) []string { return []string{"rebase", base} }

// detachArgs / softResetArgs / squashCommitArgs / ffMainlineArgs are Land's
// squash sequence, split out so the whole land is checkable in Seam A (req 10).
// The squash: detach at the container's pinned tip, rebase, soft-reset back to base
// (tree/index keep the rebased content) then commit once. ffMainlineArgs moves mainline with
// update-ref, NOT `branch -f` — the latter refuses to move a branch checked out
// by another worktree, which is exactly the per-effort topology Land runs under.
// The move is a compare-and-swap: update-ref's third positional is
// the EXPECTED old value (the base captured at Land's start), so if a concurrent
// land already moved mainline the swap fails loudly instead of clobbering it
// — the branch was rebased onto `base`, so it's a valid ff only while
// mainline is still `base`.
func detachArgs(pin string) []string     { return []string{"checkout", "--detach", pin} }
func softResetArgs(base string) []string { return []string{"reset", "--soft", base} }
func squashCommitArgs(branch, pin, trailer string) []string {
	// --allow-empty: a fog-only Chart (all defog settled in bead notes, no ADR /
	// CONTEXT edit) lands an empty tree, but must still stamp its "Chart: <id>"
	// anchor — the landed base Zoro cuts from. Same empty-anchor rationale as a
	// no-artifact direct Spec (ADR 0014 §2); without it the squash fails "nothing
	// to commit" and the Chart can never land. The TipPin line records the
	// container tip that landed, which LandedTip reads back.
	return []string{"commit", "--allow-empty", "-m", "land " + branch, "-m", TipPin(pin) + "\n" + trailer}
}
func ffMainlineArgs(branch, oldBase string) []string {
	return []string{"update-ref", "refs/heads/" + Mainline(), branch, oldBase}
}

// worktreeListArgs enumerates every worktree in machine-readable form so
// ffMainline can find the one (if any) that has Mainline checked out.
func worktreeListArgs() []string { return []string{"worktree", "list", "--porcelain"} }

// mergeFFOnlyArgs fast-forwards Mainline from INSIDE its own worktree:
// `git -C <dir> merge --ff-only <branch>`. Unlike ffMainlineArgs' update-ref, this
// moves that worktree's index and working tree along with the ref, so the main
// checkout isn't left stranded at the pre-land commit. The land lock (ADR
// 0069) is what keeps main's tree clean: mugicrew's moves of main never interleave,
// so none loses a race and leaves a reverse diff staged. --ff-only (like update-ref's
// expected old value) is only the compare-and-swap backstop against git used outside
// mugicrew: it keeps the ref safe, refusing when Mainline has advanced past the
// rebase base, so a raced land fails loudly instead of clobbering.
func mergeFFOnlyArgs(dir, branch string) []string {
	return []string{"-C", dir, "merge", "--ff-only", branch}
}

// Holder is the worktree holding a branch's one checkout: its path, and whether
// git marks it prunable (its directory gone, its admin entry never pruned).
type Holder struct {
	Path     string
	Prunable bool
}

// holderOf parses `git worktree list --porcelain` and returns the worktree that
// has branch checked out, ok false if none does. Pure, so ffMainline's
// checked-out-vs-not branch is testable without a real repo. Each stanza is a
// `worktree <path>` line, then attribute lines, ended by a blank line; a holder has
// a `branch refs/heads/...` line (a detached or bare worktree has none, so never
// matches). A prunable or locked stanza still holds the branch, so it still matches.
func holderOf(porcelain, branch string) (Holder, bool) {
	var h Holder
	held := false
	for _, line := range strings.Split(porcelain+"\n", "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			if held { // a stanza with no closing blank line still ends here
				return h, true
			}
			h = Holder{Path: strings.TrimPrefix(line, "worktree ")}
		case line == "branch refs/heads/"+branch:
			held = true
		case line == "prunable" || strings.HasPrefix(line, "prunable "):
			h.Prunable = true
		case line == "" && held:
			return h, true
		}
	}
	return Holder{}, false
}

// worktreeOn is holderOf's path, "" if branch is checked out nowhere.
func worktreeOn(porcelain, branch string) string {
	h, _ := holderOf(porcelain, branch)
	return h.Path
}

// WorktreeOn is the worktree holding branch's one checkout, ok false if none: the
// holder behind a Busy refusal (ADR 0041 §2, ADR 0056). A var so Busy's callers'
// tests script the holder without a real repo.
var WorktreeOn = func(branch string) (h Holder, ok bool, err error) {
	list, err := run(worktreeListArgs()...)
	if err != nil {
		return Holder{}, false, err
	}
	h, ok = holderOf(list, branch)
	return h, ok, nil
}

// PruneWorktrees is `git worktree prune`: it forgets worktrees whose directory is
// gone, freeing the branch a prunable one still holds (ADR 0041 §3).
func PruneWorktrees() error {
	_, err := run("worktree", "prune")
	return err
}

// ffMainline advances Mainline to the just-squashed branch. If Mainline is checked
// out in a worktree it fast-forwards THROUGH that worktree (mergeFFOnlyArgs), so the
// checkout's index and working tree move with the ref instead of being stranded.
// If Mainline is checked out nowhere — the per-effort topology where the
// land runs on the only checkout — it moves the ref with the update-ref CAS
// (ffMainlineArgs). Both paths fail loudly when a concurrent land already advanced
// Mainline past base.
func ffMainline(branch, base string) error {
	list, err := run(worktreeListArgs()...)
	if err != nil {
		return err
	}
	if dir := worktreeOn(list, Mainline()); dir != "" {
		_, err := run(mergeFFOnlyArgs(dir, branch)...)
		return err
	}
	_, err = run(ffMainlineArgs(branch, base)...)
	return err
}

// landedBaseArgs is the pure argv that re-derives a landed commit by its
// trailer: the newest commit on mainline with a message line that IS the
// trailer. Anchored to the whole line (git --grep matches per line) so a short
// id can't resolve to a longer or dotted id's land; QuoteMeta keeps a
// bead id's metachars literal, and ERE reads its backslash escapes as literals.
func landedBaseArgs(trailer string) []string { return trailerLogArgs(Mainline(), trailer) }

// trailerLogArgs is landedBaseArgs on any ref: the newest commit on ref with a
// message line that IS the trailer.
func trailerLogArgs(ref, trailer string) []string {
	re := "^" + regexp.QuoteMeta(trailer) + "$"
	return []string{"log", ref, "--extended-regexp", "--grep=" + re, "--format=%H", "-n", "1"}
}

// CutBranch is idempotent branch-if-absent (mirrors SpawnSuccessorIfAbsent):
// if name already exists it is a no-op (created=false); else it is cut from
// base without touching HEAD. base is explicit at the call site — mainline for
// a Chart, the landed SHA for a Checkpoint (the Zoro seam).
func CutBranch(name, base string) (created bool, err error) {
	if _, err := run("rev-parse", "--verify", "--quiet", "refs/heads/"+name); err == nil {
		return false, nil // already exists
	}
	if _, err := run(cutBranchArgs(name, base)...); err != nil {
		return false, err
	}
	return true, nil
}

// rebaseInProgress reports whether git left an interrupted rebase behind — the
// signature of a real merge conflict (as opposed to a rebase that refused to
// start, e.g. a dirty tree or a bad base). --git-path resolves the state dir for
// the CURRENT worktree, so this is correct inside a per-effort linked worktree
// too, where the state lives under .git/worktrees/<name>/.
func rebaseInProgress() bool {
	for _, name := range []string{"rebase-merge", "rebase-apply"} {
		p, err := run("rev-parse", "--git-path", name)
		if err != nil {
			continue
		}
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return true
		}
	}
	return false
}

// Land rebases branch onto mainline, squashes to a single commit carrying
// trailer, and fast-forwards mainline; it returns that squash commit's SHA (=
// the landed base). Idempotency keys on the trailer (ADR 0004 §7): the
// "Chart: <id>" trailer is the durable landed fact on mainline, so a present
// trailer means already-landed and Land re-derives the SHA without re-committing
// — safe to re-run after a crash and correct even if the branch ref was later
// deleted. (Branch-ancestry can't serve this: an un-landed *empty* branch sits
// at base, which is itself an ancestor of mainline, so ancestry would mistake
// "nothing landed yet" for "already landed".) A rebase conflict returns
// *RebaseConflict carrying the base SHA; the rebase is aborted and no bead is
// touched. The whole land runs under the land lock via AnchorIfAbsent, so a failed
// trailer read is not absence. A dirty main is *MainDirty, and an ADR number the
// branch adds that the locked base already holds is *ADRClash, both before anything
// is touched.
//
// Land never writes the container (Spec D5, ADR 0102 §8): it pins the
// container's tip, detaches the cwd there (run it from a route.Detached tree; any
// other tree is left detached), rebases and squashes on that detached HEAD, and
// refuses if the container moved off the pin before it moves main. A failed land so
// leaves the container at the pin, which a conflict's filer reads as its Tip and an
// Approval's Commit: pin still matches; nothing needs restoring.
func Land(branch, trailer string) (sha string, err error) {
	return AnchorIfAbsent(trailer, LandedBase, func(trailer string) (string, error) { return land(branch, trailer) })
}

// land is Land's write leg: pin, clash check, rebase, squash, pin check, ffMainline.
func land(branch, trailer string) (sha string, err error) {
	if err := mainClean(); err != nil {
		return "", err
	}
	base, err := run("rev-parse", Mainline())
	if err != nil {
		return "", err
	}
	pin, err := run("rev-parse", "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	clashes, err := adrNumberClashes(pin, base)
	if err != nil {
		return "", err
	}
	if len(clashes) > 0 {
		return "", &ADRClash{Clashes: clashes, Base: base}
	}
	if _, err := run(detachArgs(pin)...); err != nil {
		return "", err
	}
	if _, err := run(landRebaseArgs(base)...); err != nil {
		// A real conflict leaves an in-progress rebase to abort; a clean failure
		// (dirty tree, bad base) leaves none, so it's not a conflict — surface it
		// raw rather than mislabel it *RebaseConflict.
		if !rebaseInProgress() {
			return "", err
		}
		_, _ = run("rebase", "--abort")
		return "", &RebaseConflict{Base: base}
	}
	if _, err := run(softResetArgs(base)...); err != nil {
		return "", err
	}
	if _, err := run(squashCommitArgs(branch, pin, trailer)...); err != nil {
		return "", err
	}
	if sha, err = run("rev-parse", "HEAD"); err != nil {
		return "", err
	}
	// The lock keeps integrate off the container; this catches any other writer up
	// to here (one committing between this read and the ff only stays unlanded).
	if cur, err := run("rev-parse", "refs/heads/"+branch); err != nil || cur != pin {
		return "", cmp.Or(err, fmt.Errorf("git: %s moved off its pinned tip %s mid-land; nothing landed, re-land", branch, pin))
	}
	if err := ffMainline(sha, base); err != nil { // ff through main's worktree, else CAS update-ref
		return "", err
	}
	return sha, nil
}

// LandedTip is the container tip the land squash sha pinned (its TipPin line), ok
// false for a squash without one.
func LandedTip(sha string) (tip string, ok bool) {
	body, err := run("log", "-1", "--format=%B", sha)
	if err != nil {
		return "", false
	}
	return ParseTip(body)
}

// anchorCommitArgs is the pure argv for Anchor's empty commit: commit-tree reuses
// base's own tree with base as sole parent, so it touches no index, worktree or
// HEAD — only ffMainline then moves mainline onto it.
func anchorCommitArgs(base, trailer string) []string {
	return []string{"commit-tree", base + "^{tree}", "-p", base, "-m", "anchor " + trailer, "-m", trailer}
}

// Anchor writes an empty commit stamped with trailer straight onto mainline and
// returns its SHA — the direct-Spec "Spec: <id>" anchor Zoro cuts from (ADR 0014
// §2, ADR 0035 §4). It advances mainline through ffMainline exactly as Land does,
// so a checked-out main worktree's index and tree stay in sync, and a
// raced mainline fails loudly. Unlike Land it is NOT idempotent on its
// own: it is only ever AnchorIfAbsent's write leg, which holds the land lock (ADR
// 0014 §2's shape, ADR 0069). A dirty main is *MainDirty.
func Anchor(trailer string) (sha string, err error) {
	if err := mainClean(); err != nil {
		return "", err
	}
	base, err := run("rev-parse", Mainline())
	if err != nil {
		return "", err
	}
	if sha, err = run(anchorCommitArgs(base, trailer)...); err != nil {
		return "", err
	}
	if err := ffMainline(sha, base); err != nil {
		return "", err
	}
	return sha, nil
}

// MainMoved is LandLineage's refusal of a lineage branch cut from an older main:
// its one commit's parent is on main but no longer main's tip. Nothing was touched;
// re-running lineage sync rebuilds the merge on the current main.
type MainMoved struct {
	Branch, Parent, Main string
}

func (e *MainMoved) Error() string {
	return fmt.Sprintf("git: main moved to %s since %s was cut from %s; re-run lineage sync, then land-lineage the new branch", e.Main, e.Branch, e.Parent)
}

// LineageShape is LandLineage's refusal of a branch that is not exactly one
// single-parent commit on main's tip (more commits, a merge commit, or none).
type LineageShape struct {
	Branch, Why string
}

func (e *LineageShape) Error() string {
	return fmt.Sprintf("git: %s is not a lineage branch: %s", e.Branch, e.Why)
}

// NoLineageTrailer is LandLineage's refusal of a commit git's trailer parser finds
// no Lineage: trailer on.
type NoLineageTrailer struct {
	Branch string
}

func (e *NoLineageTrailer) Error() string {
	return fmt.Sprintf("git: %s's commit carries no Lineage: trailer", e.Branch)
}

// LandLineage fast-forwards main onto a lineage branch's one commit under the land
// lock and deletes the branch (Spec D1, ADR 0119): no squash, rebase or
// added trailer, so the Lineage: trailer reaches main byte-for-byte, and no lineage
// call — mugicrew only reads the trailer format. The branch must be one commit
// whose sole parent is main's tip, carrying a Lineage: trailer; else it refuses
// typed (*MainDirty, *MainMoved, *LineageShape, *NoLineageTrailer), touching
// nothing. Not idempotent: a re-run finds the branch gone.
func LandLineage(branch string) (sha string, err error) {
	return withLandLock(func() (string, error) {
		if err := mainClean(); err != nil {
			return "", err
		}
		base, err := run("rev-parse", Mainline())
		if err != nil {
			return "", err
		}
		tip, err := run("rev-parse", "--verify", "refs/heads/"+branch)
		if err != nil {
			return "", fmt.Errorf("git: no lineage branch %s: %w", branch, err)
		}
		if n, err := run("rev-list", "--count", base+".."+tip); err != nil || n != "1" {
			return "", cmp.Or(err, error(&LineageShape{Branch: branch, Why: n + " commits ahead of " + Mainline() + ", want 1"}))
		}
		parents, err := run("rev-list", "--parents", "-n", "1", tip) // "<tip> <parent>..."
		if err != nil {
			return "", err
		}
		switch p := strings.Fields(parents)[1:]; {
		case len(p) != 1:
			return "", &LineageShape{Branch: branch, Why: fmt.Sprintf("%d parents, want 1", len(p))}
		case p[0] != base: // one commit ahead, so its parent is on main
			return "", &MainMoved{Branch: branch, Parent: p[0], Main: base}
		}
		if tr, err := run("log", "-1", "--format=%(trailers:key=Lineage,valueonly)", tip); err != nil || tr == "" {
			return "", cmp.Or(err, error(&NoLineageTrailer{Branch: branch}))
		}
		if err := ffMainline(tip, base); err != nil { // ff through main's worktree, else CAS update-ref
			return "", err
		}
		if err := DeleteBranch(branch); err != nil {
			return tip, fmt.Errorf("git: landed %s but kept %s: %w", tip, branch, err)
		}
		return tip, nil
	})
}

// RevertsTrailer is the trailer a Rollback's revert commit carries (ADR 0047 §5):
// the land trailer prefixed "Reverts-" ("Reverts-Checkpoint: <id>"). LandedBase's
// anchored ^…$ grep keeps the two apart, so a revert never reads as a land.
func RevertsTrailer(kind, id string) string { return "Reverts-" + TrailerFor(kind, id) }

// MainlineCommit is one first-parent mainline commit as MainlineLog reads it: its
// land trailer id (Lands) and revert trailer id (Reverts), "" when absent.
type MainlineCommit struct {
	SHA     string
	Time    time.Time
	Lands   string // "<Kind>: <id>" trailer's id
	Reverts string // "Reverts-<Kind>: <id>" trailer's id
}

// mainlineLogArgs is the pure argv for MainlineLog: the newest n first-parent
// commits, each as sha␟unix-time␟body␞.
func mainlineLogArgs(n int) []string {
	return []string{"log", Mainline(), "--first-parent", "-n", fmt.Sprint(n), "--format=%H%x1f%ct%x1f%B%x1e"}
}

// MainlineLog reads the newest n first-parent mainline commits, newest first,
// with kind's land and revert trailer ids (the History tab's source). Read-only:
// it takes no land lock. A trailer is a whole message line, as LandedBase greps it.
func MainlineLog(kind string, n int) ([]MainlineCommit, error) {
	out, err := run(mainlineLogArgs(n)...)
	if err != nil {
		return nil, err
	}
	land, rev := TrailerFor(kind, ""), RevertsTrailer(kind, "")
	var cs []MainlineCommit
	for _, rec := range strings.Split(out, "\x1e") {
		f := strings.SplitN(strings.TrimSpace(rec), "\x1f", 3)
		if len(f) < 3 {
			continue
		}
		secs, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("git log: bad commit time %q: %w", f[1], err)
		}
		c := MainlineCommit{SHA: f[0], Time: time.Unix(secs, 0)}
		for _, l := range strings.Split(f[2], "\n") {
			l = strings.TrimSpace(l)
			if id, ok := strings.CutPrefix(l, land); ok && c.Lands == "" {
				c.Lands = id
			} else if id, ok := strings.CutPrefix(l, rev); ok && c.Reverts == "" {
				c.Reverts = id
			}
		}
		cs = append(cs, c)
	}
	return cs, nil
}

// revertTreeArgs / revertCommitArgs are Revert's plumbing pair: merge-tree applies
// squash^ onto base with squash as the merge base (exactly undoing squash's diff),
// then commit-tree writes that tree with base as sole parent. Neither touches an
// index, worktree or HEAD — only ffMainline then moves mainline, as for Anchor.
// merge-tree --write-tree with --merge-base= needs git >= 2.40; an older git fails
// it as a usage error (exit 129), which revert surfaces raw, never as a conflict.
func revertTreeArgs(base, squash string) []string {
	return []string{"merge-tree", "--write-tree", "--name-only", "--merge-base=" + squash, base, squash + "^"}
}
func revertCommitArgs(tree, base, squash, trailer string) []string {
	return []string{"commit-tree", tree, "-p", base, "-m", "revert " + squash, "-m", trailer}
}

// Revert undoes squash on mainline with one forward commit carrying trailer and
// returns its SHA (ADR 0047 §4–6): mainline never rewinds, even when squash is at
// HEAD. It advances mainline through ffMainline exactly as Land/Anchor do. It is
// idempotent on the trailer: present ⇒ that commit's SHA, nothing written; a failed
// read is not absence, so never a second revert. A conflicting revert (merge-tree
// exit 1) is *RevertConflict with mainline untouched (ADR 0049): merge-tree writes
// only objects, so there is no in-progress revert to abort.
func Revert(squash, trailer string) (sha string, err error) {
	return AnchorIfAbsent(trailer, LandedBase, func(trailer string) (string, error) { return revert(squash, trailer) })
}

// revert is Revert's write leg: one forward revert commit, then ffMainline.
func revert(squash, trailer string) (sha string, err error) {
	if err := mainClean(); err != nil {
		return "", err
	}
	base, err := run("rev-parse", Mainline())
	if err != nil {
		return "", err
	}
	tree, err := runOut(revertTreeArgs(base, squash)...)
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 { // conflicted; any other failure is raw
		return "", &RevertConflict{Base: base, Paths: conflictPaths(tree)}
	}
	if err != nil {
		return "", err
	}
	if sha, err = run(revertCommitArgs(tree, base, squash, trailer)...); err != nil {
		return "", err
	}
	if err := ffMainline(sha, base); err != nil {
		return "", err
	}
	return sha, nil
}

// reapplyCommitArgs / reapplyRefArgs are Reapply's plumbing pair: commit-tree writes
// revert's PARENT tree (exactly the revert of the revert — it cannot conflict) with
// revert as sole parent — the same tree `git revert <revert>` (ADR 0048 §4) yields,
// without a checkout — then a CAS update-ref moves branch from revert onto it. No
// index, worktree or HEAD is touched, like CutBranch and Anchor.
func reapplyCommitArgs(revert string) []string {
	return []string{"commit-tree", revert + "~1^{tree}", "-p", revert, "-m", "reapply " + revert}
}
func reapplyRefArgs(branch, sha, revert string) []string {
	return []string{"update-ref", "refs/heads/" + branch, sha, revert}
}

// Reapply seeds a Rollback's successor branch, cut at the revert commit, with one
// reapply commit restoring the reverted diff (ADR 0048 §4) and returns the branch
// tip. Skip-if-done: a tip that is no longer revert is already seeded (or has moved
// on), so nothing is written. It runs under the land lock but skips mainClean: it
// moves only its successor branch, never main (ADR 0069).
func Reapply(branch, revert string) (sha string, err error) {
	return withLandLock(func() (string, error) { return reapply(branch, revert) })
}

func reapply(branch, revert string) (sha string, err error) {
	tip, err := run("rev-parse", "refs/heads/"+branch)
	if err != nil || tip != revert {
		return tip, err
	}
	if sha, err = run(reapplyCommitArgs(revert)...); err != nil {
		return "", err
	}
	if _, err := run(reapplyRefArgs(branch, sha, revert)...); err != nil {
		return "", err
	}
	return sha, nil
}

// EffortBranch is a session's own branch, effort/<bead-id> (ADR 0102): its commits
// reach the container only through Integrate.
func EffortBranch(id string) string { return effortPrefix + id }

const effortPrefix = "effort/"

// EffortRefs is the set of bead ids holding an effort/<id> branch, read in one
// for-each-ref, never one git call per ref (Spec D7): a closed bead in it
// still has unintegrated work, so it keeps its container from draining.
func EffortRefs() (map[string]bool, error) {
	out, err := run("for-each-ref", "--format=%(refname:lstrip=3)", "refs/heads/"+effortPrefix)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for id := range strings.FieldsSeq(out) {
		ids[id] = true
	}
	return ids, nil
}

// CurrentEffort is the bead id off the cwd's branch, effort/<id>: the one input
// mugicrew integrate derives (Spec D3), read through a rebase stopped on
// it. Any other branch, or a detached HEAD outside a rebase, is an error.
func CurrentEffort() (string, error) {
	b, err := run("symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil { // a stopped rebase detaches HEAD; its state dir names the branch
		b, err = rebaseHeadName()
	}
	if id, ok := strings.CutPrefix(b, effortPrefix); err == nil && ok && id != "" {
		return id, nil
	}
	return "", fmt.Errorf("git: not on an %s<bead-id> branch (%q)", effortPrefix, b)
}

// rebaseHeadName is the short name of the branch an in-progress rebase is
// rewriting, read from its state dir's head-name.
func rebaseHeadName() (string, error) {
	for _, name := range []string{"rebase-merge", "rebase-apply"} {
		p, err := run("rev-parse", "--git-path", name+"/head-name")
		if err != nil {
			continue
		}
		if b, err := os.ReadFile(p); err == nil {
			return strings.TrimPrefix(strings.TrimSpace(string(b)), "refs/heads/"), nil
		}
	}
	return "", errors.New("git: HEAD is detached and no rebase is in progress")
}

// IntegrateConflict is Integrate's typed stop, a conflict for its caller to
// surface (ADR 0102 §3): Reason ConflictRebase (left stopped in the tree for the
// agent to resolve), ConflictHeld (Holder has the container checked out) or
// ConflictLanded (the container's land trailer is already on main). The
// container is untouched.
type IntegrateConflict struct {
	Reason    ConflictReason
	Container string
	Holder    string
}

// ConflictReason is why an integrate stopped as a conflict, the reason its
// integrate.conflict event carries (Spec FR 55).
type ConflictReason string

const (
	ConflictRebase ConflictReason = "rebase"
	ConflictHeld   ConflictReason = "held"
	ConflictLanded ConflictReason = "landed"
)

func (e *IntegrateConflict) Error() string {
	switch e.Reason {
	case ConflictHeld:
		return fmt.Sprintf("git: %s is checked out in %s; integrating would strand that tree", e.Container, e.Holder)
	case ConflictLanded:
		return fmt.Sprintf("git: %s has already landed; nothing may integrate onto it", e.Container)
	}
	return fmt.Sprintf("git: rebase onto %s conflicted", e.Container)
}

// ErrContended is Integrate's transient failure: the container moved under every
// one of its integrateRetries retries.
var ErrContended = errors.New("git: container kept moving; integrate retries spent")

// ReasonContended is the reason integrate.failed carries for ErrContended, beside
// the ConflictReasons integrate.conflict carries (Spec FR 55).
const ReasonContended = "contended"

// integrateRetries bounds Integrate's re-rebase after a lost CAS (ADR 0102 §3).
const integrateRetries = 3

// errMoved is an attempt's "the container tip moved off the rebase base".
var errMoved = errors.New("git: container moved")

// Integrate squashes the cwd's effort branch onto container (Spec D3, ADR
// 0102 §3, the prototype's loop): it rebases onto the container tip
// outside the land lock, then under it refuses a held or landed container and
// CAS-moves the container onto one commit-tree squash, subject "integrate <id>"
// plus trailer (the bead's TrailerFor). A moved tip re-rebases, up to
// integrateRetries times, then ErrContended. It returns the squash, "" when
// nothing is ahead or the effort adds nothing past its last squash (idempotent,
// integratedThrough); commits made after an earlier integrate squash as a further
// "integrate <id>". A rebase conflict is left stopped in the tree as
// *IntegrateConflict, and a re-run over that stopped rebase returns the same conflict.
func Integrate(container, id, trailer string) (sha string, retries int, err error) {
	return integrateLoop(container, func(tip string) (string, error) {
		upstream, err := integratedThrough(tip, trailer)
		if err != nil || upstream == "" {
			return "", err
		}
		if _, err := run("rebase", "--onto", tip, upstream); err != nil {
			return "", rebaseStop(container, err)
		}
		head, err := run("rev-parse", "HEAD")
		if err != nil || head == tip {
			return "", err
		}
		// an unmoved tip under the lock keeps integratedThrough's verdict
		return casLocked(container, tip, func() (string, error) {
			return run("commit-tree", head+"^{tree}", "-p", tip, "-m", "integrate "+id, "-m", trailer)
		})
	})
}

// IntegrateReplace publishes the cwd's effort branch, a history rewrite of the
// container cut at its pinned Tip, as the container itself (Spec D3, FR
// 13–14, ADR 0102 §4): no squash, a replace-CAS update-ref <container> <rewritten>
// <base> under the land lock, with Integrate's held and landed refusals. A
// container moved past the pin first has pinned..tip replayed onto the effort
// branch (replay), a conflict there left stopped in the tree as *IntegrateConflict;
// a lost CAS replays the newer commits and retries, up to integrateRetries. It
// returns the published tip, "" when the container already contains the effort's
// HEAD (nothing ahead, or a re-run after the CAS) or the effort holds nothing past
// the pin (a recut at it after an earlier replace published). An
// effort already holding the container tip (cut at it) replays nothing.
func IntegrateReplace(container, pinned string) (sha string, retries int, err error) {
	return integrateLoop(container, func(tip string) (string, error) {
		head, err := run("rev-parse", "HEAD")
		if err != nil {
			return "", err
		}
		// A head strictly behind the pin dropped commits: a rewrite to publish, not
		// nothing ahead.
		// ponytail: once that drop is published and a sibling integrates past it, a
		// re-run refuses as rewritten off the pin (exit 1); loud, never a lost drop.
		dropped := head != pinned && IsAncestor(head, pinned)
		if head == tip || (!dropped && (IsAncestor(head, tip) || IsAncestor(head, pinned))) {
			return "", nil
		}
		base, err := replayedThrough(pinned, tip)
		if err != nil {
			return "", err
		}
		if base != tip && !IsAncestor(tip, head) {
			if !IsAncestor(base, tip) {
				return "", fmt.Errorf("git: %s was rewritten off its pinned tip %s; nothing integrated", container, base)
			}
			if err := replay(base, tip); err != nil {
				return "", rebaseStop(container, err)
			}
			if head, err = run("rev-parse", "HEAD"); err != nil {
				return "", err
			}
		}
		return casLocked(container, tip, func() (string, error) { return head, nil })
	})
}

// integrateLoop is Integrate's and IntegrateReplace's shared retry: it runs
// attempt at the container's current tip until it reports anything but errMoved,
// up to integrateRetries retries, then ErrContended. A rebase already stopped in
// the tree is the same conflict again, with no attempt.
func integrateLoop(container string, attempt func(tip string) (string, error)) (sha string, retries int, err error) {
	if rebaseInProgress() {
		return "", 0, &IntegrateConflict{Reason: ConflictRebase, Container: container}
	}
	for retries = 0; retries <= integrateRetries; retries++ {
		var tip string
		if tip, err = run("rev-parse", "refs/heads/"+container); err != nil {
			return "", retries, err
		}
		if sha, err = attempt(tip); !errors.Is(err, errMoved) {
			return sha, retries, err
		}
	}
	return "", integrateRetries, ErrContended
}

// rebaseStop maps a failed rebase or replay: one left stopped in the tree is an
// *IntegrateConflict, one that refused to start (a dirty tree) is err itself.
func rebaseStop(container string, err error) error {
	if !rebaseInProgress() {
		return err
	}
	return &IntegrateConflict{Reason: ConflictRebase, Container: container}
}

// casLocked is an integrate's in-lock leg: held and landed refusals, the
// moved-tip check, then a CAS of container from tip onto the commit next makes.
// The landed check shares the lock with Land's pin check, so an integrate queued
// behind a land refuses (ADR 0102). Only a tip that actually moved is errMoved; any
// other update-ref failure (a ref lock, a permission) is returned as itself.
func casLocked(container, tip string, next func() (string, error)) (string, error) {
	return withLandLock(func() (string, error) {
		ref := "refs/heads/" + container
		if err := integrateRefusal(container); err != nil {
			return "", err
		}
		if cur, err := run("rev-parse", ref); err != nil || cur != tip {
			return "", cmp.Or(err, errMoved)
		}
		sha, err := next()
		if err != nil {
			return "", err
		}
		if _, err := run("update-ref", ref, sha, tip); err != nil {
			if cur, rerr := run("rev-parse", ref); rerr == nil && cur != tip {
				return "", errMoved // CAS lost: a writer outside the lock moved the tip
			}
			return "", err
		}
		return sha, nil
	})
}

// replayedKey is the trailer key replay stamps on the effort's newest commit: the
// container tip its replay reached, so a re-run (after the agent resolved and
// continued) replays only what came after it.
const replayedKey = "Replayed"

// replayedThrough is the container commit the effort branch already holds the
// container's history through: its newest Replayed: trailer that lies on
// pinned..tip, else pinned.
func replayedThrough(pinned, tip string) (string, error) {
	out, err := run("log", "--format=%(trailers:key="+replayedKey+",valueonly)", pinned+"..HEAD")
	if err != nil {
		return "", err
	}
	for v := range strings.FieldsSeq(out) {
		if IsAncestor(pinned, v) && IsAncestor(v, tip) {
			return v, nil
		}
	}
	return pinned, nil
}

// replay picks base..tip onto the effort branch with an interactive rebase whose
// todo is the picks plus a closing exec stamping the Replayed: trailer. Unlike
// rebase --onto HEAD base tip it keeps the effort branch checked out, so a
// conflict's rebase --continue finishes on it and --abort returns it to the rewrite.
func replay(base, tip string) error {
	shas, err := run("rev-list", "--reverse", "--no-merges", base+".."+tip)
	if err != nil {
		return err
	}
	var todo strings.Builder
	for s := range strings.FieldsSeq(shas) {
		todo.WriteString("pick " + s + "\n")
	}
	todo.WriteString("exec git commit --amend --no-edit --allow-empty --trailer '" + replayedKey + ": " + tip + "'\n")
	dir, err := os.MkdirTemp("", "mugicrew-replay-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	f := filepath.Join(dir, "todo")
	if err := os.WriteFile(f, []byte(todo.String()), 0o644); err != nil {
		return err
	}
	_, err = run("-c", "sequence.editor=cp '"+f+"'", "rebase", "-i", "--empty=drop", "--onto", "HEAD", "HEAD")
	return err
}

// integrateRefusal is the in-lock held and landed refusals both integrate modes
// share (ADR 0102 §3): any worktree on the container, or its land trailer on main.
func integrateRefusal(container string) error {
	if h, ok, err := WorktreeOn(container); err != nil || ok {
		if ok {
			err = &IntegrateConflict{Reason: ConflictHeld, Container: container, Holder: h.Path}
		}
		return err
	}
	kind, id := ParseBranchName(container)
	if _, err := LandedBase(TrailerFor(kind, id)); err == nil {
		return &IntegrateConflict{Reason: ConflictLanded, Container: container}
	} else if !errors.Is(err, ErrNotLanded) {
		return err
	}
	return nil
}

// integratedThrough is Integrate's idempotency check against the container tip:
// the upstream to rebase the effort's HEAD from, or "" when it adds nothing past
// the bead's newest squash on tip (its tree is that squash's). With
// no squash yet, or an effort recut from the container, it is tip; an effort
// that already integrated rebases only its commits past the one whose tree the
// squash took, since replaying the integrated ones onto their squash can conflict.
func integratedThrough(tip, trailer string) (string, error) {
	sq, err := run(trailerLogArgs(tip, trailer)...)
	if err != nil || sq == "" {
		return tip, err
	}
	tree, err := run("rev-parse", sq+"^{tree}")
	if err != nil {
		return "", err
	}
	if head, err := run("rev-parse", "HEAD^{tree}"); err != nil || head == tree {
		return "", err
	}
	out, err := run("log", "--format=%H %T", tip+"..HEAD")
	if err != nil {
		return "", err
	}
	for line := range strings.Lines(out) {
		if c, t, _ := strings.Cut(strings.TrimSpace(line), " "); t == tree {
			return c, nil
		}
	}
	return tip, nil
}

// adrDir names an ADR directory; each <prefix>docs/adr/ numbers its own ADRs.
const adrDir = "docs/adr/"

// adrParts splits an ADR path, <prefix>docs/adr/NNNN-<slug>.md with <prefix>
// empty or ending in "/" and no "/" after docs/adr/, into its directory (the
// number space: each docs/adr/ numbers its own ADRs, no registry) and its NNNN.
// Both are "" if the path is not an ADR.
func adrParts(path string) (dir, num string) {
	i := strings.LastIndex(path, adrDir)
	if i < 0 || (i > 0 && path[i-1] != '/') {
		return "", ""
	}
	dir, name := path[:i+len(adrDir)], path[i+len(adrDir):]
	if strings.Contains(name, "/") || !strings.HasSuffix(name, ".md") ||
		len(name) < 5 || name[4] != '-' || strings.Trim(name[:4], "0123456789") != "" {
		return "", ""
	}
	return dir, name[:4]
}

// adrSlug returns the path of an ADR minus its NNNN- (directory kept), or "" if
// the path is not an ADR. A renumber keeps the slug and the directory.
func adrSlug(path string) string {
	dir, num := adrParts(path)
	if num == "" {
		return ""
	}
	return dir + path[len(dir)+5:]
}

// adrClashes is the pure core of adrNumberClashes: each added ADR whose number
// mainline already holds in the same docs/adr/ under a DIFFERENT path, as
// "added (vs mainline, ...)".
// Same-path hits are skipped — an add/add of one path is a textual conflict
// Land's rebase already catches, and it is also what a re-run sees after the
// Chart itself landed.
func adrClashes(added, mainline []string) []string {
	byNum := map[string][]string{}
	for _, p := range mainline {
		if dir, n := adrParts(p); n != "" {
			byNum[dir+n] = append(byNum[dir+n], p)
		}
	}
	var clashes []string
	for _, p := range added {
		dir, n := adrParts(p)
		if n == "" {
			continue
		}
		var others []string
		for _, m := range byNum[dir+n] {
			if m != p {
				others = append(others, m)
			}
		}
		if len(others) > 0 {
			clashes = append(clashes, p+" (vs mainline "+strings.Join(others, ", ")+")")
		}
	}
	return clashes
}

// addedADRsArgs lists every path branch adds since mb; adrClashes keeps the
// ADRs, in any docs/adr/. --no-renames so a Chart that git-mv's an ADR onto a
// taken number shows as an add, not an R that --diff-filter=A would drop.
func addedADRsArgs(mb, branch string) []string {
	return []string{"diff", "--name-only", "--no-renames", "--diff-filter=A", mb, branch}
}

// adrNumberClashes reports the ADR numbers branch ADDS (new
// <prefix>docs/adr/NNNN-*.md vs its merge-base with base) that base already
// holds in that same docs/adr/ under another filename (ADR 0030 §1, OE
// ADR 0086 §5). Only added numbers count: duplicates already on base never clash.
// Read-only — Land runs it under the lock against the exact base it lands on.
func adrNumberClashes(branch, base string) ([]string, error) {
	mb, err := run("merge-base", base, branch)
	if err != nil {
		return nil, err
	}
	added, err := run(addedADRsArgs(mb, branch)...)
	if err != nil {
		return nil, err
	}
	onMain, err := run("ls-tree", "-r", "--name-only", base)
	if err != nil {
		return nil, err
	}
	return adrClashes(strings.Split(added, "\n"), strings.Split(onMain, "\n")), nil
}

// ResolveCheckFailed is CheckResolved's typed failure (ADR 0080 §3–4): the
// numbered assertion that failed, what it found, and the offending paths (for
// assertion 2, the clashes as adrClashes formats them).
type ResolveCheckFailed struct {
	Assertion int
	Reason    string
	Paths     []string
}

func (e *ResolveCheckFailed) Error() string {
	msg := fmt.Sprintf("git: post-resolve check %d failed: %s", e.Assertion, e.Reason)
	if len(e.Paths) > 0 {
		msg += ": " + strings.Join(e.Paths, ", ")
	}
	return msg
}

// CheckResolved is the post-resolve check on a resolved Chart branch (ADR 0080
// §3), run in its worktree against the carried base, not current main. In order:
//  1. no rebase in progress, a clean tree, and base an ancestor of HEAD;
//  2. no ADR-number clash against base (Land's own guard);
//  3. only if tip is set: every mainline path HEAD changes vs base lies in the
//     Chart's own diff, merge-base(base, tip)..tip, or is an added ADR with a
//     Chart ADR's slug (a renumber), or is a path mainline renamed, between
//     merge-base(base, tip) and base, from a Chart path.
//
// Anchoring 3 on the pinned tip, not the session-start tip, covers a bad commit
// from an earlier run. Read-only; a failure is *ResolveCheckFailed.
func CheckResolved(base, tip string) error {
	if rebaseInProgress() {
		return &ResolveCheckFailed{Assertion: 1, Reason: "rebase in progress"}
	}
	st, err := run("status", "--porcelain")
	if err != nil {
		return err
	}
	if st != "" {
		var paths []string
		for _, l := range strings.Split(st, "\n") {
			paths = append(paths, strings.TrimSpace(l[2:])) // run trims line 1's leading status space
		}
		return &ResolveCheckFailed{Assertion: 1, Reason: "tree not clean", Paths: paths}
	}
	if _, err := run("merge-base", "--is-ancestor", base, "HEAD"); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 1 {
			return err
		}
		return &ResolveCheckFailed{Assertion: 1, Reason: "base " + base + " is not an ancestor of HEAD"}
	}
	clashes, err := adrNumberClashes("HEAD", base)
	if err != nil {
		return err
	}
	if len(clashes) > 0 {
		return &ResolveCheckFailed{Assertion: 2, Reason: "ADR-number clash against " + base, Paths: clashes}
	}
	if tip == "" {
		return nil
	}
	mb, err := run("merge-base", base, tip)
	if err != nil {
		return err
	}
	chart, err := run("diff", "--name-only", "--no-renames", mb, tip)
	if err != nil {
		return err
	}
	changed, err := run("diff", "--name-status", "--no-renames", base, "HEAD")
	if err != nil {
		return err
	}
	chartPaths := strings.Split(chart, "\n")
	chartSlugs := map[string]bool{}
	for _, p := range chartPaths {
		if s := adrSlug(p); s != "" {
			chartSlugs[s] = true
		}
	}
	// A Chart path mainline renamed after the fork lands at the new path: the
	// endpoint diff collapses a chain P → Q → P' to P', at git's default 50%.
	renames, err := run("diff", "--name-status", "-M", mb, base)
	if err != nil {
		return err
	}
	for _, l := range strings.Split(renames, "\n") {
		if f := strings.Split(l, "\t"); len(f) == 3 && strings.HasPrefix(f[0], "R") && slices.Contains(chartPaths, f[1]) {
			chartPaths = append(chartPaths, f[2])
		}
	}
	var stray []string
	for _, l := range strings.Split(changed, "\n") {
		status, p, ok := strings.Cut(l, "\t")
		// An added ADR sharing a Chart ADR's slug is that ADR renumbered (FR7, ADR
		// 0030 §2): new vs base and absent from Tip's diff, yet no mainline content.
		if !ok || slices.Contains(chartPaths, p) || (status == "A" && chartSlugs[adrSlug(p)]) {
			continue
		}
		stray = append(stray, p)
	}
	if len(stray) > 0 {
		return &ResolveCheckFailed{Assertion: 3, Reason: "mainline-only paths changed outside the Chart's diff", Paths: stray}
	}
	return nil
}

// RestoreResolved undoes a failed post-resolve check (ADR 0080 §4): it aborts a
// rebase left in progress, then hard-resets the worktree's branch to start, the
// session-start tip (not the pinned Tip). Commits that predate the session are
// start's ancestors, so they stay. Untracked files go with the effort worktree's
// teardown. Never run on Escalate:.
func RestoreResolved(start string) error {
	if rebaseInProgress() {
		if _, err := run("rebase", "--abort"); err != nil {
			return err
		}
	}
	_, err := run("reset", "--hard", start)
	return err
}

// ErrNotLanded is LandedBase's "trailer absent" error, distinct from a failed git
// call, so a write-if-absent caller (the fast-lane anchor) never mistakes a git
// failure for absence and writes a second commit.
var ErrNotLanded = errors.New("git: no landed commit")

// AnchorIfAbsent is the anchor's write-if-absent shape (ADR 0014 §2): write(trailer)
// only when landed(trailer) reports ErrNotLanded — a failed read is not absence, so
// never a second anchor. Nami's fast lane, Zoro's confirm sweep, Land and Revert
// share it; the legs are parameters so each caller keeps its own test seams. Both
// legs run under one land lock, so no check-then-write window stays open (ADR 0069).
func AnchorIfAbsent(trailer string, landed, write func(string) (string, error)) (sha string, err error) {
	return withLandLock(func() (string, error) {
		sha, err := landed(trailer)
		if errors.Is(err, ErrNotLanded) {
			sha, err = write(trailer)
		}
		return sha, err
	})
}

// LandedBase re-derives a landed commit by grepping its trailer on mainline
// (ADR 0010 §2) — Zoro's read path, and Land's internal no-op derivation. An
// absent trailer is a loud error wrapping ErrNotLanded.
func LandedBase(trailer string) (sha string, err error) {
	sha, err = run(landedBaseArgs(trailer)...)
	if err != nil {
		return "", err
	}
	if sha == "" {
		return "", fmt.Errorf("%w for trailer %q", ErrNotLanded, trailer)
	}
	return sha, nil
}
