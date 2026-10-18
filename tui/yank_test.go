package tui

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// yankLog swaps the clipboard writer for one that records what it was handed.
func yankLog(t *testing.T) *[]string {
	t.Helper()
	var got []string
	old := copyWriter
	copyWriter = func(s string) { got = append(got, s) }
	t.Cleanup(func() { copyWriter = old })
	return &got
}

func TestYankCopiesSelectedBead(t *testing.T) {
	got := yankLog(t)
	m := graphModel(t, 120, 30)
	want := sel(m)
	m = press(t, m, runeKey("y"))
	if len(*got) != 1 || (*got)[0] != want || m.status != "copied "+want {
		t.Errorf("copied %v, status %q, want %q", *got, m.status, want)
	}
}

func TestYankInFocusCopiesFocusCursor(t *testing.T) {
	got := yankLog(t)
	m, _ := defaultFocus(t)
	m = ideaFocus(t, m)
	want := m.focusBead().ID
	m = press(t, m, runeKey("y"))
	if len(*got) != 1 || (*got)[0] != want || m.status != "copied "+want {
		t.Errorf("copied %v, status %q, want %q", *got, m.status, want)
	}
}

func TestYankWithNoSelectionDoesNothing(t *testing.T) {
	got := yankLog(t)
	m := press(t, sized(t, 120, 30), runeKey("y")) // empty Ideate list
	if len(*got) != 0 || m.status != "" {
		t.Errorf("copied %v, status %q", *got, m.status)
	}
}

func TestYankInTextCaptureTypesY(t *testing.T) {
	got := yankLog(t)
	m := press(t, graphModel(t, 120, 30), runeKey("/"))
	if !m.capturing() {
		t.Fatal("/ did not open the Graph query")
	}
	m = press(t, m, runeKey("y"))
	if len(*got) != 0 || !strings.Contains(ansi.Strip(m.View()), "y") || m.status != "" {
		t.Errorf("copied %v, status %q", *got, m.status)
	}
}

func TestYankUnderModalCopiesNothingAndConfirms(t *testing.T) {
	got := yankLog(t)
	confirmed := false
	m := graphModel(t, 120, 30).push(confirmDialog("Sure?", nil, "", func(m Model, _ dialog) (Model, tea.Cmd) {
		confirmed = true
		return m, nil
	}))
	m = press(t, m, runeKey("y"))
	if !confirmed || len(*got) != 0 {
		t.Errorf("confirmed=%v copied %v", confirmed, *got)
	}
}

func TestYankLeavesCtrlCQuitting(t *testing.T) {
	yankLog(t)
	_, cmd := send(t, sized(t, 120, 30), tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c: no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("ctrl+c did not quit")
	}
}

func TestRefListsYank(t *testing.T) {
	v := ansi.Strip(press(t, graphModel(t, 120, 40), runeKey("?")).View())
	if !strings.Contains(v, "copy bead id") {
		t.Errorf("? reference lacks copy bead id:\n%s", v)
	}
	if strings.Contains(ansi.Strip(graphModel(t, 120, 40).View()), "copy bead id") {
		t.Error("hints bar shows the shell key")
	}
}

func TestOSCCopyEmitsBase64ID(t *testing.T) {
	var buf bytes.Buffer
	oscCopy(&buf)("mc-23p5")
	if !strings.Contains(buf.String(), "\x1b]52;") || !strings.Contains(buf.String(), base64.StdEncoding.EncodeToString([]byte("mc-23p5"))) {
		t.Errorf("no OSC 52 with the base64 id: %q", buf.String())
	}
}
