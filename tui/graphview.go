package tui

// The Graph tab's rendering (FR14, FR22, FR31): glyphs, rows and detail view.
// Rendering reads only the shell's loaded data and runs no subprocess
// (ADR 0101).

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/loop"
	"jayulabs.org/mugicrew/registry"
	"jayulabs.org/mugicrew/session"
)

var (
	softSt     = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	closedSty  = lipgloss.NewStyle().Foreground(dim).Strikethrough(true)
	selMarker  = lipgloss.NewStyle().Foreground(lipgloss.Color("111")).Bold(true)
	sectionSt  = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Bold(true) // the Initiatives, No Initiative and Backlog headers
	labelSt    = softSt
	deadSty    = warnSt.Bold(true)
	blockedSty = deadSty
	pinkFg     = lipgloss.NewStyle().Foreground(lipgloss.Color("210"))
	blockerMk  = pinkFg.Bold(true)
	selRow     = lipgloss.NewStyle().Background(lipgloss.Color("25")).Foreground(lipgloss.Color("15")).Bold(true) // the selection, across the list's full width (FR14); blue so it reads apart from ancRow (FR15)
	pickRow    = lipgloss.NewStyle().Background(lipgloss.Color("237"))                                            // a picker's selected item, across the box's inner width; no foreground, so the item keeps its own
	ancRow     = lipgloss.NewStyle().Background(lipgloss.Color("236"))                                            // an ancestor of the selection (FR8; gray, unlike the blue selRow)
	// The pause colours (ADR 0057 §1, ADR 0078): yellow the loop's self-clearing
	// foreign-hold, red a failing pause, orange an abandoned effort, darker orange
	// recover's fence, tan a Checkup's fence, cyan a stranded integrate, blue an unrunnable profile,
	// magenta the human's manual pause; dim magenta a pause inherited (ADR 0084).
	pauseSty = map[string]lipgloss.Style{
		bd.PausedForeignHold:       lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		bd.PausedFailing:           warnSt,
		bd.PausedAbandoned:         lipgloss.NewStyle().Foreground(lipgloss.Color("208")),
		bd.PausedRecovering:        lipgloss.NewStyle().Foreground(lipgloss.Color("172")),
		bd.PausedCheckup:           lipgloss.NewStyle().Foreground(lipgloss.Color("179")),
		bd.PausedManual:            lipgloss.NewStyle().Foreground(lipgloss.Color("213")),
		bd.PausedIntegrateConflict: lipgloss.NewStyle().Foreground(lipgloss.Color("44")),
		bd.PausedNotBuilt:          lipgloss.NewStyle().Foreground(lipgloss.Color("220")),
		bd.PausedProfile:           lipgloss.NewStyle().Foreground(lipgloss.Color("75")),
		bd.PausedContext:           lipgloss.NewStyle().Foreground(lipgloss.Color("141")),
	}
	inheritSty = lipgloss.NewStyle().Foreground(lipgloss.Color("127"))

	// kindColors is from the kind colour picker prototype (picked by eye, dark background).
	kindColors = map[contract.Kind]lipgloss.Color{
		// defog
		contract.KindSignal: "186", contract.KindSurvey: "117", contract.KindGrilling: "202",
		contract.KindResearch: "60", contract.KindPrototype: "170",
		// spine
		contract.KindChart: "68", contract.KindChartConflict: "89", contract.KindSpec: "49",
		contract.KindCheckpoint: "180",
		// execution
		contract.KindBuild: "224", contract.KindReview: "108", contract.KindCritique: "167",
		contract.KindConflict: "88",
		// gates
		contract.KindApproval: "107", contract.KindPivot: "160", contract.KindRollback: "94",
		// hitl misc
		contract.KindTask: "248", contract.KindDiscussion: "246", contract.KindCheckup: "246",
		// formerly fallback, now their own
		contract.KindSurgery: "93", contract.KindInitiative: "255", contract.KindLanding: "209",
	}
)

// kindTag renders a bead's kind: a Container as a chip (black bold UPPERCASE on
// the kind colour), every other bead as plain coloured UPPERCASE.
func kindTag(b *bd.Bead) string {
	c, ok := kindColors[b.Kind]
	if !ok {
		c = "244"
	}
	name := strings.ToUpper(string(b.Kind))
	if b.Container {
		return kept(lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(c).Bold(true).Padding(0, 1).Render(name))
	}
	if name == "" {
		name = "—"
	}
	return kept(lipgloss.NewStyle().Foreground(c).Bold(true).Render(name))
}

// kindTagW is lipgloss.Width(kindTag(b)) from the kind text alone: a Container chip
// adds its 2 columns of padding, an empty kind reads as the 1-column —.
func kindTagW(b *bd.Bead) int {
	switch {
	case b.Container:
		return lipgloss.Width(strings.ToUpper(string(b.Kind))) + 2
	case b.Kind == "":
		return 1
	}
	return lipgloss.Width(strings.ToUpper(string(b.Kind)))
}

// stateGlyph renders a bead's status as a glyph, except a blocked bead reads as the
// red ⊘ and an open paused bead as ‖ in its cause's colour (ADR 0057 §1); with no
// own cause, an inherited pause is a dim magenta ‖ (ADR 0084).
func (g graphData) stateGlyph(b *bd.Bead) string {
	if g.blocked(b) {
		return kept(blockedSty.Render("⊘"))
	}
	switch b.Status {
	case "closed":
		return dimSt.Render("✓")
	case "in_progress":
		return kept(lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render("◐"))
	default:
		if cs := bd.PausedCauses(*b); len(cs) > 0 && b.Status == "open" {
			return kept(pauseSty[cs[0]].Render("‖")) // manual's colour wins; ponytail: an unknown cause renders unstyled
		}
		if b.Status == "open" && g.inheritedPause(b) != "" {
			return kept(inheritSty.Render("‖"))
		}
		return softSt.Render("○")
	}
}

// blocked reports whether b is status=blocked, or not closed with an Ordering
// blocker still open.
func (g graphData) blocked(b *bd.Bead) bool {
	return b.Status == "blocked" || (b.Status != "closed" && len(g.heldBy(b)) > 0)
}

// heldBy is b's own open blockers plus the open blockers of its nearest ancestor
// container that holds a freezing hold (ADR 0270): a Chart with any open blocker, or
// a Checkpoint with one whose kind doesn't admit a land-only hold (loop.Readmit).
// Walks like inheritedPause; a land-only Checkpoint's members stay unheld.
func (g graphData) heldBy(b *bd.Bead) []*bd.Bead {
	out := g.openBlockers(b)
	seen := map[string]bool{b.ID: true}
	for id := b.Parent; id != "" && !seen[id]; {
		seen[id] = true
		anc := g.beadByID(id)
		if anc.Status != "closed" {
			for _, h := range g.openBlockers(anc) {
				if anc.Kind == contract.KindChart || (anc.Kind == contract.KindCheckpoint && !h.Kind.AdmitsLandOnlyHold()) {
					return append(out, h)
				}
			}
		}
		id = anc.Parent
	}
	return out
}

// openBlockers returns b's non-closed Ordering targets in edge order; a target
// missing from the snapshot counts as closed, as in loop.Readmit: the default set
// (ADR 0232, tui-on-demand-closed-beads…) loads every non-closed bead.
// ponytail: linear beadIndex per edge per rendered row; index by ID if the graph grows large.
func (g graphData) openBlockers(b *bd.Bead) []*bd.Bead {
	var out []*bd.Bead
	for _, id := range b.BlockedBy {
		if i := g.beadIndex(id); i >= 0 && g.beads[i].Status != "closed" {
			out = append(out, &g.beads[i])
		}
	}
	return out
}

// verdict is a row's effort liveness verdict (ADR 0032 §5).
type verdict int

const (
	na    verdict = iota // not in_progress, or HITL: no verdict
	alive                // the probe found reconcile/claude carrying the bead id
	dead                 // an in_progress AFK effort, the probe found nothing
)

func (g graphData) liveOf(b *bd.Bead) verdict {
	switch {
	case !loop.IsActiveAFKEffort(*b):
		return na
	case g.live[b.ID]:
		return alive
	default:
		return dead
	}
}

// livenessDot is the liveness column after the status glyph, so ◐○ reads "claimed,
// but nothing is running" (ADR 0032 §5).
func livenessDot(l verdict) string {
	switch l {
	case alive:
		return kept(aliveSt.Render("●"))
	case dead:
		return kept(deadSty.Render("○"))
	}
	return dimSt.Render("·")
}

// sessionLook is how a session renders (ADR 0038 §1–3, ADR 0067 §2, ADR 0099).
type sessionLook struct {
	sty         lipgloss.Style
	glyph, word string
}

var (
	sessionLooks = map[session.State]sessionLook{
		session.Alive:  {aliveSt, "●", "alive"},
		session.Exited: {deadSty, "✕", "exited"},
	}
	doneLook  = sessionLook{aliveSt.Bold(true), "●", "done — /exit"}
	waitLooks = map[session.Waiting]sessionLook{
		session.WaitTurn: {waitSt, "●", "your turn"},
		session.WaitAsk:  {waitSt, "●", "needs approval"},
	}
)

// humanWait is the one TUI notion of waiting on the human: s's waiting bit when it is alive,
// turn or ask, its bead not closed, no Auto-exit armed (an armed countdown reads as live,
// ADR 0111) and its close not running (Closing reads as live, ADR 0108); "" otherwise.
// @waiting itself stays turn for hitl-run's clock.
func humanWait(s session.Info, closed bool) session.Waiting {
	if s.State != session.Alive || closed || !s.AutoExit.IsZero() || s.Closing {
		return ""
	}
	if s.Waiting == session.WaitTurn || s.Waiting == session.WaitAsk {
		return s.Waiting
	}
	return ""
}

// lookOf is b's session look, and whether it has a session at all. Precedence,
// lowest to highest: alive < waiting < armed Auto-exit or Closing (alive) < done < exited; derived each render.
func (g graphData) lookOf(b *bd.Bead) (sessionLook, bool) {
	if g.done(b) {
		return doneLook, true
	}
	if l, ok := g.waitLook(b); ok {
		return l, true
	}
	l, ok := sessionLooks[g.sessions[b.ID].State]
	return l, ok
}

func (g graphData) waitLook(b *bd.Bead) (sessionLook, bool) {
	l, ok := waitLooks[humanWait(g.sessions[b.ID], g.done(b))]
	return l, ok
}

// closing is an alive session whose hitl-run close is running (ADR 0108 §2).
func (g graphData) closing(id string) bool {
	s := g.sessions[id]
	return s.State == session.Alive && s.Closing
}

// done is an alive session whose bead is closed (ADR 0067 §4).
func (g graphData) done(b *bd.Bead) bool {
	return g.sessions[b.ID].State == session.Alive && b.Status == "closed"
}

func (g graphData) rowDot(b *bd.Bead) string {
	if l, ok := g.lookOf(b); ok {
		return kept(l.sty.Render(l.glyph))
	}
	return livenessDot(g.liveOf(b))
}

// sessionLine is the detail's session field value, and whether b has a session.
func (g graphData) sessionLine(b *bd.Bead) (string, bool) {
	l, ok := g.lookOf(b)
	if !ok {
		return "", false
	}
	s := g.sessions[b.ID]
	word := l.sty.Render(l.word)
	switch {
	case s.State == session.Exited && s.Signal != "":
		return word + softSt.Render(" (signal="+s.Signal+")"), true
	case s.State == session.Exited:
		return word + softSt.Render(fmt.Sprintf(" (rc=%d)", s.RC)), true
	case s.Worktree == "": // a repo-root leaf runs in no worktree
		return word, true
	}
	return word + softSt.Render(" · "+s.Worktree), true
}

// rowSuffix is a row's trailing chrome: its age, then the in-flight markers of
// ADR 0107 (launching…, a pending write's verb), closing… (ADR 0108 §3), the
// armed auto-exit, done — /exit (ADR 0067 §2) or the waiting word (ADR 0099): all
// map lookups, never a subprocess (ADR 0101).
func (g graphData) rowSuffix(b *bd.Bead) string {
	if g.launching[b.ID] {
		return ageSuffix(b) + "  " + aliveSt.Render("launching…")
	}
	if verb, ok := g.pending[b.ID]; ok {
		return ageSuffix(b) + "  " + aliveSt.Render(verb+"…")
	}
	if g.closing(b.ID) {
		return ageSuffix(b) + "  " + aliveSt.Render("closing…")
	}
	if s := g.sessions[b.ID]; s.State == session.Alive && !s.AutoExit.IsZero() {
		return ageSuffix(b) + "  " + aliveSt.Render("auto-exit in "+coarseLeft(time.Until(s.AutoExit))+"…")
	}
	if g.done(b) {
		return ageSuffix(b) + "  " + doneLook.sty.Render(doneLook.word)
	}
	if l, ok := g.waitLook(b); ok {
		return ageSuffix(b) + "  " + l.sty.Render(l.word)
	}
	if _, ok := g.lookOf(b); !ok && restsClaimed(b.Kind) && b.Status == "in_progress" {
		return "  " + dimSt.Render("idle — enter to resume")
	}
	return ageSuffix(b)
}

// ageSuffix is the dim trailing age-since-claim on in_progress rows, HITL included.
func ageSuffix(b *bd.Bead) string {
	if b.Status != "in_progress" {
		return ""
	}
	return "  " + softSt.Render(HumanAge(b.Started))
}

// coarseLeft is an Auto-exit countdown at row grain: whole minutes, then seconds.
func coarseLeft(d time.Duration) string {
	if d >= time.Minute {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%ds", max(0, int(d.Seconds())))
}

// livenessLine is the detail's liveness field value, in words.
func livenessLine(l verdict, b *bd.Bead) string {
	claimed := ""
	if !b.Started.IsZero() {
		claimed = softSt.Render(" · claimed " + HumanAge(b.Started) + " ago")
	}
	switch l {
	case alive:
		return aliveSt.Render("alive") + claimed
	case dead:
		return deadSty.Render("dead") + softSt.Render(" — no reconcile/claude process") + claimed
	}
	return softSt.Render("— (HITL: no verdict)")
}

// HumanAge renders a coarse age (5h / 2d).
func HumanAge(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := clock().Sub(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// The master list

// selBlockers is the set of the selection's open blockers, pinked in the list.
func (g graphTab) selBlockers() map[string]bool { return g.blockerSet(g.selected()) }

// gutter is a row's 1-column blocker mark; a folded group hides its members' beads.
func (g graphTab) gutter(r row, blockers map[string]bool, folds map[string]bool) string {
	var hidden []bd.Bead
	if r.header && folds[r.collapseKey] {
		for _, gr := range g.groups {
			if gr.key == r.collapseKey {
				hidden = append(hidden, gr.beads()...)
			} else {
				hidden = append(hidden, gr.nested[r.collapseKey]...) // a nested head under an Initiative
			}
		}
	}
	return blockerGutter(blockers, r.beadID, hidden)
}

// masterRow renders one master line after its gutter: a group header (with fold
// caret) or an indented member leaf; hot pinks the title of an open blocker.
func (g graphTab) masterRow(r row, width int, hot bool) string {
	pre, title, suf := g.masterParts(r)
	if r.beadID == "" { // unparented header: no title to fit
		return pre + suf
	}
	title = trunc(title, width-lipgloss.Width(pre)-lipgloss.Width(suf))
	if !r.header {
		st, _ := leafTitleStyle(g.beadByID(r.beadID), hot)
		title = st.Render(title)
	} else {
		title = containerTitle(g.beadByID(r.beadID), hot, title)
	}
	return pre + title + suf
}

// masterParts splits a master line into the chrome before and after its
// (unfitted, unstyled) title.
func (g graphTab) masterParts(r row) (pre, title, suf string) {
	marker := "  "
	if r.sel == g.sel {
		marker = selMarker.Render("❯ ")
	}
	branch := ""
	if r.depth > 0 {
		branch = softSt.Render(r.rail + "├ ")
		if r.last {
			branch = softSt.Render(r.rail + "└ ")
		}
	}
	if r.header {
		caret := "  " // a blank slot keeps sibling glyphs in one column
		live := ""    // the folded header's "N live" trailer (Spec D7)
		if r.kids {
			caret = "▾ "
			if g.folds()[r.collapseKey] {
				caret = "▸ "
				if n := g.liveN[r.collapseKey]; n > 0 {
					live = " " + aliveSt.Render(fmt.Sprintf("%d live", n))
				}
			}
		}
		if r.beadID == "" { // unparented header
			if live != "" {
				live = " " + live // the two-space gap before the trailer
			}
			return marker + softSt.Render(caret+"Unparented"), "", live
		}
		h := g.beadByID(r.beadID)
		head, tail := g.sbrnParts(h, true, 0)
		pre = marker + branch + caret + head
		if h.Kind == contract.KindInitiative { // landed N/M and the live member (D10, FR40)
			landed, chains, _ := g.initiativeProgress(h.ID)
			return pre, chartName(h.Title), tail + live + " " + softSt.Render(fmt.Sprintf("landed %d/%d", landed, chains)) + g.rowSuffix(h)
		}
		closed, total := g.childCounts(r.beadID)
		suf = tail + live + " " + softSt.Render(fmt.Sprintf("%d/%d", closed, total)) + g.rowSuffix(h)
		return pre, chartName(h.Title), suf
	}
	b := g.beadByID(r.beadID)
	head, tail := g.sbrnParts(b, true, 0)
	return marker + branch + "  " + head, b.Title, tail + g.rowSuffix(b) // caret-width pad: glyph aligns with header siblings
}

// liveCounts is, per foldable header (by collapse key, "" for Unparented), the live
// beads beneath it in the rendered tree: an Alive session (waiting and done
// included) or an alive AFK effort, the header itself left out. Read off the whole
// non-Draft graph, so no filter changes it (Spec D6).
func (g graphTab) liveCounts(groups []group) map[string]int {
	count := func(bs []bd.Bead) (n int) {
		for i := range bs {
			if g.isLive(&bs[i]) {
				n++
			}
		}
		return n
	}
	out := map[string]int{}
	for _, gr := range groups {
		out[gr.key] = count(gr.beads())
		for h, kids := range gr.nested {
			out[h] = count(kids)
		}
	}
	return out
}

// isLive is whether b's row dot is a live ●: the session look first, else AFK liveness.
func (g graphData) isLive(b *bd.Bead) bool {
	if _, ok := g.lookOf(b); ok {
		return g.sessions[b.ID].State == session.Alive
	}
	return g.liveOf(b) == alive
}

// idSet is the set of bead ids that bead picks out of rows (nil picks none): a tab's
// Actionable rows as the act argument of liveBeads.
func idSet[R any](rows []R, bead func(R) *bd.Bead) map[string]bool {
	ids := map[string]bool{}
	for _, r := range rows {
		if b := bead(r); b != nil {
			ids[b.ID] = true
		}
	}
	return ids
}

// initiativesHeaderText is the detail of a queue tab's Initiatives section header.
const initiativesHeaderText = "The Initiative groups and No Initiative.\n\nenter does nothing here (space folds)."

// running is whether b's effort is Live, launching or launched but not yet seen alive.
func (g graphData) running(b *bd.Bead) bool {
	return g.isLive(b) || g.launching[b.ID] || !g.launched[b.ID].IsZero()
}

// liveBeads are the beads of bs that are live (isLive) or being launched (launching, or
// launched and awaiting its reload) and not in the tab's Actionable rows (act, bead
// ids), in beadCmp order: a queue tab's Live section rows (no bands).
func (g graphData) liveBeads(bs []*bd.Bead, act map[string]bool) []*bd.Bead {
	out := slices.DeleteFunc(slices.Clone(bs), func(b *bd.Bead) bool {
		return act[b.ID] || !g.running(b)
	})
	slices.SortStableFunc(out, func(a, b *bd.Bead) int { return beadCmp(*a, *b) })
	return out
}

// initiativeProgress is an Initiative's landed N/M over its member chains (those
// whose Checkpoint is closed) and the live member's title: the first open,
// unblocked one in phase order (Spec D10, FR40). Read off the whole graph, so h
// never changes it.
func (g graphTab) initiativeProgress(id string) (landed, chains int, live string) {
	cs := phaseOrder(g.memoOf().members[id])
	for _, c := range cs {
		if slices.ContainsFunc(c, func(b bd.Bead) bool { return b.Kind == contract.KindCheckpoint && b.Status == "closed" }) {
			landed++
		}
		for _, b := range c {
			if live == "" && b.Open() && b.Status != "deferred" && len(g.openBlockers(&b)) == 0 {
				live = b.Title
			}
		}
	}
	return landed, len(cs), live
}

// childCounts returns (closed, total) member beads of a container, leaving out
// hidden Drafts: it is what the Graph shows. Gates use gateChildCounts.
func (g graphTab) childCounts(id string) (closed, total int) {
	c := g.memoOf().counts[id]
	return c[0], c[1]
}

// gateChildCounts counts Drafts too: a Draft is still open for draining.
func (g graphData) gateChildCounts(id string) (closed, total int) { return childCountsOf(g.beads, id) }

func childCountsOf(beads []bd.Bead, id string) (closed, total int) {
	for _, b := range beads {
		if b.Parent == id && b.Kind != "" && !b.Container {
			total++
			if b.Status == "closed" {
				closed++
			}
		}
	}
	return
}

// Geometry

// followOffset keeps the selected row visible inside a height-row window over
// total rows, centring it (clamped so the ends pin to top/bottom).
func followOffset(total, height, sel int) int {
	if total <= height {
		return 0
	}
	return min(max(sel-height/2, 0), total-height)
}

// line is master row r as drawn: its gutter and fitted text, the selected row
// highlighted across the full width, a filter's context row dimmed, an ancestor of the
// selection shaded. It formats the one row, so a caller draws only its window.
func (g graphTab) line(r row, blockers, folds, anc map[string]bool) string {
	cw := g.w
	gu := g.gutter(r, blockers, folds)
	l := ansi.Truncate(gu+g.masterRow(r, cw-lipgloss.Width(gu), blockers[r.beadID]), cw, "")
	switch {
	case r.sel == g.sel:
		l = selectRow(l, cw)
	case r.context:
		l = dimSt.Render(ansi.Strip(l))
	}
	if anc[r.sel] {
		l = shadeRow(l, cw, ancRow)
	}
	return l
}

// ancestors is the set of row keys above the selection, walking Parent (a cycle
// stops the walk).
func (g graphTab) ancestors() map[string]bool {
	anc := map[string]bool{}
	for id := g.beadByID(g.sel).Parent; id != "" && !anc[id]; id = g.beadByID(id).Parent {
		anc[id] = true
	}
	return anc
}

// shadeRow paints st's background across a w-wide line, re-applying it after every
// inner reset so it survives mid-row styles.
func shadeRow(l string, w int, st lipgloss.Style) string {
	bg := strings.TrimSuffix(st.Render(""), "\x1b[0m")
	return bg + strings.ReplaceAll(l, "\x1b[0m", "\x1b[0m"+bg) + bg + strings.Repeat(" ", max(w-lipgloss.Width(l), 0)) + "\x1b[0m"
}

var sgrRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// keepOn and keepOff are SGRs no terminal acts on (proportional spacing on and off),
// bracketing a span whose colour the selection keeps: the kind badge, the priority,
// the state glyph and the liveness dot. selectRow keeps the styles inside a bracket
// by where they come from, never by their SGR.
const (
	keepOn  = "\x1b[26m"
	keepOff = "\x1b[50m"
)

// kept brackets s, a styled span, for selectRow; an unstyled s has nothing to keep.
func kept(s string) string {
	if !strings.Contains(s, "\x1b[") {
		return s
	}
	return keepOn + s + keepOff
}

// selectRow paints the selection across a w-wide line: selRow's background and bold,
// every foreground style dropped except the spans a composer bracketed with kept, which
// stay intact under the background (a Container chip keeps its own).
func selectRow(l string, w int) string {
	bg := strings.TrimSuffix(selRow.Render(""), "\x1b[0m")
	var out strings.Builder
	out.WriteString(bg)
	last, keep, open := 0, false, false
	for _, m := range sgrRe.FindAllStringIndex(l, -1) {
		out.WriteString(l[last:m[0]])
		last = m[1]
		switch seq := l[m[0]:m[1]]; {
		case seq == keepOn:
			keep = true
		case seq == keepOff:
			keep = false
		case seq == "\x1b[0m":
			if open {
				out.WriteString(seq + bg)
			}
			open = false
		case keep:
			out.WriteString(seq)
			open = true
		}
	}
	out.WriteString(l[last:])
	return out.String() + strings.Repeat(" ", max(w-lipgloss.Width(l), 0)) + "\x1b[0m"
}

// The detail pane

// detailVP is the detail drawer's viewport (FR22): the selected bead's text wrapped
// to the drawer, scrolled to the stored offset. The geometry is the renderer's own,
// so update scrolls against exactly what view shows.
func (g graphTab) detailVP() viewport.Model {
	return drawerVP(g.open, g.w, g.h, g.detailText(), g.off)
}

// detailText is the selected bead's detail: the body (header fields, description),
// then the launch preview when there is one.
func (g graphTab) detailText() string {
	body := g.detailBody()
	if b := g.selected(); b != nil {
		if p := g.launchPreview(b); p != "" {
			return body + "\n\n" + p
		}
	}
	return body
}

// detailBody is detailText without the launch preview: the part the F10 bead pane
// shares with the drawer.
func (g graphTab) detailBody() string {
	b := g.selected()
	if b == nil {
		if g.sel == unparentedSel {
			return softSt.Render("Unparented — open Signals awaiting triage and Rollbacks awaiting Jinbe.\n\nSelect a Signal and press enter to launch Luffy to triage it, or a Rollback and press enter to run it.")
		}
		return ""
	}
	owner := "—"
	if e, err := registry.Lookup(b.Kind); err == nil {
		owner = e.Owner
	}
	mode := "AFK"
	if isHITL(b.Kind) {
		mode = "HITL"
	}
	var h strings.Builder
	fmt.Fprintf(&h, "%s  %s\n\n", g.sbrn(b, b.Title, !g.noDot, 0), softSt.Render(mode))
	field := func(k, v string) { h.WriteString(labelField(k, v) + "\n") }
	field("owner", owner)
	if b.Assignee != "" {
		field("claimed by", b.Assignee)
	}
	if b.Kind == contract.KindInitiative {
		landed, chains, live := g.initiativeProgress(b.ID)
		field("landed", fmt.Sprintf("%d/%d member chains", landed, chains))
		if live != "" {
			field("live", live)
		}
	} else if b.Container {
		closed, total := g.childCounts(b.ID)
		field("children", fmt.Sprintf("%d/%d closed", closed, total))
	}
	if p := g.beadByID(b.Parent); p.Kind == contract.KindInitiative {
		field("initiative", p.ID)
	} else if !b.Container && b.Parent != "" {
		field("chart", b.Parent)
	}
	if l := g.liveOf(b); l != na || (b.Status == "in_progress" && isHITL(b.Kind)) {
		field("liveness", livenessLine(l, b))
	}
	if v, ok := g.sessionLine(b); ok {
		field("session", v)
	}
	if l := g.pauseLine(b); l != "" {
		field("paused", l)
	}
	if g.efforts[b.ID] { // where its unintegrated work lives (ADR 0104 §7)
		field("effort", git.EffortBranch(b.ID))
	}
	field("age", HumanAge(b.Created))
	if b.Kind == contract.KindPivot && b.Open() {
		// The lead's session triages its whole Checkpoint (FR22): name what it covers.
		isLead := pivotLeads(g.beads)[pivotGate(g.beads, b.ID)].ID == b.ID
		if others := otherOpenPivots(g.beads, *b); isLead && len(others) > 0 {
			fmt.Fprintf(&h, "\n%s\n", labelSt.Render(fmt.Sprintf("+%d other open pivots:", len(others))))
			for _, o := range others {
				fmt.Fprintf(&h, "  · %s\n", trunc(o.Title, max(g.w-6, 1)))
			}
		}
	}
	parts := []string{strings.TrimRight(h.String(), "\n")}
	if body := g.beadBody(b, true); body != "" {
		parts = append(parts, body)
	}
	return strings.Join(parts, "\n\n")
}

// labelField is one header field: the key in labelSt, padded to the shared column, then v.
func labelField(k, v string) string { return labelSt.Render(fmt.Sprintf("%-11s", k)) + " " + v }

// beadBody is the body every bead drawer ends with (ADR 0265): the close line on a
// closed bead, then ROLLUP (ADR 0285), DESCRIPTION (when desc), NOTES, DESIGN, ACCEPTANCE and COMMENTS, each a
// labelled rule over its Markdown and only when non-empty. Static: it never calls bd.
func (g graphData) beadBody(b *bd.Bead, desc bool) string {
	var parts []string
	if b.Status == "closed" {
		r := b.CloseReason
		if r == "" {
			r = bd.DefaultCloseReason
		}
		parts = append(parts, labelField("closed", HumanAge(b.Closed)+" · "+r))
	}
	rule := func(label string) string {
		r := "── " + label + " "
		return labelSt.Render(r + strings.Repeat("─", max(drawerContentW(g.w)-lipgloss.Width(r), 0)))
	}
	section := func(label, text string) {
		if text != "" {
			parts = append(parts, rule(label)+"\n"+mdBody(text, g.w))
		}
	}
	parts = append(parts, rule("ROLLUP · all retained log")+"\n"+g.rollupLines(b))
	if desc {
		section("DESCRIPTION", b.Description)
	}
	section("NOTES", b.Notes)
	section("DESIGN", b.Design)
	section("ACCEPTANCE", b.AcceptanceCriteria)
	if b.CommentCount > 0 {
		parts = append(parts, rule(fmt.Sprintf("COMMENTS (%d)", b.CommentCount))+"\n"+g.commentsText(b))
	}
	return strings.Join(parts, "\n\n")
}

// oneLine is err's text on one line, runs of whitespace collapsed.
func oneLine(err error) string { return strings.Join(strings.Fields(err.Error()), " ") }

// commentsText is b's cached comments, oldest first, or the soft loading… / error line.
func (g graphData) commentsText(b *bd.Bead) string {
	e, ok := g.comments[commentKey{b.ID, b.CommentCount}]
	switch {
	case e.err != nil:
		return softSt.Render("couldn't load comments: " + oneLine(e.err))
	case !ok || e.loading:
		return softSt.Render("loading…")
	}
	var cs []string
	for _, c := range e.cs {
		cs = append(cs, labelSt.Render(c.Author+" · "+HumanAge(c.Created))+"\n"+mdBody(c.Text, g.w))
	}
	return strings.Join(cs, "\n\n")
}

// pauseLine is the detail's paused field: every cause, manual first, each in its
// colour, then "manual via <nearest-ancestor>" on a non-closed bead (ADR 0084).
func (g graphData) pauseLine(b *bd.Bead) string {
	var vs []string
	for _, c := range bd.PausedCauses(*b) {
		vs = append(vs, pauseSty[c].Render(contexts.PauseNote(*b, c)))
	}
	if b.Status != "closed" {
		if anc := g.inheritedPause(b); anc != "" {
			vs = append(vs, inheritSty.Render("manual via "+anc))
		}
	}
	return strings.Join(vs, softSt.Render(" · "))
}

// inheritedPause names b's nearest paused:manual ancestor in the loaded graph, ""
// for none.
func (g graphData) inheritedPause(b *bd.Bead) string {
	anc, _ := bd.PausedAncestor(*b, func(id string) (bd.Bead, error) { return *g.beadByID(id), nil })
	return anc
}

// launchPreview shows the session, state or intervene note for a bead, "" when
// there is none (an open HITL bead's drawer ends at its body). Static: no
// Launch call, so render runs no subprocess (ADR 0101).
func (g graphTab) launchPreview(b *bd.Bead) string {
	if g.sessions[b.ID].Has() { // a session wins enter, whatever the kind or status
		return softSt.Render("SESSION — ") + selMarker.Render("enter") + softSt.Render(" attaches (resumes if exited).")
	}
	switch g.liveOf(b) {
	case alive:
		return softSt.Render(string(b.Kind) + " is running. x aborts it.")
	case dead:
		return softSt.Render(string(b.Kind) + "'s effort is dead. x recovers it.")
	}
	if strandable(b) { // enter refuses (launchWhy): the preview agrees
		return softSt.Render(claimLine(b))
	}
	if b.Status == "in_progress" && isHITL(b.Kind) {
		return softSt.Render(string(b.Kind)+" is idle — ") + selMarker.Render("enter") + softSt.Render(" resumes it against its claim.")
	}
	if b.Container && b.Kind != contract.KindInitiative { // an Initiative's launch is the ideate/graph enter path, not a container watch
		if b.Kind == contract.KindCheckpoint {
			if g.runNowLabel(b) != "" {
				return softSt.Render("CHECKPOINT — approved & at HEAD. Press ") + selMarker.Render("enter") +
					softSt.Render(" to land it now (Jinbe), or wait for the loop. Use edit to intervene.")
			}
			if held := g.landHeldBy(b); len(held) > 0 {
				return softSt.Render("CHECKPOINT — approved; land held by open blocker: " + strings.Join(held, ", ") + ". Use edit to intervene.")
			}
			return softSt.Render("CHECKPOINT — land-watched. Jinbe lands it once an Approval\ncloses at the branch HEAD. Use edit to intervene.")
		}
		if g.runNowLabel(b) != "" {
			return softSt.Render("CONTAINER — drained & defogged. Press ") + selMarker.Render("enter") +
				softSt.Render(" to run its speccing reconcile now, or wait for the loop. Use edit to intervene.")
		}
		return softSt.Render("CONTAINER — drain-watched. The loop spawns the Spec when the\nfrontier clears (chart:defogged). Use edit to intervene.")
	}
	return ""
}

// launchOwner resolves a kind to its registry owner and checks the owner is wired
// in crew: the pre-Launch checks the enter-time launch path makes, I/O-free.
func launchOwner(crew map[string]contract.Crewmate, k contract.Kind) (string, error) {
	e, err := registry.Lookup(k)
	if err != nil {
		return "", err
	}
	if _, ok := crew[e.Owner]; !ok {
		return "", fmt.Errorf("no crewmate %q wired for kind %q", e.Owner, k)
	}
	return e.Owner, nil
}

// pauseRefusesRunNow is run-now's pause gate: an own manual, abandoned or
// recovering pause, or an inherited one (anc, "" for none), refuses.
func pauseRefusesRunNow(b bd.Bead, anc string) bool {
	return anc != "" || slices.ContainsFunc(pauseCauses, func(c pauseCause) bool { return c.refuses && bd.PausedBy(b, c.cause) })
}

// pauseCause is one paused: cause in precedence order (ADR 0078, 0104 §4, 0123 §3),
// the one table pauseRefusesRunNow, runNowHint and runNowLabel read. refuses: run-now
// won't touch the bead; else retry (when set) is its run-now label.
type pauseCause struct {
	cause   string
	refuses bool
	retry   string
	hint    func(b bd.Bead) string // refuses: why run-now won't touch b
}

// pauseHint is the first refusing cause's hint on b, "" if none.
func pauseHint(b bd.Bead) string {
	for _, c := range pauseCauses {
		if c.hint != nil && bd.PausedBy(b, c.cause) {
			return c.hint(b)
		}
	}
	return ""
}

var pauseCauses = []pauseCause{
	{cause: bd.PausedManual, refuses: true, hint: func(b bd.Bead) string {
		id := b.ID
		return id + " is paused by hand — press p or run 'bd label remove " + id + " paused:manual' first"
	}},
	{cause: bd.PausedAbandoned, refuses: true, hint: func(b bd.Bead) string {
		id := b.ID
		return id + ": abandoned effort may still be running; check, then 'bd label remove " + id + " paused:abandoned'"
	}},
	{cause: bd.PausedRecovering, refuses: true, hint: func(b bd.Bead) string {
		id := b.ID
		return id + " is being recovered, or a recover failed partway — re-run 'mugicrew recover " + id + "'"
	}},
	{cause: bd.PausedContext, refuses: true, hint: func(b bd.Bead) string {
		return b.ID + " is held: " + contexts.PauseNote(b, bd.PausedContext)
	}},
	{cause: bd.PausedIntegrateConflict, retry: "retry"},
	{cause: bd.PausedNotBuilt, retry: "retry"},
	{cause: bd.PausedFailing, retry: "retry"},
	{cause: bd.PausedProfile, retry: "retry"},
	{cause: bd.PausedForeignHold, retry: "retry hold"},
}

// landHeldBy is the titles of the open blockers holding a landable-looking
// Checkpoint's land per the last refresh (nil when it isn't held).
func (g graphData) landHeldBy(b *bd.Bead) []string {
	if !g.checkpointLooksLandable(b.ID) {
		return nil
	}
	var titles []string
	for _, h := range g.openBlockers(b) {
		titles = append(titles, cmp.Or(h.Title, h.ID))
	}
	return titles
}

// runNowLabel is the I/O-free run-now affordance (run/spec/land/retry…), "" when b
// does not look run-now-eligible from the loaded graph. A hint that may be one
// refresh stale; the press-time read is the authoritative gate.
func (g graphData) runNowLabel(b *bd.Bead) string {
	if b == nil || b.Status != "open" || pauseRefusesRunNow(*b, g.inheritedPause(b)) || g.memberHold(b) != "" {
		return ""
	}
	l := ""
	switch {
	case loop.IsAFKLeaf(*b):
		l = "run"
	case loop.IsDrainedChartCandidate(*b):
		if closed, total := g.gateChildCounts(b.ID); closed == total && len(g.heldChildren(b.ID)) == 0 {
			l = "spec"
		}
	case loop.IsCheckpointCandidate(*b):
		if g.checkpointLooksLandable(b.ID) && len(g.landHeldBy(b)) == 0 {
			l = "land"
		}
	}
	if l == "" {
		return ""
	}
	if i := slices.IndexFunc(pauseCauses, func(c pauseCause) bool { return c.retry != "" && bd.PausedBy(*b, c.cause) }); i >= 0 {
		return pauseCauses[i].retry
	}
	return l
}

// runNowWhy is "" when run-now would act on b, else why not: every refusal that needs
// no I/O, in the order runNow answers it before any blocked y/n. The press-time
// gate (runNowEligible, the pool, the land hold) still refuses after it passes.
func (g graphData) runNowWhy(b *bd.Bead) string {
	id, anc := b.ID, g.inheritedPause(b)
	switch {
	case g.busyWhy(b) != "":
		return g.busyWhy(b)
	case b.Status == "closed":
		return id + " is closed — nothing to launch or run"
	case g.liveOf(b) == alive:
		return id + " is running — t shows it · x aborts it"
	case b.Status == "in_progress":
		return id + "'s effort is not running — x recovers it"
	case pauseHint(*b) != "":
		return pauseHint(*b)
	case anc != "":
		return inheritedPauseHint(id, anc)
	case g.memberHold(b) != "":
		return g.memberHold(b)
	case g.runNowLabel(b) != "":
		return ""
	case loop.IsDrainedChartCandidate(*b):
		if held := g.heldChildren(id); len(held) > 0 {
			return chartHeldHint(id, held)
		}
		closed, total := g.gateChildCounts(id)
		return fmt.Sprintf("%s: Chart not drained — %d/%d children closed", id, closed, total)
	case loop.IsCheckpointCandidate(*b):
		if t := g.landHeldBy(b); len(t) > 0 {
			return id + ": " + landHeldNote(t)
		}
		if g.reviewDue(id) {
			return id + ": review due — the loop spawns it"
		}
		return checkpointNotReadyHint(id)
	}
	if b.Status == "blocked" {
		return id + " is blocked — enter runs a bead only once its blockers clear"
	}
	return id + " has nothing for enter to run"
}

// memberHold is why a Checkpoint's member can't run, "" when nothing holds it: an
// open Surgery on a Critique's Checkpoint (loop.SurgeryHold's snapshot twin; the
// press-time surgeryHold stays authoritative), or an open blocker of the
// Checkpoint whose kind doesn't admit a land-only hold (loop.Readmit, ADR 0269).
func (g graphData) memberHold(b *bd.Bead) string {
	i := g.beadIndex(b.Parent)
	if b.Parent == "" || i < 0 || g.beads[i].Kind != contract.KindCheckpoint {
		return ""
	}
	cp := &g.beads[i]
	if b.Kind == contract.KindCritique {
		for _, s := range g.beads {
			if s.Parent == cp.ID && s.Kind == contract.KindSurgery && s.Open() {
				return b.ID + ": held by open Surgery " + s.ID + " — ending the Surgery releases it"
			}
		}
	}
	for _, h := range g.openBlockers(cp) {
		if !h.Kind.AdmitsLandOnlyHold() {
			return cp.ID + " is gated by " + cmp.Or(h.Title, h.ID) + " (" + h.ID + ") — not running"
		}
	}
	return ""
}

// reviewDue is the I/O-free half of bd.ReviewDueOver: the Checkpoint's members are
// drained, one exists, no pivot is open and no Approval pins the tip. An unknown
// tip (no read, or a failed one) pins nothing, so only a known at-tip Approval vetoes.
func (g graphData) reviewDue(id string) bool {
	var kids []bd.Bead
	for _, k := range g.beads {
		if k.Parent == id {
			kids = append(kids, k)
		}
	}
	return g.reviewDueOver(g.beadByID(id), kids)
}

// reviewDueOver is reviewDue over cp's already-collected children.
func (g graphData) reviewDueOver(cp *bd.Bead, kids []bd.Bead) bool {
	members := 0
	tip := g.cpTips[cp.ID]
	for _, k := range kids {
		if pin, ok := git.ParseCommit(k.Description); k.Kind == contract.KindApproval && ok && tip != "" && pin == tip && (k.Open() || k.ApprovedClosed()) {
			return false
		}
		if bd.IsMember(k.Kind) {
			members++
		}
	}
	for _, h := range g.openBlockers(cp) {
		if h.Kind == contract.KindPivot {
			return false
		}
	}
	return members > 0 && bd.MembersDrainedOver(kids, g.efforts)
}

// heldChildren is a container's closed children still holding effort/<id> per the
// last refresh: the I/O-free twin of bd.DrainBlockers.
func (g graphData) heldChildren(id string) []bd.Bead {
	var kids []bd.Bead
	for _, b := range g.beads {
		if b.Parent == id {
			kids = append(kids, b)
		}
	}
	_, held := bd.DrainBlockersOver(kids, g.efforts)
	return held
}

// checkpointLooksLandable is the I/O-free half of loop.CheckpointReady: an approved-closed
// kind:approval child and no open kind:conflict / kind:critique child.
func (g graphData) checkpointLooksLandable(id string) bool {
	hasApprovedApproval := false
	for _, b := range g.beads {
		if b.Parent != id {
			continue
		}
		switch b.Kind {
		case contract.KindConflict, contract.KindCritique:
			if b.Status == "open" {
				return false
			}
		case contract.KindApproval:
			if b.ApprovedClosed() {
				hasApprovedApproval = true
			}
		}
	}
	return hasApprovedApproval
}

// view is the Graph body: the list, and while detail is open the list's top half
// over a rule and the detail drawer (ADR 0171 §2).
func (g graphTab) view(w, h int) string {
	head := g.header(w)
	if head == "" {
		return g.viewBody(w, h)
	}
	return " " + ansi.Truncate(head, w-1, "") + "\n" + g.viewBody(w, max(h-1, 1))
}

func (g graphTab) viewBody(w, h int) string {
	g.w, g.h = w, h
	rows := g.rows() // once per frame; only the drawn window is formatted
	if len(rows) == 0 {
		if g.q.active() || g.typing {
			return dimSt.Render(" Graph — no matching beads")
		}
		return dimSt.Render(" Graph — no beads yet")
	}
	blockers, folds, anc := g.selBlockers(), g.folds(), g.ancestors()
	sel := 0
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.beadID
		if r.sel == g.sel {
			sel = i
		}
	}
	var footer func(off, lh int) []string
	if b := g.selected(); b != nil {
		footer = blockerFooter(g.graphData, g.openBlockers(b), ids, w)
	}
	return drawerWin(len(rows), func(i int) string { return g.line(rows[i], blockers, folds, anc) }, sel, g.open, w, h, g.detailVP(), footer)
}

var (
	prio0Sty = warnSt.Bold(true)
	prio1Sty = lipgloss.NewStyle().Foreground(lipgloss.Color("217")) // not 210, the hot-title pink
)

// prioText is the plain P<n>; prio is it coloured: P0 bold warn red, P1 light red, the rest unstyled.
func prioText(p int) string { return "P" + strconv.Itoa(p) }

func prio(p int) string {
	switch p {
	case 0:
		return kept(prio0Sty.Render(prioText(p)))
	case 1:
		return kept(prio1Sty.Render(prioText(p)))
	}
	return prioText(p)
}

// prioTint is the badge background a member row takes from its container's priority
// cp: 203 for P0, 217 for P1, none otherwise.
func prioTint(cp int) (lipgloss.Color, bool) {
	switch cp {
	case 0:
		return "203", true
	case 1:
		return "217", true
	}
	return "", false
}

// tinted is st with prioTint(cp)'s background and a dark foreground, which the tint
// would otherwise hide; st unchanged when there is no tint.
func tinted(st lipgloss.Style, cp int) lipgloss.Style {
	if c, ok := prioTint(cp); ok {
		return st.Foreground(lipgloss.Color("16")).Background(c)
	}
	return st
}

// prioIn is prio for a member row of a container with priority cp.
func prioIn(p, cp int) string {
	if _, ok := prioTint(cp); !ok {
		return prio(p)
	}
	st := lipgloss.NewStyle()
	switch p {
	case 0:
		st = prio0Sty
	case 1:
		st = prio1Sty
	}
	return kept(tinted(st, cp).Render(prioText(p)))
}

// rowEnterWhy is why enter does nothing on a queue-tab bead row, "" when it
// launches, attaches or runs (ADR 0156 §5). b is nil on a header, summary or
// placeholder row; leaf is an open HITL leaf, the only row whose blocked or paused
// state refuses ahead of the shared gates (unlike Graph's confirm).
func (g graphData) rowEnterWhy(b *bd.Bead, leaf bool) string {
	switch {
	case b == nil:
		return "nothing to launch or run here — space folds"
	case !leaf: // the shared gates: runNow's, or launchWhy's for a session (closed members included)
		if g.launches(b) {
			return g.launchWhy(b)
		}
		return g.runNowWhy(b)
	}
	switch g.leafState(b) {
	case sBlocked:
		if bl := g.heldBy(b); len(bl) > 0 {
			return fmt.Sprintf("%s is blocked by %s (%s) — not launching", b.ID, bl[0].Title, bl[0].ID)
		}
		return b.ID + " is blocked — not launching"
	case sPaused:
		if i := slices.IndexFunc(b.Labels, func(l string) bool { return strings.HasPrefix(l, "paused:") }); i >= 0 {
			return fmt.Sprintf("%s is paused (%s) — not launching", b.ID, b.Labels[i])
		}
		return fmt.Sprintf("%s is paused by %s — not launching", b.ID, g.inheritedPause(b))
	}
	return g.launchWhy(b)
}

// rowBgShown is whether b bg shows on a row enter acts on: not on a live session,
// and an AFK row needs an exited one (ADR 0165 §5).
func (g graphData) rowBgShown(b *bd.Bead, leaf bool) bool {
	switch g.sessions[b.ID].State {
	case session.Alive:
		return false
	case session.Exited:
		return true
	}
	return isHITL(b.Kind) && leaf
}

// rowBgWhy is why b does nothing on the row: enter's refusals, then HITL-only.
func (g graphData) rowBgWhy(b *bd.Bead, leaf bool) string {
	if why := g.rowEnterWhy(b, leaf); why != "" {
		return why
	}
	if !g.launches(b) && !g.sessions[b.ID].Has() {
		return "b is HITL-only — enter runs it"
	}
	return ""
}

// rowEnterField is the detail's enter line: the row's verb, or why enter refuses.
func (g graphData) rowEnterField(b *bd.Bead, leaf bool) string {
	if why := g.rowEnterWhy(b, leaf); why != "" {
		return "refuses — " + why
	}
	return g.enterVerb(b)
}
