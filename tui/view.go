package tui

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/dispatch"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/liveness"
	"jayulabs.org/mugicrew/loop"
	"jayulabs.org/mugicrew/registry"
	"jayulabs.org/mugicrew/session"
)

// execCommand is exec.Command, indirected so the $EDITOR description edit is
// testable without spawning a real process. ponytail: test seam only.
var execCommand = exec.Command

// The session seams — session.Start / .Resume / .SwitchTo / .Drop / .DropOrphan /
// .Sessions / .RecordClient / .SetBars / .Prefix — indirected so the launch and drop tests drive tmux
// without a real server (ADR 0039 §4, ADR 0037 §5).
var (
	startSession  = session.Start
	resumeSession = session.Resume
	switchSession = session.SwitchTo
	dropSession   = session.Drop
	dropOrphan    = session.DropOrphan
	treeDirty     = git.Dirty        // whether t's kept effort tree has uncommitted work (ADR 0104 §3)
	readSessions  = session.Sessions // the tmux session table (ADR 0036 §1); its read failure matters only to orphan classification (ADR 0041 §2)
	recordClient  = session.RecordClient
	setBars       = session.SetBars
	readPrefix    = session.Prefix
)

// newSignal is bd.NewSignal, indirected so the Inbox create-form submit is
// testable without filing a live bead against the Dolt DB.
var newSignal = bd.NewSignal

// newSurgery is bd.NewSurgery, indirected like newSignal for the u form.
var newSurgery = bd.NewSurgery

// newRollback is bd.NewRollback, indirected like newSignal for the History R form.
var newRollback = bd.NewRollback

// updateBead is bd.Update, indirected so the edit-pin test sees which bead an
// inline edit commits to without a live bd.
var updateBead = bd.Update

// closeBead is bd.Close, indirected so the z reject test sees the close it issues.
var closeBead = bd.Close

// HITL claim seams (Spec FR9-12): a fresh l claims with the status CAS
// (ADR 0052), a start failure or x releases it. Indirected so the launch tests
// see the writes without a live bd.
var (
	hitlClaim   = bd.Claim
	hitlUnclaim = bd.Unclaim
)

// pause (p) seams, indirected so the toggle test sees which bd write happens.
var (
	pauseBead   = bd.Pause
	unpauseBead = bd.Unpause
)

// abortEffort is x's shared abort on a live AFK row (ADR 0083), the same one the
// CLI runs, indirected so the x test sees the call without signalling anything.
var abortEffort = dispatch.Abort

// run-now (t) seams, indirected so the trigger test drives the whole
// path — eligibility read, synchronous claim, background dispatch — without a
// live bd or a real reconcile subprocess. runDispatch is the SAME shared entry
// point the loop calls (Spec FR19), so neither launch path can drift.
var (
	readyFrontier   = bd.Ready
	claimBead       = dispatch.Claim
	runDispatch     = dispatch.Dispatch // under a TUI-minted effort id, so a confirmed quit can name what it abandons
	drainBlockers   = bd.DrainBlockers  // drained-Chart detection (Spec D7), a var so the eligibility test drives it without a live bd
	inProgressBeads = bd.InProgress     // AFK pool re-derivation, a var so the pool test drives inflight without a live bd
	pausedBeads     = bd.PausedList     // the loop's per-tick paused read, for inherited-pause parity, a var so the run-now tests run without a live bd
	showBead        = bd.Show           // an inherited-pause hop outside the press-time reads and the snapshot
	listAll         = bd.ListAll        // the refresh snapshot, a var so the liveness-load test runs without a live bd
	readEfforts     = git.EffortRefs    // the refresh's one effort-ref read (Spec D7), a var so tests run without a live repo
	probeLiveness   = liveness.Alive    // effort liveness probe shared with `mugicrew ps` (ADR 0025)

	// The held-land seams (Spec D6, FR 45), vars so the t test runs without a live bd or git.
	openBeads          = bd.OpenList             // the loop's frontier re-admit input, so t reaches a Task-held Checkpoint
	checkpointLandHold = loop.CheckpointLandHold // the land check, naming the blockers holding the land
)

var (
	dim        = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	dimmer     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	closedSty  = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Strikethrough(true)
	selMarker  = lipgloss.NewStyle().Foreground(lipgloss.Color("111")).Bold(true)
	tabOn      = lipgloss.NewStyle().Bold(true).Underline(true).Padding(0, 1)
	tabOff     = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Padding(0, 1)
	label      = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	warn       = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	aliveSty   = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	deadSty    = warn.Bold(true)
	blockedSty = deadSty
	pinkFg     = lipgloss.NewStyle().Foreground(lipgloss.Color("210"))
	blockerMk  = pinkFg.Bold(true)
	filterSty  = lipgloss.NewStyle().Reverse(true)
	cursorSty  = filterSty // a line input's cursor cell
	// The pause colours (ADR 0057 §1, ADR 0078): yellow is the loop's self-clearing
	// foreign-hold, red (warn) the loop's failing pause that needs the human, orange
	// an abandoned effort's possible live orphan (ADR 0082), cyan an integrate
	// stranded by a conflict (ADR 0104 §6), magenta the human's
	// manual pause, and a dim magenta a pause inherited from a manual-paused
	// ancestor (ADR 0084: manual, but not here); the tab-bar count uses the
	// foreign-hold yellow.
	pauseSty = map[string]lipgloss.Style{
		bd.PausedForeignHold:       lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		bd.PausedFailing:           warn,
		bd.PausedAbandoned:         lipgloss.NewStyle().Foreground(lipgloss.Color("208")),
		bd.PausedManual:            lipgloss.NewStyle().Foreground(lipgloss.Color("213")),
		bd.PausedIntegrateConflict: lipgloss.NewStyle().Foreground(lipgloss.Color("44")),
	}
	inheritSty = lipgloss.NewStyle().Foreground(lipgloss.Color("127"))
)

// tabKeyColor marks each tab's G/I/L key letter (ADR 0058 §2); clear of the kind
// palette and the status colours.
var tabKeyColor = lipgloss.Color("39")

// kindColors is one distinct xterm-256 hue per kind, no families (Spec
// Slice B, Palette A): clear of the status/warn colours and greys.
var kindColors = map[contract.Kind]lipgloss.Color{
	contract.KindSignal: "51", contract.KindGrilling: "75", contract.KindResearch: "80",
	contract.KindPrototype: "170", contract.KindChart: "141", contract.KindChartConflict: "161",
	contract.KindSpec: "147", contract.KindCheckpoint: "180", contract.KindBuild: "72",
	contract.KindReview: "108", contract.KindCritique: "167", contract.KindConflict: "160",
	contract.KindApproval: "41", contract.KindPivot: "130", contract.KindRollback: "99",
	contract.KindTask: "248",
}

// kindColor maps a kind to its tag color; unknown or empty kinds get grey 244.
func kindColor(k contract.Kind) lipgloss.Color {
	if c, ok := kindColors[k]; ok {
		return c
	}
	return "244"
}

// kindTag renders a bead's kind tag: a Container (Chart, Checkpoint) as a chip —
// black bold UPPERCASE on the kind colour, padded one space each side — so the
// chip means Container; every other bead as plain coloured UPPERCASE. Container-ness is the bd epic
// flag, the one predicate the graph, footer and preview all key on.
func kindTag(b *bd.Bead) string {
	k := b.Kind
	name := strings.ToUpper(string(k))
	if b.Container {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(kindColor(k)).Bold(true).Padding(0, 1).Render(name)
	}
	if name == "" {
		name = "—"
	}
	return lipgloss.NewStyle().Foreground(kindColor(k)).Bold(true).Render(name)
}

// stateGlyph renders a bead's status as a glyph (matches `bd show`), except a
// blocked bead — status=blocked or dep-blocked — reads as the red ⊘ so it is
// never mistaken for ready work, and an open paused bead reads as ‖
// in its cause's colour where ○ would be (ADR 0057 §1); with no own cause, an
// inherited pause reads as a dim magenta ‖ (ADR 0084).
func (m Model) stateGlyph(b *bd.Bead) string {
	if m.blocked(b) {
		return blockedSty.Render("⊘")
	}
	switch b.Status {
	case "closed":
		return dimmer.Render("✓")
	case "in_progress":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render("◐")
	default:
		if cs := bd.PausedCauses(*b); len(cs) > 0 && b.Status == "open" {
			return pauseSty[cs[0]].Render("‖") // manual's colour wins; ponytail: an unknown cause renders unstyled
		}
		if b.Status == "open" && m.inheritedPause(b) != "" {
			return inheritSty.Render("‖")
		}
		return dim.Render("○")
	}
}

// blocked reports whether b is status=blocked, or not closed with any Ordering
// blocker still open.
func (m Model) blocked(b *bd.Bead) bool {
	return b.Status == "blocked" || (b.Status != "closed" && len(m.openBlockers(b)) > 0)
}

// openBlockers returns b's non-closed Ordering targets in edge order. A target
// missing from the snapshot counts as open: beadByID's placeholder has no status.
// ponytail: linear beadByID per edge per rendered row; index by ID if the graph grows large.
func (m Model) openBlockers(b *bd.Bead) []*bd.Bead {
	var out []*bd.Bead
	for _, id := range b.BlockedBy {
		if t := m.beadByID(id); t.Status != "closed" {
			out = append(out, t)
		}
	}
	return out
}

// live is a row's effort liveness verdict (ADR 0032 §5).
type live int

const (
	na    live = iota // not in_progress, or HITL — no verdict (ADR 0032 §4)
	alive             // probe found reconcile/claude carrying the bead id
	dead              // in_progress AFK effort, probe found nothing
)

// liveOf derives a bead's verdict from the refresh's liveness map, gated by the
// same active-AFK-effort predicate `mugicrew ps` uses.
func (m Model) liveOf(b *bd.Bead) live {
	switch {
	case !loop.IsActiveAFKEffort(*b):
		return na
	case m.live[b.ID]:
		return alive
	default:
		return dead
	}
}

// livenessDot is the separate liveness column drawn right after the unchanged
// status glyph, so ◐○ reads "claimed, but nothing is running" (ADR 0032 §5).
func livenessDot(l live) string {
	switch l {
	case alive:
		return aliveSty.Render("●")
	case dead:
		return deadSty.Render("○")
	}
	return dimmer.Render("·")
}

// sessionLook is how a session renders (ADR 0038 §1–3, as amended by ADR 0067
// §2): its style, its liveness-column glyph, its detail-line word and its l verb.
// session.None has no entry; doneLook is the derived alive-and-bead-closed look,
// and waitLooks the amber looks of an alive session whose agent waits on the
// human (ADR 0099), its word your turn or needs approval.
type sessionLook struct {
	sty               lipgloss.Style
	glyph, word, verb string
}

var (
	sessionLooks = map[session.State]sessionLook{
		session.Alive:  {aliveSty, "●", "alive", "attach"},
		session.Exited: {deadSty, "✕", "exited", "resume"},
	}
	doneLook  = sessionLook{aliveSty.Bold(true), "●", "done — /exit", "attach"}
	waitSty   = pauseSty[bd.PausedForeignHold] // the foreign-hold amber
	waitLooks = map[session.Waiting]sessionLook{
		session.WaitTurn: {waitSty, "●", "your turn", "attach"},
		session.WaitAsk:  {waitSty, "●", "needs approval", "attach"},
	}
)

// lookOf is b's session look, and whether it has a session at all: an alive
// session whose bead is closed is done, waiting on /exit (ADR 0067 §2), else one
// whose agent waits on the human is waiting (ADR 0099). Precedence, lowest to
// highest: alive < waiting < done < exited. Derived each render, never latched,
// so a reopen or a reply reverts it.
func (m Model) lookOf(b *bd.Bead) (sessionLook, bool) {
	if m.done(b) {
		return doneLook, true
	}
	if l, ok := m.waitLook(b); ok {
		return l, true
	}
	l, ok := sessionLooks[m.sessions[b.ID].State]
	return l, ok
}

// waitLook is b's waiting look, and whether its alive, not-done session is waiting.
func (m Model) waitLook(b *bd.Bead) (sessionLook, bool) {
	s := m.sessions[b.ID]
	if s.State != session.Alive || m.done(b) {
		return sessionLook{}, false
	}
	l, ok := waitLooks[s.Waiting]
	return l, ok
}

// closing is an alive session whose hitl-run close is running (ADR 0108 §2); a
// dead pane is exited whatever @closing says.
func (m Model) closing(id string) bool {
	s := m.sessions[id]
	return s.State == session.Alive && s.Closing
}

// done is an alive session whose bead is closed (ADR 0067 §4: Done = bead closed).
func (m Model) done(b *bd.Bead) bool {
	return m.sessions[b.ID].State == session.Alive && b.Status == "closed"
}

// rowDot is a row's liveness column: a bead's session look when it has one (ADR
// 0067 §2: ● alive or done, red ✕ exited), else its effort verdict.
func (m Model) rowDot(b *bd.Bead) string {
	if l, ok := m.lookOf(b); ok {
		return l.sty.Render(l.glyph)
	}
	return livenessDot(m.liveOf(b))
}

// sessionLine is the detail header's session field value (ADR 0038 §6, as
// amended by ADR 0039 §3), and whether the bead has a session at all.
func (m Model) sessionLine(b *bd.Bead) (string, bool) {
	l, ok := m.lookOf(b)
	if !ok {
		return "", false
	}
	s := m.sessions[b.ID]
	word := l.sty.Render(l.word)
	switch {
	case s.State == session.Exited && s.Signal != "":
		return word + dim.Render(" (signal="+s.Signal+")"), true
	case s.State == session.Exited:
		return word + dim.Render(fmt.Sprintf(" (rc=%d)", s.RC)), true
	case s.Worktree == "": // a repo-root leaf runs in no worktree
		return word, true
	}
	return word + dim.Render(" · "+s.Worktree), true
}

// rowSuffix is a row's trailing chrome: its age, then `done — /exit` on a done
// session's row so the human sees which sessions wait on them (ADR 0067 §2), or
// the waiting word on a waiting session's row (ADR 0099), or
// `launching…` while its session start/resume is in flight (Spec FR9),
// or a pending bd write's verb (`pausing…`, ADR 0107 §1), or `closing…` on a
// Closing session's row (ADR 0108 §3), in place of the done,
// waiting and idle-Surgery words — a map lookup, never a subprocess (ADR 0101).
// An armed session reads `auto-exit in 2m…` below those three and above the done
// and waiting words (Spec D7).
// An idle Surgery or Task — in_progress with no session — reads `idle — l to
// resume` in place of the stranded hitl claim (Spec D10, FR43).
func (m Model) rowSuffix(b *bd.Bead) string {
	if m.launching[b.ID] {
		return ageSuffix(b) + "  " + aliveSty.Render("launching…")
	}
	if verb, ok := m.pending[b.ID]; ok {
		return ageSuffix(b) + "  " + aliveSty.Render(verb+"…")
	}
	if m.closing(b.ID) {
		return ageSuffix(b) + "  " + aliveSty.Render("closing…")
	}
	if s := m.sessions[b.ID]; s.State == session.Alive && !s.AutoExit.IsZero() {
		return ageSuffix(b) + "  " + aliveSty.Render("auto-exit in "+coarseLeft(time.Until(s.AutoExit))+"…")
	}
	if m.done(b) {
		return ageSuffix(b) + "  " + doneLook.sty.Render(doneLook.word)
	}
	if l, ok := m.waitLook(b); ok {
		return ageSuffix(b) + "  " + l.sty.Render(l.word)
	}
	if _, ok := m.lookOf(b); !ok && (b.Kind == contract.KindSurgery || b.Kind == contract.KindTask) && b.Status == "in_progress" {
		return "  " + dimmer.Render("idle — l to resume")
	}
	return ageSuffix(b)
}

// ageSuffix is the dim trailing age-since-claim on in_progress rows (hitl for a
// human claim). It never colours the verdict — the dot alone tells a fresh crash
// from a slow live effort.
func ageSuffix(b *bd.Bead) string {
	if b.Status != "in_progress" {
		return ""
	}
	if isHITL(b.Kind) {
		return "  " + dimmer.Render("hitl")
	}
	return "  " + dim.Render(HumanAge(b.Started))
}

// coarseLeft is an Auto-exit countdown at row grain: whole minutes, then seconds.
func coarseLeft(d time.Duration) string {
	if d >= time.Minute {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%ds", max(0, int(d.Seconds())))
}

// livenessLine is the detail pane's liveness field value, in words.
func livenessLine(l live, b *bd.Bead) string {
	claimed := ""
	if !b.Started.IsZero() {
		claimed = dim.Render(" · claimed " + HumanAge(b.Started) + " ago")
	}
	switch l {
	case alive:
		return aliveSty.Render("alive") + claimed
	case dead:
		return deadSty.Render("dead") + dim.Render(" — no reconcile/claude process") + claimed
	}
	return dim.Render("— (HITL: no verdict)")
}

// HumanAge renders a coarse age (like the prototype's 5h / 2d).
func HumanAge(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// Responsive render-path thresholds, from the Mobile-SSH viewport
// geometry research. Below the floor any layout is unreadable; below the
// breakpoint the two-pane split starves the detail, so collapse to one pane.
const (
	floorCols  = 40
	floorRows  = 12
	breakpoint = 84 // ADR 0072 §5: the 60-col list cap would starve the detail below this
	masterCap  = 60 // ADR 0072 §4: room for Search rows' titles
)

// singlePane reports whether the terminal is below the two-pane breakpoint, so
// the layout collapses to the single focused pane. Derived live from m.w — no
// cached mode field, so a resize flips it on the next render.
func (m Model) singlePane() bool { return m.w < breakpoint }

// tooSmall is the below-floor notice: naming the required and actual
// geometry beats a layout starved into garbage. View reads m.w/m.h live, so
// growing back past the floor restores the layout on the next render (FR 11,12).
func (m Model) tooSmall() string {
	return m.notice("terminal too small", fmt.Sprintf("need %dx%d, have %dx%d — enlarge to continue", floorCols, floorRows, m.w, m.h))
}

// notice is a full-screen message framed like any view (Spec FR3): both
// parts word-wrapped to the terminal width, then fitted to exactly m.h lines —
// a head too long to fit loses its tail, never the actionable hint.
func (m Model) notice(head, hint string) string {
	wrap := lipgloss.NewStyle().Width(m.w)
	top, rest, _ := strings.Cut(wrap.Inherit(warn).Render(head), "\n")
	return fitFrame(top, rest+"\n", wrap.Inherit(dim).Render(hint), m.w, m.h)
}

func (m Model) View() string {
	if m.loadErr != nil {
		return m.notice("bd read failed: "+m.loadErr.Error(), "press r to retry, q to quit")
	}
	if m.w < floorCols || m.h < floorRows {
		return m.tooSmall()
	}
	m.chrome = m.chromeW() // masterWidth runs several times a frame; measure the rows once
	bodyH := m.paneHeight()
	var body string
	switch {
	case m.help:
		body = m.helpView(bodyH)
	case m.tab == tabGraph:
		body = m.graphView(bodyH)
	case m.tab == tabInbox:
		body = m.inboxView(bodyH)
	case m.tab == tabSearch:
		body = m.searchView(bodyH)
	case m.tab == tabHistory:
		body = m.historyView(bodyH)
	case m.tab == tabStats:
		body = m.statsView(bodyH)
	case m.tab == tabSettings:
		body = m.settingsView()
	default:
		body = m.logsView(bodyH)
	}
	return fitFrame(m.tabBar(), body, m.helpLine(), m.w, m.h)
}

// fitFrame assembles the frame at exactly h lines, none wider than w (Spec
// FR3): the body is clipped to what the help lines leave, the frame padded
// at the bottom. A frame taller than the terminal makes bubbletea drop its top
// lines, and an over-wide line wraps in the terminal — either desyncs the
// renderer's line diff and leaves stale rows on scroll.
// ponytail: a safety net — the panes already fit, so clipping here only trims a
// help line that grew (status, create form) or a long notice. It assumes a
// one-line tab bar; split tabBar on \n like help if it ever wraps.
func fitFrame(tabBar, body, help string, w, h int) string {
	helpL := strings.Split(help, "\n")
	bodyL := strings.Split(body, "\n")
	if n := max(0, h-1-len(helpL)); len(bodyL) > n {
		bodyL = bodyL[:n]
	}
	lines := append(append([]string{tabBar}, bodyL...), helpL...)
	for len(lines) < h {
		lines = append(lines, "")
	}
	lines = lines[:h]
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, w, "")
	}
	return strings.Join(lines, "\n")
}

// tabBar is `Graph Inbox(N) Search History Logs  · N live · M waiting · N‖ · loop <state>` (ADR 0051 §4–5,
// ADR 0057 §4, ADR 0072), Inbox(N) counting open HITL beads, M waiting the alive waiting
// sessions whose bead isn't closed (ADR 0099) and N‖ every non-closed paused
// one (a ◐ or ⊘ row is paused too) except one whose run-now retry (t on an
// integrate-conflict, failing or foreign-hold pause) is in this TUI's m.runs,
// each shown only when > 0. Too wide for m.w first drops the tab padding, then the live
// and waiting counts, then tightens the separators to spaces, then drops the loop's @rev, then
// shrinks every tab but the active one to its key letter (Inbox keeps its (N));
// the pause and loop state segments stay. The Plan usage warning (usageWarning)
// trails on every tab, outside that fitting: fitFrame clips it at the narrowest widths.
func (m Model) tabBar() string {
	openHITL := len(inboxList(m.beads, nil)) // nil sessions: open beads only, as the label says
	count := fmt.Sprintf("(%d)", openHITL)
	names := []string{"Graph", "Inbox" + count, "Search", "History", "Logs", "#Stats", "*Settings"} // # and * are their tabs' keys, accented like the letters
	short := []string{"G", "I" + count, "S", "H", "L", "#", "*"}
	for i := range names {
		if m.filtered(tab(i)) { // a filtered tab is marked from any tab (Spec FR8); Search never is
			names[i] += "▼"
			short[i] += "▼"
		}
	}
	if m.settingsStaged() { // an unsaved Settings edit is marked from any tab, where filtered tabs show ▼ (Spec FR17)
		names[tabSettings] += "•"
		short[tabSettings] += "•"
	}
	labels := names // what render draws: the full names, or the floor's key letters
	// Run-now retries in flight, judged by the pressed snapshot's causes (runNowLabel's retry / retry hold).
	retrying := map[string]bool{}
	for _, r := range m.runs {
		if slices.ContainsFunc([]string{bd.PausedIntegrateConflict, bd.PausedFailing, bd.PausedForeignHold}, func(c string) bool { return bd.PausedBy(r.Bead, c) }) {
			retrying[r.Bead.ID] = true
		}
	}
	paused := 0
	for _, b := range m.beads {
		if b.Status != "closed" && bd.Paused(b) && !retrying[b.ID] {
			paused++
		}
	}
	waiting := 0
	for i := range m.beads {
		if _, ok := m.waitLook(&m.beads[i]); ok {
			waiting++
		}
	}
	render := func(pad, live bool, sep string, rev bool) string {
		var s string
		for i, n := range labels {
			on, off := tabOn, tabOff
			if !pad {
				on, off = on.UnsetPadding(), off.UnsetPadding()
				if i > 0 {
					s += " "
				}
			}
			st := off
			if tab(i) == m.tab {
				st = on
			}
			// The key letter in the accent, no extra columns (ADR 0058 §2).
			p := strings.Repeat(" ", st.GetPaddingLeft())
			st = st.UnsetPadding()
			s += p + st.Foreground(tabKeyColor).Render(n[:1]) + st.Render(n[1:]) + p
		}
		if live {
			s += dim.Render(fmt.Sprintf("%s%d live", sep, session.Live(m.sessions)))
			if waiting > 0 {
				s += dim.Render(sep) + waitSty.Render(fmt.Sprintf("%d waiting", waiting))
			}
		}
		if paused > 0 {
			s += dim.Render(sep) + pauseSty[bd.PausedForeignHold].Render(fmt.Sprintf("%d‖", paused))
		}
		return s + dim.Render(sep) + m.loopSegment(rev)
	}
	s := render(true, true, " · ", true)
	if lipgloss.Width(s) > m.w {
		s = render(false, true, " · ", true)
	}
	if lipgloss.Width(s) > m.w {
		s = render(false, false, " · ", true)
	}
	if lipgloss.Width(s) > m.w {
		s = render(false, false, " ", true)
	}
	if lipgloss.Width(s) > m.w {
		s = render(false, false, " ", false)
	}
	if lipgloss.Width(s) > m.w { // ADR 0072: the 7-tab bar at the 40-col floor
		labels = slices.Clone(short)
		labels[m.tab] = names[m.tab]
		// ponytail: the active tab keeps its full name (ADR 0072 §3), so with a two-digit Inbox
		// and pause count any tab but Logs on a draining loop, and every tab on a restarting
		// one, still overflows 40 cols and fitFrame clips the loop segment's tail; a shorter
		// loop wording is the upgrade path.
		s = render(false, false, " ", false)
	}
	s += m.usageWarning()
	if m.tab == tabLogs && m.logSkipped > 0 {
		s += dim.Render(" · ") + warn.Render(fmt.Sprintf("%d unparsed lines", m.logSkipped))
	}
	if m.tab == tabLogs {
		for _, f := range m.logFilters() {
			s += dim.Render(" · ") + pinkFg.Render(f)
		}
	}
	return s
}

// inlineStatus trails the title/raw editor's keys, or the create form's head, with
// the status its commit set ("updating A…", "filing new Signal…", "edit: <err>"); e, E,
// n, u and R open their editor with the status cleared.
func (m Model) inlineStatus() string {
	if m.status == "" {
		return ""
	}
	return dim.Render("  ·  ") + m.status
}

func (m Model) helpLine() string {
	switch m.editMode {
	case editPick:
		return warn.Render("edit "+m.editID+": ") + "(t)itle / (d)escription" + dim.Render("  ·  esc cancel")
	case editTitle:
		return renderKeys([]footerKey{{"enter", "commit"}, {"esc", "cancel"}}) + m.inlineStatus() + "\n" +
			warn.Render("title "+m.editID+": ") + m.editBuf.render()
	case editRaw:
		return renderKeys([]footerKey{{"enter", "run"}, {"esc", "cancel"}}) + m.inlineStatus() + "\n" +
			warn.Render("bd update "+m.editID+" ") + m.editBuf.render()
	case editCreate:
		return m.createFormLine()
	case editSearch:
		return renderKeys([]footerKey{{"enter", "apply"}, {"esc", "cancel"}}) + "\n" +
			warn.Render("/") + m.editBuf.render()
	case editSearchText: // the text itself shows in Search's header
		return renderKeys([]footerKey{{"enter", "keep"}, {"esc", "cancel"}})
	case editSearchPick:
		return renderKeys([]footerKey{{"enter", "pick"}, {"esc", "cancel"}})
	case editConfirmLaunch:
		verb := "launch"
		if m.launchBg { // b's pending launch: y won't switch (ADR 0100)
			verb = "launch in background"
		}
		return renderKeys([]footerKey{{"y", verb}, {"n/esc", "cancel"}}) + "\n" + m.status
	case editConfirmDrop:
		return renderKeys([]footerKey{{"y", "drop"}, {"n/esc", "cancel"}}) + "\n" + m.status
	case editConfirmAbort:
		return renderKeys([]footerKey{{"y", "abort"}, {"n/esc", "cancel"}}) + "\n" + m.status
	case editConfirmOrphan:
		return m.status // the ADR 0041 §3 wording carries its own keys
	case editConfirmForceStop:
		return renderKeys([]footerKey{{"y", "force"}, {"n/esc", "cancel"}}) + "\n" + m.status
	case editConfirmQuit:
		return renderKeys([]footerKey{{"y", "quit"}, {"n/esc", "cancel"}}) + "\n" + m.status
	case editRejectReason:
		return renderKeys([]footerKey{{"enter", "next"}, {"esc", "cancel"}}) + "\n" +
			warn.Render("reject "+m.editID+", reason (blank: "+rejectDefault+"): ") + m.editBuf.render()
	case editConfirmReject:
		return renderKeys([]footerKey{{"y", "reject"}, {"n/esc", "cancel"}}) + "\n" + m.status
	}
	keys := m.footer()
	if m.help {
		keys = renderKeys([]footerKey{{"?/esc/q", "close"}, {"j/k", "scroll"}})
	}
	if m.status != "" {
		return keys + "\n" + m.status
	}
	return keys
}

// footerKey is one footer command: a key glyph and an optional label.
type footerKey struct{ k, label string }

func renderKeys(ks []footerKey) string { return joinKeys(ks, " · ") }

func joinKeys(ks []footerKey, sep string) string {
	out := make([]string, len(ks))
	for i, k := range ks {
		out[i] = selMarker.Render(k.k)
		if k.label != "" {
			out[i] += " " + dim.Render(k.label)
		}
	}
	return strings.Join(out, dim.Render(sep))
}

// footer is the single command surface (ADR 0026): <bead keys> │ <view keys>,
// only the keys that apply to the tab × layout/focus × selection. Too wide for
// m.w first drops b, then u, then Graph's n, then +/- (each kept only when the whole
// footer fits untightened), then
// the right-most view key other than q, then p's hint. The drops are decided without ?, which then goes in before q
// and, if that overflows, tightens every separator to a space as the tab bar
// does: ? is never dropped and costs no other hint (ADR 0058 §5). Keys whose
// hint is hidden still work — Update is the authority.
// ponytail: tight, the widest bead set (bar +/-) + ? q (~35 cols) fits the 40-col floor,
// so no truncation fallback until a longer bead key appears.
func (m Model) footer() string {
	bead := m.beadKeys(m.selectedBead())
	view := m.viewKeys()
	sep := " · "
	render := func() string {
		if len(bead) == 0 {
			return joinKeys(view, sep)
		}
		return joinKeys(bead, sep) + dim.Render(" │ ") + joinKeys(view, sep)
	}
	// b, then u, then Graph's n, then +/-, yield first: each shows only when the
	// whole footer fits untightened (b first: Spec FR18; u next: Spec
	// FR44; Graph's n is new there).
	fits := func() bool {
		full := append(slices.Clone(view[:len(view)-1]), footerKey{"?", ""}, view[len(view)-1])
		return lipgloss.Width(joinKeys(bead, sep)+" │ "+joinKeys(full, sep)) <= m.w
	}
	if !fits() {
		bead = slices.DeleteFunc(bead, func(k footerKey) bool { return k.k == "b" })
	}
	if !fits() {
		view = slices.DeleteFunc(view, func(k footerKey) bool { return k.k == "u" })
	}
	if !fits() && m.tab == tabGraph {
		view = slices.DeleteFunc(view, func(k footerKey) bool { return k.k == "n" })
	}
	if !fits() {
		bead = slices.DeleteFunc(bead, func(k footerKey) bool { return k.k == "+/-" })
	}
	s := render()
	for lipgloss.Width(s) > m.w && len(view) > 1 {
		view = append(view[:len(view)-2], view[len(view)-1])
		s = render()
	}
	// A sessioned bead's keys + p overflow the 40-col floor (ADR 0057 §6 amendment).
	if lipgloss.Width(s) > m.w {
		bead = slices.DeleteFunc(bead, func(k footerKey) bool { return k.k == "p" })
	}
	view = append(view[:len(view)-1], footerKey{"?", ""}, view[len(view)-1])
	if s = render(); lipgloss.Width(s) > m.w {
		sep = " "
		s = render()
	}
	return s
}

// beadKeys are the commands acting on the selection, regardless of focus (ADR
// 0026 table). nil selection → none.
func (m Model) beadKeys(b *bd.Bead) []footerKey {
	if b == nil {
		return nil
	}
	if m.tab == tabSearch { // no bead actions on Search (Spec D5) bar z (Spec FR7)
		if rejectable(*b) {
			return []footerKey{{"z", "reject"}}
		}
		return nil
	}
	var ks []footerKey
	s := m.sessions[b.ID]
	switch {
	case s.Has():
		l, _ := m.lookOf(b)
		ks = append(ks, footerKey{"l", l.verb})
	case isHITL(b.Kind) && !b.Container:
		if m.blocked(b) {
			ks = append(ks, footerKey{"l", "launch⚠"})
		} else if b.Status == "open" || b.Status == "in_progress" {
			ks = append(ks, footerKey{"l", "launch"})
		}
	}
	if len(ks) > 0 && ks[0].label != "attach" { // b starts only what l would start (ADR 0100)
		ks = append(ks, footerKey{"b", "bg"})
	}
	if s.Has() {
		ks = append(ks, footerKey{"x", "drop"})
	} else if m.liveOf(b) == alive {
		ks = append(ks, footerKey{"x", "abort"})
	}
	if l := m.runNowLabel(b); m.tab == tabGraph && l != "" && !m.blocked(b) {
		ks = append(ks, footerKey{"t", l})
	}
	if m.tab == tabGraph && b.Status != "closed" {
		ks = append(ks, footerKey{"p", pauseLabel(*b)})
	}
	if b.Status != "closed" {
		ks = append(ks, footerKey{"+/-", "pri"})
	}
	if rejectable(*b) {
		ks = append(ks, footerKey{"z", "reject"})
	}
	ks = append(ks, footerKey{"e", "edit"})
	if m.tab == tabGraph {
		ks = append(ks, footerKey{"L", "logs"})
	}
	return ks
}

// viewKeys are the tab's view commands in drop priority order (ADR 0026). The tab
// hint is single-pane only; single-pane detail focus hides space/h (they act on
// the unseen list); two-pane is focus-independent.
func (m Model) viewKeys() []footerKey {
	var ks []footerKey
	listHidden := m.singlePane() && m.focusDetail
	if m.singlePane() && m.tab != tabHistory && m.tab != tabStats && m.tab != tabSettings { // History is one list, Stats and Settings one view: no detail
		if m.focusDetail {
			ks = append(ks, footerKey{"tab", "list"})
		} else {
			ks = append(ks, footerKey{"tab", "detail"})
		}
	}
	if m.tab == tabInbox || m.tab == tabGraph {
		ks = append(ks, footerKey{"n", "signal"}, footerKey{"u", "surgery"})
	}
	if m.tab == tabInbox {
		ks = append(ks, m.filterKeys()...)
	} else if m.tab == tabSearch {
		ks = append([]footerKey{{"/", "search"}}, ks...) // / first: it drops last
		if !listHidden {
			ks = append(ks, footerKey{"enter", ""})
		}
	} else if m.tab == tabLogs {
		ks = append(ks, m.logKeys()...)
	} else if m.tab == tabStats {
		ks = append(ks, footerKey{"w", "window"}, footerKey{"b", "group"}, footerKey{"f5", "refresh"})
	} else if m.tab == tabSettings {
		ks = append(ks, footerKey{"j/k", ""}, footerKey{"+/-", "change"})
		if m.settingsStaged() {
			ks = append(ks, footerKey{"enter", "save"}, footerKey{"esc", "discard"})
		}
	} else if m.tab == tabHistory {
		ks = append(ks, footerKey{"j/k", ""})
		if r, ok := m.histSelection(); ok {
			ks = append(ks, footerKey{"enter/G", "reveal"})
			if r.kind == histCheckpoint && r.badge == "" {
				ks = append(ks, footerKey{"R", "roll back"})
			}
		}
	} else if m.tab == tabGraph && !listHidden {
		rows := navRows(m.groups, m.collapsed)
		if i := indexRow(rows, m.selGraph); i >= 0 && rows[i].header {
			ks = append(ks, footerKey{"space", "fold"})
		}
		if m.hideClosedRoots {
			ks = append(ks, footerKey{"h", "show-closed"})
		} else {
			ks = append(ks, footerKey{"h", "hide-closed"})
		}
	}
	ks = append(ks, footerKey{"o", m.loopLabel()})
	// Graph's and Logs' filter keys sit right of o: their fuller footers drop them before the loop toggle.
	if m.tab == tabGraph && !listHidden || m.tab == tabLogs {
		ks = append(ks, m.filterKeys()...)
	}
	if m.tab == tabSearch { // right of o too, so s, then c, then f, then G reveal drop first (Spec FR29)
		if m.selectedBead() != nil {
			ks = append(ks, footerKey{"G", "reveal"})
		}
		ks = append(ks, footerKey{"f", "status"}, footerKey{"c", "kind"}, footerKey{"s", "sort"})
	}
	return append(ks, footerKey{"q", ""})
}

// filterKeys are the tab's filter keys (Spec FR11): f and /, plus esc
// while it has a filter to clear.
func (m Model) filterKeys() []footerKey {
	ks := []footerKey{{"f", "kind"}, {"/", "search"}}
	if m.filtered(m.tab) {
		ks = append(ks, footerKey{"esc", "clear"})
	}
	return ks
}

// createFormLine renders the 2-field new-Signal form: title and
// description, the cursor drawn in the focused field, plus the key legend.
func (m Model) createFormLine() string {
	tc, dc := m.createTitle.render(), m.createDesc.text
	if m.createField == 1 {
		tc, dc = m.createTitle.text, m.createDesc.render()
	}
	head, body := "new Signal", "description "
	if m.createFor != "" {
		head, body = "Rollback of "+m.createFor, "evidence    "
	} else if m.createSurgery {
		head, body = "new Surgery", "problem     "
	}
	return warn.Render(head) + dim.Render("  ·  tab switch field · enter file · esc cancel") + m.inlineStatus() + "\n" +
		label.Render("title       ") + tc + "\n" +
		label.Render(body) + dc
}

// Shortest readable truncations in the confirm prompts (blocked-launch, force-stop).
const (
	minPromptTitle   = 6
	minBlockerTitle  = 4
	markedBlockedFmt = `"%s" is marked blocked (no open deps) — launch anyway?`
	blockedByFmt     = `launch "%s" anyway? blocked by: `
)

// confirmLaunchPrompt is the blocked-launch confirm prompt (ADR 0026): the open
// blockers by truncated title, as many as fit in w columns, the rest as +N. The
// bead's title gets half the room the wording leaves, the blockers the rest; on a
// wide terminal nothing is cut. With no open blockers the bead is status=blocked by hand.
// ponytail: the ADR's fixed wording alone is ~50 cols, so at the 40-col floor the
// marked-blocked variant still wraps; the blocker variant fits from ~40 cols up.
func confirmLaunchPrompt(title string, blockers []*bd.Bead, w int) string {
	if len(blockers) == 0 {
		return fitTitle(markedBlockedFmt, title, w)
	}
	room := (w - lipgloss.Width(fmt.Sprintf(blockedByFmt, ""))) / 2
	titles := make([]string, len(blockers))
	for i, b := range blockers {
		titles[i] = b.Title
	}
	return foldTitles(fmt.Sprintf(blockedByFmt, trunc(title, max(minPromptTitle, room))), titles, "", w)
}

// foldTitles appends titles to s, comma-separated and each cut to the w columns
// left before end, then end; once no readable title fits, it and the rest fold
// into +N. Shared by the confirm prompts that list titles.
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
		if room < minBlockerTitle { // no room for a readable title: fold it and the rest into +N
			return strings.TrimSuffix(s, " ") + fmt.Sprintf(" +%d", len(titles)-i) + end
		}
		s += sep + trunc(t, room)
	}
	return strings.TrimSuffix(s, " ") + end
}

// dropPromptFmt is the drop confirm (ADR 0038 §4).
const dropPromptFmt = `drop "%s"? kills the session and discards uncommitted changes in its worktree`

// rejectPromptFmt is the z reject confirm (Spec FR2).
const rejectPromptFmt = `reject "%s"? closes it: %s`

// confirmRejectPrompt is the reject confirm with the title cut to what w leaves.
func confirmRejectPrompt(title, reason string, w int) string {
	return fitTitle(rejectPromptFmt, title, w, reason)
}

// confirmDropPrompt is the drop confirm with the title cut to what w leaves.
func confirmDropPrompt(title string, w int) string {
	return fitTitle(dropPromptFmt, title, w)
}

// abortPromptFmt is the AFK abort confirm (ADR 0083): what y does, in a line.
const abortPromptFmt = `abort "%s"? pauses it (paused:manual) and SIGTERMs its running effort`

// confirmAbortPrompt is the abort confirm with the title cut to what w leaves.
func confirmAbortPrompt(title string, w int) string {
	return fitTitle(abortPromptFmt, title, w)
}

// orphanPromptFmt is the orphan confirm (ADR 0041 §3).
const orphanPromptFmt = `"%s": branch held by an orphaned worktree %s (crashed session) — y drop it and launch · n cancel`

// confirmOrphanPrompt is the orphan confirm with the title cut to what w leaves;
// the path is never cut, it is what the human is asked to discard.
func confirmOrphanPrompt(title, wt string, w int) string {
	return fitTitle(orphanPromptFmt, title, w, wt)
}

// fitTitle renders a prompt format whose first %s is the title, the title cut to
// the w columns the rest of the wording leaves (never below minPromptTitle).
func fitTitle(format, title string, w int, rest ...any) string {
	room := w - lipgloss.Width(fmt.Sprintf(format, append([]any{""}, rest...)...))
	return fmt.Sprintf(format, append([]any{trunc(title, max(minPromptTitle, room))}, rest...)...)
}

// paneHeight is the fixed height of both panes (spec §9.4): the terminal less
// the tab bar + help chrome, floored so the split never collapses.
func (m Model) paneHeight() int {
	h := m.h - 4
	if h < 3 {
		h = 3
	}
	return h
}

// detailInnerW is the text width available inside the detail pane: its column
// (splitPane: m.w-mw-2) less the Padding(0,1) on each side. The description is
// wrapped at exactly this width so the pane never re-wraps and grows.
func (m Model) detailInnerW() int {
	if m.singlePane() { // full width less a 2-col gutter (mirrors the two-pane Padding(0,1) budget)
		if w := m.w - 2; w > 1 {
			return w
		}
		return 1
	}
	w := m.w - m.masterWidth() - 4
	if w < 1 {
		w = 1
	}
	return w
}

// windowRows wraps body to width, splits it into visual rows, and returns the
// height-row window starting at offset — clamped to [0, len-height]. A body that
// fits in height returns all its rows unchanged (no blank overscroll). Pure so
// the scroll math is unit-tested without a live terminal.
func windowRows(body string, width, height, offset int) []string {
	if width < 1 {
		width = 1
	}
	rows := strings.Split(lipgloss.NewStyle().Width(width).Render(body), "\n")
	if height < 1 {
		return nil
	}
	if len(rows) <= height {
		return rows
	}
	max := len(rows) - height
	if offset > max {
		offset = max
	}
	if offset < 0 {
		offset = 0
	}
	return rows[offset : offset+height]
}

// followOffset keeps the selected row visible inside a height-row window over
// `total` rows, centring it (clamped so the ends pin to top/bottom). Pure so the
// scroll-follow math unit-tests without a live terminal.
func followOffset(total, height, sel int) int {
	if total <= height {
		return 0
	}
	off := sel - height/2
	if off < 0 {
		off = 0
	}
	if max := total - height; off > max {
		off = max
	}
	return off
}

// masterWidth splits the panes ~40/60, clamped so neither collapses. The floor
// is the widest row's real chrome plus a 1-col title (chromeW), so a 3-digit
// count or a 1000d age widens the column instead of being cut (Spec FR2).
func (m Model) masterWidth() int {
	floor := m.chrome
	if floor == 0 {
		floor = m.chromeW()
	}
	w := min(max(m.w*2/5, floor), max(masterCap, floor))
	return min(w, m.w-20)
}

// chromeW is the widest master row (Graph, Inbox and Search) less its title, plus a
// 1-col title: the column every row needs so only titles are ever cut.
func (m Model) chromeW() int {
	blockers := m.selBlockers()
	w := 0
	for _, r := range navRows(m.groups, m.collapsed) {
		pre, _, suf := m.masterParts(r)
		w = max(w, lipgloss.Width(m.gutter(r, blockers)+pre+suf)+1)
	}
	for _, b := range inboxList(m.beads, m.sessions) {
		pre, suf := m.inboxParts(&b)
		w = max(w, lipgloss.Width(pre+suf)+1)
	}
	list, _ := m.searchRows()
	for _, b := range list {
		pre, suf := m.searchParts(&b)
		w = max(w, lipgloss.Width(pre+suf)+1)
	}
	return w
}

// masterColW is the width the master list renders to: full-width when the single
// pane shows the master, else the two-pane master column.
func (m Model) masterColW() int {
	if m.singlePane() {
		return m.w
	}
	return m.masterWidth()
}

// layout renders the body for the current geometry. Below the
// breakpoint it collapses to the single focused pane at full width — the detail
// when focusDetail is set (Tab flips it, spec §9.4), else the master follow-
// window; at or above it keeps today's two-pane split. Reads m.w live, so
// crossing the breakpoint switches modes on the next render with no cached mode.
// The below-floor guard is upstream in View. A non-empty bar (filterBar, or
// Search's two header lines) is pinned above the master rows, outside their
// scroll window, so it never scrolls away.
func (m Model) layout(bar string, masterLines []string, sel int, detail string, height int) string {
	// detailInnerW is the width the detail was wrapped at — window at the same
	// width so the pane never re-wraps and grows (the helper's whole invariant).
	if m.singlePane() && m.focusDetail {
		return strings.Join(windowRows(detail, m.detailInnerW(), height, 0), "\n")
	}
	rowsH := height
	var head []string
	if bar != "" {
		head = strings.Split(bar, "\n")
		rowsH -= len(head)
	}
	off := followOffset(len(masterLines), rowsH, sel)
	master := strings.Join(masterLines, "\n")
	rows := windowRows(master, m.masterColW(), rowsH, off)
	rows = append(head, rows...)
	if m.singlePane() {
		return strings.Join(rows, "\n")
	}
	return m.splitPane(rows, detail, height)
}

// borderColor accents the focused pane's separator so `tab` (focus master ↔
// detail, spec §9.4) has a visible effect.
func borderColor(focused bool) lipgloss.Color {
	if focused {
		return "111"
	}
	return "240"
}

// splitPane lays the two-pane master/detail view: a bordered master (its border
// accented when it holds focus, spec §9.4) beside the detail. Shared by both
// tabs. The unfocused pane renders Faint (Spec FR5), its inner colours
// and bold stripped first — their resets would end the faint mid-line and bold
// cancels it — so the dim reads across kind tags and glyphs too.
func (m Model) splitPane(masterLines []string, detail string, height int) string {
	mw := m.masterWidth()
	masterBody := strings.Join(masterLines, "\n")
	// ponytail: stripping drops chip backgrounds in the dim pane; re-emit Faint after each inner reset if colour must survive.
	if m.focusDetail {
		masterBody = ansi.Strip(masterBody)
	} else {
		detail = ansi.Strip(detail)
	}
	master := lipgloss.NewStyle().Width(mw).Height(height).Faint(m.focusDetail).
		BorderStyle(lipgloss.NormalBorder()).BorderRight(true).BorderForeground(borderColor(!m.focusDetail)).
		Render(masterBody)
	det := lipgloss.NewStyle().Width(m.w-mw-2).Height(height).MaxHeight(height).Padding(0, 1).Faint(!m.focusDetail).Render(detail)
	return lipgloss.JoinHorizontal(lipgloss.Top, master, det)
}

func (m Model) graphView(height int) string {
	lines, sel := m.graphLines()
	f := m.filters[tabGraph]
	bar := m.tabFilterBar(tabGraph, len(slices.DeleteFunc(groupBeads(m.groups), func(b bd.Bead) bool { return !f.matchBead(b) })))
	if bar != "" && len(lines) == 0 {
		lines = []string{dim.Render(noMatches)}
	}
	return m.layout(bar, lines, sel, m.detailView(m.selectedBead(), sel, len(lines), height), height)
}

// graphLines is the Graph master list as rendered, one fitted line per row, and
// the selection's line index.
func (m Model) graphLines() ([]string, int) {
	mw := m.masterColW()
	blockers := m.selBlockers()
	var lines []string
	sel := 0
	for _, r := range navRows(m.groups, m.collapsed) {
		if r.sel == m.selGraph {
			sel = len(lines)
		}
		g := m.gutter(r, blockers)
		lines = append(lines, fitRow(g+m.masterRow(r, mw-lipgloss.Width(g), blockers[r.beadID]), mw))
	}
	return lines, sel
}

// selBlockers is the set of the selection's open blockers, pinked in the Graph.
func (m Model) selBlockers() map[string]bool {
	blockers := map[string]bool{}
	if b := m.selectedBead(); b != nil {
		for _, t := range m.openBlockers(b) {
			blockers[t.ID] = true
		}
	}
	return blockers
}

// gutter is a Graph row's 1-column blocker mark: a pink ! on an open
// blocker of the selection, the count a folded group hides, else a space.
// Blocked-ness is the ⊘ glyph's job, never the gutter's.
func (m Model) gutter(r row, blockers map[string]bool) string {
	if blockers[r.beadID] {
		return blockerMk.Render("!")
	}
	if !r.header || !m.collapsed[r.collapseKey] {
		return " "
	}
	n := 0
	for _, g := range m.groups {
		if g.key == r.collapseKey {
			for _, b := range g.members {
				if blockers[b.ID] {
					n++
				}
			}
			break
		}
	}
	if n == 0 {
		return " "
	}
	// ponytail: 10+ hidden blockers widen the gutter by a column; clamp if that ever happens.
	return blockerMk.Render(fmt.Sprint(n))
}

// masterRow renders one Graph master line (after its gutter): a group header
// (with fold caret) or an indented member leaf; hot pinks the title of an open
// blocker of the selection.
func (m Model) masterRow(r row, width int, hot bool) string {
	pre, title, suf := m.masterParts(r)
	if r.beadID == "" { // unparented header: no title to fit
		return pre
	}
	title = fitRowTitle(title, width, pre, suf)
	if !r.header && m.beadByID(r.beadID).Status == "closed" {
		title = closedSty.Render(title)
	} else if hot {
		title = pinkFg.Render(title)
	}
	return pre + title + suf
}

// masterParts splits a Graph master line into the chrome before and after its
// (unfitted, unstyled) title.
func (m Model) masterParts(r row) (pre, title, suf string) {
	marker := "  "
	if r.sel == m.selGraph {
		marker = selMarker.Render("❯ ")
	}
	if r.header {
		caret := "▾ "
		if m.collapsed[r.collapseKey] {
			caret = "▸ "
		}
		if r.beadID == "" { // unparented header
			return marker + dim.Render(caret+"Unparented"), "", ""
		}
		h := m.beadByID(r.beadID)
		closed, total := m.childCounts(r.beadID)
		pre = m.rowPrefix(marker+caret, h)
		suf = " " + dim.Render(fmt.Sprintf("%d/%d", closed, total)) + m.rowSuffix(h)
		return pre, chartName(h.Title), suf
	}
	b := m.beadByID(r.beadID)
	branch := "├ "
	if r.last {
		branch = "└ "
	}
	pre = m.rowPrefix(marker+dim.Render(branch), b)
	return pre, b.Title, m.rowSuffix(b)
}

// rowPrefix is a master row's chrome before its title: the lead (selection
// marker, caret or branch), then state glyph, liveness dot and kind tag.
func (m Model) rowPrefix(lead string, b *bd.Bead) string {
	return lead + m.stateGlyph(b) + m.rowDot(b) + " " + kindTag(b) + " "
}

// fitRowTitle truncates a master row's title to the column width less the rendered
// width of the row around it, so the row never wraps (Spec FR1/FR2): only
// the title is cut, the tag, glyphs, counts and age always show whole.
func fitRowTitle(title string, width int, pre, suf string) string {
	return trunc(title, width-lipgloss.Width(pre)-lipgloss.Width(suf))
}

// fitRow is the last-resort clamp for a row whose chrome outgrows even the
// widened column (masterWidth caps at m.w-20): cut it rather than let windowRows
// wrap it and desync the follow window.
// ponytail: only reachable with chrome > m.w-21 cols, far past any real count/age; drop whole suffix segments if it ever is.
func fitRow(row string, w int) string { return ansi.Truncate(row, w, "") }

// inboxRow renders one Inbox master line at column width w.
func (m Model) inboxRow(b *bd.Bead, w int) string {
	pre, suf := m.inboxParts(b)
	return pre + fitRowTitle(b.Title, w, pre, suf) + suf
}

// inboxParts is an Inbox line's chrome before and after its title.
func (m Model) inboxParts(b *bd.Bead) (pre, suf string) {
	marker := "  "
	if b.ID == m.selInbox {
		marker = selMarker.Render("❯ ")
	}
	return m.rowPrefix(marker, b), " " + dim.Render(HumanAge(b.Created)) + m.rowSuffix(b)
}

func (m Model) inboxView(height int) string {
	list := m.inboxRows()
	mw := m.masterColW()
	var lines []string
	bar := m.tabFilterBar(tabInbox, len(list))
	switch {
	case len(list) == 0 && bar != "":
		lines = append(lines, dim.Render(noMatches))
	case len(list) == 0:
		lines = append(lines, dim.Render("inbox clear — nothing awaiting you"))
	}
	var sel *bd.Bead
	selRow := 0
	for i := range list {
		b := &list[i]
		if b.ID == m.selInbox {
			selRow, sel = i, b
		}
		lines = append(lines, fitRow(m.inboxRow(b, mw), mw))
	}
	return m.layout(bar, lines, selRow, m.detailView(sel, selRow, len(list), height), height)
}

// tabFilterBar is tab t's filter bar (FR8), "" while t has no filter: shown is the
// rows matching every filter; the total is t's rows with none of the bar's filters
// (kind, search, Logs scope) — the view's own h, d and e still apply.
func (m Model) tabFilterBar(t tab, shown int) string {
	if !m.filtered(t) {
		return ""
	}
	terms := m.filters[t].terms()
	if t == tabLogs {
		terms = m.logTerms()
	}
	return filterBar(terms, shown, len(m.rowKinds(t, "")), m.masterColW())
}

// noMatches is a filtered list's empty state.
const noMatches = "no matching beads"

// filterBar is a filtered list pane's first line (Spec FR8), reverse
// video across the column: FILTER, the tab's terms (Graph and Inbox feed kind and
// search; Logs adds scope:<id>), then how many of the tab's rows show. Too
// wide for the column, the esc hint goes first (the footer carries esc too).
func filterBar(terms []string, shown, total, w int) string {
	s := fmt.Sprintf("FILTER  %s  — %d of %d", strings.Join(terms, "  "), shown, total)
	if hint := " · esc clears"; lipgloss.Width(s+hint) <= w {
		s += hint
	}
	return filterSty.Width(w).Render(trunc(s, w))
}

// detailView renders the selected bead's detail pane at the given pane height,
// scrolled to the model's current offset. sel/total are the 0-based selection
// index and length of the master list, threaded through for the single-pane
// [i/n] position indicator.
func (m Model) detailView(b *bd.Bead, sel, total, height int) string {
	s, _ := m.detailLayout(b, height, m.detailOffset, sel, total)
	return s
}

// fieldLine is one detail-header field: a padded label gutter, then the value.
func fieldLine(k, v string) string { return label.Render(fmt.Sprintf("%-11s", k)) + " " + v }

// detailLayout assembles the detail pane — a fixed header (title + fields), the
// scrollable description windowed to fit, and a fixed footer (launch preview
// §9.3; commands live in the help line, ADR 0026) — and returns it with the max scroll offset so j/k can
// clamp against the same geometry the renderer used. nil selection (unparented
// header / empty) shows a hint. The description fills whatever rows the fixed
// chrome leaves, so it never overflows or grows the pane.
func (m Model) detailLayout(b *bd.Bead, paneH, offset, sel, listLen int) (string, int) {
	if b == nil {
		return dim.Render("Unparented — open Signals awaiting triage and Rollbacks awaiting Jinbe.\n\nSelect a Signal to launch its charting session, or a Rollback to run it with t."), 0
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
	bar := "" // accent bar when the detail pane holds focus (spec §9.4)
	if m.focusDetail {
		bar = lipgloss.NewStyle().Foreground(lipgloss.Color("111")).Render("▏") + " "
	}
	title := b.Title
	// [i/n] list-position indicator (ADR 0024): only in single-pane
	// detail-focused mode, where the master list that showed *where* the bead sits
	// is hidden. In two-pane mode the visible list already shows position, so omit.
	if m.singlePane() && m.focusDetail && listLen > 0 {
		title += "  " + dim.Render(fmt.Sprintf("[%d/%d]", sel+1, listLen))
	}
	fmt.Fprintf(&h, "%s%s %s %s  %s\n\n", bar, m.stateGlyph(b), kindTag(b), dim.Render(mode), title)
	field := func(k, v string) { h.WriteString(fieldLine(k, v) + "\n") }
	field("id", b.ID)
	field("state · pri", fmt.Sprintf("%s · P%d", b.Status, b.Priority))
	field("owner", owner)
	if b.Assignee != "" {
		field("claimed by", b.Assignee)
	}
	if b.Container {
		closed, total := m.childCounts(b.ID)
		field("children", fmt.Sprintf("%d/%d closed", closed, total))
	} else if b.Parent != "" {
		field("chart", b.Parent)
	}
	if l := m.liveOf(b); l != na || (b.Status == "in_progress" && isHITL(b.Kind)) {
		field("liveness", livenessLine(l, b))
	}
	if v, ok := m.sessionLine(b); ok {
		field("session", v)
	}
	if l := m.pauseLine(b); l != "" {
		field("paused", l)
	}
	if m.efforts[b.ID] { // where its unintegrated work lives, off the refresh's one ref read (ADR 0104 §7)
		field("effort", git.EffortBranch(b.ID))
	}
	field("age", HumanAge(b.Created))
	// Why it closed (Spec FR6): header chrome, so a long reason wraps into
	// the budget below; bd's default reason says nothing the state line doesn't.
	if r := b.CloseReason; r != "" && r != bd.DefaultCloseReason {
		field("reason", r)
	}
	if b.Kind == contract.KindPivot && b.Open() {
		// The lead's session triages its whole Checkpoint (FR22): name what it covers.
		// Only the lead (pivotLeads, the Inbox's row) shows it.
		isLead := pivotLeads(m.beads)[pivotGate(m.beads, b.ID)].ID == b.ID
		if others := otherOpenPivots(m.beads, *b); isLead && len(others) > 0 {
			fmt.Fprintf(&h, "\n%s\n", label.Render(fmt.Sprintf("+%d other open pivots:", len(others))))
			for _, o := range others {
				fmt.Fprintf(&h, "  · %s\n", trunc(o.Title, m.detailInnerW()-4))
			}
		}
	}
	header := strings.TrimRight(h.String(), "\n")
	footer := m.launchPreview(b)

	// Rows the fixed chrome consumes, plus one blank separator above and below the
	// description; whatever is left is the scrollable body budget.
	// ponytail: on a tiny terminal header+footer alone can exceed paneH (budget
	// clamps to 0, description hidden) — same overflow the pre-scroll pane had.
	// Counted as wrapped at innerW: a long title wraps the header, and an unwrapped
	// count lets the pane outgrow paneH (Spec FR3).
	innerW := m.detailInnerW()
	wrapped := func(s string) int { return lineCount(lipgloss.NewStyle().Width(innerW).Render(s)) }
	budget := paneH - wrapped(header) - wrapped(footer) - 2
	if budget < 0 {
		budget = 0
	}
	total := 0
	if b.Description != "" {
		total = wrapped(b.Description)
	}
	maxOff := total - budget
	if maxOff < 0 {
		maxOff = 0
	}
	parts := []string{header}
	if budget > 0 && b.Description != "" {
		parts = append(parts, strings.Join(windowRows(b.Description, innerW, budget, offset), "\n"))
	}
	parts = append(parts, footer)
	return strings.Join(parts, "\n\n"), maxOff
}

// pauseLine is the detail pane's paused field (ADR 0057 amendment): every cause,
// manual first, each in its colour — additive labels record no reason, actor or age —
// then, on a non-closed bead, "manual via <nearest-ancestor>" in dim magenta (ADR
// 0084). "" when not paused.
func (m Model) pauseLine(b *bd.Bead) string {
	var vs []string
	for _, c := range bd.PausedCauses(*b) {
		vs = append(vs, pauseSty[c].Render(c))
	}
	if b.Status != "closed" {
		if anc := m.inheritedPause(b); anc != "" {
			vs = append(vs, inheritSty.Render("manual via "+anc))
		}
	}
	return strings.Join(vs, dim.Render(" · "))
}

// inheritedPause names b's nearest paused:manual ancestor in the loaded graph, ""
// for none: the shared predicate (bd.PausedAncestor) run I/O-free over m.beads,
// whose placeholder for an unknown id ends the walk.
// ponytail: linear beadByID per hop per rendered row; index by ID if the graph grows large.
func (m Model) inheritedPause(b *bd.Bead) string {
	anc, _ := bd.PausedAncestor(*b, func(id string) (bd.Bead, error) { return *m.beadByID(id), nil })
	return anc
}

func lineCount(s string) int { return strings.Count(s, "\n") + 1 }

// launchPreview shows what launch would run (spec §9.3), or the intervene note
// for a bead the loop drives.
func (m Model) launchPreview(b *bd.Bead) string {
	if b.Status == "closed" {
		return dim.Render("closed — kept for lineage.")
	}
	if b.Container {
		if b.Kind == contract.KindCheckpoint {
			if m.runNowAffordable(b) {
				return dim.Render("CHECKPOINT — approved & at HEAD. Press ") + selMarker.Render("t") +
					dim.Render(" to land it now (Jinbe), or wait for the loop. Use edit to intervene.")
			}
			if held := m.landHeldBy(b); len(held) > 0 {
				return dim.Render("CHECKPOINT — " + landHeldNote(held) + ". Use edit to intervene.")
			}
			return dim.Render("CHECKPOINT — land-watched. Jinbe lands it once an Approval\ncloses at the branch HEAD. Use edit to intervene.")
		}
		if m.runNowAffordable(b) {
			return dim.Render("CONTAINER — drained & defogged. Press ") + selMarker.Render("t") +
				dim.Render(" to run its speccing reconcile now, or wait for the loop. Use edit to intervene.")
		}
		return dim.Render("CONTAINER — drain-watched. The loop spawns the Spec when the\nfrontier clears (chart:defogged). Use edit to intervene.")
	}
	if !isHITL(b.Kind) {
		if m.runNowAffordable(b) {
			return dim.Render("AFK — "+string(b.Kind)+" runs via the loop. Press ") + selMarker.Render("t") +
				dim.Render(" to run it now, or edit to intervene.")
		}
		return dim.Render("AFK — " + string(b.Kind) + " runs via the loop. Use edit to intervene.")
	}
	// Static, no Launch call: render runs no subprocess (ADR 0101 §1–2).
	owner, err := launchOwner(m.crew, b.Kind)
	if err != nil {
		return dim.Render("ON LAUNCH: ") + warn.Render(err.Error())
	}
	return label.Render("ON LAUNCH") + " — " + strings.ToUpper(owner[:1]) + owner[1:] +
		" opens an agent session for " + string(b.Kind)
}

func (m Model) beadByID(id string) *bd.Bead {
	for i := range m.beads {
		if m.beads[i].ID == id {
			return &m.beads[i]
		}
	}
	return &bd.Bead{ID: id, Title: id}
}

// childCounts returns (closed, total) member beads of a container.
func (m Model) childCounts(id string) (closed, total int) {
	for _, b := range m.beads {
		if b.Parent == id && b.Kind != "" && !b.Container {
			total++
			if b.Status == "closed" {
				closed++
			}
		}
	}
	return
}

// trunc cuts s to at most n terminal cells (wide glyphs count 2), ending in …
// when cut.
func trunc(s string, n int) string {
	return ansi.Truncate(s, max(n, 1), "…")
}
