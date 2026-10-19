package tui

// X on an Initiative row: the cancel dialog (Spec mc-5rttm D7, ADR 0313 §3–§5). It
// shares the withdraw dialog's modal, tags, scrolling and barred mode. Page one is a
// tally and a close-or-release choice per member chain; enter opens the review page,
// and y there runs the Cancel verb in-process with exactly the chains released.

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/withdraw"
)

var (
	keyReleaseRow = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "release"))
	keyCloseRow   = key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "close"))
	keyReview     = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "review"))
	keyCancelYes  = key.NewBinding(key.WithKeys("y", "enter"), key.WithHelp("y/enter", "cancel"))
	keyCancelLoss = key.NewBinding(key.WithKeys("y", "enter"), key.WithHelp("y/enter", "discard and cancel"))
	keyBackRows   = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
)

// xChoiceNarrow: under this many inner columns a chain's choice drops to its own line.
const xChoiceNarrow = 70

// planCancel and runCancel are the Cancel verb's read-only view and its act, vars so
// the tests stub them. Both take the released heads; the first view releases none.
var (
	planCancel = planVerb
	runCancel  = func(initiative string, release []string, discard bool) (withdraw.Result, error) {
		return runVerb(initiative, release, discard, withdraw.Cancel)
	}
)

// cancelChain is one member chain: its head, the beads it closes, and why it cannot
// be released ("" when it can).
type cancelChain struct {
	head   bd.Bead
	ids    map[string]bool
	locked string
}

// cancelState is the dialog's choices: one release flag per chain, every one starting
// on close.
type cancelState struct {
	chains  []cancelChain
	release []bool
	review  bool
	flash   string
}

// cancelOpen is X on an Initiative: read the Plan and open the dialog, barred when the
// verb would refuse.
func (m Model) cancelOpen(init bd.Bead) (tea.Model, tea.Cmd) {
	id := init.ID
	return m.openPlan(id, "cancel", func(id string) (withdraw.Plan, error) { return planCancel(id, nil) }, func(p withdraw.Plan) dialog {
		return m.cancelDialog(id, p, len(p.Refusals) > 0, "")
	})
}

func (m Model) cancelDialog(id string, p withdraw.Plan, barred bool, warn string) dialog {
	title := id
	if i := beadIndex(m.beads, id); i >= 0 {
		title = m.beads[i].Title
	}
	chains := slices.Clone(p.Chains)
	slices.SortStableFunc(chains, func(a, b withdraw.Chain) int { return beadCmp(a.Head, b.Head) })
	cs := &cancelState{release: make([]bool, len(chains))}
	for i, ch := range chains {
		c := cancelChain{head: ch.Head, ids: map[string]bool{}}
		for _, id := range ch.Beads {
			c.ids[id] = true
		}
		for _, l := range p.Locked {
			if l.Head == ch.Head.ID {
				c.locked = l.Why
			}
		}
		cs.release[i] = slices.Contains(p.Released, ch.Head.ID)
		cs.chains = append(cs.chains, c)
	}
	return dialog{kind: xdialog, title: "Cancel " + title + "?", id: id, xs: xState{plan: p, barred: barred, warn: warn, cancel: cs}}
}

// closing is the beads the cancel closes with the current choices: the plan's set less
// the released chains' beads, the Initiative itself left out.
func (cs *cancelState) closing(p withdraw.Plan) []bd.Bead {
	var out []bd.Bead
	for _, b := range p.Set {
		if b.ID != p.Target && !cs.releasedBead(b.ID) {
			out = append(out, b)
		}
	}
	return out
}

func (cs *cancelState) releasedBead(id string) bool {
	for i, c := range cs.chains {
		if cs.release[i] && c.ids[id] {
			return true
		}
	}
	return false
}

// released is the heads the cancel releases.
func (cs *cancelState) released() (hs []string) {
	for i, c := range cs.chains {
		if cs.release[i] {
			hs = append(hs, c.head.ID)
		}
	}
	return hs
}

// closingInventory is the plan's inventory narrowed to the beads that still close.
func closingInventory(inv withdraw.Inventory, set []bd.Bead) (out withdraw.Inventory) {
	in := map[string]bool{}
	for _, b := range set {
		in[b.ID] = true
	}
	for _, b := range inv.Beads {
		if in[b.ID] {
			out.Beads = append(out.Beads, b)
		}
	}
	for _, c := range inv.Containers {
		if in[c.ID] {
			out.Containers = append(out.Containers, c)
		}
	}
	return out
}

// loses reports whether the cancel with the current choices discards work.
func (cs *cancelState) loses(p withdraw.Plan) bool {
	return closingInventory(p.Inventory, cs.closing(p)).Loses()
}

// chainTags is a chain's cost: its beads' tags folded into one.
func chainTags(inv withdraw.Inventory, c cancelChain) (t xTags) {
	for id := range c.ids {
		t.add(tagsOf(inv, id))
	}
	return t
}

func chainsNoun(n int) string { return pluralN(n, "chain") }

// cancelTally is the lines above the rows, recomputed on every toggle.
func (cs *cancelState) tally(p withdraw.Plan) []string {
	closing := cs.closing(p)
	rel := len(cs.released())
	nClose := len(cs.chains) - rel
	line := fmt.Sprintf("Closes %s (%s) · releases %s.", chainsNoun(nClose), pluralN(len(closing), "bead"), chainsNoun(rel))
	return []string{line, xSummary(closingInventory(p.Inventory, closing), closing)}
}

// cancelBindings are the hints: only the keys the dialog's state honours.
func (d dialog) cancelBindings() []key.Binding {
	cs := d.xs.cancel
	switch {
	case d.xs.barred:
		return []key.Binding{keyBlocker, keyDown, keyUp, keyCloseDialog}
	case cs.review && cs.loses(d.xs.plan):
		return []key.Binding{keyCancelLoss, keyBackRows}
	case cs.review:
		return []key.Binding{keyCancelYes, keyBackRows}
	}
	return []key.Binding{keyReview, keyReleaseRow, keyCloseRow, keyDown, keyUp, keyCloseDialog}
}

// cancelKey is the cancel dialog's keys.
func (m Model) cancelKey(d dialog, k tea.KeyMsg) (tea.Model, tea.Cmd) {
	cs := *d.xs.cancel
	cs.release = slices.Clone(cs.release)
	n := len(cs.chains)
	switch {
	case key.Matches(k, keyCancel):
		if !cs.review {
			return m.pop(), nil
		}
		cs.review = false
	case d.xs.barred:
		switch {
		case key.Matches(k, keyDown) && n > 0:
			d.cur = (d.cur + 1) % n
		case key.Matches(k, keyUp) && n > 0:
			d.cur = (d.cur + n - 1) % n
		case key.Matches(k, keyBlocker):
			if r := d.xs.plan.Refusals; len(r) > 0 && len(r[0].Beads) > 0 {
				return m.pop().recentsJump(r[0].Beads[0])
			}
			return m, nil
		default:
			return m, nil
		}
	case cs.review:
		if key.Matches(k, keyCancelYes) {
			return m.pop().confirmCancel(d.id, cs.released(), cs.loses(d.xs.plan))
		}
		return m, nil
	case key.Matches(k, keyDown) && n > 0:
		d.cur = (d.cur + 1) % n
		cs.flash = ""
	case key.Matches(k, keyUp) && n > 0:
		d.cur = (d.cur + n - 1) % n
		cs.flash = ""
	case key.Matches(k, keyReleaseRow) && n > 0:
		c := cs.chains[d.cur]
		if c.locked != "" {
			cs.flash = c.head.Title + " stays closed: " + c.locked
			break
		}
		cs.release[d.cur], cs.flash = true, ""
	case key.Matches(k, keyCloseRow) && n > 0:
		cs.release[d.cur], cs.flash = false, ""
	case key.Matches(k, keyReview):
		cs.review, cs.flash = true, ""
	default:
		return m, nil
	}
	d.xs.cancel = &cs
	m.modals = slices.Clone(m.modals)
	m.modals[len(m.modals)-1] = d
	return m, nil
}

// confirmCancel runs the verb off the event loop. The verb re-reads the graph under its
// fence, so a refusal now reopens the dialog barred with the fresh refusals.
func (m Model) confirmCancel(id string, release []string, discard bool) (Model, tea.Cmd) {
	return into(m.startAct(id, "cancelling", func() writeDoneMsg {
		res, err := runCancel(id, release, discard)
		var p withdraw.Plan
		var perr error
		if replans(err) {
			p, perr = replanCancel(id, release)
		}
		return writeDoneMsg{id, func(m Model) (tea.Model, tea.Cmd) { return m.cancelDone(id, res, err, p, perr) }}
	}))
}

// replanCancel plans the cancel again with the releases the confirm used, so a
// reopened dialog keeps the human's choices; if they no longer apply (a chain
// adopted or withdrawn meanwhile) it falls back to releasing none.
func replanCancel(id string, release []string) (withdraw.Plan, error) {
	if p, err := planCancel(id, release); err == nil || len(release) == 0 {
		return p, err
	}
	return planCancel(id, nil)
}

func (m Model) cancelDone(id string, res withdraw.Result, err error, p withdraw.Plan, perr error) (tea.Model, tea.Cmd) {
	lead := "cancelled " + id
	if n := len(res.Released); n > 0 {
		lead += " · " + pluralN(n, "chain") + " released"
	}
	return m.verbDone(withdraw.KindCancel, id, err, p, perr, resultSummary(lead, res), m.cancelDialog)
}

// cancelView draws the rows page or the review page.
func (m Model) cancelView(d dialog) string {
	styleW := m.xWidth() - 2
	inner := styleW - 2
	p, cs := d.xs.plan, d.xs.cancel
	head := []string{brand.Render(d.title), ""}
	if d.xs.barred {
		head = append(head, m.refusedHead(p, withdraw.KindCancel, inner)...)
	}
	var body, tail []string
	hint := lipgloss.NewStyle().Width(styleW - 2).Render(flatHints(d.cancelBindings(), styleW-2))
	switch {
	case cs.review:
		body = m.cancelReview(d, inner)
	case d.xs.barred:
		for _, l := range wrapLines("Once clear: "+strings.ToLower(xCancelLead[:1])+xCancelLead[1:], inner) {
			head = append(head, dimSt.Render(l))
		}
		head = append(head, "")
	default:
		for _, l := range cs.tally(p) {
			head = append(head, wrapLines(l, inner)...)
		}
		head = append(head, "")
	}
	if d.xs.warn != "" && !d.xs.barred {
		for _, l := range wrapLines(d.xs.warn, inner) {
			tail = append(tail, warnSt.Render(l))
		}
	}
	if cs.flash != "" {
		for _, l := range wrapLines(cs.flash, inner) {
			tail = append(tail, warnSt.Render(l))
		}
	}
	tail = append(tail, "", hint)
	if !cs.review {
		budget := max(m.h-2-len(head)-len(tail), 1)
		body = m.cancelRows(d, inner, budget)
	}
	ls := append(append(head, body...), tail...)
	return m.placeBox(refBox.Width(styleW).Render(lipgloss.JoinVertical(lipgloss.Left, ls...)))
}

const xCancelLead = "Closes the unclosed member chains, then the Initiative, as cancelled."

// choiceOf draws a chain's "(●) close  ( ) release" choice; a locked chain keeps close.
func choiceOf(c cancelChain, released bool) string {
	if c.locked != "" {
		return "(●) close  🔒 locked"
	}
	if released {
		return "( ) close  (●) release"
	}
	return "(●) close  ( ) release"
}

// cancelRows draws the chains, windowed to budget lines with a k/N counter. Each chain
// is a line (two under xChoiceNarrow inner columns), and the highlighted locked chain
// has its reason under it.
func (m Model) cancelRows(d dialog, inner, budget int) []string {
	p, cs := d.xs.plan, d.xs.cancel
	var lines []string
	var first, last []int
	for i, c := range cs.chains {
		first = append(first, len(lines))
		tags := chainTags(p.Inventory, c)
		tagStr := ""
		if !tags.none() {
			tagStr = tags.String(inner >= xCompact)
		}
		choice := choiceOf(c, cs.release[i])
		left := kindTag(&c.head) + " " + c.head.Title
		var rows []string
		if inner < xChoiceNarrow {
			rows = []string{withTags(left, tagStr, inner), "  " + choice}
		} else {
			right := choice
			if tagStr != "" {
				right = tagStr + "  " + choice
			}
			rows = []string{withTags(left, right, inner)}
		}
		if c.locked != "" && i == d.cur && !d.xs.barred {
			for _, l := range wrapLines("🔒 "+c.locked, inner-2) {
				rows = append(rows, "  "+warnSt.Render(l))
			}
		}
		for j, r := range rows {
			switch {
			case d.xs.barred:
				r = dimSt.Render(ansi.Strip(r))
			case i == d.cur && j == 0 || i == d.cur && inner < xChoiceNarrow && j == 1:
				r = shadeRow(r, inner, pickRow)
			}
			lines = append(lines, r)
		}
		last = append(last, len(lines))
	}
	if len(lines) <= budget || len(cs.chains) == 0 {
		return lines
	}
	rows := max(budget-1, 1)
	lo := followOffset(len(lines), rows, last[d.cur]-1)
	if first[d.cur] < lo {
		lo = first[d.cur]
	}
	out := slices.Clone(lines[lo:min(lo+rows, len(lines))])
	return append(out, dimSt.Render(fmt.Sprintf("%d/%d", d.cur+1, len(cs.chains))))
}

// cancelReview is page two: what closes with each chain's cost, what is released, and
// the total discarded.
func (m Model) cancelReview(d dialog, inner int) []string {
	p, cs := d.xs.plan, d.xs.cancel
	var closes, releases []string
	for i, c := range cs.chains {
		line := "  " + kindTag(&c.head) + " " + c.head.Title
		if cs.release[i] {
			releases = append(releases, ansi.Truncate(line, inner, "…"))
			continue
		}
		s := ""
		if t := chainTags(p.Inventory, c); !t.none() {
			s = t.String(inner >= xCompact)
		}
		closes = append(closes, withTags(line, s, inner))
	}
	out := []string{fmt.Sprintf("Closes %s:", chainsNoun(len(closes)))}
	out = append(out, closes...)
	out = append(out, "", fmt.Sprintf("Releases %s:", chainsNoun(len(releases))))
	out = append(out, releases...)
	closing := cs.closing(p)
	out = append(out, "")
	out = append(out, wrapLines(xSummary(closingInventory(p.Inventory, closing), closing), inner)...)
	return out
}
