package jinbe

import (
	"fmt"
	"strings"
	"sync"

	"jayulabs.org/mugicrew/bd"
)

// graphName is forge.CurrentUser, cached per process: the account does not change.
var graphName struct {
	sync.Mutex
	name string
}

func currentUser() (string, error) {
	graphName.Lock()
	defer graphName.Unlock()
	if graphName.name == "" {
		name, err := forgeUser()
		if err != nil {
			return "", err
		}
		graphName.name = name
	}
	return graphName.name, nil
}

const replyMarker = "<!-- mugicrew-reply: "

// replyMarkerLine is the hidden last line of Jinbe's reply: the Qualified Bead Id the
// reply answers (ADR 0200).
func replyMarkerLine(beadID, graphID, name string) string {
	return fmt.Sprintf("%s%s@%s %s -->", replyMarker, beadID, graphID, name)
}

// anyReplyMarker is whether body carries a mugicrew-reply marker for any bead.
func anyReplyMarker(body string) bool {
	marked, _ := replyMarked(body, "", "")
	return marked
}

// replyMarked is whether the note carries any mugicrew-reply marker line (never a human
// reply), and forB whether one is for exactly this Qualified Bead Id (id and graph
// hash; the trailing name is ignored).
func replyMarked(body, beadID, graphID string) (marked, forB bool) {
	for _, line := range strings.Split(body, "\n") {
		inner, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), replyMarker)
		if !ok {
			continue
		}
		inner, ok = strings.CutSuffix(inner, " -->")
		if !ok {
			continue
		}
		marked = true
		if q, _, _ := strings.Cut(inner, " "); q == beadID+"@"+graphID {
			forB = true
		}
	}
	return marked, forB
}

// replyReasonMax is how much of a rationale a close-out reply carries (ADR 0319 (a-filed-comment-is-closed-out…)).
const replyReasonMax = 2000

// verdict is what a close-out reply leads with (ADR 0339 (a-close-out-reply-reads-its-verdict…)).
type verdict int

const (
	verdictClosed verdict = iota // nothing recorded a verdict: the neutral reply
	verdictAccepted
	verdictRejected
	verdictAnswered
	verdictDeferred
)

// pivotRationale is the fixed rationale of a pivoted: close, which reads as Deferred.
const pivotRationale = "The Spec is being revised; this ask moves with it."

// verdictPrefixes are the close-reason prefixes that name a verdict, matched
// case-insensitively after leading whitespace.
var verdictPrefixes = []struct {
	prefix string
	v      verdict
}{
	{"accepted:", verdictAccepted},
	{"rejected:", verdictRejected},
	{"answered:", verdictAnswered},
	{"deferred:", verdictDeferred},
	{"pivoted:", verdictDeferred},
}

// parseVerdict reads a close reason's verdict prefix: ok is false with no known prefix,
// and rationale is then the whole trimmed reason ("" for bd's literal Closed). A pivoted:
// reason is Deferred with the fixed rationale, and rest, the pivot ids, is kept.
func parseVerdict(reason string) (v verdict, rationale, rest string, ok bool) {
	reason = strings.TrimSpace(reason)
	for _, p := range verdictPrefixes {
		if len(reason) >= len(p.prefix) && strings.EqualFold(reason[:len(p.prefix)], p.prefix) {
			rest = strings.TrimSpace(reason[len(p.prefix):])
			if p.prefix == "pivoted:" {
				return p.v, pivotRationale, rest, true
			}
			return p.v, rest, "", true
		}
	}
	if reason == bd.DefaultCloseReason {
		reason = ""
	}
	return verdictClosed, reason, "", false
}

// sweptReply is the verdict and fixed rationale of a swept Critique's reply (ADR 0344 (a-swept-critique-is-deferred-only…)):
// Deferred when the pivot carried its ask into the successor, else the neutral Closed.
// The raw reason is never shown.
func sweptReply(s bd.SweptReason, carried bool) (verdict, string) {
	switch {
	case s.Escalated && carried:
		return verdictDeferred, "The Spec was sent back to charting and this Merge Request was replaced; this ask moves to the new Chart."
	case s.Escalated:
		return verdictClosed, "The Spec was sent back to charting and this Merge Request was replaced; this ask was not carried forward."
	case carried:
		return verdictDeferred, "The Spec was revised and this Merge Request was replaced; this ask moves to the revised Spec."
	}
	return verdictClosed, "The Spec was revised and this Merge Request was replaced; this ask was not carried into the revised Spec."
}

// closeVerdict is b's verdict and rationale (the rationale may be empty): the close
// reason's prefix, else the fallback, which never yields Rejected or Deferred: Accepted
// with an integrate commit (sha8 non-empty), else Answered for a "Reviewer asks …" bead
// (ADR 0317 (a-forge-review-files-whatever…)), else Closed.
func closeVerdict(b bd.Bead, sha8 string) (v verdict, rationale string) {
	v, rationale, _, ok := parseVerdict(b.CloseReason)
	switch {
	case ok:
	case sha8 != "":
		v = verdictAccepted
	case strings.Contains(b.Title, "Reviewer asks") || strings.Contains(b.Description, "Reviewer asks"):
		v = verdictAnswered
	}
	return v, rationale
}

// renderReply is Jinbe's close-out reply for a closed bead b (ADR 0339 (a-close-out-reply-reads-its-verdict…)'s
// layout over ADR 0200's marker): v leads, sha8 "" omits "fixed in <sha8>" (only an
// Accepted prints it), a Deferred names its carriers' titles, and a non-empty rationale
// follows a blank line, cut at 2000 characters with "…".
func renderReply(b bd.Bead, graphID, name string, v verdict, sha8, rationale string, carriers []string) string {
	q := fmt.Sprintf("**%s** (%s@%s)", b.Title, b.ID, name)
	var head string
	switch v {
	case verdictAccepted:
		head = "✅ Accepted · "
		if sha8 != "" {
			head += "fixed in " + sha8 + " "
		}
		head += "by " + q
	case verdictRejected:
		head = "❌ Rejected · by " + q
	case verdictAnswered:
		head = "💬 Answered · by " + q
	case verdictDeferred:
		head = "⏭️ Deferred · " + q
		if len(carriers) > 0 {
			bold := make([]string, len(carriers))
			for i, c := range carriers {
				bold[i] = "**" + c + "**"
			}
			head += " · tracked by " + strings.Join(bold, ", ")
		}
	default:
		head = "☑️ Closed · " + q
	}
	rationale = strings.TrimSpace(rationale)
	if r := []rune(rationale); len(r) > replyReasonMax {
		rationale = string(r[:replyReasonMax]) + "…"
	}
	out := "🤖 mugicrew — " + head + "\n"
	if rationale != "" {
		out += "\n**Rationale:** " + rationale + "\n"
	}
	return out + replyMarkerLine(b.ID, graphID, name)
}

// renderStranded is the reply on a bead whose integrate commit a human merge left
// out of the merged commit (ADR 0319 (a-filed-comment-is-closed-out…)): the stranded-commits Signal carries it.
func renderStranded(b bd.Bead, graphID, name, signalTitle string) string {
	return fmt.Sprintf("🤖 mugicrew: not shipped with this MR: carried by **%s**\n%s", signalTitle, replyMarkerLine(b.ID, graphID, name))
}
