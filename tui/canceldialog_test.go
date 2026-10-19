package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/withdraw"
)

// planReleases are the releases each planCancel call was given.
var planReleases [][]string

type cancelCall struct {
	release []string
	discard bool
}

// stubCancel scripts the Cancel verb's Plan and act; the act records each call.
func stubCancel(t *testing.T, plan func() withdraw.Plan, run func() (withdraw.Result, error)) *[]cancelCall {
	t.Helper()
	var calls []cancelCall
	planReleases = nil
	origPlan, origRun := planCancel, runCancel
	t.Cleanup(func() { planCancel, runCancel = origPlan, origRun })
	planCancel = func(_ string, release []string) (withdraw.Plan, error) {
		p := plan()
		planReleases = append(planReleases, release)
		p.Released = release
		return p, nil
	}
	runCancel = func(_ string, release []string, discard bool) (withdraw.Result, error) {
		calls = append(calls, cancelCall{release, discard})
		return run()
	}
	return &calls
}

// cancelMembers are two member chains of ini: ca (a Chart with one Build) and cb (a Signal).
func cancelMembers() []bd.Bead {
	mk := func(id string, k contract.Kind, parent string) bd.Bead {
		return bd.Bead{ID: id, Kind: k, Status: "open", Title: "T-" + id, Parent: parent, Created: epoch}
	}
	ca := mk("ca", contract.KindSignal, "ini")
	return []bd.Bead{ca, mk("cab", contract.KindBuild, "ca"), mk("cb", contract.KindSignal, "ini")}
}

func cancelPlan(locked ...withdraw.Lock) func() withdraw.Plan {
	return func() withdraw.Plan {
		set := append(cancelMembers(), bd.Bead{ID: "ini", Kind: contract.KindInitiative, Status: "open", Title: "An initiative"})
		mem := cancelMembers()
		chains := []withdraw.Chain{{Head: mem[0], Beads: []string{"cab", "ca"}}, {Head: mem[2], Beads: []string{"cb"}}}
		return withdraw.Plan{Kind: withdraw.KindCancel, Target: "ini", Set: set, Chains: chains, Locked: locked,
			Inventory: withdraw.Inventory{Beads: []withdraw.BeadLeftovers{
				{ID: "cab", Unintegrated: 3, Trees: []withdraw.TreeLeftover{{Class: withdraw.Removable, Dirty: 4}}},
			}}}
	}
}

func cancelOpenAt(t *testing.T, w int, plan func() withdraw.Plan) (Model, *[]cancelCall) {
	t.Helper()
	calls := stubCancel(t, plan, func() (withdraw.Result, error) { return withdraw.Result{Closed: []string{"ini"}}, nil })
	m, _ := actModel(t)
	m.beads = append(m.beads, cancelMembers()...)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: w, Height: 40})
	return press(t, selectBead(t, m, "ini"), runeKey("X")), calls
}

func viewOf(m Model) string { return ansi.Strip(m.View()) }

func TestXOnInitiativeOpensCancelDialogEverywhere(t *testing.T) {
	stubCancel(t, cancelPlan(), func() (withdraw.Result, error) { return withdraw.Result{}, nil })
	open := func(name string, m Model) {
		t.Helper()
		if !strings.Contains(hintsOf(m), "X cancel") {
			t.Errorf("%s: hints lack X: %s", name, hintsOf(m))
		}
		m = press(t, m, runeKey("X"))
		if len(m.modals) != 1 || m.modals[0].xs.cancel == nil {
			t.Errorf("%s: X did not open the cancel dialog (status %q)", name, statusOf(m))
		}
	}
	am, _ := actModel(t)
	open("Graph", selectBead(t, am, "ini"))
	im, _ := keysModel(t)
	open("Ideate", ideaAt(t, im, "i:ini"))
	dm, _ := defogActModel(t)
	open("Defog", defogAt(t, dm, "i:ini"))
	em, _ := execModel(t, append(execFixture(), dbead("lv", contract.KindApproval, "open", "cpA", 5)))
	open("Execute", execAt(t, unfolded(t, em), "i:i1"))
	fm, _ := focusModel(t, 160, 40, focusFixture(), nil, map[string]bool{})
	open("focus", ideaFocus(t, fm))
}

func TestCancelTallyFollowsReleaseAndClose(t *testing.T) {
	m, _ := cancelOpenAt(t, 160, cancelPlan())
	v := viewOf(m)
	for _, want := range []string{"Cancel An initiative?", "Closes 2 chains (3 beads) · releases 0 chains.", "Discards ✎ 4 files · ↑ 3 commits", "(●) close  ( ) release"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view lacks %q:\n%s", want, v)
		}
	}
	m = press(t, m, runeKey("r")) // ca, the cursor row, holds the discard
	v = viewOf(m)
	for _, want := range []string{"Closes 1 chain (1 bead) · releases 1 chain.", "Nothing is discarded.", "( ) close  (●) release"} {
		if !strings.Contains(v, want) {
			t.Fatalf("after r the view lacks %q:\n%s", want, v)
		}
	}
	m = press(t, m, runeKey("c"))
	if v = viewOf(m); !strings.Contains(v, "Closes 2 chains (3 beads) · releases 0 chains.") {
		t.Errorf("c did not close the row again:\n%s", v)
	}
}

func TestCancelLockedRowShowsReasonAndRefusesRelease(t *testing.T) {
	m, _ := cancelOpenAt(t, 160, cancelPlan(withdraw.Lock{Head: "ca", Why: "Require Initiatives: adopt it elsewhere first"}))
	v := viewOf(m)
	for _, want := range []string{"🔒 locked", "🔒 Require Initiatives: adopt it elsewhere first"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view lacks %q:\n%s", want, v)
		}
	}
	m = press(t, m, runeKey("r"))
	v = viewOf(m)
	if !strings.Contains(v, "T-ca stays closed: Require Initiatives: adopt it elsewhere first") || !strings.Contains(v, "releases 0 chains") {
		t.Errorf("r on a locked row released or did not say why:\n%s", v)
	}
	m = press(t, m, runeKey("j")) // the reason shows only while highlighted
	if v = viewOf(m); strings.Contains(v, "🔒 Require") {
		t.Errorf("the reason shows on a row that is not highlighted:\n%s", v)
	}
}

func TestCancelChoiceMovesToItsOwnLineUnder70(t *testing.T) {
	line := func(m Model, title string) string {
		for _, l := range strings.Split(viewOf(m), "\n") {
			if strings.Contains(l, title) {
				return l
			}
		}
		return ""
	}
	wide, _ := cancelOpenAt(t, 160, cancelPlan())
	if l := line(wide, "T-cb"); !strings.Contains(l, "(●) close") {
		t.Errorf("wide: the choice is not on the chain's line: %q", l)
	}
	narrow, _ := cancelOpenAt(t, 70, cancelPlan())
	if l := line(narrow, "T-cb"); strings.Contains(l, "(●) close") {
		t.Errorf("narrow: the choice is still on the chain's line: %q", l)
	}
	if !strings.Contains(viewOf(narrow), "  (●) close  ( ) release") {
		t.Errorf("narrow: no choice line:\n%s", viewOf(narrow))
	}
}

func TestCancelReviewEscKeepsChoicesAndYRunsCancelWithTheReleases(t *testing.T) {
	m, calls := cancelOpenAt(t, 160, cancelPlan())
	m = press(t, press(t, m, runeKey("j")), runeKey("r")) // release cb
	m = press(t, m, enterKey())
	v := viewOf(m)
	for _, want := range []string{"Closes 1 chain:", "T-ca", "↑ 3 commits", "Releases 1 chain:", "T-cb", "Discards ✎ 4 files · ↑ 3 commits"} {
		if !strings.Contains(v, want) {
			t.Fatalf("review lacks %q:\n%s", want, v)
		}
	}
	if len(*calls) != 0 {
		t.Fatal("enter ran the verb")
	}
	m = press(t, m, escKey())
	if v = viewOf(m); !strings.Contains(v, "( ) close  (●) release") || len(m.modals) != 1 {
		t.Fatalf("esc did not return to the rows with the choice kept:\n%s", v)
	}
	m = press(t, press(t, m, enterKey()), runeKey("y"))
	if want := fmt.Sprint([]cancelCall{{[]string{"cb"}, true}}); fmt.Sprint(*calls) != want {
		t.Errorf("cancel calls = %v, want %s", *calls, want)
	}
	if len(m.modals) != 0 || !strings.Contains(statusOf(m), "cancelled ini") {
		t.Errorf("after y: %d modals, status %q", len(m.modals), statusOf(m))
	}
}

func TestCancelWithoutDiscardRunsWithoutIt(t *testing.T) {
	m, calls := cancelOpenAt(t, 160, cancelPlan())
	m = press(t, m, runeKey("r")) // ca held the discard
	m = press(t, press(t, m, enterKey()), enterKey())
	if want := fmt.Sprint([]cancelCall{{[]string{"ca"}, false}}); fmt.Sprint(*calls) != want {
		t.Errorf("cancel calls = %v, want %s", *calls, want)
	}
}

func TestCancelBarredMakesYInertAndReopensBarredOnConfirmRefusal(t *testing.T) {
	refused := func() withdraw.Plan {
		p := cancelPlan()()
		p.Refusals = []withdraw.Refusal{{Kind: withdraw.InFlight, Beads: []string{"cab"}, Fix: "abort it first: x on T-cab"}}
		return p
	}
	m, calls := cancelOpenAt(t, 160, refused)
	v := viewOf(m)
	for _, want := range []string{"Refused", "✗ in-flight effort: T-cab", "fix: abort it first: x on T-cab", "Once clear:"} {
		if !strings.Contains(v, want) {
			t.Fatalf("barred view lacks %q:\n%s", want, v)
		}
	}
	for _, k := range []tea.KeyMsg{runeKey("y"), runeKey("D"), runeKey("r"), enterKey()} {
		m = press(t, m, k)
		if len(*calls) != 0 {
			t.Fatalf("%q ran the verb on a barred dialog", k.String())
		}
	}

	// A refusal at confirm time reopens the dialog barred.
	clean, _ := cancelOpenAt(t, 160, cancelPlan())
	runCancel = func(string, []string, bool) (withdraw.Result, error) {
		return withdraw.Result{}, &withdraw.RefusedError{Verb: "cancel", Target: "ini", Refusals: refused().Refusals}
	}
	clean = press(t, press(t, clean, enterKey()), runeKey("y"))
	if len(clean.modals) != 1 || !clean.modals[0].xs.barred || !strings.Contains(viewOf(clean), "Refused") {
		t.Errorf("a confirm-time refusal did not reopen barred:\n%s", viewOf(clean))
	}
}

// At a wide dialog the cancel rows carry the worded tags, as the withdraw rows do.
func TestCancelRowsShowWideTags(t *testing.T) {
	m, _ := cancelOpenAt(t, 160, cancelPlan())
	if v := viewOf(m); !strings.Contains(v, "✎ 4 files changed") || !strings.Contains(v, "↑ 3 commits") {
		t.Errorf("wide cancel rows lack worded tags:\n%s", v)
	}
}

// The rows come from the fresh Plan, not the TUI's possibly stale snapshot: a chain
// the snapshot lacks still gets its row.
func TestCancelRowsComeFromThePlan(t *testing.T) {
	plan := func() withdraw.Plan {
		p := cancelPlan()()
		extra := bd.Bead{ID: "cc", Kind: contract.KindSignal, Status: "open", Title: "T-cc", Parent: "ini", Created: epoch}
		p.Chains = append(p.Chains, withdraw.Chain{Head: extra, Beads: []string{"cc"}})
		return p
	}
	m, _ := cancelOpenAt(t, 160, plan)
	if v := viewOf(m); !strings.Contains(v, "T-cc") || !strings.Contains(v, "Closes 3 chains") {
		t.Errorf("plan-only chain has no row:\n%s", v)
	}
}

// A confirm that stops on a changed graph reopens the dialog planned with the same
// releases, with those chains still shown released.
func TestCancelReopenKeepsReleaseChoices(t *testing.T) {
	m, calls := cancelOpenAt(t, 160, cancelPlan())
	runCancel = func(_ string, release []string, discard bool) (withdraw.Result, error) {
		*calls = append(*calls, cancelCall{release, discard})
		return withdraw.Result{}, &withdraw.DiscardRequiredError{Verb: "cancel", Target: "ini"}
	}
	m = press(t, m, runeKey("r")) // release ca
	m = press(t, m, enterKey())
	m, cmd := send(t, m, runeKey("y"))
	m = settle(t, m, cmd)
	if last := planReleases[len(planReleases)-1]; fmt.Sprint(last) != "[ca]" {
		t.Errorf("re-plan releases = %v, want [ca]", last)
	}
	if len(m.modals) != 1 || !m.modals[0].xs.cancel.release[0] {
		t.Errorf("reopened dialog lost the release choice: %+v", m.modals)
	}
}
