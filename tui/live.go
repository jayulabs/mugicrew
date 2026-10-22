package tui

// Live crew data (FR5): the seams, the refresh tick, the single loadedMsg
// fold, the write guard (ADR 0098), the launch in-flight state (ADR 0107) and
// the status flashes.

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/chopper"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/dispatch"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/liveness"
	"jayulabs.org/mugicrew/loop"
	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/session"
)

// refreshInterval is the periodic refresh cadence (3 s): light with bd shell-out.
const refreshInterval = 3 * time.Second

// slowInterval is the closed-lineage cadence (ADR 0197): `bd list --all` re-reads at
// most this often by elapsed time, besides startup, the Graph tab, F5 and a drop-out.
const slowInterval = 30 * time.Second

// The seams, vars so tests run without a live bd, tmux, git or loop.
var (
	execCommand   = exec.Command
	listAll       = bd.ListAll
	listByID      = bd.ListByID
	defaultIDs    = bd.DefaultIDs
	listOpen      = bd.OpenSet
	readSessions  = session.Sessions
	recordClient  = session.RecordClient
	setBars       = session.SetBars
	probeLiveness = liveness.Alive
	readRefs      = bd.RefsAcross
	switchSession = session.SwitchTo
	watchRun      = session.Watch
	pauseBead     = bd.Pause
	unpauseBead   = bd.Unpause
	newestLog     = eventlog.ReadNewest
	olderLog      = eventlog.ReadOlder
	tailLog       = eventlog.ReadFrom
	startSession  = session.Start
	resumeSession = session.Resume
	dropSession   = session.Drop
	dropDrafts    = session.DropDrafts
	finishSession = session.Finish
	dropOrphan    = session.DropOrphan
	treeDirty     = git.Dirty
	hitlClaim     = bd.Claim
	hitlUnclaim   = bd.Unclaim
	abortEffort   = dispatch.Abort
	assessRecover = chopper.Assess
	recoverBead   = chopper.Recover
	ownSession    = session.OwnSession
	// run-now (enter): the same entry points the loop calls (FR59).
	readyFrontier      = bd.Ready
	claimBead          = dispatch.Claim
	drainBlockers      = func(id string) (int, []bd.Bead, error) { return bd.DrainBlockers(id, nil) }
	inProgressBeads    = bd.InProgress
	pausedBeads        = bd.PausedList
	showBead           = bd.Show
	openBeads          = bd.OpenList
	checkpointLandHold = loop.CheckpointLandHold
	surgeryHold        = loop.SurgeryHold // an open Surgery holds its Checkpoint's Critiques from run-now
	readLoop           = func() (looplock.Status, error) {
		common, err := git.CommonDir()
		if err != nil {
			return looplock.Status{}, err
		}
		path, err := looplock.Resolve(common)
		if err != nil {
			return looplock.Status{}, err
		}
		return looplock.Read(path)
	}
	// loopVerb runs `<self> loop <args>`; the TUI never owns the loop, so a
	// TUI-started loop outlives it (ADR 0051 §2, ADR 0053 §2).
	loopVerb = func(args ...string) (string, error) {
		self, err := os.Executable()
		if err != nil {
			return "", err
		}
		out, err := execCommand(self, append([]string{"loop"}, args...)...).CombinedOutput()
		if err != nil {
			if s := strings.TrimSpace(string(out)); s != "" {
				return "", errors.New(s) // the CLI's own words, verbatim
			}
		}
		return string(out), err
	}
)

// loadedMsg is one refresh's snapshot: the beads, effort liveness, tmux sessions,
// loop state and effort refs. Closed lineage and the slow-tier reads are carried
// across fast ticks (ADR 0197); the rest is re-derived each load.
type loadedMsg struct {
	beads       []bd.Bead       // the open set plus the closed lineage, fresh or cached (ADR 0197)
	jiraSet     bool            // the slow tier's read of the [jira] site
	closed      []bd.Bead       // the closed lineage this load used: the next load's cache
	demand      map[string]bool // the ids in closed that only a claim brought (ADR 0232)
	gone        []string        // on-demand ids the by-id read did not return: bd deleted them
	openIDs     map[string]bool
	saves       int  // Model.recentSaves when the read began
	req         int  // the model's slowLoad request count when this load began
	slowTier    bool // the slow tier ran: efforts, kinds, pool, auto-exit and commits are fresh
	live        map[string]bool
	sessions    map[string]session.Info
	recents     []string // Recents as read off the event loop
	loop        looplock.Status
	efforts     map[string]bool
	cpTips      map[string]string // checkpoint/<id> tips, read with efforts
	pool        loop.Setting
	autoExit    loop.AutoExit
	initiatives cfglayer.Initiatives
	approvals   cfglayer.Approvals
	landName    string // land_strategy, read with the slow tier
	landErr     error
	kinds       kindProfiles
	commits     []git.MainlineCommit // History's mainline read, newest first
	histErr     error                // that read failed; the rest of the load stands
	started     time.Time            // when this load began; it clears only launches that landed before it
}

type (
	errMsg  struct{ err error }
	tickMsg struct{}
)

// runDoneMsg is a run-now host's exit, already worded (runStatus).
type runDoneMsg struct{ status string }

// launchDoneMsg is a session start or resume's outcome (ADR 0107).
type launchDoneMsg struct {
	b       bd.Bead
	context bd.Bead // b's container, which carries the context: label a leaf lacks; zero when unresolved
	resume  bool
	refused bool // an unrunnable Agent Profile refused it before anything ran (ADR 0123); err is the flash
	reload  bool // refused on a stale snapshot: the view reloads
	bg      bool // no switch on success
	branch  string
	err     error
	afk     string // the live AFK effort holding the branch, when git refused it busy
	afkErr  error
}

// owner is the bead whose context: label picks the repo b's branch lives in.
func (m launchDoneMsg) owner() bd.Bead {
	if m.context.ID != "" {
		return m.context
	}
	return m.b
}

// writeDoneMsg is a background bd write's outcome (ADR 0098 §2): Update drops key
// from pending, then apply folds the result in.
type writeDoneMsg struct {
	key   string
	apply func(m Model) (tea.Model, tea.Cmd)
}

// digestNeedles prefilter the full-log scan: every digest event's raw line holds one
// (slog JSON writes "msg":"…" with no spaces). A prefix needle ends before its tail.
var digestNeedles = []string{`"msg":"session.ended"`, `"msg":"session.failed"`, `"msg":"hitl.exited"`, `"msg":"hitl.failed"`, `"msg":"hitl.continued"`, `"msg":"usage.read"`,
	`"msg":"land.`, `"msg":"gate.`, `"msg":"effort.failed"`, `"msg":"effort.paused"`}

// inDigest reports whether ev is one the full-log readers (Stats, landEvents, the
// finished-flash) need, so the shell holds only these in full (ADR 0231 §3).
func inDigest(ev eventlog.Event) bool {
	switch ev.Msg {
	case "session.ended", "session.failed", "hitl.exited", "hitl.failed", "hitl.continued", "usage.read", "effort.failed", "effort.paused":
		return true
	}
	return strings.HasPrefix(ev.Msg, "land.") || strings.HasPrefix(ev.Msg, "gate.")
}

// landEvents indexes the latest land.*, gate.*, effort.failed and effort.paused event
// msg per bead id, so rollup looks a Landing up in O(1). The log is oldest first.
func landEvents(evs []eventlog.Event) map[string]string {
	idx := map[string]string{}
	for _, ev := range evs {
		if ev.Bead != "" && (strings.HasPrefix(ev.Msg, "land.") || strings.HasPrefix(ev.Msg, "gate.") || ev.Msg == "effort.failed" || ev.Msg == "effort.paused") {
			idx[ev.Bead] = ev.Msg
		}
	}
	return idx
}

// logsMsg is one event-log read, carrying events only when the log changed.
type logsMsg struct {
	path       string
	sig        eventlog.Sig
	changed    bool
	off        int64           // the live-file offset the sig was read up to
	extend     bool            // events are appended to the held logs; else they replace them
	rotated    bool            // an extend that followed a rotation: the digest is rebuilt, not extended
	cur        eventlog.Cursor // a replace's cursor for older reads, taken at curSig
	curSig     eventlog.Sig
	base       eventlog.State // the state the read started from: an extend applies only on top of it
	events     []eventlog.Event
	digest     []eventlog.Event // the digest events of events
	scanDigest bool             // the whole-log digest is still to be read (digestCmd)
	skipped    int
	err        error
	started    time.Time // when this load began; it clears only launches that landed before it
}

// load is a fast-tier refresh: it re-reads closed lineage only on a drop-out.
func (m Model) load() tea.Cmd { return m.loadTier(false) }

// loadSlow is a refresh that also re-reads closed lineage (startup, Graph entry, F5).
func (m Model) loadSlow() tea.Cmd { return m.loadTier(true) }

// slowLoad starts a slow load unless one is in flight, in which case the next tick
// runs it: a repeated F5, Graph entry or setting save never stacks loads (FR21).
func (m *Model) slowLoad() tea.Cmd {
	m.reqSeq++ // a load already in flight predates this request and does not satisfy it
	if m.loading {
		m.slowWant = true
		return nil
	}
	m.loading = true
	return m.loadSlow()
}

// slowDue reports whether the 30 s cadence has elapsed since the last slow load.
func (m Model) slowDue() bool { return time.Since(m.slowAt) >= slowInterval }

func (m Model) loadTier(slow bool) tea.Cmd {
	prev, cached, demand, req, saves := m.openIDs, m.closed, m.demand, m.reqSeq, m.recentSaves
	claimsAll, held := m.claimsAllClosed(), m.demandClaims()
	var watch []string // open matches of the active query: if one closes, the drop-out read fetches it for keepClosers
	if w := m.searched.words; w != "" && m.tab == tabGraph {
		for _, b := range m.beads {
			if b.Unclosed() && matchWords(w, b.Title+" "+b.ID) {
				watch = append(watch, b.ID)
			}
		}
	}
	return func() tea.Msg {
		started := time.Now()
		loop.StampAutoApprove() // every refresh: isHITL reads only this snapshot (Spec D4)
		open, err := listOpen()
		if err != nil {
			return errMsg{err}
		}
		openIDs := make(map[string]bool, len(open))
		for _, b := range open {
			openIDs[b.ID] = true
		}
		listed := slow         // listAll runs on the slow tier or a drop-out; only the slow tier reads the rest
		for id := range prev { // a drop-out (closed or deleted) reads closed lineage now
			listed = listed || !openIDs[id]
		}
		recordClient()                // refresh F6's client → TUI map; a failure only costs F6 its way back
		sessions, _ := readSessions() // a failed read is an empty table
		recents := loadRecents(recentsPath())
		claimed := claimedIDs(recents, sessions)
		maps.Copy(claimed, held)
		src := cached
		var extra []string // claimed beads neither open nor default, read by id
		if listed {
			var need []string // ids to fetch: the default set plus claimed beads neither open nor default
			def := map[string]bool{}
			if claimsAll { // s any / s closed with no words: the one view that needs every closed bead
				all, err := listAll()
				if err != nil {
					return errMsg{err}
				}
				src = all
				for _, b := range all {
					def[b.ID] = true
				}
			} else {
				ids, err := defaultIDs()
				if err != nil {
					return errMsg{err}
				}
				need = slices.Clone(ids)
				for _, id := range ids {
					def[id] = true
				}
			}
			for id := range claimed {
				if !openIDs[id] && !def[id] {
					extra = append(extra, id)
				}
			}
			slices.Sort(extra)
			need = append(need, extra...)
			for _, id := range watch {
				if !openIDs[id] && !def[id] && !claimed[id] {
					need = append(need, id)
				}
			}
			var got []bd.Bead
			if len(need) > 0 {
				var err error
				if got, err = listByID(need); err != nil {
					return errMsg{err}
				}
			}
			if claimsAll { // wholesale: a delete drops out
				src = append(slices.Clone(src), got...)
			} else {
				src = got
			}
			demand = map[string]bool{}
			for _, id := range extra {
				demand[id] = true
			}
		}
		var closed []bd.Bead
		var gone []string // on-demand ids this read asked for by id and did not get back: deleted
		kept := map[string]bool{}
		for _, b := range src { // a fresh open bead wins over any closed copy, cached or fresh (a reopen is no drop-out)
			if !openIDs[b.ID] && (!demand[b.ID] || claimed[b.ID] || claimsAll) { // an on-demand bead no claim names is evicted
				closed = append(closed, b)
				kept[b.ID] = true
			}
		}
		for _, id := range extra {
			if !kept[id] {
				gone = append(gone, id)
			}
		}
		cloned := false
		for id := range demand {
			if !kept[id] {
				if !cloned {
					demand, cloned = maps.Clone(demand), true
				}
				delete(demand, id)
			}
		}
		beads := append(slices.Clone(open), closed...)
		var ids []string
		for _, b := range beads {
			if loop.IsActiveAFKEffort(b) {
				ids = append(ids, b.ID)
			}
		}
		done, left := barStates(beads, sessions)
		setBars(done, left, sessions) // a failure only costs this tick's done flip and status-left refresh
		lp, err := readLoop()
		if err != nil {
			lp = looplock.Status{State: looplock.Off} // ponytail: an unreadable lock shows off; o's CLI error says why
		}
		msg := loadedMsg{beads: beads, closed: closed, demand: demand, gone: gone, openIDs: openIDs, slowTier: slow, req: req, live: probeLiveness(ids), sessions: sessions, recents: recents, saves: saves, loop: lp, started: started}
		if slow { // the slow tier (ADR 0197): the rest of the reads ride with the closed lineage
			msg.efforts, msg.cpTips, _ = readRefs() // a failed read is empty: no tip is known, so nothing is approved
			msg.kinds = readKindProfiles()
			msg.kinds.reasons = profileReasons(m.ctx, msg.kinds)
			msg.commits, msg.histErr = readMainline()
			msg.landName, msg.landErr = readLandStrategy()
			msg.pool, msg.autoExit, msg.initiatives, msg.approvals, msg.jiraSet = readPool(), readAutoExit(), readInitiatives(), loop.ReadAutoApprove(), jiraSiteSet()
		}
		return msg
	}
}

// claimedIDs is the ids an on-demand closed bead stays loaded for without a Graph
// query: Recents and every bead with a tmux session (ADR 0232 §1).
func claimedIDs(recents []string, sessions map[string]session.Info) map[string]bool {
	c := map[string]bool{}
	for _, id := range recents {
		c[id] = true
	}
	for id := range sessions {
		c[id] = true
	}
	return c
}

// claimsAllClosed reports whether the Graph query claims every closed bead: s any or
// s closed with no words and no scope, while Graph is the tab (ADR 0232; a bare scope is
// no-words, status-any and must not, Spec FR26).
func (m Model) claimsAllClosed() bool {
	if m.tab != tabGraph {
		return false
	}
	q := m.tabAt(tabGraph).(graphTab).q
	return q.scope == "" && q.words == "" && (q.status == statusAny || q.status == statusClosed)
}

// claimLoad is the load a Graph query change owes: the query starting to claim every
// closed bead (s any / s closed, no words) reads them now, not on the next slow tick,
// under the searching… marker (ADR 0232 §6). wasAll is claimsAllClosed before the change.
func (m *Model) claimLoad(wasAll bool) tea.Cmd {
	if wasAll || !m.claimsAllClosed() {
		return nil
	}
	g := m.tabAt(tabGraph).(graphTab)
	g.searching = true
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabGraph] = g
	return m.slowLoad()
}

// revealIn is G on a loaded bead: show it in Graph.
func (m Model) revealIn(id string) (Model, tea.Cmd) {
	old := m.tabAt(tabGraph).(graphTab).rebuild() // Graph may be stale off-tab
	old.visit = false                             // G is the human's own reveal: no longer a Recents visit
	g, ok, note := old.reveal(id)
	if !ok {
		m.setStatus(id + ": not in the graph")
		return m, nil
	}
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabGraph], m.tab = g, tabGraph
	m.setStatus(note)
	if !maps.Equal(old.flip, g.flip) {
		m = m.saveGraphFolds()
	}
	return m, m.graphEntered()
	// a scoped reveal is not saved as the Graph query (ADR 0176); a visible one keeps it
}

// logWindow is how many of the newest events Logs holds on open, and how many more each
// older page adds (ADR 0231 §1).
const logWindow = 1000

// loadLogs re-reads the event log only when its stat changed since the last load: the
// first read and a truncation take the newest events again, a tail extends them, and a
// rotation reads what landed since the head across it (ADR 0231 §1).
func (m Model) loadLogs() tea.Cmd {
	prev, l, have := eventlog.State{Sig: m.logSig, Off: m.logOff}, m.evlog, max(len(m.logs), logWindow)
	return func() tea.Msg {
		var msg logsMsg
		path, err := l.Path()
		msg.path = path
		if path == "" {
			msg.changed, msg.err = true, err
			return msg
		}
		msg.sig = eventlog.Stat(path)
		if msg.changed = msg.sig != prev.Sig; msg.changed {
			var next eventlog.State
			var full bool
			rotated := prev.Sig != (eventlog.Sig{}) && msg.sig[0] != prev.Sig[0]
			switch {
			case prev.Sig == (eventlog.Sig{}):
				full = true
			case rotated:
				var ok bool
				msg.events, msg.skipped, next, ok, msg.err = eventlog.ReadRotated(path, prev)
				full = !ok
			default:
				msg.events, msg.skipped, next, full, msg.err = tailLog(path, prev)
			}
			if full && msg.err == nil {
				msg.events, msg.skipped, next, msg.cur, msg.err = newestLog(path, have)
				msg.curSig = next.Sig
			}
			msg.extend, msg.rotated, msg.base, msg.sig, msg.off = !full, rotated, prev, next.Sig, next.Off
			if msg.err == nil {
				// A full or rotated read leaves the whole-log digest to digestCmd, so the
				// first frame does not wait on the scan; msg.digest is the window's own.
				msg.digest = slices.DeleteFunc(slices.Clone(msg.events), func(ev eventlog.Event) bool { return !inDigest(ev) })
				msg.scanDigest = full || rotated
			}
			if msg.err != nil {
				msg.sig, msg.off = eventlog.Sig{}, 0 // a chmod moves neither size nor mtime: keep retrying
			}
		}
		return msg
	}
}

// digestMsg is the whole-log digest up to off, read after a full or rotated window so
// the window's first frame does not wait on the scan (ADR 0231 §3). A newer window's
// read bumps gen and drops it.
type digestMsg struct {
	gen    int
	digest []eventlog.Event
	beads  map[string]bool // every bead id the whole log names, for Logs' kind picker
	err    error
}

// digestCmd runs the one prefiltered scan off the event loop, up to what the window read covered.
func digestCmd(path string, off int64, gen int) tea.Cmd {
	return func() tea.Msg {
		d, err := eventlog.ReadWhere(path, off, digestNeedles, inDigest)
		if err != nil {
			return digestMsg{gen: gen, err: err}
		}
		ids, err := eventlog.Beads(path, off)
		return digestMsg{gen, d, ids, err}
	}
}

// olderLogsMsg is one older page of events for Logs' window, read from cur at sig.
type olderLogsMsg struct {
	from    eventlog.Cursor // the cursor the page was read from: a page for a replaced window is dropped
	evs     []eventlog.Event
	skipped int
	cur     eventlog.Cursor
	sig     eventlog.Sig
	err     error
}

// loadOlder reads the next page of events older than the window.
func (m Model) loadOlder() tea.Cmd {
	path, from, sig := m.logPath, m.logCur, m.logCurSig
	return func() tea.Msg {
		evs, sk, cur, now, err := olderLog(path, from, sig, logWindow)
		return olderLogsMsg{from, evs, sk, cur, now, err}
	}
}

// foldOlder prepends an older page. The selection is pinned by key, so it stays; one on
// the oldest row moves on to the newest of the page, the row scrolling carried it to.
func (m Model) foldOlder(msg olderLogsMsg) Model {
	m.logPaging = false
	if msg.err != nil {
		m.setStatus("older events: " + msg.err.Error())
		return m
	}
	if msg.from != m.logCur || len(m.logs) == 0 { // the window was replaced while this read ran
		return m
	}
	l := m.tabAt(tabLogs).(logsTab)
	if rows := l.rows(); !l.active() && len(rows) > 0 && len(msg.evs) > 0 { // a filter shows scan matches, not the window: nothing to move
		if _, ev := l.current(); ev != nil && ev.Key() == l.events[rows[len(rows)-1]].Key() {
			for i := len(msg.evs) - 1; i >= 0; i-- { // the newest the page shows: a hidden DEBUG one would fall back to the top
				if ev := msg.evs[i]; ev.Level != "DEBUG" || l.debug {
					l.sel = ev.Key()
					break
				}
			}
		}
	}
	m.logs = slices.Concat(msg.evs, m.logs)
	m.logSkipped += msg.skipped
	m.logCur, m.logCurSig = msg.cur, msg.sig
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabLogs] = l
	return m
}

// barStates is each alive HITL session's bead id → closed, the done flip SetBars applies,
// and → status-left: the bead's plain SBRN (ANSI stripped, no dot slot), or a
// discussion's <title> · Robin discussion (ADR 0194 §2).
func barStates(beads []bd.Bead, sessions map[string]session.Info) (closed map[string]bool, left map[string]string) {
	closed, left = map[string]bool{}, map[string]string{}
	g := graphData{beads: beads}
	for i := range beads {
		b := &beads[i]
		if sessions[b.ID].State != session.Alive {
			continue
		}
		closed[b.ID] = b.Status == "closed"
		if b.Kind == contract.KindDiscussion {
			left[b.ID] = b.Title + " · Robin discussion"
		} else if b.Kind == contract.KindCheckup {
			left[b.ID] = b.Title + " · Chopper checkup"
		} else {
			left[b.ID] = ansi.Strip(g.sbrn(b, b.Title, false, 0))
		}
	}
	return closed, left
}

// launchHoldMax ends a launch's hold in Live even if no snapshot ever shows the bead alive.
const launchHoldMax = 30 * time.Second

func tick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

// fold applies a non-key message to the shell; ok is false for one it does not own.
func (m Model) fold(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case loadedMsg:
		if msg.slowTier {
			m.snapGen++
		}
		if msg.saves == m.recentSaves { // a read that predates a save would revert it
			m.recents = msg.recents
		}
		m.launched = maps.Clone(m.launched)
		// A snapshot loaded after the launch succeeded and showing the bead live clears
		// its hold; so does any snapshot launchHoldMax or more after the success.
		for id, at := range m.launched {
			alive := msg.sessions[id].State == session.Alive || msg.live[id]
			if (msg.started.After(at) && alive) || msg.started.Sub(at) >= launchHoldMax {
				delete(m.launched, id)
			}
		}
		seen := cmp.Or(msg.started, time.Now())
		waits := map[string]time.Time{}
		for id, s := range msg.sessions {
			if humanWait(s, false) != "" {
				waits[id] = cmp.Or(m.waitSince[id], seen)
			}
		}
		m.waitSince = waits
		m.closed, m.demand, m.openIDs = msg.closed, msg.demand, msg.openIDs
		if len(msg.gone) > 0 { // the by-id read is the source of truth for on-demand beads too
			m.extra = maps.Clone(m.extra)
			for _, id := range msg.gone {
				delete(m.extra, id)
			}
		}
		fresh := msg.req == m.reqSeq // started after the last slowLoad request: it read what that request owes
		if msg.slowTier {
			m.slowAt = msg.started
			m.slowWant = m.slowWant && !fresh
		}
		m.live, m.sessions, m.loop = msg.live, msg.sessions, msg.loop
		if msg.slowTier { // a fast load keeps the slow tier's last values
			m.efforts, m.cpTips, m.pool, m.exit, m.kinds, m.initiatives, m.approvals, m.jiraSet = msg.efforts, msg.cpTips, msg.pool, msg.autoExit, msg.kinds, msg.initiatives, msg.approvals, msg.jiraSet
			m.landName, m.landErr = msg.landName, msg.landErr
			m.histCommits, m.histErr = msg.commits, msg.histErr
		}
		m.flashCloseFailed()
		var inProgress []bd.Bead
		for _, b := range msg.beads {
			if b.Status == "in_progress" {
				inProgress = append(inProgress, b)
			}
		}
		m.inflight = loop.CountInflight(inProgress)
		oldInits := m.tabAt(tabInits).(initsTab).rows()
		m = m.keepClosers(msg.beads)
		m.beads = nil // event beads are history, not work (ADR 0057 §3)
		for _, b := range msg.beads {
			if !b.Event {
				m.beads = append(m.beads, b)
			}
		}
		m = m.withExtra()
		m.idx = newSnapIndex(m.beads)
		m.inits = m.initStatsOf() // once per refresh: focusScope is too slow per frame
		m.initWaits = m.initWaitsOf()
		// A read showing the loop left the state the verb acted on confirms it; a
		// stale read still shows the old one, so wait.
		if m.loop.State != m.loopFrom.State || m.loop.Pid != m.loopFrom.Pid || m.loop.RestartTo != m.loopFrom.RestartTo {
			m.loopBusy, m.loopStarting = false, false
			if m.status == startingLoop || m.status == stoppingLoop {
				m.setStatus("")
			}
		}
		m.setTop = m.settingsTop(m.setTop)
		m.tabs = slices.Clone(m.tabs)
		if g := m.tabAt(tabGraph).(graphTab); msg.slowTier && fresh && g.searching && m.searched.words == "" {
			g.searching = false // the claim load (claimLoad) landed; a word search clears its own
			m.tabs[tabGraph] = g
		}
		if m.tab == tabGraph { // off Graph the rebuild waits for entry (enter), so the tick's fold stays flat
			g := m.tabAt(tabGraph).(graphTab).rebuild() // regroup over the new beads, keeping the selection by id
			if g.entry && fresh {                       // the entry load landed: a cursor bead it brought back has returned, else it is gone
				g.searching, g.entry, g.want = false, false, ""
			}
			m.tabs[tabGraph] = g
		}
		m.tabs[tabInits] = m.tabAt(tabInits).(initsTab).settle(oldInits) // keep the selection by id
		m.tabs[tabHistory] = m.tabAt(tabHistory).(histTab).fix()         // keep the selection by sha
		m.loadErr, m.loading, m.loaded = nil, false, true
	case statusMsg:
		m.setStatus(string(msg))
	case recentsMsg:
		if msg.err != nil {
			m.setStatus("recents won't survive a restart: " + msg.err.Error())
		} else {
			m.recents, m.recentSaves = msg.ids, m.recentSaves+1
		}
	case errMsg:
		m.loadErr, m.loading = msg.err, false
		if g := m.tabAt(tabGraph).(graphTab); g.searching && m.searched.words == "" { // the claim load failed: no marker without a read
			g.searching = false
			m.tabs = slices.Clone(m.tabs)
			m.tabs[tabGraph] = g
		}
		m.loopBusy = false // no read will confirm while bd is down
	case tickMsg:
		if m.loading {
			return m, tea.Batch(m.loadLogs(), tick()), true
		}
		m.loading = true
		return m, tea.Batch(m.loadTier(m.slowDue() || m.slowWant), m.loadLogs(), tick()), true
	case logsMsg:
		// an extend read from a state the model has moved past (an overlapping load) is stale: drop it, the next tick re-reads
		if msg.changed && msg.extend && msg.err == nil && msg.base != (eventlog.State{Sig: m.logSig, Off: m.logOff}) {
			msg.changed = false
		}
		if msg.changed {
			m.tabs = slices.Clone(m.tabs)
			m.tabs[tabLogs] = m.tabAt(tabLogs).(logsTab).pin() // FR49: anchor what the reader sees before the rows shift
			if msg.extend && msg.err == nil {
				m.logs, m.logSkipped = append(slices.Clip(m.logs), msg.events...), m.logSkipped+msg.skipped
				if msg.rotated {
					m.digest, m.digestWin = msg.digest, len(msg.digest)
				} else {
					m.digest = append(slices.Clip(m.digest), msg.digest...)
				}
			} else {
				m.logCur, m.logCurSig = msg.cur, msg.curSig
				m.logs, m.logSkipped, m.digest, m.digestWin = msg.events, msg.skipped, msg.digest, len(msg.digest)
			}
			m.logSig, m.logOff, m.logErr = msg.sig, msg.off, msg.err
			if l := m.tabAt(tabLogs).(logsTab); l.active() { // the filtered rows follow the log: a tail adds to them, a full read re-scans
				if msg.extend && msg.err == nil {
					m.tabs[tabLogs] = l.head(msg.events)
				} else {
					l.scan.key = ""
					m.tabs[tabLogs] = l
				}
			}
			m.landEv = landEvents(m.digest)
			m.tabs[tabLogs] = m.tabAt(tabLogs).(logsTab).pin() // and the newest on first load
			m.tabs[tabStats] = m.tabAt(tabStats).(statsTab).recompute()
		}
		m.logPath = msg.path
		// an unread log must not seed exitsSeen empty, nor a window before its whole-log digest lands
		if msg.err == nil && msg.path != "" && !m.digestPending && !(msg.changed && msg.scanDigest) {
			m.flashFinished()
		}
		if msg.changed && msg.scanDigest && msg.err == nil {
			m.digestGen++
			m.digestPending = true
			return m, digestCmd(msg.path, msg.off, m.digestGen), true
		}
	case digestMsg:
		if msg.gen == m.digestGen {
			m.digestPending = false
			if msg.err != nil { // retry: a zero Sig makes the next tick read the log afresh
				m.setStatus("log digest: " + msg.err.Error())
				m.logSig, m.logOff = eventlog.Sig{}, 0
				break
			}
			m.digest = slices.Concat(msg.digest, m.digest[m.digestWin:]) // the tail's events since stay
			m.digestWin = len(msg.digest)
			m.logBeads = msg.beads
			m.landEv = landEvents(m.digest)
			m.tabs = slices.Clone(m.tabs)
			m.tabs[tabStats] = m.tabAt(tabStats).(statsTab).recompute()
			m.flashFinished()
		}
	case usageTickMsg:
		return m, m.pollUsage(), true
	case usageMsg:
		m.foldUsage(msg)
	case revealMsg:
		if beadIndex(m.beads, msg.id) < 0 {
			next, cmd := m.fetchReveal(msg.id, true, Model.revealIn)
			return next, cmd, true
		}
		next, cmd := m.revealIn(msg.id)
		return next, cmd, true
	case searchMsg:
		return m.foldSearch(msg), nil, true
	case commentsMsg:
		return m.foldComments(msg), nil, true
	case rollupMsg:
		return m.foldRollup(msg), nil, true
	case demandMsg:
		next, cmd := m.foldDemand(msg)
		return next, cmd, true
	case scopeLogsMsg:
		l := m.tabAt(tabLogs).(logsTab)
		l.scope, l.kids, l.sel, l.off = msg.id, msg.kids, eventlog.Key{}, 0
		m.tabs = slices.Clone(m.tabs)
		m.tabs[tabLogs], m.tab = l, tabLogs
	case rollbackMsg:
		if m.refusePending(msg.id) {
			return m, nil, true
		}
		return m.rollbackForm(msg), nil, true
	case pickLogKindMsg:
		next, cmd := m.pickLogKind()
		return next, cmd, true
	case logKindsMsg:
		next, cmd := m.foldKinds(msg)
		return next, cmd, true
	case olderLogsMsg:
		return m.foldOlder(msg), nil, true
	case olderWantedMsg:
		if m.logPaging || m.logCur.Done {
			return m, nil, true
		}
		m.logPaging = true
		return m, m.loadOlder(), true
	case pickGraphMsg:
		return m.pickGraph(msg.facet), nil, true
	case runDoneMsg:
		m.setStatus(msg.status)
		return m, m.load(), true
	case loopDoneMsg:
		next, cmd := m.loopDone(msg)
		return next, cmd, true
	case checkupFiledMsg:
		next, cmd := m.checkupFiled(msg)
		return next, cmd, true
	case discussionCreatedMsg:
		next, cmd := m.discussionCreated(msg)
		return next, cmd, true
	case launchDoneMsg:
		next, cmd := m.launchDone(msg)
		return next, cmd, true
	case editorDoneMsg:
		return m.editorDone(msg), nil, true
	case explorerDoneMsg:
		if msg.err != nil {
			m.setStatus("explorer: " + msg.err.Error())
		}
		return m, nil, true
	case browserDoneMsg:
		next, cmd := m.browserDone(msg)
		return next, cmd, true
	case writeDoneMsg:
		m.pending = maps.Clone(m.pending)
		delete(m.pending, msg.key)
		next, cmd := msg.apply(m)
		return next, cmd, true
	default:
		return m, nil, false
	}
	return m, nil, true
}

// setStatus shows a background result, or holds it while a modal is up: the
// dialog is never rewritten under the reader, and the result shows when it closes.
func (m *Model) setStatus(s string) {
	if len(m.modals) > 0 {
		m.deferred = s
	} else {
		m.status = s
	}
}

// A pending key is a bead id, or slot(name) for a write on no bead (a create form,
// a Settings save), so the two can never collide.
const slotPrefix = "slot:"

func slot(name string) string { return slotPrefix + name }

// keyName is key as the human reads it.
func keyName(key string) string { return strings.TrimPrefix(key, slotPrefix) }

// startWrite runs a key's bd write in a tea.Cmd under the per-key guard (ADR 0098
// §2–3): a key already pending is refused, never queued; otherwise it is marked
// pending under doing's verb and its "<doing> <key>…" status written.
func (m Model) startWrite(key, doing string, run func() writeDoneMsg) (tea.Model, tea.Cmd) {
	if m.refusePending(key) {
		return m, nil
	}
	m.pending = maps.Clone(m.pending)
	if m.pending == nil {
		m.pending = map[string]string{}
	}
	m.pending[key], _, _ = strings.Cut(doing, " ")
	m.setStatus(doing + " " + keyName(key) + "…")
	return m, func() tea.Msg { return run() }
}

// startRead is startWrite shaped for a read: same per-key guard (a second press is
// refused, never queued), but the refusal names the check in flight, not a write.
func (m Model) startRead(key, doing string, run func() writeDoneMsg) (tea.Model, tea.Cmd) {
	if verb, ok := m.pending[key]; ok && verb == strings.Fields(doing)[0] { // a write pending on the bead gets refusePending's wording
		m.setStatus(keyName(key) + ": " + doing + " in flight, wait")
		return m, nil
	}
	return m.startWrite(key, doing, run)
}

// startAct is startWrite for an act the human fired on bead id: once the write
// starts (not when the per-key guard refuses it) the bead enters Recents.
func (m Model) startAct(id, doing string, run func() writeDoneMsg) (tea.Model, tea.Cmd) {
	next, cmd := m.startWrite(id, doing, run)
	if cmd == nil {
		return next, cmd
	}
	return next, tea.Batch(cmd, m.recordRecent(id))
}

// doneOrErr is a done closure's common tail (ADR 0098 §2): on err the status reads
// "<prefix>: <err>" and nothing reloads; otherwise ok, then a reload.
func (m Model) doneOrErr(prefix, ok string, err error) (tea.Model, tea.Cmd) {
	if err != nil {
		m.setStatus(prefix + ": " + err.Error())
		return m, nil
	}
	m.setStatus(ok)
	return m, m.load()
}

// refusePending reports whether key has a write in flight, writing the refusal.
func (m *Model) refusePending(key string) bool {
	_, ok := m.pending[key]
	if ok {
		m.setStatus(keyName(key) + ": write in flight, wait")
	}
	return ok
}

// startLaunch marks b's launch in flight and runs it in a tea.Cmd; an attaching
// launch takes the one attach slot, displacing any other (ADR 0107 §5–6).
func (m Model) startLaunch(b bd.Bead, bg bool, run func() launchDoneMsg) (tea.Model, tea.Cmd) {
	if lm, ok := m.tabAt(m.tab).(launchMover); ok { // read before the mark takes the row out: the cursor stays in Actionable, not on the bead into Live
		m.tabs = slices.Clone(m.tabs)
		m.tabs[m.tab] = lm.afterLaunch(b.ID)
	}
	m.launching = maps.Clone(m.launching)
	if m.launching == nil {
		m.launching = map[string]bool{}
	}
	m.launching[b.ID] = true
	s := "launching " + cmp.Or(b.Title, b.ID) + "…"
	if !bg {
		m.attachTo, s = b.ID, s+" (attaches when ready)"
	}
	m.setStatus(s)
	return m, tea.Batch(func() tea.Msg {
		msg := run()
		msg.bg = bg
		return msg
	}, m.recordRecent(b.ID))
}

// flashFinished flashes `✓ <id> finished` once per clean HITL exit the tick reads
// (ADR 0067 §2). The first read only seeds what is seen, so a fresh TUI never
// flashes history; while a modal is open the flash waits for a later read.
func (m *Model) flashFinished() {
	seed := m.exitsSeen == nil
	if seed {
		m.exitsSeen = map[eventlog.Key]bool{}
	} else if len(m.modals) > 0 {
		return
	}
	var done []string
	for _, ev := range m.digest {
		if ev.Msg != "hitl.exited" || ev.Bead == "" || ev.Raw["err"] != nil || ev.Raw["dropped"] == true || m.exitsSeen[ev.Key()] {
			continue
		}
		m.exitsSeen[ev.Key()] = true
		if !seed {
			done = append(done, "✓ "+m.finishedName(ev.Bead)+" finished")
		}
	}
	if len(done) > 0 {
		m.setStatus(strings.Join(done, " · "))
	}
}

// flashCloseFailed flashes `✕ <id> close failed — resume or drop` once per session
// the last read showed Closing and this one shows exited (ADR 0108 §4). The first
// read only seeds; a modal defers it, as flashFinished does.
func (m *Model) flashCloseFailed() {
	if m.closingSeen != nil && len(m.modals) > 0 {
		return
	}
	var failed []string
	for id := range m.closingSeen {
		if m.sessions[id].State == session.Exited {
			failed = append(failed, "✕ "+id+" close failed — resume or drop")
		}
	}
	m.closingSeen = map[string]bool{}
	for id, s := range m.sessions {
		if s.State == session.Alive && s.Closing {
			m.closingSeen[id] = true
		}
	}
	if len(failed) > 0 {
		slices.Sort(failed)
		m.setStatus(strings.Join(failed, " · "))
	}
}

// waiting is an alive, not-done session whose agent waits on the human (ADR 0099).
func (m Model) waiting(b bd.Bead) bool {
	s := m.sessions[b.ID]
	return humanWait(s, b.Status == "closed") != ""
}

// countSt renders "n label" in st, dim when n is 0 so the header doesn't shift.
func countSt(st lipgloss.Style, n int, label string) string {
	if n == 0 {
		st = dimSt
	}
	return st.Render(fmt.Sprintf("%d %s", n, label))
}

// crewCounts are the live HITL sessions, live AFK efforts, paused beads and waiting
// sessions over the beads in reports true for. retrying names the run-now retries in
// flight: not a pause the human must clear.
func (m Model) crewCounts(in func(id string) bool, retrying map[string]bool) (hitl, afk, paused, waiting int) {
	for id, s := range m.sessions {
		if s.State == session.Alive && !s.Closing && in(id) {
			hitl++
		}
	}
	for id, up := range m.live {
		if up && in(id) {
			afk++
		}
	}
	for _, b := range m.beads {
		if !in(b.ID) {
			continue
		}
		if b.Unclosed() && bd.Paused(b) && !retrying[b.ID] {
			paused++
		}
		if m.waiting(b) {
			waiting++
		}
	}
	return
}

// retrying is the beads whose retry is in flight: paused by paused:failing or
// paused:foreign-hold with a live effort (ADR 0331). It reads the effort's
// liveness, not the TUI's run-now set, so a detached host's retry counts too. A
// live paused:manual bead is an abort winding down and stays paused.
func (m Model) retrying() map[string]bool {
	r := map[string]bool{}
	for _, b := range m.beads {
		if m.live[b.ID] && (bd.PausedBy(b, bd.PausedFailing) || bd.PausedBy(b, bd.PausedForeignHold)) {
			r[b.ID] = true
		}
	}
	return r
}

// headerRight is the header's right side (FR6): N HITL · M AFK · K waiting · N‖ ·
// the loop segment. Too wide, it drops the HITL, AFK and waiting counts.
// While an Initiative is focused the counts are the back bar's, so only the loop
// segment and the usage warning stay.
func (m Model) headerRight(w int) string {
	hitl, afk, paused, waiting := m.crewCounts(func(string) bool { return true }, m.retrying())
	render := func(counts bool) string {
		if m.focus.id != "" {
			return m.loopSegment() + m.usageWarning() + " "
		}
		var segs []string
		if counts {
			segs = append(segs, countSt(aliveSt, hitl, "HITL"), countSt(afkSt, afk, "AFK"))
			if waiting > 0 {
				segs = append(segs, waitSt.Render(fmt.Sprintf("%d waiting", waiting)))
			}
		}
		if paused > 0 {
			segs = append(segs, waitSt.Render(fmt.Sprintf("%d‖", paused)))
		}
		return strings.Join(append(segs, m.loopSegment()), dimSt.Render(" · ")) + m.usageWarning() + " "
	}
	s := render(true)
	if lipgloss.Width(s) > w {
		s = render(false)
	}
	return ansi.Truncate(s, w, "")
}

// noticeView is a message in place of the frame (FR15): the head wrapped, the hint dimmed.
func (m Model) noticeView(head, hint string) string {
	wrap := lipgloss.NewStyle().Width(m.w)
	return fit(wrap.Render(head)+"\n"+wrap.Inherit(dimSt).Render(hint), m.w, m.h)
}

const minBlockerTitle = 4

// foldTitles appends titles to s, comma-separated and each cut to the w columns
// left before end, then end; once no readable title fits, it and the rest fold
// into +N.
func foldTitles(s string, titles []string, end string, w int) string {
	for i, t := range titles {
		sep, more := ", ", ""
		if i == 0 {
			sep = ""
		}
		if rest := len(titles) - i - 1; rest > 0 {
			more = fmt.Sprintf(" +%d", rest)
		}
		room := w - lipgloss.Width(s+sep+more+end)
		if room < minBlockerTitle {
			return strings.TrimSuffix(s, " ") + fmt.Sprintf(" +%d", len(titles)-i) + end
		}
		s += sep + ansi.Truncate(t, room, "…")
	}
	return strings.TrimSuffix(s, " ") + end
}

// pendingNames are the in-flight keys as the human reads them, sorted.
func pendingNames(pending map[string]string) []string {
	var ns []string
	for k := range pending {
		ns = append(ns, keyName(k))
	}
	slices.Sort(ns)
	return ns
}

// quitPrompt is the quit dialog's body: the writes still in flight, named by key:
// `N writes in flight: <key>…` (ADR 0098 §6). A run-now effort is a detached host
// and no part of it.
func quitPrompt(pending map[string]string, w int) string {
	if len(pending) == 0 {
		return ""
	}
	noun := "writes"
	if len(pending) == 1 {
		noun = "write"
	}
	return foldTitles(fmt.Sprintf("%d %s in flight: ", len(pending), noun), pendingNames(pending), " — quit?", w)
}
