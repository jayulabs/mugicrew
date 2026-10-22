package tui

import (
	"slices"
	"strconv"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// gotoKey is g then the option key k.
func gotoKey(t *testing.T, m Model, k string) Model {
	t.Helper()
	return press(t, press(t, m, runeKey("g")), runeKey(k))
}

func gotoOpen(m Model) bool { return len(m.modals) > 0 && m.modals[len(m.modals)-1].gotoMenu }

func lastLine(m Model) string { l := lines(m.View()); return l[len(l)-1] }

func TestGotoTopBottomOnGraph(t *testing.T) {
	m := graphModel(t, 120, 30)
	rows := m.tabAt(tabGraph).(graphTab).rows()
	first, last := rows[0].sel, rows[len(rows)-1].sel
	for _, c := range []struct{ open, fire, want string }{{"g", "G", last}, {"g", "b", last}, {"g", "g", first}, {"g", "t", first}} {
		n := press(t, m, runeKey(c.open))
		if !gotoOpen(n) {
			t.Fatalf("g did not open the goto menu:\n%s", ansi.Strip(n.View()))
		}
		n = press(t, n, runeKey(c.fire))
		if gotoOpen(n) || sel(n) != c.want {
			t.Errorf("g %s: open=%v sel=%q, want %q", c.fire, gotoOpen(n), sel(n), c.want)
		}
	}
	// j then enter picks the row under the cursor: the second row is bottom
	n := press(t, press(t, press(t, m, runeKey("g")), runeKey("j")), enterKey())
	if sel(n) != last {
		t.Errorf("g j enter: sel=%q, want %q", sel(n), last)
	}
}

func TestGotoEscClosesAndSwallowsKeys(t *testing.T) {
	m := graphModel(t, 120, 30)
	open := press(t, m, runeKey("g"))
	if n := press(t, open, escKey()); gotoOpen(n) || n.View() != m.View() {
		t.Errorf("esc did not close the menu with no effect")
	}
	for _, k := range []string{"p", "i", "o", "q"} {
		n := press(t, open, runeKey(k))
		if !gotoOpen(n) || n.tab != m.tab || n.View() != open.View() {
			t.Errorf("%q reached past the open menu", k)
		}
	}
}

func TestGotoNoOps(t *testing.T) {
	m := graphModel(t, 120, 30)
	for _, tab := range []string{"#", "*"} { // Stats, Settings: no list
		n := press(t, m, runeKey(tab))
		if p := press(t, n, runeKey("g")); gotoOpen(p) || p.View() != n.View() {
			t.Errorf("g did something on tab %s", tab)
		}
	}
	ref := press(t, m, runeKey("?"))
	if n := press(t, ref, runeKey("g")); gotoOpen(n) || !n.ref {
		t.Error("g acted under ?")
	}
	typing := press(t, m, runeKey("/"))
	if n := press(t, typing, runeKey("g")); gotoOpen(n) {
		t.Error("g opened the menu inside the / filter")
	}
	dlg := m.push(confirmDialog("x", nil, "", nil))
	if n := press(t, dlg, runeKey("g")); gotoOpen(n) || len(n.modals) != 1 {
		t.Error("g acted inside a dialog")
	}
}

func TestGotoInFocusView(t *testing.T) {
	m, _ := defaultFocus(t)
	m = ideaFocus(t, m)
	rows := m.focusRows()
	n := press(t, press(t, m, runeKey("g")), runeKey("G"))
	if want := rows[len(rows)-1].key; n.focus.sel != want {
		t.Errorf("g G in focus: sel=%q, want %q", n.focus.sel, want)
	}
	n = press(t, press(t, n, runeKey("g")), runeKey("g"))
	if n.focus.sel != "head" {
		t.Errorf("g g in focus: sel=%q, want head", n.focus.sel)
	}
}

func TestGotoHintsAndRef(t *testing.T) {
	m := graphModel(t, 120, 30)
	if h := lastLine(m); !strings.Contains(ansi.Strip(h), "g goto") {
		t.Errorf("hints bar lacks g goto: %q", h)
	}
	stats := press(t, m, runeKey("#"))
	if h := lastLine(stats); strings.Contains(ansi.Strip(h), "g goto") {
		t.Errorf("Stats hints g goto: %q", h)
	}
	if v := ansi.Strip(press(t, m, runeKey("?")).View()); !strings.Contains(v, "top") || !strings.Contains(v, "bottom") {
		t.Errorf("? lacks the goto group:\n%s", v)
	}
}

func TestGotoMenuBindingCoverage(t *testing.T) {
	m := press(t, graphModel(t, 120, 30), runeKey("g"))
	checkCoverage(t, "goto menu", m, false)
}

func TestGotoRevealOfferedOnGraphAndScopesInPlace(t *testing.T) {
	m := selectBead(t, graphModel(t, 120, 30), "run")
	if menu := press(t, m, runeKey("g")).View(); !strings.Contains(menu, "reveal") {
		t.Errorf("Graph menu lacks r:\n%s", menu)
	}
	n := gotoKey(t, m, "r")
	if g := gr(n); gotoOpen(n) || n.tab != tabGraph || g.q.scope == "" || g.sel != "run" {
		t.Errorf("r on Graph: tab %d scope %q sel %q", n.tab, g.q.scope, g.sel)
	}
}

// relBeads: B has parent P and predecessor C, and one bead on every edge kind in both
// directions, mixed open/closed; "dup" is both blocked-by and a child.
func relBeads() []bd.Bead {
	mk := func(id string, mod func(*bd.Bead)) bd.Bead {
		b := bd.Bead{ID: id, Kind: contract.KindBuild, Status: "open", Title: "t-" + id, Created: epoch}
		if mod != nil {
			mod(&b)
		}
		return b
	}
	closed := func(b *bd.Bead) { b.Status = "closed" }
	return []bd.Bead{
		mk("P", func(b *bd.Bead) { b.Container, b.Kind = true, contract.KindChart }),
		mk("C", func(b *bd.Bead) { b.Kind = contract.KindSignal }),
		mk("B", func(b *bd.Bead) {
			b.Parent, b.CausedBy, b.BlockedBy, b.Related = "P", "C", []string{"bbX", "bbY", "dup"}, []string{"relA"}
		}),
		mk("bbX", closed),
		mk("bbY", nil),
		mk("dup", func(b *bd.Bead) { b.Parent = "B" }),
		mk("blk", func(b *bd.Bead) { b.BlockedBy = []string{"B"} }),
		mk("kidC", func(b *bd.Bead) { b.Parent, b.Status = "B", "closed" }),
		mk("kidO", func(b *bd.Bead) { b.Parent = "B" }),
		mk("succ", func(b *bd.Bead) { b.CausedBy = "B" }),
		mk("offC", func(b *bd.Bead) { b.DiscoveredFrom = []string{"B"}; b.Status = "closed" }),
		mk("offO", func(b *bd.Bead) { b.DiscoveredFrom = []string{"B"} }),
		mk("offKid", func(b *bd.Bead) { b.DiscoveredFrom, b.Parent = []string{"B"}, "B" }), // already a child
		mk("relA", nil),
		mk("relB", func(b *bd.Bead) { b.Related = []string{"B"}; b.Status = "closed" }),
	}
}

func relModel(t *testing.T, beads []bd.Bead, id string) Model {
	t.Helper()
	m := graphModel(t, 140, 40)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: map[string]session.Info{}})
	g := m.tabAt(tabGraph).(graphTab)
	g.q = query{words: id, status: statusAny}
	g, _, _ = g.rebuild().reveal(id)
	m.tabs[tabGraph] = g
	return m
}

func TestGotoRelatedRows(t *testing.T) {
	m := press(t, relModel(t, relBeads(), "B"), runeKey("g"))
	v := ansi.Strip(m.View())
	want := []string{"1 ", "blocked-by", "t-bbY", "2 ", "t-dup", "3 ", "t-bbX", "blocks", "t-blk", "child", "t-kidO", "t-kidC", "successor", "t-succ", "offshoot", "t-offO", "t-offC", "related", "t-relA", "t-relB"}
	at := strings.Index(v, "parent")
	if at < 0 || !strings.Contains(v, "cause") {
		t.Fatalf("no p/c rows:\n%s", v)
	}
	for _, w := range want { // in order, each once
		i := strings.Index(v[at:], w)
		if i < 0 {
			t.Fatalf("%q missing or out of order:\n%s", w, v)
		}
		at += i + len(w)
	}
}

func TestGotoPCRowsDrawSBRN(t *testing.T) {
	v := ansi.Strip(press(t, relModel(t, relBeads(), "B"), runeKey("g")).View())
	col := map[string]int{}
	for _, l := range strings.Split(v, "\n") {
		for _, c := range []struct{ tag, title string }{{"parent", "t-P"}, {"cause", "t-C"}, {"related", "t-relA"}} {
			if strings.Contains(l, c.tag+" ") && strings.Contains(l, c.title) {
				col[c.tag] = strings.Index(l, c.title)
			}
		}
	}
	if len(col) != 3 || col["parent"] != col["cause"] || col["cause"] != col["related"] {
		t.Errorf("title columns %v not aligned:\n%s", col, v)
	}
}

func TestGotoClosedCauseDimmedAndLongTitleTruncated(t *testing.T) {
	bs := relBeads()
	bs[1].Status, bs[0].Title = "closed", strings.Repeat("long", 60)
	m := press(t, relModel(t, bs, "B"), runeKey("g"))
	raw := m.View()
	if !strings.Contains(raw, dimSt.Render("cause       ✓ SIGNAL  P0 · t-C · C")) {
		t.Errorf("closed cause row not dimmed:\n%s", ansi.Strip(raw))
	}
	v := ansi.Strip(raw)
	if !strings.Contains(v, "…") || strings.Contains(v, strings.Repeat("long", 60)) {
		t.Errorf("long parent title not truncated:\n%s", v)
	}
}

func TestGotoPCHiddenWithoutLinks(t *testing.T) {
	m := press(t, relModel(t, relBeads(), "bbY"), runeKey("g"))
	v := ansi.Strip(m.View())
	if strings.Contains(v, "p   parent") || strings.Contains(v, "c   cause") || strings.Contains(v, "cause  ") || strings.Contains(v, "parent  ") {
		t.Errorf("p/c shown for a bead with neither:\n%s", v)
	}
}

// landedOn asserts m is on Graph scoped to head with id selected.
func landedOn(t *testing.T, name string, m Model, head, id string) {
	t.Helper()
	if g := gr(m); gotoOpen(m) || m.tab != tabGraph || g.q.scope != head || g.sel != id {
		t.Errorf("%s: tab %d scope %q sel %q, want Graph scoped to %q on %q", name, m.tab, g.q.scope, g.sel, head, id)
	}
}

func TestGotoBeadRowsLandOnGraphScopedToHead(t *testing.T) {
	m := relModel(t, relBeads(), "B")
	for _, c := range []struct{ keys, head, id string }{{"p", "P", "P"}, {"c", "C", "C"}, {"1", "bbY", "bbY"}} {
		landedOn(t, "g "+c.keys, gotoKey(t, m, c.keys), c.head, c.id)
	}
}

func TestGotoRowsPastNineViaJK(t *testing.T) {
	bs := relBeads()
	for i := range 10 { // ten more successors: 11 rows past the 9 keyed
		bs = append(bs, bd.Bead{ID: "s" + strconv.Itoa(i), Kind: contract.KindBuild, Status: "open", Title: "s", CausedBy: "B", Created: epoch})
	}
	m := press(t, relModel(t, bs, "B"), runeKey("g"))
	if press(t, m, runeKey("0")).View() != m.View() {
		t.Error("0 acted")
	}
	// k wraps to the last row; enter jumps there
	n := press(t, press(t, m, runeKey("k")), enterKey())
	landedOn(t, "k enter", n, "relB", "relB")
}

func TestGotoWritesNoRecents(t *testing.T) {
	m := relModel(t, relBeads(), "B")
	_, cmd := send(t, press(t, m, runeKey("g")), runeKey("p"))
	if cmd != nil {
		if msg := cmd(); msg != nil {
			if _, ok := msg.(recentsMsg); ok {
				t.Error("a goto recorded a Recents interaction")
			}
		}
	}
}

// gotoFocus is g then f: the way into the Initiative focus.
func gotoFocus(t *testing.T, m Model) Model {
	return press(t, press(t, m, runeKey("g")), runeKey("f"))
}

// i is unbound in the goto menu: it stays open and nothing opens (ADR 0228, one key per move).
func TestGotoIIsUnbound(t *testing.T) {
	m, _ := onInitsRow(t)
	open := press(t, m, runeKey("g"))
	if n := press(t, open, runeKey("i")); !gotoOpen(n) || focused(n) || n.View() != open.View() {
		t.Errorf("g i changed something:\n%s", ansi.Strip(n.View()))
	}
}

// withOrigins is relBeads with B discovered-from the given ids (in edge order) and extra
// beads added.
func withOrigins(from []string, extra ...bd.Bead) []bd.Bead {
	bs := relBeads()
	bs[beadIndex(bs, "B")].DiscoveredFrom = from
	return append(bs, extra...)
}

func rawBead(id, status string) bd.Bead {
	return bd.Bead{ID: id, Kind: contract.KindSignal, Status: status, Title: "t-" + id, Created: epoch}
}

func TestGotoOriginRow(t *testing.T) {
	m := relModel(t, withOrigins([]string{"orA"}, rawBead("orA", "closed")), "B")
	v := ansi.Strip(press(t, m, runeKey("g")).View())
	at := 0
	for _, w := range []string{"parent", "cause", "origin", "t-orA", "blocked-by"} {
		i := strings.Index(v[at:], w)
		if i < 0 {
			t.Fatalf("%q missing or out of order (p, c, o):\n%s", w, v)
		}
		at += i + len(w)
	}
	// g o lands on the origin's group and writes no Recents entry
	landedOn(t, "g o", press(t, press(t, m, runeKey("g")), runeKey("o")), "orA", "orA")
	if _, cmd := send(t, press(t, m, runeKey("g")), runeKey("o")); cmd != nil {
		if _, ok := cmd().(recentsMsg); ok {
			t.Error("g o recorded a Recents interaction")
		}
	}
}

func TestGotoOriginRowsKeepEdgeOrder(t *testing.T) {
	// edge order: oldest first, a closed one before an open one; P (parent) and C (cause)
	// are skipped, "gone" does not resolve, so o is the first usable source
	bs := withOrigins([]string{"P", "gone", "orC", "C", "orO"}, rawBead("orC", "closed"), rawBead("orO", "open"))
	m := relModel(t, bs, "B")
	v := ansi.Strip(press(t, m, runeKey("g")).View())
	if strings.Contains(v, "gone") || strings.Count(v, "origin") != 2 {
		t.Errorf("a parent/cause/unresolved source shows as an origin row:\n%s", v)
	}
	if i, j := strings.Index(v, "t-orC"), strings.Index(v, "t-orO"); i < 0 || j < i {
		t.Fatalf("origin rows out of edge order:\n%s", v)
	}
	if got := press(t, press(t, m, runeKey("g")), runeKey("o")); sel(got) != "orC" {
		t.Errorf("o went to %q, want orC", sel(got))
	}
	got := press(t, press(t, m, runeKey("g")), runeKey("1"))
	landedOn(t, "1 (origin rows are keyed from 1)", got, "orO", "orO")
}

func TestGotoNoOriginWhenNoSources(t *testing.T) {
	plain := ansi.Strip(press(t, relModel(t, relBeads(), "B"), runeKey("g")).View())
	unresolved := ansi.Strip(press(t, relModel(t, withOrigins([]string{"gone"}), "B"), runeKey("g")).View())
	if plain != unresolved || strings.Contains(plain, "origin") {
		t.Errorf("menu differs without a resolved source:\n%s\n%s", plain, unresolved)
	}
}

// gotoKeys is the option keys of the open goto menu.
func gotoKeys(m Model) (ks []string) {
	for _, o := range m.gotoOpts() {
		ks = append(ks, strings.Join(o.keys, ""))
	}
	return
}

// History and Logs offer g f when the selected bead has an Initiative ancestor, and not otherwise.
func TestGotoFocusOnHistoryAndLogs(t *testing.T) {
	has := func(m Model) bool { return slices.Contains(gotoKeys(m), "f") }

	beads := append(histBeads(), bd.Bead{ID: "ini", Kind: contract.KindInitiative, Container: true, Status: "open", Title: "Init"})
	beads[0].Parent = "ini" // cp-a, the selected revert's bead
	m := sized(t, 100, 24)
	m, _ = send(t, m, histLoad(histCommits(), beads))
	m = press(t, press(t, m, runeKey("6")), runeKey("j"))
	if !has(m) {
		t.Fatalf("History under an Initiative: no f in %v", gotoKeys(m))
	}
	if n := gotoFocus(t, m); !focused(n) || n.focus.id != "ini" {
		t.Errorf("History g f: focus=%q", n.focus.id)
	} else if back := press(t, n, escKey()); focused(back) || back.tab != tabHistory || histSel(back) != histSel(m) {
		t.Errorf("leaving the focus did not return to History as it was: tab=%d sel=%q", back.tab, histSel(back))
	}
	if has(goSha(t, m, "0000001")) { // a plain commit: no bead
		t.Errorf("History plain row offers f")
	}

	l := selectLogs(logsModel(t, 120, 30)) // effort.failed on run, under ini
	if !has(l) {
		t.Fatalf("Logs under an Initiative: no f in %v", gotoKeys(l))
	}
	if n := gotoFocus(t, l); !focused(n) || n.focus.id != "ini" {
		t.Errorf("Logs g f: focus=%q", n.focus.id)
	} else if back := press(t, n, escKey()); focused(back) || back.tab != tabLogs || logs(back).sel != logs(l).sel {
		t.Errorf("leaving the focus did not return to Logs as it was: tab=%d", back.tab)
	}
	lt := logs(l)
	for _, e := range lt.events {
		if e.Msg == "loop.slow" { // oldb sits under a chart with no Initiative
			lt.sel = e.Key()
		}
	}
	l.tabs[tabLogs] = lt
	if has(l) {
		t.Errorf("Logs bead without an Initiative offers f: %v", gotoKeys(l))
	}
}

// Inside the focus view f is never offered.
func TestGotoNoFocusInFocus(t *testing.T) {
	m, _ := defaultFocus(t)
	m = ideaFocus(t, m)
	if slices.Contains(gotoKeys(m), "f") {
		t.Errorf("f offered inside the focus: %v", gotoKeys(m))
	}
}
