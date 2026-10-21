package tui

// The Defog tab (Spec mc-lu2x.10): every published Chart still in defog under its
// Initiative, each with the child beads that clear its fog, and a ●N badge of the
// HITL work only the human can unblock. A thin client over the shell's snapshot
// (graphData): no loader, no subprocess in view or update.

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
	"jayulabs.org/mugicrew/session"
)

// Row kinds.
const (
	dHead  = iota // an Initiative group (or No Initiative)
	dChart        // a Chart in defog
	dLeaf         // an open HITL leaf
	dSum          // '▸ N closed'
	dChild        // an open AFK bead, or a closed child inside an expanded '▸ N closed'
	dSec          // the 'Actionable' header
	dPlace        // the empty-Actionable placeholder
	dAct          // an Actionable HITL leaf, the twin of its dLeaf row
	dLive         // a Live effort in the Live section, the twin of its tree row
	dCW           // an open Chart in the Charts section: status only
	dFin          // a Chart closed within 24h in 'Finished in last 24h': status only
	dACW          // an own-paused open Chart in Actionable's paused band, the twin of its dCW and dChart rows
	dACA          // an own-paused open AFK Chart child in Actionable's paused band, the twin of its dChild row
)

const (
	chartsKey    = "s:charts"          // the Charts section between Live and Initiatives: unfolded by default, never pruned
	chartsPrefix = "charts:"           // a Charts row's key is chartsPrefix + the Chart's id
	chartsNone   = chartsKey + "/none" // the empty Charts section's placeholder
	chartsEmpty  = "no open Charts"
	finKey       = chartsKey + "/finished" // the nested 'Finished in last 24h' header: folded by default
	finPrefix    = "fin:"                  // a Finished row's key is finPrefix + the Chart's id
	finWindow    = 24 * time.Hour
	finRefuse    = "finished — nothing to launch"
	chartsDetail = "Every open Chart with its status word: where it is on its way to a Spec. Status only."
)

// dRow is one list line. key is the fold and selection key: "i:<initID>", the bead
// id, or "<chartID>/closed".
type dRow struct {
	kind     int
	key      string
	title    string   // a header's title
	init     *bd.Bead // a header's Initiative; nil for No Initiative
	b, chart *bd.Bead
	members  []*bd.Bead // '▸ N closed'
	afk      bool       // a dChild that is an open AFK bead: launchable via run (its title is muted)
	depth    int        // nesting: a top-level section header is 0; line() indents 2·depth columns
	n        int        // the No Initiative header's Chart count
}

// noInit is the No Initiative section header, a dHead without an Initiative.
func (r dRow) noInit() bool { return r.kind == dHead && r.init == nil }

// selectable is whether the cursor may rest on the row: not on a placeholder.
func (r dRow) selectable() bool { return r.kind != dPlace }

// foldable is whether space folds the row: headers, Charts and '▸ N closed'.
func (r dRow) foldable() bool {
	return r.kind == dHead || r.kind == dChart || r.kind == dSum || r.kind == dSec
}

// shadeIn reduces the row for the ancestor shade: a bead row stands for its bead, an
// Initiative header for its Initiative.
func (r dRow) shadeIn() shadeIn {
	switch r.kind {
	case dHead:
		s := shadeIn{grp: r.key, head: true}
		if r.init != nil {
			s.id = r.init.ID
		}
		return s
	case dAct, dACW, dACA:
		return shadeIn{id: r.b.ID, act: true}
	case dLive:
		return shadeIn{id: r.b.ID, act: true, live: true}
	case dChart, dLeaf, dChild, dCW, dFin:
		return shadeIn{id: r.b.ID}
	}
	return shadeIn{}
}

// bk is the row's blocker key: its bead id (an Actionable, Live or Charts row's key is
// "act:<id>", "live:<id>", "charts:<id>" or "fin:<id>"), else its key.
func (r dRow) bk() string {
	if r.kind == dAct || r.kind == dACW || r.kind == dACA || r.kind == dLive || r.kind == dCW || r.kind == dFin {
		return r.b.ID
	}
	return r.key
}

// leaf is whether enter routes the row as an open HITL leaf.
func (r dRow) leaf() bool {
	return r.kind == dLeaf || r.kind == dAct || r.kind == dLive && isHITL(r.b.Kind) && !r.b.Container
}

// collapsed: every container below Actionable starts folded; space flips.
func collapsed(flip map[string]bool, r dRow) bool {
	return flip[r.key] != (r.kind == dHead || r.kind == dChart || r.kind == dSum || r.key == finKey)
}

// lstate is a HITL leaf's state; the one answer row marks, enter, hints and the
// badge share.
type lstate int

const (
	sReady lstate = iota
	sBlocked
	sPaused
	sLaunching
	sLive
	sTurn
	sAsk
	sDone
	sExited
)

// leafState: launching > done > ask > turn > live > held launch > exited > blocked > paused (own
// or inherited) > ready.
func (g graphData) leafState(b *bd.Bead) lstate {
	s := g.sessions[b.ID]
	up := s.State == session.Alive
	w := humanWait(s, false) // done is tested first
	switch {
	case g.launching[b.ID]:
		return sLaunching
	case g.done(b):
		return sDone
	case w == session.WaitAsk:
		return sAsk
	case w == session.WaitTurn:
		return sTurn
	case up:
		return sLive
	case !g.launched[b.ID].IsZero(): // held after a successful launch, not yet seen alive: never ready
		return sLaunching
	case s.State == session.Exited:
		return sExited
	case g.blocked(b):
		return sBlocked
	case bd.Paused(*b) || g.inheritedPause(b) != "":
		return sPaused
	}
	return sReady
}

// defogCharts are the listed Charts: open or in_progress (tested explicitly, since
// Bead.Open() counts a deferred Draft) and not chart:defogged.
func defogCharts(beads []bd.Bead) (cs []*bd.Bead) {
	for i, b := range beads {
		if b.Kind == contract.KindChart && (b.Status == "open" || b.Status == "in_progress") && !b.HasLabel("chart:defogged") {
			cs = append(cs, &beads[i])
		}
	}
	return
}

// chartKids splits a Chart's own children into open HITL leaves (Survey first),
// open AFK beads and closed ones, each in beadCmp order. Drafts and events are out.
func chartKids(beads []bd.Bead, c *bd.Bead) (hitl, afk, closed []*bd.Bead) {
	for i, b := range beads {
		if b.Parent != c.ID || b.Event || b.Status == "deferred" {
			continue
		}
		switch {
		case b.Status == "closed":
			closed = append(closed, &beads[i])
		case isHITL(b.Kind) && !b.Container:
			hitl = append(hitl, &beads[i])
		default:
			afk = append(afk, &beads[i])
		}
	}
	byCmp := func(a, b *bd.Bead) int { return beadCmp(*a, *b) }
	slices.SortStableFunc(hitl, func(a, b *bd.Bead) int {
		if (a.Kind == contract.KindSurvey) != (b.Kind == contract.KindSurvey) {
			return boolInt(b.Kind == contract.KindSurvey) - boolInt(a.Kind == contract.KindSurvey)
		}
		return byCmp(a, b)
	})
	slices.SortStableFunc(afk, byCmp)
	slices.SortStableFunc(closed, byCmp)
	return
}

// defogList is the ordered row list for a snapshot and the session-only fold set.
func (g graphData) defogList(flip map[string]bool) []dRow {
	rows := append(g.defogActionable(flip), g.defogLive(flip)...)
	rows = append(rows, g.defogChartsSection(flip)...)
	groups, nGroups, loose, nLoose := defogTree(g.beads, flip)
	rows = append(rows, defogSection(dRow{kind: dSec, key: treeKey, title: fmt.Sprintf("Initiatives (%d)", nGroups)}, flip, groups, nGroups, "no Initiatives")...)
	return append(rows, defogSection(dRow{kind: dHead, key: "i:", title: noInitiative, n: nLoose}, flip, loose, nLoose, "no freestanding beads")...)
}

// defogSection is top-level header h, then body unless folded, or the placeholder when
// n is 0.
func defogSection(h dRow, flip map[string]bool, body []dRow, n int, place string) []dRow {
	rows := []dRow{h}
	switch {
	case collapsed(flip, h):
	case n == 0:
		rows = append(rows, dRow{kind: dPlace, key: h.key + "/none", title: place, depth: 1})
	default:
		rows = append(rows, body...)
	}
	return rows
}

// ownPausedReady is whether a bead (container or leaf) carries its own pause and is otherwise
// ready: open, unblocked, not running, not exited. An inherited-only pause stays out (Spec mc-qgtw FR 8-11).
func (g graphData) ownPausedReady(b *bd.Bead) bool {
	return b.Status == "open" && bd.Paused(*b) && !g.blocked(b) && !g.running(b) && g.sessions[b.ID].State != session.Exited
}

// defogAct is the human's queue: the HITL leaves under listed Charts, banded; own-paused ready AFK children and Charts join the paused band.
func (g graphData) defogAct() []dRow {
	wait, front, paused := g.defogBands()
	return slices.Concat(wait, front, paused)
}

// defogBands is defogAct's three bands: waiting on the human, ready, then own-paused.
func (g graphData) defogBands() (wait, front, paused []dRow) {
	var rows, afkPaused []dRow
	for _, c := range defogCharts(g.beads) {
		hitl, afk, _ := chartKids(g.beads, c)
		for _, l := range hitl {
			rows = append(rows, dRow{kind: dAct, key: "act:" + l.ID, b: l, chart: c, depth: 1})
		}
		for _, a := range afk { // own-paused ready AFK children join only the paused band (Spec mc-llx8 FR 3, 9)
			if g.ownPausedReady(a) {
				afkPaused = append(afkPaused, dRow{kind: dACA, key: "act:" + a.ID, b: a, chart: c, afk: true, depth: 1})
			}
		}
	}
	wait, front, paused = band(g, rows, func(r dRow) *bd.Bead { return r.b })
	paused = append(paused, afkPaused...)
	// An own-paused open unblocked listed Chart joins the paused band (Spec mc-qgtw FR 8): not
	// Live or launching, and an inherited-only pause stays out.
	for _, c := range defogCharts(g.beads) {
		if g.ownPausedReady(c) {
			paused = append(paused, dRow{kind: dACW, key: "act:" + c.ID, b: c, chart: c, depth: 1})
		}
	}
	for _, b := range g.homePaused(tabDefog, idSet(slices.Concat(wait, front, paused), func(r dRow) *bd.Bead { return r.b })) {
		c := g.beadByID(b.Parent)
		switch {
		case b.Kind == contract.KindChart:
			paused = append(paused, dRow{kind: dACW, key: "act:" + b.ID, b: b, chart: b, depth: 1})
		case c.Kind != contract.KindChart:
			paused = append(paused, dRow{kind: dACA, key: "act:" + b.ID, b: b, depth: 1})
		case isHITL(b.Kind) && !b.Container:
			paused = append(paused, dRow{kind: dAct, key: "act:" + b.ID, b: b, chart: c, depth: 1})
		default:
			paused = append(paused, dRow{kind: dACA, key: "act:" + b.ID, b: b, chart: c, afk: true, depth: 1})
		}
	}
	slices.SortStableFunc(paused, func(a, b dRow) int { return beadCmp(*a.b, *b.b) })
	return
}

// homePaused is the paused-band beads (Spec mc-f22wp FR 5, 10): every own-paused, otherwise ready
// bead whose home tab is tab and not in seen (bead ids). Drafts and events are out.
func (g graphData) homePaused(tab int, seen map[string]bool) (out []*bd.Bead) {
	for i := range g.beads {
		b := &g.beads[i]
		if !b.Event && !seen[b.ID] && g.ownPausedReady(b) && homeTab(*b, g.beads) == tab {
			out = append(out, b)
		}
	}
	return
}

// defogActionable is the Actionable header, then its rows or the placeholder while
// unfolded.
func (g graphData) defogActionable(flip map[string]bool) []dRow {
	act := g.defogAct()
	h := dRow{kind: dSec, key: actKey, title: fmt.Sprintf("Actionable (%d)", len(act))}
	rows := []dRow{h}
	if !collapsed(flip, h) {
		if len(act) == 0 {
			rows = append(rows, dRow{kind: dPlace, key: actKey + "/none", depth: 1})
		}
		rows = append(rows, act...)
	}
	return rows
}

// defogLive is the Live header, then every Live effort among every open Chart (stamped or
// not) and its open leaves, in beadCmp order, or the placeholder while unfolded.
func (g graphData) defogLive(flip map[string]bool) []dRow {
	var cands []*bd.Bead
	of := map[string]*bd.Bead{}
	for _, c := range g.openCharts() {
		hitl, afk, _ := chartKids(g.beads, c)
		for _, b := range append(append([]*bd.Bead{c}, hitl...), afk...) {
			cands, of[b.ID] = append(cands, b), c
		}
	}
	act := idSet(g.defogAct(), func(r dRow) *bd.Bead { return r.b })
	var live []dRow
	for _, b := range g.liveBeads(cands, act) {
		live = append(live, dRow{kind: dLive, key: livePrefix + b.ID, b: b, chart: of[b.ID], depth: 1})
	}
	h := dRow{kind: dSec, key: liveKey, title: fmt.Sprintf("Live (%d)", len(live))}
	rows := []dRow{h}
	if !collapsed(flip, h) {
		if len(live) == 0 {
			rows = append(rows, dRow{kind: dPlace, key: liveNone, depth: 1})
		}
		rows = append(rows, live...)
	}
	return rows
}

// openCharts are the Charts section's Charts: every open or in_progress Chart (a deferred
// Draft is out), stamped chart:defogged or not, by status word rank then beadCmp.
// ponytail: chartWord is recomputed per row in line(), each scanning g.beads: O(charts × beads) per render; memoize per defogList if it shows.
func (g graphData) openCharts() []*bd.Bead {
	var cs []*bd.Bead
	rank := map[string]int{}
	for i, b := range g.beads {
		if b.Kind == contract.KindChart && (b.Status == "open" || b.Status == "in_progress") {
			cs = append(cs, &g.beads[i])
			rank[b.ID] = g.chartWord(&g.beads[i]).Rank
		}
	}
	slices.SortStableFunc(cs, func(a, b *bd.Bead) int {
		if c := rank[a.ID] - rank[b.ID]; c != 0 {
			return c
		}
		return beadCmp(*a, *b)
	})
	return cs
}

// defogChartsSection is the Charts header, then a status row per open Chart or the
// placeholder while unfolded; the header's N counts them folded or not.
func (g graphData) defogChartsSection(flip map[string]bool) []dRow {
	cs := g.openCharts()
	h := dRow{kind: dSec, key: chartsKey, title: fmt.Sprintf("Charts (%d)", len(cs))}
	rows := []dRow{h}
	if collapsed(flip, h) {
		return rows
	}
	if len(cs) == 0 {
		rows = append(rows, dRow{kind: dPlace, key: chartsNone, title: chartsEmpty, depth: 1})
	}
	for _, c := range cs {
		rows = append(rows, dRow{kind: dCW, key: chartsPrefix + c.ID, b: c, chart: c, depth: 1})
	}
	if fin := g.finishedCharts(); len(fin) > 0 { // ponytail: no empty-finished placeholder
		fh := dRow{kind: dSec, key: finKey, title: fmt.Sprintf("Finished in last 24h (%d)", len(fin)), depth: 1}
		rows = append(rows, fh)
		if !collapsed(flip, fh) {
			for _, c := range fin {
				rows = append(rows, dRow{kind: dFin, key: finPrefix + c.ID, b: c, chart: c, depth: 2})
			}
		}
	}
	return rows
}

// finishedCharts are the Charts closed within finWindow of their own Closed time, newest first.
func (g graphData) finishedCharts() []*bd.Bead {
	var cs []*bd.Bead
	now := clock()
	for i, b := range g.beads {
		if b.Kind == contract.KindChart && b.Status == "closed" && !b.Event && now.Sub(b.Closed) <= finWindow {
			cs = append(cs, &g.beads[i])
		}
	}
	slices.SortStableFunc(cs, func(a, b *bd.Bead) int { return b.Closed.Compare(a.Closed) })
	return cs
}

// chartOutcome is a closed Chart's outcome word and what decided it (ADR 0204 §4), first
// match: landed (a Spec is caused by it; sha from its landed: Landing, "" if none), handed
// back:, rejected, superseded (empty reason), else closed.
func (g graphData) chartOutcome(c *bd.Bead) (word, sha, why string) {
	for _, b := range g.beads {
		if b.Kind == contract.KindSpec && b.CausedBy == c.ID {
			for _, l := range g.beads {
				if l.Parent == c.ID && l.Kind == contract.KindLanding && l.Status == "closed" && strings.HasPrefix(l.CloseReason, "landed: ") {
					sha = strings.TrimPrefix(l.CloseReason, "landed: ")
					break
				}
			}
			return "landed", sha, "Spec " + b.ID + " is caused by the Chart"
		}
	}
	why = "closed: " + closeReason(c)
	switch {
	case strings.HasPrefix(c.CloseReason, "handed back:"):
		return "handed back", "", why
	case strings.HasPrefix(c.CloseReason, "rejected"):
		return "rejected", "", why
	case c.CloseReason == "":
		return "superseded", "", why
	}
	return "closed", "", why
}

// defogTree is the Initiative groups with their count, and the freestanding Charts'
// rows (depth 1, under the No Initiative header) with theirs; folds hide neither count.
func defogTree(beads []bd.Bead, flip map[string]bool) (groups []dRow, nGroups int, loose []dRow, nLoose int) {
	inits := map[string]*bd.Bead{}
	for i, b := range beads {
		if b.Kind == contract.KindInitiative {
			inits[b.ID] = &beads[i]
		}
	}
	members := map[string][]*bd.Bead{}
	for _, c := range defogCharts(beads) {
		p := ""
		if _, ok := inits[c.Parent]; ok {
			p = c.Parent
		}
		members[p] = append(members[p], c)
	}
	var ids []string
	for id := range members {
		if id != "" {
			ids = append(ids, id)
		}
	}
	slices.SortStableFunc(ids, func(a, b string) int { return beadCmp(*inits[a], *inits[b]) })
	// charts are the Charts' rows with their leaves, at depth d.
	charts := func(cs []*bd.Bead, d int) (rows []dRow) {
		slices.SortStableFunc(cs, func(a, b *bd.Bead) int { return beadCmp(*a, *b) })
		for _, c := range cs {
			cr := dRow{kind: dChart, key: c.ID, b: c, chart: c, depth: d}
			rows = append(rows, cr)
			if collapsed(flip, cr) {
				continue
			}
			hitl, afk, closed := chartKids(beads, c)
			for _, l := range hitl {
				rows = append(rows, dRow{kind: dLeaf, key: l.ID, b: l, chart: c, depth: d + 1})
			}
			for _, a := range afk {
				rows = append(rows, dRow{kind: dChild, key: a.ID, b: a, chart: c, afk: true, depth: d + 1})
			}
			if len(closed) == 0 {
				continue
			}
			s := dRow{kind: dSum, key: c.ID + "/closed", chart: c, members: closed, depth: d + 1}
			rows = append(rows, s)
			if !collapsed(flip, s) {
				for _, m := range closed {
					rows = append(rows, dRow{kind: dChild, key: m.ID, b: m, chart: c, depth: d + 2})
				}
			}
		}
		return
	}
	for _, id := range ids {
		h := dRow{kind: dHead, key: "i:" + id, title: inits[id].Title, init: inits[id], depth: 1}
		groups = append(groups, h)
		if !collapsed(flip, h) {
			groups = append(groups, charts(members[id], 2)...)
		}
	}
	return groups, len(ids), charts(members[""], 1), len(members[""])
}

// defogBadge is the Defog ●N: the waiting band's length. Folds don't matter.
func (g graphData) defogBadge() int {
	wait, _, _ := g.defogBands()
	return len(wait)
}

// defogTab's state: the session-only folds and the selected row's key, which
// follows its bead across refreshes. g is handed over by tabAt before every key
// and frame.
type defogTab struct {
	g    graphData
	flip map[string]bool
	peek map[string]bool // keys a Recents jump flipped, with their pre-jump value: never saved
	path string          // the fold file ("" when the common dir is unresolved)
	sel  string
	w, h int  // the body's size, handed over by tabAt
	open bool // d: the detail pane is open
	off  int  // the detail viewport's scroll offset
}

// newDefog is a Defog tab with the folds the human saved on this machine.
func newDefog() component {
	path := foldsPath(foldsFile)
	flip := loadFolds(path, "defog")
	if flip == nil {
		flip = map[string]bool{}
	}
	return defogTab{path: path, flip: flip}
}

// save writes the folds minus a closed or missing bead's; the fold still applies this
// session.
func (t defogTab) save() error {
	return saveFolds(t.path, "defog", pruneFolds(unpeeked(t.flip, t.peek), t.g.beads, actKey, liveKey, chartsKey, finKey, treeKey))
}

func (t defogTab) afterLaunch(id string) component {
	t.sel = afterLaunch(t.sel, id, rowKeys(t.g.defogAct(), func(r dRow) string { return r.key }))
	return t
}

func (t defogTab) rows() []dRow { return t.g.defogList(t.flip) }

// selIndex is the selected row's index. A bead that moved between Actionable and Live stays
// selected in its new section. A Live row that left the list falls back to its tree twin, else
// its group header; any other (an Actionable row that left both sections or whose Live section
// is folded included) falls back to the first.
func (t defogTab) selIndex(rows []dRow) int {
	at := func(key string) int { return slices.IndexFunc(rows, func(r dRow) bool { return r.key == key }) }
	if i := at(t.sel); i >= 0 {
		return i
	}
	if o := otherSection(t.sel); o != "" { // moved between Actionable and Live
		if i := at(o); i >= 0 {
			return i
		}
	}
	if id, ok := strings.CutPrefix(t.sel, "act:"); ok { // a leaf (AFK child or HITL) left Actionable: its tree twin; a Chart's act: row is excluded
		b := t.g.beadByID(id)
		if i := at(id); i >= 0 && b.Kind != contract.KindChart {
			return i
		}
		if i := at(chartsPrefix + b.Parent); i >= 0 && b.Kind != contract.KindChart && t.g.beadByID(b.Parent).HasLabel("chart:defogged") { // a child of a defogged Chart has no tree row: its Chart's
			return i
		}
	}
	if id, ok := strings.CutPrefix(t.sel, livePrefix); ok {
		if i := at(id); i >= 0 {
			return i
		}
		// the Initiative header of its Chart (a Chart's own Parent, else a leaf's Chart's)
		c := t.g.beadByID(id)
		if c.Kind != contract.KindChart {
			c = t.g.beadByID(c.Parent)
		}
		init := ""
		if p := t.g.beadByID(c.Parent); p.Kind == contract.KindInitiative {
			init = p.ID
		}
		if i := at("i:" + init); i >= 0 {
			return i
		}
	}
	if id, ok := strings.CutPrefix(t.sel, chartsPrefix); ok { // left Charts: its tree twin, else the section header
		if i := at(id); i >= 0 {
			return i
		}
		return max(at(chartsKey), 0)
	}
	if strings.HasPrefix(t.sel, finPrefix) { // a Finished row folded away or aged out
		if i := at(finKey); i >= 0 {
			return i
		}
		return max(at(chartsKey), 0)
	}
	if t.flip[treeKey] { // a tree row folded away: the Initiatives header
		return max(at(treeKey), 0)
	}
	return 0
}

// drawerBead is the bead the detail shows: the row's, else its Initiative on a head row.
// The nil kinds must match the rows rowText renders without the shared bead body.
func (t defogTab) drawerBead() *bd.Bead {
	r, _ := t.selRow()
	if r.kind == dHead {
		return r.init
	}
	if r.kind == dSec || r.kind == dPlace || r.kind == dSum {
		return nil
	}
	return r.b
}

func (t defogTab) selRow() (dRow, bool) {
	rows := t.rows()
	if len(rows) == 0 {
		return dRow{}, false
	}
	return rows[t.selIndex(rows)], true
}

// selected is the cursor's bead, nil on an Initiative or '▸ N closed' row.
func (t defogTab) selected() *bd.Bead {
	r, _ := t.selRow()
	return r.b
}

func (t defogTab) count() int { return t.g.defogBadge() }
func (t defogTab) actN() int  { return len(t.g.defogAct()) }

// enterWhy is why enter does nothing on r (graphData.rowEnterWhy).
func (t defogTab) enterWhy(r dRow) string {
	if r.kind == dFin {
		return finRefuse
	}
	return t.g.rowEnterWhy(r.b, r.leaf())
}

// bindings are the hints for the selected row (placement and drop rank: ADR 0229): enter only
// where it acts, then the shared x and +/- on bead rows.
func (t defogTab) bindings() []key.Binding {
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
	if r.foldable() {
		bs = append(bs, keyFold)
	}
	if t.open {
		bs = append(bs, keyShut, keyScroll)
	} else {
		bs = append(bs, keyDetail)
	}
	if r.b == nil {
		if r.kind == dHead && isOpenInitiative(r.init) {
			bs = append(bs, keyCancelKey)
		}
		return append(bs, keySection)
	}
	if r.b.Unclosed() {
		bs = append(bs, verbKey("p", pauseLabel(r.b)))
	}
	if l := t.g.xHint(r.b); l != "" {
		bs = append(bs, verbKey("x", l))
	}
	if t.g.xOffers(r.b) {
		bs = append(bs, xBinding(r.b))
	}
	bs = append(bs, keyTranscript)
	if r.b.Unclosed() {
		bs = append(bs, keyPrio)
	}
	return append(bs, keySection)
}

// extra is moving, then the keys the row handles but does not hint (enter's
// refusals, x and +/- answering with a status line): shown in ? alone.
func (t defogTab) extra() []key.Binding {
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

// defogKey routes the Defog tab's action keys through the shared paths; ok is false
// for any key the tab doesn't own (space, j/k and d stay with update).
func (m Model) defogKey(k tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	r, ok := m.tabAt(m.tab).(defogTab).selRow()
	if !ok {
		return m, nil, false
	}
	if key.Matches(k, keyFold) && r.foldable() {
		next, cmd := m.tabKey(k)
		nm := next.(Model)
		if err := nm.tabAt(nm.tab).(defogTab).save(); err != nil {
			nm.setStatus("folds won't survive a restart: " + err.Error())
		}
		return nm, cmd, true
	}
	if r.kind == dFin && (key.Matches(k, keyLaunch) || key.Matches(k, keyBg)) {
		m.setStatus(finRefuse)
		return m, nil, true
	}
	return m.queueRowKey(k, r.b, r.leaf())
}

// queueRowKey is the queue tabs' shared routing for a selected row's action keys:
// enter and b take rowEnterWhy/rowBgWhy's refusals, then enter launches or runs now
// and the rest go to the shared paths. leaf is whether the row is an open HITL leaf;
// ok is false for a key the row doesn't own.
func (m Model) queueRowKey(k tea.KeyMsg, b *bd.Bead, leaf bool) (tea.Model, tea.Cmd, bool) {
	g := m.gateData()
	switch {
	case key.Matches(k, keyLaunch):
		if why := g.rowEnterWhy(b, leaf); why != "" {
			m.setStatus(why)
			return m, nil, true
		}
		if !g.launches(b) {
			next, cmd := m.runNow(g, *b)
			return next, cmd, true
		}
		return m.actionKey(k)
	case key.Matches(k, keyBg): // enter's background twin, with enter's refusals
		if why := g.rowBgWhy(b, leaf); why != "" {
			m.setStatus(why)
			return m, nil, true
		}
		return m.actionKey(k)
	case b == nil && key.Matches(k, keyWithdrawKey) && isOpenInitiative(m.initRow()): // a header carries no bead; actionKey finds it
		return m.actionKey(k)
	case b == nil:
		return m, nil, false
	case key.Matches(k, keyPauseKey), key.Matches(k, keyDropKey), key.Matches(k, keyPrio), key.Matches(k, keyWithdrawKey):
		return m.actionKey(k)
	}
	return m, nil, false
}

func (t defogTab) update(k tea.KeyMsg) (component, tea.Cmd) {
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
		t.sel, t.off = rows[stepTo(rows, t.selIndex(rows), d, dRow.selectable)].key, 0 // the detail follows: back to the top
	case key.Matches(k, keySection):
		d := 1
		if s := k.String(); s == "K" || s == "ctrl+up" {
			d = -1
		}
		t.sel, t.off = rows[jumpTo(rows, t.selIndex(rows), d, func(r dRow) bool { return r.kind == dSec || r.noInit() })].key, 0
	case key.Matches(k, keyFold) && r.foldable():
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

// hidden are the beads a folded row hides: those under it in the fully expanded list.
func (t defogTab) hidden(full []dRow, r dRow) []bd.Bead {
	i := slices.IndexFunc(full, func(f dRow) bool { return f.key == r.key })
	return below(full, i, func(f dRow) int { return f.depth }, func(f dRow) *bd.Bead { return f.b })
}

// expanded is the flip set that opens every row, so blocker gutters and hidden never see a folded tree.
func (t defogTab) expanded() map[string]bool {
	return expandAll(t.g.defogList, func(f map[string]bool, r dRow) bool { return r.foldable() && collapsed(f, r) }, func(r dRow) string { return r.key })
}

// line is a row's left part and its right-edge suffix; gut is its blocker gutter,
// blockers the selection's open blockers.
func (t defogTab) line(r dRow, tagW int, gut, trail string, blockers map[string]bool) (string, string) {
	ind := strings.Repeat("  ", r.depth)
	const blank = "  " // the caret slot of a row that cannot fold
	fold := "▾ "
	if collapsed(t.flip, r) {
		fold = "▸ "
	}
	suffix := func() string { return strings.TrimLeft(t.g.rowSuffix(r.b), " ") }
	switch r.kind {
	case dSec:
		switch r.key {
		case liveKey: // the count in the live ●'s green
			return gut + ind + dimSt.Render(fold) + liveTitle(r.title), ""
		case actKey:
			return gut + ind + dimSt.Render(fold) + selMarker.Render(r.title), ""
		}
		return gut + ind + dimSt.Render(fold) + sectionSt.Render(r.title), ""
	case dPlace:
		if r.title != "" {
			return gut + ind + blank + dimSt.Render(r.title), ""
		}
		if r.key == liveNone {
			return gut + ind + blank + dimSt.Render(liveEmpty), ""
		}
		return gut + ind + blank + dimSt.Render("nothing waits on you"), ""
	case dHead:
		if r.noInit() { // a top-level section: the count, and the folded trailer
			return gut + ind + dimSt.Render(fold) + sectionSt.Render(fmt.Sprintf("%s (%d)", r.title, r.n)) + trail, ""
		}
		if r.init == nil {
			return gut + ind + dimSt.Render(fold) + dimSt.Render(r.title) + trail, ""
		}
		return gut + ind + dimSt.Render(fold) + t.g.sbrn(r.init, dimSt.Render(r.title), true, 0) + trail, ""
	case dLive:
		if r.b.Kind != contract.KindChart {
			break
		}
		return gut + ind + blank + t.g.sbrn(r.b, containerTitle(r.b, false, chartName(r.b.Title)), true, 0) + trail, suffix()
	case dCW, dACW:
		return gut + ind + blank + t.g.sbrn(r.b, containerTitle(r.b, false, chartName(r.b.Title)), true, 0) + sep() + dimSt.Render(t.g.chartWord(r.b).label()), suffix()
	case dFin:
		word, sha, _ := t.g.chartOutcome(r.b)
		if sha != "" {
			word += " " + sha8(sha)
		}
		return gut + ind + blank + t.g.sbrn(r.b, containerTitle(r.b, false, chartName(r.b.Title)), true, 0) + sep() + dimSt.Render(word+" · "+HumanAge(r.b.Closed)), suffix()
	case dChart:
		return gut + ind + fold + t.g.sbrn(r.b, containerTitle(r.b, false, chartName(r.b.Title)), true, 0) + trail, suffix()
	case dSum:
		return gut + ind + dimSt.Render(fmt.Sprintf("%s%d closed", fold, len(r.members))), ""
	}
	s := gut + ind + blank + t.g.sbrnLeaf(r.b, leafTitle(r.b, blockers[r.bk()]), tagW, blockers[r.bk()])
	if (r.kind == dAct || r.kind == dLive || r.kind == dACA) && r.chart != nil {
		s += sep() + dimSt.Render("in → "+chartName(r.chart.Title))
	}
	return s, suffix()
}

func (t defogTab) view(w, h int) string {
	rows := t.rows()
	tagW := 0 // leaf and AFK rows share one kind-badge column
	for _, r := range rows {
		if r.leaf() || r.kind == dChild || r.kind == dACA || r.kind == dLive {
			tagW = max(tagW, kindTagW(r.b))
		}
	}
	sel := t.selIndex(rows)
	b := t.selected()
	blockers := t.g.blockerSet(b)
	full := t.g.defogList(t.expanded())
	row := func(i int) (string, string) {
		r := rows[i]
		var hid []bd.Bead
		trail := ""
		if collapsed(t.flip, r) && r.foldable() && r.kind != dSec {
			hid = t.hidden(full, r)
			if r.kind != dSum {
				trail = t.g.trailer(hid, false, false)
			}
		}
		return t.line(r, tagW, blockerGutter(blockers, r.bk(), hid), trail, blockers)
	}
	shade := shadeSet(t.g.parentOf, rows, sel, full, dRow.shadeIn)
	lines, selRow := make([]string, len(rows)), sel
	ids := make([]string, len(rows))
	liveAt, chartsAt, treeAt, noInitAt := 0, 0, 0, 0 // the section headers that get a blank line above
	for i, r := range rows {
		ids[i], lines[i] = r.bk(), strconv.Itoa(i)
		switch r.key {
		case liveKey:
			liveAt = i
		case chartsKey:
			chartsAt = i
		case treeKey:
			treeAt = i
		case "i:":
			noInitAt = i
		}
	}
	lines, ids, sel = gapAt(lines, ids, sel, noInitAt) // a blank line above each section header
	lines, ids, sel = gapAt(lines, ids, sel, treeAt)
	lines, ids, sel = gapAt(lines, ids, sel, chartsAt)
	lines, ids, sel = gapAt(lines, ids, sel, liveAt)
	var footer func(off, lh int) []string
	if b != nil {
		footer = blockerFooter(t.g, t.g.openBlockers(b), ids, w)
	}
	return drawRows(lines, row, shade, sel, selRow, t.open, w, h, t.detailVP(), footer)
}

func (t defogTab) drawerOpen() bool { return t.open }
