package luffy

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// The reads the re-approval rule makes, as seams so tests stay off a real bd and Forge.
var (
	approvalsOf  = func(container string) ([]bd.Bead, error) { return bd.ChildrenOfKind(container, contract.KindApproval) }
	gatePivotsOf = func(container string) ([]bd.Bead, error) { return bd.BlockersOfKind(container, contract.KindPivot) }
)

// amendedPrefix prefixes the close reason of a pivot that amended the Spec.
const amendedPrefix = "amended:"

// reapproval is the verdict on an Approval that finds its MR already un-drafted with
// auto-approve off (Spec FR 19–24, 26): skip closes it approved with History; otherwise
// the MR is set back to Draft with History (empty when no pivot caused it) and it waits.
type reapproval struct {
	skip    bool
	history string
}

// decideReapproval applies the rule over the container's other Approvals and gate pivots.
// A Chart's rule compares the Patch-id: lines of self and the cutoff instead of reading pivots.
// The cutoff is the created_at of the newest closed approved Approval; the Spec changed
// if an amended: pivot closed at or after it. An auto-approved cutoff does not vouch for
// the un-draft: Go un-drafted that MR, no human did. The auto-approved test reads the origin
// of a carried cutoff; the Chart's Patch-id comparison reads the cutoff itself.
func decideReapproval(kind contract.Kind, self bd.Bead, approvals, pivots []bd.Bead) reapproval {
	var cutoff *bd.Bead
	for i, a := range approvals {
		if a.ID != self.ID && a.ApprovedClosed() && (cutoff == nil || a.Created.After(cutoff.Created)) {
			cutoff = &approvals[i]
		}
	}
	if cutoff == nil {
		return reapproval{}
	}
	// a carried cutoff's authorship is its origin's: Go wrote the carry, no human vouched
	origin, err := bd.ApprovalOrigin(*cutoff, func(id string) (bd.Bead, error) {
		for _, a := range approvals {
			if a.ID == id {
				return a, nil
			}
		}
		return bd.Bead{}, fmt.Errorf("approval %s not among the container's Approvals", id)
	})
	if err != nil || historyEndsAny(origin.Description) {
		return reapproval{}
	}
	if kind == contract.KindChart {
		// equal ids on both sides mean the squash diff is unchanged; a missing id never vouches
		now, ok := git.ParsePatchID(self.Description)
		then, ok2 := git.ParsePatchID(cutoff.Description)
		if ok && ok2 && now == then {
			return reapproval{skip: true, history: "approved (MR already un-drafted, Chart content unchanged)"}
		}
		return reapproval{history: "re-drafted (Chart content changed since the approved Approval)"}
	}
	var changed *bd.Bead
	for i, p := range pivots {
		if p.Open() || !strings.HasPrefix(p.CloseReason, amendedPrefix) || p.Closed.Before(cutoff.Created) {
			continue
		}
		if changed == nil || p.Closed.After(changed.Closed) {
			changed = &pivots[i]
		}
	}
	if changed != nil {
		return reapproval{history: fmt.Sprintf("re-drafted (Spec changed by pivot %s)", changed.Title)}
	}
	return reapproval{skip: true, history: "approved (MR already un-drafted, Spec unchanged)"}
}

// reapprove handles an Approval whose container's MR is already un-drafted: it closes the
// Approval approved on the skip; otherwise the MR is back to Draft and the Approval waits.
func reapprove(ctx contract.Ctx, container, a bd.Bead) error {
	approvals, err := approvalsOf(container.ID)
	if err != nil {
		return err
	}
	pivots, err := gatePivotsOf(container.ID)
	if err != nil {
		return err
	}
	v := decideReapproval(container.Kind, a, approvals, pivots)
	if v.skip {
		if _, err := appendHistory(a, v.history); err != nil {
			return err
		}
		return closeOnPublish(ctx, a.ID, bd.ApprovedReason, nil)
	}
	if v.history != "" {
		body, err := appendHistory(a, v.history)
		if err != nil {
			return err
		}
		a.Description = body
	}
	if err := ApprovalMR.Open(container, a, true); err != nil {
		return fmt.Errorf("open MR: %w", err)
	}
	return edgesOnPublish(ctx, a.ID, nil)
}

// appendHistory ends the Approval's body with line unless it already ends so: a
// re-dispatch after a crash writes it once. It returns the body now on the bead, so a later
// write from the same Approval (the MR: line) does not clobber the History line.
func appendHistory(a bd.Bead, line string) (string, error) {
	if last, _ := bd.ApprovalHistoryLast(a.Description); last == line {
		return a.Description, nil
	}
	body := bd.EndApprovalHistory(a.Description, line)
	return body, updateBody(a.ID, []string{"--description", body})
}

// carriedLaps is the Approval History line of each carried Approval newer than the newest
// non-carried approved one, oldest first: the renumbers the human never looked at.
func carriedLaps(approvals []bd.Bead, selfID string) []string {
	var since time.Time
	for _, a := range approvals {
		if a.ID != selfID && a.ApprovedClosed() && !bd.IsCarriedApproval(a.Description) && a.Created.After(since) {
			since = a.Created
		}
	}
	var carried []bd.Bead
	for _, a := range approvals {
		if a.ID != selfID && a.ApprovedClosed() && bd.IsCarriedApproval(a.Description) && a.Created.After(since) {
			carried = append(carried, a)
		}
	}
	sort.Slice(carried, func(i, j int) bool { return carried[i].Created.Before(carried[j].Created) })
	lines := make([]string, len(carried))
	for i, a := range carried {
		lines[i], _ = bd.ApprovalHistoryLast(a.Description)
	}
	return lines
}

// withCarried ends the packet prompt with the clause requiring the carried laps under the
// packet's Approval History; no laps leave the prompt as is.
func withCarried(prompt string, laps []string) string {
	if len(laps) == 0 {
		return prompt
	}
	return prompt + "\n\nThe container's head was carried across renumber-only laps since the last Approval a human or the AFK session gave. List these lines, verbatim, under the packet's Approval History, before any ending line:\n\n" + strings.Join(laps, "\n")
}

// carriedLapsFor reads the container's Approvals and returns carriedLaps for the Approval.
// A failed read drops the clause, as the fallback-line read does.
func carriedLapsFor(a bd.Bead) []string {
	if a.Parent == "" {
		return nil
	}
	approvals, err := approvalsOf(a.Parent)
	if err != nil {
		return nil
	}
	return carriedLaps(approvals, a.ID)
}
