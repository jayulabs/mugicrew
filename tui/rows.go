package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/mdrender"
)

// leafRestyle styles a bead row's title, shared by every tab that lists beads: closed >
// Draft (dim) > hot (an open blocker of the selection) > AFK mute (any non-HITL kind) >
// normal. plain reports the normal case (no recolour), so a caller can keep its own richer
// rendering there without repeating the precedence. inert marks a restyle that asks for no
// attention (closed, Draft): its badge is wholly in st. An attention restyle (hot, AFK mute)
// leaves a P0/P1 effective digit in prio's colour.
func leafRestyle(b *bd.Bead, hot bool) (st lipgloss.Style, plain, inert bool) {
	switch {
	case b.Status == "closed":
		return closedSty, false, true
	case b.Draft():
		return dimSt, false, true
	case hot:
		return pinkFg, false, false
	case !isHITL(b):
		return softSt, false, false
	}
	return lipgloss.NewStyle(), true, false
}

// containerTitleStyle is leafRestyle for a container row (Chart, Checkpoint,
// Initiative): closed > hot > plain. Containers are never AFK-muted.
func containerTitleStyle(b *bd.Bead, hot bool) (st lipgloss.Style, plain bool) {
	switch {
	case b.Status == "closed":
		return closedSty, false
	case b.Draft():
		return dimSt, false
	case hot:
		return pinkFg, false
	}
	return lipgloss.NewStyle(), true
}

// containerTitle is title styled by containerTitleStyle.
func containerTitle(b *bd.Bead, hot bool, title string) string {
	st, _ := containerTitleStyle(b, hot)
	return st.Render(title)
}

// sep is the soft-grey · between the items that follow a row's badge and
// priority: title, ID and hint. The right-edge suffix and trailers have none.
func sep() string { return softSt.Render(" · ") }

// sbrn is a bead's standard bead row notation (GLOSSARY): <status>[<dot>] <kind>
// <priority> · <title> · <id>. title arrives already styled; live is the view's call
// (live-tracking views carry the dot slot on every row, others drop it); kindW is the
// minimum visible kind-tag width, padding a group's kind column so titles line up.
func (g graphData) sbrn(b *bd.Bead, title string, live bool, kindW int) string {
	head, tail := g.sbrnParts(b, live, kindW)
	return head + title + tail
}

// sbrnLeaf is sbrn for a Defog or Execute leaf row on a live view: its priority
// takes leafRestyle's recolour (closed, hot blocker, AFK mute) like its title, except
// that a hot or AFK-muted row keeps prio's colour on a P0/P1 effective digit.
func (g graphData) sbrnLeaf(b *bd.Bead, title string, kindW int, hot bool) string {
	eff, _ := g.effectivePriority(b)
	p := prioBadge(eff, b.Priority)
	if st, plain, inert := leafRestyle(b, hot); !plain {
		p = styledBadge(st, inert, eff, b.Priority)
	}
	head, tail := g.sbrnAround(b, true, kindW, p)
	return head + title + tail
}

// sbrnParts splits sbrn around its title: everything up to and including the
// separator before it, and the separator and id after it.
func (g graphData) sbrnParts(b *bd.Bead, live bool, kindW int) (head, tail string) {
	eff, _ := g.effectivePriority(b)
	return g.sbrnAround(b, live, kindW, prioBadge(eff, b.Priority))
}

// effectivePriority is b's bd.EffectivePriority over the live beads, then the walk
// ancestors the snapshot lacks.
func (g graphData) effectivePriority(b *bd.Bead) (level int, src string) {
	return bd.EffectivePriority(*b, g.effectivePriorityAncestor)
}

// inheritLine is the drawer's line under a bead's header row when an open container
// ancestor sets its effective priority: "dispatches as P<eff> from <kind> <title>".
// "" when the bead's own priority is effective. It flashes while prioFlash names b.
func (g graphData) inheritLine(b *bd.Bead) string {
	eff, src := g.effectivePriority(b)
	if src == "" {
		return ""
	}
	line := fmt.Sprintf("dispatches as P%d %s", eff, g.inheritSource(src))
	if g.prioFlash == b.ID {
		return warnSt.Reverse(true).Render(line)
	}
	return softSt.Render(line)
}

// inheritSource is "from <kind> <title>" for the ancestor src that sets a bead's effective
// priority, the wording the drawer line and the +/- status echo share.
func (g graphData) inheritSource(src string) string {
	a, _ := g.effectivePriorityAncestor(src)
	return fmt.Sprintf("from %s %s", a.Kind, a.Title)
}

// effectivePriorityAncestor is the ancestor id as the live beads or the walk ancestors hold it.
func (g graphData) effectivePriorityAncestor(id string) (bd.Bead, bool) {
	if i := g.beadIndex(id); i >= 0 {
		return g.beads[i], true
	}
	a, ok := g.ancestors[id]
	return a, ok
}

// withInherit is a drawer header row followed, when b inherits its priority, by the inherit line.
func (g graphData) withInherit(b *bd.Bead, head string) string {
	if l := g.inheritLine(b); l != "" {
		return head + "\n" + l
	}
	return head
}

// sbrnAround is sbrnParts with the styled priority p supplied by the caller.
func (g graphData) sbrnAround(b *bd.Bead, live bool, kindW int, p string) (head, tail string) {
	mark := g.stateGlyph(b)
	if live {
		mark += g.rowDot(b)
	}
	tag := kindTag(b)
	pad := strings.Repeat(" ", max(0, kindW-lipgloss.Width(tag)))
	idSt := softSt
	if b.Draft() {
		idSt = dimSt
	}
	return mark + " " + tag + pad + " " + p + sep(), sep() + idSt.Render(b.ID)
}

// leafTitle is a leaf's title styled by leafRestyle.
func leafTitle(b *bd.Bead, hot bool) string {
	if st, plain, _ := leafRestyle(b, hot); !plain {
		return st.Render(b.Title)
	}
	return b.Title
}

// drawerH is the list's rows: the whole body, or the top half over the detail drawer.
func drawerH(open bool, h int) int {
	if open {
		return h / 2
	}
	return h
}

// drawerContentW is the drawer's text width for a body w wide.
func drawerContentW(w int) int { return max(w-2, 1) }

// memoCap bounds each drawer memo: a few entries cover the open bead at the current width
// across a resize or a refresh.
const memoCap = 4

// memoEntry is one cached result keyed on the content itself and the width it was built at,
// so a changed body or a resize is a miss by construction.
type memoEntry struct {
	text string
	w    int
	out  string
}

// memo is a small bounded memo, newest last. It is pure (results depend only on the key), so
// one process-wide instance behind a mutex is safe for parallel tests and for any goroutine.
type memo struct {
	mu sync.Mutex
	es []memoEntry
}

func (c *memo) get(text string, w int, build func() string) string {
	c.mu.Lock()
	for _, e := range c.es {
		if e.w == w && e.text == text {
			c.mu.Unlock()
			return e.out
		}
	}
	c.mu.Unlock()
	out := build()
	c.mu.Lock()
	if len(c.es) >= memoCap {
		c.es = c.es[1:]
	}
	c.es = append(c.es, memoEntry{text, w, out})
	c.mu.Unlock()
	return out
}

var (
	mdMemo   memo // mdBody: (body, width) -> rendered Markdown
	wrapMemo memo // drawerVP: (text, content width) -> wrapped text
)

// mdBody renders a bead body as Markdown at the drawer's text width; an empty body
// stays empty. Memoized on (body, w) per ADR 0383, so a wheel event never re-renders it.
// The memos are package-level because memo guards itself with a mutex, so sharing one
// across Model copies is race-free and wants no pointer threaded through.
func mdBody(body string, w int) string {
	if body == "" {
		return ""
	}
	return mdMemo.get(body, w, func() string {
		return strings.Trim(mdrender.Render(body, drawerContentW(w)), "\n")
	})
}

// drawerVP is the drawer's detail viewport: width w-2, the rows under the list and its top
// edge, less the bottom edge's row. The wrapped content is memoized on (text, content width).
func drawerVP(open bool, w, h int, text string, off int) viewport.Model {
	vp := viewport.New(drawerContentW(w), max(h-drawerH(open, h)-2, 1))
	vp.SetContent(wrapMemo.get(text, vp.Width, func() string {
		return lipgloss.NewStyle().Width(vp.Width).Render(text)
	}))
	vp.SetYOffset(off)
	return vp
}

// blockerSet is the set of b's open blockers (nil b: none), the beads a tab pinks.
func (g graphData) blockerSet(b *bd.Bead) map[string]bool {
	set := map[string]bool{}
	if b != nil {
		for _, t := range g.openBlockers(b) {
			set[t.ID] = true
		}
	}
	return set
}

// blockerGutter is a row's 1-column blocker mark: a pink ! when the row's bead id
// is in blockers, else the count of blockers among hidden (the beads a folded row
// hides, nil when it is not folded), else a space.
func blockerGutter(blockers map[string]bool, id string, hidden []bd.Bead) string {
	if blockers[id] {
		return blockerMk.Render("!")
	}
	n := 0
	for _, b := range hidden {
		if blockers[b.ID] {
			n++
		}
	}
	if n == 0 {
		return " "
	}
	// ponytail: 10+ hidden blockers widen the gutter by a column; clamp if that ever happens.
	return blockerMk.Render(fmt.Sprint(n))
}

// maxFooterBlockers is the most rows the blocker footer takes; past it, the first
// maxFooterBlockers-1 blockers show and a "… +N more blockers" row sums the rest.
const maxFooterBlockers = 3

// blockerRows is the footer's content for the open blockers bl of the selection over the
// drawn window [off, off+lh) of rowIDs: the blockers whose id is not among the window's
// ids, the first maxFooterBlockers-1 of them past the cap, and how many more the cap hid.
func blockerRows(bl []*bd.Bead, rowIDs []string, off, lh int) (shown []*bd.Bead, more int) {
	drawn := rowIDs[min(off, len(rowIDs)):min(off+lh, len(rowIDs))]
	for _, b := range bl {
		if !slices.Contains(drawn, b.ID) {
			shown = append(shown, b)
		}
	}
	if len(shown) > maxFooterBlockers {
		more = len(shown) - (maxFooterBlockers - 1)
		shown = shown[:maxFooterBlockers-1]
	}
	return shown, more
}

// blockerFooter builds drawer's footer for the open blockers bl of the selection:
// those whose id is not among the rowIDs of the drawn window [off, off+lh), each a
// !-led SBRN row cut to w; past maxFooterBlockers, a dim "… +N more blockers" row
// replaces the rest. No rows when none are off screen.
func blockerFooter(g graphData, bl []*bd.Bead, rowIDs []string, w int) func(off, lh int) []string {
	return func(off, lh int) []string {
		shown, more := blockerRows(bl, rowIDs, off, lh)
		var rows []string
		for _, b := range shown {
			rows = append(rows, ansi.Truncate(blockerMk.Render("!")+g.sbrn(b, cmp.Or(b.Title, b.ID), true, 0), w, "…"))
		}
		if more > 0 {
			rows = append(rows, ansi.Truncate(dimSt.Render(fmt.Sprintf("… +%d more blockers", more)), w, "…"))
		}
		return rows
	}
}

// blockerLinks is blockerFooter's linked rows alone: the bead ids of the footer's "!" rows,
// the "+N more blockers" row not among them.
func blockerLinks(bl []*bd.Bead, rowIDs []string) func(off, lh int) []string {
	return func(off, lh int) []string {
		shown, _ := blockerRows(bl, rowIDs, off, lh)
		ids := make([]string, len(shown))
		for i, b := range shown {
			ids[i] = b.ID
		}
		return ids
	}
}

// drawerWin is the bottom-drawer body shared by the list-over-detail tabs: n lines
// windowed to drawerH around sel, and when open padded to that height, then the
// detail viewport in an accent rounded box. line(i) is called only for the drawn
// window, so n, sel and the footer's ids are all it needs from the full set. The
// footer's rows (footer may be nil) take the list area's last rows: the window shrinks
// by their count, followOffset runs against it, and since the rows depend on the offset
// the reservation settles over at most 2 passes. A footer that leaves no list row is
// dropped.
func drawerWin(n int, line func(i int) string, sel int, open bool, w, h int, vp viewport.Model, footer func(off, lh int) []string, offs offsetFn) string {
	g := drawerGeometry(n, sel, open, h, footer, offs)
	win := make([]string, 0, g.lh)
	for i := g.off; i < min(g.off+g.lh, n); i++ {
		win = append(win, line(i))
	}
	list := strings.Join(win, "\n")
	if len(g.foot) > 0 { // the footer is the list area's last rows
		list += strings.Repeat("\n", max(0, g.lh-1-strings.Count(list, "\n"))) + "\n" + strings.Join(g.foot, "\n")
	}
	if !open {
		return list
	}
	pad := strings.Repeat("\n", max(0, g.full-1-strings.Count(list, "\n")))
	return list + pad + "\n" + drawerBox(w, vp.View())
}

// drawerBox frames the detail viewport's view in a rounded accent box w wide: a top edge,
// each line as │ + the line padded to w-2 + │, a bottom edge.
func drawerBox(w int, view string) string {
	bd := lipgloss.NewStyle().Foreground(accent)
	iw := max(w-2, 0)
	ls := []string{bd.Render("╭" + strings.Repeat("─", iw) + "╮")}
	for _, l := range strings.Split(view, "\n") {
		ls = append(ls, bd.Render("│")+l+strings.Repeat(" ", max(iw-lipgloss.Width(l), 0))+bd.Render("│"))
	}
	return strings.Join(append(ls, bd.Render("╰"+strings.Repeat("─", iw)+"╯")), "\n")
}

// drawerGeo is drawerWin's arithmetic, pure: where the window starts, how tall it and the
// whole list area are, and the footer that took the list area's last rows.
type drawerGeo struct {
	full, lh, off int      // the list area's height, the window's height (full less the footer), its first line
	foot          []string // the footer rows drawn
	footOff       int      // the window offset the footer rows were built for,
	footLh        int      // and its height: what a hit re-asks blockerLinks with
}

// drawerGeometry settles drawerWin's window over n lines selected at sel in a body h tall,
// with the drawer open or shut and an optional footer.
func drawerGeometry(n, sel int, open bool, h int, footer func(off, lh int) []string, offs offsetFn) drawerGeo {
	if offs == nil {
		offs = followOffset
	}
	full := drawerH(open, h)
	g := drawerGeo{full: full, lh: full, off: offs(n, full, sel)}
	if footer == nil {
		return g
	}
	k := 0
	for range 3 { // pass 0 measures the unreserved window; two passes settle
		g.off = offs(n, full-k, sel)
		g.footOff, g.footLh = g.off, full-k
		g.foot = footer(g.off, full-k)
		if len(g.foot) == k {
			break
		}
		if k = len(g.foot); k >= full {
			k, g.foot = 0, nil
			g.off = offs(n, full, sel)
			break
		}
	}
	g.foot = g.foot[:min(len(g.foot), k)]
	g.lh = full - len(g.foot)
	g.off = offs(n, g.lh, sel)
	return g
}

// listGeom is what a drawer list's renderer and its pointer share about one frame: n lines
// selected at sel in a body h tall, the drawer open or shut, the blocker footer and its links,
// each line's anchor key and what a line is to the pointer (the zero hitTarget for an inert one).
type listGeom struct {
	n, sel, h int
	open      bool
	footer    func(off, lh int) []string
	links     func(off, lh int) []string // names the blockers the footer rows jump to
	keys      []string                   // each line's anchor key, from anchorKeys
	at        func(k int) hitTarget
}

// geometry is the frame's drawerGeo under the list's viewport lv.
func (g listGeom) geometry(lv listView) drawerGeo {
	return drawerGeometry(g.n, g.sel, g.open, g.h, g.footer, lv.offsets(g.keys))
}

// hit maps body line y through the same geometry as the renderer.
func (g listGeom) hit(lv listView, y int) hitTarget {
	geo := g.geometry(lv)
	t := hitTarget{caret: -1, line: y}
	switch {
	case y < 0 || y >= g.h:
	case y < geo.lh:
		if k := geo.off + y; k < g.n {
			if r := g.at(k); r.kind != hitNone {
				t.kind, t.key, t.bead = r.kind, r.key, r.bead
				if s := g.at(g.sel); g.sel < g.n && s.kind != hitNone {
					t.sel = s.kind == r.kind && s.key == r.key
				}
			}
		}
	case y < geo.full:
		if j := y - geo.lh; j < len(geo.foot) && g.links != nil {
			if ids := g.links(geo.footOff, geo.footLh); j < len(ids) {
				t.kind, t.key = hitBlockerLink, ids[j]
			}
		}
	case g.open && y > geo.full && y < g.h-1: // the box's top and bottom edges are inert
		t.kind = hitDrawer
	}
	return t
}

// wheeled is lv after the wheel moved the window by delta lines: Scrolled, anchored on the
// new top line. A list that fits its window has nothing to scroll.
func (g listGeom) wheeled(lv listView, delta int) listView {
	geo := g.geometry(lv)
	if g.n <= geo.lh || len(g.keys) != g.n {
		return lv
	}
	top := min(max(geo.off+delta, 0), g.n-geo.lh)
	return scrolledTo(g.keys, top)
}

// snapped is lv after a key: Following. If the selection is on screen at the held offset the
// list resumes from it and scrolls only as far as keeping the selection visible takes, else
// it re-centres as followOffset does.
func (g listGeom) snapped(lv listView) listView {
	if !lv.scrolled && !lv.resume {
		return lv
	}
	geo := g.geometry(lv)
	if g.sel < geo.off || g.sel >= geo.off+geo.lh {
		return listView{}
	}
	return listView{resume: true, off: geo.off}
}

// stopTarget is the hitTarget of a stop: a row when it carries a bead, else a header.
func stopTarget(key string, bead bool) hitTarget {
	if bead {
		return hitTarget{kind: hitRow, key: key, bead: true}
	}
	return hitTarget{kind: hitHeader, key: key}
}

// queueGeom is the listGeom of a queue tab's drawRows body. idx holds each post-gap line's
// row number as text ("" for a gap, inert), key is row i's key, stop reports row i's key,
// whether it carries a bead and whether j/k can land on it (a placeholder is not a stop).
func queueGeom(idx []string, sel int, open bool, h int, footer, links func(off, lh int) []string, rowKey func(i int) string, stop func(i int) (key string, bead, ok bool)) listGeom {
	keys := make([]string, len(idx))
	for k, s := range idx {
		if s != "" {
			i, _ := strconv.Atoi(s)
			keys[k] = rowKey(i)
		}
	}
	return listGeom{n: len(idx), sel: sel, h: h, open: open, footer: footer, links: links, keys: anchorKeys(keys), at: func(k int) hitTarget {
		if idx[k] == "" {
			return hitTarget{}
		}
		i, _ := strconv.Atoi(idx[k])
		if key, bead, ok := stop(i); ok {
			return stopTarget(key, bead)
		}
		return hitTarget{}
	}}
}

// gapAt inserts a blank separator line before line at (0 < at < len(lines)),
// shifting sel and ids (the footer's per-line bead ids) to match; else a no-op.
func gapAt(lines, ids []string, sel, at int) ([]string, []string, int) {
	if at <= 0 || at >= len(lines) {
		return lines, ids, sel
	}
	if sel >= at {
		sel++
	}
	return slices.Insert(lines, at, ""), slices.Insert(ids, at, ""), sel
}

// listLines renders n queue-tab rows into lines of width w, the selected row
// highlighted; line gives row i's left part and right-edge suffix.
func listLines(n int, line func(i int) (left, right string), w, sel int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fitRow(line, w, sel, i)
	}
	return lines
}

// fitRow is listLines' row i alone.
func fitRow(line func(i int) (left, right string), w, sel, i int) string {
	left, right := line(i)
	rw := lipgloss.Width(right)
	if rw > 0 {
		rw++ // a gap before the right edge
	}
	left = ansi.Truncate(left, max(w-rw, 1), "…")
	l := left + strings.Repeat(" ", max(0, w-rw-lipgloss.Width(left)))
	if rw > 0 {
		l += right + " "
	}
	l = ansi.Truncate(l, w, "…")
	if i == sel {
		l = selectRow("❯"+ansi.TruncateLeft(l, 1, ""), w)
	}
	return l
}

// drawRows is drawerWin over n queue-tab rows with gaps: idx holds each line's row
// number as text ("" for a gap line, as gapAt inserts them), so only the drawn window
// is formatted, shaded when its row is in shade.
func drawRows(idx []string, line func(i int) (left, right string), shade map[int]bool, sel, selRow int, open bool, w, h int, vp viewport.Model, footer func(off, lh int) []string, offs offsetFn) string {
	return drawerWin(len(idx), func(k int) string {
		if idx[k] == "" {
			return ""
		}
		i, _ := strconv.Atoi(idx[k])
		l := fitRow(line, w, selRow, i)
		if shade[i] {
			l = shadeRow(l, w, ancRow)
		}
		return l
	}, sel, open, w, h, vp, footer, offs)
}

// expandAll is the flip set that opens every row: it keeps flipping the folded rows
// of list(flip) until nothing foldable is folded. It serves all three list tabs.
func expandAll[R any](list func(map[string]bool) []R, folded func(map[string]bool, R) bool, keyOf func(R) string) map[string]bool {
	flip := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, r := range list(flip) {
			if folded(flip, r) {
				flip[keyOf(r)] = !flip[keyOf(r)]
				changed = true
			}
		}
	}
	return flip
}

// below is the beads of the rows under full[i] until depth returns to its own.
func below[R any](full []R, i int, depth func(R) int, bead func(R) *bd.Bead) (out []bd.Bead) {
	if i < 0 {
		return nil
	}
	for _, f := range full[i+1:] {
		if depth(f) <= depth(full[i]) {
			break
		}
		if b := bead(f); b != nil {
			out = append(out, *b)
		}
	}
	return
}

// subgraph is every descendant of id by Parent, breadth first with children in slice order.
// It walks the snapshot's children index; an unindexed g rescans the beads per node.
func subgraph(g graphData, id string) (out []bd.Bead) {
	seen := map[string]bool{id: true}
	for q := []string{id}; len(q) > 0; q = q[1:] {
		if g.idx != nil {
			for _, i := range g.idx.kids[q[0]] {
				if b := g.beads[i]; !seen[b.ID] {
					seen[b.ID] = true
					out = append(out, b)
					q = append(q, b.ID)
				}
			}
			continue
		}
		for _, b := range g.beads {
			if b.Parent == q[0] && !seen[b.ID] {
				seen[b.ID] = true
				out = append(out, b)
				q = append(q, b.ID)
			}
		}
	}
	return
}

// trailer is a folded header's " · N open · M live" over the beads the fold hides: a
// zero part is dropped, and with both zero it is "". drained stands in for the open part;
// noBody tags a drained Initiative "no body yet" (ADR 0182 §5).
func (g graphData) trailer(hidden []bd.Bead, noBody, drained bool) string {
	var open, live int
	for i := range hidden {
		b := &hidden[i]
		if b.Unclosed() && b.Status != "deferred" && !b.Event {
			open++
		}
		if g.isLive(b) {
			live++
		}
	}
	var parts []string
	switch {
	case drained:
		parts = append(parts, dimSt.Render("drained"))
		if noBody {
			parts = append(parts, dimSt.Render("no body yet"))
		}
	case open > 0:
		parts = append(parts, dimSt.Render(fmt.Sprintf("%d open", open)))
	}
	if live > 0 {
		parts = append(parts, aliveSt.Render(fmt.Sprintf("%d live", live)))
	}
	if len(parts) == 0 {
		return ""
	}
	return dimSt.Render(" · ") + strings.Join(parts, dimSt.Render(" · "))
}

// shadeIn is a rendered row reduced to what the ancestor shade needs (Spec D6): the
// bead it stands for ("" for none), the key of the group header it sits under (its own
// when head), whether it is a section row (Actionable or Live, act) and whether it is a
// group header; live marks a Live row, so the three rows of a bead are twins of each other.
type shadeIn struct {
	id, grp         string
	act, head, live bool
}

// shadeSet is the indexes of rows to shade for the selection rows[sel]: the rows on
// its Parent chain (headers included), the group header its tree twin sits under in
// full (the fully expanded rows), and its twin in the other section. A selection that
// is not a bead row shades nothing; a group header shades only the Actionable row it
// twins (ADR 0180 §4) and that row's Parent chain. Every row of an unclosed MR Watch or
// Landing whose Parent is a Checkpoint or Chart on that chain, the selection included, is
// shaded too (ADR 0179 §5). bead looks a bead up by id; in maps a row; a tree row's group
// is the last header above it.
func shadeSet[R any](bead func(string) *bd.Bead, rows []R, sel int, full []R, in func(R) shadeIn) map[int]bool {
	ins := func(rs []R) []shadeIn {
		out, grp := make([]shadeIn, len(rs)), ""
		for i, r := range rs {
			out[i] = in(r)
			if out[i].head {
				grp = out[i].grp
			} else if !out[i].act {
				out[i].grp = grp
			}
		}
		return out
	}
	rs, fs := ins(rows), ins(full)
	set := map[int]bool{}
	if sel < 0 || sel >= len(rs) || rs[sel].id == "" {
		return set
	}
	s := rs[sel]
	grp := s.grp // a tree selection sits under its own group header
	for _, f := range fs {
		if s.act && f.id == s.id && !f.act {
			grp = f.grp
			break
		}
	}
	// ctrs holds the Checkpoints and Charts on the chain, the selection included.
	chain, ctrs := map[string]bool{}, map[string]bool{}
	for b, first := bead(s.id), true; b.ID != "" && !chain[b.ID]; b = bead(b.Parent) {
		if !first {
			chain[b.ID] = true
		}
		if b.Kind == contract.KindCheckpoint || b.Kind == contract.KindChart {
			ctrs[b.ID] = true
		}
		first = false
	}
	// ridesChain: id is an unclosed MR Watch or Landing of a Checkpoint or Chart on the chain.
	ridesChain := func(id string) bool {
		b := bead(id)
		return b.Unclosed() && (b.Kind == contract.KindMRWatch || b.Kind == contract.KindLanding) && ctrs[b.Parent]
	}
	for i, r := range rs {
		if twin := r.id == s.id && (r.act != s.act || r.live != s.live); i != sel && (twin || chain[r.id] || r.id != "" && len(ctrs) > 0 && ridesChain(r.id) || !s.head && r.head && grp != "" && r.grp == grp) {
			set[i] = true
		}
	}
	if s.head && !slices.ContainsFunc(rs, func(r shadeIn) bool { return r.act && r.id == s.id }) {
		return map[int]bool{} // a plain group header shades nothing
	}
	return set
}

// band is rows split by leafState into the three Actionable bands (Spec D1), each in
// beadCmp order: waiting on the human (sTurn, sAsk), the takeable frontier (sReady), then
// the paused band (sPaused with an own paused:* label; an inherited-only pause is out).
// Every other state is out.
func band[R any](g graphData, rows []R, bead func(R) *bd.Bead) (wait, ready, paused []R) {
	for _, r := range rows {
		switch g.leafState(bead(r)) {
		case sTurn, sAsk:
			wait = append(wait, r)
		case sReady, sUndraft: // an un-draft is the human's, on GitLab: it waits with the frontier, not in the badged wait band
			ready = append(ready, r)
		case sPaused:
			if bd.Paused(*bead(r)) {
				paused = append(paused, r)
			}
		}
	}
	cmp := func(a, b R) int { return g.rowCmp(*bead(a), *bead(b)) }
	slices.SortStableFunc(wait, cmp)
	slices.SortStableFunc(ready, cmp)
	slices.SortStableFunc(paused, cmp)
	return
}

// containerHead is the Focus head's title: bold, composed with containerTitleStyle.
func containerHead(b *bd.Bead) string {
	st, _ := containerTitleStyle(b, false)
	return st.Bold(true).Render(chartName(b.Title))
}
