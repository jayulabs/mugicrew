package withdraw

import (
	"cmp"
	"fmt"
	"log/slog"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/session"
)

// ItemKind is what an Item is.
type ItemKind string

const (
	ItemTree   ItemKind = "tree"
	ItemBranch ItemKind = "branch"
	ItemDrafts ItemKind = "drafts"
	// ItemSession is a Task's exited pane, killed with no tree behind it.
	ItemSession ItemKind = "session"
)

// Item is one thing cleanup removed or kept. Err is why a failed removal stayed;
// Holder is whose it is when a foreign or alive hold kept it.
type Item struct {
	Kind   ItemKind
	Bead   string // the closed bead that owned it
	Repo   git.Repo
	Path   string // the tree's host path, or the branch name
	Holder string
	Err    string
}

func (i Item) String() string {
	s := fmt.Sprintf("%s %s (%s)", i.Kind, i.Path, i.Bead)
	switch {
	case i.Holder != "":
		s += ", held by " + i.Holder
	case i.Err != "":
		s += ": " + i.Err
	}
	return s
}

// seams, vars so the tests don't need a tmux server or a bd.
var (
	killTmux   = session.Kill
	dropTree   = session.DropOrphan
	dropDrafts = session.DropDraftsWith
)

// cleanup removes what the closed set left on disk (Spec D5) and records one event
// per tree and branch. Every step's error is collected and reported, never retried,
// and later steps still run; the close stands. Nothing alive or foreign is touched.
func cleanup(inv Inventory, target string, set []bd.Bead) (removed, kept []Item) {
	beads := map[string]bd.Bead{}
	for _, b := range set {
		beads[b.ID] = b
	}
	base := func(id string) []any { return append(eventlog.BeadAttrs(beads[id]), "target", target) }
	type hold struct {
		why   string
		isErr bool // a failed removal, not a foreign or alive holder
	}
	held := map[string]hold{} // repo+branch → why it can't be deleted

	tree := func(id string, repo git.Repo, t TreeLeftover) {
		holds := func(why string, isErr bool) {
			if t.Branch != "" { // a detached throwaway holds no branch
				held[string(repo)+"\x00"+t.Branch] = hold{why, isErr}
			}
		}
		it := Item{Kind: ItemTree, Bead: id, Repo: repo, Path: t.Host}
		a := append(base(id), "repo", string(repo), "tree", t.Host)
		if t.Child != "" {
			a = append(a, "child", t.Child)
		}
		if t.Class != Removable {
			it.Holder = t.Holder
			if t.Class == Alive {
				it.Holder = cmp.Or(t.Session, "a live process")
			}
			slog.Warn("tree.kept", append(a, "holder", it.Holder, "foreign", true)...)
			kept = append(kept, it)
			holds(it.Holder, false)
			return
		}
		var err error
		if t.Session != "" {
			err = killTmux(t.Session)
		}
		if err == nil {
			err = dropTree(t.Host)
		}
		if err != nil {
			it.Err = err.Error()
			slog.Warn("tree.kept", append(a, "err", it.Err)...)
			kept = append(kept, it)
			holds(it.Err, true)
			return
		}
		slog.Info("tree.removed", append(a, "session", t.Session, "dirty", t.Dirty, "rebasing", t.Rebasing, "prunable", t.Prunable)...)
		removed = append(removed, it)
	}

	// trees
	for _, b := range inv.Beads {
		for _, t := range b.Trees {
			tree(b.ID, b.Repo, t)
		}
	}
	for _, c := range inv.Containers {
		if c.Tree != nil {
			tree(c.ID, c.Repo, *c.Tree)
		}
	}
	// a HITL bead's dead pane: no work in it, so killing it is no discard (D6)
	for _, b := range inv.Beads {
		if b.Session == "" {
			continue
		}
		it := Item{Kind: ItemSession, Bead: b.ID, Path: b.Session}
		if err := killTmux(b.Session); err != nil {
			it.Err = err.Error()
			slog.Warn("session.kept", append(base(b.ID), "session", b.Session, "err", it.Err)...)
			kept = append(kept, it)
			continue
		}
		slog.Info("session.removed", append(base(b.ID), "session", b.Session)...)
		removed = append(removed, it)
	}
	// drafts
	for _, b := range inv.Beads {
		if len(b.Drafts) == 0 {
			continue
		}
		err := dropDrafts(beads[b.ID], "target", target)
		if err == nil {
			removed = append(removed, Item{Kind: ItemDrafts, Bead: b.ID, Path: strings.Join(b.Drafts, ", ")})
		} else {
			kept = append(kept, Item{Kind: ItemDrafts, Bead: b.ID, Path: strings.Join(b.Drafts, ", "), Err: err.Error()})
		}
	}
	// effort branches, then container branches. counts are the work the delete threw
	// away: unintegrated (+replace) for an effort branch, ahead for a container's.
	branch := func(id string, repo git.Repo, name string, counts ...any) {
		it := Item{Kind: ItemBranch, Bead: id, Repo: repo, Path: name}
		a := append(base(id), "repo", string(repo))
		if h, ok := held[string(repo)+"\x00"+name]; ok {
			key := "holder"
			if h.isErr {
				key, it.Err = "err", h.why
			} else {
				it.Holder = h.why
			}
			slog.Warn("branch.kept", append(a, "branch", name, key, h.why)...)
			kept = append(kept, it)
			return
		}
		err := repo.DeleteBranch(name)
		if err == nil {
			a = append(a, counts...)
		}
		eventlog.RecordBranches([]bd.BranchOutcome{{Branch: name, Err: err}}, a...)
		if err != nil {
			it.Err = err.Error()
			kept = append(kept, it)
			return
		}
		removed = append(removed, it)
	}
	for _, b := range inv.Beads {
		if b.Branch != "" {
			counts := []any{"unintegrated", b.Unintegrated}
			if b.Replace {
				counts = append(counts, "replace", true)
			}
			branch(b.ID, b.Repo, b.Branch, counts...)
		}
	}
	for _, c := range inv.Containers {
		branch(c.ID, c.Repo, c.Branch, "ahead", c.Ahead)
	}
	return removed, kept
}
