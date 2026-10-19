package withdraw

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/route"
	"jayulabs.org/mugicrew/session"
)

// Class is whether cleanup may take a tree down (Spec mc-5rttm D4).
type Class string

const (
	// Removable: a temp-shaped tree that is prunable, an orphan, or the worktree of
	// an exited session of a bead in the set.
	Removable Class = "removable"
	// Alive: the worktree of an alive session or a live process's. Never removed.
	Alive Class = "alive"
	// Foreign: not mugicrew's to remove (not temp-shaped, the main worktree on the
	// branch, a non-temp tree mid-rebase, or an exited session of a bead outside the set).
	Foreign Class = "foreign"
)

// TreeLeftover is one worktree holding a closing bead's branch (or a prototype's
// throwaway). A nested effort's Host is the Workspace tree and Child the child
// repo's tree inside it, removed first.
type TreeLeftover struct {
	Host, Child string
	Session     string // the exited session's id, if any
	Class       Class
	Prunable    bool
	Rebasing    bool
	Dirty       int    // changed files in Child and Host; not counted when Prunable or Rebasing
	Holder      string // for Foreign: the bead whose session holds it, else the tree itself
	Branch      string // the branch the tree holds; "" for a prototype's detached throwaway
}

// BeadLeftovers is what a closing bead leaves behind.
type BeadLeftovers struct {
	ID           string
	Repo         git.Repo // "" = the Workspace
	Branch       string   // effort/<id> when the bead routes on an effort and the ref exists
	Unintegrated int      // commits on Branch since its cut ref
	AtMost       bool     // no cut ref: Unintegrated is an upper bound from the container
	Replace      bool     // a replace-mode leaf: counted against its pinned Tip
	Trees        []TreeLeftover
	Drafts       []string
}

// ContainerBranch is a closing Chart's or Checkpoint's branch.
type ContainerBranch struct {
	ID, Branch string
	Repo       git.Repo
	Ahead      int // commits on Branch not on mainline: work that never landed
	Tree       *TreeLeftover
}

// Inventory is the cleanup a verb would perform, read before anything is removed.
type Inventory struct {
	Beads      []BeadLeftovers
	Containers []ContainerBranch
}

// Loses reports whether discarding the inventory loses work: a removable or alive
// tree with uncommitted changes or a stopped rebase, an unintegrated commit, or a
// container commit never landed.
func (inv Inventory) Loses() bool {
	for _, b := range inv.Beads {
		if b.Unintegrated > 0 {
			return true
		}
		for _, t := range b.Trees {
			if t.loses() {
				return true
			}
		}
	}
	for _, c := range inv.Containers {
		if c.Ahead > 0 || (c.Tree != nil && c.Tree.loses()) {
			return true
		}
	}
	return false
}

func (t TreeLeftover) loses() bool {
	return t.Class != Foreign && (t.Dirty > 0 || t.Rebasing)
}

// String is the inventory as the CLI prints it: one line per bead leftover and
// container branch, in the order they were read.
func (inv Inventory) String() string {
	var sb strings.Builder
	for _, b := range inv.Beads {
		fmt.Fprintf(&sb, "  %s:", b.ID)
		if b.Branch != "" {
			n := fmt.Sprint(b.Unintegrated)
			if b.AtMost {
				n = "≤" + n
			}
			fmt.Fprintf(&sb, " branch %s (%s unintegrated %s)", b.Branch, n, commits(b.Unintegrated))
		}
		if len(b.Drafts) > 0 {
			fmt.Fprintf(&sb, " drafts %s", strings.Join(b.Drafts, ", "))
		}
		sb.WriteString("\n")
		for _, t := range b.Trees {
			fmt.Fprintf(&sb, "    %s\n", t)
		}
	}
	for _, c := range inv.Containers {
		fmt.Fprintf(&sb, "  %s: branch %s (%d %s never landed)\n", c.ID, c.Branch, c.Ahead, commits(c.Ahead))
		if c.Tree != nil {
			fmt.Fprintf(&sb, "    %s\n", *c.Tree)
		}
	}
	return sb.String()
}

// commits is the noun for n commits.
func commits(n int) string {
	if n == 1 {
		return "commit"
	}
	return "commits"
}

func (t TreeLeftover) String() string {
	s := fmt.Sprintf("tree %s [%s]", t.Host, t.Class)
	if t.Child != "" {
		s += " child " + t.Child
	}
	switch {
	case t.Prunable:
		s += ", prunable"
	case t.Rebasing:
		s += ", stopped mid-rebase"
	default:
		s += fmt.Sprintf(", %d dirty files", t.Dirty)
	}
	if t.Holder != "" {
		s += ", held by " + t.Holder
	}
	return s
}

// DiscardRequiredError is a verb stopped before closing anything because its
// inventory loses work and Discard is not set. It is the CLI's safeguard, not a
// refusal: nothing is logged.
type DiscardRequiredError struct {
	Verb, Target string
	Inventory    Inventory
}

func (e *DiscardRequiredError) Error() string {
	return fmt.Sprintf("%s %s would discard work:\n%s\nre-run with --discard to close it anyway", e.Verb, e.Target, e.Inventory)
}

// seams, vars so the tests script the session table and the inventory.
var (
	readSessions  = session.Sessions
	takeInventory = TakeInventory
	draftsOf      = func(id string) ([]string, error) {
		ds, err := bd.Drafts(id)
		return bd.IDs(ds), err
	}
)

// TakeInventory reads the cleanup of the closing set: every bead's effort branch,
// trees and Drafts, and each closing Chart's or Checkpoint's own branch. Repos come
// from the declared-repo union, sessions from one session-table read; a failed read
// fails the inventory, so a live session's tree is never taken for an orphan.
// Measurements are all taken here, before any removal.
func TakeInventory(set []bd.Bead, o Opts) (Inventory, error) {
	sessions, err := readSessions()
	if err != nil {
		return Inventory{}, fmt.Errorf("read the session table: %w", err)
	}
	c := collector{sessions: sessions, o: o, repos: contexts.Repos(), inSet: map[string]bool{}}
	for _, b := range set {
		c.inSet[b.ID] = true
	}
	var inv Inventory
	for _, b := range set {
		left, err := c.bead(b)
		if err != nil {
			return Inventory{}, fmt.Errorf("%s: %w", b.ID, err)
		}
		if left.Branch != "" || len(left.Trees) > 0 || len(left.Drafts) > 0 {
			inv.Beads = append(inv.Beads, left)
		}
		if b.Kind == contract.KindChart || b.Kind == contract.KindCheckpoint {
			cb, ok, err := c.container(b)
			if err != nil {
				return Inventory{}, fmt.Errorf("%s: %w", b.ID, err)
			}
			if ok {
				inv.Containers = append(inv.Containers, cb)
			}
		}
	}
	return inv, nil
}

type collector struct {
	sessions map[string]session.Info
	o        Opts
	repos    map[string]string
	inSet    map[string]bool
}

func (c collector) bead(b bd.Bead) (BeadLeftovers, error) {
	left := BeadLeftovers{ID: b.ID}
	var err error
	if left.Drafts, err = draftsOf(b.ID); err != nil {
		return left, err
	}
	d, onEffort, err := route.OnEffort(b)
	if err != nil {
		return left, nil // a bead that can't route has no effort to find
	}
	if onEffort {
		r := contexts.RepoOf(contexts.Owner(b, d.Container))
		left.Repo = r
		if r.HasBranch(d.Branch) {
			if err := c.measure(&left, b, d, r); err != nil {
				return left, err
			}
			t, ok, err := c.holder(d.Branch, b)
			if err != nil {
				return left, err
			}
			if ok {
				left.Trees = append(left.Trees, t)
			}
		}
		if b.Kind == contract.KindPrototype {
			if t, ok, err := c.throwaway(b, r); err != nil {
				return left, err
			} else if ok {
				left.Trees = append(left.Trees, t)
			}
		}
	}
	return left, nil
}

// measure sets left's Branch and its Unintegrated count: from the cut ref, else from
// the container as an upper bound, and for a replace-mode leaf from its pinned Tip.
func (c collector) measure(left *BeadLeftovers, b bd.Bead, d route.Destination, r git.Repo) (err error) {
	left.Branch = d.Branch
	if d.Mode == route.Replace && d.Tip != "" {
		left.Replace = true
		left.Unintegrated, err = r.CommitsAhead(d.Tip, d.Branch)
		return err
	}
	n, ok, err := r.CommitsSinceCut(b.ID, d.Branch)
	if err != nil {
		return err
	}
	if ok {
		left.Unintegrated = n
		return nil
	}
	left.AtMost = true
	if left.Unintegrated, err = r.CommitsAhead(d.Container, d.Branch); err != nil {
		// no container to measure from: everything off mainline is the bound
		left.Unintegrated, err = r.CommitsAhead(mainlineOf(r), d.Branch)
	}
	return err
}

// container reads a closing Chart's or Checkpoint's branch, ok false when it has none.
func (c collector) container(b bd.Bead) (ContainerBranch, bool, error) {
	branch := git.BranchName(string(b.Kind), b.ID)
	r := contexts.RepoOf(b)
	if !r.HasBranch(branch) {
		return ContainerBranch{}, false, nil
	}
	cb := ContainerBranch{ID: b.ID, Branch: branch, Repo: r}
	var err error
	if cb.Ahead, err = r.CommitsAhead(mainlineOf(r), branch); err != nil {
		return cb, false, err
	}
	t, ok, err := c.holder(branch, b)
	if err != nil {
		return cb, false, err
	}
	if ok {
		cb.Tree = &t
	}
	return cb, true, nil
}

func mainlineOf(r git.Repo) string {
	if r == "" {
		return git.Mainline()
	}
	return r.Mainline()
}

// holder finds the tree with branch checked out, or stopped mid-rebase on it, in
// the cwd repo and then each declared repo.
func (c collector) holder(branch string, b bd.Bead) (TreeLeftover, bool, error) {
	h, hr, err := git.HolderAcross(branch, c.repos)
	if err != nil || h.Path == "" {
		return TreeLeftover{}, false, err
	}
	t := TreeLeftover{Host: h.Path, Prunable: h.Prunable, Rebasing: h.Rebasing, Branch: branch}
	if h.Dir != "" {
		t.Host, t.Child = h.Host, h.Path
	}
	wts, err := hr.Worktrees()
	if err != nil {
		return t, false, err
	}
	main := len(wts) > 0 && wts[0].Path == h.Path
	return t, true, c.classify(&t, b, main)
}

// throwaway finds a prototype's detached throwaway tree, cut at its temp path.
func (c collector) throwaway(b bd.Bead, r git.Repo) (TreeLeftover, bool, error) {
	p := git.TempWorktreePath(b.ID)
	wts, err := r.Worktrees()
	if err != nil {
		return TreeLeftover{}, false, err
	}
	for _, w := range wts {
		if w.Path != p {
			continue
		}
		_, serr := os.Stat(p)
		t := TreeLeftover{Host: p, Prunable: errors.Is(serr, os.ErrNotExist)}
		return t, true, c.classify(&t, b, false)
	}
	return TreeLeftover{}, false, nil
}

// classify sets t's Class, Holder and Dirty. mainTree is the repo's main worktree.
func (c collector) classify(t *TreeLeftover, b bd.Bead, mainTree bool) error {
	holder := session.HolderBead(t.Host, c.sessions)
	switch {
	case mainTree || !git.IsTempWorktree(t.Host):
		t.Class = Foreign
	case holder != "" && c.sessions[holder].State == session.Alive:
		t.Class = Alive
		t.Session = c.sessions[holder].Session
	case holder != "" && !c.inSet[holder]:
		t.Class = Foreign
	case holder == "" && c.o.InFlight != nil && c.o.InFlight(b):
		t.Class = Alive // a live process holds it, with no session
	default:
		t.Class = Removable
		if holder != "" {
			t.Session = c.sessions[holder].Session
		}
	}
	if t.Class == Foreign {
		t.Holder = holder
		if t.Holder == "" {
			t.Holder = t.Host
		}
	}
	if t.Prunable || t.Rebasing {
		return nil
	}
	return t.count()
}

// count sets Dirty: the child's changed files plus the host's, a host already gone has none.
func (t *TreeLeftover) count() (err error) {
	own := filepath.Clean(cmp.Or(t.Child, t.Host))
	if t.Dirty, err = git.DirtyFiles(own); err != nil || t.Child == "" {
		return err
	}
	if _, serr := os.Stat(t.Host); serr != nil {
		return nil
	}
	n, err := git.DirtyFiles(t.Host)
	t.Dirty += n
	return err
}
