package tui

import (
	"cmp"
	"maps"
	"slices"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// Graph's keys (ADR 0144 §2). Hints list only the verbs that apply to the
// selection, in §7's order; moving is shell-like, shown in ? alone.
var (
	keyMove    = key.NewBinding(key.WithKeys("j", "k", "up", "down"), key.WithHelp("j/k", "move"))
	keySection = key.NewBinding(key.WithKeys("J", "K", "ctrl+down", "ctrl+up"), key.WithHelp("J/K", "section"))
	keyFold    = key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "fold"))
	keyDetail  = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "detail"))
	keyShut    = key.NewBinding(key.WithKeys("d", "esc"), key.WithHelp("d", "close"))
	keyScroll  = key.NewBinding(key.WithKeys("pgup", "pgdown", "ctrl+u", "ctrl+d"), key.WithHelp("pgup/pgdn", "scroll"))

	keyLaunch = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "launch/attach (HITL) · run/spec/land/retry (AFK)"))
	keyBg     = key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "bg"))
	keyPrio   = key.NewBinding(key.WithKeys("+", "=", "-", "_"), key.WithHelp("+/-", "prio"))

	// p and x name what they do to the selection (ADR 0144 §7); these are the
	// labelless forms ? shows and the shell matches on.
	keyPauseKey   = verbKey("p", "pause")
	keyDropKey    = verbKey("x", "finish or drop the session / abort or recover the effort")
	keyTranscript = verbKey("t", "transcript")
)

func verbKey(k, label string) key.Binding {
	return key.NewBinding(key.WithKeys(k), key.WithHelp(k, label))
}

// busyWhy is why a write, a launch or a close holds b, "" when none does.
func (g graphData) busyWhy(b *bd.Bead) string {
	switch _, w := g.pending[b.ID]; {
	case w:
		return b.ID + ": write in flight, wait"
	case g.launching[b.ID]:
		return "still launching " + cmp.Or(b.Title, b.ID) + "…"
	case g.closing(b.ID):
		return b.ID + " is closing — wait"
	}
	return ""
}

// launchWhy is "" when enter may launch, attach or resume b, else why not (FR26). A
// session is attached or resumed whatever the bead, even a closed one (ADR 0038 §3, kept by FR34); a new
// launch needs an open HITL leaf.
func (g graphData) launchWhy(b *bd.Bead) string {
	switch {
	case g.busyWhy(b) != "":
		return g.busyWhy(b)
	case g.sessions[b.ID].Has():
		return ""
	case b.Status == "closed":
		return b.ID + " is closed — nothing to launch or run"
	case strandable(b): // a stranded claim's worktree may be live: never launch afresh
		return claimLine(b)
	case (b.Container && b.Kind != contract.KindInitiative) || !isHITL(b.Kind): // an open Initiative head launches Luffy's cut session
		return "b is HITL-only — enter runs it"
	}
	return ""
}

// claimLine is the one line for an in_progress HITL bead with no local session: another
// actor's claim is not ours to launch or force; ours or nobody's may be stranded (ADR 0240).
func claimLine(b *bd.Bead) string {
	if bd.ClaimedElsewhere(b.Assignee) {
		return b.ID + " is claimed by " + b.Assignee + " — not yours to launch; bd unclaim " + b.ID + " --force once " + b.Assignee + " is gone"
	}
	return b.ID + "'s claim has no session here — x recovers it if it's stranded"
}

// strandable is whether b is an in_progress HITL bead whose claim may be stranded: not
// one of the restsClaimed kinds, which keep their at-rest handling (D6).
func strandable(b *bd.Bead) bool {
	return b.Status == "in_progress" && isHITL(b.Kind) && !restsClaimed(b.Kind)
}

// restsClaimed is whether an idle in_progress bead of kind k is at rest, not
// stranded: a Surgery keeps its filing claim and a Task waits claimed (ADR 0170 §9).
func restsClaimed(k contract.Kind) bool {
	return k == contract.KindSurgery || k == contract.KindTask || k == contract.KindDiscussion || k == contract.KindCheckup
}

// launches is whether enter launches or attaches b, else it runs b: a session, or an
// open HITL leaf or Initiative head.
func (g graphData) launches(b *bd.Bead) bool {
	return g.sessions[b.ID].Has() || (isHITL(b.Kind) && (!b.Container || b.Kind == contract.KindInitiative))
}

// enterVerb is what enter does to b: resume, attach, launch or the run-now label; ""
// when launchWhy or the run-now gate refuses, so the hints bar omits it.
func (g graphData) enterVerb(b *bd.Bead) string {
	if g.launches(b) {
		switch s := g.sessions[b.ID]; {
		case g.launchWhy(b) != "":
			return ""
		case s.Has() && s.State == session.Exited:
			return "resume"
		case s.Has():
			return "attach"
		}
		return "launch"
	}
	if g.runNowWhy(b) != "" {
		return ""
	}
	return g.runNowLabel(b)
}

// xLabel is what x does to b: drop its session, abort its live AFK effort, or
// recover a dead one or a gone session; "" for none (ADR 0083, ADR 0127).
func (g graphData) xLabel(b *bd.Bead) string {
	switch {
	case g.busyWhy(b) != "":
		return ""
	case b.Kind == contract.KindCheckup && b.Unclosed():
		return "drop" // x ends a Checkup the human declined, session or none
	case g.sessions[b.ID].Has():
		return "drop" // a session always wins x
	case g.liveOf(b) == alive:
		return "abort"
	case g.liveOf(b) == dead, isHITL(b.Kind) && !bd.ClaimedElsewhere(b.Assignee) && (!b.Container || b.Kind == contract.KindInitiative) && b.Status == "in_progress" && !g.sessions[b.ID].Has():
		return "recover"
	}
	return ""
}

// xHint is x's footer label: xLabel, widened to finish/drop on an armed session.
func (g graphData) xHint(b *bd.Bead) string {
	if l := g.xLabel(b); l != "drop" || !g.sessions[b.ID].Armed() {
		return l
	}
	return "finish/drop"
}

// pauseLabel is p's verb: it adds or lifts only paused:manual.
func pauseLabel(b *bd.Bead) string {
	if bd.PausedBy(*b, bd.PausedManual) {
		return "unpause"
	}
	return "pause"
}

// statusMsg carries a tab's action result to the shell's status line (FR12).
type statusMsg string

func (g graphTab) count() int { return 0 }
func (g graphTab) actN() int  { return 0 }

func (g graphTab) capturing() bool { return g.typing }

func (g graphTab) bindings() []key.Binding {
	if g.typing {
		return []key.Binding{keyKeep, keyRevert}
	}
	rows := g.rows()
	b := g.selected()
	var bs []key.Binding
	if b != nil {
		if v := g.enterVerb(b); v != "" {
			bs = append(bs, verbKey("enter", v))
		}
		if s := g.sessions[b.ID]; g.launchWhy(b) == "" && (!s.Has() || s.State == session.Exited) {
			bs = append(bs, keyBg) // b on an alive session only refuses (FR27)
		}
	}
	if i := indexRow(rows, g.sel); i >= 0 && rows[i].kids {
		bs = append(bs, keyFold)
	}
	switch {
	case g.open:
		bs = append(bs, keyShut, keyScroll)
	case len(rows) > 0:
		bs = append(bs, keyDetail)
	}
	bs = append(bs, keySearch, keyStatus, keyKind)
	if b != nil {
		if b.Unclosed() {
			bs = append(bs, verbKey("p", pauseLabel(b)))
		}
		if l := g.xHint(b); l != "" {
			bs = append(bs, verbKey("x", l))
		}
		if g.xOffers(b) {
			bs = append(bs, xBinding(b))
		}
		bs = append(bs, keyTranscript)
	}
	forms := formBindings(g.beads, b)
	// hintOf (ADR 0229) picks each verb's section; this order is the in-section order on the bar and in ?.
	at := slices.IndexFunc(forms, func(f key.Binding) bool { return slices.Contains([]string{"c", "a", "A", "B", "U", "r"}, f.Keys()[0]) })
	if at < 0 {
		at = len(forms)
	}
	bs = append(bs, forms[:at]...)
	if b != nil && b.Unclosed() {
		bs = append(bs, keyPrio)
	}
	bs = append(bs, forms[at:]...)
	return bs
}

// extra is the keys the tab handles beyond its hinted verbs: moving, and every
// action key the selection does not offer, which answers with a status line.
func (g graphTab) extra() []key.Binding {
	if g.typing {
		return nil
	}
	shown := map[string]bool{}
	for _, b := range g.bindings() {
		for _, k := range b.Keys() {
			shown[k] = true
		}
	}
	var out []key.Binding
	if len(g.rows()) >= 2 { // one row has nowhere to move
		out = append(out, keyMove, keySection)
	}
	for _, b := range []key.Binding{keyLaunch, keyBg, keyPauseKey, keyDropKey, keyWithdrawKey, keyPrio, keyReject, keyAdopt, keyRelease, keyBlock, keyUnblock, keyCheckup} {
		if !shown[b.Keys()[0]] {
			out = append(out, b)
		}
	}
	if g.q.active() && !g.open { // esc clears the filter; §7 hints it on Logs alone
		out = append(out, keyClear)
	}
	return out
}

func (g graphTab) update(k tea.KeyMsg) (component, tea.Cmd) {
	if g.typing {
		return g.typingKey(k), nil
	}
	rows := g.rows()
	i := indexRow(rows, g.sel)
	switch {
	case key.Matches(k, keyMove):
		g.want = "" // any move cancels the cursor memory (ADR 0232 §6)
		if len(rows) == 0 {
			return g, nil
		}
		d := 1
		if s := k.String(); s == "k" || s == "up" {
			d = -1
		}
		g.sel, g.off, g.want = rows[clampWrap(i+d, len(rows))].sel, 0, "" // any move cancels the memory; the detail follows: back to the top
	case key.Matches(k, keySection):
		if len(rows) == 0 {
			return g, nil
		}
		d := 1
		if s := k.String(); s == "K" || s == "ctrl+up" {
			d = -1
		}
		g.sel, g.off, g.want = rows[jumpTo(rows, i, d, func(r row) bool { return r.header && r.depth == 0 })].sel, 0, "" // like a move: cancel the memory, detail back to the top
	case key.Matches(k, keyFold) && i >= 0 && rows[i].kids:
		f := maps.Clone(g.folds())
		f[rows[i].collapseKey] = !f[rows[i].collapseKey]
		return g.setFolds(f).fixSelection(i), nil
	case g.open && key.Matches(k, keyShut):
		g.open = false
	case !g.open && len(rows) > 0 && key.Matches(k, keyDetail):
		g.open = true
	case g.open && key.Matches(k, keyScroll):
		vp := g.detailVP()
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
		g.off = vp.YOffset
	case key.Matches(k, keySearch):
		g.typing, g.edit = true, lineAt(g.q.words)
	case key.Matches(k, keyStatus):
		return g, func() tea.Msg { return pickGraphMsg{"status"} }
	case key.Matches(k, keyKind):
		return g, func() tea.Msg { return pickGraphMsg{"kind"} }
	case key.Matches(k, keyClear) && g.visit:
		g.q, g.visit = baseline, false
		return g.rebuild(), nil
	case key.Matches(k, keyClear) && g.q.active():
		return g.clearOne(), nil
	}
	return g, nil
}

func (t graphTab) drawerOpen() bool { return t.open }
