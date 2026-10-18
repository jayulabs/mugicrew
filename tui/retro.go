package tui

// r on a finished Signal, Chart, Spec, Checkpoint or Initiative starts a Robin
// retrospective (Spec mc-4ryp.7 D2, ADR 0281).

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

var keyRetro = verbKey("r", "retrospective")

// The spine read and the related edge, vars so tests touch no live bd.
var (
	readSpine = bd.Spine
	relate    = bd.Related
)

func canRetro(b *bd.Bead) bool {
	if b == nil {
		return false
	}
	switch b.Kind {
	case contract.KindSignal, contract.KindChart, contract.KindSpec, contract.KindCheckpoint, contract.KindInitiative:
		return true
	}
	return false
}

// retroKeys is r where it is bound: hinted on an eligible row, answering elsewhere on
// any row. History and Logs do not bind it, nor does Graph's typing filter.
func (m Model) retroKeys() (hinted, extra []key.Binding) {
	if m.capturing() || (m.focus.id == "" && m.tab > tabInits) {
		return nil, nil
	}
	b := m.selection()
	switch {
	case b == nil:
		return nil, []key.Binding{keyRetro}
	case canRetro(b):
		return []key.Binding{keyRetro}, nil
	}
	return nil, []key.Binding{keyRetro}
}

// retrospective is r: the kind check is inline; the spine read runs off the event
// loop under the bead's pending guard, then the confirm dialog opens.
func (m Model) retrospective(b *bd.Bead) (tea.Model, tea.Cmd) {
	if b == nil {
		m.setStatus("nothing selected")
		return m, nil
	}
	if !canRetro(b) {
		m.setStatus(fmt.Sprintf("%s is a %s, not a Signal, Chart, Spec, Checkpoint or Initiative", b.ID, b.Kind))
		return m, nil
	}
	target := *b
	return m.startRead(target.ID, "checking", func() writeDoneMsg {
		ids, open, err := readSpine(target.ID)
		return writeDoneMsg{target.ID, func(m Model) (tea.Model, tea.Cmd) {
			switch {
			case err != nil:
				m.setStatus("retrospective: " + err.Error())
			case open:
				m.setStatus(target.ID + "'s spine still has open beads")
			case len(m.modals) > 0: // never stack on a dialog the human opened meanwhile
				m.setStatus(target.ID + "'s spine is closed; press r again")
			default:
				return m.push(confirmDialog("Start a retrospective on "+target.Title+"?", nil, target.ID, func(m Model, d dialog) (Model, tea.Cmd) {
					return into(m.retroStart(target, ids))
				})), nil
			}
			return m, nil
		}}
	})
}

// retroStart files the unparented "Retrospective <title>" discussion and its related
// edge, then runs the discussion-created path. A failure names any orphan.
func (m Model) retroStart(target bd.Bead, spine []string) (tea.Model, tea.Cmd) {
	title := "Retrospective " + target.Title
	body := "A Robin retrospective on " + target.ID + ", started from the TUI.\nTarget: " + target.ID + "\nSpine: " + strings.Join(spine, " ")
	return m.startWrite(target.ID, "filing", func() writeDoneMsg {
		id, err := newDiscussion(title, body)
		if err == nil {
			err = relate(id, target.ID)
		}
		return writeDoneMsg{target.ID, func(m Model) (tea.Model, tea.Cmd) {
			if err != nil {
				s := "retrospective: " + err.Error()
				if id != "" {
					s += " (filed " + id + ")"
				}
				m.setStatus(s)
				return m, m.load()
			}
			return m.discussionCreated(discussionCreatedMsg{b: bd.Bead{ID: id, Kind: contract.KindDiscussion, Status: "open", Title: title}})
		}}
	})
}
