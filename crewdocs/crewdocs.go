// Package crewdocs owns the Crew docs: the .md files every crewmate may Read by
// path, embedded in the binary and materialized lazily under the user cache dir
// at mugicrew/crewdocs/<content-hash>/ (ADR 0086). The hash keeps two builds
// (a pinned loop, a newer TUI) from overwriting each other; nothing is written
// into the Workspace. It lives outside apps/mugicrew/docs/, which the nix build
// excludes.
package crewdocs

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"jayulabs.org/mugicrew/git"
)

//go:embed *.md
var docs embed.FS

var (
	once   sync.Once
	dir    string
	dirErr error
)

// Dir returns the materialized Crew docs directory, writing it on first use in
// this process.
// ponytail: once-per-process (Spec D1) — a mid-process wipe or a first-call
// error sticks until restart; the next process heals. Cache only success and
// re-stat per call if a long-lived loop ever needs it.
func Dir() (string, error) {
	once.Do(func() {
		root, err := os.UserCacheDir()
		if err != nil {
			dirErr = err
			return
		}
		dir, dirErr = materialize(root, git.Mainline())
	})
	return dir, dirErr
}

// render is the embedded doc at p with {{MAINLINE}} filled: the trunk ref a
// doc's git command names (Spec slice 3). main renders the doc as it
// read before the token, so a main Workspace keeps its hash dir.
func render(p, mainline string) []byte {
	b, _ := docs.ReadFile(p)
	return bytes.ReplaceAll(b, []byte("{{MAINLINE}}"), []byte(mainline))
}

// contentHash is a sha256 over every rendered file's path and bytes, in
// WalkDir's lexical order.
func contentHash(mainline string) string {
	h := sha256.New()
	fs.WalkDir(docs, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		h.Write([]byte(p + "\x00"))
		h.Write(render(p, mainline))
		h.Write([]byte{0})
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// materialize ensures <root>/mugicrew/crewdocs/<hash>/ holds the docs rendered
// for mainline and returns it. A missing dir is written into a temp dir beside
// it and renamed into place; the loser of a rename race discards its temp and
// uses the winner's. A present dir is trusted as is.
// ponytail: old hash dirs are never pruned (~50 KB per build); prune when it matters.
func materialize(root, mainline string) (string, error) {
	parent := filepath.Join(root, "mugicrew", "crewdocs")
	target := filepath.Join(parent, contentHash(mainline))
	if _, err := os.Stat(target); err == nil {
		return target, nil
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(parent, ".tmp-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp) // no-op once renamed away
	err = fs.WalkDir(docs, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		return os.WriteFile(filepath.Join(tmp, p), render(p, mainline), 0o644)
	})
	if err != nil {
		return "", err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, target); err != nil {
		if _, statErr := os.Stat(target); statErr == nil {
			return target, nil // lost the race; the winner's dir is complete
		}
		return "", err
	}
	return target, nil
}
