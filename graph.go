package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
)

// Seams for the graph verbs' tests: the Forge, glab and bd are never live.
var (
	forgeProject  = git.ForgeProject
	forgeCheck    = forge.CheckAuth
	forgeUserName = forge.CurrentUser
	graphPush     = bd.GraphPush
	graphFetch    = bd.FetchGraph
	repoRoot      = git.RepoRoot
)

// runGraph dispatches `mugicrew graph push|fetch`.
func runGraph(args []string, w io.Writer) error {
	if len(args) >= 1 && args[0] == "fetch" {
		return runGraphFetch(args[1:], w)
	}
	return runGraphPush(args, w)
}

// runGraphPush is `mugicrew graph push` (ADR 0218): push this Bead Graph to its
// Graph ref by hand. Only under land_strategy=gitlab, only with a glab login.
// It never files a Task (the human is looking) and restarts the bd server once on a failed fetch or push (ADR 0256).
func runGraphPush(args []string, w io.Writer) error {
	if len(args) != 1 || args[0] != "push" {
		return errors.New("usage: mugicrew graph push | mugicrew graph fetch <username> [<dir>]")
	}
	strategy, err := git.LandStrategyName()
	if err != nil {
		return err
	}
	if strategy != git.LandStrategyGitlab {
		return fmt.Errorf("mugicrew graph push: needs land_strategy=gitlab (this Workspace is %s)", strategy)
	}
	proj, err := forgeProject()
	if err != nil {
		return err
	}
	if err := forgeCheck(proj.Host); err != nil {
		return fmt.Errorf("mugicrew graph push: run glab auth login --hostname %s: %w", proj.Host, err)
	}
	name, err := forgeUserName(proj)
	if err != nil {
		return err
	}
	res, err := graphPush(name)
	eventlog.RecordManualGraphPush(bd.GraphRefOf(name), err)
	var foreign *bd.ForeignGraphRef
	if errors.As(err, &foreign) {
		return fmt.Errorf("mugicrew graph push: %w; nothing pushed", err)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "pushed %s\n", res.Ref)
	if res.Warning != "" {
		fmt.Fprintln(w, "warning: "+res.Warning)
	}
	return nil
}

// runGraphFetch is `mugicrew graph fetch <username> [<dir>]` (ADR 0218): clone
// that person's Graph ref into a disposable Scratch graph and print how to read
// it. No land strategy or glab login is needed; the Forge remote is enough.
func runGraphFetch(args []string, w io.Writer) error {
	if len(args) < 1 || len(args) > 2 {
		return errors.New("usage: mugicrew graph fetch <username> [<dir>]")
	}
	name := args[0]
	dir, err := scratchDir(name, args[1:])
	if err != nil {
		return err
	}
	// graph fetch needs no Graph (requireGraph is not called), so this is the
	// only resolution of the Workspace root; BEADS_DIR below is the other keep.
	root, err := repoRoot()
	if err != nil {
		return err
	}
	var keep []string
	if root != "" {
		keep = append(keep, root)
	}
	if b := os.Getenv("BEADS_DIR"); b != "" {
		if abs, err := filepath.Abs(b); err == nil {
			keep = append(keep, abs)
		}
	}
	if err := graphFetch(name, dir, keep...); err != nil {
		return err
	}
	fmt.Fprintf(w, "fetched %s into %s\nto read it (read-only bd does not auto-start a server):\n  (cd %s && bd dolt start)\n  BEADS_DIR=%s bd list\n  (cd %s && bd dolt stop)\n", bd.GraphRefOf(name), dir, dir, filepath.Join(dir, ".beads"), dir)
	return nil
}

// scratchDir is the <dir> argument, else $XDG_CACHE_HOME/mugicrew/graphs/<name>
// (falling back to ~/.cache), made absolute.
func scratchDir(name string, rest []string) (string, error) {
	if len(rest) == 1 {
		return filepath.Abs(rest[0])
	}
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		cache = filepath.Join(home, ".cache")
	}
	return filepath.Join(cache, "mugicrew", "graphs", name), nil
}
