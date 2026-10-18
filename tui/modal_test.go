package tui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

type size struct{ w, h int }

// assertFits checks the rendered view is exactly sz, no row wider, with the cursor
// row and the picker's esc hint on screen.
func assertFits(t *testing.T, m Model, sz size, cursor string) {
	t.Helper()
	v := ansi.Strip(m.View())
	ls := strings.Split(v, "\n")
	if len(ls) != sz.h {
		t.Errorf("%v: %d rows", sz, len(ls))
	}
	for _, l := range ls {
		if ansi.StringWidth(l) > sz.w {
			t.Errorf("%v: row too wide %q", sz, l)
		}
	}
	if !strings.Contains(v, "  "+cursor) || !strings.Contains(v, "esc") {
		t.Errorf("%v: cursor or hints clipped:\n%s", sz, v)
	}
}

// A picker taller than the terminal windows its items around the cursor, so the
// row and the picker's own hints stay on screen (FR15, FR24).
func TestPickerWindowsToTerminal(t *testing.T) {
	for _, sz := range []size{{40, 12}, {80, 16}} {
		items := make([]string, 18)
		for i := range items {
			items[i] = fmt.Sprintf("item-%02d", i)
		}
		m := sized(t, sz.w, sz.h).push(pickerDialog("Pick", items, "", nil))
		for range len(items) - 1 {
			m, _ = send(t, m, runeKey("j"))
		}
		assertFits(t, m, sz, "item-17")
	}
}

// The real adopt picker (many long-titled Initiatives, opened with a) windows and
// truncates to the terminal, hint line included, at the 40×12 floor.
func TestAdoptPickerFitsTerminal(t *testing.T) {
	bs := []bd.Bead{{ID: "sig", Kind: contract.KindSignal, Status: "open", Title: "A signal", Created: epoch}}
	for i := range 18 {
		bs = append(bs, bd.Bead{ID: fmt.Sprintf("ini%02d", i), Kind: contract.KindInitiative, Container: true, Status: "open",
			Title: fmt.Sprintf("A rather long initiative title number %02d", i), Created: epoch})
	}
	for _, sz := range []size{{40, 12}, {80, 16}} {
		m := sized(t, sz.w, sz.h)
		m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{}, sessions: map[string]session.Info{}})
		m, _ = send(t, m, runeKey("4"))
		m = unfoldUnparented(t, m)
		m = press(t, selectBead(t, m, "sig"), runeKey("a"))
		if len(m.modals) != 1 {
			t.Fatalf("%v: adopt picker not open", sz)
		}
		for range 17 {
			m, _ = send(t, m, runeKey("j"))
		}
		assertFits(t, m, sz, "○")
	}
}

func TestPickerTruncatesLongLines(t *testing.T) {
	for _, sz := range []size{{80, 16}, {80, 24}, {60, 16}} {
		items := make([]string, 12)
		for i := range items {
			items[i] = fmt.Sprintf("mc-%02d Initiative with a reasonably long descriptive title number %02d", i, i)
		}
		title := "Adopt Signal HITL launch command exceeds the 14 KiB budget under the nix build into"
		m := sized(t, sz.w, sz.h).push(pickerDialog(title, items, "", nil))
		for range len(items) - 1 {
			m, _ = send(t, m, runeKey("j"))
		}
		assertFits(t, m, sz, "mc-11")
		if !strings.Contains(ansi.Strip(m.View()), "…") {
			t.Errorf("%v: long lines not truncated", sz)
		}
	}
}

// a and A on a row where they don't apply say why on the status line (FR34).
func TestAdoptReleaseRefusalsExplain(t *testing.T) {
	bs := []bd.Bead{
		{ID: "ini", Kind: contract.KindInitiative, Container: true, Status: "open", Title: "An initiative", Created: epoch},
		{ID: "sig", Kind: contract.KindSignal, Status: "open", Title: "A signal", Created: epoch},
	}
	m := sized(t, 80, 24)
	m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{}, sessions: map[string]session.Info{}})
	m, _ = send(t, m, runeKey("4"))
	m = unfoldUnparented(t, m)
	for _, c := range []struct{ id, key, want string }{
		{"ini", "a", "never nest"},
		{"sig", "A", "not in an Initiative"},
	} {
		m = press(t, selectBead(t, m, c.id), runeKey(c.key))
		if len(m.modals) != 0 || !strings.Contains(m.status, c.want) {
			t.Errorf("%s on %s: status %q", c.key, c.id, m.status)
		}
	}
}

// Confirm bodies and input rows wrap rather than truncate, and the hint line keeps
// its tab hint, at 80×24 and the 40×12 floor.
func TestDialogsWrapNotTruncate(t *testing.T) {
	wt := "/tmp/mugicrew-worktrees/some-very-long-orphaned-worktree-path-that-keeps-going-and-going-on"
	for _, sz := range [][2]int{{80, 24}, {40, 12}} {
		cases := []struct {
			d    dialog
			typ  string
			want []string
		}{
			{confirmDialog("Drop orphaned worktree?", []string{"branch held by an orphaned worktree " + wt, "drop it and launch?"}, "", nil), "", []string{"launch?"}},
			{inputDialog("Rollback", []string{"evidence", "reason"}, "", nil), strings.Repeat("x", 80) + "END", []string{"tab"}},
		}
		for _, c := range cases {
			m := sized(t, sz[0], sz[1]).push(c.d)
			for _, r := range c.typ {
				m, _ = send(t, m, runeKey(string(r)))
			}
			v := ansi.Strip(m.View())
			if n := len(strings.Split(v, "\n")); n != sz[1] {
				t.Errorf("%v: %d rows", sz, n)
			}
			flat := strings.Join(strings.Fields(strings.NewReplacer("│", " ", "\n", " ").Replace(v)), "")
			for _, w := range c.want {
				if !strings.Contains(flat, strings.ReplaceAll(w, " ", "")) {
					t.Errorf("%v: %q missing:\n%s", sz, w, v)
				}
			}
			if c.typ != "" && sz[1] >= 24 && !strings.Contains(flat, "END") {
				t.Errorf("%v: typed text cut:\n%s", sz, v)
			}
		}
	}
}

// boxWidth measures the rounded box on screen from its bottom border.
func boxWidth(t *testing.T, m Model) int {
	t.Helper()
	for _, l := range lines(m.View()) {
		if i, j := strings.Index(l, "╰"), strings.Index(l, "╯"); i >= 0 && j > i {
			return ansi.StringWidth(l[:j+len("╯")]) - ansi.StringWidth(l[:i])
		}
	}
	t.Fatal("no box on screen")
	return 0
}

// Confirm, input and picker boxes are min(clamp(w/2, 60, 100), w-4) wide.
func TestDialogWidthFollowsTerminal(t *testing.T) {
	dialogs := map[string]dialog{
		"confirm": confirmDialog("Sure?", []string{"body"}, "", nil),
		"input":   inputDialog("Name", []string{"name"}, "", nil),
		"picker":  pickerDialog("Pick", []string{"a", "b"}, "", nil),
	}
	for name, d := range dialogs {
		for _, c := range []struct{ w, want int }{{40, 36}, {80, 60}, {120, 60}, {160, 80}, {200, 100}, {300, 100}} {
			if got := boxWidth(t, sized(t, c.w, 40).push(d)); got != c.want {
				t.Errorf("%s at %d columns: box %d wide, want %d", name, c.w, got, c.want)
			}
		}
	}
}

// A prompt wraps at the box's inner width when drawn: wider on a wide terminal,
// within the box with no text lost on a narrow one.
func TestPromptWrapsAtBoxWidth(t *testing.T) {
	words := strings.Fields(strings.Repeat("alpha beta gamma delta ", 20))
	d := promptDialog("Long?", func(int) []string { return []string{strings.Join(words, " ")} }, "", nil)
	for _, c := range []struct{ w, h int }{{200, 40}, {40, 40}} {
		v := ansi.Strip(sized(t, c.w, c.h).push(d).View())
		widest := 0
		for _, l := range strings.Split(v, "\n") {
			if i, j := strings.Index(l, "│"), strings.LastIndex(l, "│"); j > i {
				widest = max(widest, ansi.StringWidth(l[i:j]))
			}
		}
		if c.w == 200 && widest <= 96 {
			t.Errorf("w=200: widest line %d, want wider than the 96-column inner width", widest)
		}
		if c.w == 40 && widest > 36 {
			t.Errorf("w=40: widest line %d overflows the box", widest)
		}
		if got := strings.Count(strings.NewReplacer("│", " ", "\n", " ").Replace(v), "delta"); got != 20 {
			t.Errorf("w=%d: %d of 20 words kept:\n%s", c.w, got, v)
		}
	}
}

// The form is min(clamp(w*3/5, 72, 120), w-4) wide, and its description
// takes clamp(h*2/5, 10, 30) rows (mc-89k1 amendment).
func TestFormSizeFollowsTerminal(t *testing.T) {
	for _, c := range []struct{ w, want int }{{40, 36}, {80, 72}, {120, 72}, {160, 96}, {200, 120}, {300, 120}} {
		if got := boxWidth(t, sized(t, c.w, 40).push(newSignalForm())); got != c.want {
			t.Errorf("form at %d columns: box %d wide, want %d", c.w, got, c.want)
		}
	}
	long := strings.Repeat("line\n", 60)
	for _, c := range []struct{ h, want int }{{40, 16}, {100, 30}} {
		m := sized(t, 120, c.h).push(editSignalForm(bd.Bead{ID: "b", Title: "t", Description: long}))
		if n := strings.Count(m.View(), "line"); n != c.want {
			t.Errorf("description at h=%d shows %d rows, want %d", c.h, n, c.want)
		}
	}
}

// rowWith is the rendered row containing s, or "" if none.
func rowWith(m Model, s string) string {
	for _, l := range lines(m.View()) {
		if strings.Contains(l, s) {
			return l
		}
	}
	return ""
}

// Below a 50-column inner width the label sits on its own line with the field under it.
func TestNarrowRowsStackLabelOverField(t *testing.T) {
	for name, d := range map[string]dialog{
		"form":  newSignalForm(),
		"input": inputDialog("Name", []string{"name"}, "", nil),
	} {
		label := map[string]string{"form": "title:", "input": "name:"}[name]
		wide := rowWith(sized(t, 120, 40).push(d), label)
		if !strings.Contains(wide, "▏") {
			t.Errorf("%s at 120: label and field should share a line, got %q", name, wide)
		}
		m := sized(t, 50, 40).push(d)
		row := rowWith(m, label)
		if strings.Contains(row, "▏") {
			t.Errorf("%s at 50: label row also holds the field: %q", name, row)
		}
		ls := lines(m.View())
		for i, l := range ls {
			if strings.Contains(l, label) {
				if !strings.Contains(ls[i+1], "▏") {
					t.Errorf("%s at 50: field not under its label: %q", name, ls[i+1])
				}
				break
			}
		}
	}
}

// A stacked form on a short terminal still fits the terminal height.
func TestStackedFormFitsShortTerminal(t *testing.T) {
	m := sized(t, 50, 20).push(newSignalForm())
	if n := len(lines(m.View())); n != 20 {
		t.Errorf("%d rows, want 20", n)
	}
	if !strings.Contains(ansi.Strip(m.View()), "ctrl+s") {
		t.Errorf("hints clipped:\n%s", m.View())
	}
	m = sized(t, 50, 40).push(newSignalForm())
	if !strings.Contains(ansi.Strip(m.View()), "labels and deps: E after saving") {
		t.Errorf("stacked form lost its footnote:\n%s", m.View())
	}
}

// The picker's selected item is a grey 237 bar with no ▸; it keeps its own foreground,
// truncated items are still shaded, and unselected lines and the k/N counter are not.
func TestPickerSelectionBar(t *testing.T) {
	forceANSI256(t)
	const bar = "48;5;237"
	items := make([]string, 12)
	for i := range items {
		items[i] = fmt.Sprintf("item-%02d with a rather long descriptive title that gets cut off", i)
	}
	items[0] = "\x1b[31mred-item\x1b[0m"
	m := sized(t, 40, 12).push(pickerDialog("Pick", items, "", nil))
	for _, cur := range []int{0, 5} {
		if cur > 0 {
			m, _ = send(t, m, runeKey("j")) // item 1
			for range cur - 1 {
				m, _ = send(t, m, runeKey("j"))
			}
		}
		var shaded int
		for _, l := range strings.Split(m.View(), "\n") {
			plain := ansi.Strip(l)
			if strings.Contains(plain, "▸") {
				t.Errorf("cur %d: picker still has ▸: %q", cur, plain)
			}
			switch has := strings.Contains(l, bar); {
			case has && strings.Contains(plain, "/12"):
				t.Errorf("cur %d: counter row shaded: %q", cur, plain)
			case has:
				shaded++
				if cur == 0 && !strings.Contains(l, "31m") {
					t.Errorf("own foreground lost under the bar: %q", l)
				}
				if cur == 5 && !strings.Contains(plain, "…") {
					t.Errorf("truncated item not shaded to the cut: %q", plain)
				}
			}
		}
		if shaded != 1 {
			t.Errorf("cur %d: %d shaded lines, want 1", cur, shaded)
		}
	}
}

// A row key fires its row like enter on it; a row may carry two keys; other keys are
// ignored and esc is the only close for a picker with no close key of its own.
func TestPickerRowKeysFireTheirRow(t *testing.T) {
	picked := -1
	d := pickerDialog("Goto", []string{"top", "bottom", "unkeyed"}, "", func(m Model, d dialog) (Model, tea.Cmd) {
		picked = d.cur
		return m, nil
	})
	d.rowKeys = [][]string{{"g", "t"}, {"G", "b"}, nil}
	open := func() Model { return sized(t, 80, 24).push(d) }

	for k, want := range map[string]int{"g": 0, "t": 0, "G": 1, "b": 1} {
		picked = -1
		m, _ := send(t, open(), runeKey(k))
		if picked != want || len(m.modals) != 0 {
			t.Errorf("%q: picked %d (want %d), modals %d", k, picked, want, len(m.modals))
		}
	}

	picked = -1
	m, _ := send(t, open(), runeKey("x"))
	if picked != -1 || len(m.modals) != 1 || m.modals[0].cur != 0 {
		t.Errorf("non-matching key: picked %d, modals %d", picked, len(m.modals))
	}

	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if len(m.modals) != 0 {
		t.Error("esc did not close the picker")
	}
}

// Unkeyed rows past the ninth are reached with j/k, the window follows, and enter picks.
func TestPickerUnkeyedRowsReachedWithJK(t *testing.T) {
	picked := -1
	items := make([]string, 12)
	for i := range items {
		items[i] = fmt.Sprintf("row-%02d", i)
	}
	d := pickerDialog("Goto", items, "", func(m Model, d dialog) (Model, tea.Cmd) {
		picked = d.cur
		return m, nil
	})
	d.rowKeys = make([][]string, len(items))
	for i := range 9 {
		d.rowKeys[i] = []string{fmt.Sprint(i + 1)}
	}
	m := sized(t, 40, 12).push(d)
	for range 11 {
		m, _ = send(t, m, runeKey("j"))
	}
	assertFits(t, m, size{40, 12}, "row-11")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "12/12") {
		t.Errorf("no k/N counter:\n%s", v)
	}
	send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if picked != 11 {
		t.Errorf("enter picked %d, want 11", picked)
	}
}
