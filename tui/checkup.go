package tui

import (
	"cmp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/chopper"
	"jayulabs.org/mugicrew/contract"
)

var keyCheckup = verbKey("c", "checkup")

// The launch's reads and writes, vars so tests file nothing against a live bd.
var (
	checkupRefusal = chopper.CheckupRefusal
	openCheckupOn  = bd.OpenCheckupOn
	newCheckup     = bd.NewCheckup
)

// canCheckup is c's gate: any bead but a Checkup, open or closed (ADR 0274).
func canCheckup(b *bd.Bead) bool { return b != nil && b.Kind != contract.KindCheckup }

// checkupFiledMsg is the launch's find-or-create-and-fence outcome.
type checkupFiledMsg struct {
	ck  bd.Bead
	err error
}

// checkupKey is c: a one-line problem prompt (blank allowed), then the launch verb's
// shared refusal, find-or-create and fence off the event loop, then the session launch.
func (m Model) checkupKey(b bd.Bead) (tea.Model, tea.Cmd) {
	if m.refusePending(b.ID) {
		return m, nil
	}
	d := inputDialog("Checkup "+cmp.Or(b.Title, b.ID), []string{"problem (optional)"}, b.ID,
		func(m Model, d dialog) (Model, tea.Cmd) {
			problem := strings.TrimSpace(d.inputs[0].text)
			return m, func() tea.Msg { return fileCheckup(b, problem) }
		})
	return m.push(d), nil
}

// fileCheckup is runCheckup's body up to the launch: refuse, find or file the
// Checkup, fence the target.
func fileCheckup(target bd.Bead, problem string) checkupFiledMsg {
	id, err := chopper.FileCheckup(chopper.CheckupFiling{Refusal: checkupRefusal, OpenOn: openCheckupOn, New: newCheckup, Fence: pauseBead}, target, problem)
	if err != nil {
		return checkupFiledMsg{err: err}
	}
	return checkupFiledMsg{ck: bd.Bead{ID: id, Kind: contract.KindCheckup, Status: "open", Title: "Repair " + target.Title}}
}

// checkupFiled launches (or attaches) the Checkup's session; a refusal or failure is
// the usual error line.
func (m Model) checkupFiled(msg checkupFiledMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.setStatus(msg.err.Error())
		return m, nil
	}
	if b, ok := m.pinned(msg.ck.ID); ok {
		msg.ck = b // a stored Checkup: its status and session decide resume or attach
	}
	next, cmd := m.launchAs(m.gateData(), msg.ck, false)
	return next, tea.Batch(cmd, next.(Model).load())
}

// closeCheckup closes b dropped, then lifts paused:checkup from each related bead
// no other open Checkup still holds.
func closeCheckup(b bd.Bead) error {
	if err := closeBead(b.ID, "dropped"); err != nil {
		return err
	}
	for _, t := range b.Related {
		if _, held, err := openCheckupOn(t); err != nil {
			return err
		} else if held {
			continue
		}
		if err := unpauseBead(t, bd.PausedCheckup); err != nil {
			return err
		}
	}
	return nil
}
