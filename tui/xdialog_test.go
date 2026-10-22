package tui

import (
	"errors"
	"fmt"
	"slices"
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

func taskPlan(string) (withdraw.Plan, error) {
	return withdraw.Plan{Kind: withdraw.KindWithdraw, Target: "tk", Set: []bd.Bead{{ID: "tk", Kind: contract.KindTask, Status: "open", Title: "T-tk", Created: epoch}}}, nil
}

func taskModel(t *testing.T, plan func(string) (withdraw.Plan, error)) (Model, *[]string) {
	t.Helper()
	calls := stubWithdraw(t, plan, func(h string, _ bool) (withdraw.Result, error) { return withdraw.Result{Closed: []string{h}}, nil })
	m, _ := actModel(t)
	m, _ = send(t, m, loadedMsg{beads: append(actionBeads(), bd.Bead{ID: "tk", Kind: contract.KindTask, Status: "open", Title: "T-tk", Created: epoch}), live: map[string]bool{"live": true}, sessions: actionSessions()})
	return selectBead(t, m, "tk"), calls
}

func TestXOpensOnAnUnclosedTaskRow(t *testing.T) {
	m, _ := taskModel(t, taskPlan)
	if !strings.Contains(hintsOf(m), "X withdraw") {
		t.Errorf("Task hints lack X: %s", hintsOf(m))
	}
	m = press(t, m, runeKey("X"))
	if len(m.modals) != 1 || m.modals[0].kind != xdialog {
		t.Fatalf("X on a Task opened %d modals (status %q)", len(m.modals), statusOf(m))
	}
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "A Task has no tree or branch, so nothing is cleaned up.") {
		t.Errorf("no Task lead:\n%s", v)
	}
	if strings.Contains(v, "✎") || strings.Contains(v, "↑") {
		t.Errorf("Task row carries a tag:\n%s", v)
	}
}

func TestXTaskDialogYConfirmsDInert(t *testing.T) {
	m, calls := taskModel(t, taskPlan)
	m = press(t, m, runeKey("X"))
	m, _ = send(t, m, runeKey("D"))
	if len(m.modals) != 1 || len(*calls) != 1 {
		t.Fatalf("D acted: modals %d calls %v", len(m.modals), *calls)
	}
	m, cmd := send(t, m, runeKey("y"))
	m = settle(t, m, cmd)
	if len(m.modals) != 0 || !strings.Contains(strings.Join(*calls, "|"), "withdraw tk discard=false") {
		t.Errorf("modals %d calls %v", len(m.modals), *calls)
	}
}

func TestXLiveTaskOpensBarredWithSessionFix(t *testing.T) {
	m, _ := taskModel(t, func(string) (withdraw.Plan, error) {
		p, _ := taskPlan("")
		p.Refusals = []withdraw.Refusal{{Kind: withdraw.InFlight, Beads: []string{"tk"}, Fix: "finish or drop its session first (x in the TUI), then try again"}}
		return p, nil
	})
	m = press(t, m, runeKey("X"))
	v := ansi.Strip(m.View())
	for _, want := range []string{"Refused", "fix: finish or drop its session first"} {
		if !strings.Contains(v, want) {
			t.Errorf("barred Task view lacks %q:\n%s", want, v)
		}
	}
}

func TestXNonHeadStatusNamesTask(t *testing.T) {
	m, _ := actModel(t)
	stubWithdraw(t, cleanPlan, ok)
	m = press(t, selectBead(t, m, "res"), runeKey("X"))
	if !strings.Contains(statusOf(m), "Task") {
		t.Errorf("status %q", statusOf(m))
	}
}

// The prototype mc-sdkpu's four scenarios (variant B, ADR 0340) as Plans.
func sweepTask(id, title string) bd.Bead {
	return bd.Bead{ID: id, Kind: contract.KindTask, Status: "open", Title: title, Created: epoch}
}

func sweepBead(id string, k contract.Kind, title, parent string) bd.Bead {
	return bd.Bead{ID: id, Kind: k, Status: "open", Title: title, Parent: parent, Created: epoch}
}

func taskAlonePlan(string) (withdraw.Plan, error) {
	t1, t2, t3 := sweepTask("t1", "Rotate the Forge deploy token"), sweepTask("t2", "Put the new token in the CI secrets"), sweepTask("t3", "Revoke the old token on the Forge")
	return withdraw.Plan{Kind: withdraw.KindWithdraw, Target: "t1", Set: []bd.Bead{t2, t3, t1},
		Swept: []bd.Swept{{Bead: t2, After: "t1"}, {Bead: t3, After: "t2"}}}, nil
}

func releasePlan(string) (withdraw.Plan, error) {
	t4, t5 := sweepTask("t4", "Provision the staging Dolt server"), sweepTask("t5", "Point the backup job at staging")
	return withdraw.Plan{Kind: withdraw.KindWithdraw, Target: "t4", Set: []bd.Bead{t5, t4},
		Swept: []bd.Swept{{Bead: t5, After: "t4"}}, Releases: []string{"cp2"}}, nil
}

func chainSweepPlan(string) (withdraw.Plan, error) {
	cp := sweepBead("c1", contract.KindCheckpoint, "Rotate logs weekly", "")
	b1 := sweepBead("c1.1", contract.KindBuild, "Build: logrotate config", "c1")
	sp := sweepBead("s1", contract.KindSpec, "Spec: weekly log rotation", "c1")
	t6, t7, t8 := sweepTask("t6", "Enable logrotate on the homelab box"), sweepTask("t7", "Delete the old log-purge cron entry"), sweepTask("t8", "Free 40 GB on /var before the Spec lands")
	return withdraw.Plan{Kind: withdraw.KindWithdraw, Target: "c1", Set: []bd.Bead{t6, t7, t8, b1, sp, cp},
		Swept: []bd.Swept{{Bead: t6, After: "c1"}, {Bead: t7, After: "t6", Also: []string{"cron"}}, {Bead: t8, After: "s1"}}}, nil
}

func leftOpenPlan(string) (withdraw.Plan, error) {
	cp := sweepBead("p1", contract.KindCheckpoint, "Stage Dolt backups", "")
	b1 := sweepBead("p1.1", contract.KindBuild, "Build: backup target flag", "p1")
	t9, t4 := sweepTask("t9", "Verify a restore from the staging backup"), sweepTask("t4", "Provision the staging Dolt server")
	return withdraw.Plan{Kind: withdraw.KindWithdraw, Target: "p1", Set: []bd.Bead{t9, b1, cp},
		Swept: []bd.Swept{{Bead: t9, After: "p1"}}, LeftOpen: []bd.Held{{Bead: t4, Before: "p1", Also: []string{"cp2"}}}}, nil
}

// beforePlan: a chain head with a before-closed Task, its chained predecessor, a Task
// swept after the before-closed Task, and a left-open Task under the head.
// ts hangs after tb only to draw the nesting: SweepTasks never builds that plan, since
// a Task blocked by a before-closed Task is already in the set before tb before-closes.
func beforePlan(string) (withdraw.Plan, error) {
	cp := sweepBead("c1", contract.KindCheckpoint, "Rotate logs weekly", "")
	tb, tp, ts, tl := sweepTask("tb", "Prepare the log volume"), sweepTask("tp", "Order the disk"), sweepTask("ts", "Archive the old logs"), sweepTask("tl", "Audit the log retention")
	return withdraw.Plan{Kind: withdraw.KindWithdraw, Target: "c1", Set: []bd.Bead{ts, tp, tb, cp},
		Swept:    []bd.Swept{{Bead: ts, After: "tb"}},
		Before:   []bd.BeforeClosed{{Bead: tb, Before: "c1"}, {Bead: tp, Before: "tb"}},
		LeftOpen: []bd.Held{{Bead: tl, Before: "c1", Also: []string{"cron", "cp2"}}}}, nil
}

func sweepView(t *testing.T, w int, plan func(string) (withdraw.Plan, error)) string {
	t.Helper()
	m, _ := xOpen(t, w, 40, plan, ok)
	m.beads = append(m.beads, sweepBead("cron", contract.KindCheckpoint, "Move cron jobs to systemd timers", ""), sweepBead("cp2", contract.KindCheckpoint, "Stage Dolt backups", ""))
	return ansi.Strip(m.View())
}

func lineOf(t *testing.T, v, sub string) string {
	t.Helper()
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, sub) {
			return l
		}
	}
	t.Fatalf("no line with %q:\n%s", sub, v)
	return ""
}

func TestXDialogTaskAloneHangsAfterTasksOnDashedEdges(t *testing.T) {
	v := sweepView(t, 160, taskAlonePlan)
	for _, want := range []string{"┄▸ ", "Closes 3 beads as withdrawn: this Task and the 2 Tasks ordered after it.", "A Task has no tree or branch"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	l2, l3 := lineOf(t, v, "Put the new token"), lineOf(t, v, "Revoke the old token")
	if !strings.Contains(l2, "┄▸") || !strings.Contains(l3, "   ┄▸") || strings.Index(l3, "┄▸") <= strings.Index(l2, "┄▸") {
		t.Errorf("after-Tasks not nested on ┄▸:\n%s\n%s", l2, l3)
	}
	for _, l := range []string{l2, l3} {
		if strings.ContainsAny(l, "├└") {
			t.Errorf("an after-Task reads as parentage: %q", l)
		}
	}
}

func TestXDialogReleasesALandHold(t *testing.T) {
	for _, w := range []int{160, 80} {
		v := sweepView(t, w, releasePlan)
		head := lineOf(t, v, "Provision the staging Dolt server")
		if !strings.Contains(head, "⚑") || (w == 160) != strings.Contains(head, "releases a land hold") {
			t.Errorf("w=%d head row %q", w, head)
		}
		if !strings.Contains(v, "⚑ Stage Dolt backups can then land without this Task's steps.") {
			t.Errorf("w=%d no release line:\n%s", w, v)
		}
		if strings.Index(v, "can then land") > strings.Index(v, "y/enter") {
			t.Errorf("w=%d release line is not above the hints:\n%s", w, v)
		}
	}
	m, calls := xOpen(t, 160, 40, releasePlan, ok)
	m, _ = send(t, m, runeKey("D"))
	if len(m.modals) != 1 {
		t.Fatalf("D acted when nothing is lost")
	}
	m, cmd := send(t, m, runeKey("y"))
	settle(t, m, cmd)
	if !strings.Contains(strings.Join(*calls, "|"), "discard=false") {
		t.Errorf("y did not confirm: %v", *calls)
	}
}

func TestXDialogChainSweepWideAndNarrow(t *testing.T) {
	v := sweepView(t, 160, chainSweepPlan)
	for _, want := range []string{"Closes 6 beads as withdrawn: 3 in the chain and 3 Tasks ordered after it.", "⇠ also after Move cron jobs to"} {
		if !strings.Contains(v, want) {
			t.Errorf("wide view lacks %q:\n%s", want, v)
		}
	}
	// t8 follows the Spec, so it hangs after the Spec's subtree; t6 hangs from the head after its members.
	rows := strings.Split(v, "\n")
	idx := func(s string) int {
		for i, l := range rows {
			if strings.Contains(l, s) {
				return i
			}
		}
		return -1
	}
	if !(idx("Build: logrotate config") < idx("Spec: weekly") && idx("Spec: weekly") < idx("Free 40 GB") && idx("Free 40 GB") < idx("Enable logrotate")) {
		t.Errorf("sweep rows out of order:\n%s", v)
	}
	if !strings.Contains(lineOf(t, v, "Delete the old log-purge"), "┄▸") {
		t.Errorf("Task after a swept Task lacks ┄▸")
	}
	n := sweepView(t, 80, chainSweepPlan)
	if l := lineOf(t, n, "Delete the old log-purge"); !strings.Contains(l, "⇠+1") || strings.Contains(l, "also after") {
		t.Errorf("narrow tag: %q", l)
	}
}

func TestXDialogLeftOpenTaskHangsDimUnderItsCheckpoint(t *testing.T) {
	v := sweepView(t, 160, leftOpenPlan)
	l := lineOf(t, v, "Provision the staging Dolt server")
	if !strings.Contains(l, "┄◂") || !strings.Contains(l, "◌ left open: also before Stage Dolt backups") {
		t.Errorf("left-open row %q", l)
	}
	if !strings.Contains(v, "Nothing is discarded · 1 left open.") || !strings.Contains(v, "Closes 3 beads as withdrawn: 2 in the chain and 1 Task ordered after it.") {
		t.Errorf("lead or summary:\n%s", v)
	}
	if n := sweepView(t, 80, leftOpenPlan); !strings.Contains(lineOf(t, n, "Provision the staging Dolt server"), "◌+1") {
		t.Errorf("narrow left-open tag missing:\n%s", n)
	}
}

func TestXDialogSweptStragglerHangsOffTheHead(t *testing.T) {
	p, _ := taskAlonePlan("")
	p.Swept[1].After = "gone"
	rows := xPlanTree(p)
	if len(rows) != 3 || !strings.HasPrefix(rows[2].prefix, "┄▸") {
		t.Errorf("rows %+v", rows)
	}
}

func TestXDialogBeforeClosedTaskHangsUndimmedUnderItsBeforeBead(t *testing.T) {
	p, _ := beforePlan("")
	rows := xPlanTree(p)
	var got []string
	for _, r := range rows {
		got = append(got, r.prefix+r.b.ID)
	}
	want := []string{"c1", "┄◂ tb", "   ┄▸ ts", "   ┄◂ tp", "┄◂ tl"}
	if !slices.Equal(got, want) {
		t.Errorf("rows %q, want %q", got, want)
	}
	v := sweepView(t, 160, beforePlan)
	l := lineOf(t, v, "Prepare the log volume")
	if !strings.Contains(l, "┄◂") || strings.ContainsAny(l, "◌⇠") || strings.Contains(l, "swept") {
		t.Errorf("before-closed row %q", l)
	}
	if !strings.Contains(lineOf(t, v, "Order the disk"), "   ┄◂") || !strings.Contains(lineOf(t, v, "Archive the old logs"), "   ┄▸") {
		t.Errorf("nesting:\n%s", v)
	}
}

func TestXDialogBeforeClosedIsUndimmedAndLeftOpenDim(t *testing.T) {
	forceANSI256(t)
	m, _ := xOpen(t, 160, 40, beforePlan, ok)
	dim := strings.TrimSuffix(dimSt.Render("x"), "x\x1b[0m")
	line := func(sub string) string {
		for _, l := range strings.Split(m.View(), "\n") {
			if strings.Contains(ansi.Strip(l), sub) {
				return l
			}
		}
		t.Fatalf("no line %q", sub)
		return ""
	}
	if l := line("Prepare the log volume"); strings.Contains(l, dim) {
		t.Errorf("before-closed row is dim: %q", l)
	}
	if l := line("Audit the log retention"); !strings.Contains(l, dim) {
		t.Errorf("left-open row is not dim: %q", l)
	}
}

func TestXDialogLeftOpenTagNamesAlsoBeforeWideAndCountNarrow(t *testing.T) {
	v := sweepView(t, 160, beforePlan)
	if l := lineOf(t, v, "Audit the log retention"); !strings.Contains(l, "◌ left open: also before Move cron jobs to") {
		t.Errorf("wide tag %q", l)
	}
	n := sweepView(t, 80, beforePlan)
	if l := lineOf(t, n, "Audit the log retention"); !strings.Contains(l, "◌+2") || strings.Contains(l, "also before") {
		t.Errorf("narrow tag %q", l)
	}
	if !strings.Contains(v, "Nothing is discarded · 1 left open.") {
		t.Errorf("summary counts more than left-open Tasks:\n%s", v)
	}
}

func TestXDialogLeadNamesEachTaskGroup(t *testing.T) {
	cp := sweepBead("c1", contract.KindCheckpoint, "C", "")
	b := sweepBead("b1", contract.KindBuild, "B", "c1")
	t1, t2, t3 := sweepTask("t1", "A"), sweepTask("t2", "B2"), sweepTask("t3", "C3")
	for _, c := range []struct {
		p    withdraw.Plan
		want string
	}{
		{withdraw.Plan{Set: []bd.Bead{t1, b, cp}, Swept: []bd.Swept{{Bead: t1, After: "c1"}}}, "Closes 3 beads as withdrawn: 2 in the chain and 1 Task ordered after it."},
		{withdraw.Plan{Set: []bd.Bead{t1, t2, b, cp}, Before: []bd.BeforeClosed{{Bead: t1, Before: "c1"}, {Bead: t2, Before: "c1"}}}, "Closes 4 beads as withdrawn: 2 in the chain and 2 Tasks ordered before it."},
		{withdraw.Plan{Set: []bd.Bead{t1, t2, t3, b, cp}, Swept: []bd.Swept{{Bead: t1, After: "c1"}, {Bead: t2, After: "c1"}}, Before: []bd.BeforeClosed{{Bead: t3, Before: "c1"}}}, "Closes 5 beads as withdrawn: 2 in the chain, 2 Tasks ordered after it and 1 ordered before it."},
	} {
		if got := xLead(c.p); !strings.HasPrefix(got, c.want) {
			t.Errorf("lead %q, want prefix %q", got, c.want)
		}
	}
}
