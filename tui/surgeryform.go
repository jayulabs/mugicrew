package tui

// The New Surgery form (ADR 0234): a title and a verbless target list. A Checkpoint row
// joins it, an Initiative row files a new Checkpoint in it, and + New standalone
// Checkpoint files one in none. The New menu's u row opens it.

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

type sgKind int

const (
	sgGap        sgKind = iota // a blank, non-selectable row between blocks
	sgJoin                     // an open Checkpoint
	sgNewIn                    // an open Initiative: a new Checkpoint in it
	sgStandalone               // a new Checkpoint in no Initiative
)

// sgRow is one target row. b is the Checkpoint (sgJoin) or Initiative (sgNewIn); why is
// the Checkpoint's JoinVerdict refusal, and surgery the open Surgery behind it, if any.
type sgRow struct {
	kind    sgKind
	b       *bd.Bead
	nested  bool // a Checkpoint listed under its Initiative's row
	why     bd.JoinRefusal
	surgery string
}

func (r sgRow) selectable() bool { return r.kind != sgGap }

// short is why as the row's ⊘ tag.
func (r sgRow) short() string {
	if r.surgery != "" {
		return "surgery " + r.surgery + " open"
	}
	if t := r.why.Tag(); t != "" {
		return t
	}
	return string(r.why)
}

// surgeryForm is the form's target state; the title is the dialog's one input.
type surgeryForm struct {
	rows []sgRow
	cur  int
	err  string
}

// surgeryRows builds the target list from a snapshot, with no subprocess: one
// bd.JoinVerdict per open Checkpoint. Closed Checkpoints are never listed; a Checkpoint
// is under an Initiative only when its Parent is an unclosed one, the rule adoptTargets lists rows by.
func surgeryRows(beads []bd.Bead, required bool) []sgRow {
	byID := map[string]*bd.Bead{}
	kids := map[string][]bd.Bead{}
	for i := range beads {
		byID[beads[i].ID] = &beads[i]
		kids[beads[i].Parent] = append(kids[beads[i].Parent], beads[i])
	}
	type join struct {
		sgRow
		ini string
	}
	var joins []join
	for i := range beads {
		cp := &beads[i]
		if cp.Kind != contract.KindCheckpoint || !cp.Open() {
			continue
		}
		var pivots []bd.Bead
		for _, id := range cp.BlockedBy {
			if b := byID[id]; b != nil && b.Kind == contract.KindPivot && b.Open() {
				pivots = append(pivots, *b)
			}
		}
		const resume = "enter to resume"
		j := join{sgRow: sgRow{kind: sgJoin, b: cp, why: bd.JoinVerdict(*cp, kids[cp.ID], pivots, resume)}}
		if j.why.Resumes() {
			for _, k := range kids[cp.ID] {
				if k.Open() && k.Kind == contract.KindSurgery {
					j.surgery = k.ID
					break
				}
			}
		}
		if p := byID[cp.Parent]; p != nil && p.Kind == contract.KindInitiative && p.Unclosed() {
			j.ini = p.ID
		}
		joins = append(joins, j)
	}
	slices.SortStableFunc(joins, func(a, b join) int { return beadCmp(*a.b, *b.b) })

	var rows []sgRow
	gap := sgRow{kind: sgGap}
	// block adds the Checkpoints of one Initiative (or none), eligible before refused.
	block := func(ini string) {
		for _, refused := range []bool{false, true} {
			for _, j := range joins {
				if j.ini == ini && (j.why != "") == refused {
					j.nested = ini != ""
					rows = append(rows, j.sgRow)
				}
			}
		}
	}
	if !required {
		rows = append(rows, sgRow{kind: sgStandalone}, gap)
	}
	n := len(rows)
	block("")
	if len(rows) > n {
		rows = append(rows, gap)
	}
	for _, ini := range adoptTargets(beads) {
		rows = append(rows, sgRow{kind: sgNewIn, b: byID[ini.ID]})
		block(ini.ID)
	}
	return rows
}

// surgeryCursor is the form's default pick: the Checkpoint the menu opened over (even
// refused), else the Initiative's row, else the first selectable row.
func surgeryCursor(rows []sgRow, cp, ini string) int {
	for _, want := range []func(sgRow) bool{
		func(r sgRow) bool { return cp != "" && r.kind == sgJoin && r.b.ID == cp },
		func(r sgRow) bool { return ini != "" && r.kind == sgNewIn && r.b.ID == ini },
		sgRow.selectable,
	} {
		if i := slices.IndexFunc(rows, want); i >= 0 {
			return i
		}
	}
	return 0
}

// surgeryContext is the Checkpoint and Initiative the human is over, captured when the
// New menu opens: the selected Checkpoint or a direct member's, else the focus view's
// or the cursor's Initiative.
func (m Model) surgeryContext() (cp, ini string) {
	if b := m.selection(); b != nil {
		if b.Kind == contract.KindCheckpoint {
			return b.ID, ""
		}
		if i := beadIndex(m.beads, b.Parent); i >= 0 && m.beads[i].Kind == contract.KindCheckpoint {
			return b.Parent, ""
		}
	}
	if m.focus.id != "" {
		return "", m.focus.id
	}
	if i := m.resolveInitiative(); i != nil {
		return "", i.ID
	}
	return "", ""
}

// openSurgery is the New menu's u row: the form over the snapshot, cursor by context.
func (m Model) openSurgery(cp, ini string) (Model, tea.Cmd) {
	if m.refusePending(slot("new Surgery")) {
		return m, nil
	}
	rows := surgeryRows(m.beads, m.initiatives.Required)
	if !slices.ContainsFunc(rows, sgRow.selectable) { // a reload emptied the list after the menu opened
		m.setStatus("no Surgery target")
		return m, nil
	}
	d := inputDialog("New Surgery", []string{"title"}, "", nil)
	d.kind = surgery
	d.sg = surgeryForm{rows: rows, cur: surgeryCursor(rows, cp, ini)}
	return m.push(d), nil
}

var (
	keySgMove = key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "target"))
	keySgJK   = key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "target (in list)"))
	keySgDown = key.NewBinding(key.WithKeys("down"))
	keySgUp   = key.NewBinding(key.WithKeys("up"))
	keySgFile = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "file"))
)

// surgeryKey is the form's keys: tab toggles title/target, ↑/↓ move the pick from either
// field, j/k only while target has focus, blank rows are skipped.
func (m Model) surgeryKey(d dialog, k tea.KeyMsg) (tea.Model, tea.Cmd) {
	move := func(dir int) {
		for i := d.sg.cur + dir; i >= 0 && i < len(d.sg.rows); i += dir {
			if d.sg.rows[i].selectable() {
				d.sg.cur = i
				return
			}
		}
	}
	d.sg.err = ""
	switch {
	case key.Matches(k, keyCancel):
		return m.pop(), nil
	case key.Matches(k, keySgFile):
		return m.surgerySubmit(d)
	case key.Matches(k, keyField):
		d.field = 1 - d.field
	case key.Matches(k, keySgDown), d.field == 1 && key.Matches(k, keyDown):
		move(1)
	case key.Matches(k, keySgUp), d.field == 1 && key.Matches(k, keyUp):
		move(-1)
	case d.field == 0:
		d.inputs = slices.Clone(d.inputs)
		d.inputs[0] = d.inputs[0].apply(k)
	}
	return m.setTopModal(d), nil
}

// surgerySubmit is enter: a refused row shows its reason (or resumes its open Surgery),
// an empty title is refused, else the row's target files.
func (m Model) surgerySubmit(d dialog) (tea.Model, tea.Cmd) {
	r := d.sg.rows[d.sg.cur]
	title := strings.TrimSpace(d.inputs[0].text)
	fail := func(msg string) (tea.Model, tea.Cmd) {
		d.sg.err = msg
		return m.setTopModal(d), nil
	}
	switch {
	case r.surgery != "":
		m = m.pop()
		i := beadIndex(m.beads, r.surgery)
		if i < 0 {
			m.setStatus(r.surgery + " is no longer open")
			return m, nil
		}
		return m.launchAs(m.gateData(), m.beads[i], false)
	case r.why != "":
		return fail(string(r.why))
	case title == "":
		return fail("create: title required")
	}
	m = m.pop()
	join, ini := "", ""
	switch r.kind {
	case sgJoin:
		join = r.b.ID
	case sgNewIn:
		ini = r.b.ID
	}
	return m.fileSurgery(slot("new Surgery"), title, d, join, ini)
}

var (
	sgPickSt  = lipgloss.NewStyle().Foreground(lipgloss.Color("114")).Bold(true)
	sgStandSt = lipgloss.NewStyle().Foreground(lipgloss.Color("213"))
	sgHintSt  = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("15"))
)

// surgeryRow draws r in inner columns: SBRN with kind tags padded to kw, a refused
// Checkpoint dimmed with its ⊘ reason.
func (m Model) surgeryRow(r sgRow, kw, inner int) string {
	switch r.kind {
	case sgGap:
		return ""
	case sgStandalone:
		return sgStandSt.Render("+ New standalone Checkpoint")
	}
	pre, suf := "", ""
	if r.nested {
		pre = "  "
	}
	if r.why != "" {
		suf = " ⊘ " + r.short()
	}
	head, tail := m.gateData().sbrnParts(r.b, false, kw)
	title := ansi.Truncate(r.b.Title, max(inner-2-lipgloss.Width(pre+head+tail+suf), 4), "…")
	row := pre + head + title + tail + suf
	if r.why != "" {
		return dimSt.Render(ansi.Strip(row))
	}
	return row
}

// surgeryView draws the form. Its height depends only on the terminal and the list, so
// the box never resizes as the pick moves or a message comes and goes.
func (m Model) surgeryView(d dialog) string {
	styleW := min(max(m.dialogWidth(), min(m.w-8, 100)), m.w-4) - 2
	inner := styleW - 2
	hint := lipgloss.NewStyle().Width(inner).Render(flatHints(d.bindings(), inner))
	ls := []string{brand.Render(d.title), ""}
	ls = append(ls, fieldRow(styleW, d.field == 0, "title:", d.inputs[0].render)...)
	ls = append(ls, focusMark(d.field == 1)+dimSt.Render("target:"))
	rows := d.sg.rows
	// the chrome below the list: two borders, a blank and the message, a blank, the hints
	budget := max(m.h-len(ls)-2-2-1-lipgloss.Height(hint), 3)
	lo, hi := 0, len(rows)
	if hi > budget {
		lo = followOffset(hi, budget-1, d.sg.cur)
		hi = lo + budget - 1
	}
	kw := 0
	for _, k := range []contract.Kind{contract.KindCheckpoint, contract.KindInitiative} {
		kw = max(kw, lipgloss.Width(kindTag(&bd.Bead{Kind: k, Container: true})))
	}
	for i := lo; i < hi; i++ {
		mark := "  "
		if i == d.sg.cur {
			mark = sgPickSt.Render("✓ ")
		}
		l := ansi.Truncate(mark+m.surgeryRow(rows[i], kw, inner), inner, "…")
		if i == d.sg.cur {
			l = shadeRow(l, inner, pickRow)
		}
		ls = append(ls, l)
	}
	if lo > 0 || hi < len(rows) {
		pos, n := 0, 0 // the counter counts selectable rows, not the blank gaps
		for i, r := range rows {
			if r.selectable() {
				n++
				if i <= d.sg.cur {
					pos = n
				}
			}
		}
		ls = append(ls, dimSt.Render(fmt.Sprintf("  %d/%d", pos, n)))
	}
	msg := ""
	switch r := rows[d.sg.cur]; {
	case d.sg.err != "":
		msg = warnSt.Render(ansi.Truncate(d.sg.err, inner, "…"))
	case r.kind == sgNewIn:
		msg = sgHintSt.Render(ansi.Truncate("this will create a new Checkpoint in "+r.b.ID, inner, "…"))
	case r.why != "":
		msg = warnSt.Render(ansi.Truncate(string(r.why), inner, "…"))
	}
	ls = append(ls, "", msg, "", hint)
	return m.placeBox(refBox.Width(styleW).Render(lipgloss.JoinVertical(lipgloss.Left, ls...)))
}
