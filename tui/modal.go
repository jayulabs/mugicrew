package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The modal stack (ADR 0142 §7): the topmost dialog gets every key, so no shell key
// or digit reaches the frame under it (FR21, FR24).
type dialogKind int

const (
	confirm  dialogKind = iota // a title, body lines and a y/n pair
	input                      // one or two embedded line inputs
	picker                     // a j/k list
	form                       // the Signal form: enter does not submit
	surgery                    // the New Surgery form: a title and a target list
	loopMenu                   // the o menu: body and options come from the model's loop
)

// dialog is one modal. id is the bead pinned when it opened: act works on it, not
// on whatever is selected by the time the human answers, and runs only on
// confirm, submit or pick.
type dialog struct {
	kind        dialogKind
	title       string
	body        []string
	prompt      func(w int) []string // confirm: lines wrapped at the box's inner width when drawn, after body
	id          string
	labels      []string // input: one label per field
	inputs      []line
	field       int // input: the focused field
	items       []string
	cur         int // picker: the highlighted item
	act         func(m Model, d dialog) (Model, tea.Cmd)
	alt         func(m Model, d dialog) (Model, tea.Cmd) // confirm: d, a second action beside y
	altKey      key.Binding                              // confirm: alt's binding, keyAlt when unset
	noKey       key.Binding                              // confirm: the dismiss binding, keyNo when unset
	sf          signalForm                               // form
	recents     bool                                     // the Recents picker, marked by identity so R can pop it
	discussions bool                                     // the Robin picker, marked so D can pop it
	sessions    bool                                     // the Sessions picker, re-derived from the model on each refresh; id is its highlighted session
	heads       map[int]string                           // picker: a dim non-selectable heading drawn above item i (Sessions)
	afkAt       int                                      // Sessions: index of the first AFK row (hints follow the highlighted row's section)
	ids         []string                                 // picker: the bead id behind each item, when rows map to beads (Sessions)
	shut        key.Binding                              // picker: the dialog's own close key beside esc
	rowKeys     [][]string                               // picker: the keys that fire each row (parallel to items; a row may have several, or none)
	gotoMenu    bool                                     // the goto menu, marked by identity; opts are its option keys, shown as its hints
	newMenu     bool                                     // the New menu, marked by identity like gotoMenu
	opts        []key.Binding                            // picker: the goto menu's option keys
	sg          surgeryForm                              // surgery: the target list and its pick
	dropAct     func(m Model, d dialog) (Model, tea.Cmd) // picker: discussion-drop key on the highlighted row, behind its own dialog
}

func confirmDialog(title string, body []string, id string, act func(Model, dialog) (Model, tea.Cmd)) dialog {
	return dialog{kind: confirm, title: title, body: body, id: id, act: act}
}

// promptDialog is a confirm whose text depends on the box's width, so it is built
// when the modal renders, never at a constant.
func promptDialog(title string, prompt func(w int) []string, id string, act func(Model, dialog) (Model, tea.Cmd)) dialog {
	return dialog{kind: confirm, title: title, prompt: prompt, id: id, act: act}
}

func inputDialog(title string, labels []string, id string, act func(Model, dialog) (Model, tea.Cmd)) dialog {
	return dialog{kind: input, title: title, id: id, labels: labels, inputs: make([]line, len(labels)), act: act}
}

func pickerDialog(title string, items []string, id string, act func(Model, dialog) (Model, tea.Cmd)) dialog {
	return dialog{kind: picker, title: title, id: id, items: items, act: act}
}

var (
	keyYes    = key.NewBinding(key.WithKeys("y", "enter"), key.WithHelp("y/enter", "confirm"))
	keyNo     = key.NewBinding(key.WithKeys("n", "esc"), key.WithHelp("n/esc", "cancel"))
	keyAlt    = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "drop"))
	keySubmit = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "submit"))
	keyCancel = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))
	keyField  = key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field"))
	keyDown   = key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/k", "move"))
	keyUp     = key.NewBinding(key.WithKeys("k", "up"))
	keyPick   = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "pick"))
)

// dialogWidth is the outer width of a confirm, input or picker box: half the
// terminal clamped to 60–100 columns, never past a 2-column margin each side.
func (m Model) dialogWidth() int { return min(max(60, min(m.w/2, 100)), m.w-4) }

// pickerInner is the width a picker item may fill: the box less its border,
// padding and the 2-column indent.
func (m Model) pickerInner() int { return m.dialogWidth() - 2 - 2 - 2 }

func focusMark(focus bool) string {
	if focus {
		return brand.Render("▸ ")
	}
	return "  "
}

// stackBelow is the inner width under which a label: field row puts the field on its own line.
const stackBelow = 50

// stacked reports whether rows in a box of style width w stack: inner is w less the padding.
func stacked(w int) bool { return w-2 < stackBelow }

// fieldRow is one label: field row in a box of style width w. Wide, the field shares the
// label's line; stacked, it sits under it, indented by the focus marker and filling the rest.
func fieldRow(w int, focus bool, label string, field func(width int) string) []string {
	cur := focusMark(focus)
	if stacked(w) {
		return []string{cur + dimSt.Render(label), "  " + field(w-2-2)}
	}
	return []string{cur + dimSt.Render(label+" ") + field(w-2-2-lipgloss.Width(label+" "))}
}

// bindings are the keys the dialog handles: the hints it shows and the coverage
// test's source.
func (d dialog) bindings() []key.Binding {
	switch d.kind {
	case input:
		if len(d.inputs) > 1 {
			return []key.Binding{keySubmit, keyCancel, keyField}
		}
		return []key.Binding{keySubmit, keyCancel}
	case picker:
		if d.gotoMenu {
			return append([]key.Binding{keyDown, keyUp, keyPick, keyCancel}, d.opts...)
		}
		if d.newMenu {
			return append([]key.Binding{keyMoveUD, keyUp, keyPick, keyNewClose}, d.opts...)
		}
		if d.recents {
			return []key.Binding{keyDown, keyUp, keyJumpPick, keyRecentsClose}
		}
		if d.sessions {
			if len(d.items) > 0 && d.cur >= d.afkAt {
				return []key.Binding{keyDown, keyUp, keyTranscript, keySessAbort, keySessionsClose}
			}
			return []key.Binding{keyDown, keyUp, keySessAttach, keyTranscript, keyDiscussionDrop, keySessionsClose}
		}
		if d.discussions {
			return []key.Binding{keyDown, keyUp, keyPick, keyDiscussionDrop, keyDiscussionsClose}
		}
		return []key.Binding{keyDown, keyUp, keyPick, keyCancel}
	case loopMenu:
		return []key.Binding{keyCancel} // the options come from loopOpts (modalView's hint)
	case surgery:
		return []key.Binding{keySgFile, keyCancel, keyField, keySgMove, keySgJK}
	case form:
		return []key.Binding{keyFormSave, keyFormEdit, keyFormField, keyCancel}
	}
	if d.alt != nil {
		return []key.Binding{keyYes, d.altBinding(), d.noBinding()}
	}
	return []key.Binding{keyYes, d.noBinding()}
}

func (d dialog) altBinding() key.Binding {
	if len(d.altKey.Keys()) > 0 {
		return d.altKey
	}
	return keyAlt
}

func (d dialog) noBinding() key.Binding {
	if len(d.noKey.Keys()) > 0 {
		return d.noKey
	}
	return keyNo
}

// push opens d over the frame and whatever modals are already up.
func (m Model) push(d dialog) Model {
	m.modals = append(slices.Clone(m.modals), d)
	return m
}

// pop closes the top modal; once the stack is empty a result held behind it shows.
func (m Model) pop() Model {
	m.modals = slices.Clone(m.modals[:len(m.modals)-1])
	if len(m.modals) == 0 && m.deferred != "" {
		m.status, m.deferred = m.deferred, ""
	}
	return m
}

// modalKey routes a key to the top modal; it never falls through to the shell.
func (m Model) modalKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := m.modals[len(m.modals)-1]
	run := func(act func(Model, dialog) (Model, tea.Cmd)) (tea.Model, tea.Cmd) {
		m = m.pop()
		if act == nil {
			return m, nil
		}
		return act(m, d)
	}
	switch d.kind {
	case form:
		return m.formKey(d, k)
	case loopMenu:
		return m.loopMenuKey(k)
	case surgery:
		return m.surgeryKey(d, k)
	case confirm:
		switch {
		case key.Matches(k, keyYes):
			return run(d.act)
		case d.alt != nil && key.Matches(k, d.altBinding()):
			return run(d.alt)
		case key.Matches(k, d.noBinding()):
			return m.pop(), nil
		}
		return m, nil
	case input:
		switch {
		case key.Matches(k, keySubmit):
			return run(d.act)
		case key.Matches(k, keyCancel):
			return m.pop(), nil
		case key.Matches(k, keyField) && len(d.inputs) > 1:
			d.field = (d.field + 1) % len(d.inputs)
		default:
			d.inputs = slices.Clone(d.inputs)
			d.inputs[d.field] = d.inputs[d.field].apply(k)
		}
	case picker:
		switch {
		case key.Matches(k, keyCancel), key.Matches(k, d.shut):
			return m.pop(), nil
		case d.sessions && len(d.items) == 0:
			return m, nil
		case key.Matches(k, keyPick):
			return run(d.act)
		case slices.ContainsFunc(d.rowKeys, hasKey(k)):
			d.cur = slices.IndexFunc(d.rowKeys, hasKey(k))
			if d.ids != nil {
				d.id = d.ids[d.cur]
			}
			return run(d.act)
		case d.sessions && key.Matches(k, keyTranscript):
			return run(func(m Model, d dialog) (Model, tea.Cmd) { return into(m.push(d).transcript(d.id)) })
		case d.dropAct != nil && key.Matches(k, keyDiscussionDrop):
			return run(d.dropAct)
		case key.Matches(k, keyDown):
			d.cur = (d.cur + 1) % len(d.items)
		case key.Matches(k, keyUp):
			d.cur = (d.cur + len(d.items) - 1) % len(d.items)
		}
		if d.ids != nil {
			d.id = d.ids[d.cur]
		}
	}
	m.modals = slices.Clone(m.modals)
	m.modals[len(m.modals)-1] = d
	return m, nil
}

// hasKey matches a row's key list against k.
func hasKey(k tea.KeyMsg) func([]string) bool {
	return func(ks []string) bool { return slices.Contains(ks, k.String()) }
}

// modalView is the top modal centred in a rounded box, with only its own keys as hints.
func (m Model) modalView() string {
	d := m.modals[len(m.modals)-1]
	if d.kind == surgery {
		return m.surgeryView(d)
	}
	styleW := m.dialogWidth() - 2 // the outer width less the border; styleW-2 is the inner width
	if d.kind == form {
		styleW = m.formWidth() - 2
	}
	hint := lipgloss.NewStyle().Width(styleW - 2).Render(flatHints(d.bindings(), styleW-2))
	if d.kind == loopMenu {
		hint = m.loopOptsLine()
		d.body = m.loopMenuBody(styleW - 2)
	}
	ls := []string{brand.Render(d.title)}
	body := d.body
	if d.prompt != nil {
		body = append(slices.Clone(body), d.prompt(styleW-2)...)
	}
	if len(body) > 0 {
		ls = append(ls, "")
		ls = append(ls, body...)
	}
	switch d.kind {
	case form:
		ls = append(ls, "")
		ls = append(ls, m.formLines(d.sf, styleW)...)
	case input:
		ls = append(ls, "")
		for i, in := range d.inputs {
			ls = append(ls, fieldRow(styleW, i == d.field, d.labels[i]+":", in.render)...)
		}
	case picker:
		// only the picker needs a fixed row count, so only its lines are cut to the
		// inner width (less the padding); confirm and input lines wrap
		for i, l := range ls {
			ls[i] = ansi.Truncate(l, styleW-2, "…")
		}
		ls = append(ls, "")
		// the box's chrome: two borders, the blank before the hints and the
		// (wrapped, never cut) hint lines
		budget := max(m.h-len(ls)-3-lipgloss.Height(hint), 1)
		lo, hi := 0, len(d.items)
		if hi+len(d.heads) > budget {
			budget -= len(d.heads)   // ponytail: reserves every heading's line, even one scrolled out of view
			rows := max(budget-1, 1) // items only: one row goes to the k/N counter
			lo = followOffset(hi, rows, d.cur)
			hi = lo + rows
		}
		for i := lo; i < hi; i++ {
			if h, ok := d.heads[i]; ok {
				ls = append(ls, dimSt.Render("  "+h))
			}
			if i == d.cur {
				ls = append(ls, shadeRow(ansi.Truncate("  "+d.items[i], styleW-2, "…"), styleW-2, pickRow))
			} else {
				ls = append(ls, ansi.Truncate("  "+d.items[i], styleW-2, "…"))
			}
		}
		if lo > 0 || hi < len(d.items) {
			ls = append(ls, dimSt.Render(fmt.Sprintf("  %d/%d", d.cur+1, len(d.items))))
		}
	}
	ls = append(ls, "", hint)
	return m.placeBox(refBox.Width(styleW).Render(lipgloss.JoinVertical(lipgloss.Left, ls...)))
}

// placeBox draws box centred over the frame, so the header and status line stay
// visible, in exactly w×h.
func (m Model) placeBox(box string) string {
	base := strings.Split(m.frame(), "\n")
	bw, bh := lipgloss.Width(box), lipgloss.Height(box)
	x, y := max((m.w-bw)/2, 0), max((m.h-bh)/2, 0)
	for i, l := range strings.Split(box, "\n") {
		if y+i >= len(base) {
			break
		}
		row := base[y+i]
		base[y+i] = ansi.Truncate(row, x, "") + strings.Repeat(" ", max(x-ansi.StringWidth(row), 0)) + l + ansi.TruncateLeft(row, x+bw, "")
	}
	return fit(strings.Join(base, "\n"), m.w, m.h)
}
