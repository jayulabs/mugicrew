package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
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
	var got bd.Bead
	var gotSpine []string
	seam(t, &readSpine, func(string) ([]string, bool, error) { return []string{"sp", "x"}, false, nil })
	seam(t, &newRetrospective, func(target bd.Bead, spine []string) (string, error) {
		got, gotSpine = target, spine
		return "disc", nil
	})

	// ineligible: status line, no Cmd
	n, cmd := send(t, press(t, selectBead(t, m, "bd"), runeKey("n")), runeKey("t"))
	if cmd != nil || !newOpen(n) {
		t.Errorf("ineligible: cmd=%v menu open=%v", cmd != nil, newOpen(n))
	}

	// eligible: Cmd, checking status, second press refused
	m = selectBead(t, m, "sp")
	n, cmd = send(t, press(t, m, runeKey("n")), runeKey("t"))
	if cmd == nil || !strings.Contains(statusOf(n), "checking sp…") {
		t.Fatalf("eligible: cmd=%v status=%q", cmd != nil, statusOf(n))
	}
	if n2, c2 := send(t, press(t, n, runeKey("n")), runeKey("t")); c2 != nil || !strings.Contains(statusOf(n2), "sp: checking in flight") {
		t.Errorf("second press: cmd=%v status=%q", c2 != nil, statusOf(n2))
	}

	// open spine refuses
	seam(t, &readSpine, func(string) ([]string, bool, error) { return nil, true, nil })
	_, cmd = send(t, press(t, m, runeKey("n")), runeKey("t"))
	if o := settle(t, m, cmd); len(o.modals) != 0 || !strings.Contains(statusOf(o), "sp's spine still has open beads") {
		t.Errorf("open spine: modals=%d status=%q", len(o.modals), statusOf(o))
	}

	// closed spine: dialog; y creates, relates, launches
	seam(t, &readSpine, func(string) ([]string, bool, error) { return []string{"sp", "x"}, false, nil })
	_, cmd = send(t, press(t, m, runeKey("n")), runeKey("t"))
	o := settle(t, m, cmd)
	if len(o.modals) != 1 || o.modals[0].title != "Start a retrospective on Done spec?" {
		t.Fatalf("dialog: %+v", o.modals)
	}
	o, cmd = send(t, o, runeKey("y"))
	drain(t, o, cmd, 4)
	if got.ID != "sp" || got.Title != "Done spec" || strings.Join(gotSpine, " ") != "sp x" {
		t.Errorf("created on %+v spine %v", got, gotSpine)
	}
	if got := strings.Join(*calls, ";"); strings.Contains(got, "related") || !strings.Contains(got, "startSession disc") {
		t.Errorf("calls = %q", got)
	}
}

func TestRetrospectiveCreateFailureNamesError(t *testing.T) {
	m, calls := retroModel(t)
	seam(t, &readSpine, func(string) ([]string, bool, error) { return []string{"sp"}, false, nil })
	seam(t, &newRetrospective, func(bd.Bead, []string) (string, error) { return "", errors.New("boom") })
	m = selectBead(t, m, "sp")
	_, cmd := send(t, press(t, m, runeKey("n")), runeKey("t"))
	o := settle(t, m, cmd)
	o, cmd = send(t, o, runeKey("y"))
	o = settle(t, o, cmd)
	if s := statusOf(o); !strings.Contains(s, "retrospective: boom") || strings.Contains(s, "filed") {
		t.Errorf("status = %q", s)
	}
	if got := strings.Join(*calls, ";"); strings.Contains(got, "related") || strings.Contains(got, "startSession") {
		t.Errorf("calls = %q", got)
	}
}

func TestRetrospectiveRoutedInFocus(t *testing.T) {
	m, _ := focusModel(t, 160, 40, focusFixture(), nil, map[string]bool{})
	f := ideaFocus(t, m)
	f = focusAt(t, f, "t s1")
	if _, cmd := send(t, press(t, f, runeKey("n")), runeKey("t")); cmd == nil {
		t.Error("n t swallowed in the focus view")
	}
}

func TestRetrospectiveDialogOpenedMeanwhile(t *testing.T) {
	m, _ := retroModel(t)
	seam(t, &readSpine, func(string) ([]string, bool, error) { return []string{"sp"}, false, nil })
	m = selectBead(t, m, "sp")
	n, cmd := send(t, press(t, m, runeKey("n")), runeKey("t"))
	n = n.push(confirmDialog("x", nil, "", nil)) // opened before the spine read lands
	o := settle(t, n, cmd)
	if !strings.Contains(o.deferred, "sp's spine is closed; press n t again") {
		t.Errorf("status = %q", o.deferred)
	}
}

func TestNewMenuRetrospectiveRow(t *testing.T) {
	m := rebaseModel(t)
	m, _ = send(t, m, loadedMsg{beads: append(slices.Clone(m.beads),
		bd.Bead{ID: "rspec", Kind: contract.KindSpec, Status: "open", Title: "S", Created: epoch},
		bd.Bead{ID: "rini", Kind: contract.KindInitiative, Status: "open", Title: "I", Created: epoch},
		bd.Bead{ID: "rbuild", Kind: contract.KindBuild, Status: "open", Title: "B", Created: epoch},
	), live: map[string]bool{}, sessions: map[string]session.Info{}})
	for id, want := range map[string]bool{"sig": true, "rchart": true, "rspec": true, "rcp": true, "rini": true, "rbuild": false} {
		o := press(t, selectBead(t, m, id), runeKey("n"))
		if got := strings.Contains(ansi.Strip(o.View()), "t   New retrospective"); got != want {
			t.Errorf("%s: t row = %v, want %v", id, got, want)
		}
		ref := ansi.Strip(press(t, selectBead(t, m, id), runeKey("?")).View())
		if got := strings.Contains(ref, "New retrospective"); got != want {
			t.Errorf("%s: ? lists t row = %v, want %v", id, got, want)
		}
		if want {
			checkCoverage(t, "New menu", o, false)
		}
	}
	if o := press(t, press(t, m, runeKey("#")), runeKey("n")); strings.Contains(ansi.Strip(o.View()), "New retrospective") {
		t.Error("t row shown with no selection")
	}
}

func TestNewMenuRetrospectiveRowOrder(t *testing.T) {
	o := press(t, selectBead(t, rebaseModel(t), "rcp"), runeKey("n"))
	var got []string
	for _, k := range o.modals[0].rowKeys {
		got = append(got, k[0])
	}
	if want := "s,i,u,r,t,d"; strings.Join(got, ",") != want {
		t.Errorf("rows %v, want %s", got, want)
	}
}

func TestNewMenuRetrospectivePinnedAcrossReload(t *testing.T) {
	m, _ := retroModel(t)
	var read string
	seam(t, &readSpine, func(id string) ([]string, bool, error) { read = id; return []string{id}, false, nil })
	o := press(t, selectBead(t, m, "sp"), runeKey("n"))
	o, _ = send(t, o, loadedMsg{beads: []bd.Bead{
		{ID: "aaa", Kind: contract.KindSpec, Status: "closed", Title: "Other", Created: epoch},
		{ID: "sp", Kind: contract.KindSpec, Status: "closed", Title: "Done spec", Created: epoch},
	}})
	_, cmd := send(t, o, runeKey("t"))
	settle(t, o, cmd)
	if read != "sp" {
		t.Errorf("spine read on %q, want sp", read)
	}
}

// r is unbound for retrospectives: on every bead tab and in the focus view, on any row,
// it starts nothing, answers nothing and hints nothing (Spec mc-6giio FR8, FR10).
func TestRNoLongerStartsRetrospective(t *testing.T) {
	m, _ := defaultFocus(t)
	seam(t, &readSpine, func(string) ([]string, bool, error) { t.Error("spine read"); return nil, false, nil })
	check := func(where string, m Model) {
		t.Helper()
		if h := hintsOf(m); strings.Contains(h, "retrospective") {
			t.Errorf("%s: hints %q", where, h)
		}
		for _, b := range m.tabExtra() {
			if b.Help().Desc == "retrospective" {
				t.Errorf("%s: ? lists r retrospective", where)
			}
		}
		before := statusOf(m)
		o, cmd := send(t, m, runeKey("r"))
		if cmd != nil || len(o.modals) != 0 || statusOf(o) != before || strings.Contains(statusOf(o), "not a Signal") {
			t.Errorf("%s: r did something: cmd=%v modals=%d status=%q", where, cmd != nil, len(o.modals), statusOf(o))
		}
	}
	walk := func(where string, m Model) {
		for i := range 12 { // eligible and ineligible rows alike
			check(fmt.Sprintf("%s row %d", where, i), m)
			m = press(t, m, runeKey("j"))
		}
	}
	for tab := tabIdeate; tab <= tabInits; tab++ {
		walk(tabNames[tab], press(t, m, runeKey(tabDefs[tab].key)))
	}
	walk("focus", ideaFocus(t, press(t, m, runeKey(tabDefs[tabIdeate].key))))
}
