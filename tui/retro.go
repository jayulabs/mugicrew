package tui

// n t on a finished Signal, Chart, Spec, Checkpoint or Initiative starts a Robin
// retrospective (Spec mc-4ryp.7 D2, ADR 0281).

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// The spine read and the Retrospective create (which writes the related edge in the
// same bd create), vars so tests touch no live bd.
var (
	readSpine        = bd.Spine
	newRetrospective = bd.NewRetrospective
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

// retrospective is the New menu's t row: the kind check is inline; the spine read runs off the event
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
				m.setStatus(target.ID + "'s spine is closed; press n t again")
			default:
				return m.push(confirmDialog("Start a retrospective on "+target.Title+"?", nil, target.ID, func(m Model, d dialog) (Model, tea.Cmd) {
					return into(m.retroStart(target, ids))
				})), nil
			}
			return m, nil
		}}
	})
}

// retroStart files the unparented "Retrospective <title>" discussion with its related
// edge in one create, then runs the discussion-created path.
func (m Model) retroStart(target bd.Bead, spine []string) (tea.Model, tea.Cmd) {
	title := "Retrospective " + target.Title
	return m.startWrite(target.ID, "filing", func() writeDoneMsg {
		id, err := newRetrospective(target, spine)
		return writeDoneMsg{target.ID, func(m Model) (tea.Model, tea.Cmd) {
			if err != nil {
				m.setStatus("retrospective: " + err.Error())
				return m, m.load()
			}
			return m.discussionCreated(discussionCreatedMsg{b: bd.Bead{ID: id, Kind: contract.KindDiscussion, Status: "open", Title: title}})
		}}
	})
}
