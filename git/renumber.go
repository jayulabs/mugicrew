package git

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strings"
)

// The rule points of ADR 0397's renumber-only check, as a reject names them.
const (
	PointPair     = "pair"
	PointReplay   = "replay"
	PointCompare  = "compare"
	PointOwnLines = "own-lines"
	PointSweep    = "sweep"
)

// Renamed is one numbered file the new head carries under a new number: its
// path in the new head, its path in the approved pin, and the padded old and new
// numbers (0394, 0395).
type Renamed struct {
	Path, OldPath, Old, New string
}

// RenumberVerdict is VerifyRenumber's answer. Accept lists the renames made. A
// reject names the failed Point and the first differing File, with Detail saying
// how. A git failure is never a verdict: VerifyRenumber returns it as an error.
type RenumberVerdict struct {
	Accept  bool
	Renames []Renamed
	Point   string
	File    string
	Detail  string
}

func rejectRenumber(point, file, format string, a ...any) RenumberVerdict {
	return RenumberVerdict{Point: point, File: file, Detail: fmt.Sprintf(format, a...)}
}

// VerifyRenumber judges whether newHead is oldPin, the approved squash, replayed
// onto a new base modulo its own renumber (ADR 0397 points 1–6): the check both the
// Jinbe and Nami hooks call. Each side's base is merge-base(mainline, tip). The
// replay needs git >= 2.40 (merge-tree --write-tree with --merge-base, like
// Revert's). Every git failure is an error, so the caller retries next tick.
func (r Repo) VerifyRenumber(rules []Rule, oldPin, newHead, mainline string) (RenumberVerdict, error) {
	ob, err := r.run("merge-base", mainline, oldPin)
	if err != nil {
		return RenumberVerdict{}, err
	}
	nb, err := r.run("merge-base", mainline, newHead)
	if err != nil {
		return RenumberVerdict{}, err
	}

	// pair
	oldAdded, err := r.addedPaths(ob, oldPin)
	if err != nil {
		return RenumberVerdict{}, err
	}
	newAdded, err := r.addedPaths(nb, newHead)
	if err != nil {
		return RenumberVerdict{}, err
	}
	renames, mapping, bad := pairRenumbered(rules, oldAdded, newAdded)
	if bad != nil {
		return *bad, nil
	}
	if len(mapping) == 0 {
		return rejectRenumber(PointPair, "", "no numbered file changes its number: not a renumber lap"), nil
	}

	// replay
	out, err := r.runOut("merge-tree", "--write-tree", "--name-only", "--merge-base="+ob, nb, oldPin)
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 { // exit 1 is a conflict; any other code is a git failure
			file := ""
			if lines := strings.Split(out, "\n"); len(lines) > 1 {
				file = lines[1]
			}
			return rejectRenumber(PointReplay, file, "the approved squash does not replay cleanly onto the new base"), nil
		}
		return RenumberVerdict{}, err
	}
	tree, _, _ := strings.Cut(out, "\n")

	// compare, own-lines, sweep
	oldPaths := map[string]string{} // new path -> old path
	for _, rn := range renames {
		oldPaths[rn.Path] = rn.OldPath
	}
	oldSide := map[string]bool{}
	for _, rn := range renames {
		oldSide[rn.OldPath] = true
	}
	added, err := r.addedLines(ob, oldPin)
	if err != nil {
		return RenumberVerdict{}, err
	}
	diffed, err := r.run("diff", "--no-renames", "--name-only", "-z", tree, newHead)
	if err != nil {
		return RenumberVerdict{}, err
	}
	var files []string // new-head paths to check, in a stable order
	for _, p := range strings.Split(diffed, "\x00") {
		if p != "" && !(oldSide[p] && oldPaths[p] == "") { // a renamed file's old path is judged under its new one
			files = append(files, p)
		}
	}
	for old := range added { // a sweep miss hides in a file the replay left identical
		p := old
		for np, op := range oldPaths {
			if op == old {
				p = np
			}
		}
		files = append(files, p)
	}
	slices.Sort(files)
	files = slices.Compact(files)

	for _, p := range files {
		op := p
		if o, ok := oldPaths[p]; ok {
			op = o
		}
		tl, tok, err := r.blobLines(tree, op)
		if err != nil {
			return RenumberVerdict{}, err
		}
		nl, nok, err := r.blobLines(newHead, p)
		if err != nil {
			return RenumberVerdict{}, err
		}
		if !tok && !nok {
			continue
		}
		if tok != nok {
			return rejectRenumber(PointCompare, p, "file is %s in the new head only", map[bool]string{true: "dropped", false: "added"}[tok]), nil
		}
		if len(tl) != len(nl) {
			return rejectRenumber(PointCompare, p, "line count %d vs %d", len(tl), len(nl)), nil
		}
		own := added[op]
		for i, t := range tl {
			n := nl[i]
			if t == n {
				if own[t] && substituteTokens(t, mapping) != t {
					return rejectRenumber(PointSweep, p, "line %d keeps its old number: %q", i+1, t), nil
				}
				continue
			}
			if !own[t] {
				return rejectRenumber(PointOwnLines, p, "line %d changes a line the approved squash did not add: %q", i+1, t), nil
			}
			if substituteTokens(t, mapping) != n {
				return rejectRenumber(PointCompare, p, "line %d is not its renumbered substitution: %q → %q", i+1, t, n), nil
			}
		}
	}
	return RenumberVerdict{Accept: true, Renames: renames}, nil
}

// addedPaths is every file tip adds over base.
func (r Repo) addedPaths(base, tip string) ([]string, error) {
	out, err := r.run("diff", "--no-renames", "--name-only", "--diff-filter=A", "-z", base, tip)
	if err != nil {
		return nil, err
	}
	var ps []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			ps = append(ps, p)
		}
	}
	return ps, nil
}

// pairRenumbered pairs the numbered files the old squash adds with the new
// squash's, one to one by (rule, path minus its digits), and returns the renames
// and the old→new number map (changed numbers only). A non-nil reject names the
// first path that does not pair.
func pairRenumbered(rules []Rule, oldAdded, newAdded []string) ([]Renamed, map[string]string, *RenumberVerdict) {
	type key struct {
		rule int
		slug string
	}
	index := func(paths []string) (map[key]string, *RenumberVerdict) {
		m := map[key]string{}
		for _, p := range paths {
			i, err := ruleOf(rules, p)
			if err != nil {
				v := rejectRenumber(PointPair, p, "%v", err)
				return nil, &v
			}
			if i < 0 {
				continue
			}
			k := key{i, seqSlug(rules[i], p)}
			if prev, dup := m[k]; dup {
				v := rejectRenumber(PointPair, p, "collides with %s", prev)
				return nil, &v
			}
			m[k] = p
		}
		return m, nil
	}
	om, bad := index(oldAdded)
	if bad != nil {
		return nil, nil, bad
	}
	nm, bad := index(newAdded)
	if bad != nil {
		return nil, nil, bad
	}
	var renames []Renamed
	mapping := map[string]string{}
	keys := make([]key, 0, len(om))
	for k := range om {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b key) int { return strings.Compare(om[a], om[b]) })
	for _, k := range keys {
		op := om[k]
		np, ok := nm[k]
		if !ok {
			v := rejectRenumber(PointPair, op, "the approved squash adds this numbered file; the new head does not (dropped, or its slug or directory changed)")
			return nil, nil, &v
		}
		delete(nm, k)
		_, _, od := seqParts(rules[k.rule], op)
		_, _, nd := seqParts(rules[k.rule], np)
		if od == nd {
			continue
		}
		if prev, dup := mapping[od]; dup && prev != nd {
			v := rejectRenumber(PointPair, np, "number %s maps to both %s and %s", od, prev, nd)
			return nil, nil, &v
		}
		mapping[od] = nd
		renames = append(renames, Renamed{Path: np, OldPath: op, Old: od, New: nd})
	}
	if len(nm) > 0 {
		var extra []string
		for _, p := range nm {
			extra = append(extra, p)
		}
		slices.Sort(extra)
		v := rejectRenumber(PointPair, extra[0], "the new head adds this numbered file; the approved squash does not")
		return nil, nil, &v
	}
	return renames, mapping, nil
}

// addedLines is, per file the squash base..tip touches, the set of lines it adds.
func (r Repo) addedLines(base, tip string) (map[string]map[string]bool, error) {
	out, err := r.runRaw("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames", "-U0", base, tip)
	if err != nil {
		return nil, err
	}
	files := map[string]map[string]bool{}
	var cur map[string]bool
	inHunk := false
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, "diff --git "):
			inHunk, cur = false, nil
		case strings.HasPrefix(l, "+++ "):
			if !inHunk {
				if p, ok := strings.CutPrefix(l, "+++ b/"); ok {
					cur = map[string]bool{}
					files[p] = cur
				} else { // +++ /dev/null: a deletion adds no lines
					cur = nil
				}
			}
		case strings.HasPrefix(l, "@@"):
			inHunk = true
		case inHunk && cur != nil && strings.HasPrefix(l, "+"):
			cur[l[1:]] = true
		}
	}
	return files, nil
}

// blobLines is path's lines at rev, and whether it exists there.
func (r Repo) blobLines(rev, path string) ([]string, bool, error) {
	ls, err := r.run("ls-tree", rev, "--", path)
	if err != nil {
		return nil, false, err
	}
	if ls == "" {
		return nil, false, nil
	}
	out, err := r.runRaw("cat-file", "blob", rev+":"+path)
	if err != nil {
		return nil, false, err
	}
	return strings.Split(out, "\n"), true, nil
}

var alnumRun = regexp.MustCompile(`[0-9A-Za-z]+`)

// substituteTokens rewrites each old number standing alone in line (no letter or
// digit beside it) to its new one, in a single pass so a chain 0394 → 0395 → 0396
// cannot double-step. An unpadded 394 is never a token.
func substituteTokens(line string, mapping map[string]string) string {
	return alnumRun.ReplaceAllStringFunc(line, func(run string) string {
		if n, ok := mapping[run]; ok {
			return n
		}
		return run
	})
}
