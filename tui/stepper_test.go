package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/loop"
)

// stepSettings is settingsModel in Mouse mode with row selected.
func stepSettings(t *testing.T, w, h, row int) (Model, *[]string) {
	t.Helper()
	m, saved := settingsModel(t)
	m, _ = send(t, m.WithMouse(true), tea.WindowSizeMsg{Width: w, Height: h})
	for m.setRow < row {
		m = press(t, m, runeKey("j"))
	}
	return m, saved
}

// glyphCell is the screen cell of the first glyph in the drawn View.
func glyphCell(t *testing.T, m Model, glyph string) (x, y int) {
	t.Helper()
	for y, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if i := strings.Index(l, glyph); i >= 0 {
			return ansi.StringWidth(l[:i]), y
		}
	}
	t.Fatalf("no %s drawn:\n%s", glyph, ansi.Strip(m.View()))
	return 0, 0
}

func drawn(m Model, glyph string) bool { return strings.Contains(ansi.Strip(m.View()), glyph) }

var steppedRows = []struct {
	name string
	row  int
}{{"pool", 0}, {"Auto-exit", 1}, {"Auto-approve", autoApproveRow}, {"Mouse mode", mouseRow}, {"Theme", themeRow}, {"Kind profile", firstKindRow}}

func TestSettingsStepperClickStagesBothWays(t *testing.T) {
	for _, r := range steppedRows {
		for _, glyph := range []string{"◀", "▶"} {
			m, saved := stepSettings(t, 100, 30, r.row)
			if m.rowStaged() {
				t.Fatalf("%s: staged before any click", r.name)
			}
			x, y := glyphCell(t, m, glyph)
			m, _ = clickXY(t, m, x, y)
			if !m.rowStaged() {
				t.Errorf("%s: a click on %s staged nothing", r.name, glyph)
			}
			if len(*saved) != 0 {
				t.Errorf("%s: a click on %s saved %v", r.name, glyph, *saved)
			}
			if m.setRow != r.row {
				t.Errorf("%s: the click moved the cursor to %d", r.name, m.setRow)
			}
		}
	}
}

func TestSettingsStepperDirections(t *testing.T) {
	m, _ := stepSettings(t, 100, 30, 0)
	x, y := glyphCell(t, m, "▶")
	m, _ = clickXY(t, m, x, y)
	if m.staged != 4 {
		t.Fatalf("▶ staged pool %d, want 4", m.staged)
	}
	m, _ = stepSettings(t, 100, 30, 0)
	x, y = glyphCell(t, m, "◀")
	m, _ = clickXY(t, m, x, y)
	if m.staged != 2 {
		t.Errorf("◀ staged pool %d, want 2", m.staged)
	}
	m, _ = stepSettings(t, 100, 30, 1)
	x, y = glyphCell(t, m, "▶")
	m, _ = clickXY(t, m, x, y)
	if m.stagedExit != time.Minute+autoExitStep {
		t.Errorf("▶ staged Auto-exit %v", m.stagedExit)
	}
}

func TestSettingsStepperHitsOneColumnBesideArrow(t *testing.T) {
	for _, glyph := range []string{"◀", "▶"} {
		for _, dx := range []int{-1, 1} {
			m, _ := stepSettings(t, 100, 30, 3)
			x, y := glyphCell(t, m, glyph)
			m, _ = clickXY(t, m, x+dx, y)
			if !m.rowStaged() {
				t.Errorf("a click %+d columns from %s did not step", dx, glyph)
			}
		}
	}
	m, _ := stepSettings(t, 100, 30, 3)
	x, y := glyphCell(t, m, "◀")
	m, _ = clickXY(t, m, x+2, y) // the value, two columns on: no target
	if m.rowStaged() {
		t.Error("a click two columns from ◀ stepped")
	}
}

func TestSettingsStepperDoubleClickIsTwoStepsNotEnter(t *testing.T) {
	adv := clickClock(t)
	m, saved := stepSettings(t, 100, 30, 0)
	x, y := glyphCell(t, m, "▶")
	m, _ = clickXY(t, m, x, y)
	adv(time.Second) // ▶ moves right of the "3 → " the first step adds; later steps hold still
	x, y = glyphCell(t, m, "▶")
	m, _ = clickXY(t, m, x, y)
	adv(100 * time.Millisecond)
	m, cmd := clickXY(t, m, x, y)
	if cmd != nil {
		m = run(t, m, cmd)
	}
	if m.staged != 6 {
		t.Errorf("a double-click staged pool %d, want 6 (one step, then two)", m.staged)
	}
	if len(*saved) != 0 {
		t.Errorf("a double-click on ▶ saved %v", *saved)
	}
}

func TestSettingsStepperHiddenAtBoundAndInert(t *testing.T) {
	two, _ := stepSettings(t, 100, 30, 0)
	lx, ly := glyphCell(t, two, "◀")
	one, _ := settingsModel(t)
	one, _ = send(t, one.WithMouse(true), tea.WindowSizeMsg{Width: 100, Height: 30})
	one, _ = send(t, one, loadedMsg{slowTier: true, pool: loop.Setting{N: 1, Source: "default"}})
	if drawn(one, "◀") {
		t.Error("pool at 1 draws ◀")
	}
	if !drawn(one, "▶") {
		t.Error("pool at 1 lost ▶")
	}
	one, _ = clickXY(t, one, lx, ly)
	if one.rowStaged() {
		t.Error("a press on the blank ◀ staged pool")
	}

	exit, _ := stepSettings(t, 100, 30, 1)
	ex, ey := glyphCell(t, exit, "◀")
	off, _ := send(t, exit, loadedMsg{slowTier: true, autoExit: loop.AutoExit{Source: "default"}})
	if drawn(off, "◀") {
		t.Error("Auto-exit at off draws ◀")
	}
	off, _ = clickXY(t, off, ex, ey)
	if off.rowStaged() {
		t.Error("a press on the blank ◀ staged Auto-exit")
	}
}

func TestSettingsStepperWrapsStagedValueAndStepsFromIt(t *testing.T) {
	m, _ := stepSettings(t, 100, 30, 0)
	x, y := glyphCell(t, m, "▶")
	m, _ = clickXY(t, m, x, y)
	if row := strings.Split(ansi.Strip(m.View()), "\n")[y]; !strings.Contains(row, "3 → ◀ 4 ▶ (unsaved)  (default)") {
		t.Errorf("staged row %q does not wrap the staged value", row)
	}
	x, y = glyphCell(t, m, "▶")
	m, _ = clickXY(t, m, x, y)
	if m.staged != 5 {
		t.Errorf("a click on a staged stepper reached %d, want 5", m.staged)
	}
}

func TestSettingsStepperKeepsArrowsOnScreen(t *testing.T) {
	long := strings.Repeat("x", 90)
	for _, w := range []int{80, 120} {
		m, _ := stepSettings(t, w, 30, firstKindRow)
		m.kinds.profiles[long] = m.kinds.profiles["haiku"]
		m.kinds.local["default"] = long
		m.kinds.reasons = map[string]string{long: strings.Repeat("because ", 20)}
		m, _ = send(t, m, tea.WindowSizeMsg{Width: w, Height: 30})
		for _, glyph := range []string{"◀", "▶"} {
			if x, _ := glyphCell(t, m, glyph); x >= w {
				t.Errorf("width %d: %s at column %d", w, glyph, x)
			}
		}
	}
}

func TestSettingsStepperAbsentOnInitiativesAndWithMouseOff(t *testing.T) {
	m, _ := stepSettings(t, 100, 30, initiativesRow)
	if drawn(m, "◀") || drawn(m, "▶") {
		t.Error("Initiatives draws a stepper")
	}
	on, _ := stepSettings(t, 100, 30, 0)
	off, _ := send(t, on, tea.WindowSizeMsg{Width: 100, Height: 30})
	off.mouseOn = false
	if drawn(off, "◀") || drawn(off, "▶") {
		t.Error("a stepper is drawn with Mouse mode off")
	}
	if !strings.Contains(ansi.Strip(off.View()), "AFK pool  3  (default)") {
		t.Errorf("Settings with Mouse mode off does not render as today:\n%s", ansi.Strip(off.View()))
	}
}

func TestSettingsStepperHoverShadesOnlyItsArrow(t *testing.T) {
	m, _ := stepSettings(t, 100, 30, 0)
	forceANSI256(t)
	x, y := glyphCell(t, m, "▶")
	lx, _ := glyphCell(t, m, "◀")
	m = moveTo(t, m, x, y)
	row := strings.Split(m.View(), "\n")[y]
	if strings.Count(row, "48;5;17m") != 1 {
		t.Fatalf("hovering ▶ shades %d spans: %q", strings.Count(row, "48;5;17m"), row)
	}
	if i := strings.Index(row, "▶"); !strings.Contains(row[strings.LastIndex(row[:i], "\x1b["):i], "48;5;17") {
		t.Errorf("the navy is not on ▶: %q", row)
	}
	if MotionFilter(m, tea.MouseMsg{X: lx, Y: y, Action: tea.MouseActionMotion}) == nil {
		t.Error("motion from ▶ to ◀ was filtered, so ◀ never repaints")
	}
	m = moveTo(t, m, lx, y)
	row = strings.Split(m.View(), "\n")[y]
	if i := strings.Index(row, "◀"); !strings.Contains(row[strings.LastIndex(row[:i], "\x1b["):i], "48;5;17") {
		t.Errorf("the navy did not move to ◀: %q", row)
	}
	m = moveTo(t, m, 2, y) // the row's name: nothing shades on the selected row
	if got := hoverRows(m); len(got) != 0 {
		t.Errorf("hover off the arrows shades %v", got)
	}
}

func TestSettingsStepperWheelNeverEdits(t *testing.T) {
	m, saved := stepSettings(t, 100, 30, 0)
	x, y := glyphCell(t, m, "▶")
	for _, b := range []tea.MouseButton{tea.MouseButtonWheelDown, tea.MouseButtonWheelUp} {
		m, _ = send(t, m, tea.MouseMsg{X: x, Y: y, Button: b, Action: tea.MouseActionPress})
	}
	if m.rowStaged() || len(*saved) != 0 {
		t.Error("the wheel over a stepper edited")
	}
}

// clickSettle is clickXY with its command run to completion.
func clickSettle(t *testing.T, m Model, x, y int) Model {
	t.Helper()
	m, cmd := clickXY(t, m, x, y)
	return settle(t, m, cmd)
}

// stagedAt is stepSettings with row staged by a ▶ click.
func stagedAt(t *testing.T, row int) (Model, *[]string) {
	t.Helper()
	m, saved := stepSettings(t, 100, 30, row)
	x, y := glyphCell(t, m, "▶")
	m, _ = clickXY(t, m, x, y)
	if !m.rowStaged() {
		t.Fatalf("row %d: ▶ staged nothing", row)
	}
	return m, saved
}

func TestSettingsSaveDiscardTargetsClickLikeEnterAndEsc(t *testing.T) {
	for _, r := range steppedRows {
		m, saved := stagedAt(t, r.row)
		x, y := glyphCell(t, m, "✓ save")
		m = clickSettle(t, m, x, y)
		if m.rowStaged() || len(*saved) != 1 {
			t.Errorf("%s: ✓ save left staged=%v saved=%v", r.name, m.rowStaged(), *saved)
		}
		if drawn(m, "✓ save") || drawn(m, "✗ discard") {
			t.Errorf("%s: targets remain after save", r.name)
		}
		m, saved = stagedAt(t, r.row)
		x, y = glyphCell(t, m, "✗ discard")
		m = clickSettle(t, m, x, y)
		if m.rowStaged() || len(*saved) != 0 {
			t.Errorf("%s: ✗ discard left staged=%v saved=%v", r.name, m.rowStaged(), *saved)
		}
		if drawn(m, "✗ discard") {
			t.Errorf("%s: targets remain after discard", r.name)
		}
	}
}

func TestSettingsSaveTargetsOnlyOnStagedRowsWithMouseOn(t *testing.T) {
	m, _ := stepSettings(t, 100, 30, 0)
	if drawn(m, "✓ save") {
		t.Error("an unstaged row shows targets")
	}
	m, _ = stagedAt(t, 0)
	if !drawn(m, "✓ save  ✗ discard") {
		t.Error("a staged row shows no targets")
	}
	m, _ = send(t, m, tea.MouseMsg{X: 0, Y: 0, Button: tea.MouseButtonNone, Action: tea.MouseActionMotion})
	m = m.WithMouse(false)
	if drawn(m, "✓ save") {
		t.Error("targets drawn with Mouse mode off")
	}
}

func TestSettingsKindRowGainsTargetsLine(t *testing.T) {
	m, _ := stagedAt(t, firstKindRow)
	y := screenRow(t, m, "✓ save")
	if strings.Contains(ansi.Strip(strings.Split(m.View(), "\n")[y]), "default") {
		t.Error("the targets share the Kind row's line")
	}
	x, y := glyphCell(t, m, "✗ discard")
	m, _ = clickXY(t, m, x, y)
	if m.rowStaged() || drawn(m, "✗ discard") {
		t.Error("the targets line outlived discard")
	}
}

func TestSettingsGlossOutsideLabelsOnlySelects(t *testing.T) {
	m, saved := stagedAt(t, 0)
	_, y := glyphCell(t, m, "✓ save")
	m, _ = clickXY(t, m, 60, y)
	m, _ = clickXY(t, m, 9, y) // the gap between the labels
	if !m.rowStaged() || len(*saved) != 0 || m.setRow != 0 {
		t.Errorf("a gloss click acted: staged=%v saved=%v row=%d", m.rowStaged(), *saved, m.setRow)
	}
}

func TestSettingsSavingMouseOffByClickEndsCapture(t *testing.T) {
	m, saved := stepSettings(t, 100, 30, mouseRow)
	for !(m.mouseStaged && m.mouseStage.to == "off") {
		x, y := glyphCell(t, m, "▶")
		m, _ = clickXY(t, m, x, y)
		if len(*saved) > 0 || m.mouseStage.to == "" {
			t.Fatal("could not stage mouse off")
		}
	}
	x, y := glyphCell(t, m, "✓ save")
	m = clickSettle(t, m, x, y)
	if m.mouseLive() || drawn(m, "◀") || drawn(m, "▶") || drawn(m, "✓ save") {
		t.Errorf("capture continued:\n%s", ansi.Strip(m.View()))
	}
	if !strings.Contains(ansi.Strip(m.View()), "Mouse mode  off") || len(*saved) != 1 || (*saved)[0] != "mouse off" {
		t.Errorf("row does not read off after saving off %v:\n%s", *saved, ansi.Strip(m.View()))
	}
}

func TestSettingsSaveTargetsHoverShadesOnlyOwnCells(t *testing.T) {
	m, _ := stagedAt(t, 0)
	forceANSI256(t)
	x, y := glyphCell(t, m, "✗ discard")
	m = moveTo(t, m, x, y)
	row := strings.Split(m.View(), "\n")[y]
	if strings.Count(row, "48;5;17m") != 1 {
		t.Fatalf("hover shades %d spans: %q", strings.Count(row, "48;5;17m"), row)
	}
	if i := strings.Index(row, "✗ discard"); !strings.Contains(row[strings.LastIndex(row[:i], "\x1b["):i], "48;5;17") {
		t.Errorf("navy not on ✗ discard: %q", row)
	}
}
