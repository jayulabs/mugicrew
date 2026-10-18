package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

var (
	keyDiscussions      = key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "discussions"))
	keyDiscussionsClose = key.NewBinding(key.WithKeys("esc", "D"), key.WithHelp("esc/D", "close"))
	keyDiscussionDrop   = key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "drop"))
)

// newDiscussion is the create seam, a var so tests file nothing against a live bd.
var newDiscussion = bd.NewDiscussion

// discussionCreatedMsg is a new discussion's create outcome.
type discussionCreatedMsg struct {
	b   bd.Bead
	err error
}

// discussionState is the row's state words, joined from the session table (ADR 0194 §4).
func (m Model) discussionState(id string) string {
	s := m.sessions[id]
	switch {
	case !s.Has():
		return "no session"
	case s.State == session.Exited:
		return "exited"
	}
	switch humanWait(s, false) {
	case session.WaitTurn:
		return "your turn"
	case session.WaitAsk:
		return "needs approval"
	}
	return "live"
}

// discussionsOpen is D: the Robin modal over the frame, + New discussion first and then
// the snapshot's open discussions, newest first. A second D pops it.
func (m Model) discussionsOpen() (tea.Model, tea.Cmd) {
	if n := len(m.modals); n > 0 && m.modals[n-1].discussions {
		return m.pop(), nil
	}
	var ds []bd.Bead
	for _, b := range m.beads {
		if b.Kind == contract.KindDiscussion && b.Status != "closed" {
			ds = append(ds, b)
		}
	}
	slices.SortStableFunc(ds, func(a, b bd.Bead) int { return b.Created.Compare(a.Created) })
	inner := m.pickerInner()
	items := []string{"+ New discussion"}
	ids := []string{""}
	g := m.gateData()
	for _, b := range ds {
		tail := fmt.Sprintf("  %s · %s", m.discussionState(b.ID), HumanAge(b.Created))
		fixed := lipgloss.Width(g.sbrn(&b, "", false, 0))
		title := ansi.Truncate(b.Title, max(inner-fixed-len(tail), 1), "…")
		items, ids = append(items, g.sbrn(&b, title, false, 0)+dimSt.Render(tail)), append(ids, b.ID)
	}
	d := pickerDialog("Discussions", items, "", func(m Model, d dialog) (Model, tea.Cmd) {
		if d.cur == 0 {
			return m.discussionNew()
		}
		b, ok := m.pinned(ids[d.cur])
		if !ok {
			m.setStatus("discussion " + ids[d.cur] + " no longer listed")
			return m, nil
		}
		return into(m.launchAs(m.gateData(), b, false))
	})
	d.discussions, d.shut = true, keyDiscussionsClose
	d.dropAct = func(m Model, d dialog) (Model, tea.Cmd) {
		if d.cur == 0 {
			return m, nil
		}
		b, ok := m.pinned(ids[d.cur])
		if !ok {
			return m, nil
		}
		return into(m.discussionDrop(b))
	}
	return m.push(d), nil
}

// discussionNew creates the Discussion <YYYY-MM-DD HH:MM> bead off the event loop (ADR
// 0194 §5); discussionCreated then runs the normal launch on it.
func (m Model) discussionNew() (Model, tea.Cmd) {
	title := "Discussion " + time.Now().Format("2006-01-02 15:04")
	return m, func() tea.Msg {
		id, err := newDiscussion(title, "A Robin discussion: read-only Q&A about the Workspace, started from the TUI.")
		return discussionCreatedMsg{b: bd.Bead{ID: id, Kind: contract.KindDiscussion, Status: "open", Title: title}, err: err}
	}
}

// discussionCreated starts nothing on a failed create; otherwise it launches the new
// bead (claim, Robin's Launch, start, attach) and reloads so it shows in the modal even
// if the start fails and releases the claim.
func (m Model) discussionCreated(msg discussionCreatedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.setStatus("new discussion: " + msg.err.Error())
		return m, nil
	}
	next, cmd := m.launchNow(msg.b, false)
	return next, tea.Batch(cmd, next.(Model).load())
}

// discussionDrop is x on a row: a session drops through the existing y/n dialog; a bead
// with none (a failed start leaves one) asks the same and closes it as dropped.
func (m Model) discussionDrop(b bd.Bead) (tea.Model, tea.Cmd) {
	g := m.gateData()
	if g.sessions[b.ID].Has() {
		return m.dropKey(g, b)
	}
	if why := g.busyWhy(&b); why != "" {
		m.setStatus(why)
		return m, nil
	}
	title, closeIt := "Drop discussion?", func() error { return closeBead(b.ID, "dropped") }
	if b.Kind == contract.KindCheckup {
		title, closeIt = "Drop Checkup?", func() error { return closeCheckup(b) }
	}
	return m.push(promptDialog(title, func(w int) []string { return []string{confirmDropPrompt(b.Title, w)} }, b.ID,
		func(m Model, d dialog) (Model, tea.Cmd) {
			return into(m.startAct(d.id, "dropping", func() writeDoneMsg {
				// A session-less discussion may still hold Drafts (tmux died before hitl-run published them).
				err := dropDrafts(b)
				if err == nil {
					err = closeIt()
				}
				return writeDoneMsg{d.id, func(m Model) (tea.Model, tea.Cmd) {
					if err != nil {
						m.setStatus("drop: " + err.Error())
						return m, nil
					}
					m.setStatus("dropped " + cmp.Or(b.Title, d.id))
					return m, m.load()
				}}
			}))
		})), nil
}

// finishedName is what ✓ … finished names: a discussion's title, any other bead's id.
func (m Model) finishedName(id string) string {
	if b, ok := m.pinned(id); ok && b.Kind == contract.KindDiscussion {
		return cmp.Or(strings.TrimSpace(b.Title), id)
	}
	return id
}
