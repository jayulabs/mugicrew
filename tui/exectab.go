package tui

// The Execute tab (Spec mc-lu2x.21, ADR 0170): an Actionable queue over every open
// Checkpoint and Spec grouped by Initiative, plus the Tasks and Pivots that wait on,
// hold or gate a Checkpoint. Modelled on Defog: a thin client over the shell's
// snapshot (graphData), routing enter and b through the shared queue-tab gates.

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// Row kinds.
const (
	eSec    = iota // 'Actionable' or an Initiative / No Initiative header
	ePlace         // the empty-Actionable placeholder
	eAct           // an Actionable HITL bead
	eCP            // a Checkpoint
	eSpec          // a Spec
	eMem           // an open member of a Checkpoint
	eSide          // a Task or Pivot waiting on a Checkpoint, not a member
	eSum           // '▸ N closed'
	eClosed        // a closed member inside an expanded '▸ N closed'
	eLive          // a Live effort in the Live section
	eCPS           // an open Checkpoint in the Checkpoints section: status only
	eLanded        // a closed Checkpoint in 'Landed in last 24h': status only
	eSPS           // an open Spec in the Specs section: status only
)

const (
	actKey   = "s:act"
	liveKey  = "s:live"  // the Live section: unfolded by default like actKey, never pruned
	specsKey = "s:specs" // the Specs section between Live and Checkpoints: likewise
	cpsKey   = "s:cps"   // the Checkpoints section between Live and Initiatives: likewise
	treeKey  = "s:tree"  // the Initiatives section above the tree: likewise

	livePrefix   = "live:"           // a Live row's key is livePrefix + the bead id
	liveNone     = liveKey + "/none" // the empty Live section's placeholder
	liveEmpty    = "nothing is running"
	specsPrefix  = "specs:"           // a Specs row's key is specsPrefix + the bead id
	specsNone    = specsKey + "/none" // the empty Specs section's placeholder
	specsEmpty   = "no open Specs"
	specsDetail  = "Every open published Spec. Status only."
	cpsPrefix    = "cps:"           // a Checkpoints row's key is cpsPrefix + the bead id
	cpsNone      = cpsKey + "/none" // the empty Checkpoints section's placeholder
	cpsEmpty     = "no open Checkpoints"
	landedKey    = cpsKey + "/landed" // the nested 'Landed in last 24h' header: folded by default
	landedPrefix = "landed:"          // a Landed row's key is landedPrefix + the Checkpoint's id
	landedWindow = 24 * time.Hour
	landedRefuse = "landed — nothing to launch"
	cpsDetail    = "Every open Checkpoint with its Rollup: where it is in its land cycle. Status only."
	liveDetail   = "The efforts the crew is running: live AFK efforts and alive sessions."
)

// otherSection is sel's key in the other of Actionable and Live ("act:<id>" <-> "live:<id>"),
// or "" when sel is neither.
func otherSection(sel string) string {
	if id, ok := strings.CutPrefix(sel, "act:"); ok {
		return livePrefix + id
	}
	if id, ok := strings.CutPrefix(sel, livePrefix); ok {
		return "act:" + id
	}
	return ""
}

// launchMover is a tab with Actionable and Live sections: a launch from one of its
// Actionable rows moves its cursor off the launched bead's row.
type launchMover interface{ afterLaunch(id string) component }

// afterLaunch is sel once bead id leaves Actionable by a launch: the row that takes its
// place in keys (the Actionable rows' keys before the launch), else the previous one,
// else the placeholder. Any other selection is left alone.
func afterLaunch(sel, id string, keys []string) string {
	i := slices.Index(keys, "act:"+id)
	if sel != "act:"+id || i < 0 {
		return sel
	}
	switch {
	case i+1 < len(keys):
		return keys[i+1]
	case i > 0:
		return keys[i-1]
	}
	return actKey + "/none"
}

func (t execTab) afterLaunch(id string) component {
	t.sel = afterLaunch(t.sel, id, rowKeys(t.g.actionable(), func(r eRow) string { return r.key }))
	return t
}

func rowKeys[R any](rows []R, key func(R) string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = key(r)
	}
	return out
}

// liveHeaderText is the Live section header's detail.
func liveHeaderText(title string) string {
	return title + "\n\n" + liveDetail + "\n\nenter does nothing here (space folds)."
}

// eRow is one list line. key is the fold and selection key: "s:act", "i:<initID>",
// "act:<id>", "live:<id>", "side:<id>", the bead id, or "<cpID>/closed".
type eRow struct {
	kind    int
	key     string
	title   string   // a header's title
	init    *bd.Bead // a header's Initiative; nil for No Initiative
	b, cp   *bd.Bead // the bead; the Checkpoint it sits under or waits on
	rel     string   // eAct/eSide/eLive: member, gates, before land, after land
	members []*bd.Bead
	depth   int
}

// id is the row's bead id, "" on a row with no bead.
func (r eRow) id() string {
	if r.b == nil {
		return ""
	}
	return r.b.ID
}

// shadeIn reduces the row for the ancestor shade: a bead row stands for its bead, an
// Initiative header for its Initiative. A Task or Pivot's cp is only a hint, not an ancestor.
func (r eRow) shadeIn() shadeIn {
	switch r.kind {
	case eSec:
		s := shadeIn{grp: r.key, head: !isSectionKey(r.key)}
		if r.init != nil {
			s.id = r.init.ID
		}
		return s
	case eAct:
		return shadeIn{id: r.b.ID, act: true}
	case eLive:
		return shadeIn{id: r.b.ID, act: true, live: true}
	case eCP, eCPS, eSPS, eLanded, eSpec, eMem, eSide, eClosed:
		return shadeIn{id: r.b.ID}
	}
	return shadeIn{}
}

// cpAct is whether the row is an own-paused Checkpoint in Actionable's paused band.
func (r eRow) cpAct() bool { return r.kind == eAct && r.b.Container }

// leaf is whether enter routes the row as an open HITL leaf (blocked and paused refuse).
func (r eRow) leaf() bool {
	return r.b != nil && r.kind != eClosed && isHITL(r.b.Kind) && !r.b.Container
}

// selectable is whether the cursor may rest on the row: not on a placeholder.
func (r eRow) selectable() bool { return r.kind != ePlace }

// isSection is whether the row is a top-level section header.
func (r eRow) isSection() bool { return r.kind == eSec && (isSectionKey(r.key) || r.key == noInitKey) }

// noInitKey is the No Initiative section's fold key, as in Ideate.
const noInitKey = "i:"

// isSectionKey is whether key is the Actionable, Live, Checkpoints or Initiatives section's header.
func isSectionKey(key string) bool {
	return key == actKey || key == liveKey || key == specsKey || key == cpsKey || key == treeKey
}

// liveTitle is the Live header text "Live (N)", all bold in the live green.
func liveTitle(title string) string {
	name, n, _ := strings.Cut(title, " ")
	return aliveSt.Bold(true).Render(name + " " + n)
}

// execLive is a status the Execute tab lists; Drafts (deferred) and closed beads are out.
func execLive(b bd.Bead) bool {
	return !b.Event && (b.Status == "open" || b.Status == "in_progress" || b.Status == "blocked")
}

// execListed splits the listed beads: every live Checkpoint and Spec.
func execListed(beads []bd.Bead) (cps, specs []*bd.Bead) {
	for i, b := range beads {
		if !execLive(b) {
			continue
		}
		switch b.Kind {
		case contract.KindCheckpoint:
			cps = append(cps, &beads[i])
		case contract.KindSpec:
			specs = append(specs, &beads[i])
		}
	}
	return
}

// waiters are cp's non-member related beads with their relation: gating Pivots and
// before-land Tasks (in cp.BlockedBy), and after-land Tasks (blocked by cp).
func (g graphData) waiters(cp *bd.Bead) (out []*bd.Bead, rel []string) {
	pos := func(yield func(int) bool) { // every bead position, or only the candidates through the index
		if g.idx == nil {
			for i := range g.beads {
				if !yield(i) {
					return
				}
			}
			return
		}
		var cand []int
		for _, id := range cp.BlockedBy {
			if i, ok := g.idx.byID[id]; ok {
				cand = append(append(cand, i), g.idx.dups[id]...)
			}
		}
		cand = append(cand, g.idx.blockers[cp.ID]...)
		slices.Sort(cand)
		for _, i := range slices.Compact(cand) {
			if !yield(i) {
				return
			}
		}
	}
	for i := range pos {
		b := g.beads[i]
		if !execLive(b) {
			continue
		}
		switch {
		case b.Kind == contract.KindPivot && slices.Contains(cp.BlockedBy, b.ID):
			out, rel = append(out, &g.beads[i]), append(rel, "gates")
		case b.Kind == contract.KindTask && slices.Contains(cp.BlockedBy, b.ID):
			out, rel = append(out, &g.beads[i]), append(rel, "before land")
		case b.Kind == contract.KindTask && slices.Contains(b.BlockedBy, cp.ID):
			out, rel = append(out, &g.beads[i]), append(rel, "after land")
		}
	}
	return
}

// cpKids splits a Checkpoint's own children into open and closed ones, each in
// beadCmp order. Drafts and events are out.
func (g graphData) cpKids(cp *bd.Bead) (open, closed []*bd.Bead) {
	for _, i := range g.children(cp.ID) {
		b := g.beads[i]
		if b.Event || b.Status == "deferred" {
			continue
		}
		if b.Status == "closed" {
			closed = append(closed, &g.beads[i])
		} else {
			open = append(open, &g.beads[i])
		}
	}
	byCmp := func(a, b *bd.Bead) int { return beadCmp(*a, *b) }
	slices.SortStableFunc(open, byCmp)
	slices.SortStableFunc(closed, byCmp)
	return
}

// actionable is the human's queue: each listed Checkpoint's open Approval and Surgery
// members plus its waiters and Tasks, deduped by id and banded (waiting, ready, then
// own-paused, where every other own-paused ready member joins too).
func (g graphData) actionable() []eRow {
	wait, front, paused := g.actionBands()
	return slices.Concat(wait, front, paused)
}

// actionBands is actionable's three bands: waiting on the human, ready, then own-paused.
func (g graphData) actionBands() (wait, front, paused []eRow) {
	var rows, memberPaused []eRow
	cps, _ := execListed(g.beads)
	seen := map[string]bool{}
	for _, cp := range cps {
		open, _ := g.cpKids(cp)
		var cand []*bd.Bead
		var rel []string
		for _, m := range open {
			if (m.Kind == contract.KindApproval || m.Kind == contract.KindSurgery) && !m.Container {
				cand, rel = append(cand, m), append(rel, "member")
			} else if !m.Container && g.ownPausedReady(m) { // paused band only (Spec mc-llx8 FR 1-2, 9)
				memberPaused = append(memberPaused, eRow{kind: eAct, key: "act:" + m.ID, b: m, cp: cp, rel: "member", depth: 1})
			}
		}
		w, wr := g.waiters(cp)
		cand, rel = append(cand, w...), append(rel, wr...)
		for i, b := range cand {
			if seen[b.ID] {
				continue
			}
			seen[b.ID] = true
			rows = append(rows, eRow{kind: eAct, key: "act:" + b.ID, b: b, cp: cp, rel: rel[i], depth: 1})
		}
	}
	rows = append(rows, g.taskRows(cps, seen)...)
	wait, front, paused = band(g, rows, func(r eRow) *bd.Bead { return r.b })
	for _, r := range memberPaused { // after the Task pass, so a waiter or Task member stays one row
		if !seen[r.b.ID] {
			seen[r.b.ID] = true
			paused = append(paused, r)
		}
	}
	for _, cp := range cps { // an own-paused unblocked Checkpoint, a row like its Checkpoints-section row
		if g.ownPausedReady(cp) {
			paused = append(paused, eRow{kind: eAct, key: "act:" + cp.ID, b: cp, cp: cp, depth: 1})
		}
	}
	slices.SortStableFunc(paused, func(a, b eRow) int { return beadCmp(*a.b, *b.b) })
	return
}

// taskAt is the hint relation of Task b: "before land" with the first listed Checkpoint
// holding it in BlockedBy, else none (nil, "").
func taskAt(b *bd.Bead, cps []*bd.Bead) (*bd.Bead, string) {
	for _, cp := range cps {
		if slices.Contains(cp.BlockedBy, b.ID) {
			return cp, "before land"
		}
	}
	return nil, ""
}

// taskRows is an Actionable row for every live Task not yet seen, wherever it sits; banding
// drops the blocked, Live and exited ones.
func (g graphData) taskRows(cps []*bd.Bead, seen map[string]bool) (rows []eRow) {
	for i, b := range g.beads {
		if b.Kind != contract.KindTask || !execLive(b) || seen[b.ID] {
			continue
		}
		seen[b.ID] = true
		cp, rel := taskAt(&g.beads[i], cps)
		rows = append(rows, eRow{kind: eAct, key: "act:" + b.ID, b: &g.beads[i], cp: cp, rel: rel, depth: 1})
	}
	return
}

// liveRows is the Live section: every Live effort among the tab's tree beads (its
// Checkpoints, Specs, open members and waiters), in beadCmp order, each with the
// Checkpoint it sits under or waits on for its context hint. actRows are the build's
// Actionable rows, computed once by execList.
func (g graphData) liveRows(actRows []eRow) []eRow {
	cps, specs := execListed(g.beads)
	type at struct {
		cp  *bd.Bead
		rel string
	}
	where, cands := map[string]at{}, []*bd.Bead{}
	add := func(b, cp *bd.Bead, rel string) {
		if _, ok := where[b.ID]; !ok {
			where[b.ID] = at{cp, rel}
			cands = append(cands, b)
		}
	}
	for i, b := range g.beads { // every live Task, hinted only by a listed Checkpoint it holds
		if b.Kind == contract.KindTask && execLive(b) {
			cp, rel := taskAt(&g.beads[i], cps)
			add(&g.beads[i], cp, rel)
		}
	}
	for _, cp := range cps {
		add(cp, nil, "")
		open, _ := g.cpKids(cp)
		for _, m := range open {
			add(m, cp, "member")
		}
		w, rel := g.waiters(cp)
		for i, x := range w {
			add(x, cp, rel[i])
		}
	}
	for _, sp := range specs {
		add(sp, nil, "")
	}
	var rows []eRow
	act := idSet(actRows, func(r eRow) *bd.Bead { return r.b })
	for _, b := range g.liveBeads(cands, act) {
		rows = append(rows, eRow{kind: eLive, key: livePrefix + b.ID, b: b, cp: where[b.ID].cp, rel: where[b.ID].rel, depth: 1})
	}
	return rows
}

// specsRows is the Specs section's rows: every listed Spec, by beadCmp.
func (g graphData) specsRows() []eRow {
	_, specs := execListed(g.beads)
	slices.SortStableFunc(specs, func(a, b *bd.Bead) int { return beadCmp(*a, *b) })
	rows := make([]eRow, len(specs))
	for i, sp := range specs {
		rows[i] = eRow{kind: eSPS, key: specsPrefix + sp.ID, b: sp, depth: 1}
	}
	return rows
}

// cpsRows is the Checkpoints section's rows: every listed Checkpoint, by Rollup rank then beadCmp.
func (g graphData) cpsRows() []eRow {
	cps, _ := execListed(g.beads)
	rank := map[string]int{}
	for _, cp := range cps {
		rank[cp.ID] = g.rollup(cp).Rank
	}
	slices.SortStableFunc(cps, func(a, b *bd.Bead) int {
		if c := rank[a.ID] - rank[b.ID]; c != 0 {
			return c
		}
		return beadCmp(*a, *b)
	})
	rows := make([]eRow, len(cps))
	for i, cp := range cps {
		rows[i] = eRow{kind: eCPS, key: cpsPrefix + cp.ID, b: cp, cp: cp, depth: 1}
	}
	return rows
}

// landingOf is the index of the Landing that landed Checkpoint cp: a child of it, looked up
// across the whole graph, closed landed: <sha>. -1 when cp is not a closed Checkpoint with one,
// so a Checkpoint closed nothing to land: or swept obsolete: never has it. It is the one
// "Checkpoint landed" test, windowless; landedRows adds its window.
func (g graphData) landingOf(cp *bd.Bead) int {
	if cp.Kind != contract.KindCheckpoint || cp.Status != "closed" {
		return -1
	}
	for _, j := range g.children(cp.ID) {
		l := g.beads[j]
		if l.Kind == contract.KindLanding && l.Status == "closed" && strings.HasPrefix(l.CloseReason, "landed: ") {
			return j
		}
	}
	return -1
}

// landedRows is the closed Checkpoints whose own Landing closed landed: <sha> within 24h, newest
// first. members[0] is that Landing. Pivot-swept (obsolete:) Checkpoints and Chart Landings never match.
func (g graphData) landedRows() []eRow {
	var rows []eRow
	now := clock()
	for i, cp := range g.beads {
		if cp.Event {
			continue
		}
		if j := g.landingOf(&g.beads[i]); j >= 0 && now.Sub(g.beads[j].Closed) <= landedWindow {
			rows = append(rows, eRow{kind: eLanded, key: landedPrefix + cp.ID, b: &g.beads[i], cp: &g.beads[i], members: []*bd.Bead{&g.beads[j]}, depth: 2})
		}
	}
	slices.SortStableFunc(rows, func(a, b eRow) int { return b.members[0].Closed.Compare(a.members[0].Closed) })
	return rows
}

// execBadge is the Execute ●N: the waiting band's length. Folds don't matter.
func (g graphData) execBadge() int {
	wait, _, _ := g.actionBands()
	return len(wait)
}

// ecollapsed: every container below Actionable starts folded (Initiative heads,
// Checkpoints, '▸ N closed'); the Actionable section is open.
func ecollapsed(flip map[string]bool, r eRow) bool {
	return flip[r.key] != (r.kind == eSum || r.kind == eCP || r.kind == eSec && !isSectionKey(r.key))
}

// initiativeOf is the id of the Initiative b belongs to: its own (Parent is an
// Initiative), else the one of cp, the Checkpoint a Task or Pivot waits on or gates
// (nil for none); "" for no Initiative.
func (g graphData) initiativeOf(b, cp *bd.Bead) string {
	own := func(b *bd.Bead) string {
		if i := g.beadIndex(b.Parent); i >= 0 && g.beads[i].Kind == contract.KindInitiative {
			return b.Parent
		}
		return ""
	}
	if id := own(b); id != "" || cp == nil {
		return id
	}
	return own(cp)
}

// execList is the ordered row list for a snapshot and the session-only fold set.
func (g graphData) execList(flip map[string]bool) []eRow {
	act := g.actionable()
	h := eRow{kind: eSec, key: actKey, title: fmt.Sprintf("Actionable (%d)", len(act))}
	rows := []eRow{h}
	if !ecollapsed(flip, h) {
		if len(act) == 0 {
			rows = append(rows, eRow{kind: ePlace, key: actKey + "/none", depth: 1})
		}
		rows = append(rows, act...)
	}

	live := g.liveRows(act)
	lh := eRow{kind: eSec, key: liveKey, title: fmt.Sprintf("Live (%d)", len(live))}
	rows = append(rows, lh)
	if !ecollapsed(flip, lh) {
		if len(live) == 0 {
			rows = append(rows, eRow{kind: ePlace, key: liveNone, depth: 1})
		}
		rows = append(rows, live...)
	}

	spRows := g.specsRows()
	sh := eRow{kind: eSec, key: specsKey, title: fmt.Sprintf("Specs (%d)", len(spRows))}
	rows = append(rows, sh)
	if !ecollapsed(flip, sh) {
		if len(spRows) == 0 {
			rows = append(rows, eRow{kind: ePlace, key: specsNone, depth: 1})
		}
		rows = append(rows, spRows...)
	}

	cpRows := g.cpsRows()
	ch := eRow{kind: eSec, key: cpsKey, title: fmt.Sprintf("Checkpoints (%d)", len(cpRows))}
	rows = append(rows, ch)
	if !ecollapsed(flip, ch) {
		if len(cpRows) == 0 {
			rows = append(rows, eRow{kind: ePlace, key: cpsNone, depth: 1})
		}
		rows = append(rows, cpRows...)
		if landed := g.landedRows(); len(landed) > 0 { // ponytail: no empty-landed placeholder
			lh := eRow{kind: eSec, key: landedKey, title: fmt.Sprintf("Landed in last 24h (%d)", len(landed)), depth: 1}
			rows = append(rows, lh)
			if !ecollapsed(flip, lh) {
				rows = append(rows, landed...)
			}
		}
	}

	inits := map[string]*bd.Bead{}
	for i, b := range g.beads {
		if b.Kind == contract.KindInitiative {
			inits[b.ID] = &g.beads[i]
		}
	}
	initOf := func(b *bd.Bead) string { return g.initiativeOf(b, nil) }
	cps, specs := execListed(g.beads)
	members := map[string][]*bd.Bead{}
	for _, b := range append(slices.Clone(cps), specs...) {
		members[initOf(b)] = append(members[initOf(b)], b)
	}
	// Tasks and Pivots are never under a Checkpoint: they sit in their own Initiative,
	// else the one of the Checkpoint they wait on.
	sides, seen := map[string][]eRow{}, map[string]bool{}
	for _, cp := range cps {
		w, rel := g.waiters(cp)
		for i, x := range w {
			if seen[x.ID] {
				continue
			}
			seen[x.ID] = true
			p := g.initiativeOf(x, cp)
			sides[p] = append(sides[p], eRow{kind: eSide, key: "side:" + x.ID, b: x, cp: cp, rel: rel[i], depth: 1})
		}
	}
	for i, b := range g.beads { // every other live Task: no cp, no hint
		if b.Kind == contract.KindTask && execLive(b) && !seen[b.ID] {
			seen[b.ID] = true
			p := g.initiativeOf(&g.beads[i], nil)
			sides[p] = append(sides[p], eRow{kind: eSide, key: "side:" + b.ID, b: &g.beads[i], depth: 1})
		}
	}
	for id := range sides {
		if _, ok := members[id]; !ok {
			members[id] = nil
		}
	}
	var ids []string
	for id := range members {
		if id != "" {
			ids = append(ids, id)
		}
	}
	slices.SortStableFunc(ids, func(a, b string) int { return beadCmp(*inits[a], *inits[b]) })

	// group emits the rows of the Initiative group (or No Initiative section) id at
	// depth d: its Checkpoints, Specs, then Tasks and Pivots.
	group := func(id string, d int) (out []eRow) {
		ms := members[id]
		slices.SortStableFunc(ms, func(a, b *bd.Bead) int { // Checkpoints first, then Specs
			if (a.Kind == contract.KindCheckpoint) != (b.Kind == contract.KindCheckpoint) {
				return boolInt(b.Kind == contract.KindCheckpoint) - boolInt(a.Kind == contract.KindCheckpoint)
			}
			return beadCmp(*a, *b)
		})
		for _, b := range ms {
			if b.Kind == contract.KindSpec {
				out = append(out, eRow{kind: eSpec, key: b.ID, b: b, depth: d})
				continue
			}
			cr := eRow{kind: eCP, key: b.ID, b: b, cp: b, depth: d}
			out = append(out, cr)
			if ecollapsed(flip, cr) {
				continue
			}
			open, closed := g.cpKids(b)
			for _, m := range open {
				out = append(out, eRow{kind: eMem, key: m.ID, b: m, cp: b, rel: "member", depth: d + 1})
			}
			if len(closed) == 0 {
				continue
			}
			s := eRow{kind: eSum, key: b.ID + "/closed", cp: b, members: closed, depth: d + 1}
			out = append(out, s)
			if !ecollapsed(flip, s) {
				for _, m := range closed {
					out = append(out, eRow{kind: eClosed, key: m.ID, b: m, cp: b, rel: "member", depth: d + 2})
				}
			}
		}
		ss := slices.Clone(sides[id])
		slices.SortStableFunc(ss, func(a, b eRow) int { return beadCmp(*a.b, *b.b) })
		for _, s := range ss {
			s.depth = d
			out = append(out, s)
		}
		return out
	}

	th := eRow{kind: eSec, key: treeKey, title: fmt.Sprintf("Initiatives (%d)", len(ids))}
	rows = append(rows, th)
	if !ecollapsed(flip, th) {
		if len(ids) == 0 {
			rows = append(rows, eRow{kind: ePlace, key: treeKey + "/none", title: "no Initiatives", depth: 1})
		}
		for _, id := range ids {
			h := eRow{kind: eSec, key: "i:" + id, title: inits[id].Title, init: inits[id], depth: 1}
			rows = append(rows, h)
			if !ecollapsed(flip, h) {
				rows = append(rows, group(id, 2)...)
			}
		}
	}

	nh := eRow{kind: eSec, key: "i:", title: fmt.Sprintf("%s (%d)", noInitiative, len(members[""])+len(sides[""]))}
	rows = append(rows, nh)
	if !ecollapsed(flip, nh) {
		if body := group("", 1); len(body) > 0 {
			rows = append(rows, body...)
		} else {
			rows = append(rows, eRow{kind: ePlace, key: nh.key + "/none", title: "no freestanding beads", depth: 1})
		}
	}
	return rows
}

// hidden are the beads a folded row hides: those under it in the fully expanded list.
func (t execTab) hidden(full []eRow, r eRow) []bd.Bead {
	i := slices.IndexFunc(full, func(f eRow) bool { return f.key == r.key })
	return below(full, i, func(f eRow) int { return f.depth }, func(f eRow) *bd.Bead { return f.b })
}

// expanded is the flip set that opens every row.
func (t execTab) expanded() map[string]bool {
	return expandAll(t.g.execList, func(f map[string]bool, r eRow) bool {
		return (r.kind == eSec || r.kind == eCP || r.kind == eSum) && ecollapsed(f, r)
	}, func(r eRow) string { return r.key })
}

// execTab's state: the folds and the selected row's key, which follows
// its bead across refreshes. g is handed over by tabAt before every key and frame.
type execTab struct {
	g    graphData
	flip map[string]bool
	peek map[string]bool // keys a Recents jump flipped, with their pre-jump value: never saved
	path string          // the fold file ("" when the common dir is unresolved)
	sel  string
	open bool // d: the detail pane is open
	off  int  // the detail viewport's scroll offset
	w, h int
}

// newExec is an Execute tab with the folds the human saved on this machine.
func newExec() component {
	path := foldsPath(foldsFile)
	flip := loadFolds(path, "execute")
	if flip == nil {
		flip = map[string]bool{}
	}
	return execTab{path: path, flip: flip}
}

// save writes the folds minus a closed or missing bead's; the Actionable key is never pruned. The fold still
// applies this session.
func (t execTab) save() error {
	return saveFolds(t.path, "execute", pruneFolds(unpeeked(t.flip, t.peek), t.g.beads, actKey, liveKey, specsKey, cpsKey, landedKey, treeKey))
}

func (t execTab) rows() []eRow { return t.g.execList(t.flip) }

// selIndex is the selected row's index. A bead that moved between Actionable and Live stays
// selected in its new section. A Live row that left the list falls back to its tree twin, else
// its group header, as does a paused-band row that was unpaused; any other (an Actionable row that left both sections or whose Live section
// is folded included) falls back to the first.
func (t execTab) selIndex(rows []eRow) int {
	at := func(key string) int { return slices.IndexFunc(rows, func(r eRow) bool { return r.key == key }) }
	if i := at(t.sel); i >= 0 {
		return i
	}
	if o := otherSection(t.sel); o != "" { // moved between Actionable and Live
		if i := at(o); i >= 0 {
			return i
		}
	}
	// left Live or Checkpoints: the tree twin, else its Initiative's header
	id, ok := strings.CutPrefix(t.sel, livePrefix)
	if !ok {
		id, ok = strings.CutPrefix(t.sel, cpsPrefix)
	}
	if !ok {
		id, ok = strings.CutPrefix(t.sel, specsPrefix)
	}
	if !ok { // a paused-band row unpaused out of Actionable, not into a folded Live
		if id, ok = strings.CutPrefix(t.sel, "act:"); ok {
			ok = !t.g.isLive(t.g.beadByID(id))
		}
	}
	if ok {
		for _, k := range []string{id, "side:" + id} {
			if i := at(k); i >= 0 {
				return i
			}
		}
		if j := t.g.beadIndex(id); j >= 0 {
			b := &t.g.beads[j]
			var cp *bd.Bead
			if p := t.g.beadIndex(b.Parent); p >= 0 && t.g.beads[p].Kind == contract.KindCheckpoint {
				cp = &t.g.beads[p]
			}
			if i := at("i:" + t.g.initiativeOf(b, cp)); i >= 0 {
				return i
			}
		}
	}
	if t.flip[treeKey] { // a tree row folded away: the Initiatives header
		return max(at(treeKey), 0)
	}
	return 0
}

// drawerBead is the bead the detail shows: the row's, else its Initiative on a head row.
// The nil kinds must match the rows rowText renders without the shared bead body.
func (t execTab) drawerBead() *bd.Bead {
	r, _ := t.selRow()
	if r.kind == eSec {
		return r.init
	}
	if r.kind == ePlace || r.kind == eSum {
		return nil
	}
	return r.b
}

func (t execTab) selRow() (eRow, bool) {
	rows := t.rows()
	if len(rows) == 0 {
		return eRow{}, false
	}
	return rows[t.selIndex(rows)], true
}

// selected is the cursor's bead, nil on a header, placeholder or '▸ N closed' row.
func (t execTab) selected() *bd.Bead {
	r, _ := t.selRow()
	return r.b
}

func (t execTab) count() int { return t.g.execBadge() }
func (t execTab) actN() int  { return len(t.g.actionable()) }
func (t execTab) liveN() int { return len(t.g.liveRows(t.g.actionable())) }

// enterWhy is why enter does nothing on r (graphData.rowEnterWhy).
func (t execTab) enterWhy(r eRow) string {
	if r.kind == eLanded {
		return landedRefuse
	}
	return t.g.rowEnterWhy(r.b, r.leaf())
}

// bindings are ADR 0170 §7's hints for the selected row: enter only where it acts,
// then the shared x and +/- on bead rows.
func (t execTab) bindings() []key.Binding {
	r, ok := t.selRow()
	if !ok {
		return nil
	}
	var bs []key.Binding
	if r.b != nil && t.enterWhy(r) == "" {
		if v := t.g.enterVerb(r.b); v != "" {
			bs = append(bs, verbKey("enter", v))
			if t.g.rowBgShown(r.b, r.leaf()) {
				bs = append(bs, keyBg)
			}
		}
	}
	if r.kind == eSec || r.kind == eSum || r.kind == eCP {
		bs = append(bs, keyFold)
	}
	if t.open {
		bs = append(bs, keyShut, keyScroll)
	} else {
		bs = append(bs, keyDetail)
	}
	if r.b == nil {
		if r.kind == eSec && isOpenInitiative(r.init) {
			bs = append(bs, keyCancelKey)
		}
		return append(bs, keySection)
	}
	if r.b.Status != "closed" {
		bs = append(bs, verbKey("p", pauseLabel(r.b)))
	}
	if l := t.g.xHint(r.b); l != "" {
		bs = append(bs, verbKey("x", l))
	}
	if t.g.xOffers(r.b) {
		bs = append(bs, xBinding(r.b))
	}
	bs = append(bs, keyTranscript)
	if r.b.Status != "closed" {
		bs = append(bs, keyPrio)
	}
	return append(bs, keySection)
}

// extra is moving, then the keys the row handles but does not hint: shown in ? alone.
func (t execTab) extra() []key.Binding {
	out := []key.Binding{keyMove}
	r, ok := t.selRow()
	if !ok {
		return out
	}
	shown := map[string]bool{}
	for _, b := range t.bindings() {
		shown[b.Keys()[0]] = true
	}
	cand := []key.Binding{keyLaunch, keyBg}
	if r.b != nil {
		cand = append(cand, keyPauseKey, keyDropKey, keyWithdrawKey, keyPrio)
	}
	for _, b := range cand {
		if !shown[b.Keys()[0]] {
			out = append(out, b)
		}
	}
	return out
}

// execKey routes the Execute tab's action keys through the shared paths, as defogKey
// does; ok is false for any key the tab doesn't own (space, j/k and d stay with update).
func (m Model) execKey(k tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	r, ok := m.tabAt(m.tab).(execTab).selRow()
	if !ok {
		return m, nil, false
	}
	if key.Matches(k, keyFold) && (r.kind == eSec || r.kind == eSum || r.kind == eCP) {
		next, cmd := m.tabKey(k)
		nm := next.(Model)
		if err := nm.tabAt(nm.tab).(execTab).save(); err != nil {
			nm.setStatus("folds won't survive a restart: " + err.Error())
		}
		return nm, cmd, true
	}
	if r.kind == eLanded && (key.Matches(k, keyLaunch) || key.Matches(k, keyBg)) {
		m.setStatus(landedRefuse)
		return m, nil, true
	}
	return m.queueRowKey(k, r.b, r.leaf())
}

func (t execTab) update(k tea.KeyMsg) (component, tea.Cmd) {
	rows := t.rows()
	if len(rows) == 0 {
		return t, nil
	}
	switch r := rows[t.selIndex(rows)]; {
	case key.Matches(k, keyMove):
		d := 1
		if s := k.String(); s == "k" || s == "up" {
			d = -1
		}
		t.sel, t.off = rows[stepTo(rows, t.selIndex(rows), d, eRow.selectable)].key, 0 // the detail follows: back to the top
	case key.Matches(k, keySection):
		d := 1
		if s := k.String(); s == "K" || s == "ctrl+up" {
			d = -1
		}
		t.sel, t.off = rows[jumpTo(rows, t.selIndex(rows), d, eRow.isSection)].key, 0
	case key.Matches(k, keyFold) && (r.kind == eSec || r.kind == eSum || r.kind == eCP):
		t.flip = maps.Clone(t.flip)
		if t.flip[r.key] = !t.flip[r.key]; !t.flip[r.key] {
			delete(t.flip, r.key) // back at the default
		}
		t.peek = unpeek(t.peek, r.key)
	case t.open && key.Matches(k, keyShut):
		t.open = false
	case !t.open && key.Matches(k, keyDetail):
		t.open = true
	case t.open && key.Matches(k, keyScroll):
		vp := t.detailVP()
		switch k.String() {
		case "pgup":
			vp.PageUp()
		case "pgdown":
			vp.PageDown()
		case "ctrl+u":
			vp.HalfPageUp()
		default:
			vp.HalfPageDown()
		}
		t.off = vp.YOffset
	}
	return t, nil
}

// relText is the context hint after a Task, Pivot or Actionable row's title and id.
func relText(rel string) string {
	switch rel {
	case "gates":
		return "gates →"
	case "before land":
		return "holds land of →"
	case "after land":
		return "waits on land of →"
	}
	return "in →"
}

// line is a row's left part and its right-edge suffix.
func (t execTab) line(r eRow, tagW int, blockers map[string]bool, trail string) (string, string) {
	left, right := t.body(r, tagW, blockers[r.id()], trail)
	return blockerGutter(blockers, r.id(), nil) + left, right
}

// body is line after its gutter; hot is whether the row's bead blocks the selection.
func (t execTab) body(r eRow, tagW int, hot bool, trail string) (string, string) {
	g := t.g
	ind := strings.Repeat("  ", r.depth)
	const blank = "  " // the caret slot of a row that can't fold
	fold := "▾ "
	if ecollapsed(t.flip, r) {
		fold = "▸ "
	}
	switch r.kind {
	case eSec:
		switch r.key {
		case actKey:
			return ind + dimSt.Render(fold) + selMarker.Render(r.title), ""
		case liveKey: // the count in the live ●'s green
			return ind + dimSt.Render(fold) + liveTitle(r.title), ""
		case specsKey, cpsKey, treeKey:
			return ind + dimSt.Render(fold) + sectionSt.Render(r.title), ""
		case noInitKey: // the count, and the folded trailer
			return ind + dimSt.Render(fold) + sectionSt.Render(r.title) + trail, ""
		}
		if r.init == nil {
			return ind + dimSt.Render(fold) + dimSt.Render(r.title) + trail, ""
		}
		return ind + dimSt.Render(fold) + g.sbrn(r.init, dimSt.Render(r.title), true, 0) + trail, ""
	case ePlace:
		if r.title != "" {
			return ind + blank + dimSt.Render(r.title), ""
		}
		if r.key == liveNone {
			return ind + blank + dimSt.Render(liveEmpty), ""
		}
		if r.key == specsNone {
			return ind + blank + dimSt.Render(specsEmpty), ""
		}
		if r.key == cpsNone {
			return ind + blank + dimSt.Render(cpsEmpty), ""
		}
		return ind + blank + dimSt.Render("nothing waits on you"), ""
	case eSum:
		return ind + dimSt.Render(fmt.Sprintf("%s%d closed", fold, len(r.members))), ""
	}
	suffix := strings.TrimLeft(g.rowSuffix(r.b), " ")
	if r.kind == eCP || r.kind == eLive && r.b.Kind == contract.KindCheckpoint {
		if r.kind == eLive { // the Live twin has nothing to fold
			fold = blank
		}
		return ind + fold + g.sbrn(r.b, containerTitle(r.b, hot, r.b.Title), true, 0) + trail, suffix
	}
	var s string
	if r.kind == eCPS { // a container row, as its tree row: no AFK mute on the title or badge
		s = ind + blank + g.sbrn(r.b, containerTitle(r.b, hot, r.b.Title), true, tagW)
		s += sep() + dimSt.Render(g.rollup(r.b).label()) // the Rollup word and its qualifiers: a status hint, not an action
	} else {
		s = ind + blank + g.sbrnLeaf(r.b, leafTitle(r.b, hot), tagW, hot)
	}
	if r.cpAct() { // a paused Checkpoint: the Rollup word, as on its Checkpoints row
		s += sep() + dimSt.Render(g.rollup(r.b).label())
	} else if (r.kind == eAct || r.kind == eSide || r.kind == eLive) && r.cp != nil && r.cp != r.b {
		s += sep() + dimSt.Render(relText(r.rel)+" "+r.cp.Title)
	}
	if r.kind == eLanded {
		s += sep() + dimSt.Render("landed "+sha8(strings.TrimPrefix(r.members[0].CloseReason, "landed: "))+" · "+HumanAge(r.members[0].Closed))
	}
	return s, suffix
}

func (t execTab) view(w, h int) string {
	rows := t.rows()
	tagW := 0 // bead rows share one kind-badge column
	for _, r := range rows {
		if r.b != nil && r.kind != eCP && !(r.kind == eLive && r.b.Kind == contract.KindCheckpoint) {
			tagW = max(tagW, kindTagW(r.b))
		}
	}
	sel := t.selIndex(rows)
	b := t.selected()
	blockers := t.g.blockerSet(b)
	full := t.g.execList(t.expanded())
	row := func(i int) (string, string) {
		r, trail := rows[i], ""
		if (r.kind == eCP || r.kind == eSec && (r.key == noInitKey || !isSectionKey(r.key))) && ecollapsed(t.flip, r) {
			trail = t.g.trailer(t.hidden(full, r), false, false)
		}
		return t.line(r, tagW, blockers, trail)
	}
	shade := shadeSet(t.g.parentOf, rows, sel, full, eRow.shadeIn)
	lines, selRow := make([]string, len(rows)), sel
	ids := make([]string, len(rows))
	liveAt, specsAt, cpsAt, treeAt, noInitAt := 0, 0, 0, 0, 0 // the section headers that get a blank line above
	for i, r := range rows {
		ids[i], lines[i] = r.id(), strconv.Itoa(i)
		switch r.key {
		case liveKey:
			liveAt = i
		case specsKey:
			specsAt = i
		case cpsKey:
			cpsAt = i
		case treeKey:
			treeAt = i
		case noInitKey:
			noInitAt = i
		}
	}
	lines, ids, sel = gapAt(lines, ids, sel, noInitAt) // a blank line above each section header after Actionable
	lines, ids, sel = gapAt(lines, ids, sel, treeAt)
	lines, ids, sel = gapAt(lines, ids, sel, cpsAt)
	lines, ids, sel = gapAt(lines, ids, sel, specsAt)
	lines, ids, sel = gapAt(lines, ids, sel, liveAt)
	var footer func(off, lh int) []string
	if b != nil {
		footer = blockerFooter(t.g, t.g.openBlockers(b), ids, w)
	}
	return drawRows(lines, row, shade, sel, selRow, t.open, w, h, t.detailVP(), footer)
}

func (t execTab) drawerOpen() bool { return t.open }
