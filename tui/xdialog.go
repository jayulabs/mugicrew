package tui

// X: withdraw the chain on the highlighted head row (Spec mc-5rttm D7, ADR 0313).
// X reads the verb's Plan (no write, no event) and opens a dialog that shows the
// chain as a tree with what it loses; confirming runs the Withdraw verb in-process.
// A refused Plan opens the same dialog barred. x never gets here: it keeps finish,
// drop, abort and recover, so no press of x can close a chain.

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/withdraw"
)

var (
	keyWithdrawKey = verbKey("X", "withdraw")
	keyCancelKey   = verbKey("X", "cancel")
	keyWithdrawYes = key.NewBinding(key.WithKeys("y", "enter"), key.WithHelp("y/enter", "withdraw"))
	keyDiscardWork = key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "discard and withdraw"))
	keyBlocker     = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "go to blocker"))
	// keyCloseDialog is esc's hint here: in a dialog titled "Cancel <Initiative>?",
	// "esc cancel" would read as cancelling the Initiative.
	keyCloseDialog = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close"))
)

// xCompact: under this many inner columns a row's tags keep their glyph and count
// only (ADR 0313 §1).
const xCompact = 80

// xBinding is X's footer hint for b: cancel on an Initiative row, withdraw on a head.
func xBinding(b *bd.Bead) key.Binding {
	if isOpenInitiative(b) {
		return keyCancelKey
	}
	return keyWithdrawKey
}

// planWithdraw and runWithdraw are the verb's read-only view and its act, vars so
// the tests stub them. The in-flight test is the CLI's: an active AFK effort or an
// alive session; a session table that can't be read refuses the act.
var (
	planWithdraw = func(head string) (withdraw.Plan, error) {
		return planVerb(head, nil)
	}
	runWithdraw = func(head string, discard bool) (withdraw.Result, error) {
		return runVerb(head, nil, discard, withdraw.Withdraw)
	}
)

// planVerb is the Plan of target for the human-run verbs, releasing the named heads.
func planVerb(target string, release []string) (withdraw.Plan, error) {
	o, err := withdraw.LiveOpts(readSessions)
	if err != nil {
		return withdraw.Plan{}, err
	}
	o.Release = release
	return withdraw.PlanOf(target, o)
}

// runVerb runs verb on target with the live in-flight test.
func runVerb(target string, release []string, discard bool, verb func(string, withdraw.Opts) (withdraw.Result, error)) (withdraw.Result, error) {
	o, err := withdraw.LiveOpts(readSessions)
	if err != nil {
		return withdraw.Result{}, err
	}
	o.Release, o.Discard = release, discard
	return verb(target, o)
}

// openPlan reads verb's Plan off the event loop and pushes the dialog open builds
// from it. A Plan that can't be read, or a dialog the human opened meanwhile, only
// sets the status.
func (m Model) openPlan(id, verb string, plan func(string) (withdraw.Plan, error), open func(withdraw.Plan) dialog) (tea.Model, tea.Cmd) {
	return m.startRead(id, "planning "+verb+" of", func() writeDoneMsg { // the Plan reads bd, git and the Forge (ADR 0098 §1)
		p, err := plan(id)
		return writeDoneMsg{id, func(m Model) (tea.Model, tea.Cmd) {
			if err != nil {
				m.setStatus(verb + ": " + err.Error())
				return m, nil
			}
			if len(m.modals) > 0 { // never stack on a dialog the human opened meanwhile
				m.setStatus(id + ": " + verb + " plan ready; press X again")
				return m, nil
			}
			return m.push(open(p)), nil
		}}
	})
}

// xState is the withdraw dialog's own state: the plan it was opened on, whether it
// is barred, and the warning y printed. The scroll cursor is the dialog's cur.
type xState struct {
	plan   withdraw.Plan
	barred bool // opened on a refusal: y and D inert, enter jumps to the first blocker
	warn   string
	cancel *cancelState // an Initiative cancel: the per-chain choices (ADR 0313 §3–§4)
}

// isHead reports whether b is a chain head in the TUI's own snapshot (Spec D1).
func (g graphData) isHead(b *bd.Bead) bool { return b != nil && bd.IsChainHead(*b, g.beads) }

// isOpenInitiative is X's cancel target: an Initiative that is not closed.
func isOpenInitiative(b *bd.Bead) bool {
	return b != nil && b.Kind == contract.KindInitiative && b.Unclosed()
}

// xOffers is whether X does anything on b: withdraw on a head, cancel on an Initiative.
func (g graphData) xOffers(b *bd.Bead) bool { return g.isHead(b) || isOpenInitiative(b) }

// withdrawKey is X: a head row's withdraw dialog. Anything but a head says why.
func (m Model) withdrawKey(g graphData, b bd.Bead) (tea.Model, tea.Cmd) {
	if why := g.busyWhy(&b); why != "" {
		m.setStatus(why)
		return m, nil
	}
	if isOpenInitiative(&b) {
		return m.cancelOpen(b)
	}
	if !g.isHead(&b) {
		m.setStatus(b.ID + ": X withdraws a chain head or cancels an Initiative — select a Signal, Chart, Spec, Checkpoint or Rollback, or an Initiative")
		return m, nil
	}
	id := b.ID
	return m.openPlan(id, "withdraw", planWithdraw, func(p withdraw.Plan) dialog {
		return m.xDialog(id, p, len(p.Refusals) > 0, "")
	})
}

func (m Model) xDialog(head string, p withdraw.Plan, barred bool, warn string) dialog {
	title := head
	if i := beadIndex(m.beads, head); i >= 0 {
		title = m.beads[i].Title
	}
	return dialog{kind: xdialog, title: "Withdraw " + title + "?", id: head, xs: xState{plan: p, barred: barred, warn: warn}}
}

// xKey is the withdraw dialog's keys.
func (m Model) xKey(d dialog, k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if d.xs.cancel != nil {
		return m.cancelKey(d, k)
	}
	n := len(d.xs.plan.Set) // one tree row per bead
	loses := d.xs.plan.Inventory.Loses()
	switch {
	case key.Matches(k, keyCancel):
		return m.pop(), nil
	case key.Matches(k, keyDown) && n > 0:
		d.cur = (d.cur + 1) % n
	case key.Matches(k, keyUp) && n > 0:
		d.cur = (d.cur + n - 1) % n
	case d.xs.barred:
		if key.Matches(k, keyBlocker) {
			if r := d.xs.plan.Refusals; len(r) > 0 && len(r[0].Beads) > 0 {
				next, cmd := m.pop().recentsJump(r[0].Beads[0])
				return next, cmd
			}
		}
		return m, nil
	case key.Matches(k, keyWithdrawYes):
		if loses {
			d.xs.warn = "This discards work. Press D to discard it and withdraw."
			break
		}
		return m.pop().confirmWithdraw(d.id, false)
	case key.Matches(k, keyDiscardWork):
		if !loses {
			return m, nil
		}
		return m.pop().confirmWithdraw(d.id, true)
	default:
		return m, nil
	}
	m.modals = slices.Clone(m.modals)
	m.modals[len(m.modals)-1] = d
	return m, nil
}

// confirmWithdraw runs the verb off the event loop. The verb re-reads the graph under
// its fence, so a refusal now (the graph changed while the dialog was open) reopens
// the dialog barred with the fresh refusals.
func (m Model) confirmWithdraw(head string, discard bool) (Model, tea.Cmd) {
	return into(m.startAct(head, "withdrawing", func() writeDoneMsg {
		res, err := runWithdraw(head, discard)
		var p withdraw.Plan
		var perr error
		if replans(err) { // the re-plan reads like the press-time one: here, off the event loop
			p, perr = planWithdraw(head)
		}
		return writeDoneMsg{head, func(m Model) (tea.Model, tea.Cmd) { return m.withdrawDone(head, res, err, p, perr) }}
	}))
}

// replans is whether a verb's error reopens its dialog on a fresh Plan.
func replans(err error) bool {
	var refused *withdraw.RefusedError
	var lossy *withdraw.DiscardRequiredError
	return errors.As(err, &refused) || errors.As(err, &lossy)
}

// withdrawDone takes the fresh Plan (p, perr) the verb's Cmd read when err is a refusal.
func (m Model) withdrawDone(head string, res withdraw.Result, err error, p withdraw.Plan, perr error) (tea.Model, tea.Cmd) {
	return m.verbDone(withdraw.KindWithdraw, head, err, p, perr, withdrawSummary(head, res), m.xDialog)
}

// verbDone is the end of a verb run: success sets lead on the status line; a refusal
// reopens the dialog barred with the fresh refusals, and a lossy graph reopens it
// with a warning. A refusal whose re-plan failed (perr) opens on an empty Plan, which
// the dialog says it could not read.
func (m Model) verbDone(k withdraw.Kind, id string, err error, p withdraw.Plan, perr error, lead string, open func(string, withdraw.Plan, bool, string) dialog) (tea.Model, tea.Cmd) {
	var refused *withdraw.RefusedError
	var lossy *withdraw.DiscardRequiredError
	switch {
	case err == nil:
		m.setStatus(lead)
		return m, m.load()
	case errors.As(err, &refused):
		if perr != nil {
			p = withdraw.Plan{Kind: k, Target: id}
		}
		p.Refusals = refused.Refusals
		return m.push(open(id, p, true, "")), m.load()
	case errors.As(err, &lossy):
		if perr != nil {
			m.setStatus(string(k) + ": " + perr.Error())
			return m, m.load()
		}
		return m.push(open(id, p, len(p.Refusals) > 0, "The graph changed: this now discards work.")), m.load()
	}
	m.setStatus(string(k) + ": " + err.Error())
	return m, m.load()
}

// withdrawSummary is the Result on the status line: what closed, what cleanup removed,
// and each item it kept with its holder or error.
func withdrawSummary(head string, res withdraw.Result) string {
	return resultSummary("withdrawn "+head, res)
}

// resultSummary is lead followed by the cleanup's outcome.
func resultSummary(lead string, res withdraw.Result) string {
	parts := []string{lead}
	var trees, branches int
	for _, it := range res.Removed {
		switch it.Kind {
		case withdraw.ItemTree:
			trees++
		case withdraw.ItemBranch:
			branches++
		}
	}
	var removed []string
	if trees > 0 {
		removed = append(removed, pluralN(trees, "tree"))
	}
	if branches > 0 {
		removed = append(removed, pluralN(branches, "branch"))
	}
	if len(removed) > 0 {
		parts = append(parts, strings.Join(removed, ", ")+" removed")
	}
	if n := len(res.Kept); n > 0 {
		var why []string
		for _, it := range res.Kept {
			switch {
			case it.Holder != "":
				why = append(why, string(it.Kind)+" "+it.Path+" held by "+it.Holder)
			case it.Err != "":
				why = append(why, string(it.Kind)+" "+it.Path+" failed: "+it.Err)
			default:
				why = append(why, string(it.Kind)+" "+it.Path)
			}
		}
		parts = append(parts, fmt.Sprintf("%d kept: %s", n, strings.Join(why, "; ")))
	}
	return strings.Join(parts, " · ")
}

// commitNoun is "commit" for one, else "commits": the count itself may read ≤N.
func commitNoun(n int) string {
	if n == 1 {
		return "commit"
	}
	return "commits"
}

func pluralN(n int, noun string) string {
	switch {
	case n == 1:
		return "1 " + noun
	case strings.HasSuffix(noun, "ch"), strings.HasSuffix(noun, "s"), strings.HasSuffix(noun, "x"):
		return fmt.Sprintf("%d %ses", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// xRow is one bead of the tree with its drawn prefix.
type xRow struct {
	prefix string
	b      bd.Bead
}

// xTree lists the closing set as ADR 0313 §1 draws it: the head (the set's last bead)
// first, then under each bead its parent-child members and its caused-by predecessor,
// with ├/└. A bead the walk did not reach hangs off the head, after the walk.
func xTree(set []bd.Bead) []xRow {
	if len(set) == 0 {
		return nil
	}
	head := set[len(set)-1]
	seen := map[string]bool{head.ID: true}
	under := map[string][]bd.Bead{}
	var reach func(b bd.Bead)
	reach = func(b bd.Bead) {
		for _, x := range set {
			if !seen[x.ID] && (x.Parent == b.ID || x.ID == b.CausedBy) {
				seen[x.ID] = true
				under[b.ID] = append(under[b.ID], x)
				reach(x)
			}
		}
	}
	reach(head)
	for _, x := range set { // stragglers hang off the head once the walk is done
		if !seen[x.ID] {
			seen[x.ID] = true
			under[head.ID] = append(under[head.ID], x)
		}
	}
	rows := []xRow{{"", head}}
	var draw func(b bd.Bead, indent string)
	draw = func(b bd.Bead, indent string) {
		cs := under[b.ID]
		for i, c := range cs {
			branch, next := "├ ", indent+"│ "
			if i == len(cs)-1 {
				branch, next = "└ ", indent+"  "
			}
			rows = append(rows, xRow{indent + branch, c})
			draw(c, next)
		}
	}
	draw(head, "")
	return rows
}

// xTags is what a bead's row loses or keeps (ADR 0313 §1).
type xTags struct {
	files, commits       int
	atMost, held, rebase bool
}

func (t *xTags) tree(l withdraw.TreeLeftover) {
	switch {
	case l.Class == withdraw.Foreign:
		t.held = true
	case l.Rebasing:
		t.rebase = true
	}
	if l.Class != withdraw.Foreign {
		t.files += l.Dirty
	}
}

// tagsOf folds a bead's leftovers and its container branch into its tags.
func tagsOf(inv withdraw.Inventory, id string) (t xTags) {
	for _, b := range inv.Beads {
		if b.ID != id {
			continue
		}
		t.commits += b.Unintegrated
		t.atMost = t.atMost || b.AtMost
		for _, l := range b.Trees {
			t.tree(l)
		}
	}
	for _, c := range inv.Containers {
		if c.ID != id {
			continue
		}
		t.commits += c.Ahead
		if c.Tree != nil {
			t.tree(*c.Tree)
		}
	}
	return t
}

// add folds x into t.
func (t *xTags) add(x xTags) {
	t.files += x.files
	t.commits += x.commits
	t.atMost = t.atMost || x.atMost
	t.held = t.held || x.held
	t.rebase = t.rebase || x.rebase
}

// withTags is left with tags at the right edge of inner columns, left truncated to
// leave room for them.
func withTags(left, tags string, inner int) string {
	if tags == "" {
		return ansi.Truncate(left, inner, "…")
	}
	left = ansi.Truncate(left, max(inner-lipgloss.Width(tags)-2, 1), "…")
	return left + strings.Repeat(" ", max(inner-lipgloss.Width(left)-lipgloss.Width(tags), 1)) + tags
}

func (t xTags) none() bool { return t == xTags{} }

// String is the tags as a row ends: with words wide, glyph and count compact.
func (t xTags) String(wide bool) string {
	var ps []string
	count := fmt.Sprint(t.commits)
	if t.atMost {
		count = "≤" + count
	}
	if t.files > 0 {
		if wide {
			ps = append(ps, "✎ "+pluralN(t.files, "file")+" changed")
		} else {
			ps = append(ps, fmt.Sprintf("✎%d", t.files))
		}
	}
	if t.commits > 0 {
		if wide {
			ps = append(ps, "↑ "+count+" "+commitNoun(t.commits))
		} else {
			ps = append(ps, "↑"+count)
		}
	}
	if t.rebase {
		if wide {
			ps = append(ps, "⟳ rebase stopped")
		} else {
			ps = append(ps, "⟳")
		}
	}
	if t.held {
		if wide {
			ps = append(ps, "⊘ held: skipped")
		} else {
			ps = append(ps, "⊘")
		}
	}
	sep := " "
	if wide {
		sep = "  "
	}
	return strings.Join(ps, sep)
}

// xSummary is the one line under the tree: each tag's total.
func xSummary(inv withdraw.Inventory, set []bd.Bead) string {
	var all xTags
	var held, rebasing int
	for _, b := range set {
		t := tagsOf(inv, b.ID)
		all.add(t)
		if t.held {
			held++
		}
		if t.rebase {
			rebasing++
		}
	}
	var ps []string
	if all.files > 0 {
		ps = append(ps, "✎ "+pluralN(all.files, "file"))
	}
	if all.commits > 0 {
		n := fmt.Sprint(all.commits)
		if all.atMost {
			n = "≤" + n
		}
		ps = append(ps, "↑ "+n+" "+commitNoun(all.commits))
	}
	if rebasing > 0 {
		ps = append(ps, "⟳ "+pluralN(rebasing, "stopped rebase"))
	}
	lost := "Nothing is discarded"
	if len(ps) > 0 {
		lost = "Discards " + strings.Join(ps, " · ")
	}
	if held > 0 {
		lost += fmt.Sprintf(" · ⊘ %d held, skipped", held)
	}
	return lost + "."
}

// xLead is the line above the tree: the reason, that no successor follows, and the
// container branches that go with it.
func xLead(p withdraw.Plan) string {
	if len(p.Set) == 0 {
		return "The plan could not be read, so what this would close is unknown."
	}
	s := fmt.Sprintf("Closes %s as withdrawn. No successor follows", pluralN(len(p.Set), "bead"))
	var bs []string
	for _, c := range p.Inventory.Containers {
		bs = append(bs, c.Branch)
	}
	if len(bs) > 0 {
		s += "; " + strings.Join(bs, ", ") + " goes with it"
	}
	return s + "."
}

// xWidth is the dialog's outer width: the tree and its tags want more than a prompt.
func (m Model) xWidth() int { return min(max(60, m.w-4), 110, m.w-4) }

// xBindings are the hints: only the keys the dialog's state honours.
func (d dialog) xBindings() []key.Binding {
	if d.xs.cancel != nil {
		return d.cancelBindings()
	}
	switch {
	case d.xs.barred:
		return []key.Binding{keyBlocker, keyDown, keyUp, keyCloseDialog}
	case d.xs.plan.Inventory.Loses():
		return []key.Binding{keyDiscardWork, keyDown, keyUp, keyCloseDialog}
	}
	return []key.Binding{keyWithdrawYes, keyDown, keyUp, keyCloseDialog}
}

func wrapLines(s string, w int) []string {
	return strings.Split(lipgloss.NewStyle().Width(max(w, 1)).Render(s), "\n")
}

// xView draws the dialog: the lead, the tree windowed to the terminal with a k/N
// counter, the summary, and a warning or the blockers.
func (m Model) xView(d dialog) string {
	if d.xs.cancel != nil {
		return m.cancelView(d)
	}
	styleW := m.xWidth() - 2
	inner := styleW - 2
	wide := inner >= xCompact
	p := d.xs.plan
	head := []string{brand.Render(d.title), ""}
	if d.xs.barred {
		head = append(head, m.refusedHead(p, withdraw.KindWithdraw, inner)...)
	}
	lead := xLead(p)
	if d.xs.barred && len(p.Set) > 0 {
		lead = "Once clear: " + strings.ToLower(lead[:1]) + lead[1:]
	}
	for _, l := range wrapLines(lead, inner) {
		head = append(head, ifBarred(d.xs.barred, l))
	}
	head = append(head, "")

	var tail []string
	if !d.xs.barred {
		tail = append(tail, "")
		tail = append(tail, wrapLines(xSummary(p.Inventory, p.Set), inner)...)
		if d.xs.warn != "" {
			for _, l := range wrapLines(d.xs.warn, inner) {
				tail = append(tail, warnSt.Render(l))
			}
		}
	}
	hint := lipgloss.NewStyle().Width(styleW - 2).Render(flatHints(d.xBindings(), styleW-2))
	tail = append(tail, "", hint)

	tree := xTree(p.Set)
	budget := max(m.h-2-len(head)-len(tail), 1)
	lo, hi := 0, len(tree)
	scrolling := len(tree) > budget
	if scrolling {
		rows := max(budget-1, 1)
		lo = followOffset(len(tree), rows, d.cur)
		hi = lo + rows
	}
	cur := d.cur
	var body []string
	for i := lo; i < hi && i < len(tree); i++ {
		r := tree[i]
		tags := tagsOf(p.Inventory, r.b.ID)
		tagStr := ""
		if !tags.none() {
			tagStr = tags.String(wide)
		}
		line := withTags(r.prefix+kindTag(&r.b)+" "+r.b.Title, tagStr, inner)
		switch {
		case d.xs.barred:
			line = dimSt.Render(ansi.Strip(line))
		case i == cur:
			line = shadeRow(line, inner, pickRow)
		}
		body = append(body, line)
	}
	if scrolling {
		body = append(body, dimSt.Render(fmt.Sprintf("%d/%d", cur+1, len(tree))))
	}
	ls := append(append(head, body...), tail...)
	return m.placeBox(refBox.Width(styleW).Render(lipgloss.JoinVertical(lipgloss.Left, ls...)))
}

func ifBarred(barred bool, s string) string {
	if barred {
		return dimSt.Render(s)
	}
	return s
}

// refusedHead is the red "Refused" banner: one ✗ line per blocker with its fix under it.
func (m Model) refusedHead(p withdraw.Plan, k withdraw.Kind, inner int) []string {
	title := func(id string) string {
		if i := beadIndex(m.beads, id); i >= 0 {
			return m.beads[i].Title
		}
		return id
	}
	head := []string{warnSt.Bold(true).Render("Refused")}
	for _, r := range p.Refusals {
		var ts []string
		for _, id := range r.Beads {
			ts = append(ts, title(id))
		}
		line := "✗ " + r.Reason(k) + ": " + strings.Join(ts, ", ")
		if r.MR != "" {
			line += " (" + r.MR + ")"
		}
		for _, l := range wrapLines(line, inner) {
			head = append(head, warnSt.Render(l))
		}
		for _, l := range wrapLines("fix: "+r.Fix, inner-2) {
			head = append(head, "  "+l)
		}
	}
	return append(head, "")
}
