package tui

// The Signal form (ADR 0148, ADR 0149 §5–6): one centred box that edits a Signal's
// title, priority and description, shared by Graph's n and e. Enter does not submit
// it (the description is multi-line); ctrl+s saves. An edit writes only the fields the
// human changed, and refuses when another writer changed one of those since it opened.

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
)

var (
	keyFormSave  = key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save"))
	keyFormEdit  = key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("ctrl+o", "$EDITOR"))
	keyFormField = key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab", "field"))
)

const (
	defaultPrio = 2
	descIndent  = 2  // the field marker's columns: the textarea starts under the description: label
	formChrome  = 10 // box rows besides the description and the hint lines
)

const (
	fieldTitle = iota
	fieldPrio
	fieldDesc
	fieldRef // only on an Initiative form under Enforce external ID
)

// sigVals are the three fields in the form of the comparison: trimmed as bd reads them back.
type sigVals struct {
	title string
	prio  int
	desc  string
}

func (v sigVals) norm() sigVals {
	v.title, v.desc = strings.TrimSpace(v.title), strings.TrimRight(v.desc, "\n")
	return v
}

func valsOf(b bd.Bead) sigVals { return sigVals{b.Title, b.Priority, b.Description}.norm() }

// signalForm is the form's state. id is "" for a create; snap is what the form opened
// with (for create, empty at P2).
type signalForm struct {
	id      string
	title   line
	prio    int
	desc    textarea.Model
	field   int
	snap    sigVals
	raw     string // the bead's description as bd holds it, for the clash check
	err     string
	noun    string                                        // "Signal" or "Initiative": what a create files
	create  func(title, desc, ref string) (string, error) // the create seam, captured when the form opened
	withRef bool                                          // the form has a required ref field
	ref     line
}

// order is the tab order, matching the drawn order of the fields.
func (f signalForm) order() []int {
	if f.withRef {
		return []int{fieldTitle, fieldRef, fieldPrio, fieldDesc}
	}
	return []int{fieldTitle, fieldPrio, fieldDesc}
}

func signalFormDialog(id string, v sigVals) dialog {
	return formDialog(id, v, "Signal", func(t, d, _ string) (string, error) { return newSignal(t, d) })
}

func formDialog(id string, v sigVals, noun string, create func(string, string, string) (string, error)) dialog {
	ta := textarea.New()
	ta.ShowLineNumbers, ta.Prompt, ta.CharLimit, ta.MaxHeight = false, "", 0, 0
	ta.Cursor.SetMode(cursor.CursorStatic) // the shell routes no blink messages
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.SetValue(v.desc)
	f := signalForm{id: id, title: lineAt(v.title), prio: v.prio, desc: ta, raw: v.norm().desc, noun: noun, create: create}
	// the textarea sanitises what it takes in (tabs become spaces), so snap what it holds
	f.snap = f.vals()
	title := "New " + noun
	if id != "" {
		title = "Edit " + id
	}
	return dialog{kind: form, title: title, id: id, sf: f}
}

func newSignalForm() dialog { return signalFormDialog("", sigVals{prio: defaultPrio}) }

// newInitiativeForm is the Initiatives tab's create form; withRef adds the ref field
// Enforce external ID requires, checked unique before the create.
func newInitiativeForm(withRef bool) dialog {
	d := formDialog("", sigVals{prio: defaultPrio}, "Initiative", func(t, desc, ref string) (string, error) {
		// late-bound, so a test's seam swapped after the form opened still applies
		if ref != "" {
			// ponytail: check-then-create, no lock for the accepted race
			hits, err := initiativesByRef(ref)
			if err != nil {
				return "", err
			}
			if len(hits) > 0 {
				return "", fmt.Errorf("ref %q is already carried by Initiative %s", ref, hits[0].ID)
			}
		}
		return newInitiative(t, desc, ref)
	})
	d.sf.withRef = withRef
	return d
}

func editSignalForm(b bd.Bead) dialog {
	return signalFormDialog(b.ID, sigVals{b.Title, b.Priority, b.Description})
}

// stepPrio moves the priority one step: stepUp toward P0, stepDown toward P4, clamped.
func (f signalForm) stepPrio(side int) signalForm {
	switch side {
	case stepUp:
		f.prio = max(f.prio-1, 0)
	case stepDown:
		f.prio = min(f.prio+1, 4)
	}
	return f
}

func (f signalForm) vals() sigVals { return sigVals{f.title.text, f.prio, f.desc.Value()}.norm() }

func (f signalForm) dirty() bool { return f.vals() != f.snap || strings.TrimSpace(f.ref.text) != "" }

// formWidth is the box's outer width: three fifths of the terminal clamped to
// 72–120 columns, never past a 2-column margin each side.
func (m Model) formWidth() int { return min(max(72, min(m.w*3/5, 120)), m.w-4) }

// formTextMax is the description's most rows: two fifths of the height, 10–30.
func (m Model) formTextMax() int { return max(10, min(m.h*2/5, 30)) }

// stackedRows are the extra rows stacking adds: title and priority each gain one.
func stackedRows(w int) int {
	if stacked(w) {
		return 2
	}
	return 0
}

// fit sizes the description to the terminal, as the picker budgets its rows.
func (f signalForm) fit(w, h, hintH, textMax int) signalForm {
	f.desc.SetWidth(max(w-2-descIndent, 1))
	chrome := formChrome + stackedRows(w)
	if f.withRef {
		chrome += 1 + stackedRows(w)/2
	}
	f.desc.SetHeight(min(max(h-chrome-hintH, 1), textMax))
	return f
}

func (m Model) formHintH(w int) int {
	return lipgloss.Height(lipgloss.NewStyle().Width(w - 2).Render(shortHelpView(helpable(dialog{kind: form}.bindings()))))
}

func (m Model) fitForm(f signalForm) signalForm {
	w := m.formWidth() - 2 // the style's width, less the border
	return f.fit(w, m.h, m.formHintH(w), m.formTextMax())
}

// formClosed reports whether the form's bead is closed in the current snapshot.
func (m Model) formClosed(f signalForm) bool {
	b, ok := m.pinned(f.id)
	return f.id != "" && ok && b.Status == "closed"
}

// setTopModal stores d back as the top modal.
func (m Model) setTopModal(d dialog) Model {
	m = m.pop()
	return m.push(d)
}

// formKey routes a key to the open form; it never falls through to the shell.
func (m Model) formKey(d dialog, k tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.fitForm(d.sf)
	f.err = ""
	focus := func(step int) {
		o := f.order()
		f.field = o[(slices.Index(o, f.field)+step+len(o))%len(o)]
		if f.field == fieldDesc {
			f.desc.Focus()
		} else {
			f.desc.Blur()
		}
	}
	switch {
	case key.Matches(k, keyFormSave):
		d.sf = f
		return m.saveForm(d)
	case key.Matches(k, keyFormEdit):
		d.sf = f
		return m.editInEditor(d)
	case key.Matches(k, keyCancel):
		if !f.dirty() {
			return m.pop(), nil
		}
		return m.push(confirmDialog("Discard changes?", []string{"Your edits to this " + f.noun + " will be lost."}, "", func(m Model, _ dialog) (Model, tea.Cmd) {
			return m.pop(), nil // the confirm is already popped: this closes the form
		})), nil
	case k.Type == tea.KeyTab:
		focus(1)
	case k.Type == tea.KeyShiftTab:
		focus(-1)
	case k.Type == tea.KeyEnter && f.field != fieldDesc:
		focus(1)
	case f.field == fieldTitle:
		f.title = f.title.apply(k)
	case f.field == fieldRef:
		f.ref = f.ref.apply(k)
	case f.field == fieldPrio:
		f = f.stepPrio(stepOf(k.String()))
	default:
		f.desc, _ = f.desc.Update(k) // ponytail: the paste command is dropped
	}
	d.sf = f
	return m.setTopModal(d), nil
}

// editorDoneMsg carries the editor's result back to the form that launched it.
type editorDoneMsg struct {
	id, noun string
	text     string
	err      error
}

// editorCmd runs $VISUAL, else $EDITOR, else vi, through sh so values with arguments work;
// the path is $1, never spliced into the command.
func editorCmd(path string) *exec.Cmd {
	ed := "vi"
	for _, v := range []string{"VISUAL", "EDITOR"} {
		if e := os.Getenv(v); e != "" {
			ed = e
			break
		}
	}
	return exec.Command("sh", "-c", ed+` "$1"`, "sh", path)
}

// editInEditor is ctrl+o: the description goes to a temp .md file, the editor runs with
// the TUI suspended, and editorDoneMsg brings the text back. Nothing is saved.
func (m Model) editInEditor(d dialog) (tea.Model, tea.Cmd) {
	fail := func(err error) (tea.Model, tea.Cmd) {
		d.sf.err = err.Error()
		return m.setTopModal(d), nil
	}
	tmp, err := os.CreateTemp("", "mugicrew-*.md")
	if err != nil {
		return fail(err)
	}
	path := tmp.Name()
	_, err = tmp.WriteString(d.sf.desc.Value())
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return fail(err)
	}
	id, noun := d.sf.id, d.sf.noun
	return m.setTopModal(d), tea.ExecProcess(editorCmd(path), func(runErr error) tea.Msg {
		b, readErr := os.ReadFile(path)
		os.Remove(path)
		if runErr == nil {
			runErr = readErr
		}
		return editorDoneMsg{id: id, noun: noun, text: string(b), err: runErr}
	})
}

// editorDone applies the editor's text to the form that launched it, if it is still on top.
func (m Model) editorDone(msg editorDoneMsg) Model {
	if n := len(m.modals); n > 0 && m.modals[n-1].kind == form && m.modals[n-1].sf.id == msg.id && m.modals[n-1].sf.noun == msg.noun {
		d := m.modals[n-1]
		if msg.err != nil {
			d.sf.err = "editor: " + msg.err.Error()
		} else {
			d.sf.desc.SetValue(msg.text)
			d.sf.err = ""
		}
		return m.setTopModal(d)
	}
	m.setStatus("editor result dropped: the form is closed")
	return m
}

// saveForm is ctrl+s: refuse an empty title or a closed bead, then create or edit.
func (m Model) saveForm(d dialog) (tea.Model, tea.Cmd) {
	f := d.sf
	v := f.vals()
	refuse := func(why string) (tea.Model, tea.Cmd) {
		d.sf.err = why
		return m.setTopModal(d), nil
	}
	switch {
	case v.title == "":
		if f.id == "" {
			return refuse("create: title required")
		}
		return refuse("edit: title required")
	case f.id == "" && f.withRef && strings.TrimSpace(f.ref.text) == "":
		return refuse("create: ref required")
	case f.id == "":
		return m.createSignal(d, v)
	case m.formClosed(f):
		return refuse(f.id + " is closed")
	case v == f.snap:
		return m.pop(), nil
	}
	return m.editSignal(d, v)
}

func (m Model) createSignal(d dialog, v sigVals) (tea.Model, tea.Cmd) {
	pend := slot("new " + d.sf.noun)
	if m.refusePending(pend) {
		return m, nil
	}
	m = m.pop()
	return m.startWrite(pend, "filing", func() writeDoneMsg {
		id, err := d.sf.create(v.title, v.desc, strings.TrimSpace(d.sf.ref.text))
		if err == nil && v.prio != defaultPrio {
			if perr := updateBead(id, []string{"--priority", strconv.Itoa(v.prio)}); perr != nil {
				return writeDoneMsg{pend, func(m Model) (tea.Model, tea.Cmd) {
					m.setStatus("create: filed " + d.sf.noun + " " + id + " but its priority was not set: " + perr.Error())
					return m, tea.Batch(m.load(), m.recordRecent(id))
				}}
			}
		}
		return writeDoneMsg{pend, func(m Model) (tea.Model, tea.Cmd) {
			if err != nil {
				return m.retry(d, "create", err)
			}
			next, cmd := m.doneOrErr("create", "filed "+d.sf.noun+" "+id, nil)
			return next, tea.Batch(cmd, m.recordRecent(id))
		}}
	})
}

// editSignal re-reads the bead off the event loop and writes only the changed fields,
// refusing if another writer changed one of them (values compared, not timestamps).
func (m Model) editSignal(d dialog, v sigVals) (tea.Model, tea.Cmd) {
	id, snap, raw := d.id, d.sf.snap, d.sf.raw
	if m.refusePending(id) {
		return m, nil
	}
	m = m.pop()
	return m.startAct(id, "updating", func() writeDoneMsg {
		err := func() error {
			cur, err := showBead(id)
			if err != nil {
				return err
			}
			if cur.Status == "closed" {
				return fmt.Errorf("%s is closed", id)
			}
			now := valsOf(cur)
			var args []string
			if v.title != snap.title {
				if now.title != snap.title {
					return fmt.Errorf("title changed elsewhere")
				}
				args = append(args, "--title", v.title)
			}
			if v.prio != snap.prio {
				if now.prio != snap.prio {
					return fmt.Errorf("priority changed elsewhere")
				}
				args = append(args, "--priority", strconv.Itoa(v.prio))
			}
			if v.desc != snap.desc {
				if now.desc != raw {
					return fmt.Errorf("description changed elsewhere")
				}
				args = append(args, "--description", v.desc)
			}
			return updateBead(id, args)
		}()
		return writeDoneMsg{id, func(m Model) (tea.Model, tea.Cmd) {
			if err != nil {
				m.setStatus("edit: " + err.Error()) // status too: the form is pushed after, which defers it
				return m.retry(d, "edit", err)
			}
			return m.doneOrErr("edit", "updated "+id, nil)
		}}
	})
}

// formField is where one form field is drawn, in rows of the box's content (below its
// top border): row is its first row (the label), text the row its one-line text sits on
// (-1 for the description and priority, which a click only focuses), col and w the text's
// start column within the content and the width it renders in.
type formField struct {
	field, row, rows, text, col, w int
	stepped                        bool          // the field draws a ◀ P2 ▶ stepper on its last row, at col
	steps                          [2]stepTarget // its ◀ and ▶, columns relative to col
}

// textField is the layout of a one-line field drawn by fieldRow(w, …, label, …) as rows
// rows from row.
func textField(id, w int, label string, row, rows int) formField {
	ff := formField{field: id, row: row, rows: rows, text: row + rows - 1, col: 2, w: w - 2 - 2}
	if !stacked(w) {
		ff.col += lipgloss.Width(label + " ")
		ff.w -= lipgloss.Width(label + " ")
	}
	return ff
}

// formLayout is the drawn form box: its screen origin and size, and each field's rows.
type formLayout struct {
	x, y, w, h int
	fields     []formField
}

// fieldAt is the field whose rows hold screen row y, if any.
func (lay formLayout) fieldAt(y int) (formField, bool) {
	for _, f := range lay.fields {
		if r := y - lay.y - 1 - f.row; r >= 0 && r < f.rows {
			return f, true
		}
	}
	return formField{}, false
}

// formLines are the form's body rows inside the box, w wide.
func (m Model) formLines(f signalForm, w int) []string {
	ls, _ := m.formRows(f, w)
	return ls
}

// formRows are formLines with where each field sits among them (row 0 is the first line).
func (m Model) formRows(f signalForm, w int) ([]string, []formField) {
	f = m.fitForm(f)
	msg := f.err
	if m.formClosed(f) {
		msg = f.id + " was closed: ctrl+s is refused, esc to leave"
	}
	var ls []string
	var fs []formField
	add := func(id int, label string, field func(int) string, oneLine bool) {
		rows := fieldRow(w, f.field == id, label, field)
		ff := formField{field: id, row: len(ls), rows: len(rows), text: -1}
		if oneLine {
			ff = textField(id, w, label, len(ls), len(rows))
		}
		ls, fs = append(ls, rows...), append(fs, ff)
	}
	add(fieldTitle, "title:", f.title.render, true)
	if f.withRef {
		add(fieldRef, "ref:", f.ref.render, true)
	}
	stepping := f.field == fieldPrio && m.modalCaptured()
	var prioSteps [2]stepTarget
	add(fieldPrio, "priority:", func(int) string {
		if !stepping {
			return prio(f.prio)
		}
		var s string
		s, prioSteps = stepper(prio(f.prio), f.prio == 0, f.prio == 4, m.hoverSide())
		return s
	}, false)
	if stepping { // the same stepper call draws the arrows and lays out their targets
		ff := &fs[len(fs)-1]
		tf := textField(fieldPrio, w, "priority:", ff.row, ff.rows)
		ff.steps = prioSteps
		ff.stepped, ff.col = true, tf.col
	}
	desc := formField{field: fieldDesc, row: len(ls), text: -1}
	ls = append(ls, focusMark(f.field == fieldDesc)+dimSt.Render("description:"))
	for _, l := range strings.Split(f.desc.View(), "\n") {
		ls = append(ls, strings.Repeat(" ", descIndent)+l)
	}
	desc.rows = len(ls) - desc.row
	fs = append(fs, desc)
	ls = append(ls, ansi.Truncate(warnSt.Render(msg), w-2, "…"))
	if m.h-formChrome-stackedRows(w)-m.formHintH(w) < 1 {
		return ls, fs // ponytail: too short for even one description row, so the footnote goes
	}
	return append(ls, dimSt.Render("labels and deps: E after saving")), fs
}

// stepAt is the priority arrow under screen cell (x, y) of the drawn form, if any.
func (lay formLayout) stepAt(x, y int) (stepTarget, bool) {
	for _, f := range lay.fields {
		if !f.stepped || y-lay.y-1 != f.row+f.rows-1 {
			continue
		}
		c := x - lay.x - 2 - f.col
		for _, t := range f.steps {
			if c >= t.hx0 && c < t.hx1 {
				return t, true
			}
		}
	}
	return stepTarget{}, false
}

// formHover is the hover for a pointer at (x, y): set only over a live priority arrow, so
// the motion filter passes just the motion that changes the shaded arrow.
func (m Model) formHover(x, y int) hover {
	d := m.modals[len(m.modals)-1]
	if sp, ok := m.hintAt(x, y); ok {
		return hover{set: true, x: x, y: y, opt: true, optSpan: sp}
	}
	if d.kind != form {
		return hover{}
	}
	if t, ok := m.dialogLayout(d).stepAt(x, y); ok && !t.inert {
		return hover{set: true, x: x, y: y, step: t.key}
	}
	return hover{}
}

// formMouse is the open form's mouse path: a left press focuses the field under it, as tab
// does, and puts the cursor of a one-line field or the description on the cell it lands on;
// the wheel over the description scrolls it. It never edits: keys, saves, the dirty check
// and esc stay on the dialog's key path. Everything else is consumed.
func (m Model) formMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action == tea.MouseActionMotion {
		m.hover = m.formHover(msg.X, msg.Y)
		return m, nil
	}
	wheel := isWheel(msg)
	if !wheel && (msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft) {
		return m, nil
	}
	d := m.modals[len(m.modals)-1]
	lay := m.dialogLayout(d)
	if msg.X < lay.x || msg.X >= lay.x+lay.w || msg.Y < lay.y || msg.Y >= lay.y+lay.h {
		return m, nil
	}
	if sp, ok := m.hintAt(msg.X, msg.Y); ok && !wheel { // an answering hint presses its key
		return m.sendKey(keyMsgOf(sp.key))
	}
	ff, ok := lay.fieldAt(msg.Y)
	if !ok {
		return m, nil
	}
	if t, ok := lay.stepAt(msg.X, msg.Y); ok && !wheel && d.kind == form && d.sf.field == fieldPrio {
		if !t.inert { // a press on a focused field's arrow steps it and keeps focus
			f := d.sf
			// the arrows point at the number, the keys at the priority: ◀ (key "-") lowers P, which + does
			d.sf = f.stepPrio(stepOf(map[string]string{"-": "+", "+": "-"}[t.key]))
			m = m.setTopModal(d)
			m.hover = m.formHover(msg.X, msg.Y)
		}
		return m, nil
	}
	col := -1
	if msg.Y-lay.y-1 == ff.text {
		col = msg.X - lay.x - 2 - ff.col
	}
	if d.kind != form {
		if wheel {
			return m, nil
		}
		d.field = ff.field
		if col >= 0 {
			d.inputs = slices.Clone(d.inputs)
			d.inputs[ff.field] = d.inputs[ff.field].at(col, ff.w)
		}
		return m.setTopModal(d), nil
	}
	f := m.fitForm(d.sf)
	r := msg.Y - lay.y - 1 - ff.row - 1 // the description's drawn row under the pointer
	if wheel {
		if ff.field == fieldDesc && r >= 0 {
			f.desc = taScroll(f.desc, wheelDelta(msg))
			d.sf = f
			return m.setTopModal(d), nil
		}
		return m, nil
	}
	f.field = ff.field
	if f.field == fieldDesc {
		f.desc.Focus()
	} else {
		f.desc.Blur()
	}
	if ff.field == fieldDesc && r >= 0 {
		f.desc = taClick(f.desc, r, msg.X-lay.x-2-descIndent)
	}
	if col >= 0 {
		switch ff.field {
		case fieldTitle:
			f.title = f.title.at(col, ff.w)
		case fieldRef:
			f.ref = f.ref.at(col, ff.w)
		}
	}
	d.sf = f
	return m.setTopModal(d), nil
}
