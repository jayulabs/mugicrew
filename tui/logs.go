package tui

// The Logs tab (FR49–52, ADR 0144 §4): the event
// log rendered raw, newest first, with no alive/dead verdict (ADR 0033 §3). The
// selection is pinned by event key; the unparsed-line and level indicators sit in
// the body's header; d toggles the detail; / filters live on words.

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
)

// Logs' keys. The labels of v and e name what pressing them will do.
var (
	keySearch  = key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search"))
	keyKind    = key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "kind"))
	keyClear   = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear"))
	keyKeep    = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "keep"))
	keyRevert  = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "revert"))
	keyDebugOn = key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "debug"))
	keyErrOnly = key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "errors"))
)

// Cross-tab moves: tabs return these as commands, the shell applies them.
type (
	// revealMsg is G: show bead id in Graph.
	revealMsg struct{ id string }
	// scopeLogsMsg is L: open Logs scoped to bead id, with its children when kids.
	scopeLogsMsg struct {
		id   string
		kids bool
	}
	// pickLogKindMsg opens Logs' kind picker.
	pickLogKindMsg struct{}
	// olderWantedMsg is scrolling past the oldest loaded row: read the next page.
	olderWantedMsg struct{}
)

// logsData is the slice of the shell's data Logs reads, handed over before every
// update and view.
type logsData struct {
	events  []eventlog.Event
	beads   []bd.Bead
	skipped int   // unparsed lines the last read skipped
	err     error // the log is unavailable
	path    string
	more    bool                     // older events remain on disk beyond the window
	kindOf  map[string]contract.Kind // the kind of beads the snapshot lacks, for f; nil until read
	named   map[string]bool          // every bead id the whole log names, so f lists kinds older than the window
	w, h    int                      // the body's size
}

// logsTab's state: the filters, the selection and the detail pane.
type logsTab struct {
	logsData
	debug, errs bool          // v shows debug events; e errors only
	kind        contract.Kind // "" = every kind
	words       string        // the kept / query
	typing      bool          // the / input is open
	edit        line
	scope       string // L: bead id; "" = unscoped
	kids        bool   // the scope includes its children (ADR 0029 §3)
	sel         eventlog.Key
	open        bool // d: the detail pane is open
	off         int  // the detail viewport's scroll offset
	memo        *logsMemo
	scan        logsScan
}

// logsMemo holds one frame's (or one key's) rows so they are computed once;
// view and update set it on their copy, and it never outlives them.
type logsMemo struct {
	rows  []int
	total int // rows with no filter, for the header's "n of m"
	have  [2]bool
}

func newLogs() logsTab { return logsTab{} }

func (l logsTab) count() int { return 0 }
func (l logsTab) actN() int  { return 0 }

func (l logsTab) capturing() bool { return l.typing }

// query is what the filter matches now: the / input's live text while typing.
func (l logsTab) query() string {
	if l.typing {
		return l.edit.text
	}
	return l.words
}

func (l logsTab) filtered() bool { return l.kind != "" || l.words != "" || l.scope != "" }

// rows are the visible events as indices into ev, newest first: Debug hidden
// unless v (ADR 0027 §4). Unfiltered they index l.events from its newest; under
// a filter (/, f, L, e) they index scan.found, the whole-log scan's matches so far
// (ADR 0231 §2). The L scope is bead==X, or for a container bead==X || parent==X
// (ADR 0029 §3); an event's kind is its bead's, so under a kind filter one with no
// bead or a bead not loaded never matches; the words read msg, bead id and the attrs.
func (l logsTab) rows() []int {
	if m := l.memo; m != nil {
		if !m.have[0] {
			m.rows, m.have[0] = l.visible(), true
		}
		return m.rows
	}
	return l.visible()
}

func (l logsTab) visible() []int {
	if !l.active() {
		return l.rowsBy("", "")
	}
	var rows []int
	for i, ev := range l.scan.found {
		if ev.Level != "DEBUG" || l.debug {
			rows = append(rows, i)
		}
	}
	return rows
}

// total is how many events the level toggles leave, whatever the filter.
func (l logsTab) total() int {
	if m := l.memo; m != nil {
		if !m.have[1] {
			m.total, m.have[1] = len(l.rowsBy("", "")), true
		}
		return m.total
	}
	return len(l.rowsBy("", ""))
}

// base is l with the kind and words filters cleared: the events total counts.
func (l logsTab) base() logsTab {
	l.kind, l.words, l.typing = "", "", false
	return l
}

// tally is the base filter as eventlog counts it: without an L scope e alone
// decides a raw line; a scope must match the event's bead (or parent) exactly.
func (l logsTab) tally() eventlog.Base {
	return eventlog.Base{Pre: l.prefilter(), Raw: l.scope == "", Match: l.matcher()}
}

// headTotal is total over the whole log: under a filter, as the scan has walked it
// so far (the window alone would undercount the matches); else total.
func (l logsTab) headTotal() int {
	if !l.active() {
		return l.total()
	}
	n := l.scan.tally.Lines + l.scan.ext.Lines
	if !l.debug {
		n -= l.scan.tally.Debug + l.scan.ext.Debug
	}
	return max(n, len(l.rows()))
}

// rowScans counts rowsBy runs, for the once-per-View test.
var rowScans int

// ev is rows' index r's event.
func (l logsTab) ev(r int) eventlog.Event {
	if l.active() {
		return l.scan.found[r]
	}
	return l.events[r]
}

// active is whether a filter the scan answers is set.
func (l logsTab) active() bool { return l.kind != "" || l.query() != "" || l.scope != "" || l.errs }

// matcher is the filter as a test on one event, less the debug level.
func (l logsTab) matcher() func(eventlog.Event) bool {
	kind, words := l.kind, l.query()
	var kindFor func(string) contract.Kind
	if kind != "" { // the lookup map is only worth building under the f filter
		kindFor = l.kindLookup()
	}
	scope, kids, errs := l.scope, l.kids, l.errs
	return func(ev eventlog.Event) bool {
		switch {
		case ev.Level != "ERROR" && errs,
			scope != "" && ev.Bead != scope && !(kids && ev.Parent == scope),
			kind != "" && kindFor(ev.Bead) != kind:
			return false
		}
		if words == "" {
			return true
		}
		text := ev.Msg + " " + ev.Bead
		for _, a := range ev.Attrs {
			text += " " + a.K + " " + a.V
		}
		return matchWords(words, text)
	}
}

// prefilter is what every matching raw line must hold, lowercased: each query word,
// the L bead id, and the ERROR level. A word with a character JSON may escape is
// left out; a kind-only filter has nothing here and parses every line.
func (l logsTab) prefilter() []string {
	var pre []string
	for _, w := range strings.Fields(strings.ToLower(l.query())) {
		if !strings.ContainsAny(w, `"\<>&`) {
			pre = append(pre, w)
		}
	}
	if l.scope != "" {
		pre = append(pre, strings.ToLower(l.scope))
	}
	if l.errs {
		pre = append(pre, `"level":"error"`)
	}
	return pre
}

// rowsBy is the loaded events the L scope, e and the debug level leave, narrowed by
// kind and words: the header's "of" total counts it with none.
func (l logsTab) rowsBy(kind contract.Kind, words string) []int {
	rowScans++
	l.kind, l.words, l.typing = kind, words, false
	match := l.matcher()
	var rows []int
	for i := len(l.events) - 1; i >= 0; i-- {
		if ev := l.events[i]; (ev.Level != "DEBUG" || l.debug) && match(ev) {
			rows = append(rows, i)
		}
	}
	return rows
}

// kindLookup returns a lookup of a bead's kind: the loaded beads first, then the kinds read for the f filter.
func (l logsTab) kindLookup() func(id string) contract.Kind {
	loaded := make(map[string]contract.Kind, len(l.beads))
	for _, b := range l.beads {
		loaded[b.ID] = b.Kind
	}
	return func(id string) contract.Kind {
		if k, ok := loaded[id]; ok {
			return k
		}
		return l.kindOf[id]
	}
}

// kindsPresent are the kinds of the beads the events name, sorted: the picker's items.
func (l logsTab) kindsPresent() []contract.Kind {
	kindFor := l.kindLookup()
	var out []contract.Kind
	add := func(id string) {
		if k := kindFor(id); k != "" && !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	for id := range l.named {
		add(id)
	}
	for _, ev := range l.events {
		add(ev.Bead)
	}
	slices.Sort(out)
	return out
}

// current is the selected event: l.sel's if still visible, else the newest.
func (l logsTab) current() (int, *eventlog.Event) {
	rows := l.rows()
	if len(rows) == 0 {
		return -1, nil
	}
	for i, r := range rows {
		if ev := l.ev(r); ev.Key() == l.sel {
			return i, &ev
		}
	}
	ev := l.ev(rows[0])
	return 0, &ev
}

// selected is the selected event's bead, nil when the event names none or the snapshot lacks it.
func (l logsTab) selected() *bd.Bead {
	if _, ev := l.current(); ev != nil {
		return beadByID(l.beads, ev.Bead)
	}
	return nil
}

// beadByID is the snapshot's bead with that id, nil when absent.
func beadByID(beads []bd.Bead, id string) *bd.Bead {
	if i := slices.IndexFunc(beads, func(b bd.Bead) bool { return b.ID == id }); i >= 0 && id != "" {
		return &beads[i]
	}
	return nil
}

// pin anchors a zero selection on the event shown now, so it stops tracking newer
// events as they land (FR49). A set selection stays put, even while filtered out.
func (l logsTab) pin() logsTab {
	if _, ev := l.current(); ev != nil && l.sel == (eventlog.Key{}) {
		l.sel = ev.Key()
	}
	return l
}

func (l logsTab) bindings() []key.Binding {
	if l.typing {
		return []key.Binding{keyKeep, keyRevert}
	}
	_, ev := l.current()
	var bs []key.Binding
	switch {
	case l.open:
		bs = append(bs, keyShut, keyScroll)
	case ev != nil:
		bs = append(bs, keyDetail)
	}
	dbg, errs := keyDebugOn, keyErrOnly
	if l.debug {
		dbg.SetHelp("v", "-debug")
	}
	if l.errs {
		errs.SetHelp("e", "all")
	}
	bs = append(bs, keySearch, keyKind, dbg, errs)
	if l.filtered() && !l.open {
		bs = append(bs, keyClear)
	}
	return bs
}

// extra is moving, shown in ? alone.
func (l logsTab) extra() []key.Binding {
	if l.typing || len(l.rows()) < 2 { // one row has nowhere to move
		return nil
	}
	return []key.Binding{keyMove}
}

func (l logsTab) update(k tea.KeyMsg) (component, tea.Cmd) {
	l.memo = &logsMemo{}
	c, cmd := l.step(k)
	n := c.(logsTab)
	n.memo = nil // the keypress may change the rows
	return n, cmd
}

func (l logsTab) step(k tea.KeyMsg) (component, tea.Cmd) {
	if l.typing {
		switch {
		case key.Matches(k, keyKeep):
			l.words, l.typing = strings.TrimSpace(l.edit.text), false
		case key.Matches(k, keyRevert):
			l.typing = false
		default:
			l.edit = l.edit.apply(k)
		}
		return l, nil
	}
	rows := l.rows()
	i, ev := l.current()
	switch {
	case key.Matches(k, keyMove):
		if len(rows) == 0 {
			return l, nil
		}
		d := 1
		if s := k.String(); s == "k" || s == "up" {
			d = -1
		}
		if i+d == len(rows) && l.more && !l.active() { // past the oldest loaded row
			return l, func() tea.Msg { return olderWantedMsg{} }
		}
		l.sel, l.off = l.ev(rows[clampWrap(i+d, len(rows))]).Key(), 0
	case l.open && key.Matches(k, keyShut):
		l.open = false
	case !l.open && ev != nil && key.Matches(k, keyDetail):
		l.open = true
	case l.open && key.Matches(k, keyScroll):
		vp := l.detailVP()
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
		l.off = vp.YOffset
	case key.Matches(k, keyDebugOn):
		l.debug = !l.debug
	case key.Matches(k, keyErrOnly):
		l.errs = !l.errs
	case key.Matches(k, keySearch):
		l.typing, l.edit = true, lineAt(l.words)
	case key.Matches(k, keyKind):
		return l, func() tea.Msg { return pickLogKindMsg{} }
	case key.Matches(k, keyClear) && l.filtered(): // text, then kind, then scope
		switch {
		case l.words != "":
			l.words = ""
		case l.kind != "":
			l.kind = ""
		default:
			l.scope, l.kids = "", false
		}
	}
	return l, nil
}

func levelTag(level string) string {
	s := fmt.Sprintf("%-5s", level)
	switch level {
	case "ERROR":
		return warnSt.Render(s)
	case "WARN":
		return pinkFg.Render(s)
	}
	return dimSt.Render(s)
}

// header is the body's first line: the active filter and how many events it
// leaves, then the unparsed-line and level indicators (FR49); "" when none.
func (l logsTab) header(w int) string {
	return fitHeader(w-1, l.edit, l.headerWith)
}

// headerWith is header around the rendered filter input.
func (l logsTab) headerWith(input string) string {
	var segs []string
	if l.typing {
		segs = append(segs, "/ "+input)
	} else if l.words != "" {
		segs = append(segs, "/ "+l.words)
	}
	if l.kind != "" {
		segs = append(segs, "kind:"+string(l.kind))
	}
	if l.scope != "" {
		s := "scope:" + l.scope
		if l.kids {
			s += "+children"
		}
		segs = append(segs, s)
	}
	if l.filtered() || l.typing {
		segs = append(segs, fmt.Sprintf("%d of %d", len(l.rows()), l.headTotal()))
	}
	if l.scan.searching {
		segs = append(segs, dimSt.Render("searching…"))
	}
	if l.skipped > 0 {
		segs = append(segs, warnSt.Render(fmt.Sprintf("%d unparsed lines", l.skipped)))
	}
	if l.errs {
		segs = append(segs, pinkFg.Render("errors only"))
	}
	if l.debug {
		segs = append(segs, pinkFg.Render("+debug"))
	}
	return strings.Join(segs, dimSt.Render(" · "))
}

// empty tells "nothing happened" from "can't read" (FR51), cut to width (0: whole).
func (l logsTab) empty(width int) string {
	s, style := "no events yet", dimSt
	if l.scan.searching {
		s = "searching…"
	} else if len(l.events) > 0 || l.active() {
		s = "no matching events" // the filters or scope hide them all
	} else if l.path != "" {
		s += " — " + l.path
	}
	if l.err != nil {
		s, style = fmt.Sprintf("event log unavailable: %v", l.err), warnSt
	}
	if width > 0 {
		s = ansi.Truncate(s, width, "…")
	}
	return style.Render(s)
}

// detailText lists every attr of the selected event; a multi-line value (an err
// chain, a stderr tail) keeps its lines, indented under its key.
func (l logsTab) detailText() string {
	_, ev := l.current()
	if ev == nil || l.err != nil {
		return l.empty(0)
	}
	kw := 11 // the key column widens for a long (group sub-)key
	for _, a := range ev.Attrs {
		kw = max(kw, len(a.K))
	}
	var b strings.Builder
	indent := "\n" + strings.Repeat(" ", kw+1) // under the key column + its space
	for _, a := range ev.Attrs {
		v := strings.ReplaceAll(strings.TrimRight(a.V, "\n"), "\n", indent)
		fmt.Fprintf(&b, "%s %s\n", labelSt.Render(fmt.Sprintf("%-*s", kw, a.K)), v)
	}
	return strings.TrimRight(b.String(), "\n")
}

// bodyH is the height under the header line.
func (l logsTab) bodyH() int {
	if l.header(l.w) != "" {
		return max(l.h-1, 1)
	}
	return l.h
}

func (l logsTab) detailVP() viewport.Model {
	return drawerVP(l.open, l.w, l.bodyH(), l.detailText(), l.off)
}

func (l logsTab) view(w, h int) string {
	l.w, l.h = w, h
	l.memo = &logsMemo{}
	body := l.list(w, l.bodyH())
	if head := l.header(w); head != "" {
		return " " + ansi.Truncate(head, w-1, "") + "\n" + body
	}
	return body
}

// list is the event rows, over the detail drawer while it is open.
func (l logsTab) list(cw, h int) string {
	sel, _ := l.current()
	rows := l.rows()
	pre := 0 // the empty-state line above any rows
	if l.err != nil || len(rows) == 0 {
		pre = 1
	}
	line := func(n int) string {
		if n < pre {
			return " " + l.empty(cw-2)
		}
		i := n - pre
		ev := l.ev(rows[i])
		rest := ev.Msg
		if ev.Bead != "" {
			rest += " " + ev.Bead
		}
		if ev.Short != "" {
			rest += " " + ev.Short
		}
		// Date, clock and local zone (YYYY-MM-DD HH:MM:SS ZONE); the message gets the width left over.
		stamp := ev.Time.Local().Format("2006-01-02 15:04:05 MST")
		s := "  " + dimSt.Render(stamp) + " " + levelTag(ev.Level) + " " + ansi.Truncate(rest, max(cw-(9+ansi.StringWidth(stamp)), 1), "…") // 9 = marker, level and their spaces around the stamp
		if i == sel {
			// ponytail: the highlight strips the row's colours, an inner reset would end a background mid-line
			s = selRow.Width(cw).Render(ansi.Strip("❯ " + s[2:]))
		}
		return s
	}
	at := 0
	if sel >= 0 {
		at = pre + sel
	}
	return drawerWin(pre+len(rows), line, at, l.open, cw, h, l.detailVP(), nil)
}

// pickLogKind opens the Logs kind picker: any, then each kind the events name.
func (m Model) pickLogKind() (Model, tea.Cmd) {
	if m.kindsStale() {
		return m.readKinds(true)
	}
	l := m.tabAt(tabLogs).(logsTab)
	l.kindOf = m.logKindOf
	items := []string{"any"}
	kinds := l.kindsPresent()
	if l.kind != "" && !slices.Contains(kinds, l.kind) { // keep the active kind selectable, as Graph's picker does
		kinds = append(kinds, l.kind)
		slices.Sort(kinds)
	}
	for _, k := range kinds {
		items = append(items, string(k))
	}
	d := pickerDialog("Logs kind", items, "", func(m Model, d dialog) (Model, tea.Cmd) {
		l := m.tabAt(tabLogs).(logsTab)
		l.kind = contract.Kind(d.items[d.cur])
		if d.cur == 0 {
			l.kind = ""
		}
		m.tabs = slices.Clone(m.tabs)
		m.tabs[tabLogs] = l
		return m, nil
	})
	d.cur = max(slices.Index(items, string(l.kind)), 0)
	return m.push(d), nil
}
