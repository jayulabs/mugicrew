package contract

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Source is one "Source: !<iid> <discussion-id> <note-id>" line of a filed Critique or
// grilling: the Merge Request note it was filed from.
type Source struct {
	IID        int
	Discussion string
	Note       string
}

// String renders s as the text after the "Source: " prefix.
func (s Source) String() string { return fmt.Sprintf("!%d %s %s", s.IID, s.Discussion, s.Note) }

// SourcePrefix opens a Source line.
const SourcePrefix = "Source: "

// SourcesOf reads every well-formed Source line of text (a bead's body and notes, joined),
// in order. Malformed lines are skipped.
func SourcesOf(text string) []Source {
	var out []Source
	for _, line := range strings.Split(text, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), SourcePrefix+"!")
		if !ok {
			continue
		}
		fs := strings.Fields(rest)
		if len(fs) < 3 {
			continue
		}
		if iid, err := strconv.Atoi(fs[0]); err == nil {
			out = append(out, Source{IID: iid, Discussion: fs[1], Note: fs[2]})
		}
	}
	return out
}

// ledgerLine matches a line of a Forge Review's per-note ledger: "<note-id> file|fold|skip".
var ledgerLine = regexp.MustCompile(`^\s*(\S+)\s+(file|fold|skip)\b`)

// LedgerLine reads a ledger line as its note id and verdict (file, fold or skip).
func LedgerLine(line string) (note, verdict string, ok bool) {
	m := ledgerLine.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// The container body lines that record pending close-outs, written by Jinbe and read by the TUI's d.
const (
	CloseOutsDuePrefix     = "Close-outs due: "
	CloseOutsFailingPrefix = "Close-outs failing: "
)

// ForgeReviewCounts is the filed, folded and skipped note counts of a closed Forge Review.
type ForgeReviewCounts struct{ Filed, Folded, Skipped int }

var forgeReviewCountsRE = regexp.MustCompile(`^filed (\d+), folded (\d+), skipped (\d+)`)

// String renders c as the head of a Forge Review's close reason, followed by ": <rest>".
func (c ForgeReviewCounts) String() string {
	return fmt.Sprintf("filed %d, folded %d, skipped %d", c.Filed, c.Folded, c.Skipped)
}

// ParseForgeReviewCounts reads the counts off a close reason written with String.
func ParseForgeReviewCounts(reason string) (ForgeReviewCounts, bool) {
	m := forgeReviewCountsRE.FindStringSubmatch(reason)
	if m == nil {
		return ForgeReviewCounts{}, false
	}
	n := func(s string) int { v, _ := strconv.Atoi(s); return v }
	return ForgeReviewCounts{n(m[1]), n(m[2]), n(m[3])}, true
}

// FiledKind is the kind of bead a Forge Review files on a container of kind k: a
// grilling on a Chart, a Critique on a Checkpoint.
func FiledKind(k Kind) Kind {
	if k == KindChart {
		return KindGrilling
	}
	return KindCritique
}
