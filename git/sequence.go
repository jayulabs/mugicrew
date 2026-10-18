package git

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Rule is one numbered-sequence template, compiled (ADR 0214): the files it
// matches number themselves per directory, by value. Build one with CompileRule.
type Rule struct {
	Template string
	re       *regexp.Regexp
}

// ADRRule is the built-in, always-on rule: ADR 0030's guard on the shared matcher.
var ADRRule = func() Rule {
	r, err := CompileRule("**/docs/adr/{n:4}-*.md")
	if err != nil {
		panic(err)
	}
	return r
}()

// SequenceRules is the rule set that applies to repo r, ADRRule first, wired by
// the loop package's init to the checked-in config (git can't import cfglayer),
// like LandStrategyName; until then it is the built-in ADR rule only. An error
// refuses the check (and so the Land) rather than skipping a rule.
var SequenceRules = func(Repo) ([]Rule, error) { return []Rule{ADRRule}, nil }

// CompileRule compiles a path template to one anchored regexp: **/ is zero or
// more whole directories, * stays in one segment, {n} is one or more digits and
// {n:W} exactly W. Exactly one {n…}, in the file name; anything else is an error.
func CompileRule(tmpl string) (Rule, error) {
	var b strings.Builder
	b.WriteString("^")
	nums := 0
	for s := tmpl; s != ""; {
		switch {
		case strings.HasPrefix(s, "**/"):
			b.WriteString("(?:.*/)?")
			s = s[3:]
		case strings.HasPrefix(s, "**"):
			return Rule{}, fmt.Errorf("sequence template %q: ** must be followed by /", tmpl)
		case s[0] == '*':
			b.WriteString("[^/]*")
			s = s[1:]
		case s[0] == '{':
			end := strings.IndexByte(s, '}')
			if end < 0 {
				return Rule{}, fmt.Errorf("sequence template %q: unclosed {", tmpl)
			}
			tok := s[1:end]
			s = s[end+1:]
			nums++
			switch w, hasW := strings.CutPrefix(tok, "n:"); {
			case tok == "n":
				b.WriteString("([0-9]+)")
			case hasW:
				n, err := strconv.Atoi(w)
				if err != nil || n < 1 {
					return Rule{}, fmt.Errorf("sequence template %q: bad width in {%s}", tmpl, tok)
				}
				fmt.Fprintf(&b, "([0-9]{%d})", n)
			default:
				return Rule{}, fmt.Errorf("sequence template %q: unknown placeholder {%s}", tmpl, tok)
			}
			if strings.Contains(s, "/") {
				return Rule{}, fmt.Errorf("sequence template %q: {n} must be in the file name", tmpl)
			}
		default:
			b.WriteString(regexp.QuoteMeta(s[:1]))
			s = s[1:]
		}
	}
	if nums != 1 {
		return Rule{}, fmt.Errorf("sequence template %q: want exactly one {n}, found %d", tmpl, nums)
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return Rule{}, fmt.Errorf("sequence template %q: %w", tmpl, err)
	}
	return Rule{Template: tmpl, re: re}, nil
}

// seqParts splits a path the rule matches into its number space (its own
// directory), its value key (digits sans leading zeros, "0" if all zeros; a
// string, so it cannot overflow) and the raw digits. All "" if it doesn't match.
func seqParts(r Rule, p string) (dir, key, digits string) {
	m := r.re.FindStringSubmatch(p)
	if m == nil {
		return "", "", ""
	}
	key = strings.TrimLeft(m[1], "0")
	if key == "" {
		key = "0"
	}
	return path.Dir(p) + "/", key, m[1]
}

// seqSlug is the path with its captured digits removed, or "" if r doesn't match:
// what a renumber keeps (assertion 3's exemption).
func seqSlug(r Rule, p string) string {
	m := r.re.FindStringSubmatchIndex(p)
	if m == nil {
		return ""
	}
	return p[:m[2]] + p[m[3]:]
}

// renumberPath splices n over the capture at the renamed file's own width; ""
// if p isn't the rule's or the result leaves it (9999 → 10000 under {n:4}).
// No Go caller renumbers yet: the crew doc's rules (sequence-renumber.md) lean on it.
func renumberPath(r Rule, p string, n int) string {
	m := r.re.FindStringSubmatchIndex(p)
	if m == nil {
		return ""
	}
	np := p[:m[2]] + fmt.Sprintf("%0*d", m[3]-m[2], n) + p[m[3]:]
	if !r.re.MatchString(np) {
		return ""
	}
	return np
}

// ruleOf is the one rule p belongs to, -1 for none; two is the overlap error.
func ruleOf(rules []Rule, p string) (int, error) {
	hit := -1
	for i, r := range rules {
		if !r.re.MatchString(p) {
			continue
		}
		if hit >= 0 {
			return 0, fmt.Errorf("git: numbered-file rules overlap on %s: %s and %s", p, rules[hit].Template, r.Template)
		}
		hit = i
	}
	return hit, nil
}

// slotClash is one added path and the other mainline paths holding its number.
type slotClash struct {
	Added, Rule string
	Others      []string
}

// seqSlotClashes is the pure core of numberClashes and PostLandClashes: each
// added path whose number (by value, in its own directory, under its rule)
// mainline already holds under a DIFFERENT path. Same-path hits are skipped — an
// add/add of one path is a textual conflict Land's rebase already catches, and
// it is also what a re-run sees after the Chart itself landed. A path matching
// two rules is an error, not a clash.
func seqSlotClashes(rules []Rule, added, mainline []string) ([]slotClash, error) {
	type slot struct {
		rule     int
		dir, key string
	}
	byNum := map[slot][]string{}
	for _, p := range mainline {
		i, err := ruleOf(rules, p)
		if err != nil {
			return nil, err
		}
		if i >= 0 {
			dir, key, _ := seqParts(rules[i], p)
			byNum[slot{i, dir, key}] = append(byNum[slot{i, dir, key}], p)
		}
	}
	var clashes []slotClash
	for _, p := range added {
		i, err := ruleOf(rules, p)
		if err != nil {
			return nil, err
		}
		if i < 0 {
			continue
		}
		dir, key, _ := seqParts(rules[i], p)
		var others []string
		for _, m := range byNum[slot{i, dir, key}] {
			if m != p {
				others = append(others, m)
			}
		}
		if len(others) > 0 {
			clashes = append(clashes, slotClash{p, rules[i].Template, others})
		}
	}
	return clashes, nil
}

// seqClashes is seqSlotClashes rendered as "added (rule T) (vs mainline p, ...)".
func seqClashes(rules []Rule, added, mainline []string) ([]string, error) {
	cs, err := seqSlotClashes(rules, added, mainline)
	var out []string
	for _, c := range cs {
		out = append(out, fmt.Sprintf("%s (rule %s) (vs mainline %s)", c.Added, c.Rule, strings.Join(c.Others, ", ")))
	}
	return out, err
}

// TreeOverlaps is every file in HEAD's tree that two of rules match, each as
// ruleOf's overlap error: Land's overlap rule, judged on the current tree.
func (r Repo) TreeOverlaps(rules []Rule) ([]error, error) {
	files, err := r.run("ls-tree", "-r", "--name-only", "HEAD")
	if err != nil {
		return nil, err
	}
	var out []error
	for _, p := range strings.Split(files, "\n") {
		if _, err := ruleOf(rules, p); err != nil {
			out = append(out, err)
		}
	}
	return out, nil
}
