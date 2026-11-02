package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestLineAtMapsCellToDrawnRune(t *testing.T) {
	long := "abcdefghijklmnopqrstuvwxyz"
	cases := []struct {
		name string
		l    line
		w    int
		col  int
		want int
	}{
		{"no clip, mid", lineAt("hello"), 20, 2, 2},
		{"no clip, past the end", line{"hello", 0}, 20, 15, 5},
		{"no clip, on the block", lineAt("hello"), 20, 5, 5},
		{"right clip, first cell", line{long, 0}, 10, 0, 0},
		{"right clip, mid", line{long, 0}, 10, 4, 4},
		{"right clip, on the …", line{long, 0}, 10, 9, 8},
		{"left clip, on the …", lineAt(long), 10, 0, 18},
		{"left clip, after the …", lineAt(long), 10, 1, 18},
		{"left clip, mid", lineAt(long), 10, 4, 21},
		{"left clip, on the block", lineAt(long), 10, 9, 26},
		{"left clip, past the block", lineAt(long), 10, 40, 26},
		{"both clips, on leading …", line{long, 13}, 10, 0, 10},
		{"both clips, mid", line{long, 13}, 10, 3, 12},
		{"both clips, on trailing …", line{long, 13}, 10, 9, 17},
		{"both clips, past the end", line{long, 13}, 10, 30, 17},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.l.at(c.col, c.w)
			if got.cur != c.want || got.text != c.l.text {
				t.Errorf("cursor %d (text %q), want %d", got.cur, got.text, c.want)
			}
		})
	}
}

// The rune a click lands on is the one render drew in that cell.
func TestLineAtAgreesWithRender(t *testing.T) {
	long := "abcdefghijklmnopqrstuvwxyz"
	for _, cur := range []int{0, 5, 13, 20, 26} {
		l := line{long, cur}
		for col := 1; col < 9; col++ {
			cell := []rune(ansi.Strip(l.render(10)))[col]
			if cell == '…' || cell == '▏' {
				continue
			}
			if got := l.at(col, 10); string(long[got.cur]) != string(cell) {
				t.Errorf("cur %d col %d: landed on %q, drawn %q", cur, col, string(long[got.cur]), string(cell))
			}
		}
	}
}

func mouseForm(t *testing.T) Model {
	t.Helper()
	m := pressN(t, formModel(t), "s")
	return m.WithMouse(true)
}

func formClick(t *testing.T, m Model, x, y int) Model {
	t.Helper()
	m, _ = send(t, m, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	return m
}

// rowOf is the screen row of the first line containing s, and the column s starts at.
func rowOf(t *testing.T, m Model, s string) (x, y int) {
	t.Helper()
	y = screenRow(t, m, s)
	l := strings.Split(ansi.Strip(m.View()), "\n")[y]
	return ansi.StringWidth(l[:strings.Index(l, s)]), y
}

func TestFormClickFocusesEachFieldAsTabDoes(t *testing.T) {
	m := mouseForm(t)
	for _, c := range []struct {
		label string
		want  int
	}{{"priority:", fieldPrio}, {"description:", fieldDesc}, {"title:", fieldTitle}} {
		x, y := rowOf(t, m, c.label)
		m = formClick(t, m, x, y)
		if got := m.modals[0].sf.field; got != c.want {
			t.Errorf("click %s: field %d, want %d", c.label, got, c.want)
		}
	}
	x, y := rowOf(t, m, "description:")
	m = formClick(t, m, x, y)
	if !m.modals[0].sf.desc.Focused() {
		t.Error("description click did not focus the textarea")
	}
	x, y = rowOf(t, m, "title:")
	m = formClick(t, m, x, y)
	if m.modals[0].sf.desc.Focused() {
		t.Error("title click left the textarea focused")
	}
}

func TestFormClickInTitleSetsCursorAndPriorityIsUnchanged(t *testing.T) {
	m := typeText(t, mouseForm(t), "hello world")
	x, y := rowOf(t, m, "title:")
	tx := x + len("title: ") + 2 // the third rune of the title, past the focus mark
	m = formClick(t, m, tx, y)
	if got := m.modals[0].sf.title.cur; got != 2 {
		t.Errorf("cursor %d, want 2", got)
	}
	m = formClick(t, m, tx+60, y)
	if got := m.modals[0].sf.title.cur; got != 11 {
		t.Errorf("past the end: cursor %d, want 11", got)
	}
	before := m.modals[0].sf.prio
	px, py := rowOf(t, m, "priority:")
	m = formClick(t, m, px+12, py)
	if sf := m.modals[0].sf; sf.field != fieldPrio || sf.prio != before {
		t.Errorf("priority click: field %d prio %d, want field %d prio %d", sf.field, sf.prio, fieldPrio, before)
	}
}

func TestFormClickOutsideBoxChangesNothing(t *testing.T) {
	m := typeText(t, mouseForm(t), "keep")
	for _, p := range [][2]int{{0, 0}, {m.w - 1, m.h - 1}, {1, 10}} {
		next := formClick(t, m, p[0], p[1])
		if len(next.modals) != 1 || next.modals[0].sf.title.text != "keep" || next.modals[0].sf.field != m.modals[0].sf.field {
			t.Errorf("click at %v changed the form", p)
		}
	}
}

func TestFormMotionDroppedAndOtherModalsStayDead(t *testing.T) {
	m := mouseForm(t)
	if got := MotionFilter(m, tea.MouseMsg{X: 5, Y: 5, Action: tea.MouseActionMotion}); got != nil {
		t.Errorf("motion over a form passed: %v", got)
	}
	fm := focusPrio(t, m)
	ax, ay := glyphCell(t, fm, "▶")
	if got := MotionFilter(fm, tea.MouseMsg{X: ax, Y: ay, Action: tea.MouseActionMotion}); got == nil {
		t.Error("motion over a form's priority arrow was dropped")
	}
	fm, _ = send(t, fm, tea.MouseMsg{X: ax, Y: ay, Action: tea.MouseActionMotion})
	if fm.hover.step != "+" {
		t.Errorf("hover step %q, want +", fm.hover.step)
	}
	m = m.push(confirmDialog("Sure?", nil, "", nil))
	before := m.modals[len(m.modals)-1]
	next := formClick(t, m, m.w/2, m.h/2)
	if len(next.modals) != 2 || next.modals[1].title != before.title || next.modals[0].sf.field != m.modals[0].sf.field {
		t.Error("a click on a confirm dialog acted")
	}
}

func TestFormClickMouseOffDoesNothing(t *testing.T) {
	m := pressN(t, formModel(t), "s")
	x, y := rowOf(t, m, "priority:")
	next, _ := send(t, m, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if next.modals[0].sf.field != fieldTitle {
		t.Error("click acted with Mouse mode off")
	}
}

func TestInitiativeFormClickPlacesRefCursor(t *testing.T) {
	m := formModel(t).WithMouse(true).push(newInitiativeForm(true))
	m = typeText(t, m, "t")
	m = press(t, m, tabKey())
	m = typeText(t, m, "ABCDEF")
	x, y := rowOf(t, m, "ref:")
	m = formClick(t, m, x+len("ref: ")+3, y)
	if sf := m.modals[len(m.modals)-1].sf; sf.field != fieldRef || sf.ref.cur != 3 {
		t.Errorf("field %d ref cursor %d, want ref at 3", sf.field, sf.ref.cur)
	}
}

// descForm is a mouse form with the description holding text, focused, and the form drawn.
func descForm(t *testing.T, text string) Model {
	t.Helper()
	m := mouseForm(t)
	d := m.modals[0]
	d.sf = m.fitForm(d.sf)
	d.sf.field = fieldDesc
	d.sf.desc.Focus()
	d.sf.desc.SetValue(text)
	_ = d.sf.desc.View() // a frame fills the viewport, then the next key's Update follows the cursor
	d.sf.desc, _ = d.sf.desc.Update(nil)
	d.sf.snap = d.sf.vals()
	return m.setTopModal(d)
}

// descPos is the description cursor as (line, column within the logical line).
func descPos(m Model) (int, int) {
	ta := m.modals[0].sf.desc
	return ta.Line(), ta.LineInfo().StartColumn + ta.LineInfo().ColumnOffset
}

// descXY is the screen cell of text column col on the textarea's drawn row r.
func descXY(t *testing.T, m Model, r, col int) (int, int) {
	t.Helper()
	x, y := rowOf(t, m, "description:")
	return x + col, y + 1 + r
}

func TestFormClickInDescriptionPlacesCursorAtLineAndColumn(t *testing.T) {
	m := descForm(t, "first line\nsecond line\nthird")
	x, y := descXY(t, m, 1, 4)
	m = formClick(t, m, x, y)
	if sf := m.modals[0].sf; sf.field != fieldDesc || !sf.desc.Focused() {
		t.Fatalf("field %d focused %v, want the description focused", sf.field, sf.desc.Focused())
	}
	if l, c := descPos(m); l != 1 || c != 4 {
		t.Errorf("cursor at %d:%d, want 1:4", l, c)
	}
	x, y = descXY(t, m, 0, 60)
	m = formClick(t, m, x, y)
	if l, c := descPos(m); l != 0 || c != 10 {
		t.Errorf("past the end: cursor at %d:%d, want 0:10", l, c)
	}
	x, y = descXY(t, m, 8, 1)
	m = formClick(t, m, x, y)
	if l, c := descPos(m); l != 2 || c != 5 {
		t.Errorf("below the last line: cursor at %d:%d, want 2:5", l, c)
	}
	if got := m.modals[0].sf.desc.Value(); got != "first line\nsecond line\nthird" {
		t.Errorf("a click changed the value to %q", got)
	}
}

func TestFormClickInSoftWrappedDescriptionCountsDrawnRows(t *testing.T) {
	m := descForm(t, "short\n"+strings.Repeat("word ", 40)+"end\nlast")
	w := m.modals[0].sf.desc.Width()
	// the second logical line wraps; its second drawn row is the textarea's third row
	x, y := descXY(t, m, 2, 3)
	m = formClick(t, m, x, y)
	l, c := descPos(m)
	if l != 1 || c < w-8 || c > w+3 {
		t.Errorf("wrapped click: cursor at %d:%d, want line 1 near column %d", l, c, w)
	}
}

func TestFormClickInScrolledDescriptionAllowsForOffset(t *testing.T) {
	var ls []string
	for i := range 40 {
		ls = append(ls, strings.Repeat("x", i%7+1)+"|")
	}
	m := descForm(t, strings.Join(ls, "\n")) // cursor at the end: the view is scrolled to the bottom
	h := m.modals[0].sf.desc.Height()
	x, y := descXY(t, m, 0, 0)
	m = formClick(t, m, x, y)
	if l, _ := descPos(m); l != 40-h {
		t.Errorf("click on the first drawn row: line %d, want %d", l, 40-h)
	}
}

func numberedLines(n int) string {
	var ls []string
	for i := range n {
		ls = append(ls, fmt.Sprintf("line %d", i))
	}
	return strings.Join(ls, "\n")
}

func TestFormWheelScrollsOverflowingDescriptionOnly(t *testing.T) {
	m := descForm(t, numberedLines(40))
	x, y := descXY(t, m, 1, 1)
	before := m.View()
	m, _ = send(t, m, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if m.View() == before {
		t.Error("wheel up over an overflowing description did not scroll")
	}
	if got := m.modals[0].sf.desc.Value(); got != numberedLines(40) {
		t.Error("wheel changed the value")
	}
	tx, ty := rowOf(t, m, "title:")
	before = m.View()
	m, _ = send(t, m, tea.MouseMsg{X: tx, Y: ty, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if m.View() != before {
		t.Error("wheel over the title changed the form")
	}
	short := descForm(t, "a\nb")
	x, y = descXY(t, short, 0, 0)
	before = short.View()
	short, _ = send(t, short, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if short.View() != before {
		t.Error("wheel over a non-overflowing description changed it")
	}
}

func TestFormWheelStepsOneLineAndTheCursorRides(t *testing.T) {
	m := descForm(t, numberedLines(40)) // cursor on the last line, view at the bottom
	x, y := descXY(t, m, 0, 0)
	h := m.modals[0].sf.desc.Height()
	firstRow := func(m Model) string {
		return strings.TrimSpace(ansi.Cut(strings.Split(ansi.Strip(m.View()), "\n")[y], x, x+20))
	}
	if got, want := firstRow(m), fmt.Sprintf("line %d", 40-h); !strings.HasPrefix(got, want) {
		t.Fatalf("first drawn row %q, want %q", got, want)
	}
	wheel := func(m Model, b tea.MouseButton) Model {
		m, _ = send(t, m, tea.MouseMsg{X: x, Y: y, Button: b, Action: tea.MouseActionPress})
		return m
	}
	m = wheel(wheel(m, tea.MouseButtonWheelUp), tea.MouseButtonWheelUp)
	if got, want := firstRow(m), fmt.Sprintf("line %d", 40-h-2); !strings.HasPrefix(got, want) {
		t.Errorf("two wheel-ups: first drawn row %q, want %q", got, want)
	}
	if l, _ := descPos(m); l != 37 {
		t.Errorf("two wheel-ups: cursor on line %d, want 37", l)
	}
	m = wheel(m, tea.MouseButtonWheelDown)
	if got, want := firstRow(m), fmt.Sprintf("line %d", 40-h-1); !strings.HasPrefix(got, want) {
		t.Errorf("wheel-down: first drawn row %q, want %q", got, want)
	}
	for range 100 {
		m = wheel(m, tea.MouseButtonWheelDown)
	}
	if got, want := firstRow(m), fmt.Sprintf("line %d", 40-h); !strings.HasPrefix(got, want) {
		t.Errorf("past the bottom: first drawn row %q, want %q", got, want)
	}
}

func TestFormWheelMouseOffDoesNothing(t *testing.T) {
	m := descForm(t, numberedLines(40)).WithMouse(false)
	x, y := descXY(t, m, 1, 1)
	before := m.View()
	m, _ = send(t, m, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if m.View() != before {
		t.Error("wheel acted with Mouse mode off")
	}
}

// dialogClick clicks the cell n runes into the text of the field labelled label.
func dialogClick(t *testing.T, m Model, label string, n int) Model {
	t.Helper()
	x, y := rowOf(t, m, label)
	return formClick(t, m, x+len(label)+1+n, y)
}

func TestInputDialogClickPlacesCursor(t *testing.T) {
	base := formModel(t).WithMouse(true)
	cases := []struct {
		name  string
		open  func(Model) Model
		label string
		field int
	}{
		{"checkup", func(m Model) Model {
			return m.push(inputDialog("Checkup x", []string{"problem (optional)"}, "id", nil))
		}, "problem (optional):", 0},
		{"reject", func(m Model) Model { return m.push(inputDialog("Reject x", []string{"reason (blank: x)"}, "id", nil)) }, "reason (blank: x):", 0},
		{"rollback evidence", func(m Model) Model { return m.rollbackForm(rollbackMsg{id: "id"}) }, "evidence:", 1},
		{"jira key", func(m Model) Model { return m.push(jiraKeyDialog("ABC-123")) }, "key:", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := c.open(base)
			m.modals[len(m.modals)-1].inputs[c.field] = lineAt("hello world")
			m = dialogClick(t, m, c.label, 4)
			d := m.modals[len(m.modals)-1]
			if d.field != c.field || d.inputs[c.field].cur != 4 {
				t.Errorf("field %d cursor %d, want field %d cursor 4", d.field, d.inputs[c.field].cur, c.field)
			}
			m = dialogClick(t, m, c.label, 40)
			if got := m.modals[len(m.modals)-1].inputs[c.field].cur; got != 11 {
				t.Errorf("past the end: cursor %d, want 11", got)
			}
		})
	}
}

func TestRollbackClickFocusesTheOtherInput(t *testing.T) {
	m := formModel(t).WithMouse(true).rollbackForm(rollbackMsg{id: "id"})
	m = dialogClick(t, m, "evidence:", 0)
	if d := m.modals[len(m.modals)-1]; d.field != 1 {
		t.Errorf("field %d, want 1", d.field)
	}
	m = dialogClick(t, m, "title:", 0)
	if d := m.modals[len(m.modals)-1]; d.field != 0 {
		t.Errorf("field %d, want 0", d.field)
	}
}

func TestSurgeryClickPlacesTitleCursorAndTargetListIsDead(t *testing.T) {
	m := pressN(t, sgModel(t, false), "u").WithMouse(true)
	d := m.modals[len(m.modals)-1]
	d.inputs[0] = lineAt("hello world")
	m = m.setTopModal(d)
	m = dialogClick(t, m, "title:", 3)
	if d := m.modals[len(m.modals)-1]; d.field != 0 || d.inputs[0].cur != 3 {
		t.Errorf("field %d cursor %d, want 0 and 3", d.field, d.inputs[0].cur)
	}
	before := m.modals[len(m.modals)-1]
	x, y := rowOf(t, m, "target:")
	next := formClick(t, m, x, y+1)
	if got := next.modals[len(next.modals)-1]; got.field != before.field || got.sg.cur != before.sg.cur {
		t.Error("a click on the target list acted")
	}
}

func TestInputDialogMotionDroppedAndMouseOffIsInert(t *testing.T) {
	m := formModel(t).WithMouse(true).push(jiraKeyDialog("ABC"))
	if got := MotionFilter(m, tea.MouseMsg{X: 5, Y: 5, Action: tea.MouseActionMotion}); got != nil {
		t.Errorf("motion over an input dialog passed: %v", got)
	}
	off := formModel(t).push(jiraKeyDialog("ABC"))
	off = dialogClick(t, off, "key:", 1)
	if off.modals[len(off.modals)-1].inputs[0].cur != 3 {
		t.Error("click moved the cursor with Mouse mode off")
	}
}
