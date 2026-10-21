// Package git is the single shell-out choke point for the git-branch layer
// (ADR 0009/0010). Every git footgun is encapsulated in the private run(...),
// never at a call site, and the argv is split into pure helpers that are
// checkable without a real repo — mirroring bd/'s newSignalArgs /
// successorCreateArgs pattern.
//
// The core ops are CutBranch (idempotent branch-if-absent),
// Land (rebase+squash onto mainline, git-native idempotent), and LandedBase
// (re-derive a landed commit by its trailer), Revert (the forward Rollback revert, ADR 0047),
// plus Land's in-lock numbered-file clash check (ADR 0030), *NumberClash. Every move of
// main (and Reapply) runs under the one land lock, and a dirty main is *MainDirty
// (ADR 0069). CheckResolved/RestoreResolved are the post-resolve check on a
// resolved Chart branch and its undo, *ResolveCheckFailed (ADR 0080).
package git

import (
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
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

	"jayulabs.org/mugicrew/forge"
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
	Path  string   // the worktree that has main checked out
	Files []string // its dirty paths, status --porcelain order, XY prefix stripped
}

// mainDirtyFilesShown caps how many Files the message names.
const mainDirtyFilesShown = 5

func (e *MainDirty) Error() string {
	list := ""
	if n := len(e.Files); n > 0 {
		list = " (" + strings.Join(e.Files[:min(n, mainDirtyFilesShown)], ", ")
		if n > mainDirtyFilesShown {
			list += fmt.Sprintf(", … +%d more", n-mainDirtyFilesShown)
		}
		list += ")"
	}
	return fmt.Sprintf("git: main worktree %s is dirty%s; clean main, then re-run", e.Path, list)
}

// NumberClash is Land's typed refusal (ADR 0030, ADR 0069): the branch adds an ADR
// number the locked base already holds under another filename — it rebases
// cleanly, so the rebase can't catch it. It carries the clashes (as adrClashes
// formats them) and the base checked against. Nothing was touched.
type NumberClash struct {
	Clashes []string
	Base    string // mainline SHA the clash check read
}

func (e *NumberClash) Error() string {
	return fmt.Sprintf("git: numbered-file clash landing onto %s: %s", e.Base, strings.Join(e.Clashes, "; "))
}

// mainClean is the write leg's guard: a *MainDirty if a worktree has main checked
// out with any tracked change or non-ignored untracked file (status --porcelain's
// default), nil if main is clean or checked out nowhere.
func mainClean() error { return Repo("").mainClean() }

// mainClean is the package mainClean on the child's main checkout (*MainDirty names
// it); the empty Repo is the cwd's repo.
func (r Repo) mainClean() error {
	h, ok, err := r.worktreeOn(r.mainline())
	if err != nil || !ok {
		return err
	}
	return mainDirtyIn(h.Path)
}

// mainDirtyIn is a *MainDirty naming dir's dirty paths, nil if dir is clean.
func mainDirtyIn(dir string) error {
	files, err := dirtyPaths(dir)
	if err != nil || len(files) == 0 {
		return err
	}
	return &MainDirty{Path: dir, Files: files}
}

// Dirty reports whether the worktree at dir has any tracked change or
// non-ignored untracked file (status --porcelain's default).
func Dirty(dir string) (bool, error) {
	n, err := DirtyFiles(dir)
	return n > 0, err
}

// DirtyFiles counts Dirty's changes: status --porcelain's lines, one per path.
func DirtyFiles(dir string) (int, error) {
	files, err := dirtyPaths(dir)
	return len(files), err
}

// dirtyPaths returns the paths status --porcelain lists, in its order, with the XY
// prefix stripped (a rename stays "old -> new"); nil if dir is clean.
func dirtyPaths(dir string) ([]string, error) {
	st, err := runRaw("-C", dir, "status", "--porcelain")
	if err != nil || st == "" {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(st, "\n"), "\n")
	for i, l := range lines {
		lines[i] = l[3:]
	}
	return lines, nil
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
	out, err := runRaw(args...)
	return strings.TrimSpace(out), err
}

// runRaw is runOut's untrimmed stdout, for output whose leading whitespace is data
// (status --porcelain's XY column).
func runRaw(args ...string) (string, error) { return runRawEnv(nil, args...) }

// runRawEnv is runRaw with extra env entries after the process env.
func runRawEnv(env []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Env = append(append(os.Environ(), env...), "LC_ALL=C") // callers that read git's text (merge-tree output) get English
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}      // a loop's Ctrl-C drains; it must not kill an effort's git step (ADR 0050)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			err = fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(ee.Stderr)))
		} else {
			err = fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
	}
	return string(out), err
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
// D2, ADR 0105): an existing branch is checked out after reuseEffort, so a
// re-dispatch rolls forward from a prior run's commits, or re-cuts when it has
// none; else it is created at container's tip, or at pin, a replace-mode leaf's
// pinned Tip, while the container still descends from it. A container rewritten
// off the pin already holds an earlier replace (published, its bead left open),
// so the recut starts from it. A var, as NewWorktree.
var NewEffortWorktree = func(id, container, pin string) (path string, teardown func(), err error) {
	branch := EffortBranch(id)
	return newTree(func(wt string) error {
		if _, err := run("rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
			Repo("").reuseEffort(id, container, pin)
			return AddWorktree(wt, branch)
		}
		at := container
		if pin != "" && IsAncestor(pin, container) {
			at = pin
		}
		if _, err := run("worktree", "add", "-b", branch, wt, at); err != nil {
			return err
		}
		return recordCut(Repo(""), id, at)
	})
}

// reuseEffort readies an existing effort/<id> for check-out (Spec mc-emoa): a
// branch with no commits of its own is re-cut to where a fresh cut would land (the
// pin while the container still descends from it, else the container's tip) and its
// cut ref re-recorded (the branch put back if the cut ref cannot be); one with own commits is left alone (ADR 0105) and, on a base
// other than that point, logged as effort.stale-base. It never fails: a branch -f
// refused because another tree holds the branch surfaces from the check-out as Busy
// (ADR 0056).
func (r Repo) reuseEffort(id, container, pin string) {
	branch := EffortBranch(id)
	tip, err := r.run("rev-parse", "refs/heads/"+branch)
	if err != nil {
		return
	}
	at := container
	if pin != "" && r.IsAncestor(pin, container) {
		at = pin
	}
	at, err = r.run("rev-parse", at+"^{commit}")
	if err != nil {
		return
	}
	cut, cutErr := r.run("rev-parse", "--verify", "--quiet", cutRefPrefix+id+"^{commit}")
	if (cutErr == nil && tip == cut) || (cutErr != nil && r.IsAncestor(tip, at)) {
		if tip != at {
			if _, err := r.run("branch", "-f", branch, at); err == nil {
				if recordCut(r, id, at) != nil {
					// the cut ref could not follow: put the branch back so the two agree
					r.run("branch", "-f", branch, tip)
				}
			}
		}
		return
	}
	base := cut
	if cutErr != nil {
		cut = ""
		base, _ = r.run("merge-base", tip, container)
	}
	if base != at {
		cTip, _ := r.run("rev-parse", container)
		slog.Warn("effort.stale-base", "bead", id, "branch", branch, "cut", cut, "container_tip", cTip)
	}
}

// cutRefPrefix holds an effort's recorded cut point, refs/mugicrew/cut/<id>, in the
// repo that holds effort/<id> (ADR 0264): where its own work starts, which Integrate
// rebases from instead of guessing it from the container.
const cutRefPrefix = "refs/mugicrew/cut/"

// recordCut writes id's cut ref at the commit its branch was cut at.
func recordCut(r Repo, id, at string) error {
	sha, err := r.run("rev-parse", at+"^{commit}")
	if err != nil {
		return err
	}
	_, err = r.run("update-ref", cutRefPrefix+id, sha)
	return err
}

// CommitsSinceCut counts branch's commits since id's recorded cut point
// (cut..branch): the effort's own work, however far its container has moved on.
// ok is false when no cut ref is recorded (an effort cut before ADR 0264), leaving
// the caller to estimate from the container.
func (r Repo) CommitsSinceCut(id, branch string) (n int, ok bool, err error) {
	_, err = r.run("rev-parse", "--verify", "--quiet", cutRefPrefix+id+"^{commit}")
	if ee := (*exec.ExitError)(nil); errors.As(err, &ee) && ee.ExitCode() == 1 {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	n, err = r.CommitsAhead(cutRefPrefix+id, branch)
	return n, err == nil, err
}

// dropCut deletes the cut ref of branch when it is an effort branch; absent is no error.
func (r Repo) dropCut(branch string) error {
	if id, ok := strings.CutPrefix(branch, effortPrefix); ok {
		_, err := r.run("update-ref", "-d", cutRefPrefix+id)
		return err
	}
	return nil
}

// newTree is NewWorktree's temp-parent cycle around add.
func newTree(add func(wt string) error) (path string, teardown func(), err error) {
	parent, err := os.MkdirTemp("", tempWorktreePrefix)
	if err != nil {
		return "", nil, err
	}
	// Born symlink-resolved, so git's porcelain, the cut-tree registry and a session's
	// @worktree all carry one spelling under a symlinked TMPDIR (macOS /var, /tmp).
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil {
		os.RemoveAll(parent)
		return "", nil, err
	}
	parent = resolved
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
	tmp := os.TempDir()
	if r, err := filepath.EvalSymlinks(tmp); err == nil { // the spelling newTree mints
		tmp = r
	}
	return filepath.Join(tmp, tempWorktreePrefix+key, "wt")
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
// refusal IS the contention interlock: git enforces one checkout per
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
func Busy(err error, branch string) (Holder, bool) { return Repo("").Busy(err, branch) }

// Busy is the package Busy with the holder looked up in the child.
func (r Repo) Busy(err error, branch string) (Holder, bool) {
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 128 {
		return Holder{}, false
	}
	h, ok, lerr := r.worktreeOn(branch)
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
// A var so tests of the loop-lock path run without the cwd's repo.
var CommonDir = func() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return run("rev-parse", "--path-format=absolute", "--git-common-dir")
	}
	commonMu.Lock()
	defer commonMu.Unlock()
	if commonCwd == cwd {
		return commonVal, nil
	}
	out, err := run("rev-parse", "--path-format=absolute", "--git-common-dir")
	if err == nil {
		commonCwd, commonVal = cwd, out
	}
	return out, err
}

// The TUI asks for the common dir several times per refresh (the tmux socket path,
// the event log, the loop lock); the answer for a given cwd does not change, so a
// success is memoized per cwd instead of costing a git process each time.
var (
	commonMu             sync.Mutex
	commonCwd, commonVal string
)

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
var Head = func(branch string) (string, error) {
	sha, err := run("rev-parse", "--verify", "--quiet", branch)
	if ee := (*exec.ExitError)(nil); errors.As(err, &ee) && ee.ExitCode() == 1 { // --quiet: 1 is "no such ref", 128 a real failure
		return "", fmt.Errorf("%w: %s", ErrMissingRef, branch)
	}
	return sha, err
}

// ErrMissingRef is Head's error for a ref that does not exist.
var ErrMissingRef = errors.New("git: no such ref")

// MissingRef reports whether err is Head's failure for a ref that does not exist,
// as opposed to a transient read failure.
func MissingRef(err error) bool { return errors.Is(err, ErrMissingRef) }

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

// DeleteBranch force-deletes a local branch (`git branch -D`), and an effort's cut
// ref with it; git refuses one a worktree still has checked out.
func DeleteBranch(name string) error { return Repo("").DeleteBranch(name) }

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

// IntegrateSubject is a squash integrate's commit subject:
// "<type>(<context>): <title> [<id>]", the scope dropped when context is "". The
// type comes from the bead's kind (docs for the charting leaves, feat for a build,
// fix for a critique or surgery, chore for any other), the title is used as is, and
// a title empty after trimming falls back to "integrate <id>". Pure.
func IntegrateSubject(kind, id, title, context string) string {
	if strings.TrimSpace(title) == "" {
		return "integrate " + id
	}
	typ := "chore"
	switch kind {
	case "survey", "grilling", "research", "prototype":
		typ = "docs"
	case "build":
		typ = "feat"
	case "critique", "surgery":
		typ = "fix"
	}
	if context != "" {
		typ += "(" + context + ")"
	}
	return typ + ": " + title + " [" + id + "]"
}

// LandSubject is a land commit's subject (and a Land MR's title):
// "<type>(<context>): <title> (<jiraKey>)", the scope dropped when context is "" and
// the parenthesised key when jiraKey is "". The type is feat for a checkpoint and docs
// for a chart; the title is used as is, and one empty after trimming falls back to
// "land <branch>". Pure.
func LandSubject(kind, title, context, jiraKey, branch string) string {
	if strings.TrimSpace(title) == "" {
		return "land " + branch
	}
	typ := "feat"
	if kind == "chart" {
		typ = "docs"
	}
	if context != "" {
		typ += "(" + context + ")"
	}
	s := typ + ": " + title
	if jiraKey != "" {
		s += " (" + jiraKey + ")"
	}
	return s
}

// TrailerBlock is the one trailer block every land commit carries under every
// strategy (ADR 0187 §1): "Tip: <tip>" (omitted when tip is ""), the "<Kind>: <id>"
// trailer, then "Graph: <graph>", as whole LF lines. Pure.
func TrailerBlock(tip, trailer, graph string) string {
	lines := []string{trailer, "Graph: " + graph}
	if tip != "" {
		lines = append([]string{TipPin(tip)}, lines...)
	}
	return strings.Join(lines, "\n")
}

// ParseGraph reads the Graph: line back from a commit message (TrailerBlock's
// reader twin); the last whole line wins, and a CRLF line or an absent one is
// (,false). Pure.
func ParseGraph(msg string) (graph string, ok bool) {
	for _, l := range strings.Split(msg, "\n") {
		if id, found := strings.CutPrefix(l, "Graph: "); found && id != "" && !strings.ContainsRune(id, '\r') {
			graph, ok = id, true
		}
	}
	return graph, ok
}

// readGraphRoot reads the Bead Graph's Dolt root hash; bd wires it (git cannot import bd).
var readGraphRoot = func() (string, error) { return "", errBdUnwired }

// errBdUnwired is the default of the bd reads when the binary never imported bd.
var errBdUnwired = errors.New("git: bd reads are not wired (import jayulabs.org/mugicrew/bd)")

// SetBdReads wires the bd-backed reads git needs: the Bead Graph's root hash and
// issue_prefix. bd calls it from init.
func SetBdReads(graphRoot, issuePrefix func() (string, error)) {
	readGraphRoot, readIssuePrefix = graphRoot, issuePrefix
}

var graphCache struct {
	sync.Mutex
	id string
}

// GraphID is the Bead Graph's Dolt root commit hash, read from bd once per
// process; a failed read is an error, never an empty Graph: line.
func GraphID() (string, error) {
	graphCache.Lock()
	defer graphCache.Unlock()
	if graphCache.id == "" {
		id, err := readGraphRoot()
		if err != nil {
			return "", err
		}
		graphCache.id = id
	}
	return graphCache.id, nil
}

// SetGraphID fakes GraphID for a test (a fixed id) and returns the restore.
func SetGraphID(id string) (restore func()) {
	graphCache.Lock()
	old := graphCache.id
	graphCache.id = id
	graphCache.Unlock()
	return func() { graphCache.Lock(); graphCache.id = old; graphCache.Unlock() }
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

// AnchorPin is the body line a gitlab direct Spec stamps with its anchor, the
// fetched Forge mainline tip: "Anchor: <sha>". Its own key, because Base: means a
// conflict bead's rebase base. ParseAnchor reads it back; absent is (,false).
func AnchorPin(sha string) string                   { return "Anchor: " + sha }
func ParseAnchor(body string) (sha string, ok bool) { return parsePin(body, "Anchor:") }

// ParseLine reads the first "<key> <value>" body line, e.g. "Checked:" or "MR:".
func ParseLine(body, key string) (string, bool) { return parsePin(body, key) }

// RecordedMR is the iid of the Landing's "MR: !<iid>" or "MR: <group/proj>!<iid>"
// line, 0 when it has none.
func RecordedMR(body string) int {
	_, iid := RecordedMRPath(body)
	return iid
}

// RecordedMRPath is RecordedMR plus the qualifying project path: "" for the bare
// "MR: !<iid>" form, which names the Workspace's project (ADR 0230).
func RecordedMRPath(body string) (path string, iid int) {
	rest, _ := ParseLine(body, "MR:")
	path, n, _ := strings.Cut(rest, "!")
	iid, _ = strconv.Atoi(n)
	return path, iid
}

// MRLine is the "MR: ..." body line for iid: "MR: !<iid>" for the Workspace's
// project (path ""), "MR: <path>!<iid>" for a nested repo's.
func MRLine(path string, iid int) string { return "MR: " + MRRef(path, iid) }

// MRLinkKey is the body key of an MR's web URL, the one Forge Review bodies use.
const MRLinkKey = "MR link:"

// MRLinkLine is the "MR link: <web_url>" body line recorded beside an "MR:" line.
func MRLinkLine(url string) string { return MRLinkKey + " " + url }

// RecordedMRLink is the web URL of the body's "MR link:" line, "" when it has none.
func RecordedMRLink(body string) string {
	url, _ := ParseLine(body, MRLinkKey)
	return url
}

// MRRef names an MR for a human: "!<iid>" for the Workspace's project (path ""),
// "<path>!<iid>" for a nested repo's.
func MRRef(path string, iid int) string { return fmt.Sprintf("%s!%d", path, iid) }

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
// so mainline moving on doesn't count: how surgery end tells a Checkpoint with
// nothing ahead of mainline from one with work (Spec D8). A missing branch errors.
func CommitsAhead(base, branch string) (int, error) { return Repo("").CommitsAhead(base, branch) }

// CommitsAhead is the package CommitsAhead in the child.
func (r Repo) CommitsAhead(base, branch string) (int, error) {
	out, err := r.run("rev-list", "--count", base+".."+branch)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(out)
}

// CommitsByTrailer counts the commits on base..branch with a message line that IS
// trailer, built like trailerLogArgs (whole-line anchored, QuoteMeta'd) so
// "Surgery: mc-s1" does not count mc-s1.1's squashes: one Surgery's own commits
// on a shared Checkpoint (Spec D8).
func CommitsByTrailer(base, branch, trailer string) (int, error) {
	return Repo("").CommitsByTrailer(base, branch, trailer)
}

// CommitsByTrailer is the package CommitsByTrailer in the child.
func (r Repo) CommitsByTrailer(base, branch, trailer string) (int, error) {
	out, err := r.run("rev-list", "--count", "--extended-regexp", "--grep="+trailerRegex(trailer), base+".."+branch)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(out)
}

// NewestByTrailer is the sha of the newest commit on branch with a message line that IS
// the trailer, "" when none carries it.
func NewestByTrailer(branch, trailer string) (string, error) {
	return Repo("").NewestByTrailer(branch, trailer)
}

// NewestByTrailer is the package NewestByTrailer in the child.
func (r Repo) NewestByTrailer(branch, trailer string) (string, error) {
	return r.run(trailerLogArgs(branch, trailer)...)
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
func squashCommitArgs(branch, subject, pin, trailer, graph, packet string) []string {
	if subject == "" { // a caller with no land subject keeps the old guard
		subject = "land " + branch
	}
	// --allow-empty: a fog-only Chart (all defog settled in bead notes, no ADR /
	// CONTEXT edit) lands an empty tree, but must still stamp its "Chart: <id>"
	// anchor — the landed base Zoro cuts from. Same empty-anchor rationale as a
	// no-artifact direct Spec (ADR 0014 §2); without it the squash fails "nothing
	// to commit" and the Chart can never land. The TipPin line records the
	// container tip that landed, which LandedTip reads back.
	//
	// A Decision Packet (the Approval's body, pin stripped) is the middle paragraph,
	// ahead of the Tip:/trailer paragraph that stays last. --cleanup=whitespace keeps
	// its markdown "#" headings, which a configured commit.cleanup=strip would eat;
	// an empty packet leaves today's argv untouched.
	block := TrailerBlock(pin, trailer, graph)
	if packet == "" {
		return []string{"commit", "--allow-empty", "-m", subject, "-m", block}
	}
	return []string{"commit", "--allow-empty", "--cleanup=whitespace", "-m", subject, "-m", packet, "-m", block}
}

// DecisionPacket is an Approval description's Decision Packet: the body with its
// leading Commit: pin line removed (the land commit's Tip: line already records what
// landed), "" for a pin-only or pre-packet Approval (first line not a pin). Pure.
func DecisionPacket(approvalBody string) string {
	body := strings.TrimSpace(approvalBody)
	first, rest, _ := strings.Cut(body, "\n")
	if strings.HasPrefix(first, "Commit:") {
		return strings.TrimSpace(rest)
	}
	// ponytail: a pin-less first line marks a pre-packet Approval (spawn text, pin on
	// line two); Luffy always writes the pin first (FR10), so no packet.
	return ""
}

func ffMainlineArgs(main, branch, oldBase string) []string {
	return []string{"update-ref", "refs/heads/" + main, branch, oldBase}
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
	// Set by HolderAcross: Rebasing is a live tree stopped mid-rebase on the branch;
	// Dir and Host are a child holder's declared dir and the host tree it sits in.
	Rebasing  bool
	Dir, Host string
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

// RebasingOn is the linked worktree stopped mid-rebase on branch, ok false if
// none: its HEAD is detached, so WorktreeOn misses it (ADR 0127). It scans the
// common dir's worktrees/*/rebase-{merge,apply}/head-name for refs/heads/<branch>.
// A tree whose dir is gone is Prunable.
var RebasingOn = func(branch string) (h Holder, ok bool, err error) {
	return Repo("").RebasingOn(branch)
}

// Rebasing maps each linked worktree stopped mid-rebase to the branch it is
// rewriting, read from the common dir's worktrees/*/rebase-{merge,apply}/head-name.
func Rebasing() (map[string]string, error) { return Repo("").Rebasing() }

// Worktree is one checkout in `git worktree list`: its path and the branch it has
// checked out, "" when its HEAD is detached.
type Worktree struct {
	Path, Branch string
}

// Worktrees lists every worktree of the repo, the main checkout first.
func Worktrees() ([]Worktree, error) { return Repo("").Worktrees() }

// PruneWorktrees is `git worktree prune`: it forgets worktrees whose directory is
// gone, freeing the branch a prunable one still holds (ADR 0041 §3).
func PruneWorktrees() error { return Repo("").PruneWorktrees() }

// ffMainline advances Mainline to the just-squashed branch. If Mainline is checked
// out in a worktree it fast-forwards THROUGH that worktree (mergeFFOnlyArgs), so the
// checkout's index and working tree move with the ref instead of being stranded.
// If Mainline is checked out nowhere — the per-effort topology where the
// land runs on the only checkout — it moves the ref with the update-ref CAS
// (ffMainlineArgs). Both paths fail loudly when a concurrent land already advanced
// Mainline past base.
func ffMainline(branch, base string) error { return Repo("").ffMainline(branch, base) }

// ffMainline is the package ffMainline on the child's trunk and main checkout.
func (r Repo) ffMainline(branch, base string) error {
	list, err := r.run(worktreeListArgs()...)
	if err != nil {
		return err
	}
	main := r.mainline()
	if dir := worktreeOn(list, main); dir != "" {
		_, err := r.run(mergeFFOnlyArgs(dir, branch)...)
		return err
	}
	_, err = r.run(ffMainlineArgs(main, branch, base)...)
	return err
}

// trailerLogArgs is the pure argv that re-derives a landed commit by its
// trailer: the newest commit on ref with a message line that IS the trailer.
// Anchored to the whole line (git --grep matches per line) so a short id can't
// resolve to a longer or dotted id's land; QuoteMeta keeps a bead id's
// metachars literal, and ERE reads its backslash escapes as literals.
func trailerLogArgs(ref, trailer string) []string {
	return []string{"log", ref, "--extended-regexp", "--grep=" + trailerRegex(trailer), "--format=%H", "-n", "1"}
}

func trailerRegex(trailer string) string { return "^" + regexp.QuoteMeta(trailer) + "$" }

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
func rebaseInProgress() bool { return Repo("").rebaseInProgress() }

func (r Repo) rebaseInProgress() bool {
	for _, name := range []string{"rebase-merge", "rebase-apply"} {
		p, err := r.run("rev-parse", "--path-format=absolute", "--git-path", name)
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
// branch adds that the locked base already holds is *NumberClash, both before anything
// is touched.
//
// Land never writes the container (Spec D5, ADR 0102 §8): it pins the
// container's tip, detaches the cwd there (run it from a route.Detached tree; any
// other tree is left detached), rebases and squashes on that detached HEAD, and
// refuses if the container moved off the pin before it moves main. A failed land so
// leaves the container at the pin, which a conflict's filer reads as its Tip and an
// Approval's Commit: pin still matches; nothing needs restoring.
//
// strategy is the write leg run under the lock (ADR 0069 §1): the lock, the
// LandedBase read and the trailer stay common to every strategy. Its outcomes:
// nil error landed, *RebaseConflict or *NumberClash conflict, ErrLandPending
// pending, anything else (including *MainDirty) an error.
func Land(branch, trailer string, strategy LandStrategy) (sha string, err error) {
	return AnchorIfAbsent(trailer, LandedBase, func(trailer string) (string, error) { return strategy(branch, trailer) })
}

// LandStrategy is Land's write leg: (branch, trailer) to the landed sha.
type LandStrategy func(branch, trailer string) (sha string, err error)

// ErrLandPending is a LandStrategy's "submitted, not landed yet" outcome, matched
// with errors.Is (the gitlab strategy returns *LandPending, which is one).
var ErrLandPending = errors.New("git: land pending")

// The land_strategy values LandStrategyByName accepts.
const (
	LandStrategyLocal  = "local"
	LandStrategyGitlab = "gitlab"
)

// LandStrategyByName maps a land_strategy value to its strategy; any other name
// is an error naming it.
func LandStrategyByName(name string) (LandStrategy, error) {
	switch name {
	case LandStrategyLocal:
		return LandLocal, nil
	case LandStrategyGitlab:
		return landGitlab, nil
	}
	return nil, fmt.Errorf("git: unknown land strategy %q (want local or gitlab)", name)
}

// LandStrategyFor is the land_strategy seam taking the repo: the empty Repo is
// LandStrategyByName, and local and gitlab both act on r (Spec D3), so no caller
// gets a cwd-bound strategy for a child.
func LandStrategyFor(name string, r Repo, subject string) (LandStrategy, error) {
	switch name {
	case LandStrategyLocal:
		return r.landLocal(subject), nil
	case LandStrategyGitlab:
		return r.landGitlab(subject), nil
	}
	return LandStrategyByName(name)
}

// landLocal is LandLocal in the child, a Compose plus LandSameBase run through the
// handle (the Repo must be a tree path for the compose to leave main's checkout alone).
// The squash commit's subject is subject ("" keeps "land <branch>").
func (r Repo) landLocal(subject string) LandStrategy {
	return func(branch, trailer string) (string, error) {
		composed, base, pin, err := r.Compose(branch, trailer, "", subject)
		if err != nil {
			return "", err
		}
		return r.LandSameBase(composed, base, pin)(branch, trailer)
	}
}

// Land is the package Land on the child's trunk: trailer-idempotent under the
// Workspace's land lock (never a second one in the child).
func (r Repo) Land(branch, trailer string, strategy LandStrategy) (string, error) {
	return AnchorIfAbsent(trailer, r.landedBase, func(trailer string) (string, error) { return strategy(branch, trailer) })
}

// ErrNestedGitlab is the typed refusal of a nested repo under land_strategy=gitlab:
// only the local-only Repo.revert leg returns it (ADR 0235); no gitlab
// path reaches it, so it is a backstop that writes nothing to the child's main.
var ErrNestedGitlab = errors.New("git: land_strategy gitlab never moves a nested repo's local main (local-only backstop)")

// refuseNestedGitlab is ErrNestedGitlab for a child Repo under gitlab, nil for the
// Workspace's own repo or under local.
func (r Repo) refuseNestedGitlab() error {
	if r == "" {
		return nil
	}
	name, err := LandStrategyName()
	if err != nil {
		return err
	}
	if name == LandStrategyGitlab {
		return ErrNestedGitlab
	}
	return nil
}

// ForgeRemote is the remote the Forge's mainline lives on: branch.main.remote
// (the configured trunk's), else origin.
func ForgeRemote() string { return Repo("").ForgeRemote() }

// ForgeRemote is the package ForgeRemote on the child's own config (its trunk's
// branch.<main>.remote, else origin), read through its repo handle.
func (r Repo) ForgeRemote() string {
	if v, err := r.run("config", "--get", "branch."+r.mainline()+".remote"); err == nil && v != "" {
		return v
	}
	return "origin"
}

// MainDiverged is FetchMainline's typed refusal: local main is not an ancestor of
// the Forge's, so it cannot be fast-forwarded. Nothing was moved.
type MainDiverged struct {
	Local, Forge string
}

func (e *MainDiverged) Error() string {
	return fmt.Sprintf("git: local main %s has diverged from the Forge's main %s", e.Local, e.Forge)
}

// MainAhead is FetchMainline's typed refusal for a local main strictly ahead of the
// Forge's: Commits are the unpushed one-line subjects, newest first. Nothing was moved.
type MainAhead struct {
	Remote, Main string
	Commits      []string
}

func (e *MainAhead) Error() string {
	return fmt.Sprintf("git: local %s is %d commit(s) ahead of %s/%s, not diverged; unpushed:\n  %s\n"+
		"push them through the Workspace's usual flow, or, after saving anything wanted, run `git reset --hard %s/%s` on the main worktree",
		e.Main, len(e.Commits), e.Remote, e.Main, strings.Join(e.Commits, "\n  "), e.Remote, e.Main)
}

// FetchMainline fetches the Forge's mainline from remote and fast-forwards local
// main to it under the land lock, through ffMainline (main's worktree when checked
// out, else the update-ref CAS). Equal is a no-op, so every reader of local main
// then sees the Forge's truth; a local main strictly ahead is *MainAhead, one with
// commits on both sides *MainDiverged.
func FetchMainline(remote string) (sha string, err error) {
	return withLandLock(func() (string, error) { return Repo("").fetchFF(remote) })
}

// FetchMainline is the package FetchMainline on the child's Forge remote and main,
// under the Workspace's land lock (no land.lock is ever created in a child).
func (r Repo) FetchMainline() (string, error) {
	return withLandLock(func() (string, error) { return r.fetchFF(r.ForgeRemote()) })
}

// fetchFF is FetchMainline's leg without the land lock, for a caller already holding it.
func fetchFF(remote string) (string, error) { return Repo("").fetchFF(remote) }

func (r Repo) fetchFF(remote string) (string, error) {
	main := r.mainline()
	// an explicit tracking ref, not FETCH_HEAD, which a concurrent fetch in this repo overwrites
	tracking := "refs/remotes/" + remote + "/" + main
	if _, err := r.runForge(remote, "fetch", "--quiet", remote, "+refs/heads/"+main+":"+tracking); err != nil {
		return "", err
	}
	forge, err := r.run("rev-parse", tracking+"^{commit}")
	if err != nil {
		return "", err
	}
	local, err := r.run("rev-parse", "refs/heads/"+main)
	if err != nil {
		return "", err
	}
	switch {
	case forge == local:
	case r.IsAncestor(local, forge):
		if err := r.ffMainline(forge, local); err != nil {
			return "", err
		}
	case r.IsAncestor(forge, local):
		subjects, err := r.run("log", "--format=%s", forge+".."+local)
		if err != nil {
			return "", err
		}
		return "", &MainAhead{Remote: remote, Main: main, Commits: strings.Split(subjects, "\n")}
	default:
		return "", &MainDiverged{Local: local, Forge: forge}
	}
	return forge, nil
}

// ForgeProject is the GitLab project the Forge remote's URL names.
func ForgeProject() (forge.Project, error) { return Repo("").ForgeProject() }

// ForgeProject is the package ForgeProject for the child's own Forge remote.
func (r Repo) ForgeProject() (forge.Project, error) {
	remoteURL, err := r.run("config", "--get", "remote."+r.ForgeRemote()+".url")
	if err != nil {
		return forge.Project{}, err
	}
	return forge.ParseRemote(remoteURL)
}

// DifferentHost is a nested repo's project on another GitLab host than the
// Workspace's: unsupported (one credential set per Workspace), refused at loop start
// and again at Landing time.
type DifferentHost struct {
	Workspace, Child forge.Project
}

func (e *DifferentHost) Error() string {
	return fmt.Sprintf("git: nested repo project %s is on host %s, not the Workspace's %s; only same-host child projects are supported (README, \"GitLab land strategy\")",
		e.Child.ID, e.Child.Host, e.Workspace.Host)
}

// CheckSameHost is *DifferentHost when child's host is not the Workspace's.
func CheckSameHost(workspace, child forge.Project) error {
	if child.Host != workspace.Host {
		return &DifferentHost{Workspace: workspace, Child: child}
	}
	return nil
}

// SameHostProject is r's Forge project through projectOf; a child's (r != "") refuses
// another host than the Workspace's before any push or MR call (D10).
func SameHostProject(r Repo, projectOf func(Repo) (forge.Project, error)) (forge.Project, error) {
	proj, err := projectOf(r)
	if err == nil && r != "" {
		var ws forge.Project
		if ws, err = projectOf(""); err == nil {
			err = CheckSameHost(ws, proj)
		}
	}
	return proj, err
}

// LandPending is the gitlab strategy's ErrLandPending outcome with where the land
// waits: the MR's iid and absolute web URL, and whether this tick pushed the pin and
// opened the MR.
type LandPending struct {
	MR             int
	WebURL         string
	Pushed, Opened bool
}

func (e *LandPending) Error() string {
	return fmt.Sprintf("%v: waiting on MR !%d", ErrLandPending, e.MR)
}
func (e *LandPending) Is(t error) bool { return t == ErrLandPending }

// landGitlab is the gitlab strategy's write leg, run under the land lock: force-push
// the container's tip (the Commit: pin) to its branch on the Forge unless it is already
// there, reuse the branch's open MR or create one (a 409 lost the race: re-list), assert
// the trailer block in its description, and return *LandPending. It never merges or
// arms auto-merge: the merge is a human's act (ADR 0189). The plain --force push
// overwriting a Forge-side Rebase is intended: it carries a re-Approved pin.
func landGitlab(branch, trailer string) (string, error) {
	return Repo("").landGitlab("")(branch, trailer)
}

// landGitlab is the package landGitlab on the child's branch, remote and project; the
// MR is titled subject ("" keeps "land <branch>").
func (r Repo) landGitlab(subject string) LandStrategy {
	return func(branch, trailer string) (string, error) {
		pin, err := r.run("rev-parse", "refs/heads/"+branch)
		if err != nil {
			return "", err
		}
		title := cmp.Or(subject, "land "+branch)
		return r.landGitlabAt(branch, MRPresentation{Title: title}, trailer, pin, true)
	}
}

// MRPresentation is what a Land MR shows a human: its title and the body above the
// trailer block. An empty Body gives a trailer-only description.
type MRPresentation struct{ Title, Body string }

// description is the MR description: the human body, a blank line, then block (always last).
func (p MRPresentation) description(block string) string {
	if p.Body == "" {
		return block + "\n"
	}
	return strings.TrimRight(p.Body, "\n") + "\n\n" + block + "\n"
}

// PinnedGitlab is the gitlab strategy for a Landing's Commit: pin: it pushes and
// stamps that sha, never the branch head read at land time, so a branch that moved
// after the Landing's pin==head check cannot put an unreviewed tip in the MR (Spec FR 9).
// Acting on r: its push, ls-remote, MR and target mainline are r's and its project's.
func (r Repo) PinnedGitlab(pin string, pres MRPresentation) LandStrategy {
	return func(branch, trailer string) (string, error) {
		return r.landGitlabAt(branch, pres, trailer, pin, true)
	}
}

// landGitlabAt pushes pin to branch and opens or reuses its MR presented as pres. withTip
// is whether the trailer block carries a Tip: line (pin): a Land MR's does, and a reused
// one gets its title and description rewritten in one PUT; a revert MR's has none, and a
// reused one only has its trailer block repaired (ADR 0213).
func (r Repo) landGitlabAt(branch string, pres MRPresentation, trailer, pin string, withTip bool) (string, error) {
	remote := r.ForgeRemote()
	proj, err := r.ForgeProject()
	if err != nil {
		return "", err
	}
	graph, err := GraphID()
	if err != nil {
		return "", err
	}
	tip := ""
	if withTip {
		tip = pin
	}
	block := TrailerBlock(tip, trailer, graph)
	pending := &LandPending{}
	checked := false // the project read happens once, and only before a push or create
	if out, err := r.runForge(remote, "ls-remote", remote, "refs/heads/"+branch); err != nil {
		return "", err
	} else if f := strings.Fields(out); len(f) == 0 || f[0] != pin {
		if err := checkForgeProject(proj); err != nil {
			return "", err
		}
		checked = true
		if _, err := r.runForge(remote, "push", "--force", remote, pin+":refs/heads/"+branch); err != nil {
			return "", err
		}
		pending.Pushed = true
	}
	reuse := func(mr *forge.MR) error {
		if withTip {
			return forge.SetMR(proj, mr.IID, pres.Title, pres.description(block))
		}
		return assertBlock(proj, mr, block)
	}
	mr, err := forge.FindOpenMR(proj, branch)
	if err != nil {
		return "", err
	}
	if mr != nil {
		if err := reuse(mr); err != nil {
			return "", err
		}
		pending.MR, pending.WebURL = mr.IID, mr.WebURL
		return "", pending
	}
	if !checked {
		if err := checkForgeProject(proj); err != nil {
			return "", err
		}
	}
	assignee, err := forge.CurrentUserID(proj)
	if err != nil {
		return "", err
	}
	mr, err = forge.CreateMR(proj, branch, r.mainline(), pres.Title, pres.description(block), assignee)
	switch {
	case errors.Is(err, forge.ErrDuplicate): // lost the race: re-list
		if mr, err = forge.FindOpenMR(proj, branch); err != nil {
			return "", err
		}
		if mr == nil {
			return "", fmt.Errorf("git: Forge reports a duplicate MR for %s but lists none open", branch)
		}
		if err := reuse(mr); err != nil {
			return "", err
		}
	case err != nil:
		return "", err
	default:
		pending.Opened = true
	}
	pending.MR, pending.WebURL = mr.IID, mr.WebURL
	return "", pending
}

// PushRevertMR pushes the revert pin to rollback/<id> and opens or reuses its MR,
// titled with the revert commit's own subject and described by the tip-less trailer
// block (ADR 0213). It checks the Forge project before any push or create.
func (r Repo) PushRevertMR(id, trailer, pin string) (*LandPending, error) {
	title, err := r.run("log", "-1", "--format=%s", pin)
	if err != nil {
		return nil, err
	}
	_, err = r.landGitlabAt("rollback/"+id, MRPresentation{Title: title}, trailer, pin, false)
	var p *LandPending
	if errors.As(err, &p) {
		return p, nil
	}
	return nil, err
}

// assertBlock repairs the trailer block in the MR's description when it is missing or
// broken: stale trailer lines at the body's end are dropped, the fresh block follows a
// blank line, and every other line stays as the human left it.
func assertBlock(proj forge.Project, mr *forge.MR, block string) error {
	if hasLines(mr.Description, block) {
		return nil
	}
	lines := strings.Split(strings.TrimRight(mr.Description, "\n"), "\n")
	for len(lines) > 0 && isTrailerLine(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	body := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	if body != "" {
		body += "\n\n"
	}
	return forge.SetDescription(proj, mr.IID, body+block+"\n")
}

// isTrailerLine is a Tip:, Graph: or own-kind trailer line, the lines TrailerBlock writes.
func isTrailerLine(l string) bool {
	k, _, ok := strings.Cut(l, ": ")
	return ok && (k == "Tip" || k == "Graph" || slices.Contains(ownKinds, k))
}

// AssertMRDescription is a pending tick's whole write against a recorded, opened MR:
// the trailer block for pin ("" for a revert MR: no Tip: line), rewritten only when mr.Description lacks it. It reads
// nothing from the Forge (the caller already holds mr).
func (r Repo) AssertMRDescription(mr *forge.MR, pin, trailer string) error {
	proj, err := r.ForgeProject()
	if err != nil {
		return err
	}
	graph, err := GraphID()
	if err != nil {
		return err
	}
	return assertBlock(proj, mr, TrailerBlock(pin, trailer, graph))
}

// checkForgeProject refuses a project whose squash settings would drop the trailer
// (Spec FR 37). Run at loop start and on the write leg before a push or create,
// never on a plain pending tick.
func checkForgeProject(proj forge.Project) error {
	s, err := forge.ReadSettings(proj)
	if err != nil {
		return err
	}
	return forge.ValidateSettings(s)
}

// readIssuePrefix reads the graph's issue_prefix; bd wires it (see readGraphRoot).
var readIssuePrefix = func() (string, error) { return "", errBdUnwired }

// DeclaredRepos is the live repo = table, context name to dir (nil with none
// declared, before any git call); contexts wires it (git cannot import contexts).
var DeclaredRepos = func() map[string]string { return nil }

// PreflightGitlab is `loop start`'s gitlab check (Spec FR 44, D9): for the Workspace's
// project and each declared nested repo's (deduped by project), check glab auth, fetch
// mainline, check the squash settings, then refuse with *ForeignLanding when mainline
// holds a trailer carrying our issue_prefix and another graph's Graph:. A child's
// failure, or a child on another host, is wrapped to name its context. No-op under
// local: no glab call, no fetch.
func PreflightGitlab() error {
	if name, err := LandStrategyName(); err != nil || name != LandStrategyGitlab {
		return err
	}
	entries, err := GitlabProjects()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := preflightProject(e.Repo, e.Project); err != nil {
			if e.Context != "" {
				err = fmt.Errorf("context %s: %w", e.Context, err)
			}
			return err
		}
	}
	return nil
}

// GitlabProject is one Forge project a gitlab Workspace lands to: the owning context
// ("" for the Workspace itself) and the repo whose mainline it is.
type GitlabProject struct {
	Context string
	Repo    Repo
	Project forge.Project
}

// GitlabProjects is the Workspace's project followed by each declared nested repo's, deduped
// by project ID, a child on another host refused (wrapped to name its context). Local config
// reads only: no glab call, no fetch. Shared by PreflightGitlab and doctor.
func GitlabProjects() ([]GitlabProject, error) {
	proj, err := ForgeProject()
	if err != nil {
		return nil, err
	}
	out := []GitlabProject{{Repo: Repo(""), Project: proj}}
	root, seen := "", map[string]bool{proj.ID: true}
	repos := DeclaredRepos()
	for _, name := range slices.Sorted(maps.Keys(repos)) {
		if root == "" {
			if root, err = RepoRoot(); err != nil {
				return nil, err
			}
		}
		child := Repo(filepath.Join(root, repos[name]))
		cp, err := child.ForgeProject()
		if err == nil {
			err = CheckSameHost(proj, cp)
		}
		if err != nil {
			return nil, fmt.Errorf("context %s: %w", name, err)
		}
		if seen[cp.ID] {
			continue
		}
		seen[cp.ID] = true
		out = append(out, GitlabProject{name, child, cp})
	}
	return out, nil
}

// preflightProject is one project's preflight over repo r's mainline.
func preflightProject(r Repo, proj forge.Project) error {
	if err := forge.CheckAuth(proj.Host); err != nil {
		return err
	}
	if _, err := r.FetchMainline(); err != nil {
		return err
	}
	if err := checkForgeProject(proj); err != nil {
		return err
	}
	prefix, err := readIssuePrefix()
	if err != nil {
		return err
	}
	ours, err := GraphID()
	if err != nil {
		return err
	}
	// no --first-parent: a squash under the merge method is a merge's second parent.
	out, err := r.run("log", r.mainline(), "--extended-regexp", "--grep=^("+strings.Join(ownKinds, "|")+"): "+regexp.QuoteMeta(prefix)+"-", "--format=%H%x00%B%x1e")
	if err != nil {
		return err
	}
	for _, rec := range strings.Split(out, "\x1e") {
		sha, msg, ok := strings.Cut(strings.TrimSpace(rec), "\x00")
		if !ok {
			continue
		}
		if g, ok := ParseGraph(msg); ok && g != ours {
			return fmt.Errorf("loop start refused (README, \"GitLab land strategy\"): %w",
				&ForeignLanding{Trailer: ownTrailer(msg, prefix), Ours: ours, Theirs: g, Sha: sha})
		}
	}
	return nil
}

// ownKinds are the trailer keys that carry a Graph: line (FR 38): lands, anchors, reverts.
var ownKinds = []string{"Chart", "Checkpoint", "Spec", "Reverts-Chart", "Reverts-Checkpoint"}

// ownTrailer is msg's "<Kind>: <prefix>-…" line.
func ownTrailer(msg, prefix string) string {
	for _, l := range strings.Split(msg, "\n") {
		if k, id, ok := strings.Cut(l, ": "); ok && slices.Contains(ownKinds, k) && strings.HasPrefix(id, prefix+"-") {
			return l
		}
	}
	return ""
}

// hasLines reports whether every line of block is a whole LF line of text (a \r
// before the LF breaks it).
func hasLines(text, block string) bool {
	have := strings.Split(text, "\n")
	for _, l := range strings.Split(block, "\n") {
		if !slices.Contains(have, l) {
			return false
		}
	}
	return true
}

// PinMoved is the same-base land's typed refusal: the container is no longer at
// the pin the squash was composed from. Nothing was written to mainline.
type PinMoved struct {
	Branch, Pin, Now string
}

func (e *PinMoved) Error() string {
	return fmt.Sprintf("git: %s moved off its pinned tip %s (now %s); nothing landed, re-land", e.Branch, e.Pin, e.Now)
}

// BaseMoved is the same-base land's typed refusal: mainline is no longer at the
// base the squash was composed on. Nothing was written to mainline.
type BaseMoved struct {
	Base, Now string
}

func (e *BaseMoved) Error() string {
	return fmt.Sprintf("git: mainline moved off base %s (now %s); nothing landed, re-compose", e.Base, e.Now)
}

// Compose builds, without the land lock, the squash LandLocal would land from the
// current (pin, base): main clean, base and pin read, numbered-file clash check, detach
// at the pin, rebase onto base, soft reset, squash commit. Run it in a detached
// reconcile tree; it moves neither mainline nor the container (ADR 0150).
func Compose(branch, trailer, packet, subject string) (composed, base, pin string, err error) {
	return Repo("").Compose(branch, trailer, packet, subject)
}

// Compose is the package Compose in the child; run it on a Repo built from the
// child's detached land-tree path (<tree>/<dir>), where the rebase and squash happen.
// Main-clean and the ADR-clash check read the child, never the Workspace.
func (r Repo) Compose(branch, trailer, packet, subject string) (composed, base, pin string, err error) {
	if err := r.mainClean(); err != nil {
		return "", "", "", err
	}
	if base, err = r.ResolveProbeBase(); err != nil {
		return "", "", "", err
	}
	if pin, err = r.run("rev-parse", "refs/heads/"+branch); err != nil {
		return "", "", "", err
	}
	clashes, err := r.numberClashes(pin, base)
	if err != nil {
		return "", "", "", err
	}
	if len(clashes) > 0 {
		return "", "", "", &NumberClash{Clashes: clashes, Base: base}
	}
	if _, err := r.run(detachArgs(pin)...); err != nil {
		return "", "", "", err
	}
	if _, err := r.run(landRebaseArgs(base)...); err != nil {
		// A real conflict leaves an in-progress rebase to abort; a clean failure
		// (dirty tree, bad base) leaves none, so it's not a conflict — surface it
		// raw rather than mislabel it *RebaseConflict.
		if !r.rebaseInProgress() {
			return "", "", "", err
		}
		_, _ = r.run("rebase", "--abort")
		return "", "", "", &RebaseConflict{Base: base}
	}
	if _, err := r.run(softResetArgs(base)...); err != nil {
		return "", "", "", err
	}
	graph, err := GraphID()
	if err != nil {
		return "", "", "", err
	}
	if _, err := r.run(squashCommitArgs(branch, subject, pin, trailer, graph, packet)...); err != nil {
		return "", "", "", err
	}
	if composed, err = r.run("rev-parse", "HEAD"); err != nil {
		return "", "", "", err
	}
	return composed, base, pin, nil
}

// LandSameBase is the write leg that lands a Compose result: under the land lock it
// re-checks main clean, the container at pin and mainline at base (*PinMoved,
// *BaseMoved otherwise), then fast-forwards mainline to composed.
func LandSameBase(composed, base, pin string) LandStrategy {
	return Repo("").LandSameBase(composed, base, pin)
}

// LandSameBase is the package LandSameBase on the child's trunk and main checkout.
func (r Repo) LandSameBase(composed, base, pin string) LandStrategy {
	return func(branch, _ string) (string, error) {
		if err := r.mainClean(); err != nil {
			return "", err
		}
		// The lock keeps integrate off the container; this catches any other writer.
		if cur, err := r.run("rev-parse", "refs/heads/"+branch); err != nil || cur != pin {
			return "", cmp.Or(err, error(&PinMoved{Branch: branch, Pin: pin, Now: cur}))
		}
		if cur, err := r.run("rev-parse", r.mainline()); err != nil || cur != base {
			return "", cmp.Or(err, error(&BaseMoved{Base: base, Now: cur}))
		}
		if err := r.ffMainline(composed, base); err != nil { // ff through main's worktree, else CAS update-ref
			return "", err
		}
		return composed, nil
	}
}

// LandLocal is the local strategy: Compose then LandSameBase in one locked call,
// with no Decision Packet (a Checkpoint's goes through gatedLand's composeLanding).
func LandLocal(branch, trailer string) (sha string, err error) {
	return Repo("").landLocal("")(branch, trailer)
}

// LandedTip is the container tip the land squash sha pinned (its TipPin line), ok
// false for a squash without one.
func LandedTip(sha string) (tip string, ok bool) {
	body, err := run("log", "-1", "--format=%B", sha)
	if err != nil {
		return "", false
	}
	// The trailer paragraph is last; an earlier "Tip:" prose line in the packet must not win.
	lines := strings.Split(body, "\n")
	slices.Reverse(lines)
	return ParseTip(strings.Join(lines, "\n"))
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
// nothing. Not idempotent: a re-run finds the branch gone. Under gitlab it opens an
// MR instead and returns its URL (landLineageGitlab).
func LandLineage(branch string) (sha string, err error) {
	if name, err := LandStrategyName(); err != nil {
		return "", err
	} else if name == LandStrategyGitlab {
		return landLineageGitlab(branch)
	}
	return withLandLock(func() (string, error) {
		if err := mainClean(); err != nil {
			return "", err
		}
		base, err := run("rev-parse", Mainline())
		if err != nil {
			return "", err
		}
		tip, err := checkLineage(branch, base)
		if err != nil {
			return "", err
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

// checkLineage runs LandLineage's refusals for branch against base, the main tip
// (local, or the fetched Forge's under gitlab), and returns the branch's one commit.
func checkLineage(branch, base string) (string, error) {
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
	return tip, nil
}

// landLineageGitlab is LandLineage under gitlab (ADR 0210): check the Forge project,
// fetch and fast-forward local main to the Forge's (a diverged main is refused) and run
// the refusals against it under the land lock, then reuse the branch's open MR or push
// it and open one whose title and description are the commit's own subject and body,
// so GitLab's one-commit squash renders the same message. It returns the MR's URL and
// keeps the local branch; nothing watches the MR. The refusals run before the MR
// lookup (FR 49), so a re-run after the Forge's main moved refuses *MainMoved
// instead of printing the open MR's URL.
func landLineageGitlab(branch string) (string, error) {
	proj, err := ForgeProject()
	if err != nil {
		return "", err
	}
	if err := checkForgeProject(proj); err != nil {
		return "", err
	}
	remote := ForgeRemote()
	tip, err := withLandLock(func() (string, error) {
		if err := mainClean(); err != nil {
			return "", err
		}
		base, err := fetchFF(remote)
		if err != nil {
			return "", err
		}
		return checkLineage(branch, base)
	})
	if err != nil {
		return "", err
	}
	mr, err := forge.FindOpenMR(proj, branch)
	if err != nil {
		return "", err
	}
	if mr != nil {
		return mr.WebURL, nil
	}
	title, err := run("log", "-1", "--format=%s", tip)
	if err != nil {
		return "", err
	}
	body, err := run("log", "-1", "--format=%b", tip)
	if err != nil {
		return "", err
	}
	if _, err := Repo("").runForge(remote, "push", "--force", remote, tip+":refs/heads/"+branch); err != nil {
		return "", err
	}
	mr, err = forge.CreateMR(proj, branch, Mainline(), title, body, 0)
	if errors.Is(err, forge.ErrDuplicate) { // lost the race: re-list
		mr, err = forge.FindOpenMR(proj, branch)
		if err == nil && mr == nil {
			err = forge.ErrDuplicate
		}
	}
	if err != nil {
		return "", err
	}
	return mr.WebURL, nil
}

// RevertsTrailer is the trailer a Rollback's revert commit carries (ADR 0047 §5):
// the land trailer prefixed "Reverts-" ("Reverts-Checkpoint: <id>"). LandedBase's
// anchored ^…$ grep keeps the two apart, so a revert never reads as a land.
func RevertsTrailer(kind, id string) string { return "Reverts-" + TrailerFor(kind, id) }

// MainlineCommit is one first-parent mainline commit as MainlineLog reads it: its
// land kind and trailer id (Kind, Lands), revert trailer id (Reverts) and subject.
// Kind and Lands are "" for a revert or a plain commit; Reverts is "" when absent.
type MainlineCommit struct {
	SHA     string
	Time    time.Time
	Kind    string // the land trailer's kind: "checkpoint", "chart" or "spec"
	Subject string // the message's first line
	Lands   string // "<Kind>: <id>" trailer's id
	Reverts string // "Reverts-<Kind>: <id>" trailer's id
	Context string // MainlineLogAcross: the declared repo context this commit landed in, "" for the Workspace
}

// mainlineLogArgs is the pure argv for MainlineLog: the newest n first-parent
// commits, each as sha␟unix-time␟body␞.
func mainlineLogArgs(mainline string, n int) []string {
	return []string{"log", mainline, "--first-parent", "-n", fmt.Sprint(n), "--format=%H%x1f%ct%x1f%B%x1e"}
}

// landKinds are the kinds whose land trailer MainlineLog reads.
var landKinds = []string{"checkpoint", "chart", "spec"}

// MainlineLog reads the newest n first-parent mainline commits, newest first,
// with every land kind's trailer, the Checkpoint revert trailer and the subject
// (the History tab's source). Read-only: it takes no land lock. A trailer is a
// whole message line, as LandedBase greps it; the last one wins, since
// TrailerBlock writes the trailer at the message's end.
func MainlineLog(n int) ([]MainlineCommit, error) { return Repo("").MainlineLog(n) }

// MainlineLog is the package MainlineLog in the child, through the child's trunk.
func (r Repo) MainlineLog(n int) ([]MainlineCommit, error) {
	out, err := r.run(mainlineLogArgs(r.mainline(), n)...)
	if err != nil {
		return nil, err
	}
	rev := RevertsTrailer("checkpoint", "")
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
		for i, l := range strings.Split(f[2], "\n") {
			l = strings.TrimSpace(l)
			if i == 0 {
				c.Subject = l
			}
			if id, ok := strings.CutPrefix(l, rev); ok {
				c.Reverts = id
				continue
			}
			for _, k := range landKinds {
				if id, ok := strings.CutPrefix(l, TrailerFor(k, "")); ok {
					c.Kind, c.Lands = k, id
				}
			}
		}
		cs = append(cs, c)
	}
	return cs, nil
}

// IsRepoRoot reports whether dir holds its own .git (a dir, or the file of a linked
// tree). It only stats: a .git dir that is not a valid repo (an empty fixture) passes
// while git -C dir still walks up into the parent.
func IsRepoRoot(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// MainlineLogAcross is the Workspace's mainline log merged with each declared repo's
// (repos maps context name to dir), newest first and capped at n, the child's rows
// tagged with their context (Spec D8). With no repo declared it is MainlineLog alone.
func MainlineLogAcross(n int, repos map[string]string) ([]MainlineCommit, error) {
	cs, err := MainlineLog(n)
	if err != nil || len(repos) == 0 {
		return cs, err
	}
	root, err := RepoRoot()
	if err != nil {
		return nil, err
	}
	for _, name := range slices.Sorted(maps.Keys(repos)) {
		dir := filepath.Join(root, repos[name])
		if !IsRepoRoot(dir) { // a declared child not checked out has no history
			continue
		}
		kids, err := Repo(dir).MainlineLog(n)
		if err != nil { // one broken child must not blank the whole History tab
			continue
		}
		for _, c := range kids {
			c.Context = name
			cs = append(cs, c)
		}
	}
	slices.SortStableFunc(cs, func(a, b MainlineCommit) int { return b.Time.Compare(a.Time) })
	return cs[:min(n, len(cs))], nil
}

// revertTreeArgs / revertCommitArgs are Revert's plumbing pair: merge-tree applies
// squash^ onto base with squash as the merge base (exactly undoing squash's diff),
// then commit-tree writes that tree with base as sole parent. Neither touches an
// index, worktree or HEAD — only ffMainline then moves mainline, as for Land.
// merge-tree --write-tree with --merge-base= needs git >= 2.40; an older git fails
// it as a usage error (exit 129), which revert surfaces raw, never as a conflict.
func revertTreeArgs(base, squash string) []string {
	return []string{"merge-tree", "--write-tree", "--name-only", "--merge-base=" + squash, base, squash + "^"}
}
func revertCommitArgs(tree, base, squashSubject, trailer, graph string) []string {
	return []string{"commit-tree", tree, "-p", base, "-m", "revert: " + squashSubject, "-m", TrailerBlock("", trailer, graph)}
}

// Revert undoes squash on mainline with one forward commit carrying trailer and
// returns its SHA (ADR 0047 §4–6): mainline never rewinds, even when squash is at
// HEAD. It advances mainline through ffMainline exactly as Land does. It is
// idempotent on the trailer: present ⇒ that commit's SHA, nothing written; a failed
// read is not absence, so never a second revert. A conflicting revert (merge-tree
// exit 1) is *RevertConflict with mainline untouched (ADR 0049): merge-tree writes
// only objects, so there is no in-progress revert to abort.
func Revert(squash, trailer string) (sha string, err error) {
	return Repo("").Revert(squash, trailer)
}

// Revert is the package Revert on the child's trunk, under the Workspace's land lock
// (never a second one in the child).
func (r Repo) Revert(squash, trailer string) (sha string, err error) {
	return AnchorIfAbsent(trailer, r.landedBase, func(trailer string) (string, error) { return r.revert(squash, trailer) })
}

// revert is Revert's write leg: one forward revert commit, then ffMainline.
func (r Repo) revert(squash, trailer string) (sha string, err error) {
	if err := r.refuseNestedGitlab(); err != nil {
		return "", err
	}
	if err := r.mainClean(); err != nil {
		return "", err
	}
	base, err := r.run("rev-parse", r.mainline())
	if err != nil {
		return "", err
	}
	if sha, err = r.RevertOnto(base, squash, trailer); err != nil {
		return "", err
	}
	if err := r.ffMainline(sha, base); err != nil {
		return "", err
	}
	return sha, nil
}

// RevertOnto builds Revert's commit on base (the fetched Forge main under gitlab) and
// returns its SHA without moving any ref or taking the land lock. A conflict is
// *RevertConflict.
func RevertOnto(base, squash, trailer string) (string, error) {
	return Repo("").RevertOnto(base, squash, trailer)
}

// RevertOnto is the package RevertOnto in the child's object store.
func (r Repo) RevertOnto(base, squash, trailer string) (string, error) {
	tree, err := r.runOut(revertTreeArgs(base, squash)...)
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 { // conflicted; any other failure is raw
		return "", &RevertConflict{Base: base, Paths: conflictPaths(tree)}
	}
	if err != nil {
		return "", err
	}
	graph, err := GraphID()
	if err != nil {
		return "", err
	}
	subject, err := r.run("log", "-1", "--format=%s", squash)
	if err != nil {
		return "", err
	}
	return r.run(revertCommitArgs(tree, base, subject, trailer, graph)...)
}

// FirstParentOnMain normalises x to mainline's first-parent line: x itself when it is
// on it, else the oldest first-parent commit whose ancestry reaches x (the merge that
// brought a squash in as its second parent, ADR 0211).
func FirstParentOnMain(x string) (string, error) { return Repo("").FirstParentOnMain(x) }

// FirstParentOnMain is the package FirstParentOnMain on the child's mainline.
func (r Repo) FirstParentOnMain(x string) (string, error) {
	main := r.mainline()
	if _, err := r.run("merge-base", "--is-ancestor", x, main); err != nil {
		return "", fmt.Errorf("git: %s is not on %s", x, main)
	}
	out, err := r.run("rev-list", "--first-parent", "--ancestry-path", x+".."+main)
	if err != nil {
		return "", err
	}
	f := strings.Fields(out)
	if len(f) == 0 {
		return x, nil // x is mainline's tip
	}
	oldest := f[len(f)-1]
	if first, err := r.run("rev-parse", oldest+"^1"); err != nil {
		return "", err
	} else if first == x {
		return x, nil // x is the first parent of the line's next commit: on the line itself
	}
	return oldest, nil
}

// reapplyCommitArgs / reapplyRefArgs are Reapply's plumbing pair: commit-tree writes
// revert's PARENT tree (exactly the revert of the revert — it cannot conflict) with
// revert as sole parent — the same tree `git revert <revert>` (ADR 0048 §4) yields,
// without a checkout — then a CAS update-ref moves branch from revert onto it. No
// index, worktree or HEAD is touched, like CutBranch.
//
// The reapply's subject is the original land subject: revertSubject with its leading
// "revert: " stripped. A revert written before conventional subjects lacks the prefix,
// so its reapply is "revert: <revert subject>", still conventional.
func reapplyCommitArgs(revert, revertSubject string) []string {
	subject, ok := strings.CutPrefix(revertSubject, "revert: ")
	if !ok {
		subject = "revert: " + revertSubject
	}
	return []string{"commit-tree", revert + "~1^{tree}", "-p", revert, "-m", subject}
}
func reapplyRefArgs(branch, sha, revert string) []string {
	return []string{"update-ref", "refs/heads/" + branch, sha, revert}
}

// Reapply seeds a Rollback's successor branch, cut at the revert commit, with one
// reapply commit restoring the reverted diff (ADR 0048 §4) and returns the branch
// tip. Skip-if-done: a tip that is no longer revert is already seeded (or has moved
// on), so nothing is written. It runs under the land lock but skips mainClean: it
// moves only its successor branch, never main (ADR 0069).
func Reapply(branch, revert string) (sha string, err error) { return Repo("").Reapply(branch, revert) }

// Reapply is the package Reapply on the child's branch, under the Workspace's land lock.
func (r Repo) Reapply(branch, revert string) (sha string, err error) {
	return withLandLock(func() (string, error) { return r.reapply(branch, revert) })
}

func (r Repo) reapply(branch, revert string) (sha string, err error) {
	tip, err := r.run("rev-parse", "refs/heads/"+branch)
	if err != nil || tip != revert {
		return tip, err
	}
	subject, err := r.run("log", "-1", "--format=%s", revert)
	if err != nil {
		return "", err
	}
	if sha, err = r.run(reapplyCommitArgs(revert, subject)...); err != nil {
		return "", err
	}
	if _, err := r.run(reapplyRefArgs(branch, sha, revert)...); err != nil {
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
func EffortRefs() (map[string]bool, error) { return Repo("").EffortRefs() }

// CurrentEffort is the bead id off the cwd's branch, effort/<id>: the one input
// mugicrew integrate derives (Spec D3), read through a rebase stopped on
// it. Any other branch, or a detached HEAD outside a rebase, is an error.
func CurrentEffort() (string, error) { return Repo("").CurrentEffort() }

// CurrentEffort is the package CurrentEffort in the child worktree r (a tree-scoped
// method: r is <tree>/<dir>, never the child's main checkout).
func (r Repo) CurrentEffort() (string, error) {
	b, err := r.run("symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil { // a stopped rebase detaches HEAD; its state dir names the branch
		b, err = r.rebaseHeadName()
	}
	if id, ok := strings.CutPrefix(b, effortPrefix); err == nil && ok && id != "" {
		return id, nil
	}
	return "", fmt.Errorf("git: not on an %s<bead-id> branch (%q)", effortPrefix, b)
}

// Detached reports whether the cwd's HEAD is detached outside a stopped rebase:
// no branch to name, so mugicrew integrate's host probe (Spec D6) may look at
// the children.
func Detached() bool {
	r := Repo("")
	if _, err := r.run("symbolic-ref", "--quiet", "HEAD"); err == nil {
		return false
	}
	_, err := r.rebaseHeadName()
	return err != nil
}

// rebaseHeadName is the short name of the branch an in-progress rebase is
// rewriting, read from its state dir's head-name.
func (r Repo) rebaseHeadName() (string, error) {
	for _, name := range []string{"rebase-merge", "rebase-apply"} {
		p, err := r.run("rev-parse", "--path-format=absolute", "--git-path", name+"/head-name")
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

// ErrNoCutPoint is Integrate's non-transient refusal: the effort has no cut ref and
// its base is not in the container's reflog (the container was rewritten), so no
// replay start can be trusted. Nothing is integrated.
var ErrNoCutPoint = errors.New("git: no cut point for the effort (no cut ref, no fork point): the container was rewritten; nothing integrated")

// ErrOffPin is IntegrateReplace's refusal: the container was rewritten off the
// bead's pinned tip, so the effort's cut is stale. Nothing is integrated; the
// backstop closes the bead moot, re-pins it once, or pauses it.
var ErrOffPin = errors.New("git: container was rewritten off its pinned tip")

// errMoved is an attempt's "the container tip moved off the rebase base".
var errMoved = errors.New("git: container moved")

// Integrate squashes the cwd's effort branch onto container (Spec D3, ADR
// 0102 §3, the prototype's loop): it rebases onto the container tip
// outside the land lock, then under it refuses a held or landed container and
// CAS-moves the container onto one commit-tree squash, the caller's subject
// (IntegrateSubject) plus trailer (the bead's TrailerFor). A moved tip re-rebases, up to
// integrateRetries times, then ErrContended. It returns the squash, "" when
// nothing is ahead or the effort adds nothing past its last squash (idempotent,
// integratedThrough); commits made after an earlier integrate squash as a further
// commit under the same subject. A rebase conflict is left stopped in the tree as
// *IntegrateConflict, and a re-run over that stopped rebase returns the same conflict.
func Integrate(container, id, subject, trailer string) (sha string, retries int, err error) {
	return Repo("").Integrate(container, id, subject, trailer)
}

// Integrate is the package Integrate in the child worktree r (tree-scoped); the land
// lock stays the Workspace's.
func (r Repo) Integrate(container, id, subject, trailer string) (sha string, retries int, err error) {
	return r.integrateLoop(container, func(tip string) (string, error) {
		cut, err := r.run("rev-parse", "--verify", "--quiet", cutRefPrefix+id)
		upstream := cut
		if err != nil { // no cut ref: a branch cut before ADR 0264
			cut = ""
			fp, ferr := r.run("merge-base", "--fork-point", container, "refs/heads/"+EffortBranch(id))
			if ferr != nil || fp == "" { // base not in the container's reflog: it was rewritten
				return "", ErrNoCutPoint
			}
			if upstream, err = r.integratedThrough(tip, trailer); err != nil || upstream == "" {
				return "", err
			} else if upstream == tip { // no squash on tip yet: the fork point is the cut
				upstream = fp
			}
		}
		orig, err := r.run("rev-parse", "HEAD")
		if err != nil {
			return "", err
		}
		if _, err := r.run("rebase", "--onto", tip, upstream); err != nil {
			return "", r.rebaseStop(container, err)
		}
		head, err := r.run("rev-parse", "HEAD")
		var sha string
		if err == nil && head != tip {
			// an unmoved tip under the lock keeps integratedThrough's verdict
			sha, err = r.casLocked(container, tip, func() (string, error) {
				return r.run("commit-tree", head+"^{tree}", "-p", tip, "-m", subject, "-m", trailer)
			})
			if err == nil { // only a won CAS moves the cut ref; a pre-ADR-0264 branch gets one here
				// ponytail: the container already moved, so a failed write is not an integrate
				// failure; the next integrate falls back to the fork point again
				_, _ = r.run("update-ref", cutRefPrefix+id, head)
			}
		}
		if sha == "" && orig != head {
			// Nothing published: put the effort back, so the unmoved cut ref (or fork
			// point) still marks where its own work starts and the next attempt does
			// not replay the container commits this rebase carried.
			if _, rerr := r.run("reset", "--hard", orig); rerr != nil {
				err = cmp.Or(err, rerr)
			}
		}
		return sha, err
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
	return Repo("").IntegrateReplace(container, pinned)
}

// IntegrateReplace is the package IntegrateReplace in the child worktree r (tree-scoped).
func (r Repo) IntegrateReplace(container, pinned string) (sha string, retries int, err error) {
	return r.integrateLoop(container, func(tip string) (string, error) {
		head, err := r.run("rev-parse", "HEAD")
		if err != nil {
			return "", err
		}
		// A head strictly behind the pin dropped commits: a rewrite to publish, not
		// nothing ahead.
		// ponytail: once that drop is published and a sibling integrates past it, a
		// re-run refuses as rewritten off the pin (exit 1); loud, never a lost drop.
		dropped := head != pinned && r.IsAncestor(head, pinned)
		if head == tip || (!dropped && (r.IsAncestor(head, tip) || r.IsAncestor(head, pinned))) {
			return "", nil
		}
		base, err := r.replayedThrough(pinned, tip)
		if err != nil {
			return "", err
		}
		if base != tip && !r.IsAncestor(tip, head) {
			if !r.IsAncestor(base, tip) {
				return "", fmt.Errorf("%w: %s was rewritten off its pinned tip %s; nothing integrated", ErrOffPin, container, base)
			}
			if err := r.replay(base, tip); err != nil {
				return "", r.rebaseStop(container, err)
			}
			if head, err = r.run("rev-parse", "HEAD"); err != nil {
				return "", err
			}
		}
		return r.casLocked(container, tip, func() (string, error) { return head, nil })
	})
}

// integrateLoop is Integrate's and IntegrateReplace's shared retry: it runs
// attempt at the container's current tip until it reports anything but errMoved,
// up to integrateRetries retries, then ErrContended. A rebase already stopped in
// the tree is the same conflict again, with no attempt.
func (r Repo) integrateLoop(container string, attempt func(tip string) (string, error)) (sha string, retries int, err error) {
	if r.rebaseInProgress() {
		return "", 0, &IntegrateConflict{Reason: ConflictRebase, Container: container}
	}
	for retries = 0; retries <= integrateRetries; retries++ {
		var tip string
		if tip, err = r.run("rev-parse", "refs/heads/"+container); err != nil {
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
func (r Repo) rebaseStop(container string, err error) error {
	if !r.rebaseInProgress() {
		return err
	}
	return &IntegrateConflict{Reason: ConflictRebase, Container: container}
}

// casLocked is an integrate's in-lock leg: held and landed refusals, the
// moved-tip check, then a CAS of container from tip onto the commit next makes.
// The landed check shares the lock with Land's pin check, so an integrate queued
// behind a land refuses (ADR 0102). Only a tip that actually moved is errMoved; any
// other update-ref failure (a ref lock, a permission) is returned as itself.
func (r Repo) casLocked(container, tip string, next func() (string, error)) (string, error) {
	return withLandLock(func() (string, error) {
		ref := "refs/heads/" + container
		if err := r.integrateRefusal(container); err != nil {
			return "", err
		}
		if cur, err := r.run("rev-parse", ref); err != nil || cur != tip {
			return "", cmp.Or(err, errMoved)
		}
		sha, err := next()
		if err != nil {
			return "", err
		}
		if _, err := r.run("update-ref", ref, sha, tip); err != nil {
			if cur, rerr := r.run("rev-parse", ref); rerr == nil && cur != tip {
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
func (r Repo) replayedThrough(pinned, tip string) (string, error) {
	out, err := r.run("log", "--format=%(trailers:key="+replayedKey+",valueonly)", pinned+"..HEAD")
	if err != nil {
		return "", err
	}
	for v := range strings.FieldsSeq(out) {
		if r.IsAncestor(pinned, v) && r.IsAncestor(v, tip) {
			return v, nil
		}
	}
	return pinned, nil
}

// replay picks base..tip onto the effort branch with an interactive rebase whose
// todo is the picks plus a closing exec stamping the Replayed: trailer. Unlike
// rebase --onto HEAD base tip it keeps the effort branch checked out, so a
// conflict's rebase --continue finishes on it and --abort returns it to the rewrite.
func (r Repo) replay(base, tip string) error {
	shas, err := r.run("rev-list", "--reverse", "--no-merges", base+".."+tip)
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
	_, err = r.run("-c", "sequence.editor=cp '"+f+"'", "rebase", "-i", "--empty=drop", "--onto", "HEAD", "HEAD")
	return err
}

// integrateRefusal is the in-lock held and landed refusals both integrate modes
// share (ADR 0102 §3): any worktree on the container, or its land trailer on main.
func (r Repo) integrateRefusal(container string) error {
	if h, ok, err := r.worktreeOn(container); err != nil || ok {
		if ok {
			err = &IntegrateConflict{Reason: ConflictHeld, Container: container, Holder: h.Path}
		}
		return err
	}
	kind, id := ParseBranchName(container)
	if _, err := r.landedBase(TrailerFor(kind, id)); err == nil {
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
func (r Repo) integratedThrough(tip, trailer string) (string, error) {
	sq, err := r.run(trailerLogArgs(tip, trailer)...)
	if err != nil || sq == "" {
		return tip, err
	}
	tree, err := r.run("rev-parse", sq+"^{tree}")
	if err != nil {
		return "", err
	}
	if head, err := r.run("rev-parse", "HEAD^{tree}"); err != nil || head == tree {
		return "", err
	}
	out, err := r.run("log", "--format=%H %T", tip+"..HEAD")
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

// addedPathsArgs lists every path branch adds since mb; seqClashes keeps the
// ones a rule matches. --no-renames so a Chart that git-mv's an ADR onto a
// taken number shows as an add, not an R that --diff-filter=A would drop.
func addedPathsArgs(mb, branch string) []string {
	return []string{"diff", "--name-only", "--no-renames", "--diff-filter=A", mb, branch}
}

// ChangedPaths lists every path rev changes since its merge-base with base — the
// review diff's three-dot base. --no-renames so a move shows as both its paths.
func ChangedPaths(base, rev string) ([]string, error) {
	mb, err := run("merge-base", base, rev)
	if err != nil {
		return nil, err
	}
	out, err := run("diff", "--name-only", "--no-renames", mb, rev)
	if err != nil || out == "" {
		return nil, err
	}
	return strings.Split(out, "\n"), nil
}

// numberClashes reports the ADR numbers branch ADDS (new
// <prefix>docs/adr/NNNN-*.md vs its merge-base with base) that base already
// holds in that same docs/adr/ under another filename (ADR 0030 §1, OE
// ADR 0086 §5). Only added numbers count: duplicates already on base never clash.
// Read-only — Land runs it under the lock against the exact base it lands on.
func numberClashes(branch, base string) ([]string, error) {
	return Repo("").numberClashes(branch, base)
}

// ResolveProbeBase is the mainline SHA a Landing would rebase onto, resolved as the
// local land path does (Compose): read it once per probe and pin it.
func ResolveProbeBase() (string, error) { return Repo("").ResolveProbeBase() }

func (r Repo) ResolveProbeBase() (string, error) { return r.run("rev-parse", r.mainline()) }

// ProbeConflict reports, read-only and without the land lock, whether landing branch
// onto base would collide (Spec FR 3-5): *NumberClash first (numberClashes itself, as
// Compose calls it), then *RebaseConflict from merge-tree --write-tree, else nil. Any
// other git failure is a plain error, never a typed collision. merge-tree writes
// objects only; no ref, index or worktree is touched.
func ProbeConflict(branch, base string) error { return Repo("").ProbeConflict(branch, base) }

func (r Repo) ProbeConflict(branch, base string) error {
	pin, err := r.run("rev-parse", "--verify", "refs/heads/"+branch)
	if err != nil {
		return err
	}
	clashes, err := r.numberClashes(pin, base)
	if err != nil {
		return err
	}
	if len(clashes) > 0 {
		return &NumberClash{Clashes: clashes, Base: base}
	}
	_, err = r.runOut("merge-tree", "--write-tree", "--name-only", base, pin)
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 { // conflicted; any other failure is raw
		return &RebaseConflict{Base: base}
	}
	return err
}

// numberClashes is the package check against the child's docs/adr, its own number
// space (Spec FR 43).
func (r Repo) numberClashes(branch, base string) ([]string, error) {
	mb, err := r.run("merge-base", base, branch)
	if err != nil {
		return nil, err
	}
	added, err := r.run(addedPathsArgs(mb, branch)...)
	if err != nil {
		return nil, err
	}
	onMain, err := r.run("ls-tree", "-r", "--name-only", base)
	if err != nil {
		return nil, err
	}
	rules, err := SequenceRules(r)
	if err != nil {
		return nil, err
	}
	return seqClashes(rules, strings.Split(added, "\n"), strings.Split(onMain, "\n"))
}

// ResolveCheckFailed is CheckResolved's typed failure (ADR 0080 §3–4): the
// numbered assertion that failed, what it found, and the offending paths (for
// assertion 2, the clashes as seqClashes formats them).
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
//  2. no numbered-file clash against base (every rule) (Land's own guard);
//  3. only if tip is set: every mainline path HEAD changes vs base lies in the
//     Chart's own diff, merge-base(base, tip)..tip, or is an added ADR with a
//     Chart ADR's slug (a renumber), or is a path mainline renamed, between
//     merge-base(base, tip) and base, from a Chart path.
//
// Anchoring 3 on the pinned tip, not the session-start tip, covers a bad commit
// from an earlier run. Read-only; a failure is *ResolveCheckFailed.
func CheckResolved(base, tip string) error { return Repo("").CheckResolved(base, tip) }

// CheckResolved is the package check run in the child repo r (Spec FR 30).
func (r Repo) CheckResolved(base, tip string) error {
	if r.rebaseInProgress() {
		return &ResolveCheckFailed{Assertion: 1, Reason: "rebase in progress"}
	}
	st, err := r.run("status", "--porcelain")
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
	if _, err := r.run("merge-base", "--is-ancestor", base, "HEAD"); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 1 {
			return err
		}
		return &ResolveCheckFailed{Assertion: 1, Reason: "base " + base + " is not an ancestor of HEAD"}
	}
	clashes, err := r.numberClashes("HEAD", base)
	if err != nil {
		return err
	}
	if len(clashes) > 0 {
		return &ResolveCheckFailed{Assertion: 2, Reason: "numbered-file clash against " + base, Paths: clashes}
	}
	if tip == "" {
		return nil
	}
	mb, err := r.run("merge-base", base, tip)
	if err != nil {
		return err
	}
	chart, err := r.run("diff", "--name-only", "--no-renames", mb, tip)
	if err != nil {
		return err
	}
	changed, err := r.run("diff", "--name-status", "--no-renames", base, "HEAD")
	if err != nil {
		return err
	}
	chartPaths := strings.Split(chart, "\n")
	rules, err := SequenceRules(r)
	if err != nil {
		return err
	}
	slugs := func(p string) (out []string) { // keyed by rule: a renumber keeps rule and slug
		for _, ru := range rules {
			if s := seqSlug(ru, p); s != "" {
				out = append(out, ru.Template+"\x00"+s)
			}
		}
		return
	}
	chartSlugs := map[string]bool{}
	for _, p := range chartPaths {
		for _, s := range slugs(p) {
			chartSlugs[s] = true
		}
	}
	// A Chart path mainline renamed after the fork lands at the new path: the
	// endpoint diff collapses a chain P → Q → P' to P', at git's default 50%.
	renames, err := r.run("diff", "--name-status", "-M", mb, base)
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
		if !ok || slices.Contains(chartPaths, p) || (status == "A" && slices.ContainsFunc(slugs(p), func(s string) bool { return chartSlugs[s] })) {
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
func RestoreResolved(start string) error { return Repo("").RestoreResolved(start) }

// RestoreResolved is the package undo run in the child repo r.
func (r Repo) RestoreResolved(start string) error {
	if r.rebaseInProgress() {
		if _, err := r.run("rebase", "--abort"); err != nil {
			return err
		}
	}
	_, err := r.run("reset", "--hard", start)
	return err
}

// ErrNotLanded is LandedBase's "trailer absent" error, distinct from a failed git
// call, so a write-if-absent caller never mistakes a git
// failure for absence and writes a second commit.
var ErrNotLanded = errors.New("git: no landed commit")

// AnchorIfAbsent is the write-if-absent shape (ADR 0014 §2): write(trailer)
// only when landed(trailer) reports ErrNotLanded — a failed read is not absence, so
// never a second commit. Land and Revert share it; the legs are parameters so each caller keeps its own test seams. Both
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

// LandStrategyName is the active land_strategy of the cwd's repo, wired by the
// loop package's init to the layered config (git can't import it); local until then,
// so a binary or test that imports git without loop runs local, silently (ADR 0147
// forbids that fallback; ceiling: every binary imports loop via main). An
// unusable value is an error, never local. LandedBase reads Graph: only
// under gitlab (ADR 0187 §2).
var LandStrategyName = func() (string, error) { return LandStrategyLocal, nil }

// ForeignLanding is LandedBase's "the trailer is on mainline, but only under
// another Bead Graph" refusal: neither landed nor ErrNotLanded (ADR 0187 §2).
type ForeignLanding struct {
	Trailer, Ours, Theirs, Sha string
}

func (e *ForeignLanding) Error() string {
	return fmt.Sprintf("git: %q on mainline at %s belongs to Bead Graph %s, not ours (%s); a shared issue_prefix? bd rename-prefix to a distinct prefix, then re-cut the branch and MR",
		e.Trailer, e.Sha, e.Theirs, e.Ours)
}

// LandedBase re-derives a landed commit by grepping its trailer on mainline
// (ADR 0010 §2) — Zoro's read path, and Land's internal no-op derivation. An
// absent trailer is a loud error wrapping ErrNotLanded. Under gitlab a match
// must also carry our Graph: (or none, a legacy land); only foreign matches are
// *ForeignLanding.
func LandedBase(trailer string) (sha string, err error) { return Repo("").LandedBase(trailer) }

// Repo is a nested child repo's dir (<workspace>/<repo>, or one of its linked
// worktrees); every op runs as `git -C <dir>` through run. The package funcs keep
// meaning the cwd's repo (ADR 0166).
type Repo string

// mainline is Mainline for the empty Repo (the package funcs' meaning), else r.Mainline.
func (r Repo) mainline() string {
	if r == "" {
		return Mainline()
	}
	return r.Mainline()
}

// runOut is the package runOut as `git -C r`; the empty Repo is the cwd's repo.
func (r Repo) runOut(args ...string) (string, error) {
	if r == "" {
		return runOut(args...)
	}
	return runOut(append([]string{"-C", string(r)}, args...)...)
}

// runForge is run for a call that talks to the Forge remote: it carries glab's
// credential helper for that remote's URL (ADR 0300), none for an ssh remote.
func (r Repo) runForge(remote string, args ...string) (string, error) {
	u, _ := r.run("config", "--get", "remote."+remote+".url")
	if r != "" {
		args = append([]string{"-C", string(r)}, args...)
	}
	out, err := runRawEnv(GlabHelperEnv(u), args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (r Repo) run(args ...string) (string, error) {
	if r == "" {
		return run(args...)
	}
	return run(append([]string{"-C", string(r)}, args...)...)
}

// Mainline is the child's trunk, through its own common dir (the per-dir cache is
// shared with the package Mainline); main when the dir can't be resolved.
func (r Repo) Mainline() string {
	dir, err := CommonDirOf(string(r))
	if err != nil {
		return "main"
	}
	if m, ok := mainlines.Load(dir); ok {
		return m.(string)
	}
	m, _ := mainlines.LoadOrStore(dir, MainlineOf(dir))
	return m.(string)
}

// Head is a branch's live tip SHA in the child.
// A missing ref is ErrMissingRef, as package Head maps it.
func (r Repo) Head(branch string) (string, error) {
	sha, err := r.run("rev-parse", "--verify", "--quiet", branch)
	if ee := (*exec.ExitError)(nil); errors.As(err, &ee) && ee.ExitCode() == 1 {
		return "", fmt.Errorf("%w: %s", ErrMissingRef, branch)
	}
	return sha, err
}

// HasBranch reports whether refs/heads/<name> exists in r.
func (r Repo) HasBranch(name string) bool {
	_, err := r.run("rev-parse", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
}

// CutBranch is the package CutBranch in the child: branch-if-absent, no-op if name exists.
func (r Repo) CutBranch(name, base string) (created bool, err error) {
	if r.HasBranch(name) {
		return false, nil
	}
	if _, err := r.run(cutBranchArgs(name, base)...); err != nil {
		return false, err
	}
	return true, nil
}

// LandedBase is the package LandedBase on the child's trunk: an absent trailer wraps
// ErrNotLanded, never a fallback to the Workspace's main. Under gitlab a match must
// also carry our Graph: (or none, a legacy land), else *ForeignLanding (ADR 0187).
func (r Repo) LandedBase(trailer string) (string, error) {
	name, err := LandStrategyName()
	if err != nil {
		return "", err
	}
	if name != LandStrategyGitlab {
		sha, err := r.run(trailerLogArgs(r.mainline(), trailer)...)
		if err != nil {
			return "", err
		}
		if sha == "" {
			return "", fmt.Errorf("%w for trailer %q", ErrNotLanded, trailer)
		}
		return sha, nil
	}
	// no -n 1 and no --first-parent: a squash under the merge method is a merge's second parent.
	out, err := r.run("log", r.mainline(), "--extended-regexp", "--grep="+trailerRegex(trailer), "--format=%H")
	if err != nil {
		return "", err
	}
	shas := strings.Fields(out)
	if len(shas) == 0 {
		return "", fmt.Errorf("%w for trailer %q", ErrNotLanded, trailer)
	}
	ours, err := GraphID()
	if err != nil {
		return "", err
	}
	var foreign *ForeignLanding // the newest foreign match
	for _, s := range shas {
		msg, err := r.run("log", "-1", "--format=%B", s)
		if err != nil {
			return "", err
		}
		g, ok := ParseGraph(msg)
		if !ok || g == ours {
			return s, nil
		}
		if foreign == nil {
			foreign = &ForeignLanding{Trailer: trailer, Ours: ours, Theirs: g, Sha: s}
		}
	}
	return "", foreign
}

// LandedTip is the package LandedTip for a squash in the child.
func (r Repo) LandedTip(sha string) (tip string, ok bool) {
	body, err := r.run("log", "-1", "--format=%B", sha)
	if err != nil {
		return "", false
	}
	lines := strings.Split(body, "\n")
	slices.Reverse(lines)
	return ParseTip(strings.Join(lines, "\n"))
}

// IsAncestor is the package IsAncestor in the child.
func (r Repo) IsAncestor(a, b string) bool {
	_, err := r.run("merge-base", "--is-ancestor", a, b)
	return err == nil
}

// HasCommit is whether the child holds commit sha locally.
func (r Repo) HasCommit(sha string) bool {
	_, err := r.run("cat-file", "-e", sha+"^{commit}")
	return err == nil
}

// FetchCommit fetches sha from the child's Forge remote, so a pushed head some other
// clone made can be tested for ancestry.
func (r Repo) FetchCommit(sha string) error {
	remote := r.ForgeRemote()
	_, err := r.runForge(remote, "fetch", "--quiet", remote, sha)
	return err
}

// DeleteBranch is the package DeleteBranch in the child.
func (r Repo) DeleteBranch(name string) error {
	if _, err := r.run("branch", "-D", name); err != nil {
		return err
	}
	return r.dropCut(name)
}

// worktreeOn and landedBase are WorktreeOn and LandedBase on r; the empty Repo keeps
// the package funcs (and their test seams).
func (r Repo) worktreeOn(branch string) (Holder, bool, error) {
	if r == "" {
		return WorktreeOn(branch)
	}
	return r.WorktreeOn(branch)
}

func (r Repo) landedBase(trailer string) (string, error) {
	if r == "" {
		return LandedBase(trailer)
	}
	return r.LandedBase(trailer)
}

// WorktreeOn is the package WorktreeOn in the child.
func (r Repo) WorktreeOn(branch string) (h Holder, ok bool, err error) {
	list, err := r.run(worktreeListArgs()...)
	if err != nil {
		return Holder{}, false, err
	}
	h, ok = holderOf(list, branch)
	return h, ok, nil
}

// EffortRefs is the package EffortRefs in the child.
func (r Repo) EffortRefs() (map[string]bool, error) {
	out, err := r.run("for-each-ref", "--format=%(refname:lstrip=3)", "refs/heads/"+effortPrefix)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for id := range strings.FieldsSeq(out) {
		ids[id] = true
	}
	return ids, nil
}

// BranchRefs is EffortRefs plus the checkpoint/<id> tips (id → sha), both off one
// for-each-ref so the TUI's refresh spawns no extra process (Spec D1).
func (r Repo) BranchRefs() (efforts map[string]bool, tips map[string]string, err error) {
	out, err := r.run("for-each-ref", "--format=%(refname) %(objectname)", "refs/heads/"+effortPrefix, "refs/heads/checkpoint/")
	if err != nil {
		return nil, nil, err
	}
	efforts, tips = map[string]bool{}, map[string]string{}
	for line := range strings.Lines(out) {
		ref, sha, _ := strings.Cut(strings.TrimSpace(line), " ")
		if id, ok := strings.CutPrefix(ref, "refs/heads/"+effortPrefix); ok {
			efforts[id] = true
		} else if id, ok := strings.CutPrefix(ref, "refs/heads/checkpoint/"); ok {
			tips[id] = sha
		}
	}
	return efforts, tips, nil
}

// Materialize checks effort/<id> out in the child at path, a linked worktree of r
// (Spec D2, D4): worktree prune first, so a prunable entry from an outer-first
// teardown never blocks the re-cut; an existing effort/<id> rolls forward after
// reuseEffort (re-cut when it has no own commits), else it is cut at container's
// tip, or at pin while the container still descends from it (NewEffortWorktree's
// rule). The teardown is worktree remove --force.
func (r Repo) Materialize(path, id, container, pin string) (teardown func(), err error) {
	if _, err := r.run("worktree", "prune"); err != nil {
		return nil, err
	}
	branch := EffortBranch(id)
	if _, err := r.run("rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		r.reuseEffort(id, container, pin)
		_, err = r.run(worktreeAddArgs(path, branch)...)
	} else {
		at := container
		if pin != "" && r.IsAncestor(pin, container) {
			at = pin
		}
		if _, err = r.run("worktree", "add", "-b", branch, path, at); err == nil {
			err = recordCut(r, id, at)
		}
	}
	if err != nil {
		return nil, err
	}
	return func() { r.RemoveWorktree(path) }, nil
}

// MaterializeDetached is Materialize for a land tree: ref checked out detached at
// path, a linked worktree of r, holding no branch. The teardown is worktree remove --force.
func (r Repo) MaterializeDetached(path, ref string) (teardown func(), err error) {
	if _, err := r.run("worktree", "prune"); err != nil {
		return nil, err
	}
	if _, err := r.run("worktree", "add", "--detach", path, ref); err != nil {
		return nil, err
	}
	return func() { r.RemoveWorktree(path) }, nil
}

// RequireIgnored fails unless the Workspace repo ignores dir: a nested child
// materialized there would otherwise stage as a bogus gitlink in every outer add -A
// (Spec FR 5). The error names dir and the fix.
func RequireIgnored(dir string) error {
	if _, err := run("check-ignore", "-q", dir); err != nil {
		return fmt.Errorf("git: nested repo dir %q is not ignored by the Workspace repo; add %q to the Workspace's .gitignore", dir, strings.TrimSuffix(dir, "/")+"/")
	}
	return nil
}

// RemoveWorktree is the package RemoveWorktree for a linked worktree of the child.
func (r Repo) RemoveWorktree(path string) error {
	_, err := r.run(worktreeRemoveArgs(path)...)
	return err
}

// Worktrees is the package Worktrees in the child.
func (r Repo) Worktrees() ([]Worktree, error) {
	list, err := r.run(worktreeListArgs()...)
	if err != nil {
		return nil, err
	}
	var ws []Worktree
	for line := range strings.Lines(list) {
		line = strings.TrimSuffix(line, "\n")
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			ws = append(ws, Worktree{Path: p})
		} else if b, ok := strings.CutPrefix(line, "branch refs/heads/"); ok && len(ws) > 0 {
			ws[len(ws)-1].Branch = b
		}
	}
	return ws, nil
}

// PruneWorktrees is the package PruneWorktrees in the child.
func (r Repo) PruneWorktrees() error {
	_, err := r.run("worktree", "prune")
	return err
}

// Rebasing is the package Rebasing in the child.
func (r Repo) Rebasing() (map[string]string, error) {
	var common string
	var err error
	if r == "" {
		common, err = CommonDir()
	} else {
		common, err = CommonDirOf(string(r))
	}
	if err != nil {
		return nil, err
	}
	merge, _ := filepath.Glob(filepath.Join(common, "worktrees", "*", "rebase-merge", "head-name"))
	apply, _ := filepath.Glob(filepath.Join(common, "worktrees", "*", "rebase-apply", "head-name"))
	trees := map[string]string{}
	for _, n := range append(merge, apply...) {
		b, err := os.ReadFile(n)
		if err != nil {
			continue
		}
		gitdir, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(n)), "gitdir"))
		if err != nil { // a broken admin entry: git worktree prune's to clear
			continue
		}
		trees[filepath.Dir(strings.TrimSpace(string(gitdir)))] = strings.TrimPrefix(strings.TrimSpace(string(b)), "refs/heads/")
	}
	return trees, nil
}

// RebasingOn is the package RebasingOn in the child.
func (r Repo) RebasingOn(branch string) (h Holder, ok bool, err error) {
	trees, err := r.Rebasing()
	if err != nil {
		return Holder{}, false, err
	}
	for path, b := range trees {
		if b == branch {
			_, serr := os.Stat(path)
			return Holder{Path: path, Prunable: errors.Is(serr, os.ErrNotExist)}, true, nil
		}
	}
	return Holder{}, false, nil
}

// holderIn is r's holder of branch: the worktree with it checked out, else one
// stopped mid-rebase on it. The empty Repo goes through the package seams.
func (r Repo) holderIn(branch string) (Holder, bool, error) {
	h, ok, err := r.worktreeOn(branch)
	if err != nil || ok {
		return h, ok, err
	}
	if r == "" {
		h, ok, err = RebasingOn(branch)
	} else {
		h, ok, err = r.RebasingOn(branch)
	}
	h.Rebasing = ok && !h.Prunable
	return h, ok, err
}

// HolderAcross finds the holder of branch across the cwd repo, then each declared
// repo dir (repos' values, relative to the Workspace root) in sorted order, so a
// nested effort is never missed (ADR 0169). A child holder carries its Dir and its
// Host, the child path with /<dir> stripped, and repo is the child; the cwd's holder
// has repo "". Path "" is no holder. With no repos it is the cwd lookup alone; a
// declared dir that isn't checked out is skipped.
func HolderAcross(branch string, repos map[string]string) (h Holder, repo Repo, err error) {
	h, ok, err := Repo("").holderIn(branch)
	if err != nil || ok || len(repos) == 0 {
		return h, "", err
	}
	root, err := RepoRoot()
	if err != nil {
		return Holder{}, "", err
	}
	for _, dir := range slices.Compact(slices.Sorted(maps.Values(repos))) {
		r := Repo(filepath.Join(root, dir))
		if !IsRepoRoot(string(r)) {
			continue
		}
		h, ok, err := r.holderIn(branch)
		if err != nil {
			return Holder{}, "", err
		}
		if ok {
			h.Dir = dir
			h.Host = strings.TrimSuffix(h.Path, string(filepath.Separator)+filepath.Clean(dir))
			return h, r, nil
		}
	}
	return Holder{}, "", nil
}

// ForgeURL is the Forge remote's URL.
func ForgeURL() (string, error) {
	return run("config", "--get", "remote."+ForgeRemote()+".url")
}

// GlabHelperEnv is the env that points git at glab's credential helper for a
// Forge remote URL (ADR 0300): helper only, never a username entry, which trips
// glab's username-mismatch refusal under GITLAB_TOKEN. Nil for a non-http(s) URL.
func GlabHelperEnv(forgeURL string) []string {
	u, err := url.Parse(forgeURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil
	}
	return []string{"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=credential." + u.Scheme + "://" + u.Host + ".helper",
		"GIT_CONFIG_VALUE_0=!glab auth git-credential"}
}

// PostLandClash is one numbered file the landed commit added whose number
// another file holds on mainline, with this container's file the later side
// (ADR 0225): the added path, the earlier file(s) in its slot, and the mainline
// sha the read was made at. Commits and LandedBy run parallel to Others: each other
// file's first-parent introducing commit, and the container id its land trailer
// names ("" when none is found; no mapping is required, Spec FR 8).
type PostLandClash struct {
	Added    string
	Others   []string
	Mainline string
	Commits  []string
	LandedBy []string
}

// PostLandClashes is the post-land twin of numberClashes (ADR 0225, Spec FR 3-8,
// 11): of the numbered paths landed (the container's LandedBase sha) added, those
// whose number another file holds at mainline's tip and where landed's first-parent
// commit is the later of the two, by ancestry, never by date. Same commit is a
// pre-existing duplicate and skipped. Read-only; no bd.
func PostLandClashes(landed string) ([]PostLandClash, error) { return Repo("").PostLandClashes(landed) }

func (r Repo) PostLandClashes(landed string) ([]PostLandClash, error) {
	tip, err := r.run("rev-parse", r.mainline())
	if err != nil {
		return nil, err
	}
	added, err := r.run(addedPathsArgs(landed+"^1", landed)...)
	if err != nil {
		return nil, err
	}
	onMain, err := r.run("ls-tree", "-r", "--name-only", tip)
	if err != nil {
		return nil, err
	}
	rules, err := SequenceRules(r)
	if err != nil {
		return nil, err
	}
	mainline := strings.Split(onMain, "\n")
	// A path no longer on the tip (a landed renumber moved it) is a settled clash.
	addedOnTip := slices.DeleteFunc(strings.Split(added, "\n"), func(p string) bool { return !slices.Contains(mainline, p) })
	slots, err := seqSlotClashes(rules, addedOnTip, mainline)
	if err != nil || len(slots) == 0 {
		return nil, err
	}
	ours, err := r.firstParentOf(landed, tip)
	if err != nil {
		return nil, err
	}
	var out []PostLandClash
	for _, s := range slots {
		pc := PostLandClash{Added: s.Added, Mainline: tip}
		for _, o := range s.Others {
			c, err := r.introducedAt(tip, o)
			if err != nil {
				return nil, err
			}
			if c != ours && r.IsAncestor(c, ours) {
				by, err := r.landedBy(c)
				if err != nil {
					return nil, err
				}
				pc.Others, pc.Commits, pc.LandedBy = append(pc.Others, o), append(pc.Commits, c), append(pc.LandedBy, by)
			}
		}
		if len(pc.Others) > 0 {
			out = append(out, pc)
		}
	}
	return out, nil
}

// landedBy is the container id of the "Chart:"/"Checkpoint:" land trailer on c, or on
// c^1..c when c is a merge (the trailer rides the squash, not the merge); "" when none.
func (r Repo) landedBy(c string) (string, error) {
	rng := c + "^!"
	if ps, err := r.run("rev-list", "--parents", "-n", "1", c); err != nil {
		return "", err
	} else if len(strings.Fields(ps)) > 2 {
		rng = c + "^1.." + c
	}
	// real trailers only: a "Chart: x" line in prose does not count
	out, err := r.run("log", "--format=%(trailers:key=Chart,key=Checkpoint,valueonly,separator=%x0a)", rng)
	if err != nil {
		return "", err
	}
	by := ""
	for _, id := range strings.Fields(out) {
		by = id
	}
	return by, nil
}

// firstParentOf is the first-parent commit C of tip whose C^1..C holds landed:
// landed itself (ff, rebase), or the merge that brought it in.
func (r Repo) firstParentOf(landed, tip string) (string, error) {
	chain, err := r.run("rev-list", "--first-parent", tip)
	if err != nil {
		return "", err
	}
	l, err := r.run("rev-parse", landed)
	if err != nil {
		return "", err
	}
	if slices.Contains(strings.Fields(chain), l) {
		return l, nil
	}
	path, err := r.run("rev-list", "--first-parent", "--ancestry-path", l+".."+tip)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", fmt.Errorf("git: %s is not on %s", landed, tip)
	}
	lines := strings.Split(path, "\n")
	return lines[len(lines)-1], nil // oldest first-parent commit holding landed
}

// introducedAt is the newest first-parent commit on tip that adds exactly path,
// so a deleted-then-re-added path reads its current add.
func (r Repo) introducedAt(tip, path string) (string, error) {
	c, err := r.run("log", tip, "--first-parent", "--no-renames", "--diff-filter=A", "--format=%H", "-n", "1", "--", ":(literal)"+path)
	if err != nil {
		return "", err
	}
	if c == "" {
		return "", fmt.Errorf("git: no first-parent commit on %s adds %s", tip, path)
	}
	return c, nil
}

// WorktreeTop is the cwd's own worktree top-level dir: unlike RepoRoot, the
// linked worktree itself when the cwd is inside one.
func WorktreeTop() (string, error) { return run("rev-parse", "--show-toplevel") }

// OnMainline reports whether the cwd's worktree has the mainline branch checked
// out (a detached HEAD never does).
func OnMainline() bool {
	b, err := run("symbolic-ref", "--quiet", "--short", "HEAD")
	return err == nil && b == Mainline()
}
