package git

import (
	"fmt"
	"strconv"
	"strings"
)

// branchAdds is one other open branch's added paths, for seqNext.
type branchAdds struct {
	Branch string
	Added  []string
}

// seqNext is the pure core of `mugicrew sequence next`: the number a renumber of p
// moves to, one above the max over mainline's files in p's directory under p's rule
// and every file another branch adds there, printed at p's own width. head is what
// the current branch adds: a file an other branch adds with the same directory and
// slug as one of those is the same file on another lap and is skipped. p matching no
// rule, two, or a result that leaves the rule (9999 → 10000 under {n:4}) is an error;
// gaps are accepted.
func seqNext(rules []Rule, p string, mainline, head []string, others []branchAdds) (string, error) {
	ri, err := ruleOf(rules, p)
	if err != nil {
		return "", err
	}
	if ri < 0 {
		return "", fmt.Errorf("sequence next: %s matches no sequence rule", p)
	}
	rule := rules[ri]
	dir, _, digits := seqParts(rule, p)
	ownSlugs := map[string]bool{}
	for _, h := range head {
		if i, err := ruleOf(rules, h); err == nil && i == ri {
			ownSlugs[seqSlug(rule, h)] = true
		}
	}
	max := 0
	see := func(f string) error {
		if i, err := ruleOf(rules, f); err != nil || i != ri {
			return err
		}
		d, key, _ := seqParts(rule, f)
		if d != dir {
			return nil
		}
		n, err := strconv.Atoi(key)
		if err != nil {
			return fmt.Errorf("sequence next: %s: number outgrows an int", f)
		}
		max = max2(max, n)
		return nil
	}
	for _, f := range mainline {
		if err := see(f); err != nil {
			return "", err
		}
	}
	for _, o := range others {
		for _, f := range o.Added {
			if ownSlugs[seqSlug(rule, f)] {
				continue
			}
			if err := see(f); err != nil {
				return "", err
			}
		}
	}
	next := max + 1
	np := renumberPath(rule, p, next)
	if np == "" {
		return "", fmt.Errorf("sequence next: %d outgrows the %d-digit width of %s (rule %s); escalate", next, len(digits), p, rule.Template)
	}
	_, _, out := seqParts(rule, np)
	return out, nil
}

func max2(a, b int) int {
	if b > a {
		return b
	}
	return a
}

// openBranchPrefixes are the local branches whose adds a renumber must clear: every
// open container and effort branch.
var openBranchPrefixes = []string{"refs/heads/checkpoint/", "refs/heads/chart/", "refs/heads/effort/"}

// SequenceNext is seqNext over repo r's local refs: p is repo-relative, mainline is
// its trunk's files, and each other checkpoint/*, chart/* or effort/* branch adds
// what diff --diff-filter=A shows from its merge-base with mainline. HEAD's branch
// is skipped, its own adds feed the same-slug exemption. No bead is read.
func (r Repo) SequenceNext(p string) (string, error) {
	rules, err := SequenceRules(r)
	if err != nil {
		return "", err
	}
	main := r.mainline()
	onMain, err := r.run("ls-tree", "-r", "--name-only", main)
	if err != nil {
		return "", err
	}
	adds := func(rev string) ([]string, error) {
		mb, err := r.run("merge-base", main, rev)
		if err != nil {
			return nil, err
		}
		out, err := r.run(addedPathsArgs(mb, rev)...)
		if err != nil {
			return nil, err
		}
		return strings.Fields(out), nil
	}
	head, err := adds("HEAD")
	if err != nil {
		return "", err
	}
	cur, _ := r.run("symbolic-ref", "-q", "HEAD") // "" when detached
	refs, err := r.run(append([]string{"for-each-ref", "--format=%(refname)"}, openBranchPrefixes...)...)
	if err != nil {
		return "", err
	}
	var others []branchAdds
	for ref := range strings.Lines(refs) {
		ref = strings.TrimSpace(ref)
		if ref == "" || ref == cur {
			continue
		}
		a, err := adds(ref)
		if err != nil {
			return "", err
		}
		others = append(others, branchAdds{ref, a})
	}
	return seqNext(rules, p, strings.Split(onMain, "\n"), head, others)
}
