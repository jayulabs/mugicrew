package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

func checkupModel(t *testing.T) (Model, *[]string) {
	t.Helper()
	calls := stubActions(t)
	m := sized(t, 160, 40)
	m.crew = map[string]contract.Crewmate{"chopper": fakeCrew{}}
	m, _ = send(t, m, loadedMsg{beads: []bd.Bead{
		{ID: "tk", Kind: contract.KindTask, Status: "open", Title: "A task", Created: epoch},
		{ID: "cl", Kind: contract.KindTask, Status: "closed", Title: "Done task", Created: epoch},
		{ID: "ck", Kind: contract.KindCheckup, Status: "open", Title: "Repair A task", Created: epoch, Related: []string{"tk"}},
	}})
	m, _ = send(t, m, runeKey("4"))
	if sel(m) == unparentedSel {
		m, _ = send(t, m, tea.KeyMsg{Type: tea.KeySpace})
	}
	return m, calls
}

func TestCheckupHintOnEveryRowButCheckup(t *testing.T) {
	m, _ := checkupModel(t)
	for id, want := range map[string]bool{"tk": true, "ck": false} {
		if got := strings.Contains(hintsOf(selectBead(t, m, id)), "c checkup"); got != want {
			t.Errorf("%s: c checkup shown = %v, want %v", id, got, want)
		}
	}
	closed := bd.Bead{Kind: contract.KindTask, Status: "closed"}
	if len(formBindings(nil, &closed)) == 0 || !canCheckup(&closed) {
		t.Error("c must be offered on a closed bead")
	}
}

func TestCheckupRefusalIsErrorLine(t *testing.T) {
	m, calls := checkupModel(t)
	seam(t, &checkupRefusal, func(bd.Bead) error { return errors.New("stranded by a dead effort: recover it (x)") })
	m, _ = at(t, m, "tk", runeKey("c"))
	m, cmd := send(t, m, enterKey())
	settle(t, m, cmd)
	m, _ = send(t, m, cmd())
	if got := statusOf(m); !strings.Contains(got, "recover it (x)") || has(*calls, "pauseBead") {
		t.Errorf("status %q calls %v", got, *calls)
	}
}

func TestCheckupFilesFencesAndLaunches(t *testing.T) {
	m, calls := checkupModel(t)
	seam(t, &checkupRefusal, func(bd.Bead) error { return nil })
	seam(t, &openCheckupOn, func(string) (bd.Bead, bool, error) { return bd.Bead{}, false, nil })
	var problem string
	seam(t, &newCheckup, func(b bd.Bead, p string) (string, error) { problem = p; return "nk", nil })
	m, _ = at(t, m, "tk", runeKey("c"))
	m = typeText(t, m, "no edge")
	m, cmd := send(t, m, enterKey())
	m, cmd = send(t, m, cmd())
	settle(t, m, cmd)
	if problem != "no edge" || !has(*calls, "pauseBead tk") || !has(*calls, "startSession nk") {
		t.Errorf("problem %q calls %v", problem, *calls)
	}
}

func TestCheckupAttachesToOpenOne(t *testing.T) {
	m, calls := checkupModel(t)
	seam(t, &checkupRefusal, func(bd.Bead) error { return nil })
	seam(t, &openCheckupOn, func(string) (bd.Bead, bool, error) { return bd.Bead{ID: "ck"}, true, nil })
	seam(t, &newCheckup, func(bd.Bead, string) (string, error) { t.Error("filed a second Checkup"); return "", nil })
	m, _ = at(t, m, "tk", runeKey("c"))
	m, cmd := send(t, m, enterKey())
	m, cmd = send(t, m, cmd())
	settle(t, m, cmd)
	if !has(*calls, "startSession ck") {
		t.Errorf("calls %v", *calls)
	}
}

func TestCheckupDropClosesAndLiftsFence(t *testing.T) {
	m, calls := checkupModel(t)
	seam(t, &dropDrafts, func(b bd.Bead) error { *calls = append(*calls, "dropDrafts "+b.ID); return nil })
	seam(t, &closeBead, func(id, r string) error { *calls = append(*calls, "closeBead "+id+" "+r); return nil })
	seam(t, &openCheckupOn, func(string) (bd.Bead, bool, error) { return bd.Bead{}, false, nil })
	m, _ = at(t, m, "ck", runeKey("x"))
	m, cmd := send(t, m, runeKey("y"))
	settle(t, m, cmd)
	if got := strings.Join(*calls, ";"); got != "dropDrafts ck;closeBead ck dropped;unpauseBead tk" {
		t.Errorf("calls = %q", got)
	}
}
