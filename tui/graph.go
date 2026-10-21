package tui

// The Graph tab's tree (FR31): grouping, ordering, and folding logic
// (ADRs 0131, 0038 §2). There are no Inbox or Search arms and no regex
// listFilter (ADR 0143); the filter arrives in a later Build.

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/registry"
	"jayulabs.org/mugicrew/session"
)

// isHITL reports whether a kind is human-in-the-loop; the registry is the one
// AFK/HITL source.
func isHITL(k contract.Kind) bool {
	e, err := registry.Lookup(k)
	return err == nil && e.Mode == registry.HITL
}

// unparentedSel is the selection key of the Unparented group header, which has no
// backing bead. Non-printable so it can't collide with a bead id.
const unparentedSel = "\x00unparented"

// group is one master-pane group: a Chart container (head) and its member defog
// beads, an Initiative (head) and its member chains, or the head-less Unparented
// group.
type group struct {
	key     string   // head bead id, or "" for the unparented group (its collapse key)
	head    *bd.Bead // the Chart container; nil for the unparented group
	name    string
	members []bd.Bead
	// nested is an Initiative's nested Chart and Checkpoint heads' own members, by
	// head id; those heads sit in members, chain by chain.
	nested map[string][]bd.Bead
	// ctx is the filtered tree's context rows by selection key: ancestors kept only
	// because a descendant matched. Dimmed and uncounted; nil when unfiltered.
	ctx map[string]bool
}

// beads is every bead under g's head, in render order.
func (g group) beads() []bd.Bead {
	var bs []bd.Bead
	for _, b := range g.members {
		bs = append(append(bs, b), g.nested[b.ID]...)
	}
	return bs
}

// buildGraph groups the whole graph by Chart. Only kind-bearing beads are shown;
// containers become group heads; each other bead sits under its parent container,
// or in the Unparented group. An Initiative's member Chart and Checkpoint heads
// nest under it with their own members, its member chains in phaseOrder: depth 3.
func buildGraph(beads []bd.Bead) []group {
	var order []string
	heads := map[string]*bd.Bead{}
	for i := range beads {
		b := &beads[i]
		if b.Kind == "" || !b.Container {
			continue
		}
		if _, ok := heads[b.ID]; !ok {
			heads[b.ID] = b
			order = append(order, b.ID)
		}
	}
	isInitiative := func(id string) bool { h, ok := heads[id]; return ok && h.Kind == contract.KindInitiative }
	members := map[string][]bd.Bead{}
	var unparented []bd.Bead
	for _, b := range beads {
		if b.Kind == "" || b.Container && (b.Kind == contract.KindInitiative || !isInitiative(b.Parent)) {
			continue // a top-level head
		}
		if _, ok := heads[b.Parent]; ok {
			members[b.Parent] = append(members[b.Parent], b)
		} else {
			unparented = append(unparented, b)
		}
	}
	order = slices.DeleteFunc(order, func(id string) bool {
		return heads[id].Kind != contract.KindInitiative && isInitiative(heads[id].Parent)
	})
	slices.SortStableFunc(order, func(a, b string) int { return beadCmp(*heads[a], *heads[b]) })
	var groups []group
	if len(unparented) > 0 {
		slices.SortStableFunc(unparented, beadCmp)
		groups = append(groups, group{key: "", name: "Unparented", members: unparented})
	}
	for _, id := range order {
		h := heads[id]
		g := group{key: id, head: h, name: chartName(h.Title), members: members[id]}
		if h.Kind == contract.KindInitiative {
			g.members, g.nested = slices.Concat(phaseOrder(g.members)...), map[string][]bd.Bead{}
			for _, c := range g.members {
				if c.Container {
					g.nested[c.ID] = slices.SortedStableFunc(slices.Values(members[c.ID]), beadCmp)
				}
			}
		} else {
			slices.SortStableFunc(g.members, beadCmp)
		}
		groups = append(groups, g)
	}
	return groups
}

// phaseOrder splits an Initiative's direct members into member chains — linked by
// caused-by, each in lineage order — and orders the chains by phase: topological
// over member blocks edges, beadCmp of each chain's best bead breaking ties. A
// blocks cycle falls back to beadCmp. A phase may hold several members (ADR 0252):
// each member chain is one entry here, and members joined by no edge sort side by side.
// ponytail: O(chains²) selection; members per Initiative are few.
func phaseOrder(ms []bd.Bead) [][]bd.Bead {
	byID := map[string]bd.Bead{}
	for _, b := range ms {
		byID[b.ID] = b
	}
	root := func(b bd.Bead) string {
		for seen := map[string]bool{}; !seen[b.ID]; {
			seen[b.ID] = true
			p, ok := byID[b.CausedBy]
			if !ok {
				break
			}
			b = p
		}
		return b.ID
	}
	var keys []string
	chains := map[string][]bd.Bead{}
	chainOf := map[string]string{}
	for _, b := range ms {
		k := root(b)
		if _, ok := chains[k]; !ok {
			keys = append(keys, k)
		}
		chains[k], chainOf[b.ID] = append(chains[k], b), k
	}
	depth := func(b bd.Bead) int {
		n := 0
		for k := chainOf[b.ID]; b.ID != k && n < len(ms); n++ {
			b = byID[b.CausedBy]
		}
		return n
	}
	best := map[string]bd.Bead{}
	after := map[string]map[string]bool{} // chain → the chains it is ordered after
	for _, k := range keys {
		slices.SortStableFunc(chains[k], func(a, b bd.Bead) int { return cmp.Compare(depth(a), depth(b)) })
		best[k] = slices.MinFunc(chains[k], beadCmp)
		after[k] = map[string]bool{}
		for _, b := range chains[k] {
			for _, id := range b.BlockedBy {
				if o, ok := chainOf[id]; ok && o != k {
					after[k][o] = true
				}
			}
		}
	}
	var out [][]bd.Bead
	done := map[string]bool{}
	waiting := func(k string) bool {
		for o := range after[k] {
			if !done[o] {
				return true
			}
		}
		return false
	}
	for len(out) < len(keys) {
		next := ""
		for _, ready := range []bool{true, false} { // a cycle leaves none ready: take the best anyway
			for _, k := range keys {
				if done[k] || ready && waiting(k) {
					continue
				}
				if next == "" || beadCmp(best[k], best[next]) < 0 {
					next = k
				}
			}
			if next != "" {
				break
			}
		}
		done[next] = true
		out = append(out, chains[next])
	}
	return out
}

// beadCmp is the list order: open before closed, then priority ascending (P0
// first), then newest-created first.
func beadCmp(a, b bd.Bead) int {
	return cmp.Or(
		cmp.Compare(boolInt(a.Status == "closed"), boolInt(b.Status == "closed")),
		cmp.Compare(a.Priority, b.Priority),
		b.Created.Compare(a.Created),
	)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// chartName shortens a Chart title for the group header: anything after an em-dash
// is detail.
func chartName(title string) string {
	if i := strings.Index(title, " — "); i >= 0 {
		return title[:i]
	}
	return title
}

// row is one navigable line in the master pane: a group header (collapsible; a
// Chart head also carries a bead) or a member leaf.
type row struct {
	sel         string // selection key: bead id, or unparentedSel
	beadID      string // the bead this row selects; "" for the head-less unparented header
	collapseKey string // group key this row toggles; "" for a leaf (not collapsible)
	header      bool
	last        bool   // a member only: last of its group (└ vs ├ tree connector)
	depth       int    // 0 a top-level head, 1 a member, 2 a nested head's member under an Initiative
	rail        string // depth 2 only: the nested head's tree rail ("│ ", or "  " under its last)
	context     bool   // a filter's ancestor row: dimmed, not a match
	kids        bool   // a header only: has at least one visible child (else no caret)
}

// navRows flattens the groups into the visible master rows in render order; a
// collapsed group's children are skipped at either depth.
func navRows(groups []group, collapsed map[string]bool) []row {
	var rows []row
	for _, g := range groups {
		if g.head != nil {
			rows = append(rows, row{sel: g.head.ID, beadID: g.head.ID, collapseKey: g.key, header: true, kids: len(g.members) > 0, context: g.ctx[g.head.ID]})
		} else {
			rows = append(rows, row{sel: unparentedSel, collapseKey: g.key, header: true, kids: len(g.members) > 0, context: g.ctx[unparentedSel]})
		}
		if collapsed[g.key] {
			continue
		}
		for i, m := range g.members {
			r := row{sel: m.ID, beadID: m.ID, last: i == len(g.members)-1, depth: 1, context: g.ctx[m.ID]}
			kids, nests := g.nested[m.ID]
			if nests {
				r.header, r.collapseKey, r.kids = true, m.ID, len(kids) > 0
			}
			rows = append(rows, r)
			if !nests || collapsed[m.ID] {
				continue
			}
			rail := "│ "
			if r.last {
				rail = "  "
			}
			for j, k := range kids {
				rows = append(rows, row{sel: k.ID, beadID: k.ID, last: j == len(kids)-1, depth: 2, rail: rail})
			}
		}
	}
	return rows
}

// isClosedRoot reports whether a bead has no parent and is no longer open: finished
// top-level work, whose group opens folded.
func isClosedRoot(b bd.Bead) bool { return b.Parent == "" && !b.Open() }

// defaultFolds is the live default over the unfiltered tree: Unparented and each
// closed-root group head folded, nested chain heads open (ADR 0176 §2).
func defaultFolds(groups []group) map[string]bool {
	def := map[string]bool{"": true}
	for _, g := range groups {
		if g.head != nil && isClosedRoot(*g.head) {
			def[g.key] = true
		}
	}
	return def
}

// pivotLeads maps each gate (pivotGate) to its lead: the oldest open pivot.
func pivotLeads(beads []bd.Bead) map[string]bd.Bead {
	leads := map[string]bd.Bead{}
	for _, b := range beads {
		if b.Kind == contract.KindPivot && b.Open() {
			k := pivotGate(beads, b.ID)
			if l, ok := leads[k]; !ok || b.Created.Before(l.Created) {
				leads[k] = b
			}
		}
	}
	return leads
}

// pivotGate is the Checkpoint a pivot blocks, or the pivot's own id before the
// raise tail has gated it.
func pivotGate(beads []bd.Bead, pivotID string) string {
	for _, b := range beads {
		if b.Kind == contract.KindCheckpoint && slices.Contains(b.BlockedBy, pivotID) {
			return b.ID
		}
	}
	return pivotID
}

// otherOpenPivots is every open pivot gating the same Checkpoint as p, bar p,
// oldest first.
func otherOpenPivots(beads []bd.Bead, p bd.Bead) []bd.Bead {
	gate := pivotGate(beads, p.ID)
	var out []bd.Bead
	for _, b := range beads {
		if b.Kind == contract.KindPivot && b.Open() && b.ID != p.ID && pivotGate(beads, b.ID) == gate {
			out = append(out, b)
		}
	}
	slices.SortStableFunc(out, func(a, b bd.Bead) int { return a.Created.Compare(b.Created) })
	return out
}

func indexRow(rows []row, sel string) int {
	return slices.IndexFunc(rows, func(r row) bool { return r.sel == sel })
}

// clampWrap wraps an index into [0,n): j past the end returns to the top.
func clampWrap(i, n int) int {
	if n == 0 {
		return 0
	}
	return ((i % n) + n) % n
}

// stepTo is the index one row from i in direction d (±1) that ok accepts, wrapping; i when none does.
func stepTo[R any](rows []R, i, d int, ok func(R) bool) int {
	for j := i; ; {
		if j = clampWrap(j+d, len(rows)); j == i || ok(rows[j]) {
			return j
		}
	}
}

// jumpTo is the nearest row from i in direction d (±1) that is a section header; i at the
// end, with no wrap.
func jumpTo[R any](rows []R, i, d int, head func(R) bool) int {
	for j := i + d; j >= 0 && j < len(rows); j += d {
		if head(rows[j]) {
			return j
		}
	}
	return i
}

// graphData is the slice of the shell's live data the Graph reads: the shell hands
// it over before every update and view, so the tab never holds a stale copy.
type graphData struct {
	beads     []bd.Bead
	live      map[string]bool
	sessions  map[string]session.Info
	efforts   map[string]bool
	cpTips    map[string]string // Checkpoint id → checkpoint/<id> tip; unknown counts as not at the tip
	landEv    map[string]string // bead id → msg of its latest land.*, gate.*, effort.failed or effort.paused event
	launching map[string]bool
	launched  map[string]time.Time // launches that succeeded, held in Live until a later snapshot shows it alive, or one launchHoldMax on
	pending   map[string]string
	crew      map[string]contract.Crewmate
	idx       *snapIndex                  // lookups over beads; nil falls back to a linear scan
	comments  map[commentKey]commentEntry // fetched comments; a miss renders as loading…
	rollups   map[string]rollupEntry      // fetched ROLLUP totals; a miss renders as loading…
	extRef    bool                        // Enforce external ID is on: an open Initiative with no ref shows "no ref"
	landName  string                      // the Workspace land_strategy, cached by the slow tier; "" when unread or unusable
	landErr   error                       // why it is unusable: the Merge Requests section shows it
	w, h      int                         // the body's size
}

// snapIndex is the immutable lookup index over one bead snapshot, built once per refresh
// and never mutated. byID is a bead's position in the slice; kids maps a parent ID to its
// children's positions, in slice order. blockers maps a bead ID to the positions of the beads
// whose BlockedBy names it, in slice order. dups holds the later positions of an ID that more
// than one bead carries (byID keeps the first).
type snapIndex struct {
	byID     map[string]int
	kids     map[string][]int
	blockers map[string][]int
	dups     map[string][]int
}

func newSnapIndex(beads []bd.Bead) *snapIndex {
	x := &snapIndex{byID: make(map[string]int, len(beads)), kids: map[string][]int{}, blockers: map[string][]int{}, dups: map[string][]int{}}
	for i, b := range beads {
		x.kids[b.Parent] = append(x.kids[b.Parent], i)
		if _, dup := x.byID[b.ID]; !dup { // the first wins, as the linear scan does
			x.byID[b.ID] = i
		} else {
			x.dups[b.ID] = append(x.dups[b.ID], i)
		}
		for j, id := range b.BlockedBy {
			if !slices.Contains(b.BlockedBy[:j], id) { // one position per bead, however often it names id
				x.blockers[id] = append(x.blockers[id], i)
			}
		}
	}
	return x
}

// beadIndex is bead id's index in g.beads, -1 if absent: O(1) through the snapshot index.
func (g graphData) beadIndex(id string) int {
	if g.idx == nil {
		return beadIndex(g.beads, id)
	}
	if i, ok := g.idx.byID[id]; ok {
		return i
	}
	return -1
}

// children is the positions in g.beads of id's children, in snapshot order: through the
// snapshot index's kids map, else by a linear scan.
func (g graphData) children(id string) []int {
	if g.idx != nil {
		return g.idx.kids[id]
	}
	var out []int
	for i, b := range g.beads {
		if b.Parent == id {
			out = append(out, i)
		}
	}
	return out
}

// graphTab's state: the tree, its folds and selection, and the detail pane.
type graphTab struct {
	graphData
	groups    []group
	noDot     bool            // the Initiatives tab borrows the detail but is not a live-tracking view
	flip      map[string]bool // the absolute fold state of the keys the human touched (FR6); saved only where it differs from def
	def       map[string]bool // the live default, worked out in rebuild
	path      string          // graph-folds.json; "" when the common dir can't be resolved
	qpath     string          // graph-filter.json, likewise
	sel       string          // the selected row's key; follows its bead across refreshes
	open      bool            // d: the detail pane is open
	off       int             // the detail viewport's scroll offset
	q         query           // the kept filter
	scopedTo  query           // the query a scoped reveal set; while q still equals it, esc returns to the baseline (zero: none)
	typing    bool            // the / input is open
	visit     bool            // q is a Recents fallback scope: never saved, esc returns to the baseline (ADR 0176 §4)
	edit      line
	qfold     map[string]bool // the folds while filtering; flip is untouched, so clearing restores it
	qfoldFor  query           // the query qfold belongs to; a new query starts with every group open (FR40)
	matched   int             // beads the filter matches, and the unfiltered total
	total     int
	liveN     map[string]int // "N live" per foldable header by collapse key, off the unfiltered tree; set in rebuild
	searching bool           // a fetch for the current generation is in flight (ADR 0232 §6)
	entry     bool           // searching was set by Graph entry awaiting its load, not by a fetch
	want      string         // the cursor bead lost while searching: the cursor returns to it when it arrives
	memo      *graphMemo     // visible() and what rows derive from it; replaced in rebuild, never mutated; nil until then
}

// graphMemo is one rebuild's view of visible(): each container's (closed, total)
// member counts and the member beads per parent.
type graphMemo struct {
	counts  map[string][2]int
	members map[string][]bd.Bead // children with a Kind, by parent, in visible order
}

func newMemo(vis []bd.Bead) *graphMemo {
	m := &graphMemo{counts: map[string][2]int{}, members: map[string][]bd.Bead{}}
	for _, b := range vis {
		if b.Parent == "" || b.Kind == "" {
			continue
		}
		m.members[b.Parent] = append(m.members[b.Parent], b)
		if !b.Container {
			c := m.counts[b.Parent]
			c[1]++
			if b.Status == "closed" {
				c[0]++
			}
			m.counts[b.Parent] = c
		}
	}
	return m
}

// memoOf is the memo, built on the spot for a graphTab that never rebuilt. That
// fallback is for tests that set beads by hand: every shipped path rebuilds first
// (the loadedMsg fold), and a render that skips it would be O(rows x beads) again.
func (g graphTab) memoOf() *graphMemo {
	if g.memo != nil {
		return g.memo
	}
	return newMemo(g.visible())
}

// newGraph is a Graph tab with the folds the human saved on this machine (ADR 0176).
func newGraph() graphTab {
	path := foldsPath("graph-folds.json")
	flip := loadFlatFolds(path)
	if flip == nil {
		flip = map[string]bool{}
	}
	qpath := foldsPath(queryFile)
	return graphTab{q: loadQuery(qpath), flip: flip, path: path, qpath: qpath, qfold: map[string]bool{}}
}

// save writes the touched states that differ from the live default, minus those of
// beads gone from the snapshot; "" is never pruned and a closed bead keeps its key.
// The touched state still applies this session.
func (g graphTab) save() error {
	saved := map[string]bool{}
	for k, v := range g.flip {
		if v != g.def[k] && (k == "" || g.beadIndex(k) >= 0) {
			saved[k] = v
		}
	}
	return saveFlatFolds(g.path, saved)
}

// query is the filter now: the / input's live text while typing.
func (g graphTab) query() query {
	q := g.q
	if g.typing {
		q.words = strings.TrimSpace(g.edit.text)
	}
	return q
}

// folds are the collapse flags the tree shows: while filtering every group is open
// but for the ones space folded since (ADR 0143 §4).
func (g graphTab) folds() map[string]bool {
	if g.query().active() {
		return g.qfold
	}
	eff := maps.Clone(g.def)
	if eff == nil {
		eff = map[string]bool{}
	}
	maps.Copy(eff, g.flip)
	return eff
}

func (g graphTab) rows() []row { return navRows(g.groups, g.folds()) }

// rebuild regroups the beads and reworks the default folds over the unfiltered
// graph, so a closed root stays folded even while the status facet hides it.
func (g graphTab) rebuild() graphTab {
	vis := g.visible() // the one call per rebuild
	g.memo = newMemo(vis)
	all := buildGraph(vis)
	g.def = defaultFolds(all)
	old := indexRow(g.rows(), g.sel)
	q := g.query()
	g.total, g.liveN = countBeads(all), g.liveCounts(all)
	g.groups, g.matched = filterGroups(all, q, g.sessions) // the baseline filters too (ADR 0175)
	if !q.active() {
		g.matched = 0
		g.qfold, g.qfoldFor = map[string]bool{}, baseline
	} else if q != g.qfoldFor {
		g.qfold, g.qfoldFor = map[string]bool{}, q
	}
	return g.fixSelection(old)
}

// fixSelection re-anchors the selection after a refresh or a fold: the same bead
// by id, else the row now at its old index, clamped (ADR 0076 §2); old < 0 (no
// prior selection) takes the first row.
func (g graphTab) fixSelection(old int) graphTab {
	rows := g.rows()
	if g.searching && g.want == "" && g.sel != "" && indexRow(rows, g.sel) < 0 {
		g.want = g.sel // lost to a fetch in flight: park, and return when it arrives
	}
	if i := indexRow(rows, g.want); g.want != "" && i >= 0 {
		g.sel, g.want = g.want, ""
	}
	switch {
	case len(rows) == 0:
		g.sel = ""
	case indexRow(rows, g.sel) < 0:
		g.sel = rows[min(max(old, 0), len(rows)-1)].sel
	}
	return g
}

// setFolds stores f, the effective folds, as what the tree shows: while filtering as
// qfold, else as touched states for the keys it changes.
func (g graphTab) setFolds(f map[string]bool) graphTab {
	if g.query().active() {
		g.qfold = f
	} else {
		cur := g.folds()
		g.flip = maps.Clone(g.flip)
		for k, v := range f {
			if v != cur[k] {
				g.flip[k] = v
			}
		}
		for k, v := range cur {
			if v && !f[k] {
				g.flip[k] = false
			}
		}
	}
	return g
}

// visible is the beads the Graph shows: Drafts (status deferred, ADR 0059 §2) and
// their subtrees are hidden unless the status facet is deferred. Display-only:
// gates read g.beads and keep counting a Draft as open.
func (g graphTab) visible() []bd.Bead {
	if g.query().status == "deferred" {
		return g.beads
	}
	parent, draft := make(map[string]string, len(g.beads)), map[string]bool{}
	for _, b := range g.beads {
		parent[b.ID], draft[b.ID] = b.Parent, b.Status == "deferred"
	}
	return slices.DeleteFunc(slices.Clone(g.beads), func(b bd.Bead) bool {
		for id, n := b.ID, 0; id != "" && n <= len(parent); id, n = parent[id], n+1 {
			if draft[id] {
				return true
			}
		}
		return false
	})
}

// selected is the selected row's bead, nil for the Unparented header or no rows.
func (g graphTab) selected() *bd.Bead {
	if g.sel == "" || g.sel == unparentedSel {
		return nil
	}
	if i := g.beadIndex(g.sel); i >= 0 {
		return &g.beads[i]
	}
	return nil
}

// beadIndex is bead id's index in beads, -1 if absent.
func beadIndex(beads []bd.Bead, id string) int {
	return slices.IndexFunc(beads, func(b bd.Bead) bool { return b.ID == id })
}

func (g graphData) beadByID(id string) *bd.Bead {
	if i := g.beadIndex(id); i >= 0 {
		return &g.beads[i]
	}
	return &bd.Bead{ID: id, Title: id}
}

// groupOf is the keys of the groups bead id heads or sits in: its group's, then
// under an Initiative the nested head's it sits under, if any.
func groupOf(groups []group, id string) ([]string, bool) {
	for _, g := range groups {
		if g.key == id || slices.ContainsFunc(g.members, func(b bd.Bead) bool { return b.ID == id }) {
			return []string{g.key}, true
		}
		for h, kids := range g.nested {
			if slices.ContainsFunc(kids, func(b bd.Bead) bool { return b.ID == id }) {
				return []string{g.key, h}, true
			}
		}
	}
	return nil, false
}

// reveal is G from Logs or History (ADR 0074 §2–3, ADR 0143 §8): select bead id. A
// bead the query shows as a match keeps the query and unfolds only its group. One it
// hides, or shows only as a context row, scopes the query to {words: id, status any}
// (Spec "G reveal scopes Graph to the revealed bead"); the query's expand-all opens
// its groups, so no fold flips. ok is false when the graph does not hold the bead;
// note is a status for what it changed.
func (g graphTab) reveal(id string) (_ graphTab, ok bool, note string) {
	if _, ok := groupOf(g.groups, id); ok && !slices.ContainsFunc(g.groups, func(gr group) bool { return gr.ctx[id] }) {
		keys, _ := groupOf(g.groups, id)
		f := maps.Clone(g.folds())
		for _, k := range keys {
			f[k] = false
		}
		g = g.setFolds(f)
		g.sel, g.off, g.scopedTo = id, 0, query{}
		return g.fixSelection(0), true, ""
	}
	scoped := g
	scoped.q, scoped.typing = query{words: id, status: statusAny}, false
	scoped = scoped.rebuild()
	if _, ok := groupOf(scoped.groups, id); !ok {
		return g, false, ""
	}
	scoped.sel, scoped.off, scoped.scopedTo = id, 0, scoped.q
	return scoped.fixSelection(0), true, "scoped Graph to " + id
}
