// Package locate is `mugicrew triage locate` (ADR 0322): the read-only check Luffy
// runs before proposing FAST LANE. From the tokens and paths Luffy picks, it says
// whether the Signal's code is only on one live Checkpoint's branch. Only the
// verdict "only <id>" opens REROUTE.
package locate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// Hit is one asked term: whether it is on mainline, and the live Checkpoints whose
// own diff carries it. Unknown is a term that matches nowhere (a mistyped one).
type Hit struct {
	Ask         string   `json:"ask"`  // the token or path
	Kind        string   `json:"kind"` // token | path
	OnMain      bool     `json:"on_main"`
	Unknown     bool     `json:"unknown,omitempty"`
	Checkpoints []string `json:"checkpoints"`
}

// Skip is a non-closed Checkpoint the verb did not read, with why.
type Skip struct {
	ID  string `json:"id"`
	Why string `json:"why"`
}

// Result is the verb's answer. Verdict is only | main | several | none; Checkpoint
// is set for only.
type Result struct {
	Verdict    string   `json:"verdict"`
	Checkpoint string   `json:"checkpoint,omitempty"`
	Hits       []Hit    `json:"hits"`
	Skipped    []Skip   `json:"skipped,omitempty"`
	Considered []string `json:"considered"`
	Warnings   []string `json:"warnings,omitempty"`
}

// Query is the terms Luffy picked.
type Query struct {
	Tokens, Paths []string
}

// Deps are the verb's reads, seams so a test scripts the bead list and the repos.
type Deps struct {
	Checkpoints func() ([]bd.Bead, error)  // the non-closed kind:checkpoint beads
	RepoOf      func(bd.Bead) git.Repo     // the repo a bead's context: label names ("" = the Workspace's)
	Repos       func() ([]git.Repo, error) // every declared repo, the Workspace's included; on error the repos it could name
	Only        *git.Repo                  // --signal's repo; nil reads every declared repo
	Strategy    func() (string, error)     // land_strategy
}

// Locate reads every selected repo and applies the decision rule: a token pulls the
// verdict to main only when it is on mainline and no live Checkpoint's diff touches
// it; a path only when it exists on mainline and no live Checkpoint changed it, and a
// changed path defers to the tokens when tokens are given.
func (d Deps) Locate(q Query) (Result, error) {
	if len(q.Tokens)+len(q.Paths) == 0 {
		return Result{}, errors.New("triage locate: give at least one --token or --path")
	}
	beads, err := d.Checkpoints()
	if err != nil {
		return Result{}, err
	}
	strategy, err := d.Strategy()
	if err != nil {
		return Result{}, err
	}
	byRepo := map[git.Repo][]bd.Bead{}
	for _, b := range beads {
		r := d.RepoOf(b)
		byRepo[r] = append(byRepo[r], b)
	}
	res := Result{Hits: []Hit{}, Considered: []string{}}
	repos, err := d.Repos()
	res.warn(err, "could not resolve the declared repos; searched only the repos it named")
	if d.Only != nil {
		repos = []git.Repo{*d.Only}
	}

	for _, t := range q.Tokens {
		res.Hits = append(res.Hits, Hit{Ask: t, Kind: "token"})
	}
	for _, p := range q.Paths {
		res.Hits = append(res.Hits, Hit{Ask: p, Kind: "path"})
	}
	for _, r := range repos {
		main := r.Mainline()
		if strategy == git.LandStrategyGitlab {
			if ref, err := r.FetchTracking(r.ForgeRemote()); err != nil {
				res.Warnings = append(res.Warnings, "mainline: local, fetch failed: "+err.Error())
			} else {
				main = ref
			}
		}
		live := res.skipDead(r, main, byRepo[r])
		res.Considered = append(res.Considered, live...)
		for i := range res.Hits {
			h := &res.Hits[i]
			switch h.Kind {
			case "token":
				on, err := r.GrepAt(main, h.Ask)
				res.warn(err, "could not search "+main+" for token "+h.Ask)
				h.OnMain = h.OnMain || on
				for _, id := range live {
					in, err := r.IntroducesToken(main, branchRef(id), h.Ask)
					res.warn(err, "could not search "+id+" for token "+h.Ask)
					if in {
						h.Checkpoints = append(h.Checkpoints, id)
					}
				}
			case "path":
				changed := false
				for _, id := range live {
					ch, err := r.ChangedOn(main, branchRef(id), h.Ask)
					res.warn(err, "could not diff "+id+" for path "+h.Ask)
					if ch {
						h.Checkpoints = append(h.Checkpoints, id)
						changed = true
					}
				}
				h.OnMain = h.OnMain || (!changed && r.PathAt(main, h.Ask))
			}
		}
	}
	slices.Sort(res.Considered)
	for i := range res.Hits {
		h := &res.Hits[i]
		slices.Sort(h.Checkpoints)
		h.Unknown = !h.OnMain && len(h.Checkpoints) == 0
	}
	res.judge(len(q.Tokens) > 0)
	return res, nil
}

// warn records a git failure as a Result warning, so a bad read is not a silent miss.
func (res *Result) warn(err error, what string) {
	if err != nil {
		res.Warnings = append(res.Warnings, what+": "+err.Error())
	}
}

func branchRef(id string) string { return "refs/heads/" + git.BranchName("checkpoint", id) }

// skipDead records the Checkpoints of r that are not live, with why, and returns the
// live ones' ids: a Checkpoint: trailer on main drops one, and so does having no
// refs/heads/checkpoint/<id>. Only that branch names a Checkpoint's code.
func (res *Result) skipDead(r git.Repo, main string, beads []bd.Bead) (live []string) {
	for _, b := range beads {
		sha, ok, err := r.TrailerOn(main, git.TrailerFor(string(contract.KindCheckpoint), b.ID))
		if err != nil {
			res.Warnings = append(res.Warnings, "could not read "+b.ID+"'s Checkpoint: trailer on "+main+": "+err.Error())
		}
		if ok {
			res.Skipped = append(res.Skipped, Skip{b.ID, "landed " + git.ShortSha(sha) + " (Checkpoint: trailer on mainline); bead not yet closed, branch may be kept"})
			continue
		}
		if !r.HasBranch(git.BranchName("checkpoint", b.ID)) {
			res.Skipped = append(res.Skipped, Skip{b.ID, "no checkpoint/" + b.ID + " branch"})
			continue
		}
		live = append(live, b.ID)
	}
	return live
}

func (res *Result) judge(tokens bool) {
	onMain, holders := false, map[string]bool{}
	for _, h := range res.Hits {
		if h.Kind == "path" && tokens && len(h.Checkpoints) > 0 {
			continue // a changed path defers to the tokens
		}
		onMain = onMain || (h.OnMain && len(h.Checkpoints) == 0)
		for _, id := range h.Checkpoints {
			holders[id] = true
		}
	}
	ids := slices.Sorted(maps.Keys(holders))
	switch {
	case onMain:
		res.Verdict = "main"
	case len(ids) == 1:
		res.Verdict, res.Checkpoint = "only", ids[0]
	case len(ids) > 1:
		res.Verdict = "several"
	default:
		res.Verdict = "none"
	}
}

// WriteJSON is the --json output.
func (res Result) WriteJSON(w io.Writer) error {
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	return e.Encode(res)
}

// WriteText is the default output: the verdict first, then each term, each skipped
// Checkpoint and the considered ones.
func (res Result) WriteText(w io.Writer) {
	for _, m := range res.Warnings {
		fmt.Fprintln(w, "warning:", m)
	}
	fmt.Fprintln(w, "verdict:", strings.TrimSpace(res.Verdict+" "+res.Checkpoint))
	for _, h := range res.Hits {
		note := ""
		if h.Unknown {
			note = "  (matched nowhere: check the spelling or use a repo-relative path)"
		}
		fmt.Fprintf(w, "  %s %s: on_main=%v checkpoints=%v%s\n", h.Kind, h.Ask, h.OnMain, h.Checkpoints, note)
	}
	for _, s := range res.Skipped {
		fmt.Fprintf(w, "  skipped %s: %s\n", s.ID, s.Why)
	}
	fmt.Fprintf(w, "considered: %s\n", strings.Join(res.Considered, " "))
}
