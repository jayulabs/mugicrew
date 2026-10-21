package tui

// The Initiative focus view (Spec mc-u0fg, ADRs 0173, 0174): i on a row of Graph,
// Ideate, Defog or Execute swaps the content area for one Initiative's members under
// a back bar, in three by-kind sections. It is a drilldown, not a tab: its state lives
// on the shell Model next to the untouched origin tab, so esc restores the tab exactly.
// Rows are rebuilt from the full snapshot on every render and key, then filtered to the
// Initiative, so the shared gates keep seeing blockers and pauses outside it.

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// focus is the drilldown's state: the Initiative, the cursor's row key, the folds
// flipped from their default and the detail drawer. id "" is off.
type focus struct {
	id   string
	sel  string
	flip map[string]bool
	open bool
	off  int // the drawer's scroll offset
}

var (
	keyBack  = verbKey("esc", "back")
	keyShutD = verbKey("d", "close")
)

// Row kinds.
const (
	fHead  = iota // the Initiative's own row
	fSec          // Ideate, Defog or Execute
	fBead         // a member bead
	fSum          // '▸ N closed'
	fPlace        // an empty section's placeholder
	fGap          // the blank line before a section; the cursor skips it
)

// Sections, in display order.
const (
	secIdeate = iota
	secDefog
	secExec
)

var (
	secNames  = [...]string{"Ideate", "Defog", "Execute"}
	secEmpty  = [...]string{"no Signals", "no Charts", "no Specs or Checkpoints"}
	noFocusIn = "i: this row is not in an Initiative"
)

// fRow is one focus line. key is the fold and selection key: "head", "sec:<Name>", a
// bead id, or "<parent key>/closed".
type fRow struct {
	kind    int
	key     string
	sec     int
	b       *bd.Bead
	members []*bd.Bead // '▸ N closed'
	depth   int
	n       int      // a section's open count
	leaf    bool     // an open HITL leaf in a queue section: blocked and paused refuse
	cp      *bd.Bead // Execute: the Checkpoint a member sits under or a side row waits on
	rel     string   // Execute: member, gates, before land, after land
	side    bool     // Execute: a Task or Pivot outside any Checkpoint
}

// fSection is what one section lists: its open rows, how many beads head them, and
// the closed beads that go under its '▸ N closed'.
type fSection struct {
	rows   []fRow
	open   int
	closed []*bd.Bead
}

// folded: '▸ N closed' rows and Checkpoints start folded, everything else open; flip
// holds the keys flipped from that.
func (f focus) folded(r fRow) bool {
	return f.flip[r.key] != (r.kind == fSum || (r.kind == fBead && r.b.Kind == contract.KindCheckpoint))
}

// foldable is a row space toggles: a section, a '▸ N closed' or an open container.
func (r fRow) foldable() bool {
	return r.kind == fSec || r.kind == fSum || (r.kind == fBead && (r.b.Container || r.b.Kind == contract.KindCheckpoint) && r.b.Unclosed())
}

// focusRows builds the Initiative's rows; nil when it is not in the snapshot.
func (m Model) focusRows() []fRow {
	g := m.gateData()
	i := g.beadIndex(m.focus.id)
	if i < 0 {
		return nil
	}
	rows := []fRow{{kind: fHead, key: "head", b: &g.beads[i]}}
	for sec, body := range [...]func(graphData) fSection{m.focus.ideateSection, m.focus.defogSection, m.focus.execSection} {
		s := body(g)
		rows = append(rows, fRow{kind: fGap, key: "gap:" + secNames[sec]})
		h := fRow{kind: fSec, key: "sec:" + secNames[sec], sec: sec, n: s.open}
		rows = append(rows, h)
		switch {
		case m.focus.folded(h):
		case len(s.rows) == 0 && len(s.closed) == 0:
			rows = append(rows, fRow{kind: fPlace, key: h.key + "/none", sec: sec, depth: 1})
		default:
			rows = append(rows, s.rows...)
			rows = append(rows, m.focus.foldRows(sec, 1, h.key, s.closed)...)
		}
	}
	return rows
}

// foldRows is a '▸ N closed' row under parent and, unfolded, its beads; nil for none.
func (f focus) foldRows(sec, depth int, parent string, closed []*bd.Bead) []fRow {
	if len(closed) == 0 {
		return nil
	}
	s := fRow{kind: fSum, key: parent + "/closed", sec: sec, members: closed, depth: depth}
	rows := []fRow{s}
	if !f.folded(s) {
		for _, c := range closed {
			rows = append(rows, fRow{kind: fBead, key: c.ID, sec: sec, b: c, depth: depth + 1})
		}
	}
	return rows
}

// members are the Initiative's non-Draft direct children of the given kinds, open
// ones then closed ones, each in beadCmp order.
func (f focus) members(g graphData, kinds ...contract.Kind) (open, closed []*bd.Bead) {
	for i, b := range g.beads {
		if b.Parent != f.id || b.Event || b.Status == "deferred" || !slices.Contains(kinds, b.Kind) {
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

// ideateSection lists every member Signal, P3/P4 and stranded in-triage ones too:
// not ideaList, which routes them to the Backlog or drops them.
func (f focus) ideateSection(g graphData) (s fSection) {
	open, closed := f.members(g, contract.KindSignal)
	for _, b := range open {
		s.rows = append(s.rows, fRow{kind: fBead, key: b.ID, sec: secIdeate, b: b, depth: 1})
	}
	s.open, s.closed = len(open), closed
	return
}

// defogSection lists member Charts, chart:defogged ones that have not landed too, each
// with the Defog tab's kids: HITL leaves, AFK children, then its '▸ N closed'.
func (f focus) defogSection(g graphData) (s fSection) {
	open, closed := f.members(g, contract.KindChart)
	for _, c := range open {
		s.rows = append(s.rows, fRow{kind: fBead, key: c.ID, sec: secDefog, b: c, depth: 1})
		if f.folded(s.rows[len(s.rows)-1]) {
			continue
		}
		hitl, afk, done := chartKids(g.beads, c)
		for _, l := range hitl {
			s.rows = append(s.rows, fRow{kind: fBead, key: l.ID, sec: secDefog, b: l, depth: 2, leaf: true})
		}
		for _, a := range afk {
			s.rows = append(s.rows, fRow{kind: fBead, key: a.ID, sec: secDefog, b: a, depth: 2})
		}
		s.rows = append(s.rows, f.foldRows(secDefog, 2, c.ID, done)...)
	}
	s.open, s.closed = len(open), closed
	return
}

// defogRow is r as the Defog tab's row, for its line, detail and hints.
func (r fRow) defogRow() dRow {
	d := dRow{kind: dChild, key: r.key, b: r.b, depth: r.depth, afk: true}
	switch {
	case r.kind == fSum:
		d.kind, d.members = dSum, r.members
	case r.leaf:
		d.kind = dLeaf
	case r.b.Kind == contract.KindChart && r.b.Unclosed():
		d.kind = dChart
	}
	return d
}

// execSection lists the open member Checkpoints (unfolded: cpKids, then their closed
// fold), the Specs, then the side Tasks and Pivots that gate or wait on any Checkpoint
// and belong here by the shared initiativeOf rule; closed Checkpoints and Specs fold.
func (f focus) execSection(g graphData) (s fSection) {
	cps, closedCPs := f.members(g, contract.KindCheckpoint)
	specs, closedSpecs := f.members(g, contract.KindSpec)
	for _, cp := range cps {
		s.rows = append(s.rows, fRow{kind: fBead, key: cp.ID, sec: secExec, b: cp, cp: cp, depth: 1})
		if f.folded(s.rows[len(s.rows)-1]) {
			continue
		}
		open, closed := g.cpKids(cp)
		for _, m := range open {
			s.rows = append(s.rows, fRow{kind: fBead, key: m.ID, sec: secExec, b: m, cp: cp, rel: "member", leaf: isHITL(m.Kind) && !m.Container, depth: 2})
		}
		rs := f.foldRows(secExec, 2, cp.ID, closed)
		for i := range rs {
			rs[i].cp = cp
			if rs[i].kind == fBead {
				rs[i].rel = "member"
			}
		}
		s.rows = append(s.rows, rs...)
	}
	for _, b := range specs {
		s.rows = append(s.rows, fRow{kind: fBead, key: b.ID, sec: secExec, b: b, depth: 1})
	}
	allCPs, _ := execListed(g.beads)
	var sides []fRow
	seen := map[string]bool{}
	for _, cp := range allCPs {
		ws, rels := g.waiters(cp)
		for i, w := range ws {
			if seen[w.ID] {
				continue
			}
			seen[w.ID] = true
			if g.initiativeOf(w, cp) == f.id {
				sides = append(sides, fRow{kind: fBead, key: "side:" + w.ID, sec: secExec, b: w, cp: cp, rel: rels[i], leaf: isHITL(w.Kind) && !w.Container, side: true, depth: 1})
			}
		}
	}
	slices.SortStableFunc(sides, func(a, b fRow) int { return beadCmp(*a.b, *b.b) })
	s.rows = append(s.rows, sides...)
	s.open = len(cps) + len(specs) + len(sides)
	s.closed = append(closedCPs, closedSpecs...)
	return
}

// eRow is the Execute tab's row for r, so the focus renders and describes its Execute
// rows with Execute's own builders.
func (r fRow) eRow() eRow {
	e := eRow{key: r.key, b: r.b, cp: r.cp, rel: r.rel, members: r.members, depth: r.depth}
	switch {
	case r.kind == fSum:
		e.kind = eSum
	case r.b.Status == "closed":
		e.kind = eClosed
	case r.side:
		e.kind = eSide
	case r.b.Kind == contract.KindCheckpoint:
		e.kind = eCP
	case r.b.Kind == contract.KindSpec:
		e.kind = eSpec
	default:
		e.kind = eMem
	}
	return e
}

// index is the cursor's row index; a key that left the list falls back to the header.
func (f focus) index(rows []fRow) int {
	return max(slices.IndexFunc(rows, func(r fRow) bool { return r.key == f.sel }), 0)
}

// focusSel is the cursor's row; false when the Initiative left the snapshot.
func (m Model) focusSel() (fRow, bool) {
	rows := m.focusRows()
	if len(rows) == 0 {
		return fRow{}, false
	}
	return rows[m.focus.index(rows)], true
}

// focusBead is the bead the cursor row stands for (the Initiative on its header),
// nil on a section, fold or placeholder row.
func (m Model) focusBead() *bd.Bead {
	r, _ := m.focusSel()
	return r.b
}

// focusKeys is the current row keys, kept across a refresh by focusSettle.
func (m Model) focusKeys() (ks []string) {
	for _, r := range m.focusRows() {
		ks = append(ks, r.key)
	}
	return
}

// focusSettle keeps the cursor on its row key across a refresh; if the row is gone it
// moves to the nearest row above that is still listed. old is focusKeys before the refresh.
func (m Model) focusSettle(old []string) Model {
	if m.focus.id == "" {
		return m
	}
	now := m.focusKeys()
	if slices.Contains(now, m.focus.sel) {
		return m
	}
	for i := slices.Index(old, m.focus.sel) - 1; i >= 0; i-- {
		if k := old[i]; !strings.HasPrefix(k, "gap:") && slices.Contains(now, k) {
			m.focus.sel = k
			return m
		}
	}
	m.focus.sel = "head"
	return m
}

// resolveInitiative is the Initiative the active tab's cursor row sits in: a header is
// its own, an Execute row under or waiting on a Checkpoint takes the shared
// initiativeOf rule, any other row walks Parent up to the first Initiative. Nil for none.
func (m Model) resolveInitiative() *bd.Bead {
	g := m.gateData()
	byID := func(id string) *bd.Bead {
		if i := g.beadIndex(id); i >= 0 {
			return &g.beads[i]
		}
		return nil
	}
	var b *bd.Bead
	switch t := m.tabAt(m.tab).(type) {
	case ideaTab:
		c := t.cursor()
		if c.init != nil { // a header, or a Live/Actionable Initiative row
			return c.init
		}
		b = c.b
	case defogTab:
		r, _ := t.selRow()
		if r.kind == dHead {
			return r.init
		}
		if b = r.b; b == nil {
			b = r.chart
		}
	case execTab:
		r, _ := t.selRow()
		if r.kind == eSec {
			return r.init
		}
		if b = r.b; b == nil {
			b = r.cp
		}
		if b != nil && r.cp != nil {
			return byID(g.initiativeOf(b, r.cp))
		}
	default:
		b = m.selection()
	}
	for n := 0; b != nil && n < 8; n++ { // the guard stops a Parent cycle
		if b.Kind == contract.KindInitiative {
			return b
		}
		b = byID(b.Parent)
	}
	return nil
}

// focusOpen is g f and Initiatives' enter: open the cursor row's Initiative fresh, or refuse.
func (m Model) focusOpen() (tea.Model, tea.Cmd) {
	init := m.resolveInitiative()
	if init == nil {
		m.setStatus(noFocusIn)
		return m, nil
	}
	m.focus = focus{id: init.ID, sel: "head", flip: map[string]bool{}}
	m.setStatus("")
	return m, nil
}

// focusScope is every bead the focus counts: the Initiative, its Parent subtree and
// the waiters of its Checkpoints.
func (m Model) focusScope() map[string]bool { return m.scopeOf(m.focus.id) }

// scopeOf is focusScope for Initiative id.
func (m Model) scopeOf(id string) map[string]bool {
	g := m.gateData()
	return g.withWaiters(subtree(g.beads, id))
}

// withWaiters adds the waiters of the Checkpoints in sc to sc.
func (g graphData) withWaiters(sc map[string]bool) map[string]bool {
	for i, b := range g.beads {
		if sc[b.ID] && b.Kind == contract.KindCheckpoint {
			ws, _ := g.waiters(&g.beads[i])
			for _, w := range ws {
				sc[w.ID] = true
			}
		}
	}
	return sc
}

// subtree is id and every bead under it by Parent, up to 8 levels.
func subtree(beads []bd.Bead, id string) map[string]bool {
	at := make(map[string]int, len(beads))
	for i, b := range beads {
		at[b.ID] = i
	}
	sc := map[string]bool{}
	for i := range beads {
		for b, n := &beads[i], 0; n < 8; n++ {
			if b.ID == id {
				sc[beads[i].ID] = true
				break
			}
			j, ok := at[b.Parent]
			if !ok {
				break
			}
			b = &beads[j]
		}
	}
	return sc
}

// isShellKey is a key the shell owns even while focused: a tab switch, quit, loop, f5, ?.
func isShellKey(k tea.KeyMsg) bool {
	return slices.ContainsFunc(shellKeys, func(b key.Binding) bool { return key.Matches(k, b) })
}

// focusKey owns every non-shell key while the focus is open; ok is false for a shell
// key, which the shell then handles (a tab key leaves the focus in enter).
func (m Model) focusKey(k tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if isShellKey(k) {
		return m, nil, false
	}
	if key.Matches(k, keyBack) {
		if m.focus.open {
			m.focus.open, m.focus.off = false, 0
		} else {
			id := m.focus.id
			m.focus = focus{}
			m.setStatus("")
			return m, m.recordRecent(id), true // closing the Initiative's focus is an act on it
		}
		return m, nil, true
	}
	rows := m.focusRows()
	if len(rows) == 0 {
		return m, nil, true
	}
	i := m.focus.index(rows)
	r := rows[i]
	g := m.gateData()
	switch {
	case key.Matches(k, keyMove):
		d := 1
		if s := k.String(); s == "k" || s == "up" {
			d = -1
		}
		j := clampWrap(i+d, len(rows))
		for rows[j].kind == fGap {
			j = clampWrap(j+d, len(rows))
		}
		m.focus.sel, m.focus.off = rows[j].key, 0 // the detail follows: back to the top
	case key.Matches(k, keySection):
		d := 1
		if s := k.String(); s == "K" || s == "ctrl+up" {
			d = -1
		}
		m.focus.sel, m.focus.off = rows[jumpTo(rows, i, d, func(r fRow) bool { return r.kind == fSec })].key, 0
	case key.Matches(k, keyFold) && r.foldable():
		m.focus.flip = maps.Clone(m.focus.flip)
		m.focus.flip[r.key] = !m.focus.flip[r.key]
	case key.Matches(k, keyDetail) && m.focus.open:
		m.focus.open, m.focus.off = false, 0
	case key.Matches(k, keyDetail) && r.hasDetail():
		m.focus.open = true
	case m.focus.open && key.Matches(k, keyScroll):
		vp := m.focusDetailVP(r)
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
		m.focus.off = vp.YOffset
	case key.Matches(k, keyCheckup) && r.b != nil && (r.kind == fBead || r.kind == fHead) && canCheckup(r.b):
		next, cmd := m.checkupKey(*r.b)
		return next, cmd, true
	case key.Matches(k, keyLaunch), key.Matches(k, keyBg):
		if r.b == nil {
			m.setStatus(g.rowEnterWhy(nil, false))
			return m, nil, true
		}
		return m.focusAction(k, r)
	case r.kind == fHead && key.Matches(k, keyWithdrawKey): // X on the Initiative's own row cancels it
		return m.actionKey(k)
	case r.kind == fBead && r.sec == secIdeate:
		return m.focusIdeateKey(k)
	case r.kind == fBead:
		next, cmd, _ := m.queueRowKey(k, r.b, r.leaf) // x, +/- and E as on the tab
		return next, cmd, true
	}
	return m, nil, true
}

// focusAction is enter and b on a bead or header row. A header takes Graph's path
// (Luffy's cut session, ADR 0146) and an Ideate Signal the working tab's, straight to
// actionKey: a blocked or paused bead gets the y/n, not a refusal. Other rows take the
// queue tabs' shared routing.
func (m Model) focusAction(k tea.KeyMsg, r fRow) (tea.Model, tea.Cmd, bool) {
	if r.kind == fHead || r.sec == secIdeate {
		return m.actionKey(k)
	}
	next, cmd, _ := m.queueRowKey(k, r.b, r.leaf)
	return next, cmd, true
}

// focusIdeateKey is an Ideate Signal's keys beyond enter and b: p, x, +/-, e and z
// over the shared action and form paths. a, A and n are Ideate's alone.
func (m Model) focusIdeateKey(k tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch {
	case key.Matches(k, keyPrio), key.Matches(k, keyPauseKey), key.Matches(k, keyDropKey) && m.gateData().xHint(m.focusBead()) != "": // unhinted x does nothing
		return m.actionKey(k)
	case key.Matches(k, keyWithdrawKey):
		return m.actionKey(k)
	case key.Matches(k, keyEdit), key.Matches(k, keyReject):
		return m.graphKey(k)
	}
	return m, nil, true
}

// hasDetail is whether d opens a drawer on the row.
func (r fRow) hasDetail() bool {
	return r.kind == fHead || r.kind == fBead || (r.kind == fSum && r.cp != nil)
}

// focusDetailText is the row's detail from its source tab's builder: Graph's for the
// Initiative, Ideate's for a Signal.
func (m Model) focusDetailText(r fRow) string {
	g := m.gateData()
	if r.kind == fBead && r.sec == secIdeate {
		return ideaTab{ideaData: ideaData{beads: g.beads, gate: g, w: g.w}}.beadText(r.b)
	}
	if r.kind == fBead && r.sec == secDefog {
		return defogTab{g: g}.rowText(r.defogRow())
	}
	if r.sec == secExec && r.hasDetail() {
		return execTab{g: g}.rowText(r.eRow())
	}
	if r.b == nil {
		return ""
	}
	return graphTab{graphData: g, sel: r.b.ID}.detailText()
}

func (m Model) focusDetailVP(r fRow) viewport.Model {
	return drawerVP(m.focus.open, m.w, m.h-chromeH+1, m.focusDetailText(r), m.focus.off)
}

// focusBindings are the hints for the cursor row (placement and drop rank: ADR 0229); every mode ends esc back.
func (m Model) focusBindings() []key.Binding {
	r, ok := m.focusSel()
	if !ok {
		return []key.Binding{keyBack}
	}
	g := m.gateData()
	var bs []key.Binding
	switch {
	case r.kind == fHead:
		if v := g.enterVerb(r.b); v != "" {
			bs = append(bs, verbKey("enter", v))
		}
		if s := g.sessions[r.b.ID]; g.launchWhy(r.b) == "" && (!s.Has() || s.State == session.Exited) {
			bs = append(bs, keyBg) // b on an alive session only refuses, as on Graph
		}
	case r.kind == fBead && (r.sec == secDefog || r.sec == secExec):
		if g.rowEnterWhy(r.b, r.leaf) == "" {
			if v := g.enterVerb(r.b); v != "" {
				bs = append(bs, verbKey("enter", v))
				if g.rowBgShown(r.b, r.leaf) {
					bs = append(bs, keyBg)
				}
			}
		}
	case r.kind == fBead && r.sec == secIdeate:
		if g.launchWhy(r.b) == "" {
			bs = append(bs, keyTriage)
			if g.sessions[r.b.ID].State != session.Alive {
				bs = append(bs, keyBg)
			}
		}
	case r.kind == fBead && r.sec == secExec && g.rowEnterWhy(r.b, r.leaf) == "":
		if v := g.enterVerb(r.b); v != "" {
			bs = append(bs, verbKey("enter", v))
			if g.rowBgShown(r.b, r.leaf) {
				bs = append(bs, keyBg)
			}
		}
	}
	if r.foldable() {
		bs = append(bs, keyFold)
	}
	switch {
	case m.focus.open:
		bs = append(bs, keyShutD, keyScroll)
	case r.hasDetail():
		bs = append(bs, keyDetail)
	}
	if r.kind == fBead && r.sec == secIdeate {
		if rejectable(*r.b) {
			bs = append(bs, keyReject)
		}
		if r.b.Unclosed() {
			bs = append(bs, verbKey("p", pauseLabel(r.b)))
		}
		if l := g.xHint(r.b); l != "" {
			bs = append(bs, verbKey("x", l))
		}
		bs = append(bs, keyEdit, keyPrio)
	}
	if r.kind == fBead && (r.sec == secDefog || r.sec == secExec) {
		if r.b.Unclosed() {
			bs = append(bs, verbKey("p", pauseLabel(r.b)))
		}
		if l := g.xHint(r.b); l != "" {
			bs = append(bs, verbKey("x", l))
		}
		if r.b.Unclosed() {
			bs = append(bs, keyPrio)
		}
	}
	if r.b != nil && (r.kind == fBead || r.kind == fHead) && g.xOffers(r.b) {
		bs = append(bs, xBinding(r.b))
	}
	if r.b != nil && (r.kind == fBead || r.kind == fHead) && canCheckup(r.b) {
		bs = append(bs, keyCheckup)
	}
	if r.b != nil { // t acts on any bead row, right after x (else p)
		at := len(bs)
		for _, k := range []string{"x", "p"} {
			if i := slices.IndexFunc(bs, func(b key.Binding) bool { return b.Help().Key == k }); i >= 0 {
				at = i + 1
				break
			}
		}
		bs = slices.Insert(bs, at, keyTranscript)
	}
	return append(bs, keyBack)
}

// focusExtra is moving and section jumps, then the verbs the row handles but does not hint (enter's and
// b's refusals): shown in ? alone.
func (m Model) focusExtra() []key.Binding {
	if _, ok := m.focusSel(); !ok {
		return nil
	}
	out := []key.Binding{keyMove, keySection}
	shown := map[string]bool{}
	for _, b := range m.focusBindings() {
		shown[b.Keys()[0]] = true
	}
	r, _ := m.focusSel()
	cand := []key.Binding{keyLaunch, keyBg}
	if r.kind == fBead && r.sec == secIdeate {
		cand = append(cand, keyReject, keyPauseKey) // z answers with a status line where it is not hinted
	}
	if r.kind == fBead && (r.sec == secDefog || r.sec == secExec) {
		cand = append(cand, keyPauseKey, keyDropKey, keyWithdrawKey, keyPrio)
	}
	for _, b := range cand {
		if !shown[b.Keys()[0]] {
			out = append(out, b)
		}
	}
	return out
}

// focusView is the body: the rows over the detail drawer, or the left-the-graph line.
func (m Model) focusView(w, h int) string {
	rows := m.focusRows()
	if len(rows) == 0 {
		return dimSt.Render("  the Initiative left the graph — esc returns")
	}
	g := m.gateData()
	sel := m.focus.index(rows)
	tagW := 0 // Execute's bead rows share one kind-badge column
	for _, r := range rows {
		if r.kind == fBead && r.sec == secExec && r.b.Kind != contract.KindCheckpoint {
			tagW = max(tagW, kindTagW(r.b))
		}
	}
	b := m.focusBead()
	blockers := g.blockerSet(b)
	line := func(i int) (string, string) { return m.focusLine(g, rows[i], tagW, blockers) }
	var footer func(off, lh int) []string
	if b != nil {
		ids := make([]string, len(rows))
		for i, r := range rows {
			if r.b != nil {
				ids[i] = r.b.ID
			}
		}
		footer = blockerFooter(g, g.openBlockers(b), ids, w)
	}
	return drawerWin(len(rows), func(i int) string { return fitRow(line, w, sel, i) }, sel, m.focus.open, w, h, m.focusDetailVP(rows[sel]), footer)
}

// focusLine is a row's left part and right-edge suffix, in its source tab's anatomy;
// blockers are the selection's open blockers, marked in the gutter.
func (m Model) focusLine(g graphData, r fRow, tagW int, blockers map[string]bool) (string, string) {
	ind := strings.Repeat("  ", r.depth)
	caret := "▾ "
	if m.focus.folded(r) {
		caret = "▸ "
	}
	id := ""
	if r.b != nil {
		id = r.b.ID
	}
	var hid []bd.Bead
	if r.kind == fSum && m.focus.folded(r) {
		for _, c := range r.members {
			hid = append(hid, *c)
		}
	}
	// ponytail: a folded section, Chart or Checkpoint shows no hidden count; its blockers reach the footer.
	gut := blockerGutter(blockers, id, hid)
	switch r.kind {
	case fHead:
		head := gut + ind + "  " + g.sbrn(r.b, containerHead(r.b), true, 0)
		if bodyless(r.b) {
			head += softSt.Render(" · no body yet")
		}
		if g.noRef(r.b) {
			head += softSt.Render(" · no ref")
		}
		return head, strings.TrimLeft(g.rowSuffix(r.b), " ")
	case fSec:
		return gut + ind + dimSt.Render(caret) + lipgloss.NewStyle().Bold(true).Foreground(accent).Render(secNames[r.sec]) + dimSt.Render(fmt.Sprintf("  %d open", r.n)), ""
	case fSum:
		return gut + ind + dimSt.Render(fmt.Sprintf("%s%d closed", caret, len(r.members))), ""
	case fPlace:
		return gut + ind + "  " + dimSt.Render(secEmpty[r.sec]), ""
	case fGap:
		return "", ""
	}
	if r.sec == secDefog {
		// Defog's fold default differs from the focus's (a Chart is open here), so
		// hand it a flip whose collapsed() is the focus's fold state.
		dr := r.defogRow()
		flip := maps.Clone(m.focus.flip)
		flip[dr.key] = m.focus.folded(r) != collapsed(nil, dr)
		left, suffix := defogTab{g: g, flip: flip}.line(dr, m.focusTagW(), gut, "", blockers)
		if r.b.HasLabel("chart:defogged") && r.b.Unclosed() {
			left += dimSt.Render(" · defogged, awaiting land")
		}
		return left, suffix
	}
	if r.sec == secExec {
		return execTab{g: g, flip: m.focus.flip}.line(r.eRow(), tagW, blockers, "")
	}
	return ideaTab{ideaData: ideaData{gate: g}}.line(ideaRow{key: r.b.ID, b: r.b, depth: r.depth}, blockers)
}

// focusTagW is the width of the Defog leaf and AFK rows' shared kind-badge column.
func (m Model) focusTagW() (w int) {
	for _, r := range m.focusRows() {
		if r.kind == fBead && r.sec == secDefog && r.b.Kind != contract.KindChart {
			w = max(w, lipgloss.Width(kindTag(r.b)))
		}
	}
	return
}

// backBar is the one-row bar, plus a rule, that replaces the tab bar: ← esc back, the
// Initiative's title and its ●N of sessions waiting on you, then the scoped crew counts
// and where esc returns. The left side is cut first.
func (m Model) backBar() string {
	title := ""
	if i := beadIndex(m.beads, m.focus.id); i >= 0 {
		title = chartName(m.beads[i].Title)
	}
	sc := m.focusScope()
	hitl, afk, paused, _ := m.crewCounts(func(id string) bool { return sc[id] }, m.retrying())
	waiting := 0
	for _, b := range m.beads {
		if sc[b.ID] && m.waiting(b) {
			waiting++
		}
	}
	left := " " + lipgloss.NewStyle().Bold(true).Foreground(accent).Render("← esc back") + "  " + lipgloss.NewStyle().Bold(true).Render(title)
	if waiting > 0 {
		left += " " + badgeSt.Render(fmt.Sprintf("●%d", waiting))
	}
	segs := []string{countSt(aliveSt, hitl, "HITL"), countSt(afkSt, afk, "AFK")}
	if paused > 0 {
		segs = append(segs, waitSt.Render(fmt.Sprintf("%d‖", paused)))
	}
	sp := dimSt.Render(" · ")
	right := strings.Join(segs, sp) + sp + dimSt.Render("from "+tabNames[m.tab]) + " "
	left = ansi.Truncate(left, max(m.w-lipgloss.Width(right)-1, 1), "…")
	gap := max(1, m.w-lipgloss.Width(left)-lipgloss.Width(right))
	return ansi.Truncate(left+strings.Repeat(" ", gap)+right, m.w, "") + "\n" + dimSt.Render(strings.Repeat("─", m.w))
}

// focusFrame is frame with the back bar's two rows in place of the tab bar's three.
func (m Model) focusFrame() string {
	return fit(strings.Join([]string{
		m.headerView(),
		m.backBar(),
		fit(m.focusView(m.w, m.h-chromeH+1), m.w, m.h-chromeH+1),
		" " + strings.ReplaceAll(m.status, "\n", " "),
		m.hintsView(),
	}, "\n"), m.w, m.h)
}
