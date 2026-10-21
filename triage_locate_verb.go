package main

import (
	"errors"
	"flag"
	"io"
	"path/filepath"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/locate"
)

// locateDeps is the live reads `triage locate` runs on; a seam so the verb's tests
// script the graph and repos.
var locateDeps = func() locate.Deps {
	return locate.Deps{
		Checkpoints: func() ([]bd.Bead, error) {
			open, err := bd.OpenSet()
			if err != nil {
				return nil, err
			}
			var cps []bd.Bead
			for _, b := range open {
				if b.Kind == contract.KindCheckpoint {
					cps = append(cps, b)
				}
			}
			return cps, nil
		},
		RepoOf: contexts.RepoOf,
		Repos: func() ([]git.Repo, error) {
			repos := []git.Repo{""}
			root, err := git.RepoRoot()
			if err != nil {
				return repos, err
			}
			seen := map[string]bool{}
			for _, dir := range contexts.Repos() {
				if !seen[dir] {
					seen[dir] = true
					repos = append(repos, git.Repo(filepath.Join(root, dir)))
				}
			}
			return repos, nil
		},
		Strategy: git.LandStrategyName,
	}
}

// showSignal reads --signal's bead; a seam for the verb's tests.
var showSignal = bd.Show

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

// runTriageLocate is `mugicrew triage locate [--signal <id>] --token <t>... --path <p>...
// [--json]` (ADR 0322): whether the Signal's code is only on one live Checkpoint. Read-only.
func runTriageLocate(argv []string, w io.Writer) error {
	const usage = "usage: mugicrew triage locate [--signal <id>] --token <t>... --path <p>... [--json]"
	fs := flag.NewFlagSet("triage locate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var tokens, paths listFlag
	signal := fs.String("signal", "", "")
	asJSON := fs.Bool("json", false, "")
	fs.Var(&tokens, "token", "")
	fs.Var(&paths, "path", "")
	if fs.Parse(argv) != nil || fs.NArg() != 0 || len(tokens)+len(paths) == 0 {
		return errors.New(usage)
	}
	d := locateDeps()
	if *signal != "" {
		b, err := showSignal(*signal)
		if err != nil {
			return err
		}
		only := contexts.RepoOf(b)
		d.Only = &only
	}
	res, err := d.Locate(locate.Query{Tokens: tokens, Paths: paths})
	if err != nil {
		return err
	}
	if *asJSON {
		return res.WriteJSON(w)
	}
	res.WriteText(w)
	return nil
}
