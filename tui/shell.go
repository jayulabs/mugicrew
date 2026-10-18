// Package tui is the TUI a bare mugicrew opens (Spec mc-lu2x.2, ADRs
// 0141–0144): a header, folder tabs, the active tab's body, a status line and a
// hints bar generated from the same key.Binding set as the ? key reference.
package tui

import (
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/dispatch"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/session"
)

// Below the floor any layout is unreadable (ADR 0024).
const (
	floorCols = 40
	floorRows = 12
	chromeH   = 6 // header + 3 tab rows + status line + hints bar
)

// The tabs in display order: the one table the constants, names, jump keys and
// New's components all come from.
const (
	tabIdeate = iota
	tabDefog
	tabExecute
	tabGraph
	tabInits
	tabHistory
	tabStats
	tabLogs
	tabSettings
	numTabs
)

var tabDefs = [numTabs]struct {
	name string
	key  string // the jump key (ADR 0171 §1)
	new  func() component
}{
	tabIdeate:   {"Ideate", "1", func() component { return newIdea() }},
	tabDefog:    {"Defog", "2", newDefog},
	tabExecute:  {"Execute", "3", newExec},
	tabGraph:    {"Graph", "4", func() component { return newGraph() }},
	tabInits:    {"Initiatives", "5", newInits},
	tabHistory:  {"History", "6", func() component { return newHistory() }},
	tabStats:    {"Stats", "#", func() component { return newStats() }},
	tabLogs:     {"Logs", "$", func() component { return newLogs() }},
	tabSettings: {"Settings", "*", func() component { return settingsTab{} }},
}

var tabNames = func() (ns []string) {
	for _, d := range tabDefs {
		ns = append(ns, d.name)
	}
	return
}()

// component is one tab: its own state, update and view, the count of items
// awaiting the human (its ●N badge, FR10) and the verbs valid on its current
// selection, which feed both the hints bar and ? (FR27, FR30).
type component interface {
	update(tea.KeyMsg) (component, tea.Cmd)
	view(w, h int) string
	count() int
	actN() int // the Actionable section's row count, 0 for a tab without one
	bindings() []key.Binding
}

// extraKeys is an optional second binding set: keys a tab handles that its hints
// bar omits (moving), shown in ? and counted by the binding-coverage test.
type extraKeys interface{ extra() []key.Binding }

// liveCounter is an optional interface: a tab that reports its Live section's row count.
type liveCounter interface{ liveN() int }

// tabExtra is the active tab's extra keys, for ? and the coverage test.
func (m Model) tabExtra() []key.Binding {
	_, r := m.retroKeys()
	if m.focus.id != "" {
		return slices.Concat(m.focusExtra(), r)
	}
	if e, ok := m.tabAt(m.tab).(extraKeys); ok {
		return slices.Concat(e.extra(), r)
	}
	return r
}

// tabAt is tab i with the shell's live data handed over: the shell owns the data, so
// no tab holds a stale copy between a refresh and a key or a frame.
func (m Model) tabAt(i int) component {
	switch t := m.tabs[i].(type) {
	case graphTab:
		t.graphData = m.gateData()
		if t.header(m.w) != "" { // the filter line takes one body row
			t.h--
		}
		return t
	case ideaTab:
		t.ideaData = ideaData{beads: m.beads, sessions: m.sessions, gate: m.gateData(), w: m.w, h: m.h - chromeH}
		// the badge must not depend on folds: the waiting band is fold-independent (ADR 0177 §2).
		wait, _, _ := t.gate.ideaBands()
		t.waiting = len(wait)
		return t
	case defogTab:
		t.g, t.w, t.h = m.gateData(), m.w, m.h-chromeH
		return t
	case execTab:
		t.g, t.w, t.h = m.gateData(), m.w, m.h-chromeH
		return t
	case initsTab:
		t.g, t.w, t.h = m.gateData(), m.w, m.h-chromeH
		t.stats = m.inits
		t.waits = m.initWaits
		return t
	case histTab:
		t.histData = histData{m.histCommits, m.histErr, m.beads, m.w, m.h - chromeH}
		return t
	case logsTab:
		t.logsData = logsData{m.logs, m.beads, m.logSkipped, m.logErr, m.logPath, m.logCur != (eventlog.Cursor{}) && !m.logCur.Done, m.logKindOf, m.logBeads, m.w, m.h - chromeH}
		return t
	case settingsTab:
		t.m = m
		return t
	case statsTab:
		t.statsData = statsData{m.digest, m.usageBand(), m.w}
		return t
	}
	return m.tabs[i]
}

// gateData is the shell's live snapshot the launch, action and form gates read
// (launchWhy, blocked, openBlockers, canAdopt…); any tab's keys route through it.
func (m Model) gateData() graphData {
	return graphData{
		beads: m.beads, idx: m.idx, live: m.live, sessions: m.sessions, efforts: m.efforts, cpTips: m.cpTips, landEv: m.landEv, launching: m.launching, launched: m.launched,
		pending: m.pending, crew: m.crew, comments: m.comments, rollups: m.rollups, extRef: m.initiatives.ExternalRef, w: m.w, h: m.h - chromeH,
	}
}

// selection is the active tab's selected bead, nil if it has none or no notion of one.
func (m Model) selection() *bd.Bead {
	if m.focus.id != "" { // the shared action paths act on the focus's cursor row
		return m.focusBead()
	}
	if s, ok := m.tabAt(m.tab).(interface{ selected() *bd.Bead }); ok {
		return s.selected()
	}
	return nil
}

// placeholder is a tab with no content yet: one dimmed line, no verbs, count 0.
// No tab is one now (FR57); the type stays for a tab not yet built.
type placeholder struct{ name, line string }

func (p placeholder) update(tea.KeyMsg) (component, tea.Cmd) { return p, nil }
func (p placeholder) view(w, h int) string                   { return dimSt.Render(" " + p.name + " " + p.line) }
func (placeholder) count() int                               { return 0 }
func (placeholder) actN() int                                { return 0 }
func (placeholder) bindings() []key.Binding                  { return nil }

// jumpKeys are the tabs' jump keys, in table order.
func jumpKeys() (ks []string) {
	for _, d := range tabDefs {
		ks = append(ks, d.key)
	}
	return
}

// settingsTab is the Settings tab. Its cache and staged edits live on the Model
// (the poller and the saves share them), so tabAt hands it the whole shell and
// tabKey takes back what a key did to it.
type settingsTab struct{ m Model }

func (t settingsTab) update(k tea.KeyMsg) (component, tea.Cmd) {
	next, cmd, _ := t.m.settingsKey(k.String())
	t.m = next.(Model)
	return t, cmd
}
func (t settingsTab) view(w, h int) string    { return t.m.settingsView(h) }
func (settingsTab) count() int                { return 0 }
func (settingsTab) actN() int                 { return 0 }
func (t settingsTab) bindings() []key.Binding { return t.m.settingsBindings() }

// Shell keys (ADR 0142): shown in ?, never in the hints bar (FR29).
var (
	keyJump     = key.NewBinding(key.WithKeys(jumpKeys()...), key.WithHelp("1-6 # $ *", "jump to tab"))
	keyNext     = key.NewBinding(key.WithKeys("tab", "right", "l"), key.WithHelp("tab/→/l", "next tab"))
	keyPrev     = key.NewBinding(key.WithKeys("shift+tab", "left", "h"), key.WithHelp("⇧tab/←/h", "prev tab"))
	keyQuit     = key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit"))
	keyLoop     = key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "loop"))
	keyExplorer = key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "explorer"))
	keyF5       = key.NewBinding(key.WithKeys("f5"), key.WithHelp("f5", "refresh"))
	keyRef      = key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "keys"))
	keyYank     = key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy bead id"))
	keyRecents  = key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "recents"))
	keyClose    = key.NewBinding(key.WithKeys("?", "esc", "q"), key.WithHelp("?/esc", "close"))
	shellKeys   = []key.Binding{keyJump, keyNext, keyPrev, keyLoop, keyExplorer, keyYank, keyF5, keyRecents, keyDiscussions, keySessions, keyQuit, keyRef}
)

// Model is the TUI shell: it owns the tab index, the live crew data, the modal
// stack and the ? reference, and routes a key to the topmost modal first, then the
// shell keys, then the active tab.
type Model struct {
	ctx        contract.Ctx
	crew       map[string]contract.Crewmate
	tabs       []component
	tab        int
	jiraSet    bool // the [jira] site was set when the slow tier last loaded
	surgerySet bool // some Surgery target existed then
	ref        bool // the ? key reference is open
	refOff     int  // its first visible line (Spec mc-bruk D7)
	focus           // the Initiative focus drilldown over the active tab (ADR 0173); id "" is off
	w, h       int

	settingsState // the Settings tab's cache and staged edits (FR54–56)
	usageState    // the Plan usage poller's cache and backoff (FR53)

	modals []dialog // the modal stack, topmost last
	status string   // the last action's result (FR12)
	// deferred is a result that landed under a modal; it shows once the stack empties.
	deferred    string
	loadErr     error
	loaded      bool                     // a snapshot has loaded; before it Recents prunes nothing
	loading     bool                     // a tick's load is in flight; the next tick skips its load
	slowAt      time.Time                // when listAll last ran; zero until the first slow load
	slowWant    bool                     // Graph entry landed on a load in flight: the next tick runs the slow tier
	reqSeq      int                      // slowLoad requests made; a load clears a request's flags only if it began after the last
	closed      []bd.Bead                // the last --all's closed lineage, the fast tier's cache (ADR 0197)
	demand      map[string]bool          // ids in closed that only a claim brought; evicted when unclaimed (ADR 0232)
	logKindOf   map[string]contract.Kind // every bead's kind, read for the Logs f filter; nil until asked (ADR 0232)
	logKindAt   time.Time                // when logKindOf was read: stale once a slow tier has loaded since
	logKindBusy bool                     // a read of logKindOf is in flight
	logKindPick bool                     // an f press arrived while it was: open the picker when it lands
	rollups     map[string]rollupEntry   // the open drawer's beads' ROLLUP totals, fetched in a Cmd; render only reads it
	lineage     *bd.Lineage              // the lineage read at snapshot lineageGen, reused across beads
	lineageGen  int
	snapGen     int                         // bumped by each slow-tier snapshot
	comments    map[commentKey]commentEntry // the open drawer's beads' comments, fetched in a Cmd; render only reads it
	extra       map[string]bd.Bead          // closed beads a G, Recents jump or Goto menu fetched, held while a claim names them (ADR 0232)
	revealed    map[string]bool             // the ids of the last G/Recents fetch: bead and ancestors; claimed while Graph is the tab
	gotoHeld    map[string]bool             // the ids the open Goto menu's fetch brought; claimed while the menu is open
	demandGen   int                         // the newest reveal fetch; an older one landing is dropped
	searchGen   int                         // the newest closed search; an older one landing is dropped
	gotoGen     int                         // the newest Goto menu open; an older menu's fetch landing is dropped
	hits        map[string]bool             // the closed word search's hits and their groups; claimed while Graph is the tab
	searched    query                       // the query the hits answer; words "" when no search has run
	openIDs     map[string]bool             // the open set's ids at the last load, for drop-out detection

	// beads is the graph's work beads; event beads never land here. idx indexes this exact
	// slice: any edit that reorders, adds or removes beads must rebuild it. The priority echo
	// clones the slice and edits a field in place, so it keeps idx.
	beads       []bd.Bead
	idx         *snapIndex
	live        map[string]bool         // effort liveness: active AFK effort id → alive; display only
	sessions    map[string]session.Info // the tmux session table; display and attach only
	recents     []string                // Recents, read with each load and after each save; never read on the event loop
	recentSaves int                     // saves folded so far: a load that began before one holds a stale Recents
	efforts     map[string]bool         // bead ids holding effort/<id>
	cpTips      map[string]string       // Checkpoint id → its checkpoint/<id> tip sha; empty when unread
	landEv      map[string]string       // bead id → latest landing-relevant event msg, folded from logs
	inits       map[string]initStats    // the Initiatives tab's line-2 counts, per refresh
	initWaits   map[string]bool         // Initiatives whose session waits (row ●), same snapshot as inits

	loop           looplock.Status // the loop per the last read; display and o only
	loopBusy       bool            // a loop verb has not returned, or (a stop) no read has confirmed it
	loopFrom       looplock.Status // the loop that verb acted on
	loopStarting   bool            // o's start is unconfirmed: the header reads loop starting…
	loopRestarting bool            // a `loop restart` has not returned: the menu's options are dimmed and inert
	watched        bool            // opened with --watch: o starts a watched loop

	runs      []dispatch.Inflight  // run-now efforts in flight, oldest first
	launched  map[string]time.Time // bead id → when its launch succeeded; cleared by a loadedMsg that began after that and shows it alive, or one launchHoldMax on
	waitSince map[string]time.Time // bead id → first snapshot its session read waiting; dropped when it stops (Sessions modal order)
	launching map[string]bool      // bead ids whose session start is in flight (ADR 0107 §5)
	attachTo  string               // the one in-flight launch that switches this terminal (ADR 0107 §6)
	pending   map[string]string    // bd writes in flight, key → verb (ADR 0098 §3)

	evlog                *eventlog.Log        // the process's event log (WithLog); nil reads as no path
	histCommits          []git.MainlineCommit // History's last mainline read, newest first
	histErr              error                // that read failed; History shows it
	logs                 []eventlog.Event
	logBeads             map[string]bool  // every bead id the whole log names, from the digest scan; the window's tail adds its own
	digestPending        bool             // a digestCmd is out: the window alone must not seed the finished-flash
	digestGen, digestWin int              // the live whole-log scan, and how many digest events precede the tail
	digest               []eventlog.Event // the full-log events Stats, landEv and the finished-flash read (ADR 0231 §3)
	logSig               eventlog.Sig
	logCur               eventlog.Cursor       // where Logs' older reads continue (ADR 0231 §1)
	logCurSig            eventlog.Sig          // the Sig logCur was taken at, for rotation
	logPaging            bool                  // an older page is being read
	logOff               int64                 // live-file bytes already read (logSig's incremental half)
	logSkipped           int                   // unparsed lines the last read skipped
	logErr               error                 // the log is unavailable: unresolvable or unreadable
	logPath              string                // the log's path, for the empty state
	exitsSeen            map[eventlog.Key]bool // hitl.exited events already read; nil until the first read seeds it
	closingSeen          map[string]bool       // bead ids the last read showed Closing; nil until seeded
}

// WithLog hands the model the process's event log: the ✓ finished flash reads it.
func (m Model) WithLog(l *eventlog.Log) Model {
	m.evlog = l
	return m
}

func newTabs() []component {
	cs := make([]component, numTabs)
	for i, d := range tabDefs {
		cs[i] = d.new()
	}
	return cs
}

func New(ctx contract.Ctx, crew map[string]contract.Crewmate) Model {
	return Model{
		ctx:     ctx,
		crew:    crew,
		tabs:    newTabs(),
		loading: true, // Init's slow load is in flight; a tick must not stack another
		w:       80,
		h:       24,
	}
}

func (m Model) Init() tea.Cmd { return tea.Batch(m.loadSlow(), m.loadLogs(), tick(), m.pollUsage()) }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if s, ok := msg.(logsScanMsg); ok {
		return m.foldScan(s)
	}
	next, cmd := m.update(msg)
	m, scan := next.(Model).syncScan()
	m, comments := m.syncComments()
	m, rollup := m.syncRollup()
	return m, tea.Batch(cmd, scan, comments, rollup)
}

// copyWriter puts s on the operator's clipboard; tests swap it out.
var copyWriter = oscCopy(os.Stdout)

// oscCopy is a writer that sends s to w as an OSC 52 sequence (write-and-forget).
func oscCopy(w io.Writer) func(string) { return termenv.NewOutput(w).Copy }

// yank copies the selected bead's id over OSC 52 and says so; no selection does nothing.
func (m Model) yank() (tea.Model, tea.Cmd) {
	b := m.selection()
	if b == nil {
		return m, nil
	}
	id := b.ID
	m.setStatus("copied " + id)
	return m, func() tea.Msg { copyWriter(id); return nil }
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.setTop = m.settingsTop(m.setTop)
	case tea.KeyMsg:
		return m.keyRecording(msg)
	default:
		old := m.focusKeys()
		if next, cmd, ok := m.fold(msg); ok {
			return next.(Model).focusSettle(old).sessionsSync(), cmd
		}
	}
	return m, nil
}

func (m Model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.w < floorCols || m.h < floorRows { // only the too-small notice shows, so only quit acts; modals and tab state wait for the window to grow back (FR15, FR24)
		if key.Matches(k, keyQuit) {
			return m.quit()
		}
		return m, nil
	}
	if m.loadErr != nil && len(m.modals) == 0 { // a dialog draws over the notice and owns its keys; bare, only f5 and q act
		switch {
		case key.Matches(k, keyQuit):
			return m.quit() // an in-flight effort or write still gets its Quit? dialog (FR20, ADR 0082)
		case key.Matches(k, keyF5):
			return m, tea.Batch(m.slowLoad(), m.loadLogs())
		}
		return m, nil
	}
	if len(m.modals) > 0 { // the top modal owns every key (FR21), but ctrl+c quits from a dialog's input as from a tab's
		if k.Type == tea.KeyCtrlC && m.modals[len(m.modals)-1].kind != confirm && m.modals[len(m.modals)-1].kind != picker {
			return m.quit()
		}
		return m.modalKey(k)
	}
	if m.ref { // modal: no shell key fires under it (FR21), but ctrl+c quits as from a dialog's input
		if k.Type == tea.KeyCtrlC {
			return m.quit()
		}
		return m.refKey(k), nil
	}
	if m.capturing() && k.Type != tea.KeyCtrlC { // a tab's text input owns every key but quit
		return m.graphQueryKey(k)
	}
	if key.Matches(k, keyRecents) {
		return m.recentsOpen()
	}
	if key.Matches(k, keyDiscussions) {
		return m.discussionsOpen()
	}
	if key.Matches(k, keySessions) {
		return m.sessionsOpen()
	}
	if key.Matches(k, keyGoto) {
		return m.gotoOpen()
	}
	if key.Matches(k, keyNew) {
		return m.newOpen()
	}
	if key.Matches(k, keyTranscript) && (m.focus.id != "" || m.tab <= tabInits) {
		if b := m.selection(); b != nil {
			return m.transcript(b.ID)
		}
		return m, nil
	}
	if h, e := m.retroKeys(); key.Matches(k, keyRetro) && len(h)+len(e) > 0 {
		return m.retrospective(m.selection())
	}
	if key.Matches(k, keyYank) {
		return m.yank()
	}
	if m.focus.id != "" {
		if next, cmd, ok := m.focusKey(k); ok {
			return next, cmd
		}
	}
	switch {
	case key.Matches(k, keyLaunch) && m.focus.id == "" && m.tab == tabInits: // before the launch path: the cut session is never launched from here (ADR 0164 §7)
		if m.selection() == nil { // a section header
			return m, nil
		}
		return m.focusOpen()
	case key.Matches(k, keyQuit):
		return m.quit()
	case key.Matches(k, keyLoop):
		return m.openLoopMenu()
	case key.Matches(k, keyExplorer):
		return m.openExplorer()
	case key.Matches(k, keyF5):
		m.setStatus("refreshing…")
		m = m.retryComments().retryRollup()
		cmds := []tea.Cmd{m.slowLoad(), m.loadLogs()}
		if m.tab == tabStats {
			cmds = append(cmds, m.refreshUsage())
		}
		return m, tea.Batch(cmds...)
	case key.Matches(k, keyRef):
		m = m.scanNew()
		m.ref, m.refOff = true, 0
	case key.Matches(k, keyNext):
		return m.enter((m.tab + 1) % len(m.tabs))
	case key.Matches(k, keyPrev):
		return m.enter((m.tab + len(m.tabs) - 1) % len(m.tabs))
	case key.Matches(k, keyJump):
		for i, d := range tabDefs {
			if d.key == k.String() {
				return m.enter(i)
			}
		}
	default:
		switch m.tabAt(m.tab).(type) {
		case ideaTab:
			if next, cmd, ok := m.ideaKey(k); ok {
				return next, cmd
			}
		case defogTab:
			if next, cmd, ok := m.defogKey(k); ok {
				return next, cmd
			}
		case execTab:
			if next, cmd, ok := m.execKey(k); ok {
				return next, cmd
			}
		case initsTab:
			if next, cmd, ok := m.initsKey(k); ok {
				return next, cmd
			}
		case graphTab:
			if next, cmd, ok := m.graphKey(k); ok {
				return next, cmd
			}
			if next, cmd, ok := m.actionKey(k); ok {
				return next, cmd
			}
			if key.Matches(k, keyFold) && !m.tabAt(tabGraph).(graphTab).query().active() {
				old := m.tabAt(tabGraph).(graphTab).flip
				next, cmd := m.tabKey(k)
				if nm := next.(Model); !maps.Equal(old, nm.tabAt(tabGraph).(graphTab).flip) {
					next = nm.saveGraphFolds()
				}
				return next, cmd
			}
			return m.graphQueryKey(k)
		}
		return m.tabKey(k)
	}
	return m, nil
}

// graphQueryKey is a key for the active tab, saving the Graph query if it committed a
// change (a keystroke in the / input changes only the edit, never q).
func (m Model) graphQueryKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	g, isGraph := m.tabAt(m.tab).(graphTab)
	wasAll := m.claimsAllClosed()
	next, cmd := m.tabKey(k)
	if isGraph && !(g.visit && key.Matches(k, keyClear)) { // esc out of a visit restores the baseline, not a saved query
		next = next.(Model).saveGraphFilter(g.q)
	}
	if isGraph {
		nm, sc := next.(Model).searchSync()
		lc := nm.claimLoad(wasAll)
		next, cmd = nm, tea.Batch(cmd, sc, lc)
	}
	return next, cmd
}

// saveGraphFilter writes the committed Graph query when it differs from old; a failure
// goes on the status line, the query kept in memory (ADR 0176 §4).
func (m Model) saveGraphFilter(old query) Model {
	g := m.tabAt(tabGraph).(graphTab)
	if g.q == old || g.visit {
		return m
	}
	if err := g.saveQuery(); err != nil {
		m.setStatus("filter won't survive a restart: " + err.Error())
	}
	return m
}

// saveGraphFolds writes the Graph folds; a failure goes on the status line, the folds
// kept in memory.
func (m Model) saveGraphFolds() Model {
	if err := m.tabAt(tabGraph).(graphTab).save(); err != nil {
		m.setStatus("folds won't survive a restart: " + err.Error())
	}
	return m
}

// quit exits at once, or asks first while an effort, write or launch is in flight.
// A Quit? already on top is not stacked again (below the floor every q lands here).
func (m Model) quit() (tea.Model, tea.Cmd) {
	if len(m.runs) == 0 && len(m.pending) == 0 && len(m.launching) == 0 {
		return m, tea.Quit
	}
	if n := len(m.modals); n > 0 && m.modals[n-1].title == "Quit?" {
		return m, nil
	}
	runs, pending, launching := m.runs, m.pending, len(m.launching)
	body := func(w int) []string {
		s := quitPrompt(runs, pending, w)
		if launching > 0 { // ADR 0098 §6: quitting must not silently strand a claim
			s = strings.TrimSpace(fmt.Sprintf("%d launching — a claimed bead may be left without a session. %s", launching, s))
			if len(runs) == 0 && len(pending) == 0 {
				s += " Quit?"
			}
		}
		return []string{s}
	}
	return m.push(promptDialog("Quit?", body, "",
		func(m Model, _ dialog) (Model, tea.Cmd) {
			dispatch.Abandon(m.runs, pauseBead) // paused:abandoned before each effort.abandoned (ADR 0082)
			return m, tea.Quit
		})), nil
}

// graphEntered is the rule for any switch to the Graph: it shows closed lineage, so read it
// now, or on the next tick when a load is already in flight (never stacked).
func (m *Model) graphEntered() tea.Cmd {
	var cmd tea.Cmd
	*m, cmd = m.searchSync() // the remembered hits are re-read by this load's claims
	return tea.Batch(cmd, m.slowLoad())
}

// enter switches to tab i. Entering Stats re-sums, since its window edge moved with
// time, and reads Plan usage past its floor (FR15).
func (m Model) enter(i int) (tea.Model, tea.Cmd) {
	m.tab, m.focus = i, focus{} // a tab key leaves the focus and goes to that tab
	if i == tabGraph {
		m.tabs = slices.Clone(m.tabs)
		g := m.tabAt(tabGraph).(graphTab)
		if g.sel != "" && !slices.ContainsFunc(m.beads, func(b bd.Bead) bool { return b.ID == g.sel }) && (m.claimsAllClosed() || m.hits[g.sel] || m.revealed[g.sel]) {
			g.searching, g.entry = true, true // the entry load re-reads the cursor bead: park, don't forget it
		}
		m.tabs[tabGraph] = g.rebuild() // catch up on loads that skipped it
		if cmd := m.graphEntered(); cmd != nil {
			return m, cmd
		}
	}
	if i != tabStats {
		return m, nil
	}
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabStats] = m.tabAt(tabStats).(statsTab).recompute()
	cmd := m.refreshUsage() // mutates m, so read before the return copies it
	return m, cmd
}

// tabKey hands k to the active tab.
func (m Model) tabKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.tabs = slices.Clone(m.tabs) // a copied Model must not share the tab it updates
	m.tabs[m.tab], cmd = m.tabAt(m.tab).update(k)
	if st, ok := m.tabs[m.tab].(settingsTab); ok { // take back what the key did to the shell
		m = st.m
		m.tabs = slices.Clone(m.tabs)
		m.tabs[m.tab] = settingsTab{}
	}
	return m, cmd
}

// capturing reports whether the active tab is typing into an input, which takes
// every key before the shell's.
func (m Model) capturing() bool {
	c, ok := m.tabAt(m.tab).(interface{ capturing() bool })
	return ok && c.capturing()
}

// modeBindings is every key the current mode handles: the binding-coverage
// test's single source.
func (m Model) modeBindings() []key.Binding {
	if len(m.modals) > 0 {
		return m.modals[len(m.modals)-1].bindings()
	}
	if m.ref {
		return []key.Binding{keyRefScroll, keyClose, key.NewBinding(key.WithKeys("ctrl+c"))} // ctrl+c quits under the reference too
	}
	if m.capturing() {
		return m.tabBindings()
	}
	return slices.Concat(m.tabBindings(), m.tabExtra(), m.gotoBindings(), m.newBindings(), shellKeys)
}

var (
	accent    = lipgloss.Color("212")
	hintBg    = lipgloss.Color("236")
	dim       = lipgloss.Color("240")
	dimSt     = lipgloss.NewStyle().Foreground(dim)
	warnSt    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	aliveSt   = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	afkSt     = lipgloss.NewStyle().Foreground(lipgloss.Color("151"))
	waitSt    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	cursorSty = lipgloss.NewStyle().Reverse(true) // a line input's cursor cell
	brand     = lipgloss.NewStyle().Bold(true).Foreground(accent)
	badgeSt   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	hintBar   = lipgloss.NewStyle().Background(hintBg)
	refBox    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(0, 1)

	// variant A folder tabs (prototype mc-lu2x.1): the active one opens into the body
	boxActive   = lipgloss.Border{Top: "─", Bottom: " ", Left: "│", Right: "│", TopLeft: "╭", TopRight: "╮", BottomLeft: "┘", BottomRight: "└"}
	boxInactive = lipgloss.Border{Top: "─", Bottom: "─", Left: "│", Right: "│", TopLeft: "╭", TopRight: "╮", BottomLeft: "┴", BottomRight: "┴"}
	tabActive   = lipgloss.NewStyle().Border(boxActive).BorderForeground(accent).Foreground(accent).Bold(true).Padding(0, 1)
	tabInactive = lipgloss.NewStyle().Border(boxInactive).BorderForeground(dim).Foreground(lipgloss.Color("245")).Padding(0, 1)

	hintHelp = func() help.Model {
		h := help.New()
		hs := &h.Styles // the bar's background must survive each key's style reset
		hs.ShortKey = hs.ShortKey.Background(hintBg).Foreground(lipgloss.Color("252")).Bold(true)
		hs.ShortDesc = hs.ShortDesc.Background(hintBg).Foreground(lipgloss.Color("245"))
		hs.ShortSeparator = hs.ShortSeparator.Background(hintBg).Foreground(dim)
		return h
	}()
)

// shortHelpView is bubbles' help.ShortHelpView without its unstyled " " between
// a key and its label: the key, then " "+desc in the desc style, joined by the
// separator; disabled bindings are skipped.
func shortHelpView(bs []key.Binding) string {
	st := hintHelp.Styles
	var parts []string
	for _, b := range bs {
		if !b.Enabled() {
			continue
		}
		p := st.ShortKey.Inline(true).Render(b.Help().Key)
		if b.Help().Desc != "" { // key-only (FR10)
			p += st.ShortDesc.Inline(true).Render(" " + b.Help().Desc)
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, st.ShortSeparator.Inline(true).Render(" • "))
}

// tabBindings are the active tab's verbs.
func (m Model) tabBindings() []key.Binding {
	h, _ := m.retroKeys()
	if m.focus.id != "" {
		fb := m.focusBindings()
		return slices.Concat(fb[:len(fb)-1], h, fb[len(fb)-1:]) // esc back stays last
	}
	return slices.Concat(m.tabAt(m.tab).bindings(), h)
}

// tabLabel is "<jump key> <Name>", plus amber ●N while N sessions wait on you, else a
// light-blue ●N for the N Actionable rows (Spec mc-lu2x.38 D3), else a bold green ●N for
// the N Live rows of a tab that reports them (liveCounter).
func tabLabel(i int, c component) string {
	l := tabDefs[i].key + " " + tabNames[i]
	if n := c.count(); n > 0 {
		l += " " + badgeSt.Render(fmt.Sprintf("●%d", n))
	} else if n := c.actN(); n > 0 {
		l += " " + selMarker.Render(fmt.Sprintf("●%d", n))
	} else if lc, ok := c.(liveCounter); ok && lc.liveN() > 0 {
		l += " " + aliveSt.Bold(true).Render(fmt.Sprintf("●%d", lc.liveN()))
	}
	return l
}

// tabBar renders the folder tabs into exactly w columns, sliding a window of
// whole tabs that always holds the active one; ‹ › on the label row mark the
// clipped sides (FR7, FR8).
func tabBar(labels []string, active, w int) string {
	blocks := make([]string, len(labels))
	for i, l := range labels {
		if i == active {
			blocks[i] = tabActive.Render(l)
		} else {
			blocks[i] = tabInactive.Render(l)
		}
	}
	span := func(a, b int) (n int) {
		for i := a; i <= b; i++ {
			n += lipgloss.Width(blocks[i])
		}
		return n
	}
	fill := func(n int) string { return "\n\n" + dimSt.Render(strings.Repeat("─", n)) }
	avail := w - 4 // room for both markers
	start, end := 0, active
	for start < active && span(start, active) > avail {
		start++
	}
	for end+1 < len(labels) && span(start, end+1) <= avail {
		end++
	}
	clipped := start > 0 || end < len(labels)-1
	marker := func(on bool, s string) string {
		col := strings.Split(fill(2), "\n") // the marker continues the rule line
		if on {
			col[1] = lipgloss.NewStyle().Foreground(accent).Bold(true).Render(s)
		}
		return strings.Join(col, "\n")
	}
	var parts []string
	if clipped {
		parts = append(parts, marker(start > 0, "‹ "))
	}
	parts = append(parts, blocks[start:end+1]...)
	if clipped {
		parts = append(parts, marker(end < len(labels)-1, " ›"))
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Bottom, parts...)
	if gap := w - lipgloss.Width(bar); gap > 0 {
		bar = lipgloss.JoinHorizontal(lipgloss.Bottom, bar, fill(gap))
	}
	return bar
}

// flatHints is a dialog's or input's keys as one flat line (no sections, no rank): when
// it does not fit w it shortens every label to key-only (ADR 0229 §4; Spec FR18).
func flatHints(bs []key.Binding, w int) string {
	bs = helpable(bs)
	if line := shortHelpView(bs); lipgloss.Width(line) <= w {
		return line
	}
	for i := range bs {
		bs[i].SetHelp(bs[i].Help().Key, "")
	}
	return shortHelpView(bs)
}

// hintsBar is a flat line on the bar's own background, exactly w wide; blank when bs is empty.
func hintsBar(bs []key.Binding, w int) string {
	return hintBar.Width(w).Render(ansi.Truncate(" "+flatHints(bs, w-1), w, ""))
}

// helpable drops bindings with no help key: they are handled (the up halves of
// j/k pairs) but would render as blank hint items.
func helpable(bs []key.Binding) []key.Binding {
	return slices.DeleteFunc(slices.Clone(bs), func(b key.Binding) bool { return b.Help().Key == "" })
}

// fit pads or clips s to exactly h lines, none wider than w: a taller frame makes
// bubbletea drop its top lines, a wider one wraps and desyncs the renderer.
func fit(s string, w, h int) string {
	ls := strings.Split(s, "\n")
	ls = append(ls, make([]string, max(0, h-len(ls)))...)[:h]
	for i, l := range ls {
		ls[i] = ansi.Truncate(l, w, "")
	}
	return strings.Join(ls, "\n")
}

func (m Model) View() string {
	if m.loadErr != nil && len(m.modals) == 0 {
		return m.noticeView("bd read failed: "+m.loadErr.Error(), "press f5 to retry, q to quit")
	}
	if m.w < floorCols || m.h < floorRows {
		wrap := lipgloss.NewStyle().Width(m.w)
		return fit(wrap.Render("terminal too small")+"\n"+
			wrap.Inherit(dimSt).Render(fmt.Sprintf("need %dx%d, have %dx%d — enlarge to continue", floorCols, floorRows, m.w, m.h)), m.w, m.h)
	}
	if len(m.modals) > 0 {
		return m.modalView()
	}
	if m.ref {
		return m.refView()
	}
	return m.frame()
}

// frame is the header, tabs, body, status line and hints bar: what a dialog or ?
// is drawn over.
func (m Model) frame() string {
	if m.focus.id != "" {
		return m.focusFrame()
	}
	labels := make([]string, len(m.tabs))
	for i := range m.tabs {
		labels[i] = tabLabel(i, m.tabAt(i)) // the badge reads live data
	}
	if m.settingsStaged() { // FR11
		labels[tabSettings] += " •"
	}
	body := fit(m.tabAt(m.tab).view(m.w, m.h-chromeH), m.w, m.h-chromeH)
	return fit(strings.Join([]string{
		m.headerView(),
		tabBar(labels, m.tab, m.w),
		body,
		" " + strings.ReplaceAll(m.status, "\n", " "), // status line
		m.hintsView(),
	}, "\n"), m.w, m.h)
}

// hintsView is the bar for the current mode: blank under a dialog or ?, a tab input's
// keep/revert, or the tab's verbs plus ? keys (FR24, ADR 0229).
func (m Model) hintsView() string {
	if len(m.modals) > 0 || m.ref { // the box alone holds the keys (FR15, FR16)
		return hintsBar(nil, m.w)
	}
	if m.capturing() { // / has no box and ? would be typed: flat, no ? keys (FR17)
		return hintsBar(m.tabBindings(), m.w)
	}
	return sectionedBar(slices.Concat(m.tabBindings(), m.gotoBindings(), m.newBindings()), m.w, true)
}

// headerView is mugicrew on the left, the crew's state on the right (FR6).
func (m Model) headerView() string {
	left := " " + brand.Render("mugicrew")
	right := m.headerRight(m.w - lipgloss.Width(left) - 1)
	return left + strings.Repeat(" ", max(1, m.w-lipgloss.Width(left)-lipgloss.Width(right))) + right
}
