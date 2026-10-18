package tui

import (
	"fmt"
	"slices"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// chartWord is c's Chart status word (ADR 0204 §1): the I/O-free 17-rank waterfall over
// its children, shaped like rollup. Every rule is evaluated; the first match drives and the
// rest are Also. Ranks 1–17 and the qualifiers are here.
func (g graphData) chartWord(c *bd.Bead) rollupResult {
	var landedL, pauseL, openL, conflictL *bd.Bead // each rule's driver bead
	var surveyL, hitlL, afkL, draftL *bd.Bead
	landings, obsolete := 0, false
	pause, unclosed := "", 0
	var fogBlockers []*bd.Bead // the distinct outside blockers, while every defog child waits on one
	fogBlocked := true
	for i, b := range g.beads {
		if b.Parent != c.ID || b.Event {
			continue
		}
		if b.Status != "closed" && b.Kind != contract.KindLanding {
			unclosed++ // Drafts count: bd.DrainBlockersOver's sense
		}
		if b.Status == "deferred" {
			if draftL == nil {
				draftL = &g.beads[i]
			}
			continue
		}
		if b.Status != "closed" && b.Kind != contract.KindLanding && b.Kind != contract.KindChartConflict {
			bl := g.openBlockers(&g.beads[i])
			switch inSession := g.sessions[b.ID].State == session.Alive; {
			case b.Kind == contract.KindSurvey:
				if surveyL == nil || inSession {
					surveyL = &g.beads[i]
				}
				continue
			case isHITL(b.Kind) && !b.Container && (inSession || len(bl) == 0):
				// "your turn" outranks a working session as the driver
				if hitlL == nil || g.sessions[hitlL.ID].State == session.Alive && humanWait(g.sessions[hitlL.ID], false) == "" {
					hitlL = &g.beads[i]
				}
			case !isHITL(b.Kind) && (g.liveOf(&g.beads[i]) == alive || len(bl) == 0):
				if afkL == nil || g.liveOf(&g.beads[i]) == alive {
					afkL = &g.beads[i]
				}
			}
			fogBlocked = fogBlocked && len(bl) > 0
			for _, t := range bl {
				if t.Parent == c.ID {
					fogBlocked = false
				} else if !slices.Contains(fogBlockers, t) {
					fogBlockers = append(fogBlockers, t)
				}
			}
		}
		switch b.Kind {
		case contract.KindLanding:
			landings++
			obsolete = obsolete || b.Status == "closed" && strings.HasPrefix(b.CloseReason, "obsolete:")
			if b.Status == "closed" {
				if strings.HasPrefix(b.CloseReason, "landed: ") && landedL == nil {
					landedL = &g.beads[i]
				}
				continue
			}
			if openL == nil {
				openL = &g.beads[i]
			}
			if pauseL == nil && bd.Paused(b) {
				pauseL, pause = &g.beads[i], "land paused"
				if b.HasLabel("paused:" + bd.PausedFailing) {
					pause = "land failed"
				}
			}
		case contract.KindChartConflict:
			if b.Status != "closed" && conflictL == nil {
				conflictL = &g.beads[i]
			}
		}
	}
	blockers := g.openBlockers(c)
	blocked, blockerL := "", (*bd.Bead)(nil)
	if len(blockers) > 0 {
		blockerL = blockers[0]
		blocked = "blocked on " + kindWord(blockerL.Kind)
		if len(blockers) > 1 {
			blocked += fmt.Sprintf(" +%d", len(blockers)-1)
		}
	}
	charting := boolWord(surveyL != nil, "survey due")
	if surveyL != nil && g.sessions[surveyL.ID].State == session.Alive {
		charting = "charting"
	}
	fog := ""
	if fogBlocked && len(fogBlockers) > 0 {
		fog = "fog blocked on " + kindWord(fogBlockers[0].Kind)
		if len(fogBlockers) > 1 {
			fog += fmt.Sprintf(" +%d", len(fogBlockers)-1)
		}
	}
	turn, turnWhy := "", ""
	if hitlL != nil {
		turn, turnWhy = "your turn", "ready to launch"
		if s := g.sessions[hitlL.ID]; s.State == session.Alive {
			turn, turnWhy = "in defog session", "session working"
			if humanWait(s, false) != "" {
				turn, turnWhy = "your turn", "session waiting on the human"
			}
		}
	}
	afk := ""
	if afkL != nil {
		afk = boolWord(g.liveOf(afkL) == alive, "defogging")
		if afk == "" {
			afk = "defog queued"
		}
	}
	held := g.heldChildren(c.ID)
	var heldL, fogL *bd.Bead
	if len(held) > 0 {
		heldL = &held[0]
	}
	if len(fogBlockers) > 0 {
		fogL = fogBlockers[0]
	}
	drained := unclosed == 0
	stamped := c.HasLabel(contract.LabelDefogged)
	judge := ""
	if drained && !stamped {
		judge = "judge due"
		if g.liveOf(c) == alive {
			judge = "judging"
		}
	}
	own := ""
	switch {
	case c.HasLabel("paused:" + bd.PausedFailing):
		own = "failing"
	case bd.Paused(*c):
		own = "paused"
	}
	desc := func(b *bd.Bead, extra string) string { // "<id> <kind> <state>" plus what drove it
		if b == nil {
			return ""
		}
		return fmt.Sprintf("%s %s %s%s", b.ID, b.Kind, b.Status, extra)
	}
	rules := []struct{ word, why string }{ // rank 1–16; "" is no match
		{boolWord(landedL != nil && g.liveOf(c) == alive, "speccing"), "Landing " + desc(landedL, "") + ": " + closeOf(landedL) + ", Spec session holds the Chart"},
		{boolWord(landedL != nil, "landed"), "Landing " + desc(landedL, "") + ": " + closeOf(landedL)},
		{pause, desc(pauseL, " with "+pausedLabel(pauseL))},
		{boolWord(openL != nil, "landing"), desc(openL, "")},
		{boolWord(conflictL != nil, "conflict"), desc(conflictL, "")},
		{blocked, desc(blockerL, " blocking the Chart")},
		{own, "the Chart itself carries " + strings.Join(bd.PausedCauses(*c), ", ")},
		{charting, desc(surveyL, "")},
		{turn, desc(hitlL, ", "+turnWhy)},
		{afk, desc(afkL, "")},
		{fog, "every defog child waits on " + desc(fogL, "")},
		{boolWord(draftL != nil, "drafts pending"), desc(draftL, "")},
		{boolWord(heldL != nil, "integrate due"), desc(heldL, ", its effort ref is not integrated")},
		{boolWord(judge == "judging", "judging"), "drained, unstamped, promote-or-stamp session on the Chart"},
		{boolWord(judge == "judge due", "judge due"), "drained, no chart:defogged"},
		{boolWord(drained && stamped && openL == nil && landedL == nil, "landing due"), "stamped and drained with no Landing yet (an off-Landing land reads this too)"},
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
	if r.Word == "" {
		r.Word, r.Rank, r.Why = "idle ⚠", 17, "no rule matched"
	}
	// qualifiers, in order: round N, re-land, retrying
	if landings >= 2 {
		r.Qualifiers = append(r.Qualifiers, fmt.Sprintf("round %d", landings))
	}
	if obsolete {
		r.Qualifiers = append(r.Qualifiers, "re-land")
	}
	if r.Word == "landing" && landRetrying(g.landEv[openL.ID]) {
		r.Qualifiers = append(r.Qualifiers, "retrying")
	}
	return r
}
