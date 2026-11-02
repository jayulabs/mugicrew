package tui

// The Initiatives tab (Spec mc-y2zc, ADR 0181): every Initiative in two sections,
// Open and Closed, one row each. It is the entry to the Initiative focus view, so it
// lists Initiatives without their members. Thin client over the shell's snapshot.

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

const (
	initsOpen   = "s:open"
	initsClosed = "s:closed"

	initsSection = "initiatives" // its section of folds.json
)

// iRow is a section header (b nil) or one Initiative; key is the Initiative's id or
// the section key.
type iRow struct {
	key   string
	b     *bd.Bead
	label string
}

// initsTab's state: the in-memory folds and the selected row's key. g and waits are
// handed over by tabAt before every key and frame.
type initsTab struct {
	g     graphData
	waits map[string]bool // Initiative ids whose cut session waits on the human
	stats map[string]initStats
	flip  map[string]bool
	peek  map[string]bool // keys a Recents jump flipped, with their pre-jump value: never saved
	path  string          // folds.json; "" when the common dir can't be resolved
	sel   string
	open  bool // the detail drawer; only ever shown on an Initiative row
	off   int  // the drawer's scroll offset
	lv    listView
	w, h  int
}

// newInits is an Initiatives tab with the folds saved on this machine.
func newInits() component {
	path := foldsPath("folds.json")
	flip := map[string]bool{}
	var file map[string]map[string]bool
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &file) // garbage is no folds
	}
	for k, v := range file[initsSection] {
		if v && (k == initsOpen || k == initsClosed) {
			flip[k] = true
		}
	}
	return initsTab{flip: flip, path: path}
}

// save rewrites only the "initiatives" section of the shared folds file, leaving the
// other tabs' sections as they are. The tab has no bead-keyed folds, so pruning keeps
// the two section keys and drops the rest.
func (t initsTab) save() error {
	if t.path == "" {
		return errors.New("common dir unresolved")
	}
	var file map[string]json.RawMessage
	if b, err := os.ReadFile(t.path); err == nil {
		json.Unmarshal(b, &file)
	}
	if file == nil {
		file = map[string]json.RawMessage{}
	}
	saved := map[string]bool{}
	flip := unpeeked(t.flip, t.peek)
	for _, k := range []string{initsOpen, initsClosed} {
		if flip[k] {
			saved[k] = true
		}
	}
	sec, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	file[initsSection] = sec
	b, err := json.Marshal(file)
	if err != nil {
		return err
	}
	return writeFileAtomic(t.path, b)
}

// initsKey saves the folds after a fold key; a failed write goes on the status line and
// the fold stays (ADR 0176 §5).
func (m Model) initsKey(k tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if !key.Matches(k, keyFold) {
		return m, nil, false
	}
	old := m.tabAt(m.tab).(initsTab).flip
	next, cmd := m.tabKey(k)
	nm := next.(Model)
	if nt := nm.tabAt(nm.tab).(initsTab); maps.Equal(old, nt.flip) {
		return nm, cmd, true // a fold key on an Initiative row changes nothing
	} else if err := nt.save(); err != nil {
		nm.setStatus("folds won't survive a restart: " + err.Error())
	}
	return nm, cmd, true
}

// initStats are an Initiative's line-2 counts over its Parent subtree (Spec D4), computed
// once per snapshot refresh: focusScope is too slow to run per frame.
type initStats struct{ cpOpen, cpClosed, open, live, waiting, members int }

// initWaitsOf are the Initiatives whose session waits on the human, per refresh.
func (m Model) initWaitsOf() map[string]bool {
	out := map[string]bool{}
	for _, b := range m.beads {
		if b.Kind == contract.KindInitiative && m.waiting(b) {
			out[b.ID] = true
		}
	}
	return out
}

// initStatsOf is every Initiative's line-2 counts, keyed by id (Spec D4). Drafts and the
// Initiative itself are out of the subtree counts; waiting reads focusScope (Initiative included), so it
// matches the back bar's ●N.
func (m Model) initStatsOf() map[string]initStats {
	g := m.gateData()
	out := map[string]initStats{}
	// Checkpoint id -> waiter ids, one pass: graphData.waiters per Checkpoint is O(n) each.
	// Same rule as waiters: live Pivots/Errands in cp.BlockedBy, live Errands blocked by cp.
	byID := make(map[string]*bd.Bead, len(m.beads))
	waiters := map[string][]string{}
	for i, b := range m.beads {
		byID[b.ID] = &m.beads[i]
		if b.Kind == contract.KindErrand && execLive(b) {
			for _, id := range b.BlockedBy {
				waiters[id] = append(waiters[id], b.ID)
			}
		}
	}
	for _, cp := range m.beads {
		if cp.Kind != contract.KindCheckpoint {
			continue
		}
		for _, id := range cp.BlockedBy {
			if b := byID[id]; b != nil && execLive(*b) && (b.Kind == contract.KindPivot || b.Kind == contract.KindErrand) {
				waiters[cp.ID] = append(waiters[cp.ID], b.ID)
			}
		}
	}
	for _, in := range m.beads {
		if in.Kind != contract.KindInitiative {
			continue
		}
		var st initStats
		sub := subtree(m.beads, in.ID)
		scope := maps.Clone(sub) // the waiters join a copy: the counts below read sub
		for _, cp := range m.beads {
			if sub[cp.ID] && cp.Kind == contract.KindCheckpoint {
				for _, w := range waiters[cp.ID] {
					scope[w] = true
				}
			}
		}
		for i, b := range m.beads {
			if !sub[b.ID] || b.ID == in.ID {
				continue
			}
			st.members++ // Drafts count: the glossary's members are "Drafts included"
			if b.Status == "deferred" {
				continue
			}
			closed := b.Status == "closed"
			switch {
			case b.Kind == contract.KindCheckpoint && closed:
				st.cpClosed++
			case b.Kind == contract.KindCheckpoint:
				st.cpOpen++
			}
			if !closed {
				st.open++
			}
			if g.isLive(&m.beads[i]) {
				st.live++
			}
		}
		for i, b := range m.beads {
			if scope[b.ID] && m.waiting(m.beads[i]) {
				st.waiting++
			}
		}
		out[in.ID] = st
	}
	return out
}

// folded: Closed starts folded, Open unfolded.
func (t initsTab) folded(k string) bool { return t.flip[k] != (k == initsClosed) }

// closedCmp orders Closed by real close time, newest first; beads with no close time
// sort as a block after the timed ones, and a tie falls back to beadCmp (a total order).
func (g graphData) closedCmp(a, b bd.Bead) int {
	if a.Closed.IsZero() != b.Closed.IsZero() {
		if a.Closed.IsZero() {
			return 1
		}
		return -1
	}
	return cmp.Or(b.Closed.Compare(a.Closed), g.rowCmp(a, b))
}

func (t initsTab) rows() []iRow {
	var open, closed []*bd.Bead
	for i, b := range t.g.beads {
		switch {
		case b.Kind != contract.KindInitiative || b.Event || b.Status == "deferred":
		case b.Status == "closed":
			closed = append(closed, &t.g.beads[i])
		default:
			open = append(open, &t.g.beads[i])
		}
	}
	if len(open)+len(closed) == 0 {
		return nil
	}
	slices.SortStableFunc(open, func(a, b *bd.Bead) int { return t.g.rowCmp(*a, *b) })
	slices.SortStableFunc(closed, func(a, b *bd.Bead) int { return t.g.closedCmp(*a, *b) })
	var rows []iRow
	for _, s := range []struct {
		key, name string
		bs        []*bd.Bead
	}{{initsOpen, "Open", open}, {initsClosed, "Closed", closed}} {
		fold := "▾ "
		if t.folded(s.key) {
			fold = "▸ "
		}
		rows = append(rows, iRow{key: s.key, label: fmt.Sprintf("%s%s (%d)", fold, s.name, len(s.bs))})
		if t.folded(s.key) {
			continue
		}
		for _, b := range s.bs {
			rows = append(rows, iRow{key: b.ID, b: b})
		}
	}
	return rows
}

func (t initsTab) selIndex(rows []iRow) int {
	return max(slices.IndexFunc(rows, func(r iRow) bool { return r.key == t.sel }), 0)
}

// settle keeps sel across a refresh (ADR 0181 §9). old is the row list before it: a
// selection that closed into a folded Closed lands on its header; one that left the
// snapshot moves to the nearest row above it that is still listed (ADR 0174 §7).
func (t initsTab) settle(old []iRow) initsTab {
	now := t.rows()
	if t.sel == "" || slices.ContainsFunc(now, func(r iRow) bool { return r.key == t.sel }) {
		return t
	}
	if slices.ContainsFunc(t.g.beads, func(b bd.Bead) bool {
		return b.ID == t.sel && b.Kind == contract.KindInitiative && b.Status == "closed"
	}) {
		t.sel = initsClosed
		return t
	}
	for i := slices.IndexFunc(old, func(r iRow) bool { return r.key == t.sel }) - 1; i >= 0; i-- {
		if slices.ContainsFunc(now, func(r iRow) bool { return r.key == old[i].key }) {
			t.sel = old[i].key
			return t
		}
	}
	t.sel = ""
	return t
}

// selected is the cursor's Initiative, nil on a header or an empty tab.
func (t initsTab) selected() *bd.Bead {
	rows := t.rows()
	if len(rows) == 0 {
		return nil
	}
	return rows[t.selIndex(rows)].b
}

// count is 0: Ideate's badge already counts a waiting cut session (ADR 0181 §7).
func (initsTab) count() int { return 0 }
func (initsTab) actN() int  { return 0 }

// drawerOpen: the drawer needs an Initiative to describe, so a cursor on a header shuts it.
func (t initsTab) drawerOpen() bool { return t.open && t.selected() != nil }

// detailVP is Graph's Initiative detail, rendered by Graph's own builder.
func (t initsTab) detailVP() viewport.Model {
	text := ""
	if b := t.selected(); b != nil {
		text = graphTab{graphData: t.g, sel: b.ID, noDot: true}.detailText()
	}
	return drawerVP(t.drawerOpen(), t.w, t.h, text, t.off)
}

func (t initsTab) bindings() []key.Binding {
	rows := t.rows()
	switch {
	case len(rows) == 0:
		return nil
	case rows[t.selIndex(rows)].b == nil:
		return []key.Binding{keyFold, keySection}
	}
	hinted, _ := beadVerbBindings(t.g, rows[t.selIndex(rows)].b, roleFull)
	if t.drawerOpen() {
		return slices.Concat([]key.Binding{keyShut, keyScroll, keyTranscript}, hinted, []key.Binding{keySection})
	}
	return slices.Concat([]key.Binding{verbKey("enter", "focus"), keyDetail, keyTranscript}, hinted, []key.Binding{keySection})
}

// extra: enter still opens the focus under the drawer, though only the bar without it hints it.
func (t initsTab) extra() []key.Binding {
	out := []key.Binding{keyMove}
	if t.drawerOpen() {
		out = append(out, verbKey("enter", "focus"))
	}
	_, verbs := beadVerbBindings(t.g, t.selected(), roleFull)
	return append(out, verbs...)
}

func (t initsTab) update(k tea.KeyMsg) (component, tea.Cmd) {
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
		t.sel, t.off = rows[clampWrap(t.selIndex(rows)+d, len(rows))].key, 0
	case key.Matches(k, keySection):
		d := 1
		if s := k.String(); s == "K" || s == "ctrl+up" {
			d = -1
		}
		t.sel, t.off = rows[jumpTo(rows, t.selIndex(rows), d, func(r iRow) bool { return r.b == nil })].key, 0
	case r.b != nil && t.drawerOpen() && key.Matches(k, keyShut):
		t.open, t.off = false, 0
	case r.b != nil && key.Matches(k, keyDetail):
		t.open = true
	case t.drawerOpen() && key.Matches(k, keyScroll):
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
	case key.Matches(k, keyFold) && r.b == nil:
		t.flip = maps.Clone(t.flip)
		if t.flip[r.key] = !t.flip[r.key]; !t.flip[r.key] {
			delete(t.flip, r.key) // back at the default
		}
		t.peek = unpeek(t.peek, r.key)
	}
	return t, nil
}

func (t initsTab) line(r iRow) (string, string) {
	if r.b == nil {
		return " " + dimSt.Render(r.label), ""
	}
	b := r.b
	s := "  " + t.g.sbrn(b, containerTitle(b, false, b.Title), false, 0)
	if t.waits[b.ID] {
		s += " " + badgeSt.Render("●")
	}
	if b.Status == "closed" && strings.HasPrefix(b.CloseReason, "cancelled: ") {
		s += sep() + dimSt.Render("cancelled")
	} else if bodyless(b) { // the cancelled slot: an open row is never cancelled (ADR 0182 §5)
		s += sep() + dimSt.Render("no body yet")
	}
	if t.g.noRef(b) {
		s += sep() + dimSt.Render("no ref")
	}
	return s, ""
}

// line2 is the muted count line under an Initiative's row, indented to start under its title.
func (t initsTab) line2(b *bd.Bead) string {
	st := t.stats[b.ID]
	head, _ := t.g.sbrnParts(b, false, 0)
	in := strings.Repeat(" ", 2+ansi.StringWidth(head)) // the row's lead plus its sbrn head, as line builds it
	if bodyless(b) && st.members == 0 {
		return in + dimSt.Render("no body yet · its cut session settles Done when and Phases first")
	}
	return in + dimSt.Render(fmt.Sprintf("checkpoints %d open · %d closed · %d open descendants · ", st.cpOpen, st.cpClosed, st.open)) +
		countSt(aliveSt, st.live, "live") + dimSt.Render(" · ") + countSt(waitSt, st.waiting, "waiting")
}

// lines is the Initiatives list in line space, as view windows it: src[k] is line k's row
// index, 2*i+1 for a bead row's line 2 (row i), -1 for the blank above Closed; at is the line
// the window must keep visible, the cursor row's last.
func (t initsTab) lines(rows []iRow, sel int) (src []int, at int) {
	closedAt := 0
	for i, r := range rows {
		if r.key == initsClosed {
			closedAt = len(src)
		}
		src = append(src, 2*i)
		if r.b != nil { // line 2 belongs to its row, not to the cursor's stops
			src = append(src, 2*i+1)
		}
		if i == sel {
			at = len(src) - 1
		}
	}
	if closedAt > 0 && closedAt < len(src) { // a blank line above the Closed header
		if at >= closedAt {
			at++
		}
		src = slices.Insert(src, closedAt, -1)
	}
	return src, at
}

// hit is the pointer's view of body line y of a w by h body (pointable): both lines of a row
// are that row.
func (t initsTab) hit(w, h, y int) hitTarget {
	rows := t.rows()
	if len(rows) == 0 {
		return hitTarget{caret: -1}
	}
	return t.geom(rows, h).hit(t.lv, y)
}

// geom is what View and the pointer share about an h tall body over rows (not empty).
func (t initsTab) geom(rows []iRow, h int) listGeom {
	src, at := t.lines(rows, t.selIndex(rows))
	keys := make([]string, len(src))
	for k, c := range src {
		if c >= 0 {
			keys[k] = rows[c/2].key + "/" + strconv.Itoa(c%2)
		}
	}
	return listGeom{n: len(src), sel: at, h: h, open: t.drawerOpen(), keys: anchorKeys(keys), at: func(k int) hitTarget {
		if src[k] < 0 {
			return hitTarget{}
		}
		r := rows[src[k]/2]
		return stopTarget(r.key, r.b != nil)
	}}
}

// selectKey selects a row as j/k would.
func (t initsTab) selectKey(key string) component { t.sel, t.off = key, 0; return t }

func (t initsTab) wheel(w, h, y, delta int) component {
	rows := t.rows()
	if len(rows) == 0 {
		return t
	}
	g := t.geom(rows, h)
	if g.hit(t.lv, y).kind == hitDrawer {
		t.off = wheelDrawer(t.detailVP(), delta)
		return t
	}
	t.lv = g.wheeled(t.lv, delta)
	return t
}

func (t initsTab) snap(w, h int) component {
	if rows := t.rows(); len(rows) > 0 {
		t.lv = t.geom(rows, h).snapped(t.lv)
	}
	return t
}

func (t initsTab) unscroll() component { t.lv = listView{}; return t }

func (t initsTab) view(w, h int) string {
	rows := t.rows()
	if len(rows) == 0 {
		return dimSt.Render(" no Initiatives yet — n i to start one")
	}
	sel := t.selIndex(rows)
	src, at := t.lines(rows, sel)
	line := func(k int) string {
		switch c := src[k]; {
		case c < 0:
			return ""
		case c%2 == 1:
			return ansi.Truncate(t.line2(rows[c/2].b), w, "…")
		default:
			return fitRow(func(int) (string, string) { return t.line(rows[c/2]) }, w, sel, c/2)
		}
	}
	return drawerWin(len(src), line, at, t.drawerOpen(), w, h, t.detailVP(), nil, t.lv.offsets(t.geom(rows, h).keys))
}
