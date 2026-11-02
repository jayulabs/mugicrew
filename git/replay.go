package git

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Replay is ReplayOnto's verdict: the tree of the stranded range laid onto
// mainline when it applies cleanly, else the conflicting paths (Spec mc-d8xi1 D5).
type Replay struct {
	Tree      string   // the merged tree; "" when Conflicts is set
	Conflicts []string // sorted, deduplicated; nil on a clean replay
}

// Clean reports whether the replay applied without conflict.
func (p Replay) Clean() bool { return len(p.Conflicts) == 0 }

// ReplayOnto three-way merges head onto mainline with tip as the merge base
// (merge-tree --write-tree --merge-base=<tip> <mainline> <head>), so a head the
// container was rewritten past (tip not its ancestor, ADR 0263) replays the same.
// It writes tree objects only, never a ref or a commit, so a dry-run can call it
// as-is; SeedCommit turns a clean tree into a commit. Needs git >= 2.40, like
// Revert. A conflict (exit 1) is data; any other git failure is an error.
func (r Repo) ReplayOnto(tip, mainline, head string) (Replay, error) {
	out, err := r.runOut("merge-tree", "--write-tree", "--name-only", "--merge-base="+tip, mainline, head)
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return Replay{Conflicts: conflictPaths(out)}, nil
	}
	if err != nil {
		return Replay{}, err
	}
	tree, _, _ := strings.Cut(out, "\n")
	return Replay{Tree: tree}, nil
}

// SeedCommit writes one commit of tree on top of mainline with the given subject
// and trailer lines (one paragraph, in order) and returns its SHA. It moves no ref.
func (r Repo) SeedCommit(mainline, tree, subject string, trailers ...string) (string, error) {
	args := []string{"commit-tree", tree, "-p", mainline, "-m", subject}
	if len(trailers) > 0 {
		args = append(args, "-m", strings.Join(trailers, "\n"))
	}
	return r.run(args...)
}

// ErrNoCutRef is AdoptEffort's refusal when the old effort has no cut ref: the only
// refusal Spec mc-d8xi1 D5 lets a respin turn into a cherry-pick note.
var ErrNoCutRef = errors.New("git: effort has no cut ref")

// AdoptEffort points effort/<new> at effort/<old>'s head and its cut ref at the
// old one's, so Integrate on <new> replays exactly the old effort's own commits
// (Spec mc-d8xi1 D5). It refuses, creating no ref, when the old cut ref is missing
// (an effort cut before ADR 0264) or effort/<new> already exists.
func (r Repo) AdoptEffort(old, new string) error {
	cut, err := r.Head(CutRef(old))
	if errors.Is(err, ErrMissingRef) {
		return fmt.Errorf("adopt effort/%s: %w", old, ErrNoCutRef)
	}
	if err != nil {
		return fmt.Errorf("adopt effort/%s: %w", old, err)
	}
	head, err := r.Head("refs/heads/" + EffortBranch(old))
	if err != nil {
		return fmt.Errorf("adopt effort/%s: %w", old, err)
	}
	// one transaction, so a crash never leaves effort/<new> without its cut ref; the
	// all-zero old-value makes each create-only, so an existing ref is never moved
	tx := "start\ncreate refs/heads/" + EffortBranch(new) + " " + head + "\n" +
		"create " + CutRef(new) + " " + cut + "\ncommit\n"
	if _, err := runRawStdin(tx, nil, r.args("update-ref", "--stdin")...); err != nil {
		return err
	}
	return nil
}

// Moves are every file commit sha renamed against its first parent, each as
// `old → new`, in path order: the moves a landed Renumber Checkpoint made on mainline.
func (r Repo) Moves(sha string) ([]string, error) {
	out, err := r.run("diff", "--name-status", "-M", sha+"^1", sha)
	if err != nil {
		return nil, err
	}
	var moves []string
	for _, l := range strings.Split(out, "\n") {
		if f := strings.Split(l, "\t"); len(f) == 3 && strings.HasPrefix(f[0], "R") {
			moves = append(moves, f[1]+" → "+f[2])
		}
	}
	return moves, nil
}
