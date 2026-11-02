package bd

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// RemnantBody is a Remnant's body parsed once: the prose header naming the landed
// container and the sha a Forge merge landed it at, then `key: value` lines. It is pure
// text, with no git. The format is frozen: a line counts when it starts with its exact
// "key: " prefix, except Stranded:, which matches without the space. Landed-tip: and
// Stranded: are single-valued: of repeated lines only the first (the first non-empty
// Stranded: line) is read. Jinbe writes one of each.
type RemnantBody struct {
	landedID, landedSHA string
	tip                 string
	strandedLine        string // the first non-empty Stranded: line, verbatim
	strandedRange       string
	strandedOld         string
	kept                []KeptEntry
	children            []ChildEntry
	errands             []string
}

// KeptEntry is one `Kept: <branch> <rest>` line: the effort branch, what follows it, and
// the value after "Kept: " whole. Format it back with FormatKept.
type KeptEntry struct {
	Branch, Rest, Value string
}

// ChildEntry is one `Child: <id> <rest>` line: the child's id and the value whole.
type ChildEntry struct {
	ID, Value string
}

// The Kept: and Child: line prefixes, shared with the keyed-line check in remnant.go.
const (
	keptPrefix  = "Kept: "
	childPrefix = "Child: "
)

var (
	remnantLandedAt = regexp.MustCompile(`landed at (\S+?)\.?(\s|$)`)
	remnantOf       = regexp.MustCompile(`^Remnant of (\S+), which`)
)

// ParseRemnantBody reads a Remnant body in one pass.
func ParseRemnantBody(body string) RemnantBody {
	var r RemnantBody
	if m := remnantOf.FindStringSubmatch(body); m != nil {
		r.landedID = m[1]
	}
	if m := remnantLandedAt.FindStringSubmatch(body); m != nil {
		r.landedSHA = m[1]
	}
	seenTip := false
	for _, l := range strings.Split(body, "\n") {
		if v, ok := strings.CutPrefix(l, "Landed-tip: "); ok && !seenTip {
			seenTip = true
			r.tip = FirstWord(v)
		}
		if rest, ok := strings.CutPrefix(l, "Stranded:"); ok {
			if f := strings.Fields(rest); len(f) > 0 && r.strandedLine == "" {
				r.strandedLine = l
				r.strandedOld = strandedOldHead(f)
			}
			if v, ok := strings.CutPrefix(l, "Stranded: "); ok && r.strandedRange == "" {
				if f, _, _ := strings.Cut(v, " "); strings.Contains(f, "..") && !strings.Contains(f, "...") {
					r.strandedRange = f
				}
			}
		}
		if v, ok := strings.CutPrefix(l, keptPrefix); ok {
			e := KeptEntry{Value: v}
			e.Branch, e.Rest, _ = strings.Cut(strings.TrimSpace(v), " ")
			r.kept = append(r.kept, e)
		}
		if v, ok := strings.CutPrefix(l, childPrefix); ok {
			r.children = append(r.children, ChildEntry{ID: FirstWord(v), Value: v})
		}
		if v, ok := strings.CutPrefix(l, "Errand: "); ok && FirstWord(v) != "" {
			r.errands = append(r.errands, v)
		}
	}
	return r
}

// strandedOldHead is the head a Stranded: line's last word names after "..", "" when
// it names none. A rewritten branch's line ends `<tip>...<head>`.
func strandedOldHead(w []string) string {
	last := w[len(w)-1]
	if i := strings.LastIndex(last, ".."); i >= 0 {
		return strings.TrimLeft(last[i:], ".")
	}
	return ""
}

// LandedID is the landed container's id from the prose header, "" when absent.
func (r RemnantBody) LandedID() string { return r.landedID }

// LandedSHA is the sha the Forge merge landed the container at, "" when absent.
func (r RemnantBody) LandedSHA() string { return r.landedSHA }

// LandedTip is the Landed-tip: value (its first word, so "unknown" when the squash carries
// no Tip:), "" when the body has no such line.
func (r RemnantBody) LandedTip() string { return r.tip }

// StrandedLine is the Stranded: line verbatim, "" when absent.
func (r RemnantBody) StrandedLine() string { return r.strandedLine }

// StrandedRange is the Stranded: line's Tip..head range, "" when absent or for a
// rewritten branch's <tip>...<head> range-diff form.
func (r RemnantBody) StrandedRange() string { return r.strandedRange }

// StrandedHead is the old branch head the Stranded: line names, "" when none.
func (r RemnantBody) StrandedHead() string { return r.strandedOld }

// Kept is each Kept: entry in body order.
func (r RemnantBody) Kept() []KeptEntry { return r.kept }

// KeptFor is the last Kept: entry for branch. A Remnant now holds one Kept: line per
// branch (appendMissing skips a second); last-wins keeps parsing the Remnants written
// before that, where a re-run carry appended the newer state.
func (r RemnantBody) KeptFor(branch string) (KeptEntry, bool) {
	for i := len(r.kept) - 1; i >= 0; i-- {
		if r.kept[i].Branch == branch {
			return r.kept[i], true
		}
	}
	return KeptEntry{}, false
}

// KeptBranches is the distinct Kept: branches, in first-seen order.
func (r RemnantBody) KeptBranches() []string {
	var out []string
	for _, e := range r.kept {
		if e.Branch != "" && !slices.Contains(out, e.Branch) {
			out = append(out, e.Branch)
		}
	}
	return out
}

// Children is each Child: entry in body order.
func (r RemnantBody) Children() []ChildEntry { return r.children }

// Errands is each Errand: value whole (id first), in body order.
func (r RemnantBody) Errands() []string { return r.errands }

// ErrandIDs is each Errand: value's carried Errand id, in body order.
func (r RemnantBody) ErrandIDs() []string {
	var out []string
	for _, e := range r.errands {
		out = append(out, FirstWord(e))
	}
	return out
}

// FormatRemnantHeader is a Remnant's first lines: the prose header naming the landed
// container and sha, then Landed-tip:, then strandedLine when it is not empty.
func FormatRemnantHeader(containerID, sha, tip, strandedLine string) string {
	return strings.Join(append([]string{
		fmt.Sprintf("Remnant of %s, which a Forge merge landed at %s.", containerID, sha),
	}, FormatTipLines(tip, strandedLine)...), "\n")
}

// FormatTipLines are the Landed-tip: line and the Stranded: line (when not empty) a
// Remnant and its respin share.
func FormatTipLines(tip, strandedLine string) []string {
	lines := []string{"Landed-tip: " + tip}
	if strandedLine != "" {
		lines = append(lines, strandedLine)
	}
	return lines
}

// FormatKept is a Kept: line for value, the part after "Kept: " (a KeptEntry's Value).
func FormatKept(value string) string { return keptPrefix + value }

// FormatKeptLine is a Kept: line for branch with a cut ref.
func FormatKeptLine(branch, cut, tip string) string {
	return FormatKept(fmt.Sprintf("%s %s..%s", branch, cut, tip))
}

// FormatKeptNoCutLine is a Kept: line for branch whose cut ref is gone.
func FormatKeptNoCutLine(branch, tip string) string {
	return FormatKept(fmt.Sprintf("%s %s (no cut ref)", branch, tip))
}

// FormatChildLine is a Child: line for value.
func FormatChildLine(value string) string { return childPrefix + value }

// FirstWord is the first space-separated word of s, "" when it has none.
func FirstWord(s string) string {
	w, _, _ := strings.Cut(strings.TrimSpace(s), " ")
	return w
}
