package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/git"
)

const sequenceUsage = "usage: mugicrew sequence next <path>"

// runSequence is `mugicrew sequence next <path>`: the number a renumber of the
// numbered file at path moves to, printed alone at the rule's width. It reads only
// local git refs and the checked-in sequence rules of the repo path lives in.
func runSequence(argv []string, w io.Writer) error {
	if len(argv) != 2 || argv[0] != "next" {
		return errors.New(sequenceUsage)
	}
	abs, err := filepath.Abs(argv[1])
	if err != nil {
		return err
	}
	dir := filepath.Dir(abs)
	tail := filepath.Base(abs)
	for { // the file may not exist yet: judge from its nearest existing directory
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return fmt.Errorf("sequence next: no directory of %s exists", argv[1])
		}
		tail = filepath.Join(filepath.Base(dir), tail)
		dir = parent
	}
	top, err := git.TopLevelOf(dir)
	if err != nil {
		return fmt.Errorf("sequence next: %s is not in a git repo: %w", argv[1], err)
	}
	if real, err := filepath.EvalSymlinks(top); err == nil {
		top = real
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	rel, err := filepath.Rel(top, filepath.Join(dir, tail))
	if err != nil {
		return err
	}
	repo, rel, err := sequenceRepo(top, filepath.ToSlash(rel))
	if err != nil {
		return err
	}
	n, err := repo.SequenceNext(rel)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, n)
	return err
}

// sequenceRepo is the repo that judges rel (slash path under top, the current worktree)
// and the path within it. A path whose leading segments name a declared repo = dir that
// is not a repo root in this worktree is judged in the child at the Workspace root; a
// child checked out in neither is an error naming the context, never "matches no rule".
// Everything else is top's own. The repo table is read only when the config declares one.
func sequenceRepo(top, rel string) (git.Repo, string, error) {
	for name, dir := range cfglayer.Repos(top) {
		rest, ok := strings.CutPrefix(rel, strings.TrimSuffix(filepath.ToSlash(dir), "/")+"/")
		if !ok || git.IsRepoRoot(filepath.Join(top, dir)) {
			continue
		}
		wsRoot := top
		if cd, err := git.CommonDirOf(top); err == nil {
			if r, err := git.RepoRootAt(cd); err == nil {
				wsRoot = r
			}
		}
		if child := filepath.Join(wsRoot, dir); git.IsRepoRoot(child) {
			return git.Repo(child), rest, nil
		}
		return "", "", fmt.Errorf("sequence next: context %s's repo %s/ is not checked out here or at %s", name, dir, wsRoot)
	}
	return git.Repo(top), rel, nil
}
