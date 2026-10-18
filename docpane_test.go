package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func dpSend(m tea.Model, msg tea.Msg) (tea.Model, tea.Cmd) { return m.Update(msg) }

func dpFile(t *testing.T, body string) string {
	p := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func dpDoc() string {
	var b strings.Builder
	for i := 0; i < 30; i++ {
		b.WriteString("Paragraph about things that wraps when the pane is narrow, and then some more words to fill it out.\n\n")
	}
	return b.String()
}

func TestDocPaneResizeRewrapsAndAnchors(t *testing.T) {
	var m tea.Model = newDocPane(dpFile(t, dpDoc()))
	m, _ = dpSend(m, tea.WindowSizeMsg{Width: 30, Height: 10})
	if v := m.View(); !strings.Contains(v, "Paragraph") {
		t.Fatalf("first render lacks the doc: %q", v)
	}
	narrow := m.(docPane).vp.TotalLineCount()
	for i := 0; i < 20; i++ {
		m, _ = dpSend(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	old := m.(docPane).vp.YOffset
	if old != 20 {
		t.Fatalf("offset before resize = %d", old)
	}
	m, _ = dpSend(m, tea.WindowSizeMsg{Width: 120, Height: 10})
	d := m.(docPane)
	if d.vp.TotalLineCount() >= narrow {
		t.Errorf("wide render has %d lines, narrow %d", d.vp.TotalLineCount(), narrow)
	}
	for _, l := range strings.Split(d.View(), "\n") {
		if ansi.StringWidth(l) > 120 {
			t.Errorf("line wider than 120: %q", l)
		}
	}
	if want := old * d.vp.TotalLineCount() / narrow; d.vp.YOffset != want {
		t.Errorf("offset = %d, want %d", d.vp.YOffset, want)
	}
}

func TestDocPaneKeys(t *testing.T) {
	var m tea.Model = newDocPane(dpFile(t, dpDoc()))
	m, _ = dpSend(m, tea.WindowSizeMsg{Width: 40, Height: 10})
	m, _ = dpSend(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if !m.(docPane).vp.AtBottom() {
		t.Error("G did not reach bottom")
	}
	m, _ = dpSend(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if !m.(docPane).vp.AtTop() {
		t.Error("g did not reach top")
	}
	for _, k := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("q")}, {Type: tea.KeyCtrlC}} {
		if _, cmd := dpSend(m, k); cmd == nil || cmd() != tea.Quit() {
			t.Errorf("%v did not quit", k)
		}
	}
}

func TestDocPaneMissingAndEmptyShowOneLine(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.md")
	for file, want := range map[string]string{missing: "no doc at " + missing, dpFile(t, " \n"): "is empty"} {
		var m tea.Model = newDocPane(file)
		m, _ = dpSend(m, tea.WindowSizeMsg{Width: 200, Height: 5})
		if v := strings.TrimSpace(m.View()); !strings.Contains(v, want) || strings.Contains(v, "\n") {
			t.Errorf("view = %q, want one line with %q", v, want)
		}
		if _, cmd := dpSend(m, tea.KeyMsg{Type: tea.KeyDown}); cmd != nil && cmd() == tea.Quit() {
			t.Error("quit on non-quit key")
		}
	}
}

func TestDocPaneUsage(t *testing.T) {
	if err := runDocPane(nil); err == nil || err.Error() != "usage: mugicrew doc-pane <body-file>" {
		t.Errorf("err = %v", err)
	}
}
