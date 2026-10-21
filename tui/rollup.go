package tui

import (
	"fmt"
	"slices"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// rollupResult is a Checkpoint's Rollup (ADR 0198): the waterfall's word and its
// rank (1–13, the section sort key). Qualifiers are round N, re-review, stale pin, a gitlab Landing's !<iid> and MR sub-state (ADR 0226), and retrying;
// Why names the driver, the re-review cause and a stale pin's shas. Also lists the
// words of the other rules that matched, in rank order.
type rollupResult struct {
	Word       string
	Rank       int
	Qualifiers []string
	Why        string
	Also       []string
}

// rollup is cp's Rollup, the I/O-free waterfall over its children, its open blockers
// and the last refresh's checkpoint/* tips. Every rule is evaluated; the first match
// drives and the rest are Also.
func (g graphData) rollup(cp *bd.Bead) rollupResult {
	var kids []bd.Bead
	for _, i := range g.children(cp.ID) {
		kids = append(kids, g.beads[i])
	}
	tip := g.cpTips[cp.ID]
	atTip := func(a bd.Bead) bool { // an unknown tip is never "at the tip"
		pin, ok := git.ParseCommit(a.Description)
		return ok && tip != "" && pin == tip
	}
	var landings, approvals []bd.Bead
	members := 0
	for _, k := range kids {
		switch k.Kind {
		case contract.KindLanding:
			landings = append(landings, k)
		case contract.KindApproval:
			approvals = append(approvals, k)
		}
		if bd.IsMember(k.Kind) {
			members++
		}
	}
	pause := ""
	var landedL, pauseL, openL, spent *bd.Bead // each rule's driver bead
	for i, l := range landings {
		if l.Status == "closed" {
			if strings.HasPrefix(l.CloseReason, "landed: ") && landedL == nil {
				landedL = &landings[i]
			}
			continue
		}
		if openL == nil {
			openL = &landings[i]
		}
		if c := bd.PausedCauses(l); pause == "" && len(c) > 0 {
			pauseL = &landings[i]
			pause = "land paused"
			if l.HasLabel("paused:" + bd.PausedFailing) {
				pause = "land failed"
			}
		}
	}
	blockers := g.openBlockers(cp)
	var approvedL, openAp *bd.Bead
	for i, a := range approvals {
		if a.Unclosed() {
			openAp = &approvals[i]
		} else if approvedL == nil && a.ApprovedClosed() && atTip(a) {
			approvedL = &approvals[i]
		}
	}
	spawned := false // a Landing made from this Approval: created at/after it closed
	for _, l := range landings {
		spawned = spawned || approvedL != nil && !l.Created.Before(approvedL.Closed)
	}
	blocked := ""
	if len(blockers) > 0 {
		blocked = "blocked on " + kindWord(blockers[0].Kind)
		if len(blockers) > 1 {
			blocked += fmt.Sprintf(" +%d", len(blockers)-1)
		}
	}
	firstOpen := func(kinds ...contract.Kind) *bd.Bead {
		for i, k := range kids {
			if k.Unclosed() && slices.Contains(kinds, k.Kind) {
				return &kids[i]
			}
		}
		return nil
	}
	var blockerL *bd.Bead
	if len(blockers) > 0 {
		blockerL = blockers[0]
	}
	conflictL, fixL := firstOpen(contract.KindConflict), firstOpen(contract.KindCritique, contract.KindSurgery)
	reviewL, buildL := firstOpen(contract.KindReview), firstOpen(contract.KindBuild)
	desc := func(b *bd.Bead, extra string) string { // "<id> <kind> <state>" plus what drove it
		if b == nil {
			return ""
		}
		return fmt.Sprintf("%s %s %s%s", b.ID, b.Kind, b.Status, extra)
	}
	rules := []struct{ word, why string }{ // rank 1–11; "" is no match
		{boolWord(landedL != nil, "landed"), "Landing " + desc(landedL, "") + ": " + closeOf(landedL)},
		{pause, desc(pauseL, " with "+pausedLabel(pauseL))},
		{boolWord(openL != nil, "landing"), desc(openL, "")},
		{boolWord(conflictL != nil, "conflict"), desc(conflictL, "")},
		{blocked, desc(blockerL, " blocking the Checkpoint")},
		{boolWord(fixL != nil, "fixing"), desc(fixL, "")},
		{boolWord(openAp != nil, "awaiting approval"), desc(openAp, "")},
		{boolWord(reviewL != nil, "in review"), desc(reviewL, "")},
		{boolWord(buildL != nil, "in build"), desc(buildL, "")},
		{boolWord(approvedL != nil && !spawned, "approved"), "Approval " + desc(approvedL, "") + " at the tip"},
		{boolWord(g.reviewDueOver(cp, kids), "review due"), "members drained, no Pivot open, no Approval at the tip"},
	}
	var r rollupResult
	for i, rule := range rules {
		switch {
		case rule.word == "":
		case r.Word == "":
			r.Word, r.Rank, r.Why = rule.word, i+1, rule.why
		default:
			r.Also = append(r.Also, rule.word)
		}
	}
	switch { // ranks 12–13 are the fallbacks: they only ever drive
	case r.Word != "":
	case members == 0:
		r.Word, r.Rank, r.Why = "cut", 12, "no members yet"
	default:
		r.Word, r.Rank, r.Why = "idle ⚠", 13, "members exist but no rule matched"
	}
	// qualifiers, in order: round N, re-review, stale pin, !<iid>, MR sub-state, forge review, retrying
	rounds := 0
	for _, k := range kids {
		if k.Kind == contract.KindReview {
			rounds++
		}
	}
	if rounds >= 2 && r.Word != "landed" {
		r.Qualifiers = append(r.Qualifiers, fmt.Sprintf("round %d", rounds))
	}
	for i, l := range landings { // the earliest spent Landing or Approval gives the cause
		if l.Status == "closed" && strings.HasPrefix(l.CloseReason, "obsolete:") {
			spent = earlier(spent, &landings[i])
		}
	}
	for i, a := range approvals {
		if a.Status == "closed" && !a.ApprovedClosed() {
			spent = earlier(spent, &approvals[i])
		}
	}
	if spent != nil {
		r.Qualifiers = append(r.Qualifiers, "re-review")
		r.Why += fmt.Sprintf(" · after %s %s", kindWord(spent.Kind), spent.CloseReason)
	}
	if r.Word == "awaiting approval" && openAp != nil {
		if pin, ok := git.ParseCommit(openAp.Description); ok && tip != "" && pin != tip {
			r.Qualifiers = append(r.Qualifiers, "stale pin")
			r.Why += fmt.Sprintf(" · pin %s ≠ tip %s", sha8(pin), sha8(tip))
		}
	}
	if mrL := map[string]*bd.Bead{"landing": openL, "land failed": pauseL, "land paused": pauseL}[r.Word]; mrL != nil {
		if path, iid := git.RecordedMRPath(mrL.Description); iid != 0 {
			raw, _ := git.ParseLine(mrL.Description, "MR status:")
			ctxName := "" // only the nested body form names a context; a bare iid is the Workspace's, whatever contexts it declares
			if path != "" {
				ctxName = bd.ContextOf(*cp)
			}
			r.Qualifiers = append(r.Qualifiers, git.MRRef(ctxName, iid))
			if w := mrStatusWord[raw]; w != "" && r.Word == "landing" { // a paused Landing's status may be stale
				r.Qualifiers = append(r.Qualifiers, w)
			}
			r.Why += " · MR " + git.MRRef(path, iid)
			if raw != "" {
				r.Why += " detailed_merge_status=" + raw
			}
		}
	}
	r.Qualifiers = append(r.Qualifiers, forgeReviewQualifier(kids)...)
	if r.Word == "landing" { // the snapshot wins: openL is from the beads, so an event for a closed Landing is never read
		if landRetrying(g.landEv[openL.ID]) {
			r.Qualifiers = append(r.Qualifiers, "retrying")
		}
	}
	return r
}

// forgeReviewQualifier is the forge review qualifier (ADR 0320 (a-forge-review-surfaces…)) over a container's
// children: none without an open Forge Review, "forge review paused" when one is paused
// (failing or manual, which holds the container on its own row), else "forge review". It is
// a qualifier, never a Rollup word: a pending Landing outranks it.
func forgeReviewQualifier(kids []bd.Bead) []string {
	var q []string
	for _, b := range kids {
		if b.Kind != contract.KindForgeReview || b.Status == "closed" || b.Status == "deferred" {
			continue
		}
		if bd.PausedBy(b, bd.PausedFailing) || bd.PausedBy(b, bd.PausedManual) {
			return []string{"forge review paused"}
		}
		q = []string{"forge review"}
	}
	return q
}

// mrStatusWord maps GitLab's raw detailed_merge_status (the Landing's MR status: line) to its
// qualifier word; an unlisted status has none.
var mrStatusWord = map[string]string{
	"mergeable":                "awaiting merge",
	"ci_still_running":         "pipeline running",
	"need_rebase":              "needs rebase",
	"not_approved":             "needs approval",
	"discussions_not_resolved": "threads open",
	"draft_status":             "draft",
}

// landRetrying is whether ev, an open Landing's latest event, is a failure or infra retry:
// the Checkpoint Rollup's and the Chart status word's retrying qualifier share it.
func landRetrying(ev string) bool {
	switch ev {
	case "land.failed", "gate.infra", "gate.pending", "effort.failed":
		return true
	}
	return false
}

// label is the word and its qualifiers, as the section row and the detail's rollup: field show it.
func (r rollupResult) label() string {
	return strings.Join(append([]string{r.Word}, r.Qualifiers...), " · ")
}

func sha8(s string) string { return s[:min(8, len(s))] }

func earlier(a, b *bd.Bead) *bd.Bead {
	if a == nil || b.Closed.Before(a.Closed) {
		return b
	}
	return a
}

func closeOf(b *bd.Bead) string {
	if b == nil {
		return ""
	}
	return b.CloseReason
}

func pausedLabel(b *bd.Bead) string {
	if b != nil && b.HasLabel("paused:"+bd.PausedFailing) {
		return "paused:" + bd.PausedFailing
	}
	return "a paused:* label"
}

func boolWord(ok bool, w string) string {
	if ok {
		return w
	}
	return ""
}

// kindWord title-cases a kind for the Rollup prose; a bead with no kind:* label reads "Bead".
func kindWord(k contract.Kind) string {
	if k == "" {
		return "Bead"
	}
	return strings.ToUpper(string(k[:1])) + string(k[1:])
}
