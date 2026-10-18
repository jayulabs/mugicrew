package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/mdrender"
)

// leafTitleStyle styles a bead row's title, shared by every tab that lists
// beads: closed > hot (an open blocker of the selection) > AFK mute (any non-HITL
// kind) > normal. plain reports the normal case (no recolour), so a caller can
// keep its own richer rendering there without repeating the precedence.
func leafTitleStyle(b *bd.Bead, hot bool) (st lipgloss.Style, plain bool) {
	switch {
	case b.Status == "closed":
		return closedSty, false
	case hot:
		return pinkFg, false
	case !isHITL(b.Kind):
		return softSt, false
	}
	return lipgloss.NewStyle(), true
}

// containerTitleStyle is leafTitleStyle for a container row (Chart, Checkpoint,
// Initiative): closed > hot > plain. Containers are never AFK-muted.
func containerTitleStyle(b *bd.Bead, hot bool) (st lipgloss.Style, plain bool) {
	switch {
	case b.Status == "closed":
		return closedSty, false
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
// takes leafTitleStyle's recolour (closed, hot blocker, AFK mute) like its title.
func (g graphData) sbrnLeaf(b *bd.Bead, title string, kindW int, hot bool) string {
	cp, member := g.containerPrio(b)
	p := prio(b.Priority)
	if member {
		p = prioIn(b.Priority, cp)
	}
	if st, plain := leafTitleStyle(b, hot); !plain {
		if !member {
			cp = 2 // no tint
		}
		p = tinted(st, cp).Render(prioText(b.Priority))
	}
	head, tail := g.sbrnAround(b, true, kindW, p)
	return head + title + tail
}

// sbrnParts splits sbrn around its title: everything up to and including the
// separator before it, and the separator and id after it.
func (g graphData) sbrnParts(b *bd.Bead, live bool, kindW int) (head, tail string) {
	p := prio(b.Priority)
	if cp, member := g.containerPrio(b); member {
		p = prioIn(b.Priority, cp)
	}
	return g.sbrnAround(b, live, kindW, p)
}

// containerPrio is the priority of b's container, a Checkpoint or Chart parent read
// from the live beads; member is false for a bead with no such parent (an Initiative
// parent included), which renders untinted.
func (g graphData) containerPrio(b *bd.Bead) (cp int, member bool) {
	if i := g.beadIndex(b.Parent); b.Parent != "" && i >= 0 {
		if k := g.beads[i].Kind; k == contract.KindCheckpoint || k == contract.KindChart {
			return g.beads[i].Priority, true
		}
	}
	return 0, false
}

// sbrnAround is sbrnParts with the styled priority p supplied by the caller.
func (g graphData) sbrnAround(b *bd.Bead, live bool, kindW int, p string) (head, tail string) {
	mark := g.stateGlyph(b)
	if live {
		mark += g.rowDot(b)
	}
	tag := kindTag(b)
	pad := strings.Repeat(" ", max(0, kindW-lipgloss.Width(tag)))
	return mark + " " + tag + pad + " " + p + sep(), sep() + softSt.Render(b.ID)
}

// leafTitle is a leaf's title styled by leafTitleStyle.
func leafTitle(b *bd.Bead, hot bool) string {
	if st, plain := leafTitleStyle(b, hot); !plain {
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

// mdBody renders a bead body as Markdown at the drawer's text width; an empty body
// stays empty. ponytail: no cache; a one-entry (body, width) memo behind a pointer
// if rendering per frame ever shows up in a profile.
func mdBody(body string, w int) string {
	if body == "" {
		return ""
	}
	return strings.Trim(mdrender.Render(body, drawerContentW(w)), "\n")
}

// drawerVP is the drawer's detail viewport: width w-2, the rows under the list and its rule.
func drawerVP(open bool, w, h int, text string, off int) viewport.Model {
	vp := viewport.New(drawerContentW(w), max(h-drawerH(open, h)-1, 1))
	vp.SetContent(lipgloss.NewStyle().Width(vp.Width).Render(text))
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

// blockerFooter builds drawer's footer for the open blockers bl of the selection:
// those whose id is not among the rowIDs of the drawn window [off, off+lh), each a
// !-led SBRN row cut to w; past maxFooterBlockers, a dim "… +N more blockers" row
// replaces the rest. No rows when none are off screen.
func blockerFooter(g graphData, bl []*bd.Bead, rowIDs []string, w int) func(off, lh int) []string {
	return func(off, lh int) []string {
		drawn := rowIDs[min(off, len(rowIDs)):min(off+lh, len(rowIDs))]
		var hidden []*bd.Bead
		for _, b := range bl {
			if !slices.Contains(drawn, b.ID) {
				hidden = append(hidden, b)
			}
		}
		more := 0
		if len(hidden) > maxFooterBlockers {
			more = len(hidden) - (maxFooterBlockers - 1)
			hidden = hidden[:maxFooterBlockers-1]
		}
		var rows []string
		for _, b := range hidden {
			rows = append(rows, ansi.Truncate(blockerMk.Render("!")+g.sbrn(b, cmp.Or(b.Title, b.ID), true, 0), w, "…"))
		}
		if more > 0 {
			rows = append(rows, ansi.Truncate(dimSt.Render(fmt.Sprintf("… +%d more blockers", more)), w, "…"))
		}
		return rows
	}
}

// drawerWin is the bottom-drawer body shared by the list-over-detail tabs: n lines
// windowed to drawerH around sel, and when open padded to that height, a dim
// full-width rule, then the detail viewport. line(i) is called only for the drawn
// window, so n, sel and the footer's ids are all it needs from the full set. The
// footer's rows (footer may be nil) take the list area's last rows: the window shrinks
// by their count, followOffset runs against it, and since the rows depend on the offset
// the reservation settles over at most 2 passes. A footer that leaves no list row is
// dropped.
func drawerWin(n int, line func(i int) string, sel int, open bool, w, h int, vp viewport.Model, footer func(off, lh int) []string) string {
	full := drawerH(open, h)
	lh, off := full, followOffset(n, full, sel)
	var foot []string
	if footer != nil {
		k := 0
		for range 3 { // pass 0 measures the unreserved window; two passes settle
			off = followOffset(n, full-k, sel)
			foot = footer(off, full-k)
			if len(foot) == k {
				break
			}
			if k = len(foot); k >= full {
				k, foot = 0, nil
				off = followOffset(n, full, sel)
				break
			}
		}
		foot = foot[:min(len(foot), k)]
		lh = full - len(foot)
		off = followOffset(n, lh, sel)
	}
	win := make([]string, 0, lh)
	for i := off; i < min(off+lh, n); i++ {
		win = append(win, line(i))
	}
	list := strings.Join(win, "\n")
	if len(foot) > 0 { // the footer is the list area's last rows
		list += strings.Repeat("\n", max(0, lh-1-strings.Count(list, "\n"))) + "\n" + strings.Join(foot, "\n")
	}
	if !open {
		return list
	}
	pad := strings.Repeat("\n", max(0, full-1-strings.Count(list, "\n")))
	return list + pad + "\n" + dimSt.Render(strings.Repeat("─", w)) + "\n" + vp.View()
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
func drawRows(idx []string, line func(i int) (left, right string), shade map[int]bool, sel, selRow int, open bool, w, h int, vp viewport.Model, footer func(off, lh int) []string) string {
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
	}, sel, open, w, h, vp, footer)
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
		if b.Status != "closed" && b.Status != "deferred" && !b.Event {
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
// twins (ADR 0180 §4) and that row's Parent chain. in maps a row; a tree row's group
// is the last header above it.
func shadeSet[R any](parent func(string) string, rows []R, sel int, full []R, in func(R) shadeIn) map[int]bool {
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
	chain := map[string]bool{}
	for id := parent(s.id); id != "" && !chain[id]; id = parent(id) {
		chain[id] = true
	}
	for i, r := range rs {
		if twin := r.id == s.id && (r.act != s.act || r.live != s.live); i != sel && (twin || chain[r.id] || !s.head && r.head && grp != "" && r.grp == grp) {
			set[i] = true
		}
	}
	if s.head && !slices.ContainsFunc(rs, func(r shadeIn) bool { return r.act && r.id == s.id }) {
		return map[int]bool{} // a plain group header shades nothing
	}
	return set
}

// parentOf is bead id's Parent ("" for none or an unknown id).
func (g graphData) parentOf(id string) string { return g.beadByID(id).Parent }

// band is rows split by leafState into the three Actionable bands (Spec D1), each in
// beadCmp order: waiting on the human (sTurn, sAsk), the takeable frontier (sReady), then
// the paused band (sPaused with an own paused:* label; an inherited-only pause is out).
// Every other state is out.
func band[R any](g graphData, rows []R, bead func(R) *bd.Bead) (wait, ready, paused []R) {
	for _, r := range rows {
		switch g.leafState(bead(r)) {
		case sTurn, sAsk:
			wait = append(wait, r)
		case sReady:
			ready = append(ready, r)
		case sPaused:
			if bd.Paused(*bead(r)) {
				paused = append(paused, r)
			}
		}
	}
	cmp := func(a, b R) int { return beadCmp(*bead(a), *bead(b)) }
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
