package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
)

func ctrlS() tea.KeyMsg  { return tea.KeyMsg{Type: tea.KeyCtrlS} }
func tabKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyTab} }

func editForm(t *testing.T) Model {
	t.Helper()
	return press(t, selectBead(t, formModel(t), "long"), runeKey("e"))
}

func view(m Model) string { return ansi.Strip(m.View()) }

func TestFormEnterAdvancesThenNewlines(t *testing.T) {
	m := pressN(t, formModel(t), "s")
	m = press(t, typeText(t, m, "T"), enter())
	if m.modals[0].sf.field != fieldPrio {
		t.Fatalf("enter on title: field %d", m.modals[0].sf.field)
	}
	m = press(t, m, enter())
	if m.modals[0].sf.field != fieldDesc {
		t.Fatalf("enter on priority: field %d", m.modals[0].sf.field)
	}
	m = press(t, typeText(t, m, "a"), enter())
	m = typeText(t, m, "b")
	if got := m.modals[0].sf.desc.Value(); got != "a\nb" {
		t.Errorf("description %q", got)
	}
}

func TestFormPriorityClampsAndIsNeverTyped(t *testing.T) {
	m := press(t, pressN(t, formModel(t), "s"), tabKey())
	for range 6 {
		m = press(t, m, runeKey("+"))
	}
	if p := m.modals[0].sf.prio; p != 0 {
		t.Errorf("clamp at P0: %d", p)
	}
	for range 6 {
		m = press(t, m, runeKey("-"))
	}
	m = press(t, m, runeKey("3"))
	if p := m.modals[0].sf.prio; p != 4 {
		t.Errorf("clamp at P4 / digit ignored: %d", p)
	}
}

func TestFormEmptyTitleRefused(t *testing.T) {
	seam(t, &newSignal, func(string, string) (string, error) { t.Fatal("filed"); return "", nil })
	m := press(t, pressN(t, formModel(t), "s"), ctrlS())
	if len(m.modals) != 1 || !strings.Contains(view(m), "title required") {
		t.Errorf("modals %d:\n%s", len(m.modals), view(m))
	}
}

func TestFormCreateWritesPriorityOnlyWhenNotP2(t *testing.T) {
	var got, upd []string
	seam(t, &newSignal, func(title, desc string) (string, error) { got = append(got, title, desc); return "s9", nil })
	seam(t, &updateBead, func(id string, a []string) error { upd = append([]string{id}, a...); return nil })
	m := typeText(t, pressN(t, formModel(t), "s"), "Fix it")
	m = press(t, press(t, m, tabKey()), ctrlS()) // ctrl+s from the priority field
	if !slices.Equal(got, []string{"Fix it", ""}) || upd != nil || len(m.modals) != 0 || !strings.Contains(m.status, "filed Signal s9") {
		t.Fatalf("P2: got %v upd %v modals %d status %q", got, upd, len(m.modals), m.status)
	}
	m = typeText(t, pressN(t, m, "s"), "Hi")
	m = press(t, press(t, m, tabKey()), runeKey("+"))
	m = press(t, m, ctrlS())
	if !slices.Equal(upd, []string{"s9", "--priority", "1"}) {
		t.Errorf("update %v", upd)
	}
}

func TestFormCreateFailureKeepsForm(t *testing.T) {
	seam(t, &newSignal, func(string, string) (string, error) { return "", errTest })
	m := typeText(t, pressN(t, formModel(t), "s"), "T")
	m = press(t, m, ctrlS())
	if len(m.modals) != 1 || !strings.Contains(view(m), "boom") || m.modals[0].sf.title.text != "T" {
		t.Errorf("failed write lost the form:\n%s", view(m))
	}
}

func TestFormEditWritesOnlyChangedFlags(t *testing.T) {
	var args []string
	writes := 0
	seam(t, &showBead, func(id string) (bd.Bead, error) { b, _ := formModel(t).pinned(id); return b, nil })
	seam(t, &updateBead, func(id string, a []string) error { args = a; writes++; return nil })
	m := press(t, editForm(t), ctrlS()) // nothing changed
	if writes != 0 || len(m.modals) != 0 {
		t.Fatalf("unchanged save: writes %d modals %d", writes, len(m.modals))
	}
	m = typeText(t, editForm(t), "!")
	m = press(t, m, ctrlS())
	if !slices.Equal(args, []string{"--title", "Long description!"}) || len(m.modals) != 0 {
		t.Errorf("args %v", args)
	}
	m = press(t, editForm(t), tabKey())
	m = press(t, press(t, m, runeKey("-")), ctrlS())
	if !slices.Equal(args, []string{"--priority", "3"}) {
		t.Errorf("args %v", args)
	}
}

func TestFormEditClash(t *testing.T) {
	var args []string
	cur := bd.Bead{}
	seam(t, &showBead, func(string) (bd.Bead, error) { return cur, nil })
	seam(t, &updateBead, func(id string, a []string) error { args = a; return nil })
	m := formModel(t)
	cur, _ = m.pinned("long")
	cur.Title = "Changed elsewhere"
	m = typeText(t, editForm(t), "!") // title edited
	m = press(t, m, ctrlS())
	if args != nil || len(m.modals) != 1 || !strings.Contains(m.status, "title") || !strings.Contains(view(m), "title changed elsewhere") {
		t.Fatalf("clash saved: args %v modals %d status %q", args, len(m.modals), m.status)
	}
	// a clash on a field the human did not change still saves
	m = press(t, editForm(t), tabKey())
	m = press(t, press(t, m, runeKey("-")), ctrlS())
	if !slices.Equal(args, []string{"--priority", "3"}) {
		t.Errorf("args %v", args)
	}
}

func TestFormClosedBeadRefusesSave(t *testing.T) {
	seam(t, &updateBead, func(string, []string) error { t.Fatal("wrote"); return nil })
	m := typeText(t, editForm(t), "!")
	bs := slices.Clone(m.beads)
	for i := range bs {
		if bs[i].ID == "long" {
			bs[i].Status = "closed"
		}
	}
	m, _ = send(t, m, loadedMsg{beads: bs})
	m = press(t, m, ctrlS())
	if len(m.modals) != 1 || !strings.Contains(view(m), "was closed") {
		t.Errorf("no warning:\n%s", view(m))
	}
	if m = press(t, press(t, m, esc()), runeKey("y")); len(m.modals) != 0 { // esc is the way out
		t.Fatal("esc did not leave")
	}
}

func TestFormEscCleanClosesDirtyAsks(t *testing.T) {
	m := press(t, editForm(t), esc())
	if len(m.modals) != 0 {
		t.Fatal("clean form stayed")
	}
	m = press(t, typeText(t, editForm(t), "!"), esc())
	if len(m.modals) != 2 || !strings.Contains(view(m), "Discard changes?") {
		t.Fatalf("no discard dialog: %d", len(m.modals))
	}
	if n := pressN(t, m, "s"); len(n.modals) != 1 || n.modals[0].kind != form {
		t.Error("n did not return to the form")
	}
	if n := press(t, m, esc()); len(n.modals) != 1 {
		t.Error("esc did not return to the form")
	}
	for _, k := range []tea.KeyMsg{runeKey("y"), enter()} {
		if n := press(t, m, k); len(n.modals) != 0 {
			t.Errorf("%s did not discard", k)
		}
	}
	// a new form is clean only while empty at P2
	m = press(t, press(t, pressN(t, formModel(t), "s"), tabKey()), runeKey("+"))
	if m = press(t, m, esc()); len(m.modals) != 2 {
		t.Error("changed priority closed at once")
	}
}

func TestFormCapturesKeys(t *testing.T) {
	m := pressN(t, formModel(t), "s")
	for _, k := range []tea.KeyMsg{runeKey("1"), runeKey("5"), runeKey("q"), tabKey(), runeKey("o"), runeKey("h"), runeKey("l")} {
		next, cmd := send(t, m, k)
		if next.tab != m.tab || cmd != nil || len(next.modals) != 1 {
			t.Errorf("%s reached the shell", k)
		}
	}
	if got := typeText(t, m, "q15").modals[0].sf.title.text; got != "q15" {
		t.Errorf("title %q", got)
	}
}

func TestFormFitsSmallTerminals(t *testing.T) {
	for _, sz := range [][2]int{{40, 12}, {80, 24}, {200, 60}} {
		m := sized(t, sz[0], sz[1]).push(editSignalForm(bd.Bead{ID: "b", Title: strings.Repeat("t", 100), Description: strings.Repeat("line\n", 30)}))
		ls := lines(m.View())
		if len(ls) != sz[1] {
			t.Errorf("%v: %d lines", sz, len(ls))
		}
		for _, l := range ls {
			if lipgloss.Width(l) > sz[0] {
				t.Errorf("%v: %q too wide", sz, l)
			}
		}
		if !strings.Contains(view(m), "ctrl+s") {
			t.Errorf("%v: hints cut:\n%s", sz, view(m))
		}
	}
}

func TestFormTabDescriptionIsCleanAndNeverRewritten(t *testing.T) {
	b := bd.Bead{ID: "sig", Title: "A signal", Priority: 2, Status: "open", Description: "go\nfunc f() {\n\treturn\n}\n"}
	var args []string
	seam(t, &showBead, func(string) (bd.Bead, error) { return b, nil })
	seam(t, &updateBead, func(id string, a []string) error { args = a; return nil })
	open := func() Model { return formModel(t).push(editSignalForm(b)) }
	if m := press(t, open(), esc()); len(m.modals) != 0 {
		t.Fatalf("untouched form asked to discard: %d modals", len(m.modals))
	}
	m := press(t, typeText(t, open(), "!"), ctrlS())
	if !slices.Equal(args, []string{"--title", "A signal!"}) || len(m.modals) != 0 {
		t.Errorf("args %v", args)
	}
}

func ctrlO() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlO} }

func TestFormCtrlOOpensEditorInEveryField(t *testing.T) {
	for _, m := range []Model{pressN(t, formModel(t), "s"), editForm(t)} {
		for range 3 {
			if _, cmd := send(t, m, ctrlO()); cmd == nil {
				t.Errorf("field %d: no command", m.modals[0].sf.field)
			}
			m = press(t, m, tabKey())
		}
	}
	if !strings.Contains(view(pressN(t, formModel(t), "s")), "ctrl+o $EDITOR") {
		t.Error("hint line lacks ctrl+o $EDITOR")
	}
}

func TestFormEditorDone(t *testing.T) {
	writes := 0
	seam(t, &updateBead, func(string, []string) error { writes++; return nil })
	m := editForm(t)
	f := m.modals[0].sf
	done := func(m Model, text string, err error) Model {
		m, _ = send(t, m, editorDoneMsg{id: f.id, noun: f.noun, text: text, err: err})
		return m
	}
	orig := f.desc.Value()
	if got := done(m, orig+"\n", nil); got.modals[0].sf.dirty() {
		t.Error("no-change round trip is dirty")
	}
	got := done(m, "rewritten\n", nil)
	if sf := got.modals[0].sf; sf.desc.Value() != "rewritten\n" || !sf.dirty() || writes != 0 {
		t.Errorf("desc %q dirty %v writes %d", sf.desc.Value(), sf.dirty(), writes)
	}
	if n := press(t, got, esc()); len(n.modals) != 2 || !strings.Contains(view(n), "Discard changes?") {
		t.Error("esc did not ask")
	}
	if e := done(done(m, "x", nil), "", nil).modals[0].sf; e.desc.Value() != "" || !e.dirty() {
		t.Errorf("empty result: %q", e.desc.Value())
	}
	bad := done(m, "ignored", errors.New("exit status 1"))
	if bad.modals[0].sf.desc.Value() != orig || !strings.Contains(view(bad), "exit status 1") {
		t.Errorf("error case: %q", bad.modals[0].sf.desc.Value())
	}
	// dropped when the form is gone or another one is up
	gone := done(press(t, m, esc()), "late", nil)
	if len(gone.modals) != 0 || !strings.Contains(gone.status, "editor") {
		t.Errorf("late message: modals %d status %q", len(gone.modals), gone.status)
	}
	other := pressN(t, formModel(t), "s")
	if o, _ := send(t, other, editorDoneMsg{id: f.id, noun: f.noun, text: "late"}); o.modals[0].sf.desc.Value() != "" {
		t.Error("message reached a different form")
	}
}

func TestEditorCmd(t *testing.T) {
	cases := []struct{ visual, editor, want string }{
		{"v", "e", "v"}, {"", "e", "e"}, {"", "", "vi"}, {"code --wait", "e", "code --wait"},
	}
	for _, c := range cases {
		t.Setenv("VISUAL", c.visual)
		t.Setenv("EDITOR", c.editor)
		want := []string{"sh", "-c", c.want + ` "$1"`, "sh", "/tmp/x.md"}
		if got := editorCmd("/tmp/x.md").Args; !slices.Equal(got, want) {
			t.Errorf("VISUAL=%q EDITOR=%q: %q", c.visual, c.editor, got)
		}
	}
}
