// Package rebase files a human-requested rebase of an open Chart or Checkpoint onto
// current mainline (ADR 0262): a `requested`-variant Conflict / Chart-conflict, pinned
// at filing. One function serves the verb and the TUI key.
package rebase

import (
	"errors"
	"fmt"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/nami"
)

// Result names how a request ended.
type Result int

const (
	Filed            Result = iota // a requested rebase was filed
	AlreadyRequested               // an open requested rebase stands; nothing filed
	OpenConflict                   // an open rebase-conflict / number-clash stands; nothing filed
	AlreadyOnMain                  // the container already contains mainline; nothing filed
	Refused                        // the target cannot be rebased; Reason says why
)

// Outcome is Request's typed result. ID/Title name the filed or standing child; Base is
// the pin (filed: the new one; OpenConflict: the standing child's; AlreadyOnMain: mainline).
// Warning is the optional gitlab open-MR note; it never blocks filing.
type Outcome struct {
	Result    Result
	ID, Title string
	Base      string
	Reason    string
	Warning   string
}

// Seams are Request's bd, git and forge reads, vars so tests script them.
type Seams struct {
	Show       func(id string) (bd.Bead, error)
	Repo       func(b bd.Bead) (git.Repo, string) // the bead's repo, "" for the cwd's
	Strategy   func() (string, error)
	Fetch      func(r git.Repo) error // FetchMainline, gitlab only
	Landed     func(r git.Repo, trailer string) (bool, error)
	HasBranch  func(r git.Repo, name string) bool
	Base       func(r git.Repo) (string, error)
	Head       func(r git.Repo, ref string) (string, error)
	IsAncestor func(r git.Repo, a, b string) bool
	Spawn      func(parent string, s bd.Successor) (id string, created bool, err error)
	FindOpenMR func(r git.Repo, branch string) (*forge.MR, error)
}

// Real is the production wiring.
var Real = Seams{
	Show:      bd.Show,
	Repo:      contexts.RepoDirOf,
	Strategy:  git.LandStrategyName,
	Fetch:     func(r git.Repo) error { _, err := r.FetchMainline(); return err },
	HasBranch: func(r git.Repo, n string) bool { return r.HasBranch(n) },
	Landed: func(r git.Repo, trailer string) (bool, error) {
		_, err := r.LandedBase(trailer)
		if errors.Is(err, git.ErrNotLanded) {
			return false, nil
		}
		return err == nil, err
	},
	Base:       func(r git.Repo) (string, error) { return r.ResolveProbeBase() },
	Head:       func(r git.Repo, ref string) (string, error) { return r.Head(ref) },
	IsAncestor: func(r git.Repo, a, b string) bool { return r.IsAncestor(a, b) },
	Spawn:      bd.SpawnChildIfAbsent,
	FindOpenMR: func(r git.Repo, branch string) (*forge.MR, error) {
		p, err := r.ForgeProject()
		if err != nil {
			return nil, err
		}
		return forge.FindOpenMR(p, branch)
	},
}

func refuse(format string, a ...any) (Outcome, error) {
	return Outcome{Result: Refused, Reason: fmt.Sprintf(format, a...)}, nil
}

// Request files a requested rebase on container id (Spec FR 1, 3–11, 19). A refusal is
// an Outcome, not an error; an error is an infrastructure failure and files nothing.
func Request(s Seams, id string) (Outcome, error) {
	c, err := s.Show(id)
	if err != nil {
		if bd.IsNotFound(err) {
			return refuse("no such bead %s: %v", id, err)
		}
		return Outcome{}, err
	}
	if c.Kind != contract.KindChart && c.Kind != contract.KindCheckpoint {
		return refuse("%s is a %s; only an open Chart or Checkpoint can be rebased", id, c.Kind)
	}
	if !c.Open() {
		return refuse("%s is closed", id)
	}
	repo, _ := s.Repo(c)
	branch := git.BranchName(string(c.Kind), c.ID)
	if !s.HasBranch(repo, branch) {
		return refuse("%s has no branch %s yet", id, branch)
	}
	name, err := s.Strategy()
	if err != nil {
		return Outcome{}, err
	}
	gitlab := name == git.LandStrategyGitlab
	if gitlab {
		if err := s.Fetch(repo); err != nil {
			return Outcome{}, err
		}
	}
	landed, err := s.Landed(repo, git.TrailerFor(string(c.Kind), c.ID))
	if err != nil {
		return Outcome{}, err
	}
	if landed {
		return refuse("%s has already landed", id)
	}
	base, err := s.Base(repo)
	if err != nil {
		return Outcome{}, err
	}
	tip, err := s.Head(repo, "refs/heads/"+branch)
	if err != nil {
		return Outcome{}, err
	}
	if s.IsAncestor(repo, base, tip) {
		return Outcome{Result: AlreadyOnMain, Base: base}, nil
	}
	succ := bd.Successor{Kind: contract.KindConflict, Title: nami.RequestedCheckpointTitle(c.ID), Description: nami.RequestedCheckpointBody(c.ID, base, tip)}
	if c.Kind == contract.KindChart {
		succ = bd.Successor{Kind: contract.KindChartConflict, Title: nami.RequestedChartTitle(c.Title),
			Description: nami.ChartConflictBody(c.ID, base, tip, nami.ConflictRequested, nil)}
	}
	childID, created, err := s.Spawn(c.ID, succ)
	if err != nil {
		return Outcome{}, err
	}
	out := Outcome{Result: Filed, ID: childID, Title: succ.Title, Base: base}
	if !created {
		kid, err := s.Show(childID)
		if err != nil {
			return Outcome{}, err
		}
		out.Title = kid.Title
		if nami.BodyVariant(kid.Description) == nami.ConflictRequested {
			out.Result = AlreadyRequested
		} else {
			out.Result = OpenConflict
			out.Base, _ = git.ParseBase(kid.Description)
		}
	}
	if gitlab {
		mr, err := s.FindOpenMR(repo, branch)
		switch {
		case err != nil:
			out.Warning = "could not check for an open MR"
		case mr != nil:
			out.Warning = fmt.Sprintf("open MR %s stays mergeable at the old pin until the next Landing", mr.WebURL)
		}
	}
	return out, nil
}
