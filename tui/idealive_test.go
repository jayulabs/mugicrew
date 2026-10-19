package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/session"
)

// ideaLiveFixture: a and w run detached triage sessions (w waiting on a turn), x's
// session exited; all in Initiative ini.
func ideaLiveFixture() ([]bd.Bead, map[string]session.Info) {
	return []bd.Bead{ini("ini", "open", "Ship", 1), sig("a", "in_progress", 1, "ini"), sig("w", "in_progress", 2, "ini"),
			sig("x", "in_progress", 2, "ini"), sig("r", "open", 2, "ini")},
		map[string]session.Info{"a": {State: session.Alive}, "w": {State: session.Alive, Waiting: session.WaitTurn}, "x": {State: session.Exited}}
}

func ideaLiveKeys(rows []ideaRow) string {
	var ks []string
	for _, r := range rows {
		if r.live {
			ks = append(ks, r.key)
		}
	}
	return strings.Join(ks, ",")
}

func TestIdeateLiveSection(t *testing.T) {
	beads, sess := ideaLiveFixture()
	m := ideaModel(t, 100, 30, beads, sess)
	rows := m.tabAt(tabIdeate).(ideaTab).rows()
	var hdr []string
	for _, r := range rows {
		if r.sec {
			hdr = append(hdr, r.title)
		}
	}
	// directly below Actionable (after its rows): a listed (w waits, so Actionable only); exited x absent
	if strings.Join(hdr, "|") != "Actionable (2)|Live (1)|Initiatives (1)||" || ideaLiveKeys(rows) != "live:a" {
		t.Errorf("headers %v live %s", hdr, ideaLiveKeys(rows))
	}
	if got := m.View(); !strings.Contains(ansi.Strip(got), "Live (1)") {
		t.Errorf("view lacks header:\n%s", ansi.Strip(got))
	}
	// folded: rows hide, N stays
	t2 := m.tabAt(tabIdeate).(ideaTab)
	t2.flip = map[string]bool{liveKey: true}
	rows = t2.rows()
	if ideaLiveKeys(rows) != "" || rows[slices.IndexFunc(rows, func(r ideaRow) bool { return r.key == liveKey })].title != "Live (1)" {
		t.Errorf("folded rows %v", shape(rows))
	}
	// empty: placeholder
	m = ideaModel(t, 100, 30, []bd.Bead{sig("r", "open", 2, "")}, nil)
	rows = m.tabAt(tabIdeate).(ideaTab).rows()
	if i := slices.IndexFunc(rows, func(r ideaRow) bool { return r.key == liveKey+"/none" }); i < 0 || rows[i-1].title != "Live (0)" {
		t.Errorf("no placeholder")
	}
}

func TestIdeateLiveFoldPersists(t *testing.T) {
	dir := t.TempDir()
	stubCommonDir(t, dir)
	beads, sess := ideaLiveFixture()
	m := ideaModelFresh(t, 100, 30, beads, sess)
	m = press(t, ideaAt(t, m, liveKey), runeKey(" "))
	if got := readSection(t, dir, "ideate"); got != `{"s:live":true}` {
		t.Errorf("folds = %s", got)
	}
	tab := m.tabAt(tabIdeate).(ideaTab)
	tab.flip["gone"] = true
	if err := tab.save(); err != nil {
		t.Fatal(err)
	}
	if got := readSection(t, dir, "ideate"); got != `{"s:live":true}` {
		t.Errorf("pruned = %s", got)
	}
}

func TestIdeateLiveShadeFallbackKeys(t *testing.T) {
	beads, sess := ideaLiveFixture()
	m := ideaModel(t, 100, 30, beads, sess)
	shaded := func(m Model, k string) bool {
		tab := m.tabAt(tabIdeate).(ideaTab)
		rows := tab.rows()
		return tab.shaded(tab.frame(), tab.selIndex(rows))[slices.IndexFunc(rows, func(r ideaRow) bool { return r.key == k })]
	}
	m = ideaAt(t, m, "live:a")
	if !shaded(m, "a") || !shaded(m, "i:ini") {
		t.Errorf("live:a should shade its tree twin and group header")
	}
	m = ideaAt(t, m, "a")
	if !shaded(m, "live:a") {
		t.Errorf("tree a should shade its Live twin")
	}
	if hintsOf(ideaAt(t, m, "live:a")) != hintsOf(ideaAt(t, m, "a")) {
		t.Errorf("hints differ on Live row")
	}
	// the Live row's bead goes non-live: tree twin
	m = ideaAt(t, m, "live:a")
	sess["a"] = session.Info{State: session.Exited}
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: sess})
	if got := cursorKey(m); got != "a" {
		t.Errorf("fell back to %q", got)
	}
}

// i on a Live Initiative row focuses the Initiative, as on its tree header.
func TestIdeateLiveInitiativeRowFocuses(t *testing.T) {
	beads, _ := ideaLiveFixture()
	m := ideaModel(t, 100, 30, beads, map[string]session.Info{"ini": {State: session.Alive}})
	m = gotoFocus(t, ideaAt(t, m, "live:ini"))
	if !focused(m) {
		t.Errorf("g f on the Live Initiative row did not focus:\n%s", ansi.Strip(m.View()))
	}
}

func TestIdeateLiveRowKeepsBacklogHint(t *testing.T) {
	beads := []bd.Bead{ini("ini", "open", "Ship", 1), sig("b", "open", 3, "ini")}
	m := ideaModel(t, 100, 30, beads, map[string]session.Info{"b": {State: session.Alive}})
	rows := m.tabAt(tabIdeate).(ideaTab).rows()
	i := slices.IndexFunc(rows, func(r ideaRow) bool { return r.key == "live:b" })
	if i < 0 || rows[i].hint != "Ship" {
		t.Errorf("live backlog row hint: rows %v", shape(rows))
	}
}

func TestIdeateInitiativesHeader(t *testing.T) {
	dir := t.TempDir()
	stubCommonDir(t, dir)
	beads, sess := ideaLiveFixture()
	m := ideaModelFresh(t, 100, 30, beads, sess)
	rows := m.tabAt(tabIdeate).(ideaTab).rows()
	hi := slices.IndexFunc(rows, func(r ideaRow) bool { return r.key == treeKey })
	if hi < 0 || rows[hi].title != "Initiatives (1)" || !rows[hi].sec || !rows[hi+1].header() || rows[hi-1].key == "" && !rows[hi-1].act {
		t.Fatalf("header misplaced: %v", shape(rows))
	}
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	li := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "Initiatives") && strings.Contains(l, "▾") })
	if li < 2 || strings.TrimSpace(lines[li-1]) != "" || strings.TrimSpace(lines[li-2]) == "" {
		t.Fatalf("no blank line above the header:\n%s", strings.Join(lines, "\n"))
	}
	// folding drops every tree row (Backlog too), Live (N) is unchanged, and it persists
	m = ideaModel(t, 100, 30, append(beads, sig("bk", "open", 3, "")), sess)
	live := func(m Model) string {
		for _, r := range m.tabAt(tabIdeate).(ideaTab).rows() {
			if strings.HasPrefix(r.title, "Live") {
				return r.title
			}
		}
		return ""
	}
	before := live(m)
	m = press(t, ideaAt(t, m, treeKey), runeKey(" "))
	for _, r := range m.tabAt(tabIdeate).(ideaTab).rows() {
		if r.header() && !r.sec || r.key == "bk" || r.key == "r" {
			t.Fatalf("tree row %s survives the fold", r.key)
		}
	}
	if live(m) != before || before != "Live (1)" {
		t.Errorf("Live %q -> %q", before, live(m))
	}
	if got := readSection(t, dir, "ideate"); got != `{"i:ini":true,"s:tree":true}` {
		t.Errorf("folds = %s", got)
	}
	// survives pruning
	tab := m.tabAt(tabIdeate).(ideaTab)
	tab.flip["gone"] = true
	if err := tab.save(); err != nil {
		t.Fatal(err)
	}
	if got := readSection(t, dir, "ideate"); got != `{"i:ini":true,"s:tree":true}` {
		t.Errorf("pruned = %s", got)
	}
	// the cursor on a folded-away row falls back to the header; bead keys have no bead
	tab.sel = "r"
	if c := tab.cursor(); c.key != treeKey || tab.selected() != nil {
		t.Errorf("cursor %q", c.key)
	}
	m = press(t, m, runeKey(" ")) // unfold
	if rows := m.tabAt(tabIdeate).(ideaTab).rows(); !slices.ContainsFunc(rows, func(r ideaRow) bool { return r.key == "r" }) {
		t.Errorf("unfold lost the tree")
	}
}

// the cursor follows a Signal between Actionable and Live, and falls back to the tree twin
// when the destination section is folded.
func TestIdeateCursorFollowsActionableLive(t *testing.T) {
	refresh := func(m Model, id string, s session.Info) Model {
		beads, sess := ideaLiveFixture()
		sess[id] = s
		m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: sess})
		return m
	}
	beads, sess := ideaLiveFixture()
	m := ideaAt(t, ideaModel(t, 100, 30, beads, sess), "live:a")
	m = refresh(m, "a", session.Info{State: session.Alive, Waiting: session.WaitTurn})
	if got := cursorKey(m); got != "act:a" {
		t.Fatalf("Live to Actionable: cursor on %q", got)
	}
	if m = refresh(m, "a", session.Info{State: session.Alive, Waiting: session.WaitTurn}); cursorKey(m) != "act:a" {
		t.Errorf("second refresh: cursor on %q", cursorKey(m))
	}
	m = ideaAt(t, m, "act:a")
	m = refresh(m, "a", session.Info{State: session.Alive})
	if got := cursorKey(m); got != "live:a" {
		t.Errorf("Actionable to Live: cursor on %q", got)
	}
	// folded destination: the tree twin
	m = press(t, ideaAt(t, ideaModel(t, 100, 30, beads, sess), liveKey), runeKey(" "))
	m = ideaAt(t, m, "act:w")
	if m = refresh(m, "w", session.Info{State: session.Alive}); cursorKey(m) != "w" {
		t.Errorf("folded Live: cursor on %q, want w", cursorKey(m))
	}
}

// A loose Signal's Actionable row shades the No Initiative header, a Backlog Signal's Live
// row the Backlog header, folded or not; a plain header selection shades nothing.
func TestIdeateShadeLightsLooseAndBacklogHeaders(t *testing.T) {
	beads := []bd.Bead{sig("l", "open", 1, ""), sig("b", "open", 3, ""), sig("q", "open", 3, "")}
	sess := map[string]session.Info{"b": {State: session.Alive}}
	for _, open := range []bool{true, false} {
		m := ideaModelFresh(t, 100, 30, beads, sess)
		if open {
			m = treesOpen(m)
			ide := m.tabs[tabIdeate].(ideaTab)
			ide.flip["i:"], ide.flip[backlogKey] = true, true
			m.tabs[tabIdeate] = ide
		}
		shaded := func(sel, k string) bool {
			m := ideaAt(t, m, sel)
			tab := m.tabAt(tabIdeate).(ideaTab)
			rows := tab.rows()
			return tab.shaded(tab.frame(), tab.selIndex(rows))[slices.IndexFunc(rows, func(r ideaRow) bool { return r.key == k })]
		}
		if !shaded("act:l", "i:") {
			t.Errorf("open=%v: act:l did not shade No Initiative", open)
		}
		if !shaded("live:b", backlogKey) {
			t.Errorf("open=%v: live:b did not shade Backlog", open)
		}
		if !open {
			continue
		}
		if !shaded("l", "i:") || !shaded("l", "act:l") || !shaded("q", backlogKey) {
			t.Errorf("tree row should shade its header and Actionable twin")
		}
		for _, h := range []string{"i:", backlogKey} {
			if shaded(h, "act:l") || shaded(h, "q") {
				t.Errorf("header %q selection shaded its members", h)
			}
		}
	}
}

// A member nested in a parked Initiative shades its group header and the Backlog header.
func TestIdeateShadeNestedBacklogMember(t *testing.T) {
	m := ideaModelFresh(t, 100, 30, []bd.Bead{ini("p", "open", "Park", 3), sig("m1", "open", 1, "p")}, nil)
	m = treesOpen(m)
	ide := m.tabs[tabIdeate].(ideaTab)
	ide.flip[backlogKey], ide.flip["i:p"] = true, true
	m.tabs[tabIdeate] = ide
	shaded := func(sel, k string) bool {
		m := ideaAt(t, m, sel)
		tab := m.tabAt(tabIdeate).(ideaTab)
		rows := tab.rows()
		i := slices.IndexFunc(rows, func(r ideaRow) bool { return r.key == k })
		if i < 0 {
			t.Fatalf("no row %q: %v", k, shape(rows))
		}
		return tab.shaded(tab.frame(), tab.selIndex(rows))[i]
	}
	if !shaded("m1", "i:p") || !shaded("m1", backlogKey) {
		t.Errorf("nested member should shade its group and the Backlog header")
	}
	if shaded(backlogKey, "m1") || shaded(backlogKey, "i:p") {
		t.Errorf("selected Backlog header shaded its members")
	}
}

// A working P3 cut session lists in Live with no hint; its header twin is in Initiatives.
func TestIdeateLiveParkedInitiativeNoHint(t *testing.T) {
	beads := []bd.Bead{ini("pi", "open", "Parked", 3), sig("m", "open", 1, "pi")}
	m := ideaModel(t, 100, 30, beads, map[string]session.Info{"pi": {State: session.Alive}})
	rows := m.tabAt(tabIdeate).(ideaTab).rows()
	i := slices.IndexFunc(rows, func(r ideaRow) bool { return r.key == "live:pi" })
	if i < 0 || rows[i].hint != "" {
		t.Fatalf("live row: %v", shape(rows))
	}
	ti, hi := slices.IndexFunc(rows, func(r ideaRow) bool { return r.key == treeKey }), slices.IndexFunc(rows, func(r ideaRow) bool { return r.key == "i:pi" })
	bi := slices.IndexFunc(rows, func(r ideaRow) bool { return r.key == backlogKey })
	if ti < 0 || hi < ti || bi >= 0 && hi > bi {
		t.Errorf("header twin not in Initiatives: %v", shape(rows))
	}
	// a Live P3 Signal of a P0–P2 Initiative keeps its hint
	m = ideaModel(t, 100, 30, []bd.Bead{ini("ini", "open", "Ship", 1), sig("b", "open", 4, "ini")}, map[string]session.Info{"b": {State: session.Alive}})
	rows = m.tabAt(tabIdeate).(ideaTab).rows()
	if i := slices.IndexFunc(rows, func(r ideaRow) bool { return r.key == "live:b" }); i < 0 || rows[i].hint != "Ship" {
		t.Errorf("hint lost: %v", shape(rows))
	}
}
