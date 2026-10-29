package tui

import (
	"testing"
)

func filterEdit(t *testing.T, m Model) *line {
	t.Helper()
	switch tt := m.tabAt(m.tab).(type) {
	case graphTab:
		return &tt.edit
	case logsTab:
		return &tt.edit
	}
	t.Fatal("not a filter tab")
	return nil
}

func TestFilterLineClickPlacesCursor(t *testing.T) {
	for name, mk := range map[string]func() Model{
		"graph": func() Model { return graphModel(t, 100, 30) },
		"logs":  func() Model { return logsModel(t, 100, 30) },
	} {
		t.Run(name, func(t *testing.T) {
			m := typeText(t, press(t, mk().WithMouse(true), runeKey("/")), "hello world")
			y := m.bodyTop()
			x, _ := rowOf(t, m, "/ hello")
			click := func(m Model, x int) Model {
				return formClick(t, m, x, y)
			}
			m = click(m, x+2+3)
			if e := filterEdit(t, m); e.cur != 3 || e.text != "hello world" {
				t.Errorf("mid click: cur %d text %q", e.cur, e.text)
			}
			m = click(m, 90)
			if e := filterEdit(t, m); e.cur != 11 {
				t.Errorf("past end click: cur %d", e.cur)
			}
			if !m.capturing() {
				t.Error("click left the edit")
			}
		})
	}
}

func TestFilterLineClickWhenNotEditingIsUnchanged(t *testing.T) {
	m := graphModel(t, 100, 30).WithMouse(true)
	before := m.View()
	m = formClick(t, m, 5, m.bodyTop())
	if m.capturing() || m.View() != before {
		t.Error("click changed a Graph that isn't being edited")
	}
}
