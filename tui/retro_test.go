package tui

import (
	"errors"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

func retroModel(t *testing.T) (Model, *[]string) {
	t.Helper()
	calls := stubActions(t)
	m := sized(t, 160, 40)
	m.crew = map[string]contract.Crewmate{"robin": fakeCrew{}}
	m, _ = send(t, m, loadedMsg{beads: []bd.Bead{
		{ID: "sp", Kind: contract.KindSpec, Status: "closed", Title: "Done spec", Created: epoch},
		{ID: "bd", Kind: contract.KindBuild, Status: "closed", Title: "A build", Created: epoch},
	}})
	m = pick(t, press(t, m, runeKey("4")), "s", "any")
	return m, calls
}

func TestRetrospectiveFlow(t *testing.T) {
	m, calls := retroModel(t)
	var created, body string
	seam(t, &readSpine, func(string) ([]string, bool, error) { return []string{"sp", "x"}, false, nil })
	seam(t, &newDiscussion, func(title, b string) (string, error) { created, body = title, b; return "disc", nil })
	seam(t, &relate, func(from, to string) error { *calls = append(*calls, "related "+from+" "+to); return nil })

	// ineligible: status line, no Cmd
	n, cmd := send(t, selectBead(t, m, "bd"), runeKey("r"))
	if cmd != nil || !strings.Contains(statusOf(n), "bd is a build, not a Signal") {
		t.Errorf("ineligible: cmd=%v status=%q", cmd != nil, statusOf(n))
	}

	// eligible: Cmd, checking status, second press refused
	m = selectBead(t, m, "sp")
	n, cmd = send(t, m, runeKey("r"))
	if cmd == nil || !strings.Contains(statusOf(n), "checking sp…") {
		t.Fatalf("eligible: cmd=%v status=%q", cmd != nil, statusOf(n))
	}
	if n2, c2 := send(t, n, runeKey("r")); c2 != nil || !strings.Contains(statusOf(n2), "sp: checking in flight") {
		t.Errorf("second press: cmd=%v status=%q", c2 != nil, statusOf(n2))
	}

	// open spine refuses
	seam(t, &readSpine, func(string) ([]string, bool, error) { return nil, true, nil })
	_, cmd = send(t, m, runeKey("r"))
	if o := settle(t, m, cmd); len(o.modals) != 0 || !strings.Contains(statusOf(o), "sp's spine still has open beads") {
		t.Errorf("open spine: modals=%d status=%q", len(o.modals), statusOf(o))
	}

	// closed spine: dialog; y creates, relates, launches
	seam(t, &readSpine, func(string) ([]string, bool, error) { return []string{"sp", "x"}, false, nil })
	_, cmd = send(t, m, runeKey("r"))
	o := settle(t, m, cmd)
	if len(o.modals) != 1 || o.modals[0].title != "Start a retrospective on Done spec?" {
		t.Fatalf("dialog: %+v", o.modals)
	}
	o, cmd = send(t, o, runeKey("y"))
	drain(t, o, cmd, 4)
	if created != "Retrospective Done spec" || !strings.Contains(body, "\nTarget: sp\nSpine: sp x") {
		t.Errorf("created %q body %q", created, body)
	}
	if got := strings.Join(*calls, ";"); !strings.Contains(got, "related disc sp") || !strings.Contains(got, "startSession disc") {
		t.Errorf("calls = %q", got)
	}
}

func TestRetrospectiveEdgeFailureNamesOrphan(t *testing.T) {
	m, _ := retroModel(t)
	seam(t, &readSpine, func(string) ([]string, bool, error) { return []string{"sp"}, false, nil })
	seam(t, &newDiscussion, func(string, string) (string, error) { return "disc", nil })
	seam(t, &relate, func(string, string) error { return errors.New("boom") })
	m = selectBead(t, m, "sp")
	_, cmd := send(t, m, runeKey("r"))
	o := settle(t, m, cmd)
	o, cmd = send(t, o, runeKey("y"))
	o = settle(t, o, cmd)
	if s := statusOf(o); !strings.Contains(s, "boom") || !strings.Contains(s, "filed disc") {
		t.Errorf("status = %q", s)
	}
}

func TestRetrospectiveRoutedInFocus(t *testing.T) {
	m, _ := focusModel(t, 160, 40, focusFixture(), nil, map[string]bool{})
	f := ideaFocus(t, m)
	f = focusAt(t, f, "t s1")
	if _, cmd := send(t, f, runeKey("r")); cmd == nil {
		t.Error("r swallowed in the focus view")
	}
	if !strings.Contains(hintsOf(f), "r retrospective") {
		t.Errorf("focus hints = %q", hintsOf(f))
	}
}
