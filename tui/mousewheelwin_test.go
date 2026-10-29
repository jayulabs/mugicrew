package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// histScrolled is a History tab over 60 commits at h=20, wheeled down 3 notches.
func histScrolled(t *testing.T) Model {
	t.Helper()
	m := sized(t, 100, 20)
	m, _ = send(t, m, histLoad(histManyCommits(60), histBeads()))
	m, _ = send(t, m, runeKey("6"))
	for range 3 {
		m = wheel(t, m, 6, false)
	}
	return m
}

func TestHistoryWheelScrollsWithoutMovingSelection(t *testing.T) {
	m := sized(t, 100, 20)
	m, _ = send(t, m, histLoad(histManyCommits(60), histBeads()))
	m, _ = send(t, m, runeKey("6"))
	want, before := histSel(m), bodyText(m, 4)
	m = wheel(t, m, 6, false)
	if histSel(m) != want {
		t.Errorf("the wheel moved the selection to %q", histSel(m))
	}
	after := bodyText(m, 4)
	if after == before {
		t.Fatal("the wheel did not scroll the commit window")
	}
	if strings.Split(after, "\n")[0] != strings.Split(before, "\n")[0] {
		t.Error("the main · newest line moved")
	}
	m = wheel(t, m, 6, true)
	if trimLines(bodyText(m, 4)) != trimLines(before) {
		t.Errorf("wheeling back did not restore the window:\n%s\nwas\n%s", bodyText(m, 4), before)
	}
}

func TestHistoryScrolledHoldsTopThroughRefreshAndResize(t *testing.T) {
	m := histScrolled(t)
	top := strings.Split(bodyText(m, 4), "\n")[1]
	newer := histManyCommits(3)
	for i := range newer {
		newer[i].SHA = "fff000" + string(rune('a'+i)) + "bbbb"
	}
	m, _ = send(t, m, histLoad(append(newer, histManyCommits(60)...), histBeads()))
	if got := strings.Split(bodyText(m, 4), "\n")[1]; got != top {
		t.Errorf("commits added above moved the top row: %q, was %q", got, top)
	}
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 90, Height: 20})
	if got := strings.Split(bodyText(m, 4), "\n")[1]; strings.TrimSpace(got) == "" || got[:9] != top[:9] {
		t.Errorf("a resize moved the top row: %q, was %q", got, top)
	}
	// the anchor commit removed: the nearest survivor above takes its place
	gone := histManyCommits(60)
	sha := strings.Fields(top)[0]
	var kept = gone[:0:0]
	for _, c := range gone {
		if !strings.HasPrefix(c.SHA, sha) {
			kept = append(kept, c)
		}
	}
	m, _ = send(t, m, histLoad(kept, histBeads()))
	if got := strings.Split(bodyText(m, 4), "\n")[1]; strings.Contains(got, sha) || strings.TrimSpace(got) == "" {
		t.Errorf("anchor removed: top row %q", got)
	}
}

func TestHistoryKeySnapsBackAndClickKeepsScrolled(t *testing.T) {
	forceANSI256(t)
	m := histScrolled(t)
	top := firstLine(m, 4)
	body := screenBody(m, 4)
	clicked := clickAt(t, m, 4+5)
	if got := firstLine(clicked, 4); got != top {
		t.Errorf("a click moved the window: %q, was %q", got, top)
	}
	if want := strings.Fields(ansi.Strip(body[5]))[0]; !strings.HasPrefix(histSel(clicked), want) {
		t.Errorf("click selected %q, want the drawn %q", histSel(clicked), want)
	}
	after := press(t, m, runeKey("j"))
	if selBodyLine(after, 4) == "" {
		t.Error("after a key the selection is not on screen")
	}
	// a tab switch resets to Following
	m = press(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if got := m.tabs[tabHistory].(histTab); got.lv.scrolled {
		t.Error("a tab switch kept History scrolled")
	}
}

func TestHistoryScrolledParity(t *testing.T) {
	m := histScrolled(t)
	for y, raw := range screenBody(m, 4) {
		after := clickAt(t, m, 4+y)
		text := ansi.Strip(raw)
		if y == 0 {
			if histSel(after) != histSel(m) {
				t.Errorf("header line selected %q", histSel(after))
			}
			continue
		}
		if got := histSel(after); got != histSel(m) && !strings.Contains(text, got[:7]) {
			t.Errorf("line %d %q selected %q", y, text, got)
		}
	}
}

// settingsScrolled is Settings at h=14 with the wheel run down the pane.
func settingsScrolled(t *testing.T) Model {
	t.Helper()
	m, _ := settingsModel(t)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 14})
	for range 2 {
		m = wheel(t, m, 6, false)
	}
	return m
}

func TestSettingsWheelScrollsWithoutMovingSelection(t *testing.T) {
	m, _ := settingsModel(t)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 14})
	before, row := bodyText(m, 4), m.setRow
	m = wheel(t, m, 6, false)
	if m.setRow != row {
		t.Errorf("the wheel moved the cursor to row %d", m.setRow)
	}
	if bodyText(m, 4) == before {
		t.Fatal("the wheel did not scroll the pane")
	}
	if !strings.Contains(bodyText(m, 4), "↑") {
		t.Error("a scrolled pane shows no ↑ N more marker")
	}
}

func TestSettingsScrolledHoldsTopThroughResizeAndSnapsOnKey(t *testing.T) {
	m := settingsScrolled(t)
	top := strings.Split(bodyText(m, 4), "\n")[1]
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 16})
	if got := strings.Split(bodyText(m, 4), "\n")[1]; got != top {
		t.Errorf("a resize moved the held top: %q, was %q", got, top)
	}
	row := m.setRow
	after := press(t, m, runeKey("j"))
	if after.setRow != row+1 {
		t.Errorf("j from the cursor went to row %d, want %d", after.setRow, row+1)
	}
	if after.setLV.scrolled {
		t.Error("a key left Settings scrolled")
	}
	lines, sel, rowOf := after.settingsLines()
	wtop := after.settingsWindow(lines, sel, rowOf, after.bodyH())
	if sel.lo < wtop || sel.hi > wtop+after.bodyH() {
		t.Errorf("after a key the cursor row [%d,%d) is off the window at %d", sel.lo, sel.hi, wtop)
	}
}

func TestSettingsClickKeepsScrolledAndTabSwitchResets(t *testing.T) {
	m := settingsScrolled(t)
	top := firstLine(m, 4)
	body := screenBody(m, 4)
	y := -1
	for i, l := range body {
		if i > 0 && i < len(body)-1 && strings.TrimSpace(ansi.Strip(l)) != "" && strings.Contains(ansi.Strip(l), "  ") {
			if h := m.settingsHit(m.bodyH(), i); h.kind == hitRow {
				y = i
				break
			}
		}
	}
	if y < 0 {
		t.Fatal("no clickable row on screen")
	}
	c := clickAt(t, m, 4+y)
	if got := firstLine(c, 4); got != top {
		t.Errorf("a click moved the pane: %q, was %q", got, top)
	}
	if !c.setLV.scrolled {
		t.Error("a click returned Settings to Following")
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.setLV.scrolled {
		t.Error("a tab switch kept Settings scrolled")
	}
}

func trimLines(s string) string {
	ls := strings.Split(s, "\n")
	for i, l := range ls {
		ls[i] = strings.TrimRight(l, " ")
	}
	return strings.Join(ls, "\n")
}
