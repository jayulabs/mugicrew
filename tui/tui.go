// Package tui is the human-action TUI (spec §9): a tabbed bubbletea program to see,
// interact with, and intervene on the whole bead graph. The Graph tab is the
// MVP-primary surface — whole-graph master/detail grouped by Chart, every kind
// and state incl. closed lineage and loop-run AFK work. The Inbox tab is the
// secondary open-HITL-only queue. Detail actions are launch (the owning
// crewmate's Launch command) and edit (set any bd field). A periodic refresh
// reflects the loop's writes (§9.5). The prototype (artifact
// 476BcWU8rZKUXorMCUwbk9) fixed the layout this implements.
package tui

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/dispatch"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/loop"
	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/registry"
	"jayulabs.org/mugicrew/route"
	"jayulabs.org/mugicrew/session"
	"jayulabs.org/mugicrew/stats"
)

// refreshInterval is the §9.5 periodic-refresh cadence: light with bd shell-out
// (process-per-call), so the Graph tab reflects loop writes without the human
// hitting `r`. ponytail: fixed; a config knob is the upgrade path if needed.
const refreshInterval = 3 * time.Second

// isHITL reports whether a kind is human-in-the-loop — the Inbox's filter
// invariant (spec §9.2): HITL kinds gate the queue and are launched via l; AFK
// kinds (research, drained Chart) never appear — the loop runs them, and they
// show in the Graph tab for observe/intervene only. The registry IS the AFK/HITL
// source (registry pkg doc: "Nothing is derived or stored elsewhere"), so this
// consults it rather than a parallel list that drifts — the missing KindApproval
// / KindChartConflict rows are why approvals mislabeled as AFK.
func isHITL(k contract.Kind) bool {
	e, err := registry.Lookup(k)
	return err == nil && e.Mode == registry.HITL
}

// unparentedSel is the selection key for the Unparented group header,
// which has no backing bead. Non-printable so it can't collide with a bead id.
const unparentedSel = "\x00unparented"

// group is one master-pane group: a Chart container (head) and its member defog
// beads, or the head-less Unparented group (spec §9.1).
type group struct {
	key     string   // head bead id, or "" for the unparented group (its collapse key)
	head    *bd.Bead // the Chart container; nil for the unparented group
	name    string
	members []bd.Bead
}

// buildGraph groups the whole graph by Chart (spec §9.1). Only kind-bearing
// beads (the mugicrew ontology, ADR 0002) are shown; bd-native planning beads
// carry no kind: label and are omitted. Containers (bd epics) become group
// heads; each non-container bead is placed under its parent container, or into
// the Unparented group when it has no container parent (open intake Signals,
// human-filed Rollbacks — ADR 0047, they skip intake; also any parentless leaf,
// e.g. a succession-spawned Spec — rare).
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
	members := map[string][]bd.Bead{}
	var unparented []bd.Bead
	for _, b := range beads {
		if b.Kind == "" || b.Container {
			continue
		}
		if _, ok := heads[b.Parent]; ok {
			members[b.Parent] = append(members[b.Parent], b)
		} else {
			unparented = append(unparented, b)
		}
	}
	slices.SortStableFunc(order, func(a, b string) int { return beadCmp(*heads[a], *heads[b]) })
	var groups []group
	if len(unparented) > 0 {
		slices.SortStableFunc(unparented, beadCmp)
		groups = append(groups, group{key: "", name: "Unparented", members: unparented})
	}
	for _, id := range order {
		slices.SortStableFunc(members[id], beadCmp)
		h := heads[id]
		groups = append(groups, group{key: id, head: h, name: chartName(h.Title), members: members[id]})
	}
	return groups
}

// beadCmp is the one list order Graph and Inbox share (the Spec's beadLess,
// FR2): open before closed, then priority ascending (P0 first), then
// newest-created first — the order the loop cares about (bd ready sorts by priority).
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

// chartName shortens a Chart title for the group header (the reconcile-seeded
// title is the Signal's title; anything after an em-dash is detail).
func chartName(title string) string {
	if i := strings.Index(title, " — "); i >= 0 {
		return title[:i]
	}
	return title
}

// row is one rendered/navigable line in the master pane: a group header
// (collapsible; a Chart head also carries a bead) or a member leaf.
type row struct {
	sel         string // selection key: bead id, or unparentedSel
	beadID      string // the bead this row selects; "" for the head-less unparented header
	collapseKey string // group key this row toggles; "" for a leaf (not collapsible)
	header      bool
	last        bool // leaf only: last member of its group (└ vs ├ tree connector)
}

// navRows flattens the groups into the visible master rows in render order,
// respecting collapse (spec §9.1: j/k walk the visible tree; children under a
// collapsed group are skipped, chart heads included).
func navRows(groups []group, collapsed map[string]bool) []row {
	var rows []row
	for _, g := range groups {
		if g.head != nil {
			rows = append(rows, row{sel: g.head.ID, beadID: g.head.ID, collapseKey: g.key, header: true})
		} else {
			rows = append(rows, row{sel: unparentedSel, collapseKey: g.key, header: true})
		}
		if collapsed[g.key] {
			continue
		}
		for i, m := range g.members {
			rows = append(rows, row{sel: m.ID, beadID: m.ID, last: i == len(g.members)-1})
		}
	}
	return rows
}

// isClosedRoot reports whether a bead is a closed root — no parent and no longer
// open. The `h` toggle hides these: finished top-level work (a drained Chart, a
// closed intake Signal) folded away to declutter the graph.
func isClosedRoot(b bd.Bead) bool { return b.Parent == "" && !b.Open() }

// hideClosedRootGroups drops closed-root beads when the `h` toggle is on: a group
// whose Chart head is a closed root is hidden whole (head + members), and closed
// root leaves are dropped from the Unparented group (a group left empty is dropped
// too). When off, groups pass through unchanged. A bead with a session is never
// hidden (ADR 0038 §2): it keeps its leaf, or its whole group.
func hideClosedRootGroups(groups []group, hide bool, sessions map[string]session.Info) []group {
	if !hide {
		return groups
	}
	var out []group
	for _, g := range groups {
		if g.head != nil {
			if isClosedRoot(*g.head) && !hasSession(g, sessions) {
				continue
			}
			out = append(out, g)
			continue
		}
		var kept []bd.Bead
		for _, m := range g.members {
			if sessions[m.ID].Has() || !isClosedRoot(m) {
				kept = append(kept, m)
			}
		}
		if len(kept) > 0 {
			g.members = kept
			out = append(out, g)
		}
	}
	return out
}

// hasSession reports whether g's head or any member has a session.
func hasSession(g group, sessions map[string]session.Info) bool {
	if sessions[g.head.ID].Has() {
		return true
	}
	for _, m := range g.members {
		if sessions[m.ID].Has() {
			return true
		}
	}
	return false
}

// seedCollapsedClosedRoots marks each closed-root group head collapsed in the
// startup collapse map: closed epics open folded away. It reuses the
// isClosedRoot predicate so this opening state agrees with hideClosedRootGroups.
// Head-less groups (Unparented) are never seeded. It only sets keys, so a group
// already collapsed stays collapsed and nothing is ever force-expanded.
func seedCollapsedClosedRoots(groups []group, collapsed map[string]bool) {
	for _, g := range groups {
		if g.head != nil && isClosedRoot(*g.head) {
			collapsed[g.key] = true
		}
	}
}

// inboxList is the Inbox queue (spec §9.2): open HITL kinds only, plus any HITL
// bead in sessions whatever its status (a session never drops from view, ADR 0038
// §1), in beadCmp order. A Checkpoint's open pivots collapse
// to one row, its lead (FR20–22, ADR 0045/0046): the human triages it once.
// An open bead with any open blocker is left out — only work the human can take
// now — unless it has a session (FR14–16); Graph and Search still show it.
func inboxList(beads []bd.Bead, sessions map[string]session.Info) []bd.Bead {
	leads := pivotLeads(beads)
	closed := map[string]bool{}
	for _, b := range beads {
		closed[b.ID] = b.Status == "closed"
	}
	// A blocker missing from the snapshot counts as open, as in openBlockers.
	blocked := func(b bd.Bead) bool {
		return slices.ContainsFunc(b.BlockedBy, func(id string) bool { return !closed[id] })
	}
	var out []bd.Bead
	for _, b := range beads {
		if !isHITL(b.Kind) {
			continue
		}
		// A lead shows while Open: a claimed lead whose session was lost must not
		// hide its whole Checkpoint behind a lead slot with no row.
		isLead := b.Kind == contract.KindPivot && b.Open() && leads[pivotGate(beads, b.ID)].ID == b.ID
		if sessions[b.ID].Has() || isLead || (b.Status == "open" && b.Kind != contract.KindPivot && !blocked(b)) {
			out = append(out, b)
		}
	}
	slices.SortStableFunc(out, beadCmp)
	return out
}

// pivotLeads maps each gate (pivotGate) to its lead: the oldest open pivot, the
// first seen on a tie. The Inbox row and the detail's "other open pivots" line
// both key on it, so they agree on which pivot leads.
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

// pivotGate is the Checkpoint a pivot blocks — the bead whose BlockedBy (the
// blocks gate edge, FR15) names it — or the pivot's own id before the raise tail
// has gated it, so an ungated pivot stands as its own lead.
// ponytail: O(beads) scan per call, O(n²) per Inbox render; index once if the snapshot grows large.
func pivotGate(beads []bd.Bead, pivotID string) string {
	for _, b := range beads {
		if b.Kind == contract.KindCheckpoint && slices.Contains(b.BlockedBy, pivotID) {
			return b.ID
		}
	}
	return pivotID
}

// otherOpenPivots is every open pivot gating the same Checkpoint as p, bar p,
// oldest first — what the lead's session also covers (FR22). Snapshot twin of
// bd.OpenPivots/bd.PivotCheckpoint: the same blocks gate, read without a bd call.
func otherOpenPivots(beads []bd.Bead, p bd.Bead) []bd.Bead {
	gate := pivotGate(beads, p.ID)
	var out []bd.Bead
	for _, b := range beads {
		if b.Kind == contract.KindPivot && b.Open() && b.ID != p.ID && pivotGate(beads, b.ID) == gate {
			out = append(out, b)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}

// launchFor returns the command the human runs to launch a bead (spec §9.3).
// Every HITL kind — a Signal included (ADR 0039 §6; the `signal triage` verb is
// CLI-only) — resolves to its registry owner and returns that crewmate's Launch
// command directly, so an agent launch carries the harness's pre-assigned
// session id; the owner-from-kind seam keeps the command with the kind's owner,
// not here.
func launchFor(ctx contract.Ctx, crew map[string]contract.Crewmate, b bd.Bead) (contract.LaunchCommand, error) {
	owner, err := launchOwner(crew, b.Kind)
	if err != nil {
		return nil, err
	}
	return crew[owner].Launch(contract.WithClosingLine(ctx), contract.Bead{ID: b.ID, Kind: b.Kind})
}

// launchOwner resolves a kind to its registry owner and checks the owner is
// wired in crew: launchFor's pre-Launch checks, I/O-free so the launch preview
// can make them on render (ADR 0101 §2).
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

// tokenizeArgs splits the edit escape-hatch input into bd args, honoring single
// and double quotes so a value with spaces (--reason "not doing this") stays one
// arg. Minimal shell-style tokenizer — no escapes, no nesting (YAGNI).
func tokenizeArgs(s string) []string {
	var args []string
	var cur strings.Builder
	inTok := false
	var quote rune
	flush := func() {
		if inTok {
			args = append(args, cur.String())
			cur.Reset()
			inTok = false
		}
	}
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
			inTok = true
		case r == '"' || r == '\'':
			quote = r
			inTok = true
		case r == ' ' || r == '\t':
			flush()
		default:
			cur.WriteRune(r)
			inTok = true
		}
	}
	flush()
	return args
}

// guidedUpdateArgs builds the `bd update <id>` flag args for one guided-edit
// field: "title" or "description". The value rides as a --flag arg
// rather than a temp-file path — exec.Command hands argv straight to bd with no
// shell in the way, so a multi-line description survives with its newlines intact
// (bd's -d/--description accepts an embedded-newline value). Pure so the arg
// construction is unit-tested without the interactive editor.
func guidedUpdateArgs(field, value string) []string {
	return []string{"--" + field, value}
}

// editMode is the guided/raw edit state machine. editPick is the tiny
// title/description chooser (e); editTitle is the inline single-line title editor;
// editRaw is the raw `bd update <id> …` arg escape hatch (E). Description editing
// runs out-of-band in $EDITOR, so it has no inline mode.
type editMode int

const (
	editNone editMode = iota
	editPick
	editTitle
	editRaw
	editCreate           // 2-field new-Signal form on the Inbox tab
	editConfirmLaunch    // y-only confirm before launching a blocked HITL bead (ADR 0026)
	editConfirmDrop      // y-only confirm before dropping a bead's session (ADR 0038 §4)
	editConfirmAbort     // y-only confirm before aborting a row's live AFK effort (ADR 0083)
	editConfirmOrphan    // y-only confirm before dropping an orphaned worktree, then launching (ADR 0041 §3)
	editConfirmForceStop // y-only confirm before force-stopping a draining loop (ADR 0051 §3)
	editConfirmQuit      // y-only confirm before a quit abandons run-now efforts (ADR 0051 quit amendment)
	editSearch           // the / regex search prompt (Spec FR4)
	editSearchText       // Search's live / word query (Spec D5): not the regex prompt
	editSearchPick       // Search's f/c/s facet picker (Spec D5)
	editRejectReason     // z's one-line reject reason (Spec FR3)
	editConfirmReject    // y-only confirm before z closes the Signal (Spec FR2)
)

// --- bubbletea model ---

type tab int

const (
	tabGraph tab = iota
	tabInbox
	tabSearch   // the flat filter-and-sort bead list (ADR 0072)
	tabHistory  // main's landed Checkpoints and reverts
	tabLogs     // the event log, raw and newest-first
	tabStats    // Session cost and Agent time over a window
	tabSettings // the setting rows, the AFK pool's first (ADR 0109)
	tabCount    // left/right cycle over the tabs above
)

// loadedMsg carries one refresh's bead snapshot plus the effort liveness map
// probed alongside it and the tmux session table (ADR 0036 §1) — a
// volatile view, re-derived every tick and never read by run-now or dispatch
// (ADR 0031).
type loadedMsg struct {
	beads    []bd.Bead
	live     map[string]bool
	sessions map[string]session.Info
	loop     looplock.Status
	commits  []git.MainlineCommit // the History tab's mainline read
	histErr  error                // that read failed; the rest of the load stands
	efforts  map[string]bool      // bead ids holding effort/<id> (git.EffortRefs)
	loopTip  string               // a watched loop's source mainline tip; "" unknown
	pool     loop.Setting         // the layered AFK pool read, for the Settings row
	autoExit loop.AutoExit        // the layered auto_exit read, for the Settings row
}
type errMsg struct{ err error }

type tickMsg struct{}
type prefixMsg string // the tmux prefix, in the reference's notation

// runDoneMsg folds a background run-now effort's completion back into the model:
// id names the bead, err its outcome, tail the last N lines of the
// captured output surfaced only on failure (Spec FR10/FR12).
type runDoneMsg struct {
	id   string
	run  string // the effort id, matched against Model.runs
	err  error
	tail string
}

// launchDoneMsg is a session start or resume's outcome, run in a tea.Cmd so l
// never freezes the TUI (Spec FR9). bg is b's no-switch intent (ADR
// 0100). branch is the start's routed branch for the busy check; a resume has
// none. afk/afkErr are afkHolder(branch), read in the Cmd only when git refused
// the start as busy, so onBusy runs no bd call on the loop (ADR 0098 §1).
type launchDoneMsg struct {
	b      bd.Bead
	resume bool
	bg     bool // b's start: success stays in the TUI, no switch (ADR 0100)
	branch string
	err    error
	afk    string
	afkErr error
}

// writeDoneMsg is a background bd write's outcome (ADR 0098 §2): Update drops
// key from pending, then apply folds the result in (status via setStatus, any
// echo, the reload or follow-on Cmd).
type writeDoneMsg struct {
	key   string                             // the pending key: bead id, or a create form's
	apply func(m Model) (tea.Model, tea.Cmd) // runs on the Model current when the write lands
}

// descEditedMsg is fired when the $EDITOR description session returns:
// path is the temp file holding the edited body, id the bead to commit it to.
type descEditedMsg struct {
	id   string
	path string
	err  error
}

// Model is the TUI state. It holds the crewmate ctx + crew table (for launch, as
// the loop does) and re-reads the whole graph each refresh — no state is derived
// once and cached (spec §9.5, ADR 0003 re-derive-from-bd).
type Model struct {
	ctx  contract.Ctx
	crew map[string]contract.Crewmate

	beads        []bd.Bead               // the graph's work beads; event beads never land here (ADR 0057 §3)
	live         map[string]bool         // effort liveness view: active AFK effort id → alive (ADR 0031); display only
	sessions     map[string]session.Info // Session list by bead id (ADR 0036 §1); display + attach only
	efforts      map[string]bool         // bead ids holding effort/<id>, read in refresh, never in render (ADR 0101, Spec D7)
	groups       []group
	chrome       int                 // View's memoized chromeW for this render (0 = measure); never set on a stored Model
	loop         looplock.Status     // the loop's state per the last read (ADR 0051 §6); display + o only
	loopTip      string              // a watched loop's source mainline tip, same read; "" unknown or failed (display only)
	loopBusy     bool                // a loop verb o ran has not returned, or (a stop) no read has confirmed it yet
	loopFrom     looplock.Status     // the loop that verb acted on; a read showing another state or pid confirms it
	loopStarting bool                // o's start has not errored and no read has confirmed it: the tab bar reads loop starting…
	watch        bool                // opened with --watch: o starts `loop start --watch`
	runs         []dispatch.Inflight // run-now efforts in flight, oldest first: claimed, no runDoneMsg yet

	filters   [tabCount]listFilter // each tab's kind filter + search (Spec FR3–7)
	preFilter map[string]bool      // the Graph's collapse map before its filter applied; nil while unfiltered (FR6)
	graphAll  []group              // the Graph before its kind/search filter (h applied): f's kinds, the bar's total

	launching map[string]bool   // bead ids whose session start/resume is in flight; a second l/b on one is refused (ADR 0107 §5)
	attachTo  string            // the one in-flight launch that switches this terminal when it lands: the latest l's (ADR 0107 §6)
	pending   map[string]string // bd writes in flight, bead id (or create-form key) → its verb for the row word (ADR 0107 §1); a second write on a key is refused (ADR 0098 §3)

	tab             tab
	selGraph        string               // selection key in the Graph master (bead id or unparentedSel)
	selInbox        string               // selected bead id in the Inbox
	selSearch       string               // selected bead id on Search ("" = none)
	selHist         string               // selected History row's commit sha ("" = none)
	histCommits     []git.MainlineCommit // History's last mainline read, newest first
	histErr         error                // that read failed (History shows it)
	search          searchQuery          // Search's query, in memory only (ADR 0077)
	searchPrev      string               // the text as it was on /, what esc in input reverts to
	pickFacet       string               // the open Search picker's key: f status, c kind, s sort
	pickCur         string               // its cursor, by value so a reload can't shift it
	collapsed       map[string]bool
	seeded          bool   // opening collapse state applied once, on the first load
	hideClosedRoots bool   // `h` toggles hiding closed root beads (parentless, done)
	focusDetail     bool   // tab toggles focus master ↔ detail (spec §9.4)
	detailOffset    int    // scroll offset (visual rows) into the focused detail body
	help            bool   // ? key reference replaces the body (ADR 0058 §4); not an editMode
	helpOffset      int    // scroll offset (lines) into the ? reference
	prefix          string // the tmux prefix in the reference's notation (ctrl+b), read once at startup

	editMode editMode // guided/raw edit state; editNone when not editing
	editBuf  line     // inline text input (title, raw arg hatch, reject reason, filter, Search text)
	editID   string   // bead pinned when an edit/confirm mode opens; the mode acts on it, not the selection
	orphanWT string   // the orphaned worktree editConfirmOrphan drops (ADR 0041 §3)
	launchBg bool     // the blocked/orphan confirm's pending launch is b's: y starts without the switch (ADR 0100)
	dropSess string   // @session editConfirmDrop opened on; a refresh mid-confirm can't retarget y

	createTitle   line // new-Signal / Surgery / Rollback form fields, each with its own cursor; active only in editCreate
	createDesc    line
	createField   int    // which create field has focus: 0 title, 1 description
	createFor     string // the Checkpoint an R form rolls back (Spec FR8); "" files a Signal
	createSurgery bool   // the u form: enter files a Surgery and launches it (Spec D10)

	status         string // transient hint / last-action result
	deferredStatus string // a result that landed while the quit confirm was up; a cancel shows it
	loadErr        error
	loading        bool // a tick's load is in flight; the next tick skips its load (embedded-Dolt bd serializes, so overlapping loads pile up)

	evlog       *eventlog.Log    // the process's event log (WithLog); nil reads as no path
	logs        []eventlog.Event // event log in append order; the tab shows it reversed
	logSkipped  int              // unparsed lines skipped by the last read
	statsWin    int              // the Stats window, an index into statsWindows
	statsBy     int              // the Stats grouping, an index into statsGroups
	statsTotal  stats.Row        // the Stats table, cached by recomputeStats
	statsRows   []stats.Row
	pool        loop.Setting          // the AFK pool in force, cached by the refresh (Spec FR25)
	inflight    int                   // AFK efforts in flight, counted as run-now counts them
	staged      int                   // the AFK pool size +/- staged on Settings; 0 = none (ADR 0109)
	stagedFrom  int                   // the cached pool size when staging began, so a moved file shows
	exit        loop.AutoExit         // the auto_exit in force, cached by the refresh (Spec D10)
	exitStaged  bool                  // an Auto-exit value is staged on Settings (off is a value, so not 0 = none)
	stagedExit  time.Duration         // the staged Auto-exit
	exitFrom    time.Duration         // the cached auto_exit when staging began
	setRow      int                   // the Settings row +/- act on: 0 pool, 1 Auto-exit
	logSig      eventlog.Sig          // stat of the files last read; unchanged → no re-read
	logPath     string                // the event log's path, for the empty state
	logErr      error                 // the log is unavailable (unresolvable or unreadable)
	selLog      eventlog.Key          // selected event's identity; zero → newest
	exitsSeen   map[eventlog.Key]bool // hitl.exited events already read; nil until the first read seeds it, so history never flashes
	closingSeen map[string]bool       // bead ids the last session read showed Closing; nil until the first read seeds it (ADR 0108 §4)
	logWarned   bool                  // the emitter's one-time warning has reached the status line
	logDebug    bool                  // d: show Debug records (hidden by default, ADR 0027 §4)
	logErrors   bool                  // e: Error records only
	// L scope from the Graph: a bead id, "" for none; logScopeKids also matches its
	// direct children (a container, ADR 0029 §3).
	logScope     string
	logScopeKids bool

	usageRead   usageReader        // the Plan usage reader (WithUsage); nil = unavailable, no polling
	usage       contract.PlanUsage // the last good Plan usage read
	usageAt     time.Time          // when that read landed; zero = no good read yet
	usageErr    error              // the latest read's failure, its text the unavailable reason; nil after a good read
	usageNext   time.Time          // a RetryAfter backoff: polls before it send nothing
	usageReadAt time.Time          // the latest read's fire or landing, for the Stats 60s floor
	usageSkip   string             // the token that last got a 401, passed back as skipToken

	w, h int
}

// New builds the TUI model with the crewmate ctx and owner→crewmate table (only
// Nami is wired for the MVP, matching the loop, spec §5.2).
func New(ctx contract.Ctx, crew map[string]contract.Crewmate) Model {
	return Model{
		ctx:             ctx,
		crew:            crew,
		collapsed:       map[string]bool{},
		hideClosedRoots: true,                                // open with closed roots hidden
		search:          searchQuery{status: statusUnclosed}, // D6 defaults: any kind, newest updated
		prefix:          "ctrl+b",
		statsWin:        statsWinDefault,
		w:               80,
		h:               24,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.load(), m.loadLogs(), tick(), m.prefixCmd(), m.pollUsage())
}

func (m Model) load() tea.Cmd {
	return func() tea.Msg {
		beads, err := listAll()
		if err != nil {
			return errMsg{err}
		}
		// Same predicate + probe as `mugicrew ps`, so the two surfaces can't disagree.
		var ids []string
		for _, b := range beads {
			if loop.IsActiveAFKEffort(b) {
				ids = append(ids, b.ID)
			}
		}
		recordClient()                                // refresh F12's client → TUI map; a failure only costs F12 its way back (it detaches)
		sessions, _ := readSessions()                 // a failed read is an empty table: rows fall back to ·
		setBars(barStates(beads, sessions), sessions) // a failure only costs this tick's done flip
		lp, err := readLoop()
		if err != nil {
			lp = looplock.Status{State: looplock.Off} // ponytail: an unreadable lock shows off; o's CLI error says why
		}
		tip := ""
		if lp.Watch && lp.Source != "" {
			tip, _ = readTip(lp.Source) // a failed read keeps the hash: never latest without proof
		}
		// ponytail: History's git log re-runs every tick on any tab; gate it on tabHistory if ticks get slow.
		commits, histErr := readMainline()
		efforts, _ := readEfforts() // ponytail: a failed read shows no refs; t's press-time read is the gate
		return loadedMsg{beads, probeLiveness(ids), sessions, lp, commits, histErr, efforts, tip, readPool(), readAutoExit()}
	}
}

// barStates is each alive HITL session's bead id → closed, the done flip SetBars
// applies (ADR 0066 §4): derived from this read, never latched, so a reopen reverts.
// An exited session gets nothing.
func barStates(beads []bd.Bead, sessions map[string]session.Info) map[string]bool {
	closed := map[string]bool{}
	for _, b := range beads {
		if sessions[b.ID].State == session.Alive {
			closed[b.ID] = b.Status == "closed"
		}
	}
	return closed
}

func tick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case loadedMsg:
		m.live, m.sessions, m.loop, m.loopTip, m.efforts = msg.live, msg.sessions, msg.loop, msg.loopTip, msg.efforts
		m.flashCloseFailed()
		m.histCommits, m.histErr = msg.commits, msg.histErr
		m.pool, m.exit = msg.pool, msg.autoExit
		var inProgress []bd.Bead
		for _, b := range msg.beads {
			if b.Status == "in_progress" {
				inProgress = append(inProgress, b)
			}
		}
		m.inflight = loop.CountInflight(inProgress)
		selIdx, pickIdx := m.searchPlace() // before the beads change under them
		// Event beads are history, not work: dropped here so no row or count sees them.
		m.beads = nil
		for _, b := range msg.beads {
			if !b.Event {
				m.beads = append(m.beads, b)
			}
		}
		// A read showing the loop left the state the verb acted on confirms it (a
		// stopped restart drain loses restart_to); the tab bar now shows the state.
		// A stale read still shows the old one, so wait.
		if m.loop.State != m.loopFrom.State || m.loop.Pid != m.loopFrom.Pid || m.loop.RestartTo != m.loopFrom.RestartTo {
			m.loopBusy, m.loopStarting = false, false
			if m.status == startingLoop || m.status == stoppingLoop {
				m.status = ""
			}
		}
		if !m.seeded {
			// Seed opening collapse state once, over the unfiltered graph so
			// closed-head groups stay folded even while hidden by `h`.
			seedCollapsedClosedRoots(buildGraph(m.beads), m.collapsed)
			m.seeded = true
		}
		m.rebuildGroups()
		m.loadErr, m.loading = nil, false
		m.keepSearchPlace(selIdx, pickIdx)
		m.fixSelection()
	case prefixMsg:
		m.prefix = string(msg)
	case errMsg:
		m.loadErr, m.loading = msg.err, false
		m.loopBusy = false // no read will confirm while bd is down; a plain stop still re-reads the lock
	case tickMsg:
		if w := m.evlog.Warning(); w != "" && !m.logWarned && m.editMode != editConfirmQuit {
			m.status, m.logWarned = w, true
		}
		if m.loading {
			return m, tea.Batch(m.loadLogs(), tick())
		}
		m.loading = true
		return m, tea.Batch(m.load(), m.loadLogs(), tick())
	case logsMsg:
		if msg.changed {
			m.pinLog() // FR45: anchor what the reader sees before the rows shift
			m.logs, m.logSkipped, m.logSig, m.logErr = msg.events, msg.skipped, msg.sig, msg.err
			m.pinLog() // and the newest on first load
			m.recomputeStats()
		}
		if msg.err == nil && msg.path != "" { // an unread log must not seed exitsSeen empty
			m.flashFinished()
		}
		m.logPath = msg.path
	case usageTickMsg:
		return m, m.pollUsage()
	case usageMsg:
		m.foldUsage(msg)
	case runDoneMsg:
		m.runs = slices.DeleteFunc(slices.Clone(m.runs), func(r dispatch.Inflight) bool { return r.ID == msg.run })
		// Completion status + refresh: the shared dispatch mutated the graph itself
		// (close + spawn), so a reload surfaces the bead's new state without the
		// human pressing r (Spec FR10/FR11). On failure the captured tail
		// rides along for a no-log-file diagnosis (FR12).
		s := "run " + msg.id + " done"
		if msg.err != nil {
			s = "run " + msg.id + " failed: " + msg.err.Error()
			if msg.tail != "" {
				s += "\n" + msg.tail
			}
		}
		m.setStatus(s)
		return m, m.load()
	case loopDoneMsg:
		return m.loopDone(msg)
	case launchDoneMsg:
		return m.launchDone(msg)
	case writeDoneMsg:
		m.pending = maps.Clone(m.pending)
		delete(m.pending, msg.key)
		return msg.apply(m)
	case descEditedMsg:
		return m.finishDescEdit(msg)
	case tea.KeyMsg:
		if m.editMode != editNone {
			return m.updateEditing(msg)
		}
		if m.help {
			return m.updateHelp(msg)
		}
		return m.updateKey(msg)
	}
	return m, nil
}

// switchTab is the one tab change the arrows and G/I/L share: status and detail
// offset reset; the current tab is a no-op (ADR 0058 §1).
func (m Model) switchTab(t tab) (tea.Model, tea.Cmd) {
	if t == m.tab {
		return m, nil
	}
	m.tab, m.status, m.detailOffset = t, "", 0
	if t == tabStats {
		m.recomputeStats() // the window edge moved with time
		return m, m.refreshUsage()
	}
	return m, nil
}

func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if (m.tab == tabSearch && !slices.Contains(searchKeys, msg.String())) ||
		(m.tab == tabHistory && !slices.Contains(historyKeys, msg.String())) ||
		(m.tab == tabStats && !slices.Contains(statsKeys, msg.String())) ||
		(m.tab == tabSettings && !slices.Contains(settingsKeys, msg.String())) {
		return m, nil
	}
	if m.tab == tabSettings {
		if next, cmd, ok := m.settingsKey(msg.String()); ok {
			return next, cmd
		}
	}
	switch msg.String() {
	case "q", "ctrl+c":
		if len(m.runs) == 0 && len(m.pending) == 0 {
			return m, tea.Quit
		}
		m.editMode, m.deferredStatus = editConfirmQuit, ""
		m.status = confirmQuitPrompt(m.runs, m.pending, m.w)
	case "left", "shift+tab":
		return m.switchTab(tab(clampWrap(int(m.tab)-1, int(tabCount))))
	case "right":
		return m.switchTab(tab(clampWrap(int(m.tab)+1, int(tabCount))))
	case "G":
		if r, ok := m.histSelection(); m.tab == tabHistory && ok {
			return m.reveal(r.id)
		}
		if b := m.selectedBead(); (m.tab == tabSearch || m.tab == tabInbox) && b != nil {
			return m.reveal(b.ID)
		}
		return m.switchTab(tabGraph)
	case "I":
		return m.switchTab(tabInbox)
	case "S":
		return m.switchTab(tabSearch)
	case "H":
		return m.switchTab(tabHistory)
	case "#":
		return m.switchTab(tabStats)
	case "*":
		return m.switchTab(tabSettings)
	case "tab":
		m.focusDetail = !m.focusDetail
	case "j", "down":
		if m.focusDetail {
			m.scrollDetail(1)
		} else {
			m.move(1)
		}
	case "k", "up":
		if m.focusDetail {
			m.scrollDetail(-1)
		} else {
			m.move(-1)
		}
	case " ", "enter":
		if r, ok := m.histSelection(); m.tab == tabHistory && ok {
			return m.reveal(r.id) // FR7: enter reveals, as G does
		}
		if m.tab == tabSearch {
			m.focusDetail = true // enter opens the selection's detail
		}
		m.toggleCollapse()
	case "l":
		return m.launch()
	case "b":
		if m.tab == tabGraph || m.tab == tabInbox { // b acts only where l launches (Spec FR20)
			return m.launchAs(true)
		}
		if m.tab == tabStats { // b groups on Stats, never launches (Spec FR31)
			m.statsBy = (m.statsBy + 1) % len(statsGroups)
			m.recomputeStats()
		}
	case "w":
		if m.tab == tabStats {
			m.statsWin = (m.statsWin + 1) % len(statsWindows)
			m.recomputeStats()
		}
	case "t":
		return m.runNow()
	case "p":
		return m.togglePause()
	case "x":
		if b := m.selectedBead(); b != nil && !m.launching[b.ID] && !m.refuseClosing(b.ID) && !m.refusePending(b.ID) { // never drop a session mid-start/resume or mid-close, nor stack on a write (ADR 0107 §2, ADR 0108 §5)
			if s := m.sessions[b.ID]; s.Has() {
				m.editMode, m.editID, m.dropSess = editConfirmDrop, b.ID, s.Session
				m.status = confirmDropPrompt(b.Title, m.w)
			} else if m.liveOf(b) == alive { // a session always wins x; else abort a live AFK effort (ADR 0083)
				m.editMode, m.editID = editConfirmAbort, b.ID
				m.status = confirmAbortPrompt(b.Title, m.w)
			}
		}
	case "n", "u":
		// n always files a plain, unparented intake Signal (ADR 0006), whatever the
		// selection; u files a Surgery (ADR 0095). Both only on Graph and Inbox.
		if m.tab == tabInbox || m.tab == tabGraph {
			m.editMode = editCreate
			m.createTitle, m.createDesc, m.createField, m.createFor = line{}, line{}, 0, ""
			m.createSurgery, m.status = msg.String() == "u", ""
		}
	case "R":
		if m.tab == tabHistory {
			m.openRollback()
		}
	case "d":
		if m.tab == tabLogs {
			m.logDebug = !m.logDebug
			m.detailOffset = 0
		}
	case "L":
		// On Graph with a selection, L scopes Logs to the bead (ADR 0029 §3);
		// elsewhere it plain-jumps and keeps any scope (ADR 0058 §1).
		if b := m.selectedBead(); m.tab == tabGraph && b != nil {
			m.logScope, m.logScopeKids, m.selLog = b.ID, b.Container, eventlog.Key{}
		}
		return m.switchTab(tabLogs)
	case "f":
		if m.tab == tabSearch {
			m.openPicker("f") // a status picker on Search, not the kind cycle (Spec FR21)
		} else {
			m.cycleKind()
		}
	case "c", "s":
		if m.tab == tabSearch {
			m.openPicker(msg.String())
		}
	case "/":
		if m.tab == tabSearch {
			// Focus the list: single-pane detail focus would hide the header being typed into.
			m.editMode, m.searchPrev, m.status, m.focusDetail = editSearchText, m.search.text, "", false
			m.editBuf = lineAt(m.search.text)
		} else {
			m.editMode, m.editBuf, m.status = editSearch, lineAt(m.filters[m.tab].src), ""
		}
	case "esc":
		if m.tab == tabSearch {
			m.setSearchText("") // the text only: facets are deliberate (FR19)
		} else {
			m.clearFilter()
		}
	case "e":
		if m.tab == tabLogs {
			m.logErrors = !m.logErrors
			m.detailOffset = 0
		} else if b := m.selectedBead(); b != nil && !m.refusePending(b.ID) {
			m.editMode = editPick
			m.editID = b.ID
			m.status = ""
		}
	case "E":
		if b := m.selectedBead(); b != nil && !m.refusePending(b.ID) {
			m.editMode = editRaw
			m.editID = b.ID
			m.editBuf = lineAt("--")
			m.status = ""
		}
	case "h":
		// Graph-only (ADR 0058 §6): Inbox/Logs can't show the change.
		if m.tab == tabGraph {
			m.hideClosedRoots = !m.hideClosedRoots
			m.rebuildGroups()
			m.fixSelection()
			if m.hideClosedRoots {
				m.status = "hiding closed root beads"
			} else {
				m.status = "showing closed root beads"
			}
		}
	case "z":
		if b := m.selectedBead(); b != nil && rejectable(*b) {
			m.editMode, m.editID, m.editBuf, m.status = editRejectReason, b.ID, line{}, ""
		}
	case "+", "=":
		return m.bumpPriority(-1)
	case "-":
		return m.bumpPriority(1)
	case "?":
		m.help, m.helpOffset = true, 0
	case "o":
		return m.toggleLoop()
	case "f5": // r is left unbound, freed for rollback (Spec FR17-18)
		m.status = "refreshing…"
		if m.tab == tabStats {
			return m, tea.Batch(m.load(), m.loadLogs(), m.refreshUsage())
		}
		return m, tea.Batch(m.load(), m.loadLogs())
	}
	return m, nil
}

// minPriority and maxPriority are the bounds bumpPriority stops at: P0 (highest) and P4.
const minPriority, maxPriority = 0, 4

// bumpPriority moves the selected bead's priority by d (-1 raises, +1 lowers)
// with one background `bd update <id> --priority N`, through the edit actions'
// updateBead path, then reloads (Spec FR1, ADR 0098 §4). Past P0/P4 or
// on a closed bead nothing is written and the status says why.
func (m Model) bumpPriority(d int) (tea.Model, tea.Cmd) {
	b := m.selectedBead()
	if b == nil {
		return m, nil
	}
	if b.Status == "closed" {
		m.status = b.Title + " is closed — priority unchanged"
		return m, nil
	}
	p := b.Priority + d
	if p < minPriority || p > maxPriority {
		m.status = fmt.Sprintf("%s is already P%d", b.ID, b.Priority)
		return m, nil
	}
	id := b.ID
	return m.startWrite(id, "updating", func() writeDoneMsg {
		err := updateBead(id, []string{"--priority", strconv.Itoa(p)})
		return writeDoneMsg{id, func(m Model) (tea.Model, tea.Cmd) {
			// Echo on the bead as it is now, so a press before the reload steps from p.
			if i := slices.IndexFunc(m.beads, func(b bd.Bead) bool { return b.ID == id }); err == nil && i >= 0 {
				m.beads = slices.Clone(m.beads)
				m.beads[i].Priority = p
			}
			return m.doneOrErr("priority", fmt.Sprintf("%s → P%d", id, p), err)
		}}
	})
}

// startWrite runs a key's bd write in a tea.Cmd under the per-key guard (ADR
// 0098 §2–3): a key already pending is refused, never queued; otherwise it is
// marked pending under doing's verb, the row word (ADR 0107 §1: "dropping orphan
// for" reads dropping…), and its "<doing> <key>…" status written directly, as startLaunch does.
func (m Model) startWrite(key, doing string, run func() writeDoneMsg) (tea.Model, tea.Cmd) {
	if m.refusePending(key) {
		return m, nil
	}
	m.pending = maps.Clone(m.pending)
	if m.pending == nil {
		m.pending = map[string]string{}
	}
	m.pending[key], _, _ = strings.Cut(doing, " ")
	m.status = doing + " " + key + "…"
	return m, func() tea.Msg { return run() }
}

// doneOrErr is a done closure's common tail (ADR 0098 §2): on err the status
// reads "<prefix>: <err>" and nothing reloads; otherwise ok, then a reload.
func (m Model) doneOrErr(prefix, ok string, err error) (tea.Model, tea.Cmd) {
	if err != nil {
		m.setStatus(prefix + ": " + err.Error())
		return m, nil
	}
	m.setStatus(ok)
	return m, m.load()
}

// refusePending reports whether key has a write in flight, writing the guard's
// refusal status if so. The edit openers call it too, so no edit is typed against
// a bead whose commit startWrite would refuse.
func (m *Model) refusePending(key string) bool {
	_, ok := m.pending[key]
	if ok {
		m.status = key + ": write in flight, wait"
	}
	return ok
}

// refuseClosing reports whether id's session is Closing, writing the refusal
// status if so: l and x would race the close (ADR 0108 §5).
func (m *Model) refuseClosing(id string) bool {
	ok := m.closing(id)
	if ok {
		m.status = id + " is closing — wait"
	}
	return ok
}

// reveal is G on Search (Spec D7, ADR 0074 §2–3) and Inbox:
// switch to the Graph with bead id selected (a Chart's id is its group
// header's key), switching h off if it hid the bead and unfolding the bead's
// group, no other. A Graph filter hiding the bead is cleared whole (kind and
// search), its pre-filter folds back before the unfold. The calling tab's
// query, filter and selection are untouched.
func (m Model) reveal(id string) (tea.Model, tea.Cmd) {
	next, _ := m.switchTab(tabGraph)
	m = next.(Model)
	key, ok := groupOf(m.graphAll, id)
	if !ok && m.hideClosedRoots {
		m.hideClosedRoots = false
		m.status = "showing closed root beads"
		m.rebuildGroups()
		key, ok = groupOf(m.graphAll, id)
	}
	if _, shown := groupOf(m.groups, id); ok && !shown && m.filters[tabGraph].active() {
		m.filters[tabGraph] = listFilter{}
		m.filterChanged(false)
		m.status = strings.TrimPrefix(m.status+" · cleared Graph filter", " · ")
	}
	if ok {
		m.collapsed[key] = false
	}
	m.selGraph = id
	m.fixSelection()
	return m, nil
}

// groupOf is the key of the group bead id heads or sits in.
func groupOf(groups []group, id string) (string, bool) {
	for _, g := range groups {
		if g.key == id || slices.ContainsFunc(g.members, func(b bd.Bead) bool { return b.ID == id }) {
			return g.key, true
		}
	}
	return "", false
}

// rebuildGroups rebuilds graphAll from the current beads with the `h`
// hide-closed-roots filter applied, then groups through the tab's filter. Called
// on every load, whenever `h` toggles, and when the Graph filter changes.
func (m *Model) rebuildGroups() {
	m.graphAll = hideClosedRootGroups(buildGraph(m.beads), m.hideClosedRoots, m.sessions)
	m.groups = m.graphAll
	if f := m.filters[tabGraph]; f.active() {
		m.groups, _ = filterGroups(m.graphAll, f)
	}
}

// updateEditing dispatches key input while editing: the picker gets a
// single-keypress chooser, the inline modes (title/raw) share the text editor.
func (m Model) updateEditing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.editMode {
	case editPick:
		return m.updatePick(msg)
	case editCreate:
		return m.updateCreate(msg)
	case editSearch:
		return m.updateSearch(msg)
	case editSearchText:
		return m.updateSearchText(msg)
	case editSearchPick:
		return m.updateSearchPick(msg)
	case editConfirmLaunch:
		m.editMode = editNone
		if msg.String() == "y" {
			// Resolve the pinned id, not the selection: a refresh may have moved it.
			if b, ok := m.pinned(); ok && isHITL(b.Kind) {
				return m.launchNow(*b, m.launchBg)
			}
			m.status = "launch cancelled: " + m.editID + " no longer available"
			return m, nil
		}
		m.status = "launch cancelled"
		return m, nil
	case editConfirmDrop:
		m.editMode = editNone
		if msg.String() == "y" {
			return m.drop()
		}
		m.status = "drop cancelled"
		return m, nil
	case editConfirmAbort:
		m.editMode = editNone
		if msg.String() != "y" {
			m.status = "abort cancelled"
			return m, nil
		}
		id := m.editID
		return m.startWrite(id, "aborting", func() writeDoneMsg {
			pids, err := abortEffort(id, "tui")
			return writeDoneMsg{id, func(m Model) (tea.Model, tea.Cmd) { return m.abortDone(id, pids, err) }}
		})
	case editConfirmOrphan:
		m.editMode = editNone
		if msg.String() != "y" {
			m.status = "launch cancelled"
			return m, nil
		}
		b, ok := m.pinned()
		if !ok {
			m.status = "launch cancelled: " + m.editID + " no longer available"
			return m, nil
		}
		bead, wt, bg := *b, m.orphanWT, m.launchBg
		return m.startWrite(bead.ID, "dropping orphan for", func() writeDoneMsg {
			err := dropOrphan(wt)
			return writeDoneMsg{bead.ID, func(m Model) (tea.Model, tea.Cmd) {
				if err != nil {
					m.setStatus("drop orphan: " + err.Error())
					return m, nil
				}
				return m.launchNow(bead, bg)
			}}
		})
	case editConfirmForceStop:
		m.editMode = editNone
		if msg.String() == "y" {
			return m.runLoopVerb(stoppingLoop, "stop", "--force")
		}
		m.status = "force stop cancelled"
		return m, nil
	case editRejectReason:
		switch msg.Type {
		case tea.KeyEsc, tea.KeyCtrlC:
			m.editMode, m.status = editNone, "reject cancelled"
		case tea.KeyEnter:
			b, ok := m.pinned()
			if !ok {
				m.editMode, m.status = editNone, "reject cancelled: "+m.editID+" no longer available"
				return m, nil
			}
			m.editBuf = lineAt(cmp.Or(strings.TrimSpace(m.editBuf.text), rejectDefault))
			m.editMode, m.status = editConfirmReject, confirmRejectPrompt(b.Title, m.editBuf.text, m.w)
		default:
			m.editBuf = m.editBuf.apply(msg)
		}
		return m, nil
	case editConfirmReject:
		m.editMode = editNone
		if msg.String() != "y" {
			m.status = "reject cancelled"
			return m, nil
		}
		return m.reject()
	case editConfirmQuit:
		m.editMode = editNone
		if k := msg.String(); k == "y" || k == "ctrl+c" { // a second ctrl+c forces (Spec FR49)
			dispatch.Abandon(m.runs, pauseBead) // paused:abandoned before each effort.abandoned (ADR 0082)
			return m, tea.Quit
		}
		m.status, m.deferredStatus = cmp.Or(m.deferredStatus, "quit cancelled"), ""
		return m, nil
	}
	return m.updateInline(msg)
}

// drop removes the pinned bead's session, alive or exited (ADR 0037 §5), and
// records hitl.dropped — the only hitl event the TUI writes (ADR 0029 amendment).
// A dropped in_progress bead's claim is released, so the reload returns the row
// to · open with its branch free, and l launches (and claims) fresh. The
// session table is re-read, as onBusy does: one that ended, or was relaunched under
// a new @session since the confirm opened, is not dropped by name. The whole drop,
// tmux included, runs in a Cmd so hitl.dropped stays ordered before the unclaim
// (ADR 0098 §1); the pinned bead is copied from the snapshot at press time.
func (m Model) drop() (tea.Model, tea.Cmd) {
	id, sess := m.editID, m.dropSess
	var b *bd.Bead
	if p, ok := m.pinned(); ok {
		c := *p
		b = &c
	}
	return m.startWrite(id, "dropping", func() writeDoneMsg {
		status, reload := dropRun(id, sess, b)
		return writeDoneMsg{id, func(m Model) (tea.Model, tea.Cmd) {
			m.setStatus(status)
			if !reload {
				return m, nil
			}
			return m, m.load()
		}}
	})
}

// dropRun is drop's I/O: it returns the status and whether to reload.
func dropRun(id, sess string, b *bd.Bead) (string, bool) {
	fresh, err := readSessions()
	if err != nil {
		return "drop: " + err.Error(), false
	}
	if b == nil {
		return "drop cancelled: " + id + " is no longer listed", true
	}
	s, has := fresh[id]
	if !has || s.Session != sess {
		return "drop cancelled: " + id + "'s session changed, look again", true
	}
	err = dropSession(*b, s)
	eventlog.RecordHitlDropped(*b, s.Session, string(s.State), err)
	if err != nil {
		return "drop: " + err.Error(), true
	}
	status := "dropped " + id + "'s session"
	// Release the launch's claim back to open (Spec FR12); a Surgery stays
	// in_progress, idle, since its filing holds the claim.
	if b.Status == "in_progress" && b.Kind != contract.KindSurgery {
		if err := hitlUnclaim(id); err != nil {
			status += "; unclaim: " + err.Error()
		}
	}
	return status, true
}

// rejectDefault is the close reason a blank z reason falls back to (Spec FR4).
const rejectDefault = "rejected from TUI"

// rejectable is z's gate (Spec FR5): an open, unclaimed Signal. A claimed
// or in-progress one is a live session's or another actor's to close.
func rejectable(b bd.Bead) bool {
	return b.Kind == contract.KindSignal && b.Status == "open" && b.Assignee == ""
}

// reject closes the pinned Signal with the confirmed reason (editBuf) in a Cmd,
// re-checking the gate on the current snapshot at press time so a stale confirm
// can't close one someone just claimed (FR8). ponytail: the snapshot is up to
// one tick old; a bd-side compare-and-set close is the upgrade path if that
// window bites.
func (m Model) reject() (tea.Model, tea.Cmd) {
	b, ok := m.pinned()
	if !ok || !rejectable(*b) {
		m.status = m.editID + " not rejected: no longer an open, unclaimed Signal"
		return m, m.load()
	}
	id, reason := b.ID, m.editBuf.text
	return m.startWrite(id, "rejecting", func() writeDoneMsg {
		err := closeBead(id, reason)
		return writeDoneMsg{id, func(m Model) (tea.Model, tea.Cmd) {
			return m.doneOrErr("reject", "rejected "+id, err)
		}}
	})
}

// pinned resolves the bead an edit/confirm mode opened on (editID) in the current
// snapshot; false if a refresh dropped it.
func (m Model) pinned() (*bd.Bead, bool) {
	for i := range m.beads {
		if m.beads[i].ID == m.editID {
			return &m.beads[i], true
		}
	}
	return nil, false
}

// updateCreate drives the 2-field new-Signal form, or with createFor
// set the History tab's Rollback form (Spec FR8), or with createSurgery
// set the u form (Spec D10): tab (or up/down)
// switches between the title and description (evidence) fields, enter files the
// Signal (or the Rollback of createFor) from the two values in a Cmd (Spec
// FR14–15), esc or ctrl+c cancels. Text keys edit the focused field; all keys are
// ignored while the filing is in flight. Both fields are single-line; a body
// that wants newlines is edited after create through e → $EDITOR.
func (m Model) updateCreate(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := m.createKey()
	if _, ok := m.pending[key]; ok {
		return m, nil
	}
	field := &m.createTitle
	if m.createField == 1 {
		field = &m.createDesc
	}
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.editMode = editNone
		m.status = "create cancelled"
	case tea.KeyTab, tea.KeyDown, tea.KeyUp:
		m.createField ^= 1
	case tea.KeyEnter:
		title := strings.TrimSpace(m.createTitle.text)
		if title == "" {
			m.status = "create: title required"
			return m, nil
		}
		if m.createSurgery {
			return m.fileSurgery(title)
		}
		desc, of := m.createDesc.text, m.createFor
		if of != "" {
			return m.startWrite(key, "filing", func() writeDoneMsg {
				id, err := newRollback(title, desc, of)
				return writeDoneMsg{key, func(m Model) (tea.Model, tea.Cmd) {
					if id == "" && err != nil {
						m.setStatus("create: " + err.Error())
						return m, nil
					}
					m = m.closeCreate()
					if err != nil { // filed, but unrelated: no badge until the edge is added by hand
						m.setStatus("filed Rollback " + id + " without its related edge: " + err.Error())
					} else {
						m.setStatus("filed Rollback " + id + " of " + of)
					}
					return m, m.load()
				}}
			})
		}
		return m.startWrite(key, "filing", func() writeDoneMsg {
			id, err := newSignal(title, desc)
			return writeDoneMsg{key, func(m Model) (tea.Model, tea.Cmd) {
				if err == nil {
					m = m.closeCreate()
				}
				return m.doneOrErr("create", "filed Signal "+id, err)
			}}
		})
	default:
		*field = field.apply(msg)
	}
	return m, nil
}

// createKey is the open create form's pending key (ADR 0098 §2): the bead doesn't
// exist yet, so it is "new <kind>", kept visibly apart from bead ids in the
// "filing …" status and the quit prompt. The form ignores input while it is pending.
func (m Model) createKey() string {
	switch {
	case m.createSurgery:
		return "new Surgery"
	case m.createFor != "":
		return "new Rollback"
	}
	return "new Signal"
}

// closeCreate closes the create form and clears its fields.
func (m Model) closeCreate() Model {
	m.editMode = editNone
	m.createTitle, m.createDesc, m.createField = line{}, line{}, 0
	return m
}

// fileSurgery files the u form's Surgery and its Checkpoint, then launches the
// first session (Spec FR2, FR16). A filing failure names any
// orphan; a launch failure leaves the surgery idle for l to retry (FR3, see launchFailed).
func (m Model) fileSurgery(title string) (tea.Model, tea.Cmd) {
	key, desc := m.createKey(), m.createDesc.text
	return m.startWrite(key, "filing", func() writeDoneMsg {
		id, cp, err := newSurgery(title, desc)
		return writeDoneMsg{key, func(m Model) (tea.Model, tea.Cmd) {
			if err != nil {
				status := "create: " + err.Error()
				if id != "" || cp != "" {
					m = m.closeCreate()
					status += " (filed " + strings.TrimSpace(id+" "+cp) + ")"
				}
				m.setStatus(status)
				return m, nil
			}
			m = m.closeCreate()
			return m.launchNow(bd.Bead{ID: id, Kind: contract.KindSurgery, Status: "in_progress", Title: title, Parent: cp}, false)
		}}
	})
}

// updatePick handles the 'edit: (t)itle / (d)escription' chooser. t
// opens the inline title editor pre-filled with the current title; d hands the
// current description to $EDITOR; esc or ctrl+c cancels; anything else is ignored.
func (m Model) updatePick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	b, ok := m.pinned()
	if msg.Type == tea.KeyEsc || msg.Type == tea.KeyCtrlC || !ok {
		m.editMode = editNone
		m.status = "edit cancelled"
		return m, nil
	}
	switch msg.String() {
	case "t":
		m.editMode = editTitle
		m.editBuf = lineAt(b.Title) // pre-fill with the current title, cursor at the end
	case "d":
		m.editMode = editNone
		if m.refusePending(b.ID) {
			return m, nil
		}
		return m, m.editDescription(b)
	}
	return m, nil
}

// updateInline is the shared single-line text editor for the title and raw-hatch
// modes. Enter commits by mode (guided --title vs the raw arg line) in
// a Cmd (Spec FR10, FR12): the editor stays open ignoring keys while the
// write files, closes on success and keeps editBuf on failure. Esc or ctrl+c cancels.
func (m Model) updateInline(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if _, ok := m.pending[m.editID]; ok { // its commit is filing: the openers refuse a pending bead, so no other write holds it
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.editMode = editNone
		m.editBuf = line{}
		m.status = "edit cancelled"
	case tea.KeyEnter:
		id := m.editID
		if _, ok := m.pinned(); !ok {
			m.editMode, m.editBuf = editNone, line{}
			m.status = "edit cancelled: " + id + " no longer available"
			return m, nil
		}
		var args []string
		if m.editMode == editTitle {
			if strings.TrimSpace(m.editBuf.text) == "" {
				m.editMode, m.editBuf = editNone, line{}
				m.status = "edit: empty title, nothing to do"
				return m, nil
			}
			args = guidedUpdateArgs("title", m.editBuf.text)
		} else {
			args = tokenizeArgs(m.editBuf.text)
		}
		if len(args) == 0 {
			m.editMode, m.editBuf = editNone, line{}
			m.status = "edit: nothing to do"
			return m, nil
		}
		return m.startWrite(id, "updating", func() writeDoneMsg {
			err := updateBead(id, args)
			return writeDoneMsg{id, func(m Model) (tea.Model, tea.Cmd) {
				if err == nil {
					m.editMode, m.editBuf = editNone, line{}
				}
				return m.doneOrErr("edit", "updated "+id, err)
			}}
		})
	default:
		m.editBuf = m.editBuf.apply(msg)
	}
	return m, nil
}

// editDescription round-trips the bead's description through $EDITOR:
// write it to a temp file, suspend the TUI to run the editor (tea.ExecProcess,
// same pattern as launch), and hand the file path back via descEditedMsg for the
// bd commit. Falls back to vi when $EDITOR is unset; honors args in $EDITOR
// (e.g. "code --wait"). The temp file is named mugicrew-<id>-<title-slug>-*.md
// so the editor shows which bead is open (Spec FR5).
func (m *Model) editDescription(b *bd.Bead) tea.Cmd {
	prefix := "mugicrew-" + b.ID + "-"
	if s := titleSlug(b.Title); s != "" {
		prefix += s + "-"
	}
	f, err := os.CreateTemp("", prefix+"*.md")
	if err != nil {
		m.status = "edit: " + err.Error()
		return nil
	}
	if _, err := f.WriteString(b.Description); err != nil {
		f.Close()
		os.Remove(f.Name())
		m.status = "edit: " + err.Error()
		return nil
	}
	f.Close()
	path := f.Name()
	id := b.ID
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	parts := strings.Fields(editor)
	cmd := execCommand(parts[0], append(parts[1:], path)...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return descEditedMsg{id: id, path: path, err: err}
	})
}

// titleSlug turns a title into a file-name-safe slug (Spec FR6): lowercase
// ASCII letters and digits, every other run collapsed to one hyphen, no leading
// or trailing hyphen, capped at 40 bytes. "" when nothing survives.
func titleSlug(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if s := b.String(); s != "" && !strings.HasSuffix(s, "-") {
			b.WriteByte('-')
		}
	}
	s := b.String()
	if len(s) > 40 {
		s = s[:40]
	}
	return strings.Trim(s, "-")
}

// finishDescEdit commits the $EDITOR-edited description in a Cmd (Spec
// FR11): read the temp file back, run `bd update <id> --description
// <body>`, then drop the temp file once the write finishes. A human edit is
// last-write-wins by design — no serializer; the human sees the result (ADR 0070 §2).
func (m Model) finishDescEdit(msg descEditedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		os.Remove(msg.path)
		m.status = "edit: " + msg.err.Error()
		return m, nil
	}
	if _, ok := m.pending[msg.id]; ok { // the openers refuse first; if one still lands, keep the human's text
		m.setStatus(msg.id + ": write in flight, edit kept at " + msg.path)
		return m, nil
	}
	return m.startWrite(msg.id, "updating", func() writeDoneMsg {
		defer os.Remove(msg.path)
		body, err := os.ReadFile(msg.path)
		if err == nil {
			// Editors append a trailing newline; strip it so the stored body matches how
			// bd reads descriptions back (trimmed), same as git does for edited messages.
			err = updateBead(msg.id, guidedUpdateArgs("description", strings.TrimRight(string(body), "\n")))
		}
		return writeDoneMsg{msg.id, func(m Model) (tea.Model, tea.Cmd) {
			return m.doneOrErr("edit", "updated "+msg.id+" description", err)
		}}
	})
}

// move walks the visible list in the active tab (spec §9.4 j/k).
func (m *Model) move(d int) {
	if m.tab == tabLogs {
		m.moveLog(d)
	} else if m.tab == tabHistory {
		m.moveHist(d)
	} else if m.tab == tabGraph {
		rows := navRows(m.groups, m.collapsed)
		if len(rows) == 0 {
			return
		}
		i := indexRow(rows, m.selGraph)
		i = clampWrap(i+d, len(rows))
		m.selGraph = rows[i].sel
	} else {
		list, sel := m.inboxRows(), &m.selInbox
		if m.tab == tabSearch {
			list, _ = m.searchRows()
			sel = &m.selSearch
		}
		if len(list) == 0 {
			return
		}
		i := indexBead(list, *sel)
		i = clampWrap(i+d, len(list))
		*sel = list[i].ID
	}
	m.detailOffset = 0 // new selection → back to the top of its description
	m.status = ""
}

// scrollDetail moves the detail body offset by d visual rows when the detail pane
// holds focus, clamped against the same geometry the renderer uses so
// there is no blank overscroll at either end.
func (m *Model) scrollDetail(d int) {
	var maxOff int
	if m.tab == tabLogs {
		maxOff = m.logDetailMaxOff()
	} else {
		_, maxOff = m.detailLayout(m.selectedBead(), m.paneHeight(), m.detailOffset, 0, 0)
	}
	o := m.detailOffset + d
	if o > maxOff {
		o = maxOff
	}
	if o < 0 {
		o = 0
	}
	m.detailOffset = o
}

// toggleCollapse folds/unfolds the current group header (spec §9.1). A leaf row
// (no collapse key) is a no-op.
func (m *Model) toggleCollapse() {
	if m.tab != tabGraph {
		return
	}
	rows := navRows(m.groups, m.collapsed)
	i := indexRow(rows, m.selGraph)
	if i < 0 || !rows[i].header {
		return
	}
	key := rows[i].collapseKey
	m.collapsed[key] = !m.collapsed[key]
}

// launch runs the selected bead's owning-crewmate Launch command (spec §9.3):
// an agent command starts a session this terminal is switched to (ADR 0065 §1).
// Non-HITL / no selection is a
// no-op with a hint. A blocked HITL bead first asks for y-only confirmation
// (ADR 0026 §Blocked-launch confirm); y re-enters via launchNow.
func (m Model) launch() (tea.Model, tea.Cmd) { return m.launchAs(false) }

// launchAs is l's path for both keys; bg (b) starts or resumes without the
// switch, and an alive session is only named, not attached (ADR 0100).
func (m Model) launchAs(bg bool) (tea.Model, tea.Cmd) {
	b := m.selectedBead()
	if b == nil {
		return m, nil
	}
	if m.launching[b.ID] { // one launch per bead: a stray l can't start a duplicate on a stale session table
		m.status = "still launching " + cmp.Or(b.Title, b.ID) + "…"
		return m, nil
	}
	if s := m.sessions[b.ID]; s.Has() {
		// Reattach an alive session, resume an exited one in its kept worktree —
		// whatever the bead's status, no blocked confirm: nothing new is launched
		// (ADR 0038 §3). Only this terminal's client moves; another TUI viewing it stays.
		if s.State == session.Exited {
			bb, h := *b, m.ctx.Harness()
			return m.startLaunch(bb, bg, func() launchDoneMsg {
				return launchDoneMsg{b: bb, resume: true, err: resumeSession(bb, s, h)}
			})
		}
		if bg {
			m.status = "already running — l attach"
			return m, nil
		}
		if m.refuseClosing(b.ID) { // its agent has exited; the close owns the pane (ADR 0108 §5)
			return m, nil
		}
		m.attachTo = "" // an instant attach outranks any launch still in flight (ADR 0107 §6)
		return m.attach(b.ID)
	}
	if !isHITL(b.Kind) {
		m.status = b.ID + " is not a launchable HITL bead (AFK/container — use edit to intervene)"
		return m, nil
	}
	// Only a new launch is refused; attach/resume above work whatever the bead
	// (ADR 0058 §6), so the handler matches the hidden l hint.
	if b.Status == "closed" {
		m.status = b.ID + " is closed — not launching"
		return m, nil
	}
	if b.Container {
		m.status = b.ID + " is a container — not launching"
		return m, nil
	}
	if m.blocked(b) {
		m.editMode, m.editID, m.launchBg = editConfirmLaunch, b.ID, bg
		m.status = confirmLaunchPrompt(b.Title, m.openBlockers(b), m.w)
		return m, nil
	}
	return m.launchNow(*b, bg)
}

// launchNow is the launch path proper: owner routing, then a detached tmux
// session (worktree cwd, busy interlock) the TUI attaches to unless bg. The
// command is resolved inside the Cmd: a crewmate's Launch may read bd (ADR 0101
// §3), and its error reaches the status through launchDone's launchFailed.
func (m Model) launchNow(b bd.Bead, bg bool) (tea.Model, tea.Cmd) {
	ctx, crew := m.ctx, m.crew
	return m.startLaunch(b, bg, func() launchDoneMsg {
		lc, err := launchFor(ctx, crew, b)
		if err != nil {
			return launchDoneMsg{b: b, err: err}
		}
		c, ok := lc.(contract.AgentCommand)
		switch {
		case !ok:
			return launchDoneMsg{b: b, err: fmt.Errorf("unknown command %T", lc)}
		case len(c.Argv) == 0:
			return launchDoneMsg{b: b, err: errors.New("empty command")}
		}
		// Start the session detached on the repo's tmux server, in the tree its kind
		// routes to, then switch this terminal to it (ADR 0065 §1).
		// The bead is claimed first, as AFK dispatch does (Spec FR9-10): a
		// failed claim starts nothing, and a failed start releases the claim (ADR 0034).
		// A Surgery is skipped: its filing already claimed it.
		// An idle in_progress Task is skipped too: l resumes it.
		resume := b.Kind == contract.KindTask && b.Status == "in_progress"
		claim := b.Kind != contract.KindSurgery && !resume
		if claim {
			if err := hitlClaim(b.ID); err != nil {
				return launchDoneMsg{b: b, err: fmt.Errorf("claim: %w", err)}
			}
		}
		d, _ := route.Route(b) // startSession routes too: a route error never reaches the busy check
		err = startSession(b, c)
		if err != nil && claim {
			_ = hitlUnclaim(b.ID) // ponytail: a failed release strands the claim; bd unclaim by hand
		}
		msg := launchDoneMsg{b: b, branch: d.Branch, err: err}
		if _, busy := git.Busy(err, d.Branch); busy {
			msg.afk, msg.afkErr = afkHolder(d.Branch)
		}
		return msg
	})
}

// startLaunch marks b's launch in flight and runs it in a tea.Cmd; launchDone
// folds the result back (Spec FR9). An l launch takes the attach slot,
// displacing any other (ADR 0107 §6). Its status goes through setStatus: the
// orphan drop's done message launches too, and must not rewrite a quit confirm.
func (m Model) startLaunch(b bd.Bead, bg bool, run func() launchDoneMsg) (tea.Model, tea.Cmd) {
	m.launching = maps.Clone(m.launching) // Update's value receiver: never mutate a map an earlier Model shares
	if m.launching == nil {
		m.launching = map[string]bool{}
	}
	m.launching[b.ID] = true
	s := "launching " + cmp.Or(b.Title, b.ID) + "…"
	if !bg {
		m.attachTo, s = b.ID, s+" (attaches when ready)"
	}
	m.setStatus(s)
	return m, func() tea.Msg {
		msg := run()
		msg.bg = bg
		return msg
	}
}

// abortDone is the abort write's done closure (ADR 0107 §2): x's outcome (signalled pids, paused only, or the error)
// and reloads. A refusal or pause failure signalled nothing; a kill error keeps
// what was signalled.
func (m Model) abortDone(id string, signalled []int, err error) (tea.Model, tea.Cmd) {
	pids := dispatch.FormatPids(signalled)
	switch {
	case err != nil && pids != "":
		m.setStatus("abort: " + err.Error() + " (signalled " + pids + ")")
	case err != nil:
		m.setStatus("abort: " + err.Error())
	case pids != "":
		m.setStatus("aborted " + id + ": signalled " + pids)
	default:
		m.setStatus("aborted " + id + ": no live process; paused only")
	}
	return m, m.load()
}

// launchDone handles a start/resume outcome as the synchronous path did: attach on
// success only if this launch still holds the attach slot; a b start, or an l
// displaced from the slot (named as downgraded), only names itself and reloads
// (ADR 0100, ADR 0107 §6); a failed resume
// is the status, hinting x only while the re-read table still has it exited
// (alive means another TUI resumed it, and the reload is the answer). A start git refused because a listed worktree holds the branch (ADR
// 0056) — the AFK loop or another session — goes to onBusy instead of
// surfacing a crash/corrupted-tree error; a holderless refusal is a plain launch
// error. The in-flight mark clears on every outcome. A success that lands while an
// edit/confirm is open only reloads: the row turns alive and l attaches, rather
// than switching away from (or blanking) the open prompt.
// ponytail: a failure mid-confirm still overwrites the prompt text; the mode and
// its footer keys stay, so y/n work. Defer it like the quit confirm if it bites.
func (m Model) launchDone(msg launchDoneMsg) (tea.Model, tea.Cmd) {
	m.launching = maps.Clone(m.launching)
	delete(m.launching, msg.b.ID)
	slot := m.attachTo == msg.b.ID
	if slot {
		m.attachTo = ""
	}
	switch {
	case msg.err == nil && m.editMode != editNone:
		return m, m.load()
	case msg.err == nil && !slot:
		s := "started " + cmp.Or(msg.b.Title, msg.b.ID) + " — l attaches"
		if !msg.bg {
			s += " (downgraded: a later l took the attach)"
		}
		m.setStatus(s)
		return m, m.load()
	case msg.err == nil:
		m.setStatus("")
		return m.attach(msg.b.ID)
	case msg.resume:
		s := "resume: " + msg.err.Error()
		if fresh, ferr := readSessions(); ferr == nil && fresh[msg.b.ID].State == session.Exited {
			s += " — x drops it"
		}
		m.setStatus(s)
		return m, m.load()
	}
	if h, busy := git.Busy(msg.err, msg.branch); busy {
		return m.onBusy(msg, h.Path)
	}
	m.setStatus(launchFailed(msg.b, msg.err))
	return m, nil
}

// launchFailed is a failed launch's status; a Surgery's names it idle, since l
// retries it (Spec FR3).
func launchFailed(b bd.Bead, err error) string {
	if b.Kind == contract.KindSurgery {
		return "launch: " + err.Error() + " — Surgery " + b.ID + " idle, l retries"
	}
	return "launch: " + err.Error()
}

// attach switches this terminal's client to id's session (ADR 0065 §1); the TUI
// keeps running and refreshing on its own session meanwhile. It reloads either
// way: the session may have just been started or resumed.
func (m Model) attach(id string) (tea.Model, tea.Cmd) {
	if err := switchSession(id); err != nil {
		m.setStatus("attach: " + err.Error())
	}
	return m, m.load()
}

// onBusy answers a fresh launch git refused as busy (ADR 0041 §2–3): an orphaned
// worktree holding the bead's branch opens the orphan confirm; any other holder
// keeps the busy status, naming the holding bead where one is known (ADR 0037 §7).
// The beads, AFK liveness, and session table are all re-read, not the last
// refresh's: a session or effort started since must not read as an orphan; the
// launch Cmd read the AFK holder (msg.afk), the session table is read here. Any
// read that fails keeps the plain busy status — never a confirm on a guess.
// ponytail: an AFK worktreeRun's tree exists a few ms before its reconcile
// subprocess does, and after it exits until teardown; liveness sees only the
// process, so that tree reads as an orphan in those windows. The human y on the
// confirm is the guard; skip trees younger than a few seconds if it ever bites.
func (m Model) onBusy(msg launchDoneMsg, wt string) (tea.Model, tea.Cmd) {
	b, afk, afkErr := msg.b, msg.afk, msg.afkErr
	busy := func(holder string) (tea.Model, tea.Cmd) {
		m.setStatus(b.ID + ": " + string(b.Kind) + " branch busy — held by " + holder + ", retry")
		return m, nil
	}
	sessions, sessErr := readSessions()
	if afk != "" {
		return busy(afk + "'s AFK effort")
	}
	if id := session.HolderBead(wt, sessions); id != "" {
		return busy(id + "'s session")
	}
	for id := range m.launching { // a tree an in-flight launch is still building is never an orphan (ADR 0107 §5)
		if d, err := route.Route(*m.beadByID(id)); err == nil && d.Branch == msg.branch {
			return busy(id + "'s launch")
		}
	}
	if afkErr != nil || sessErr != nil || !session.Orphan(wt, sessions) {
		return busy("another session")
	}
	if m.editMode != editNone { // the launch landed mid-edit/confirm: never clobber it with the orphan confirm
		return busy("an orphaned worktree " + wt)
	}
	m.editMode, m.editID, m.orphanWT, m.launchBg = editConfirmOrphan, b.ID, wt, msg.bg
	m.status = confirmOrphanPrompt(b.Title, wt, m.w)
	return m, nil
}

// afkHolder is the live AFK effort whose tree is on branch, "" if none: the loop's
// tree is a mugicrew-wt-* worktree with no session, yet no orphan. It reads a fresh
// bead list, so an effort claimed since the last refresh still counts.
func afkHolder(branch string) (string, error) {
	live, err := liveAFK()
	if err != nil {
		return "", err
	}
	for _, b := range live {
		if d, err := route.Route(b); err == nil && d.Branch == branch {
			return b.ID, nil
		}
	}
	return "", nil
}

// liveAFK is loop's shared live-AFK reader over the TUI's list and probe seams:
// afkHolder's set, and run-now's in-flight set for Dispatch's own-contention test
// (ADR 0054).
func liveAFK() ([]bd.Bead, error) { return loop.LiveAFK(listAll, probeLiveness)() }

// runTailLines is how many trailing lines of a failed effort's captured output
// the status surfaces for diagnosis (Spec FR12) — enough for a stack/error
// context, not the whole run. ponytail: fixed; a scrollable log pane is OOS (Q5).
const runTailLines = 12

// runNow is the t run-now trigger (Spec FR1,2,7-11,16-18): dispatch a ready
// AFK bead's owner effort through the SAME shared entry point the loop uses
// (dispatch.Dispatch), in the background so the UI never freezes for the minutes an
// agentic reconcile takes. Eligibility is the loop's parity contract — only a bead
// the loop would dispatch this tick — so a human can't force work through a side
// door (FR5). Graph tab only; the Inbox is open-HITL and never carries AFK work.
func (m Model) runNow() (tea.Model, tea.Cmd) {
	if m.tab != tabGraph {
		return m, nil
	}
	b := m.selectedBead()
	if b == nil {
		// The Unparented header selects no bead (spec §9.1); t is a no-op with a
		// hint, not a silent nothing (Spec FR4). An empty graph selects nothing.
		if m.tab == tabGraph && m.selGraph == unparentedSel {
			m.status = "Unparented is a group header — select a Signal and press l to triage it, or a Rollback and press t to run it"
		}
		return m, nil
	}
	// The reads, eligibility, pool check and claim run in one Cmd under the bead's
	// pending key (ADR 0098 §6): a second t, or any other write, on it is refused.
	bead, crews := *b, m.crew
	return m.startWrite(bead.ID, "dispatching", func() writeDoneMsg {
		status, crew, r := claimRunNow(bead, crews)
		return writeDoneMsg{bead.ID, func(m Model) (tea.Model, tea.Cmd) {
			if crew == nil {
				m.setStatus(status)
				return m, nil
			}
			// Tracked only once claimed, so a concurrent loop tick's busy count is truthful.
			m.runs = append(slices.Clip(m.runs), r)
			return m, runNowCmd(m.ctx, crew, r)
		}}
	})
}

// claimRunNow is t's background half: the press-time eligibility re-check, the
// crewmate lookup, the AFK pool check and the claim, in that order. It returns
// the crewmate and the minted Inflight once b is claimed, else a nil crewmate and
// the refusal or error status.
func claimRunNow(b bd.Bead, crews map[string]contract.Crewmate) (string, contract.Crewmate, dispatch.Inflight) {
	fail := func(err error) (string, contract.Crewmate, dispatch.Inflight) {
		return "run-now: " + err.Error(), nil, dispatch.Inflight{}
	}
	refuse := func(s string) (string, contract.Crewmate, dispatch.Inflight) { return s, nil, dispatch.Inflight{} }
	// Press-time eligibility is the authoritative gate: bd ready is an I/O read
	// re-checked now, so a one-refresh-stale row can't run a since-drained bead.
	ready, err := readyFrontier()
	if err != nil {
		return fail(err)
	}
	// Widened as the loop's is (Spec D6): bd ready hides a Task-held Checkpoint.
	open, err := openBeads()
	if err != nil {
		return fail(err)
	}
	ready = loop.Readmit(ready, open)
	// The pause labels come from this fresh read too: a manual pause added, or a
	// foreign-hold lifted, since the last refresh is honoured now.
	if i := slices.IndexFunc(ready, func(r bd.Bead) bool { return r.ID == b.ID }); i >= 0 {
		b.Labels = ready[i].Labels
	}
	inProgress, err := inProgressBeads()
	if err != nil {
		return fail(err)
	}
	// Inherited pause off the loop's own reads — the paused list plus in-progress
	// rows (ADR 0081 §7) — through the loop's own check, for parity with candidates().
	paused, err := pausedBeads()
	if err != nil {
		return fail(err)
	}
	// A hop outside those reads goes to bd show, as the loop's does (Spec F:
	// parity); a failed show refuses the press, never a silent "not paused".
	anc, err := loop.InheritedPause(ready, slices.Concat(paused, inProgress), showBead)(b)
	if err != nil {
		return fail(err)
	}
	ok, held, err := runNowEligible(b, ready, anc)
	if err != nil {
		return fail(err)
	}
	if !ok {
		return refuse(runNowHint(b, ready, anc, held))
	}
	e, err := registry.Lookup(b.Kind)
	if err != nil {
		return fail(err)
	}
	crew, ok := crews[e.Owner]
	if !ok {
		return refuse(fmt.Sprintf("run-now: no crewmate %q wired for %s", e.Owner, b.ID))
	}
	// Re-derive the AFK pool's in-flight total from bd and honor the pool BEFORE
	// claiming (Spec FR9). The loop and TUI are separate processes coordinating
	// only through bd, so the trigger reuses the loop's own CountInflight and Pool —
	// the same sources the loop reads, never re-spelled — counting every owner's AFK
	// against the layered pool size (local override, then the Workspace's config.toml,
	// then 3; ADR 0110), re-read on every press.
	// ponytail: this is a check-then-claim across two processes, NOT atomic — bd has no
	// claim-if-under-pool primitive, so loop and TUI reading the same count on one tick
	// can overshoot the pool by one. Accepted: the land lock is the safety rule (ADR 0102), the
	// pool only a throughput/cost budget the next tick re-reads (Spec FR34); NO
	// cross-process lock is added (Spec FR22, ADR 0070 §4).
	root, _ := git.RepoRoot() // unresolved ⇒ "" ⇒ the layers read relative to cwd, never a refusal
	if n, pool := loop.CountInflight(inProgress), loop.Pool(root); n >= pool {
		return refuse(fmt.Sprintf("AFK pool full (%d/%d) — %s will run once an AFK effort finishes", n, pool, b.ID))
	}
	// Claim in_progress BEFORE the dispatch goroutine (spec §6.5/FR16), exactly as
	// the loop does: a concurrent loop tick then sees a truthful busy count and
	// does not double-dispatch. Claim is deliberately the caller's step, NOT inside
	// dispatch.Dispatch, to preserve that "claim before goroutine" invariant.
	if s := dropKeptTree(b); s != "" {
		return refuse(s)
	}
	if err := claimBead(b); err != nil {
		return refuse("run-now: claim " + b.ID + ": " + err.Error())
	}
	return "", crew, dispatch.Inflight{ID: dispatch.NewEffortID(), Bead: b}
}

// dropKeptTree is t's handling of an orphaned tree still holding b's effort/<id>
// before the cut (ADR 0104 §3, ADR 0105 §2): a backstop conflict's kept tree, or a
// stale loop tree no in-flight effort accounts for. A clean one is dropped so the
// retry recuts on the reused branch; a dirty one refuses, naming the tree. Any other
// holder (a session, a live effort, a human checkout) is left to Dispatch's held
// path. It returns the refusal status, "" to go on.
func dropKeptTree(b bd.Bead) string {
	d, ok, _ := route.OnEffort(b)
	if !ok {
		return ""
	}
	h, ok, err := git.WorktreeOn(d.Branch)
	if err != nil {
		return "run-now: " + err.Error()
	}
	if !ok {
		return ""
	}
	sessions, err := readSessions()
	if err != nil || !session.Orphan(h.Path, sessions) {
		return ""
	}
	if id, err := afkHolder(d.Branch); err != nil || id != "" {
		return ""
	}
	dirty := false
	if !h.Prunable { // a prunable tree's directory is already gone
		if dirty, err = treeDirty(h.Path); err != nil {
			return "run-now: " + err.Error()
		}
	}
	if dirty {
		return b.ID + ": " + d.Branch + " is held by an orphaned worktree with uncommitted work, " + h.Path +
			" — commit or discard it there, or 'git worktree remove --force " + h.Path + "', then t"
	}
	if err := dropOrphan(h.Path); err != nil {
		return "run-now: drop orphan: " + err.Error()
	}
	return ""
}

// pauseLabel is the footer's p label: p adds or lifts only paused:manual; a
// foreign-hold pause stays beside it for the loop to lift (ADR 0070 §3).
func pauseLabel(b bd.Bead) string {
	if bd.PausedBy(b, bd.PausedManual) {
		return "unpause"
	}
	return "pause"
}

// togglePause is the p bare manual pause (ADR 0057 §6): no paused:manual → add
// it, paused:manual → lift it, closed → refused; foreign-hold is never touched.
// No confirm (p reverses it) and no event-log write (ADR 0055 §8). The label
// write runs in a Cmd under the per-bead guard (ADR 0098); the status suffix is
// composed from the bead as seen at press time, then the view reloads.
func (m Model) togglePause() (tea.Model, tea.Cmd) {
	b := m.selectedBead()
	if m.tab != tabGraph || b == nil {
		return m, nil
	}
	if b.Status == "closed" {
		m.status = b.ID + ": closed beads are never dispatched"
		return m, nil
	}
	id, verb, doing, write, done, why := b.ID, "pause", "pausing", pauseBead, "paused "+b.ID, ""
	if bd.PausedBy(*b, bd.PausedManual) {
		verb, doing, write, done = "unpause", "unpausing", unpauseBead, "unpaused "+b.ID
		// Deliberate (FR18/FR19): a container still inherited-paused says so in
		// place of "its members resume" — they don't yet.
		if anc := m.inheritedPause(b); anc != "" {
			why = " — still paused via " + anc
		} else if b.Container {
			why = " — its members resume"
		}
	} else {
		if bd.PausedBy(*b, bd.PausedForeignHold) {
			why = " — foreign-hold stays until the loop lifts it"
		}
		if b.Container { // the pause cascades through membership (ADR 0081)
			why += " — its open members are out of dispatch too"
			if b.Status == "in_progress" {
				why += "; the running effort continues"
			}
		} else if b.Status == "in_progress" {
			why += " — the running effort continues"
		}
	}
	return m.startWrite(id, doing, func() writeDoneMsg {
		err := write(id, bd.PausedManual)
		return writeDoneMsg{id, func(m Model) (tea.Model, tea.Cmd) {
			return m.doneOrErr(verb, done+why, err)
		}}
	})
}

// setStatus shows a background result, or holds it while the quit confirm is up:
// the prompt is never rewritten under the reader (Spec FR50).
func (m *Model) setStatus(s string) {
	if m.editMode == editConfirmQuit {
		m.deferredStatus = s
	} else {
		m.status = s
	}
}

// confirmQuitPrompt is the quit confirm (ADR 0051 quit amendment): `N run-now
// efforts will be abandoned: <title>…, <title>… +N — quit?`, then the writes
// still in flight, named by key (Spec FR26): `; N writes in flight: <key>…`.
func confirmQuitPrompt(runs []dispatch.Inflight, pending map[string]string, w int) string {
	const quit = " — quit?"
	end := quit // only the last part folds against it
	if len(pending) > 0 {
		end = ""
	}
	s := ""
	if len(runs) > 0 {
		s = foldTitles(fmt.Sprintf("%d run-now efforts will be abandoned: ", len(runs)), bd.Titles(dispatch.Beads(runs)), end, w)
	}
	if len(pending) == 0 {
		return s
	}
	if s != "" {
		s += "; "
	}
	noun := "writes"
	if len(pending) == 1 {
		noun = "write"
	}
	return foldTitles(s+fmt.Sprintf("%d %s in flight: ", len(pending), noun), slices.Sorted(maps.Keys(pending)), quit, w)
}

// runNowEligible is the strict-parity predicate (Spec FR5): a bead is
// run-now-eligible iff the loop would dispatch it this tick — it is in the bd
// ready frontier AND a loop-dispatched AFK kind. It reuses the loop's own
// classifiers (never re-spelled — they ARE the parity contract): a research leaf
// via loop.IsAFKLeaf, a drained defogged Chart via
// loop.IsDrainedChartCandidate + 0 open children, or a land-ready
// Checkpoint via loop.IsCheckpointCandidate + loop.CheckpointReady,
// mirroring the loop's candidates() three-arm classify. A Chart's closed children
// still holding effort/<id> count as open (Spec D7) and come back as held
// for the refusal to name. The child/git reads are
// I/O, so this press-time check is the authoritative gate — the row affordance may
// be one refresh stale.
// The AFK pool refusal is runNow's, after eligibility.
// A bead with paused:manual or paused:abandoned (ADR 0082), or under a manual-
// paused ancestor anc (inherited pause, ADR 0081/0084), is never eligible, whatever
// else it carries; one with only paused:foreign-hold is (run-now overrides it, ADR
// 0055 §6).
func runNowEligible(b bd.Bead, ready []bd.Bead, anc string) (ok bool, held []bd.Bead, err error) {
	if pauseRefusesRunNow(b, anc) || !inFrontier(b.ID, ready) {
		return false, nil, nil
	}
	switch {
	case loop.IsAFKLeaf(b):
		return true, nil, nil
	case loop.IsDrainedChartCandidate(b):
		open, held, err := drainBlockers(b.ID)
		return err == nil && open == 0 && len(held) == 0, held, err
	case loop.IsCheckpointCandidate(b):
		return checkpointLandHold(b)
	default:
		return false, nil, nil
	}
}

// pauseRefusesRunNow is run-now's pause gate, shared by the press-time check and
// the t affordance: an own manual or abandoned pause, or an inherited one (anc,
// the manual-paused ancestor, "" for none), refuses; integrate-conflict, failing
// and foreign-hold don't.
func pauseRefusesRunNow(b bd.Bead, anc string) bool {
	return bd.PausedBy(b, bd.PausedManual) || bd.PausedBy(b, bd.PausedAbandoned) || anc != ""
}

func inFrontier(id string, ready []bd.Bead) bool {
	for _, r := range ready {
		if r.ID == id {
			return true
		}
	}
	return false
}

// runNowHint explains why t is a no-op on an ineligible row (Spec FR4),
// one message per row class — mirroring launch's non-HITL guard so a refused
// press says what the row IS and what to do instead, never a bare "not eligible".
// Reached only after the press-time gate (runNowEligible) has refused, so it
// classifies from the loop's own predicates: a closed bead, an open HITL bead
// (launch it), a not-drained Chart, or a blocked/not-ready bead. held is the
// Chart's closed children still holding effort/<id>, named with both ways out, or
// the open blockers holding an approved Checkpoint's land (Spec FR 45).
func runNowHint(b bd.Bead, ready []bd.Bead, anc string, held []bd.Bead) string {
	switch {
	case b.Status == "closed":
		return b.ID + " is closed — kept for lineage, nothing to run"
	case bd.PausedBy(b, bd.PausedManual):
		return b.ID + " is paused by hand — press p or run 'bd label remove " + b.ID + " paused:manual' first"
	case bd.PausedBy(b, bd.PausedAbandoned):
		return b.ID + ": abandoned effort may still be running; check, then 'bd label remove " + b.ID + " paused:abandoned'"
	case anc != "":
		return b.ID + " is paused via " + anc + " — select it and press p, or run 'bd label remove " + anc + " paused:manual'"
	case isHITL(b.Kind):
		return b.ID + " is an open HITL bead — press l to launch it, not t"
	case !inFrontier(b.ID, ready):
		// Frontier first: a drained, defogged Chart that is merely blocked (not in
		// bd ready) is NOT un-drained — reporting the drain/defog state here would
		// lie. The Chart arms below only speak to a Chart that IS in the frontier.
		return b.ID + " is blocked / not ready — run-now only drives a bead already in the bd ready frontier"
	case loop.IsDrainedChartCandidate(b) && len(held) > 0:
		return b.ID + ": Chart not drained — " + bd.HeldHint(held)
	case loop.IsDrainedChartCandidate(b):
		return b.ID + ": Chart not drained — its defog frontier hasn't cleared; run-now drives it once every child closes"
	case loop.IsCheckpointCandidate(b) && len(held) > 0:
		return b.ID + ": " + landHeldNote(bd.Titles(held))
	case loop.IsCheckpointCandidate(b):
		return b.ID + ": Checkpoint not ready to land — needs a closed Approval pinning the branch HEAD and no open conflict/critique children"
	default:
		return b.ID + " is not run-now eligible (only a ready AFK leaf — e.g. research or a Rollback — a drained Chart, or a land-ready Checkpoint runs via t)"
	}
}

// landHeldNote is the held-land words the preview and t's refusal share (Spec
// FR 44–45).
func landHeldNote(titles []string) string {
	return "approved; land held by open blocker: " + strings.Join(titles, ", ")
}

// landHeldBy is the titles of the open blockers holding a landable-looking
// Checkpoint's land per the last refresh (nil when it isn't held): the I/O-free
// twin of loop.CheckpointLandHold's held list.
func (m Model) landHeldBy(b *bd.Bead) []string {
	if !m.checkpointLooksLandable(b.ID) {
		return nil
	}
	var titles []string
	for _, h := range m.openBlockers(b) {
		titles = append(titles, cmp.Or(h.Title, h.ID)) // a blocker missing from the snapshot has no title
	}
	return titles
}

// runNowAffordable reports whether the launch preview / footer offers t.
func (m Model) runNowAffordable(b *bd.Bead) bool { return m.runNowLabel(b) != "" }

// runNowLabel is the I/O-free run-now affordance for the launch preview /
// footer t key (Spec FR6): the footer label (run/spec/land) if the selected
// bead LOOKS run-now-eligible from the already-loaded graph, else "" — mirroring runNowEligible's arms against
// model state only — an open AFK leaf, a drained defogged Chart (every child
// closed, per childCounts over m.beads), or a Checkpoint that looks landable
// (checkpointLooksLandable). It is a hint that may be one refresh
// stale: runNow's press-time bd ready / open-children read is the authoritative
// gate. It deliberately does no bd I/O so rendering never blocks the UI.
// A manual, abandoned (ADR 0082) or inherited (ADR 0084) pause hides t; on an
// integrate-conflict (ADR 0104 §3) or failing (ADR 0078) pause t reads retry, else
// on a foreign-hold pause retry hold (ADR 0057 §5).
func (m Model) runNowLabel(b *bd.Bead) string {
	if b == nil || b.Status != "open" || pauseRefusesRunNow(*b, m.inheritedPause(b)) {
		return ""
	}
	l := ""
	switch {
	case loop.IsAFKLeaf(*b):
		l = "run"
	case loop.IsDrainedChartCandidate(*b):
		// drained: no open children left, and none closed still holding effort/<id>
		if closed, total := m.childCounts(b.ID); closed == total && len(m.heldChildren(b.ID)) == 0 {
			l = "spec"
		}
	case loop.IsCheckpointCandidate(*b):
		// I/O-free approximation of CheckpointReady's cheap legs: no open
		// conflict/critique child and a closed Approval present. The git approvedAtHead
		// leg is inherently I/O, so runNowEligible's press-time read is the real gate —
		// this only surfaces the t affordance, and may be one refresh stale.
		// An open blocker holds the land (Spec FR 44): the preview names it instead.
		if m.checkpointLooksLandable(b.ID) && len(m.landHeldBy(b)) == 0 {
			l = "land"
		}
	}
	switch {
	case l == "":
		return ""
	// manual > abandoned > integrate-conflict > failing > foreign-hold (ADR 0078, 0104 §4)
	case bd.PausedBy(*b, bd.PausedIntegrateConflict), bd.PausedBy(*b, bd.PausedFailing):
		return "retry"
	case bd.PausedBy(*b, bd.PausedForeignHold):
		return "retry hold"
	}
	return l
}

// heldChildren is a container's closed children still holding effort/<id> per the
// last refresh (Spec D7): the drain hint's I/O-free twin of bd.DrainBlockers.
func (m Model) heldChildren(id string) []bd.Bead {
	var kids []bd.Bead
	for _, b := range m.beads {
		if b.Parent == id {
			kids = append(kids, b)
		}
	}
	_, held := bd.DrainBlockersOver(kids, m.efforts)
	return held
}

// checkpointLooksLandable is the I/O-free half of loop.CheckpointReady over model
// state (m.beads): true when the Checkpoint has a closed kind:approval child and no
// open kind:conflict / kind:critique child. It deliberately omits the git
// approvedAtHead / landed-trailer leg (I/O) — runNowEligible re-checks that at press
// time — so it is an affordance hint, not the authoritative gate.
func (m Model) checkpointLooksLandable(id string) bool {
	hasClosedApproval := false
	for _, b := range m.beads {
		if b.Parent != id {
			continue
		}
		switch b.Kind {
		case contract.KindConflict, contract.KindCritique:
			if b.Status == "open" {
				return false
			}
		case contract.KindApproval:
			if b.Status == "closed" {
				hasClosedApproval = true
			}
		}
	}
	return hasClosedApproval
}

// runNowCmd runs the shared dispatch entry point in a goroutine (a tea.Cmd) so the
// UI stays live (FR7). Output goes to a buffer — never the terminal — so a
// background effort can't corrupt the bubbletea alt-screen the TUI owns (FR8); on
// failure the tail rides back for diagnosis (FR12), on success it is discarded.
func runNowCmd(ctx contract.Ctx, crew contract.Crewmate, r dispatch.Inflight) tea.Cmd {
	return func() tea.Msg {
		var buf bytes.Buffer
		o, err := runDispatch(ctx, crew, r.Bead, &buf, r.ID, liveAFK)
		clean := err == nil // the run's own return, before any unpause error joins in
		// A retry hold whose cut went through lifts the pause (ADR 0055 §6); a refused
		// one already logged effort.held and keeps it. The loop's held count is
		// another process's, untouched.
		// ponytail: lifted once Dispatch returns, not at the cut — ◐ masks the ‖ while it
		// runs and the loop's auto-clear reads only ready beads; hook Dispatch if that bites.
		if o.Cut == dispatch.CutOK && bd.PausedBy(r.Bead, bd.PausedForeignHold) {
			if uerr := unpauseBead(r.Bead.ID, bd.PausedForeignHold); uerr != nil {
				err = errors.Join(err, uerr)
			} else {
				slog.Info("effort.resumed", append(eventlog.BeadAttrs(r.Bead), "cause", bd.PausedForeignHold, "branch", o.Branch)...)
			}
		}
		// A failing pause lifts on a clean return (ADR 0078); a failed one keeps it,
		// and the loop's failure count is another process's, untouched.
		if clean && bd.PausedBy(r.Bead, bd.PausedFailing) {
			if uerr := unpauseBead(r.Bead.ID, bd.PausedFailing); uerr != nil {
				err = errors.Join(err, uerr)
			} else {
				slog.Info("effort.resumed", append(eventlog.BeadAttrs(r.Bead), "cause", bd.PausedFailing)...)
			}
		}
		tail := ""
		if err != nil {
			tail = lastLines(buf.String(), runTailLines)
		}
		return runDoneMsg{id: r.Bead.ID, run: r.ID, err: err, tail: tail}
	}
}

// lastLines returns the final n lines of s (fewer if s is shorter), the failure
// tail surfaced in the status.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// selectedBead resolves the active tab's selection to a bead, or nil (the
// unparented header, an empty graph, or the Logs tab, which selects events).
func (m Model) selectedBead() *bd.Bead {
	if m.tab == tabLogs || m.tab == tabHistory || m.tab == tabStats || m.tab == tabSettings {
		return nil
	}
	id := m.selInbox
	switch m.tab {
	case tabGraph:
		id = m.selGraph
	case tabSearch:
		id = m.selSearch
	}
	if id == "" || id == unparentedSel {
		return nil
	}
	for i := range m.beads {
		if m.beads[i].ID == id {
			return &m.beads[i]
		}
	}
	return nil
}

// fixSelection re-anchors selections after a refresh: if the selected row is
// gone, fall back to the first visible row (Search's reload runs keepSearchPlace first).
func (m *Model) fixSelection() {
	// A filter can empty a list: then nothing is selected, not a hidden row.
	if rows := navRows(m.groups, m.collapsed); len(rows) == 0 {
		m.selGraph = ""
	} else if indexRow(rows, m.selGraph) < 0 {
		m.selGraph = rows[0].sel
	}
	if list := m.inboxRows(); len(list) == 0 {
		m.selInbox = ""
	} else if indexBead(list, m.selInbox) < 0 {
		m.selInbox = list[0].ID
	}
	// Search's reload fallback is the nearest neighbour (keepSearchPlace); a query change lands on the top row.
	if list, _ := m.searchRows(); len(list) == 0 {
		m.selSearch = ""
	} else if indexBead(list, m.selSearch) < 0 {
		m.selSearch = list[0].ID
	}
	m.fixHist()
}

func indexRow(rows []row, sel string) int {
	for i, r := range rows {
		if r.sel == sel {
			return i
		}
	}
	return -1
}

func indexBead(list []bd.Bead, id string) int {
	for i, b := range list {
		if b.ID == id {
			return i
		}
	}
	return -1
}

// clampWrap wraps an index into [0,n) (j past the end returns to the top).
func clampWrap(i, n int) int {
	if n == 0 {
		return 0
	}
	return ((i % n) + n) % n
}
