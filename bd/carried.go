package bd

import (
	"fmt"
	"strings"
)

// Rename is one numbered file's renumber: its path at the new head and its old and
// new padded numbers (0394, 0395). The renumber-only verifier returns these.
type Rename struct{ Path, Old, New string }

// carriedHistoryPrefix opens a carried Approval's Approval History line.
const carriedHistoryPrefix = "carried (renumber-only"

// CarriedApprovalBody is the body of a carried Approval (ADR 0398 §4): it opens with the
// carry, pins the new head, the recomputed Patch-id when given (a Chart's; "" for a
// Checkpoint) and ends with an Approval History line naming each rename in path order.
// No Packet is copied: the origin holds it.
func CarriedApprovalBody(oldTitle, head, patchID, oldSHA string, renames []Rename) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Approval carried from Approval %s: renumber-only.\nCommit: %s\n", oldTitle, head)
	if patchID != "" {
		fmt.Fprintf(&b, "Patch-id: %s\n", patchID)
	}
	parts := make([]string, len(renames))
	for i, r := range renames {
		parts[i] = r.Old + " → " + r.New
	}
	fmt.Fprintf(&b, "\n### Approval History\n\ncarried (renumber-only from %s: %s)\n", oldSHA, strings.Join(parts, ", "))
	return b.String()
}

// IsCarriedApproval reports whether body's last Approval History line is a carried one.
func IsCarriedApproval(body string) bool {
	last, _ := ApprovalHistoryLast(body)
	return strings.HasPrefix(last, carriedHistoryPrefix)
}

// ApprovalOrigin walks caused-by from a carried Approval to the first Approval that was
// not carried; a non-carried Approval is its own origin. Every reader of a stamp's
// authorship (human or AFK) reads the origin, never Go's carry.
func ApprovalOrigin(a Bead, get func(id string) (Bead, error)) (Bead, error) {
	seen := map[string]bool{}
	for IsCarriedApproval(a.Description) {
		if a.CausedBy == "" || seen[a.ID] {
			return Bead{}, fmt.Errorf("carried Approval %s has no origin up its caused-by chain", a.Title)
		}
		seen[a.ID] = true
		next, err := get(a.CausedBy)
		if err != nil {
			return Bead{}, fmt.Errorf("origin of carried Approval %s: %w", a.Title, err)
		}
		a = next
	}
	return a, nil
}

// NotCarriedNote is the one-line note a fallback Review or Approval carries when the
// renumber-only check rejects (ADR 0398 §5).
func NotCarriedNote(point, file string) string {
	return fmt.Sprintf("not carried: %s (%s)", point, file)
}

// NewestApproved is the Approval in approvals closed approved last (carried or not,
// human or AFK): the one a renumber-only head carries from (ADR 0398 §3). False when none.
func NewestApproved(approvals []Bead) (Bead, bool) {
	var best Bead
	found := false
	for _, a := range approvals {
		if !a.ApprovedClosed() {
			continue
		}
		if !found || a.Closed.After(best.Closed) || (a.Closed.Equal(best.Closed) && a.Created.After(best.Created)) {
			best, found = a, true
		}
	}
	return best, found
}
