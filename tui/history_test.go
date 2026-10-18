package tui

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/session"
)

func histBeads() []bd.Bead {
	return []bd.Bead{
		{ID: "cp-a", Kind: contract.KindCheckpoint, Container: true, Status: "closed", Title: "Alpha"},
		{ID: "cp-b", Kind: contract.KindCheckpoint, Container: true, Status: "open", Title: "Beta"},
		{ID: "ch-a", Kind: contract.KindChart, Container: true, Status: "closed", Title: "Gamma"},
		{ID: "sp-a", Kind: contract.KindSpec, Status: "closed", Title: "Delta"},
	}
}

func histCommits() []git.MainlineCommit {
	now := time.Now()
	return []git.MainlineCommit{
		{SHA: "0000001aaaa", Time: now, Subject: "leading"},
		{SHA: "0000002aaaa", Time: now.Add(-time.Hour), Reverts: "cp-a"},
		{SHA: "0000003aaaa", Time: now, Subject: "fix typo"},
		{SHA: "0000004aaaa", Time: now, Kind: "chart", Lands: "ch-a"},
		{SHA: "0000005aaaa", Time: now, Kind: "spec", Lands: "sp-a"},
		{SHA: "0000006aaaa", Time: now.Add(-48 * time.Hour), Kind: "checkpoint", Lands: "cp-b"},
		{SHA: "0000007aaaa", Time: now, Kind: "checkpoint", Lands: "cp-gone"},
		{SHA: "0000008aaaa", Time: now, Kind: "checkpoint", Lands: "cp-a"},
	}
}

func histLoad(commits []git.MainlineCommit, beads []bd.Bead) loadedMsg {
	return loadedMsg{slowTier: true, beads: beads, live: map[string]bool{}, sessions: map[string]session.Info{}, commits: commits}
}

// historyModel is a Model on the History tab, on the revert of cp-a.
func historyModel(t *testing.T) Model {
	t.Helper()
	m := sized(t, 100, 24)
	m, _ = send(t, m, histLoad(histCommits(), histBeads()))
	m, _ = send(t, m, runeKey("6"))
	return press(t, m, runeKey("j"))
}

// goSha moves the cursor down to the row with sha prefix.
func goSha(t *testing.T, m Model, sha string) Model {
	t.Helper()
	for range 20 {
		if strings.HasPrefix(histSel(m), sha) {
			return m
		}
		m = press(t, m, runeKey("j"))
	}
	t.Fatalf("no row %s", sha)
	return m
}

func histSel(m Model) string { return m.tabAt(tabHistory).(histTab).sel }

func TestHistoryFoldsAndBadges(t *testing.T) {
	m := historyModel(t)
	m.beads = append(m.beads, bd.Bead{ID: "rb", Kind: contract.KindRollback, Status: "open", Related: []string{"cp-b"}})
	v := body(m)
	for _, want := range []string{"0000001 · 0m · leading", "0000003 · 0m · fix typo", "↺ 0000002 · 1h · revert of Alpha (cp-a)",
		"0000004 · 0m · Gamma (ch-a)  CHART", "0000005 · 0m · Delta (sp-a) SPEC",
		"0000006 · 2d · Beta (cp-b) rollback pending  CHECKPOINT", "0000008 · 0m · Alpha (cp-a) rolled back  CHECKPOINT", "cp-gone  CHECKPOINT"} {
		if !strings.Contains(v, want) {
			t.Errorf("History lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "hidden") {
		t.Errorf("History still folds:\n%s", v)
	}
	if histSel(historyModel(t)) != "0000002aaaa" {
		t.Errorf("fixture should sit on the revert")
	}
	fresh := sized(t, 100, 24)
	fresh, _ = send(t, fresh, histLoad(histCommits(), histBeads()))
	if histSel(press(t, fresh, runeKey("6"))) != "0000001aaaa" {
		t.Errorf("first selection is not the top row")
	}
}

func TestHistoryMoveVisitsPlainRowsAndKeepsShaOnRefresh(t *testing.T) {
	m := press(t, historyModel(t), runeKey("j"))
	if histSel(m) != "0000003aaaa" {
		t.Fatalf("j sel = %q, want the plain row", histSel(m))
	}
	if got := histSel(press(t, press(t, historyModel(t), runeKey("k")), runeKey("k"))); got != "0000008aaaa" {
		t.Errorf("k wraps to %q", got)
	}
	m = goSha(t, m, "0000006")
	newer := append([]git.MainlineCommit{{SHA: "000000faaaa", Kind: "checkpoint", Lands: "cp-new"}}, histCommits()...)
	m, _ = send(t, m, histLoad(newer, histBeads()))
	if histSel(m) != "0000006aaaa" {
		t.Errorf("refresh moved sel to %q", histSel(m))
	}
	m, _ = send(t, m, histLoad(newer[:3], histBeads()))
	if histSel(m) != "000000faaaa" {
		t.Errorf("gone commit: sel = %q, want the top row", histSel(m))
	}
}

func TestHistoryGotoEdgeLandsOnPlainRows(t *testing.T) {
	m := historyModel(t)
	if got := histSel(gotoKey(t, m, "g")); got != "0000001aaaa" {
		t.Errorf("g g = %q, want the top plain row", got)
	}
	plainLast := append(histCommits(), git.MainlineCommit{SHA: "0000009aaaa", Subject: "oldest"})
	m, _ = send(t, m, histLoad(plainLast, histBeads()))
	if got := histSel(gotoKey(t, m, "G")); got != "0000009aaaa" {
		t.Errorf("g G = %q, want the bottom plain row", got)
	}
}

func TestHistoryRevealAndScope(t *testing.T) {
	m := goSha(t, historyModel(t), "0000006") // cp-b
	for name, g := range map[string]Model{"enter": press(t, m, tea.KeyMsg{Type: tea.KeyEnter}), "g r": gotoKey(t, m, "r")} {
		if g.tab != tabGraph || sel(g) != "cp-b" {
			t.Errorf("%s: tab=%d graph sel=%q, want Graph on cp-b", name, g.tab, sel(g))
		}
		if histSel(g) != "0000006aaaa" {
			t.Errorf("reveal moved History's selection to %q", histSel(g))
		}
	}
	l := gotoKey(t, m, "l")
	if lt := logs(l); l.tab != tabLogs || lt.scope != "cp-b" || !lt.kids {
		t.Errorf("g l: tab=%d scope=%q kids=%v", l.tab, lt.scope, lt.kids)
	}
	for sha, id := range map[string]string{"0000004": "ch-a", "0000005": "sp-a"} {
		if g := press(t, goSha(t, m, sha), tea.KeyMsg{Type: tea.KeyEnter}); g.tab != tabGraph || sel(g) != id {
			t.Errorf("enter on %s: tab=%d graph sel=%q, want Graph on %s", sha, g.tab, sel(g), id)
		}
	}
	plain := goSha(t, historyModel(t), "0000003")
	if g, cmd := send(t, plain, tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil || g.tab != tabHistory {
		t.Errorf("enter on a plain row did something: tab=%d", g.tab)
	}
	for _, k := range []string{"G", "L"} {
		if n := press(t, m, runeKey(k)); n.tab != tabHistory {
			t.Errorf("%s left History", k)
		}
	}
}

func TestHistoryRefusalsOpenNothing(t *testing.T) {
	filed := 0
	seam(t, &newRollback, func(string, string, bd.Bead) (string, error) { filed++; return "x", nil })
	m := historyModel(t)
	check := func(name, want string) {
		t.Helper()
		r := press(t, m, runeKey("u"))
		if len(r.modals) != 0 || r.status != want {
			t.Errorf("%s: modals=%d status=%q, want %q", name, len(r.modals), r.status, want)
		}
	}
	check("revert", "a revert can't be rolled back")
	m.beads = append(m.beads, bd.Bead{ID: "rb", Kind: contract.KindRollback, Status: "open", Related: []string{"cp-b"}})
	m = goSha(t, m, "0000003")
	check("plain", "not a land")
	m = press(t, m, runeKey("j"))
	check("chart", "a Chart land can't be rolled back")
	m = press(t, m, runeKey("j"))
	check("spec", "a Spec anchor can't be rolled back")
	m = press(t, m, runeKey("j"))
	check("pending", "cp-b: rollback pending")
	m = goSha(t, m, "0000008") // cp-a, reverted
	check("badged", "cp-a: already rolled back")
	m.beads[len(m.beads)-1] = bd.Bead{ID: "rb", Kind: contract.KindRollback, Status: "closed", CloseReason: bd.FixForwardReason + " x", Related: []string{"cp-b"}}
	m = press(t, press(t, m, runeKey("k")), runeKey("k"))
	check("fixed forward", "cp-b: already fixed forward")
	empty := press(t, sized(t, 100, 24), runeKey("6"))
	if r := press(t, empty, runeKey("u")); len(r.modals) != 0 || r.status != "nothing to roll back" {
		t.Errorf("empty: modals=%d status=%q", len(r.modals), r.status)
	}
	if filed != 0 {
		t.Error("a refusal filed a Rollback")
	}
}

func TestHistoryRollbackFormFilesOnSubmitOnly(t *testing.T) {
	var got []string
	seam(t, &newRollback, func(title, body string, target bd.Bead) (string, error) {
		got = append(got, title, body, target.ID)
		return "rb-new", nil
	})
	m := goSha(t, historyModel(t), "0000006") // cp-b, unbadged
	m = press(t, m, runeKey("u"))
	if len(m.modals) != 1 || m.modals[0].inputs[0].text != "Roll back Beta" || !strings.Contains(body(m), "Rollback of cp-b") {
		t.Fatalf("u did not open the prefilled form:\n%s", body(m))
	}
	if got != nil {
		t.Fatal("opening filed")
	}
	if c := press(t, m, tea.KeyMsg{Type: tea.KeyEsc}); len(c.modals) != 0 || got != nil {
		t.Errorf("esc: modals=%d filed=%v", len(c.modals), got)
	}
	blank := m
	blank.modals = slices.Clone(m.modals)
	blank.modals[0].inputs = []line{lineAt("  "), {}}
	if b := press(t, blank, tea.KeyMsg{Type: tea.KeyEnter}); len(b.modals) != 1 || got != nil {
		t.Errorf("blank title: modals=%d filed=%v, want the form kept", len(b.modals), got)
	}
	m = typeText(t, press(t, m, tea.KeyMsg{Type: tea.KeyTab}), "x")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if want := []string{"Roll back Beta", "x", "cp-b"}; !slices.Equal(got, want) || len(m.modals) != 0 {
		t.Errorf("submit: filed %v modals=%d, want %v", got, len(m.modals), want)
	}
	if m.status != "filed Rollback rb-new of cp-b" {
		t.Errorf("status = %q", m.status)
	}
}

func TestHistoryRollbackSubmitRechecksGate(t *testing.T) {
	filed := 0
	seam(t, &newRollback, func(string, string, bd.Bead) (string, error) { filed++; return "x", nil })
	m := press(t, goSha(t, historyModel(t), "0000006"), runeKey("u"))
	m.beads = append(m.beads, bd.Bead{ID: "rb", Kind: contract.KindRollback, Status: "open", Related: []string{"cp-b"}})
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if filed != 0 || m.status != "cp-b: rollback pending" {
		t.Errorf("filed=%d status=%q, want refused", filed, m.status)
	}
}

func TestHistoryHintsAndBindingCoverage(t *testing.T) {
	bar := func(m Model) string { return hints(m) }
	m := goSha(t, historyModel(t), "0000006") // rollbackable
	b := bar(m)
	for _, want := range []string{"g goto", "enter reveal", "u undo"} {
		if !strings.Contains(b, want) {
			t.Errorf("hints lack %q: %q", want, b)
		}
	}
	for _, bad := range []string{"detail", "fold", "search"} {
		if strings.Contains(b, bad) {
			t.Errorf("hints list %q: %q", bad, b)
		}
	}
	if r := press(t, m, runeKey("r")); len(r.modals) != 0 || r.status != "" {
		t.Errorf("r on History: modals=%d status=%q, want nothing", len(r.modals), r.status)
	}
	checkCoverage(t, "History rollbackable", m, true, "6")
	rev := historyModel(t)
	if b := bar(rev); strings.Contains(b, "undo") || !strings.Contains(b, "reveal") {
		t.Errorf("unrollbackable hints = %q", b)
	}
	checkCoverage(t, "History revert", rev, true, "6", "u")
	plain := goSha(t, historyModel(t), "0000003")
	if b := bar(plain); strings.Contains(b, "undo") || strings.Contains(b, "reveal") {
		t.Errorf("plain-row hints = %q", b)
	}
	checkCoverage(t, "History plain", plain, true, "6", "u")
	for _, k := range []tea.KeyMsg{runeKey("d"), runeKey("/"), {Type: tea.KeySpace}} {
		if next, cmd := send(t, m, k); cmd != nil || next.View() != m.View() {
			t.Errorf("%q did something on History", k)
		}
	}
}

func TestHistoryRendersNoSubprocess(t *testing.T) {
	logf := fakeTools(t)
	m := goSha(t, historyModel(t), "0000006")
	m.View()
	open := press(t, m, runeKey("u"))
	if len(open.modals) == 0 {
		t.Fatal("u did not open")
	}
	open.View()
	if got, _ := os.ReadFile(logf); len(got) > 0 {
		t.Fatalf("History rendering started subprocesses:\n%s", got)
	}
}

func TestHistoryFrameFits(t *testing.T) {
	for _, sz := range [][2]int{{40, 12}, {60, 20}, {200, 50}} {
		m := sized(t, sz[0], sz[1])
		m, _ = send(t, m, histLoad(histCommits(), histBeads()))
		m, _ = send(t, m, runeKey("6"))
		if n := len(lines(m.View())); n != sz[1] {
			t.Errorf("%v: %d lines", sz, n)
		}
	}
}

// FR47: a nested row carries its context in a column (blank for Workspace rows), and
// r on it files the Rollback of the nested Checkpoint.
func TestHistoryNestedRowShowsContextAndRollsBack(t *testing.T) {
	now := time.Now()
	beads := append(histBeads(), bd.Bead{ID: "cp-n", Kind: contract.KindCheckpoint, Container: true, Status: "closed", Title: "Nested"})
	commits := []git.MainlineCommit{
		{SHA: "0000001aaaa", Time: now, Kind: "checkpoint", Lands: "cp-a"},
		{SHA: "0000002aaaa", Time: now.Add(-time.Hour), Kind: "checkpoint", Lands: "cp-n", Context: "kid"},
		{SHA: "0000003aaaa", Time: now.Add(-2 * time.Hour), Kind: "chart", Lands: "ch-n", Context: "kid"},
	}
	beads = append(beads, bd.Bead{ID: "ch-n", Kind: contract.KindChart, Container: true, Status: "closed", Title: "NestedChart"})
	m := sized(t, 100, 24)
	m, _ = send(t, m, histLoad(commits, beads))
	m, _ = send(t, m, runeKey("6"))
	if v := body(m); !strings.Contains(v, "· kid · ") {
		t.Errorf("view lacks the context column:\n%s", v)
	}
	if v := body(m); !strings.Contains(v, "kid · 2h · NestedChart (ch-n)  CHART") {
		t.Errorf("nested Chart row missing:\n%s", v)
	}
	m = press(t, press(t, m, runeKey("j")), runeKey("u"))
	if len(m.modals) != 1 || !strings.Contains(body(m), "Rollback of cp-n") {
		t.Errorf("u on the nested row did not aim at cp-n:\n%s", body(m))
	}
}

func graphLoad(beads ...bd.Bead) loadedMsg {
	return loadedMsg{slowTier: true, beads: beads, live: map[string]bool{}, sessions: map[string]session.Info{}}
}

// A load off Graph leaves its derived state alone; entering Graph shows what a rebuild on every load would.
func TestGraphRebuildsOnlyWhileActive(t *testing.T) {
	a := bd.Bead{ID: "a", Title: "Alpha", Kind: contract.KindSignal, Status: "open"}
	b := bd.Bead{ID: "b", Title: "Beta", Kind: contract.KindSignal, Status: "open"}
	ref := sized(t, 100, 24)
	ref, _ = send(t, ref, runeKey("4"))
	ref, _ = send(t, ref, graphLoad(a))
	ref, _ = send(t, ref, graphLoad(a, b))

	m := sized(t, 100, 24)
	m, _ = send(t, m, graphLoad(a))
	m, _ = send(t, m, runeKey("4"))
	m, _ = send(t, m, runeKey("6"))
	m, _ = send(t, m, graphLoad(a, b))
	if n := m.tabs[tabGraph].(graphTab).total; n != 1 {
		t.Errorf("a load off Graph rebuilt it: total %d", n)
	}
	m, _ = send(t, m, runeKey("4"))
	if got, want := body(m), body(ref); got != want || m.tabs[tabGraph].(graphTab).total != 2 {
		t.Errorf("entering Graph differs from an always-rebuilt Graph:\n%s\nwant:\n%s", got, want)
	}
	m, _ = send(t, m, graphLoad(a))
	if m.tabs[tabGraph].(graphTab).total != 1 {
		t.Error("a load on Graph did not rebuild it")
	}
}

// A selected land row keeps its kind badge's colour under the highlight, drops the
// dimmed head's grey, and an unselected land row's badge renders as before.
func TestHistorySelectedLandKeepsBadgeColour(t *testing.T) {
	forceANSI256(t)
	m := goSha(t, historyModel(t), "0000004")
	lines := strings.Split(m.tabAt(tabHistory).(histTab).view(100, 24), "\n")
	var sel, plain string
	for _, l := range lines {
		switch {
		case strings.Contains(l, "CHART") && strings.Contains(l, "❯"):
			sel = l
		case strings.Contains(l, "DELTA") || strings.Contains(l, "SPEC"):
			plain = l
		}
	}
	if sel == "" {
		t.Fatalf("no selected chart row:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(sel, "48;5;"+string(kindColors[contract.KindChart])+"mCHART") {
		t.Errorf("selected row lacks the chip colour:\n%q", sel)
	}
	if !strings.Contains(sel, strings.TrimSuffix(selRow.Render(""), "\x1b[0m")) {
		t.Errorf("selected row lacks the selection background:\n%q", sel)
	}
	if grey := strings.TrimSuffix(strings.TrimPrefix(dimSt.Render("x"), ""), "x\x1b[0m"); strings.Contains(sel, grey) {
		t.Errorf("selected row carries the dimmed head grey %q:\n%q", grey, sel)
	}
	if plain != "" && !strings.Contains(plain, "\x1b[") {
		t.Errorf("unselected row lost its styling:\n%q", plain)
	}
	if w := lipgloss.Width(sel); w != 100 {
		t.Errorf("selected row width %d, want 100", w)
	}
}
