package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/withdraw"
)

// stubWithdraw scripts the verb's Plan and act; run records each call's discard.
func stubWithdraw(t *testing.T, plan func(head string) (withdraw.Plan, error), run func(head string, discard bool) (withdraw.Result, error)) *[]string {
	t.Helper()
	var calls []string
	origPlan, origRun := planWithdraw, runWithdraw
	t.Cleanup(func() { planWithdraw, runWithdraw = origPlan, origRun })
	planWithdraw = func(h string) (withdraw.Plan, error) { calls = append(calls, "plan "+h); return plan(h) }
	runWithdraw = func(h string, d bool) (withdraw.Result, error) {
		calls = append(calls, fmt.Sprintf("withdraw %s discard=%v", h, d))
		return run(h, d)
	}
	return &calls
}

func chainSet() []bd.Bead {
	mk := func(id string, k contract.Kind, parent string) bd.Bead {
		return bd.Bead{ID: id, Kind: k, Status: "open", Title: "T-" + id, Parent: parent, Created: epoch}
	}
	return []bd.Bead{mk("b1", contract.KindBuild, "sig"), mk("b2", contract.KindBuild, "sig"), mk("sig", contract.KindSignal, "")}
}

func cleanPlan(string) (withdraw.Plan, error) {
	return withdraw.Plan{Kind: withdraw.KindWithdraw, Target: "sig", Set: chainSet()}, nil
}

func lossyPlan(string) (withdraw.Plan, error) {
	p, _ := cleanPlan("")
	p.Inventory = withdraw.Inventory{
		Beads: []withdraw.BeadLeftovers{
			{ID: "b1", Unintegrated: 3, Trees: []withdraw.TreeLeftover{{Class: withdraw.Removable, Dirty: 4}}},
			{ID: "b2", AtMost: true, Unintegrated: 2, Trees: []withdraw.TreeLeftover{{Class: withdraw.Foreign}, {Class: withdraw.Removable, Rebasing: true}}},
		},
		Containers: []withdraw.ContainerBranch{{ID: "sig", Branch: "chart/sig"}},
	}
	return p, nil
}

func refusedPlan(string) (withdraw.Plan, error) {
	p, _ := cleanPlan("")
	p.Refusals = []withdraw.Refusal{
		{Kind: withdraw.InFlight, Beads: []string{"b2"}, Fix: "abort it first: x on T-b2"},
		{Kind: withdraw.OrderedAfter, Beads: []string{"res"}, Fix: "withdraw res first"},
	}
	return p, nil
}

func xOpen(t *testing.T, w, h int, plan func(string) (withdraw.Plan, error), run func(string, bool) (withdraw.Result, error)) (Model, *[]string) {
	t.Helper()
	calls := stubWithdraw(t, plan, run)
	m, _ := actModel(t)
	m.beads = append(m.beads, chainSet()[:2]...) // the Plan's members, for titles
	m, _ = send(t, m, tea.WindowSizeMsg{Width: w, Height: h})
	m = press(t, selectBead(t, m, "sig"), runeKey("X"))
	return m, calls
}

func ok(string, bool) (withdraw.Result, error) { return withdraw.Result{Closed: []string{"sig"}}, nil }

func TestXOpensWithdrawDialogOnHeadRowsOnly(t *testing.T) {
	m, _ := actModel(t)
	stubWithdraw(t, cleanPlan, ok)
	for _, id := range []string{"res"} { // a Build
		o := selectBead(t, m, id)
		if strings.Contains(hintsOf(o), "X withdraw") {
			t.Errorf("%s hints offer X: %s", id, hintsOf(o))
		}
		o = press(t, o, runeKey("X"))
		if len(o.modals) != 0 {
			t.Errorf("X on %s opened a dialog", id)
		}
	}
	o := selectBead(t, m, "sig")
	if !strings.Contains(hintsOf(o), "X withdraw") {
		t.Errorf("head hints lack X: %s", hintsOf(o))
	}
	if !strings.Contains(hintsOf(o), "p pause") || strings.Contains(hintsOf(o), "x withdraw") {
		t.Errorf("x labels changed: %s", hintsOf(o))
	}
	o = press(t, o, runeKey("X"))
	if len(o.modals) != 1 || o.modals[0].kind != xdialog {
		t.Fatalf("X on a head opened %d modals", len(o.modals))
	}
}

func TestXOnEachTab(t *testing.T) {
	stubWithdraw(t, cleanPlan, ok)
	open := func(name string, m Model) {
		t.Helper()
		if !strings.Contains(hintsOf(m), "X withdraw") {
			t.Errorf("%s: hints lack X: %s", name, hintsOf(m))
		}
		if m = press(t, m, runeKey("X")); len(m.modals) != 1 || m.modals[0].kind != xdialog {
			t.Errorf("%s: X did not open the dialog (status %q)", name, statusOf(m))
		}
	}
	im, _ := keysModel(t)
	open("Ideate", ideaAt(t, im, "mc-1"))
	dm, _ := defogActModel(t)
	open("Defog", defogAt(t, dm, "c"))
	em, _ := execModel(t, append(execFixture(), dbead("lv", contract.KindApproval, "open", "cpA", 5)))
	open("Execute", execAt(t, unfolded(t, em), "cpA"))
}

func TestXDialogTreeWideAndNarrow(t *testing.T) {
	m, _ := xOpen(t, 160, 40, lossyPlan, ok)
	v := ansi.Strip(m.View())
	for _, want := range []string{"Withdraw ", "├ ", "└ ", "✎ 4 files changed", "↑ 3 commits", "↑ ≤2 commits", "⊘ held: skipped", "⟳ rebase stopped",
		"Closes 3 beads as withdrawn. No successor follows; chart/sig goes with it.", "Discards ✎ 4 files · ↑ ≤5 commits · ⟳ 1 stopped rebase"} {
		if !strings.Contains(v, want) {
			t.Errorf("wide view lacks %q:\n%s", want, v)
		}
	}
	if i, j := strings.Index(v, "T-sig"), strings.Index(v, "T-b1"); i < 0 || j < 0 || i > j {
		t.Errorf("the head is not listed first:\n%s", v)
	}
	m, _ = xOpen(t, 80, 40, lossyPlan, ok)
	v = ansi.Strip(m.View())
	for _, want := range []string{"✎4", "↑3", "↑≤2", "⊘", "⟳"} {
		if !strings.Contains(v, want) {
			t.Errorf("narrow view lacks %q:\n%s", want, v)
		}
	}
	for _, not := range []string{"files changed", "commits ", "held: skipped"} {
		if strings.Contains(strings.Split(v, "Discards")[0], not) {
			t.Errorf("narrow rows keep words %q:\n%s", not, v)
		}
	}
	assertXFits(t, m, 80, 40)
}

func assertXFits(t *testing.T, m Model, w, h int) {
	t.Helper()
	ls := strings.Split(ansi.Strip(m.View()), "\n")
	if len(ls) != h {
		t.Errorf("%d rows, want %d", len(ls), h)
	}
	for _, l := range ls {
		if ansi.StringWidth(l) > w {
			t.Errorf("row too wide %q", l)
		}
	}
}

func TestXDialogScrollsWithCounter(t *testing.T) {
	big := func(string) (withdraw.Plan, error) {
		p, _ := cleanPlan("")
		p.Set = nil
		for i := range 20 {
			p.Set = append(p.Set, bd.Bead{ID: fmt.Sprintf("m%02d", i), Kind: contract.KindBuild, Status: "open", Title: fmt.Sprintf("Member %02d", i), Parent: "sig"})
		}
		p.Set = append(p.Set, bd.Bead{ID: "sig", Kind: contract.KindSignal, Status: "open", Title: "Head"})
		return p, nil
	}
	m, _ := xOpen(t, 100, 20, big, ok)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "1/21") {
		t.Errorf("no k/N counter:\n%s", v)
	}
	assertXFits(t, m, 100, 20)
	for range 20 {
		m, _ = send(t, m, runeKey("j"))
	}
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "21/21") || !strings.Contains(v, "Member 19") {
		t.Errorf("cursor row not scrolled into view:\n%s", v)
	}
	assertXFits(t, m, 100, 20)
}

func TestXDialogYConfirmsWhenNothingLost(t *testing.T) {
	for _, k := range []tea_key{{"y"}, {"enter"}} {
		m, calls := xOpen(t, 120, 40, cleanPlan, ok)
		if !strings.Contains(ansi.Strip(m.View()), "y/enter withdraw") {
			t.Errorf("hint lacks y/enter withdraw")
		}
		m, cmd := send(t, m, k.msg())
		m = settle(t, m, cmd)
		if len(m.modals) != 0 || !strings.Contains(strings.Join(*calls, "|"), "withdraw sig discard=false") {
			t.Errorf("%s: modals %d calls %v", k.name, len(m.modals), *calls)
		}
		if !strings.Contains(statusOf(m), "withdrawn sig") {
			t.Errorf("status %q", statusOf(m))
		}
	}
}

func TestXDialogLossyYWarnsDConfirms(t *testing.T) {
	m, calls := xOpen(t, 120, 40, lossyPlan, ok)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "D discard and withdraw") {
		t.Errorf("hint lacks D:\n%s", v)
	}
	for _, k := range []tea_key{{"y"}, {"enter"}} {
		m, _ = send(t, m, k.msg())
	}
	if v := ansi.Strip(m.View()); len(m.modals) != 1 || !strings.Contains(v, "This discards work. Press D to discard it and withdraw.") {
		t.Fatalf("y did not warn:\n%s", v)
	}
	if strings.Contains(strings.Join(*calls, "|"), "withdraw sig") {
		t.Fatalf("y ran the verb: %v", *calls)
	}
	m, cmd := send(t, m, runeKey("D"))
	m = settle(t, m, cmd)
	if len(m.modals) != 0 || !strings.Contains(strings.Join(*calls, "|"), "withdraw sig discard=true") {
		t.Errorf("D: modals %d calls %v", len(m.modals), *calls)
	}
}

func TestXDialogDInertWhenNothingLost(t *testing.T) {
	m, calls := xOpen(t, 120, 40, cleanPlan, ok)
	m, _ = send(t, m, runeKey("D"))
	if len(m.modals) != 1 || len(*calls) != 1 { // the Plan alone
		t.Errorf("D acted: modals %d calls %v", len(m.modals), *calls)
	}
	m, _ = send(t, m, escKey())
	if len(m.modals) != 0 {
		t.Error("esc did not close")
	}
}

func TestXDialogBarred(t *testing.T) {
	m, calls := xOpen(t, 120, 40, refusedPlan, ok)
	v := ansi.Strip(m.View())
	for _, want := range []string{"Refused", "✗ in-flight effort: T-b2", "fix: abort it first: x on T-b2", "✗ ordered after the chain:", "fix: withdraw res first", "Once clear: closes 3 beads"} {
		if !strings.Contains(v, want) {
			t.Errorf("barred view lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "y/enter withdraw") || strings.Contains(v, "D discard") {
		t.Errorf("barred hints offer a confirm:\n%s", v)
	}
	for _, k := range []string{"y", "D"} {
		m, _ = send(t, m, runeKey(k))
	}
	if len(m.modals) != 1 || len(*calls) != 1 {
		t.Fatalf("y/D acted on a barred dialog: modals %d calls %v", len(m.modals), *calls)
	}
	m, _ = send(t, m, enterKey()) // jumps to the first blocker, b2, and closes
	if len(m.modals) != 0 {
		t.Fatal("enter did not close the barred dialog")
	}
	if len(*calls) != 1 {
		t.Errorf("enter ran the verb: %v", *calls)
	}
}

func TestXDialogConfirmTimeRefusalReopensBarred(t *testing.T) {
	refused := &withdraw.RefusedError{Verb: "withdraw", Target: "sig", Refusals: []withdraw.Refusal{{Kind: withdraw.InFlight, Beads: []string{"b1"}, Fix: "abort b1"}}}
	m, calls := xOpen(t, 120, 40, cleanPlan, func(string, bool) (withdraw.Result, error) { return withdraw.Result{}, refused })
	m, cmd := send(t, m, runeKey("y"))
	m = settle(t, m, cmd)
	if len(m.modals) != 1 || !m.modals[0].xs.barred {
		t.Fatalf("no barred dialog after the refusal: %d modals; calls %v", len(m.modals), *calls)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "fix: abort b1") {
		t.Errorf("fresh refusals not shown:\n%s", v)
	}
}

func TestXDialogOutcomeSummary(t *testing.T) {
	res := withdraw.Result{Closed: []string{"b1", "sig"},
		Removed: []withdraw.Item{{Kind: "tree"}, {Kind: "tree"}, {Kind: "branch"}, {Kind: "branch"}, {Kind: "branch"}},
		Kept:    []withdraw.Item{{Kind: "tree", Path: "/w/x", Holder: "someone's linked tree"}}}
	m, _ := xOpen(t, 160, 40, cleanPlan, func(string, bool) (withdraw.Result, error) { return res, nil })
	m, cmd := send(t, m, runeKey("y"))
	m = settle(t, m, cmd)
	want := "withdrawn sig · 2 trees, 3 branches removed · 1 kept: tree /w/x held by someone's linked tree"
	if got := statusOf(m); !strings.Contains(got, want) {
		t.Errorf("status %q, want it to contain %q", got, want)
	}
}

func TestXPlanErrorIsAStatus(t *testing.T) {
	m, _ := xOpen(t, 120, 40, func(string) (withdraw.Plan, error) { return withdraw.Plan{}, errors.New("graph unreadable") }, ok)
	if len(m.modals) != 0 || !strings.Contains(statusOf(m), "graph unreadable") {
		t.Errorf("modals %d status %q", len(m.modals), statusOf(m))
	}
}

// tea_key names a key press for a table.
type tea_key struct{ name string }

func (k tea_key) msg() tea.KeyMsg {
	if k.name == "enter" {
		return enterKey()
	}
	return runeKey(k.name)
}

// X runs the Plan's bd, git and Forge reads in a Cmd, never in Update (ADR 0098 §1):
// the press starts none, the Cmd reads once, and a confirm-time refusal re-plans
// inside the verb's Cmd, not when its result is applied.
func TestXPlansOffTheEventLoop(t *testing.T) {
	refuse := func(string, bool) (withdraw.Result, error) {
		return withdraw.Result{}, &withdraw.RefusedError{Refusals: []withdraw.Refusal{{Kind: withdraw.InFlight, Beads: []string{"b2"}}}}
	}
	calls := stubWithdraw(t, cleanPlan, refuse)
	m, _ := actModel(t)
	m.beads = append(m.beads, chainSet()[:2]...)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = selectBead(t, m, "sig")
	*calls = nil

	m, cmd := send(t, m, runeKey("X"))
	if len(*calls) != 0 || cmd == nil || len(m.modals) != 0 {
		t.Fatalf("press X: calls %v, cmd nil=%v, modals %d", *calls, cmd == nil, len(m.modals))
	}
	msg := cmd()
	if len(*calls) != 1 {
		t.Fatalf("Cmd calls %v, want one plan", *calls)
	}
	m, _ = send(t, m, msg)
	if len(m.modals) != 1 || len(*calls) != 1 {
		t.Fatalf("done: modals %d, calls %v", len(m.modals), *calls)
	}

	m, cmd = send(t, m, runeKey("y"))
	n := len(*calls)
	msg = nil
	if b, ok := cmd().(tea.BatchMsg); ok { // the verb rides with its Recents save
		for _, c := range b {
			if w, ok := c().(writeDoneMsg); ok {
				msg = w
			}
		}
	}
	if len(*calls) != n+2 { // the verb, then its re-plan
		t.Fatalf("verb Cmd calls %v, want withdraw then plan", (*calls)[n:])
	}
	n = len(*calls)
	m, _ = send(t, m, msg)
	if len(*calls) != n {
		t.Errorf("applying the result called %v on the event loop", (*calls)[n:])
	}
}

// Counts are pluralised: one commit, one branch, and -es for branches.
func TestXPluralisation(t *testing.T) {
	if got := (xTags{commits: 1}).String(true); got != "↑ 1 commit" {
		t.Errorf("one commit tag = %q", got)
	}
	if got := (xTags{commits: 2, atMost: true}).String(true); got != "↑ ≤2 commits" {
		t.Errorf("many commits tag = %q", got)
	}
	inv := withdraw.Inventory{Beads: []withdraw.BeadLeftovers{{ID: "b", Unintegrated: 1}}}
	if got := xSummary(inv, []bd.Bead{{ID: "b"}}); !strings.Contains(got, "↑ 1 commit.") {
		t.Errorf("summary = %q", got)
	}
	got := resultSummary("w", withdraw.Result{Removed: []withdraw.Item{{Kind: withdraw.ItemBranch}, {Kind: withdraw.ItemBranch}}})
	if !strings.Contains(got, "2 branches removed") {
		t.Errorf("summary = %q", got)
	}
}

// A grandchild renders under its own parent, not flat under the head.
func TestXTreeGrandchildStaysUnderParent(t *testing.T) {
	mk := func(id string, k contract.Kind, parent, causedBy string) bd.Bead {
		return bd.Bead{ID: id, Kind: k, Status: "open", Title: id, Parent: parent, CausedBy: causedBy, Created: epoch}
	}
	// d1 is under chart, which is spec's predecessor: two levels below cp's.
	set := []bd.Bead{
		mk("d1", contract.KindBuild, "chart", ""), mk("chart", contract.KindChart, "", ""),
		mk("spec", contract.KindSpec, "", "chart"), mk("cp", contract.KindCheckpoint, "", "spec"),
	}
	rows := xTree(set)
	want := []string{"cp", "└ spec", "  └ chart", "    └ d1"}
	var have []string
	for _, r := range rows {
		have = append(have, r.prefix+r.b.ID)
	}
	if strings.Join(have, "|") != strings.Join(want, "|") {
		t.Errorf("tree = %v, want %v", have, want)
	}
}

// A barred dialog whose re-plan failed says so instead of "Closes 0 beads".
func TestXDialogBarredUnreadablePlan(t *testing.T) {
	refused := &withdraw.RefusedError{Verb: "withdraw", Target: "sig", Refusals: []withdraw.Refusal{{Kind: withdraw.InFlight, Beads: []string{"b1"}, Fix: "abort b1"}}}
	m, _ := xOpen(t, 120, 40, cleanPlan, ok)
	next, _ := m.withdrawDone("sig", withdraw.Result{}, refused, withdraw.Plan{}, errors.New("boom"))
	m = next.(Model)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "could not be read") || strings.Contains(v, "Closes 0") {
		t.Errorf("view:\n%s", v)
	}
}

// esc is labelled close in the withdraw and cancel dialogs, never cancel.
func TestXDialogEscHintIsClose(t *testing.T) {
	m, _ := xOpen(t, 120, 40, cleanPlan, ok)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "esc close") || strings.Contains(v, "esc cancel") {
		t.Errorf("view:\n%s", v)
	}
}
