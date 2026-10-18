package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/mdrender"
)

// docPane is the hidden HITL doc-pane viewer (ADR 0237): a pager over a body
// file's Markdown that re-renders at the pane's width on every resize.
type docPane struct {
	src, note string // note, when set, is the one line shown instead of the doc
	vp        viewport.Model
	ready     bool
}

func newDocPane(file string) docPane {
	b, err := os.ReadFile(file)
	switch {
	case err != nil:
		return docPane{note: "no doc at " + file}
	case strings.TrimSpace(string(b)) == "":
		return docPane{note: file + " is empty"}
	}
	return docPane{src: string(b)}
}

func (m docPane) Init() tea.Cmd { return nil }

func (m docPane) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		content := m.note
		if m.note == "" {
			content = mdrender.Render(m.src, msg.Width)
		}
		if !m.ready {
			m.vp = viewport.New(msg.Width, msg.Height)
			m.vp.SetContent(content)
			m.ready = true
			break
		}
		old, oldTotal := m.vp.YOffset, m.vp.TotalLineCount()
		m.vp.Width, m.vp.Height = msg.Width, msg.Height
		m.vp.SetContent(content)
		if oldTotal > 0 {
			m.vp.SetYOffset(old * m.vp.TotalLineCount() / oldTotal) // proportional anchor
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "g", "home":
			m.vp.GotoTop()
			return m, nil
		case "G", "end":
			m.vp.GotoBottom()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m docPane) View() string {
	if !m.ready {
		return ""
	}
	return m.vp.View()
}

func runDocPane(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: mugicrew doc-pane <body-file>")
	}
	_, err := tea.NewProgram(newDocPane(args[0]), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}
