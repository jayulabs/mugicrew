package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

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
	n, err := git.Repo(top).SequenceNext(filepath.ToSlash(rel))
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, n)
	return err
}
