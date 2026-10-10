package looplock

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"jayulabs.org/mugicrew/git"
)

// buildLink is the pinned build's out-link in the Workspace's state dir: its GC
// root from build until exec (ADR 0060). One fixed name; each start re-points it,
// and a running loop's path is held by its own process's runtime roots.
const buildLink = "loop-build"

// MainlineBuild builds the flake's packages.mugicrew at the source repo's
// mainline (ADR 0060 §1, §5, ADR 0090 §2): it resolves that SHA once against
// sourceDir (the source repo's git common dir) and builds exactly that rev with
// BuildAt (never the invoking checkout or its dirty state), rooted under stateDir
// (the Workspace's git common dir), returning the rev and the store binary.
func MainlineBuild(sourceDir, stateDir string) (rev, bin string, err error) {
	rev, err = git.MainlineAt(sourceDir)
	if err != nil {
		return "", "", err
	}
	bin, err = BuildAt(context.Background(), sourceDir, stateDir, rev)
	if err != nil {
		return "", "", err
	}
	return rev, bin, nil
}

// BuildAt builds the source repo's mainline@rev packages.mugicrew, roots it with
// the out-link in the Workspace's state dir (ADR 0090 §6), and returns its store
// binary (ADR 0062 §1). Cancelling ctx kills the nix child.
func BuildAt(ctx context.Context, sourceDir, stateDir, rev string) (bin string, err error) {
	dir := filepath.Join(stateDir, "mugicrew")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	out, err := runNix(ctx, "build", flakeRef(sourceDir, rev),
		"--out-link", filepath.Join(dir, buildLink), "--print-out-paths")
	if err != nil {
		return "", err
	}
	store, _, _ := strings.Cut(out, "\n")
	return filepath.Join(store, "bin", "mugicrew"), nil
}

// OutPathAt evaluates the source's mainline@rev packages.mugicrew outPath without
// building it: the store dir BuildAt of the same rev yields (ADR 0062 §1).
// Cancelling ctx kills the nix child.
func OutPathAt(ctx context.Context, sourceDir, rev string) (string, error) {
	return runNix(ctx, "eval", "--raw", flakeRef(sourceDir, rev)+".outPath")
}

// flakeRef is mainline@rev's mugicrew in the source repo (its git common dir).
func flakeRef(sourceDir, rev string) string {
	return "git+file://" + sourceDir + "?ref=" + git.MainlineOf(sourceDir) + "&rev=" + rev + "#mugicrew"
}

// runNix runs nix args and returns its trimmed stdout. A nix failure is nix's own
// stderr verbatim; no nix is Go's exec error.
func runNix(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "nix", args...)
	cmd.WaitDelay = 5 * time.Second // a cancelled nix's grandchild holding stdout can't pin us
	var stderr bytes.Buffer         // not Output's: it elides the middle of a long log
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	var ee *exec.ExitError
	if msg := strings.TrimSpace(stderr.String()); errors.As(err, &ee) && msg != "" {
		return "", errors.New(msg)
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
