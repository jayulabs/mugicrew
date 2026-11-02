// Package rebase files a human-requested rebase of an open Chart or Checkpoint onto
// current mainline (ADR 0262): a `requested`-variant Conflict / Chart-conflict, pinned
// at filing. One function serves the verb and the TUI key.
package rebase

import (
	"errors"
	"fmt"
	"slices"

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
	// MembersMayBePaused marks an AlreadyOnMain Checkpoint whose members may still be paused
	// behind-mainline, which a rebase has nothing to clear; the caller words the follow-up.
	MembersMayBePaused bool
	Edged              []string // members paused behind-mainline now blocked by ID and unpaused (ADR 0420)
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
	Members    func(id string) ([]bd.Bead, error) // a Checkpoint's children, any status
	AddBlocker func(blocked, blocker string) error
	Unpause    func(id, cause string) error
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
	Members:    bd.ListChildren,
	AddBlocker: bd.Blocks,
	Unpause:    bd.Unpause,
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
		out := Outcome{Result: AlreadyOnMain, Base: base}
		if c.Kind == contract.KindCheckpoint {
			out.MembersMayBePaused = true
		}
		return out, nil
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
	if c.Kind == contract.KindCheckpoint {
		if out.Edged, err = edgeBehindMainline(s, c.ID, childID); err != nil {
			return Outcome{}, err
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

// edgeBehindMainline makes each open member of checkpoint labelled paused:behind-mainline wait on
// rebaseID, then lifts that pause (ADR 0420): edge before lift, so a crash between the
// two leaves the Build held, never free. Other members are left unblocked.
func edgeBehindMainline(s Seams, checkpoint, rebaseID string) ([]string, error) {
	kids, err := s.Members(checkpoint)
	if err != nil {
		return nil, err
	}
	var edged []string
	for _, k := range kids {
		if !k.Open() || !slices.Contains(bd.PausedCauses(k), bd.PausedBehindMainline) {
			continue
		}
		if err := s.AddBlocker(k.ID, rebaseID); err != nil {
			return edged, err
		}
		if err := s.Unpause(k.ID, bd.PausedBehindMainline); err != nil {
			return edged, err
		}
		edged = append(edged, k.ID)
	}
	return edged, nil
}

// MemberPauseHint is the callers' follow-up for an AlreadyOnMain Checkpoint with
// MembersMayBePaused.
const MemberPauseHint = "a Build still paused behind-mainline: bd label remove it"
