package tui

import (
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/session"
)

// WithMouse hands the model whether the program was built with mouse capture
// (main passes the resolved [tui] mouse). Every mouse path checks it first.
func (m Model) WithMouse(on bool) Model {
	m.mouseOn = on
	if m.wheelAcc == nil {
		m.wheelAcc = &wheelAcc{}
	}
	return m
}

// WithTheme hands the model the resolved [tui] theme at launch and installs that
// preset's palette, so the first frame paints in it.
func (m Model) WithTheme(name string) Model {
	ApplyThemeName(name)
	m.painted = name
	return m
}

// MotionFilter is the program's tea.WithFilter hook: it drops pointer motion that
// resolves to the hover the model already holds, so neither Update nor View runs for it.
// Everything else passes, and so does the first motion after a key cleared hover. It
// resolves through the geometric hit-test, never a j/k walk.
func MotionFilter(model tea.Model, msg tea.Msg) tea.Msg {
	mm, ok := msg.(tea.MouseMsg)
	if !ok {
		return msg
	}
	if isWheel(mm) {
		if m, ok := model.(Model); ok && m.mergeWheel(mm) {
			return nil
		}
		return msg
	}
	if mm.Action != tea.MouseActionMotion {
		return msg
	}
	m, ok := model.(Model)
	if ok && m.loopMenuLive() {
		h := m.loopHover(mm.X, mm.Y)
		if m.hover.set && !h.opt && !m.hover.opt { // nothing shades off the options
			return nil
		}
		if h.same(m.hover) {
			return nil
		}
		return msg
	}
	if ok && m.formLive() { // a form has no hover
		return nil
	}
	if ok && m.modalLive() { // only a change of picker item matters
		d := m.modals[len(m.modals)-1]
		if d.kind != picker {
			return nil
		}
		if i := m.pickerItemAt(d, mm.X, mm.Y); i < 0 || i == d.cur {
			return nil
		}
		return msg
	}
	if !ok || !m.mouseLive() {
		return msg
	}
	if m.menu != nil { // only a change of menu item matters
		if i := m.menu.itemAt(mm.X, mm.Y); i < 0 || i == m.menu.cur {
			return nil
		}
		return msg
	}
	if !m.hover.set {
		return msg
	}
	if m.pointAt(mm.X, mm.Y).same(m.hover) {
		return nil
	}
	return msg
}

// hover is what the pointer rests on: its cell, and the stop (or inert line) under it.
// set is false until the pointer moves and again after any key, so the first motion
// after a key always passes MotionFilter. y is -1 off the body and the back span.
type hover struct {
	set     bool
	x, y    int
	back    bool // the focus view's "← esc back" span
	tabOn   bool // the pointer is on a tab block
	tab     int
	hdr     bool         // the pointer is on a clickable header segment
	head    headerSpan   // that segment; its first column names it
	opt     bool         // the pointer is on a Loop menu option
	optSpan loopOptCells // that option, in screen columns
	t       hitTarget
}

// same is whether two hovers shade the same thing.
func (h hover) same(o hover) bool {
	return h.set == o.set && h.y == o.y && h.back == o.back && h.tabOn == o.tabOn && h.tab == o.tab && h.hdr == o.hdr && h.head == o.head && h.opt == o.opt && h.optSpan == o.optSpan && h.t.kind == o.t.kind && h.t.key == o.t.key
}

// bodyHit is the hit-test of body line by on the active surface.
func (m Model) bodyHit(by int) hitTarget {
	if m.focus.id != "" { // the focus view is Model state, not a tab
		return m.focusHit(m.w, m.h-chromeH+1, by)
	}
	if p, ok := m.tabAt(m.tab).(pointable); ok {
		return p.hit(m.w, m.h-chromeH, by)
	}
	return hitTarget{}
}

// pointAt resolves screen cell (x, y) to a hover: only a stop, a blocker link or the
// back span shades; any other line holds the pointer without a target.
func (m Model) pointAt(x, y int) hover {
	if m.focus.id != "" && backSpan(x, y) {
		return hover{set: true, x: x, y: y, back: true}
	}
	if sp, ok := m.headerAt(x, y); ok {
		return hover{set: true, x: x, y: y, hdr: true, head: sp}
	}
	by, ok := m.bodyLine(y)
	if !ok {
		if i, isTab := m.tabAtCell(x, y); isTab {
			return hover{set: true, x: x, y: y, tabOn: true, tab: i}
		}
		return hover{set: true, x: x, y: -1}
	}
	h := hover{set: true, x: x, y: y}
	switch t := m.bodyHit(by); t.kind {
	case hitRow, hitHeader, hitBlockerLink:
		h.t = t
	}
	return h
}

// headerAt is the clickable header segment covering screen cell (x, y), if any.
func (m Model) headerAt(x, y int) (headerSpan, bool) {
	if y != 0 {
		return headerSpan{}, false
	}
	for _, sp := range m.headerSpans() {
		if x >= sp.x0 && x < sp.x1 {
			return sp, true
		}
	}
	return headerSpan{}, false
}

// headerClick opens what the header segment under a left press names, by the same path
// as its key; a double-click pairs on the segment and counts as one click.
func (m Model) headerClick(sp headerSpan) (tea.Model, tea.Cmd) {
	target := sp.kind.target()
	now := clock()
	if m.click.target == target && now.Sub(m.click.at) <= doubleClickWindow {
		m.click = lastClick{}
		return m, nil
	}
	m.click = lastClick{target: target, at: now}
	if sp.kind == headerLoop {
		return m.openLoopMenu()
	}
	return m.sessionsOpen()
}

// tabAtCell is the tab whose block covers screen cell (x, y) on the strip, if any.
func (m Model) tabAtCell(x, y int) (int, bool) {
	if m.focus.id != "" || y < 1 || y > 3 {
		return 0, false
	}
	for _, s := range m.strip(-1).spans {
		if x >= s.x0 && x < s.x1 {
			return s.tab, true
		}
	}
	return 0, false
}

// slideStrip moves the strip's start one tab by dir (-1 left, +1 right) and holds it there.
func (m Model) slideStrip(dir int) Model {
	st := m.strip(-1)
	if !st.clipped {
		return m
	}
	m.tabHeld, m.tabStart = true, max(0, min(st.start+dir, st.maxStart))
	return m
}

// stripEvent handles a wheel or left press on the tab strip rows; ok is whether it was one.
func (m Model) stripEvent(msg tea.MouseMsg) (tea.Model, tea.Cmd, bool) {
	if m.focus.id != "" || msg.Y < 1 || msg.Y > 3 || msg.Action != tea.MouseActionPress {
		return m, nil, false
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelLeft:
		return m.slideStrip(-1), nil, true
	case tea.MouseButtonWheelDown, tea.MouseButtonWheelRight:
		return m.slideStrip(1), nil, true
	case tea.MouseButtonLeft:
		st := m.strip(-1)
		in := func(c [2]int) bool { return c[1] > c[0] && msg.X >= c[0] && msg.X < c[1] }
		switch {
		case in(st.left):
			return m.slideStrip(-1), nil, true
		case in(st.right):
			return m.slideStrip(1), nil, true
		}
		i, ok := m.tabAtCell(msg.X, msg.Y)
		if !ok {
			return m, nil, true
		}
		target := "tab/" + strconv.Itoa(i)
		now := clock()
		if m.click.target == target && now.Sub(m.click.at) <= doubleClickWindow {
			m.click = lastClick{} // a double-click is one click
			return m, nil, true
		}
		m.click = lastClick{target: target, at: now}
		m.tabHeld = false
		next, cmd := m.enter(i)
		if nm, ok := next.(Model); ok && nm.tab != m.tab { // a tab switch drops any scrolled offset
			next = nm.unscrollAll()
		}
		return next, cmd, true
	}
	return m, nil, true // a right-click or other button on the strip does nothing
}

// hoverSettle re-hit-tests the resting pointer after any message, so a refresh that
// moved or removed the row under it leaves hover on what is there now.
func (m Model) hoverSettle() Model {
	if !m.hover.set {
		return m
	}
	if m.loopMenuLive() {
		m.hover = m.loopHover(m.hover.x, m.hover.y)
		return m
	}
	if !m.mouseLive() {
		m.hover = hover{}
		return m
	}
	m.hover = m.pointAt(m.hover.x, m.hover.y)
	return m
}

// hoverRow is the hover shade: navy, behind the row's own colours (ADR 0372 §1).
var hoverRow lipgloss.Style

// paintHover shades the hovered target's lines of a drawn frame, but never the selected
// row's. A continuation line shades with its row.
func (m Model) paintHover(frame string) string {
	if !m.hover.set || !m.mouseLive() {
		return frame
	}
	h := m.hover
	lines := strings.Split(frame, "\n")
	if h.hdr {
		l := lines[0]
		x0, x1 := h.head.x0, h.head.x1
		lines[0] = ansi.Cut(l, 0, x0) + shadeRow(ansi.Cut(l, x0, x1), x1-x0, hoverRow) + ansi.Cut(l, x1, m.w)
		return strings.Join(lines, "\n")
	}
	if h.back {
		if h.y < len(lines) {
			span := lipgloss.Width("← esc back")
			l := lines[h.y]
			lines[h.y] = ansi.Cut(l, 0, 1) + shadeRow(ansi.Cut(l, 1, 1+span), span, hoverRow) + ansi.Cut(l, 1+span, m.w)
		}
		return strings.Join(lines, "\n")
	}
	if h.t.kind == hitNone {
		return frame
	}
	by, _ := m.bodyLine(h.y)
	lo, hi := by, by
	for lo > 0 && m.sameTarget(lo-1, h.t) {
		lo--
	}
	for hi < m.h-chromeH+1 && m.sameTarget(hi+1, h.t) {
		hi++
	}
	top := m.bodyTop()
	navy := strings.TrimSuffix(hoverRow.Render(""), "\x1b[0m")
	ancBg := strings.TrimSuffix(ancRow.Render(""), "\x1b[0m")
	for by := lo; by <= hi; by++ {
		if y := top + by; y < len(lines) && !h.t.sel {
			lines[y] = shadeRow(strings.ReplaceAll(lines[y], ancBg, navy), m.w, hoverRow)
		}
	}
	return strings.Join(lines, "\n")
}

// sameTarget is whether body line by holds the same stop as t.
func (m Model) sameTarget(by int, t hitTarget) bool {
	u := m.bodyHit(by)
	return u.kind == t.kind && u.key == t.key
}

// setMouse switches capture live to on, when that changes what the shell holds:
// a save that moved the resolved setting. Off disables; on resets then enables, so
// a terminal already in another mouse mode lands in all-motion.
func (m *Model) setMouse(on bool) tea.Cmd {
	if on == m.mouseOn {
		return nil
	}
	m.mouseOn = on
	if !on {
		return tea.Batch(tea.DisableMouse, tmuxMouse(false))
	}
	return tea.Batch(tea.Sequence(tea.DisableMouse, tea.EnableMouseAllMotion), tmuxMouse(true))
}

// setTUIMouse sets tmux's mouse on the TUI's own session; a var as the Settings
// tab's save vars are, so tests never reach a real tmux server.
var setTUIMouse = session.SetTUIMouse

// tmuxMouse makes the TUI's tmux session follow Mouse mode. A failure is noted
// on the log and swallowed: capture and the saved setting are unaffected.
func tmuxMouse(on bool) tea.Cmd {
	return func() tea.Msg {
		if err := setTUIMouse(on); err != nil {
			slog.Warn("tmux.mouse.failed", "on", on, "err", err.Error())
		}
		return nil
	}
}

// rearmMouse re-sends all-motion capture while it is on. Bubble Tea's
// RestoreTerminal never re-arms the mouse, so every hand-back (a resume, an
// ExecProcess callback) must; a new exec site must do the same.
func (m Model) rearmMouse() tea.Cmd {
	if !m.mouseOn {
		return nil
	}
	return tea.EnableMouseAllMotion
}

// mouseLive is whether a mouse event may act: capture is on, no modal, picker,
// form or the ? reference is open, and the screen is at or above the floor.
func (m Model) mouseLive() bool {
	return m.mouseOn && len(m.modals) == 0 && !m.ref && m.w >= floorCols && m.h >= floorRows
}

// modalCaptured is whether the mouse may act on a modal at all: capture is on, a modal is
// open, the ? reference is not, and the screen is at or above the floor.
func (m Model) modalCaptured() bool {
	return m.mouseOn && !m.ref && len(m.modals) > 0 && m.w >= floorCols && m.h >= floorRows
}

// formLive is whether the mouse acts on the top modal: capture is on, it is a Signal or
// Initiative form, an input dialog or the New Surgery dialog, and the screen is at or above
// the floor. Pickers, the Loop menu and confirms are live through modalLive; only the X
// withdraw dialog stays mouse-dead.
func (m Model) formLive() bool {
	if !m.modalCaptured() {
		return false
	}
	switch m.modals[len(m.modals)-1].kind {
	case form, input, surgery:
		return true
	}
	return false
}

// hitKind is what a body line is to the pointer.
type hitKind int

const (
	hitNone        hitKind = iota // inert: a gap, padding, the drawer's rule, an error or empty line
	hitRow                        // a stop (with a bead unless bead is false: a commit, a setting)
	hitHeader                     // a stop without one: a section or group header, a create row
	hitBlockerLink                // a blocker footer row that jumps the selection to its blocker
	hitDrawer                     // the open drawer's body: the wheel only
)

// hitTarget is what hit finds on one body line.
type hitTarget struct {
	kind  hitKind
	key   string // the row key j/k would store; for hitBlockerLink, the blocker's bead id
	bead  bool   // the row carries a bead
	caret int    // the tree caret's column, -1 when the row has none
	line  int    // the screen line (body-relative) the target is drawn on
	sel   bool   // the target is the surface's selected row, which never shows hover
}

// pointable is an optional tab interface, as extraKeys and liveCounter are: a tab the
// pointer can act on. hit is recomputed from the model against View's geometry (a body w by
// h), never by walking the list; selectKey selects a row as j/k would; wheel scrolls.
type pointable interface {
	hit(w, h, y int) hitTarget
	selectKey(key string) component
	wheel(w, h, y, delta int) component
	// snap returns a scrolled list to following the selection, as a key does (ADR 0374).
	snap(w, h int) component
	// unscroll drops any scrolled offset: a tab switch.
	unscroll() component
}

// wheelStep is how many lines one wheel event scrolls.
const wheelStep = 1

// unscrollAll returns every list on every tab, and the focus view's, to Following.
func (m Model) unscrollAll() Model {
	m.tabs = slices.Clone(m.tabs)
	for i, t := range m.tabs {
		if p, ok := t.(pointable); ok {
			m.tabs[i] = p.unscroll()
		}
	}
	m.focus.lv = listView{}
	m.setLV = listView{}
	return m
}

// snapLists snaps the active list back to following the selection before a key acts: it
// scrolls as little as keeping the selection visible takes, or re-centres when it is off screen.
func (m Model) snapLists() Model {
	if len(m.modals) > 0 || m.ref || m.w < floorCols || m.h < floorRows {
		return m
	}
	if m.focus.id != "" {
		return m.focusSnap(m.w, m.h-chromeH+1)
	}
	if p, ok := m.tabAt(m.tab).(pointable); ok {
		m.tabs = slices.Clone(m.tabs)
		m.tabs[m.tab] = p.snap(m.w, m.h-chromeH)
		m = m.takeBackSettings()
	}
	return m
}

// wheelAcc is the wheel events MotionFilter swallowed while a render was pending: their
// summed delta, the cell they were at, and whether a flush is in flight. The Model shares
// it by pointer; the event loop runs the filter and Update on one goroutine.
type wheelAcc struct {
	armed bool
	x, y  int
	delta int // lines, signed
}

// wheelFlush is the message an armed accumulator's Cmd sends; Update applies the pending
// delta before any message, so the flush itself does nothing more.
type wheelFlush struct{}

func isWheel(mm tea.MouseMsg) bool {
	return mm.Action == tea.MouseActionPress && (mm.Button == tea.MouseButtonWheelUp || mm.Button == tea.MouseButtonWheelDown)
}

func wheelDelta(mm tea.MouseMsg) int {
	if mm.Button == tea.MouseButtonWheelUp {
		return -wheelStep
	}
	return wheelStep
}

// mergeWheel adds a body wheel press at the armed accumulator's cell to its delta and
// reports it swallowed. Anything else passes: an unarmed press scrolls on its own Update,
// a press at another cell flushes there, and the tab strip never merges.
func (m Model) mergeWheel(mm tea.MouseMsg) bool {
	a := m.wheelAcc
	if a == nil || !a.armed || !m.mouseLive() || m.menu != nil {
		return false
	}
	if _, ok := m.bodyLine(mm.Y); !ok || a.x != mm.X || a.y != mm.Y {
		return false
	}
	a.delta += wheelDelta(mm)
	return true
}

// flushWheel applies the pending merged delta at its cell and disarms the accumulator.
func (m Model) flushWheel() Model {
	a := m.wheelAcc
	if a == nil || !a.armed {
		return m
	}
	d := a.delta
	a.armed, a.delta = false, 0
	if d == 0 {
		return m
	}
	if y, ok := m.bodyLine(a.y); ok {
		m = m.wheelBy(y, d)
		m.hover = m.pointAt(a.x, a.y)
	}
	return m
}

// wheelEvent scrolls the list or drawer under the pointer by one notch, leaving the selection
// where it is, and arms the accumulator so presses queued behind this render merge.
func (m Model) wheelEvent(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	y, ok := m.bodyLine(msg.Y)
	if !ok {
		return m, nil
	}
	m = m.wheelBy(y, wheelDelta(msg))
	if a := m.wheelAcc; a != nil {
		a.armed, a.x, a.y, a.delta = true, msg.X, msg.Y, 0
		return m, func() tea.Msg { return wheelFlush{} }
	}
	return m, nil
}

// wheelBy scrolls the list or drawer at body line y by delta lines.
func (m Model) wheelBy(y, delta int) Model {
	if m.focus.id != "" {
		return m.focusWheel(m.w, m.h-chromeH+1, y, delta)
	}
	if p, ok := m.tabAt(m.tab).(pointable); ok {
		m.tabs = slices.Clone(m.tabs)
		m.tabs[m.tab] = p.wheel(m.w, m.h-chromeH, y, delta)
		m = m.takeBackSettings()
	}
	return m
}

// bodyTop is the screen row the body starts at: under the header and three tab rows, or
// under the header and the focus view's back bar.
func (m Model) bodyTop() int {
	if m.focus.id != "" {
		return 3
	}
	return 4
}

// bodyLine maps a screen row to a body line, false on the chrome (header, tab strip, status
// line, hints bar). x never chooses a row.
func (m Model) bodyLine(y int) (int, bool) {
	top := m.bodyTop()
	h := m.h - chromeH
	if m.focus.id != "" {
		h++
	}
	return y - top, y >= top && y < top+h
}

// doubleClickWindow is how close two left presses on one target must be to pair.
const doubleClickWindow = 450 * time.Millisecond

// lastClick is the memory a double-click pairs against: the logical target and when it was pressed.
type lastClick struct {
	target string
	at     time.Time
}

// backSpan is the focus view's "← esc back" on the back bar: screen row 1, columns 1–10.
func backSpan(x, y int) bool { return y == 1 && x >= 1 && x < 1+lipgloss.Width("← esc back") }

// caretAt is the column of the ▾/▸ the screen row y of the drawn frame carries, -1 for none.
func (m Model) caretAt(y int) int {
	lines := strings.Split(m.View(), "\n")
	if y < 0 || y >= len(lines) {
		return -1
	}
	s := ansi.Strip(lines[y])
	i := strings.IndexAny(s, "▾▸")
	if i < 0 {
		return -1
	}
	return ansi.StringWidth(s[:i])
}

// sendKey runs a synthesized key through the shell's normal key path; like any key it
// hides the hover.
func (m Model) sendKey(k tea.KeyMsg) (Model, tea.Cmd) {
	m.noSnap = true // a click keeps the list scrolled
	m.hover = hover{}
	next, cmd := m.keyRecording(k)
	nm := next.(Model)
	nm.noSnap = false
	return nm, cmd
}

// loopMenuLive is whether the mouse acts on an open Loop menu on top.
func (m Model) loopMenuLive() bool {
	return m.modalLive() && m.modals[len(m.modals)-1].kind == loopMenu
}

// mouseEvent is the shell's tea.MouseMsg branch, ahead of the fold: a left press on the
// active surface selects what its hit names, folds a header or a tree caret, opens on a
// double-click, jumps a blocker link and leaves the focus view from its back bar.
func (m Model) mouseEvent(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.formLive() {
		return m.formMouse(msg)
	}
	if m.modalLive() {
		return m.modalMouse(msg)
	}
	if !m.mouseLive() {
		return m, nil
	}
	if m.menu != nil {
		return m.menuMouse(msg)
	}
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonRight {
		return m.rightPress(msg)
	}
	if msg.Action == tea.MouseActionMotion {
		m.hover = m.pointAt(msg.X, msg.Y)
		return m, nil
	}
	if next, cmd, ok := m.stripEvent(msg); ok {
		return next, cmd
	}
	if msg.Action == tea.MouseActionPress && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown) {
		next, cmd := m.wheelEvent(msg)
		nm := next.(Model)
		nm.hover = nm.pointAt(msg.X, msg.Y) // the list moved under the resting pointer
		return nm, cmd
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	if m.focus.id != "" && backSpan(msg.X, msg.Y) {
		m.click = lastClick{}
		return m.sendKey(tea.KeyMsg{Type: tea.KeyEsc})
	}
	if sp, ok := m.headerAt(msg.X, msg.Y); ok {
		return m.headerClick(sp)
	}
	y, ok := m.bodyLine(msg.Y)
	if !ok {
		return m, nil
	}
	if next, ok := m.filterClick(msg.X, y); ok {
		return next, nil
	}
	var (
		h   hitTarget
		sel func(Model, string) Model
		tag string
	)
	if m.focus.id != "" { // the focus view is Model state, not a tab
		h = m.focusHit(m.w, m.h-chromeH+1, y)
		sel, tag = Model.focusSelectKey, "focus"
	} else {
		p, isP := m.tabAt(m.tab).(pointable)
		if !isP {
			return m, nil
		}
		h = p.hit(m.w, m.h-chromeH, y)
		sel = func(m Model, key string) Model {
			m.tabs = slices.Clone(m.tabs) // a copied Model must not share the tab it updates
			m.tabs[m.tab] = p.selectKey(key)
			return m.takeBackSettings()
		}
		tag = strconv.Itoa(m.tab)
	}
	switch h.kind {
	case hitBlockerLink:
		m.click = lastClick{}
		next, cmd := m.unscrollAll().recentsJump(h.key) // the jump returns the lists to Following
		return next, cmd
	case hitRow, hitHeader:
	default:
		return m, nil
	}
	if h.kind == hitRow && h.caret < 0 {
		h.caret = m.caretAt(msg.Y)
	}
	m = sel(m, h.key)
	target := tag + "/" + h.key
	// A header, or a click on a tree row's caret, is sent as space; it folds only if the view changes.
	if h.kind == hitHeader || h.caret >= 0 && msg.X >= h.caret-1 && msg.X <= h.caret+1 {
		before := m.View()
		next, cmd := m.sendKey(tea.KeyMsg{Type: tea.KeySpace})
		if next.View() != before {
			next.click = lastClick{}
			return next, cmd
		}
		m = next
		_ = cmd // an unfolded stop's space did nothing worth running
	}
	now := clock()
	if m.click.target == target && now.Sub(m.click.at) <= doubleClickWindow {
		m.click = lastClick{}
		return m.sendKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	m.click = lastClick{target: target, at: now}
	return m, nil
}
