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

// replyReasonMax is how much of a close reason a close-out reply carries (ADR 0319 (a-filed-comment-is-closed-out…)).
const replyReasonMax = 2000

// renderReply is Jinbe's close-out reply for a closed bead b (ADR 0200's template, with
// ADR 0319 (a-filed-comment-is-closed-out…)'s reason); sha8 "" omits " in <sha8>". The reason is the whole close
// reason, cut at 2000 characters with "…".
func renderReply(b bd.Bead, graphID, name, sha8 string) string {
	reason := strings.TrimSpace(b.CloseReason)
	if r := []rune(reason); len(r) > replyReasonMax {
		reason = string(r[:replyReasonMax]) + "…"
	}
	in := ""
	if sha8 != "" {
		in = " in " + sha8
	}
	return fmt.Sprintf("🤖 mugicrew: addressed by **%s** (%s@%s)%s: %s\n%s", b.Title, b.ID, name, in, reason, replyMarkerLine(b.ID, graphID, name))
}

// renderStranded is the reply on a bead whose integrate commit a human merge left
// out of the merged commit (ADR 0319 (a-filed-comment-is-closed-out…)): the stranded-commits Signal carries it.
func renderStranded(b bd.Bead, graphID, name, signalTitle string) string {
	return fmt.Sprintf("🤖 mugicrew: not shipped with this MR: carried by **%s**\n%s", signalTitle, replyMarkerLine(b.ID, graphID, name))
}
