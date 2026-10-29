package tui

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// pickCell is the screen row whose text holds s, taken from the rendered frame, and the
// column it starts at.
func pickCell(t *testing.T, m Model, s string) (x, y int) {
	t.Helper()
	for y, l := range strings.Split(m.View(), "\n") {
		if i := strings.Index(ansi.Strip(l), s); i >= 0 {
			return ansi.StringWidth(ansi.Strip(l)[:i]), y
		}
	}
	t.Fatalf("%q not in frame", s)
	return 0, 0
}

func topPicker(m Model) dialog { return m.modals[len(m.modals)-1] }

func manyItems(n int) []string {
	var is []string
	for i := 0; i < n; i++ {
		is = append(is, fmt.Sprintf("item-%02d", i))
	}
	return is
}

func TestHoverMovesPickerHighlight(t *testing.T) {
	for name, n := range map[string]int{"plain": 4, "scrolled": 60} {
		t.Run(name, func(t *testing.T) {
			var ran string
			d := pickerDialog("Pick", manyItems(n), "", func(m Model, d dialog) (Model, tea.Cmd) { ran = d.items[d.cur]; return m, nil })
			m := modalFixture(t, d)
			x, y := pickCell(t, m, "item-02")
			m, _ = modalMsg(t, m, x, y, tea.MouseButtonNone, tea.MouseActionMotion)
			if topPicker(m).cur != 2 {
				t.Fatalf("cur = %d", topPicker(m).cur)
			}
			if !strings.Contains(ansi.Strip(m.View()), "item-02") || len(m.modals) != 1 {
				t.Error("hover changed the stack")
			}
			m, _ = modalMsg(t, m, x, y, tea.MouseButtonLeft, tea.MouseActionPress)
			if ran != "item-02" || len(m.modals) != 0 {
				t.Errorf("click ran %q, modals %d", ran, len(m.modals))
			}
		})
	}
}

func TestHoverOnScrolledPickerNeverScrolls(t *testing.T) {
	m := modalFixture(t, pickerDialog("Pick", manyItems(60), "", nil))
	x, _ := pickCell(t, m, "item-00")
	before := ansi.Strip(m.View())
	for _, drawn := range []string{"item-03", "item-05"} {
		_, y2 := pickCell(t, m, drawn)
		m, _ = modalMsg(t, m, x, y2, tea.MouseButtonNone, tea.MouseActionMotion)
	}
	if !strings.Contains(ansi.Strip(m.View()), "item-00") && strings.Contains(before, "item-00") {
		t.Error("hover scrolled the window")
	}
}

func TestInertPickerLines(t *testing.T) {
	var hit bool
	d := pickerDialog("Pick", manyItems(3), "", acted(&hit))
	d.heads = map[int]string{1: "SECTION"}
	m := modalFixture(t, d)
	for _, s := range []string{"Pick", "SECTION"} {
		x, y := pickCell(t, m, s)
		for _, a := range []tea.MouseAction{tea.MouseActionMotion, tea.MouseActionPress} {
			n, _ := modalMsg(t, m, x, y, tea.MouseButtonLeft, a)
			if topPicker(n).cur != 0 || len(n.modals) != 1 || hit {
				t.Errorf("%q reacted to %v", s, a)
			}
		}
	}
	lay := m.dialogLayout(d)
	_, y0 := pickCell(t, m, "item-00")
	for _, y := range []int{lay.y, lay.y + lay.h - 1, y0 - 1, lay.y + lay.h - 2} { // border, blank, hints
		n, _ := modalMsg(t, m, lay.x+5, y, tea.MouseButtonLeft, tea.MouseActionPress)
		if hit || len(n.modals) != 1 {
			t.Errorf("row %d reacted", y)
		}
	}
	// the heading line sits between item-00 and item-01; item-01 is below it
	x, y := pickCell(t, m, "item-01")
	n, _ := modalMsg(t, m, x, y, tea.MouseButtonNone, tea.MouseActionMotion)
	if topPicker(n).cur != 1 {
		t.Errorf("cur = %d with a heading above", topPicker(n).cur)
	}
}

func TestPickerCounterIsInert(t *testing.T) {
	m := modalFixture(t, pickerDialog("Pick", manyItems(60), "", nil))
	x, y := pickCell(t, m, "1/60")
	n, _ := modalMsg(t, m, x, y, tea.MouseButtonLeft, tea.MouseActionPress)
	if len(n.modals) != 1 || topPicker(n).cur != 0 {
		t.Error("counter reacted")
	}
}

func TestPickerHoverHasNoSecondHighlight(t *testing.T) {
	m := modalFixture(t, pickerDialog("Pick", manyItems(4), "", nil))
	x, y := pickCell(t, m, "item-02")
	m, _ = modalMsg(t, m, x, y, tea.MouseButtonNone, tea.MouseActionMotion)
	if m.hover.set || topPicker(m).cur != 2 { // the one highlight is the picker's own cur
		t.Errorf("hover.set=%v cur=%d", m.hover.set, topPicker(m).cur)
	}
}

func TestMotionFilterOnPickerItems(t *testing.T) {
	m := modalFixture(t, pickerDialog("Pick", manyItems(4), "", nil))
	x, y := pickCell(t, m, "item-00")
	motion := func(x, y int) tea.Msg {
		return MotionFilter(m, tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion})
	}
	if motion(x, y) != nil {
		t.Error("motion within the current item passed")
	}
	if motion(0, 0) != nil {
		t.Error("motion off an item passed")
	}
	_, y1 := pickCell(t, m, "item-01")
	if motion(x, y1) == nil {
		t.Error("motion onto another item dropped")
	}
}

// clickEqualsEnter clicks the last item of m's top picker and checks it does what enter does
// with the highlight on that row: the same modal stack, tab and command. It returns the
// click's result.
func clickEqualsEnter(t *testing.T, m Model) Model {
	t.Helper()
	d := topPicker(m)
	if len(d.items) < 2 {
		t.Fatalf("picker has %d items", len(d.items))
	}
	last := len(d.items) - 1
	text := strings.TrimSpace(ansi.Strip(d.items[last]))
	x, y := pickCell(t, m, text)
	got, cmdC := modalMsg(t, m, x, y, tea.MouseButtonLeft, tea.MouseActionPress)
	k := m
	k.modals = slices.Clone(k.modals)
	k.modals[len(k.modals)-1].cur = last
	if d.ids != nil {
		k.modals[len(k.modals)-1].id = d.ids[last]
	}
	exp, cmdK := send(t, k, tea.KeyMsg{Type: tea.KeyEnter})
	titles := func(m Model) (ts []string) {
		for _, d := range m.modals {
			ts = append(ts, d.title)
		}
		return ts
	}
	if !slices.Equal(titles(got), titles(exp)) || got.tab != exp.tab || (cmdC == nil) != (cmdK == nil) {
		t.Errorf("click: modals %v tab %d cmd %v; enter: modals %v tab %d cmd %v",
			titles(got), got.tab, cmdC != nil, titles(exp), exp.tab, cmdK != nil)
	}
	if len(got.modals) == len(m.modals) && got.tab == m.tab && cmdC == nil && ansi.Strip(got.View()) == ansi.Strip(m.View()) {
		t.Error("click changed nothing")
	}
	return got
}

func TestClickRunsGenericPickerRow(t *testing.T) {
	var ran []string
	d := pickerDialog("Pick", manyItems(4), "", func(m Model, d dialog) (Model, tea.Cmd) { ran = append(ran, d.items[d.cur]); return m, nil })
	m := modalFixture(t, d)
	x, y := pickCell(t, m, "item-03")
	got, _ := modalMsg(t, m, x, y, tea.MouseButtonLeft, tea.MouseActionPress)
	if !slices.Equal(ran, []string{"item-03"}) || len(got.modals) != 0 {
		t.Errorf("ran %v, modals %d", ran, len(got.modals))
	}
}

func TestClickRunsGotoRow(t *testing.T) {
	next, ok := headerFixture(t, 100).gotoMenu()
	if !ok || !topPicker(next).gotoMenu {
		t.Fatal("no goto menu")
	}
	clickEqualsEnter(t, next)
}

func TestClickRunsNewMenuRow(t *testing.T) {
	m := press(t, graphModel(t, 120, 30).WithMouse(true), runeKey("n"))
	if d := topPicker(m); d.kind != picker || d.title == "" {
		t.Fatalf("New menu not open: %+v", d)
	}
	clickEqualsEnter(t, m)
}

func TestClickRunsRecentsRow(t *testing.T) {
	m := recentsModel(t, focusFixture(), "s1", "s2").WithMouse(true)
	m = press(t, m, runeKey("R"))
	if !recentsOpen(m) {
		t.Fatal("Recents not open")
	}
	clickEqualsEnter(t, m)
}

func TestClickRunsSessionsRow(t *testing.T) {
	m := press(t, sessionsModel(t).WithMouse(true), runeKey("S"))
	if topModal(m) != "sessions" || len(topPicker(m).ids) < 2 {
		t.Fatalf("sessions not populated: %q", topModal(m))
	}
	d := topPicker(m)
	x, y := pickCell(t, m, strings.TrimSpace(ansi.Strip(d.items[len(d.items)-1])))
	n, _ := modalMsg(t, m, x, y, tea.MouseButtonNone, tea.MouseActionMotion)
	if last := len(d.items) - 1; topPicker(n).cur != last || topPicker(n).id != d.ids[last] {
		t.Errorf("hover: cur %d id %q", topPicker(n).cur, topPicker(n).id)
	}
	clickEqualsEnter(t, m)
}

func TestEmptySessionsClickIsInert(t *testing.T) {
	m := sized(t, 120, 30).WithMouse(true)
	m, _ = send(t, m, loadedMsg{beads: nil, live: map[string]bool{}})
	m = press(t, m, runeKey("S"))
	if topModal(m) != "sessions" || len(topPicker(m).items) != 0 {
		t.Fatal("want an empty Sessions modal")
	}
	x, y := pickCell(t, m, "no sessions")
	before := ansi.Strip(m.View())
	n, cmd := modalMsg(t, m, x, y, tea.MouseButtonLeft, tea.MouseActionPress)
	if len(n.modals) != 1 || cmd != nil || ansi.Strip(n.View()) != before {
		t.Errorf("click in empty Sessions did something: modals %d cmd %v", len(n.modals), cmd != nil)
	}
}

func TestHoverInMidListPickerKeepsWindow(t *testing.T) {
	d := pickerDialog("Pick", manyItems(60), "", nil)
	m := modalFixture(t, d)
	var keys []string
	for i := 0; i < 30; i++ {
		keys = append(keys, "j")
	}
	for i := 0; i < 5; i++ {
		keys = append(keys, "k")
	}
	for _, k := range keys { // scroll down by keys, then back up inside the window
		n, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		m = n.(Model)
	}
	before := ansi.Strip(m.View())
	x, y := pickCell(t, m, "item-28")
	m, _ = modalMsg(t, m, x, y, tea.MouseButtonNone, tea.MouseActionMotion)
	if topPicker(m).cur != 28 {
		t.Fatalf("cur = %d", topPicker(m).cur)
	}
	// the k/N counter changes with cur; the drawn items must not
	if got := ansi.Strip(m.View()); strings.Split(got, "item-")[1] != strings.Split(before, "item-")[1] {
		t.Fatal("window moved")
	}
	x2, y2 := pickCell(t, m, "item-28")
	if y2 != y || x2 != x {
		t.Fatalf("item-28 moved from %d,%d to %d,%d", x, y, x2, y2)
	}
	if MotionFilter(m, tea.MouseMsg{X: x + 1, Y: y, Action: tea.MouseActionMotion}) != nil {
		t.Error("second motion on the row passed the filter")
	}
	if topPicker(m).cur != 28 {
		t.Error("cur moved")
	}
}

func TestHoverOnPickerOpenedMidListNeverScrolls(t *testing.T) {
	d := pickerDialog("Pick", manyItems(60), "", nil)
	d.cur = 40
	m := modalFixture(t, d)
	before := ansi.Strip(m.View())
	var first int
	fmt.Sscanf(before[strings.Index(before, "item-")+5:], "%d", &first)
	x, y := pickCell(t, m, fmt.Sprintf("item-%02d", first+1)) // a drawn row above cur
	n, _ := modalMsg(t, m, x, y, tea.MouseButtonNone, tea.MouseActionMotion)
	counter := regexp.MustCompile(`\d+/60`) // the k/N counter follows cur
	if got := ansi.Strip(n.View()); counter.ReplaceAllString(got, "") != counter.ReplaceAllString(before, "") {
		t.Errorf("hover moved the window:\nbefore:\n%s\nafter:\n%s", before, got)
	}
}

func TestHoverOnRefreshedScrolledSessionsNeverScrolls(t *testing.T) {
	m := sessionsModel(t).WithMouse(true)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 12})
	mm, _ := m.sessionsOpen()
	m = mm.(Model)
	d := topPicker(m)
	for range 4 {
		m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	if d = topPicker(m); d.cur != 4 {
		t.Fatalf("cur = %d", d.cur)
	}
	m = m.sessionsSync()
	before := sessionsView(m)
	x, y := pickCell(t, m, "Working one · work") // a drawn row above cur, only in the modal
	n, _ := modalMsg(t, m, x, y, tea.MouseButtonNone, tea.MouseActionMotion)
	counter := regexp.MustCompile(`\d+/\d+ `)
	if topPicker(n).cur == topPicker(m).cur {
		t.Fatal("hover did not move cur")
	}
	if after := sessionsView(n); counter.ReplaceAllString(after, "") != counter.ReplaceAllString(before, "") {
		t.Errorf("window moved:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}
