package tui

import (
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/key"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

func gr(m Model) graphTab { return m.tabAt(tabGraph).(graphTab) }

// shown is the filtered tree's rows with every group open: matches and context ids.
func shown(groups []group) (matches, context []string) {
	for _, r := range navRows(groups, nil) {
		if r.beadID == "" {
			continue
		}
		if r.context {
			context = append(context, r.beadID)
		} else {
			matches = append(matches, r.beadID)
		}
	}
	return
}

// The pure filter (ADR 0143 §1–3): matches, their ancestors as uncounted context
// rows, in phase order; facets default to any.
func TestFilterGroups(t *testing.T) {
	kindless := []group{{key: "c", head: &bd.Bead{ID: "c", Kind: contract.KindChart, Title: "Chart"}, members: []bd.Bead{{ID: "k", Title: "No kind"}}}}
	for _, tc := range []struct {
		name    string
		groups  []group
		q       query
		matches []string
		context []string
	}{
		{"words AND, any order, case-insensitive", buildGraph(stubBeads()), query{status: statusAny, words: "BUILD paused"}, []string{"paused"}, []string{"ini", "ch"}},
		{"words hit the id", buildGraph(stubBeads()), query{status: statusAny, words: "oldb"}, []string{"oldb"}, []string{"old"}},
		{"ancestors are uncounted context, in phase order", buildGraph(stubBeads()), query{status: statusAny, words: "build"}, []string{"paused", "run", "oldb"}, []string{"ini", "ch", "old"}},
		{"a matching head is a match, not context", buildGraph(stubBeads()), query{status: statusAny, words: "ship"}, []string{"ini"}, nil},
		{"no match, no rows", buildGraph(stubBeads()), query{status: statusAny, words: "zzz"}, nil, nil},
		{"status facet", buildGraph(stubBeads()), query{status: "closed"}, []string{"old", "oldb"}, nil},
		{"unclosed with no words hides closed", buildGraph(stubBeads()), query{status: "unclosed"}, []string{"ini", "ch", "paused", "run", "long"}, nil},
		{"unclosed with words widens to closed", buildGraph(stubBeads()), query{status: "unclosed", words: "build"}, []string{"paused", "run", "oldb"}, []string{"ini", "ch", "old"}},
		{"unclosed widening still honours the kind facet", buildGraph(stubBeads()), query{status: "unclosed", words: "old", kind: contract.KindBuild}, []string{"oldb"}, []string{"old"}},
		{"explicit open with words still narrows", buildGraph(stubBeads()), query{status: "open", words: "old"}, nil, nil},
		{"kind facet", buildGraph(stubBeads()), query{status: statusAny, kind: contract.KindBuild}, []string{"paused", "run", "long", "oldb"}, []string{"ini", "ch", "old"}},
		{"any status, kindless beads kept", kindless, query{status: statusAny}, []string{"c", "k"}, nil},
		{"a kind drops kindless beads", kindless, query{status: statusAny, kind: contract.KindChart}, []string{"c"}, nil},
	} {
		got, n := filterGroups(tc.groups, tc.q, nil)
		matches, context := shown(got)
		if !slices.Equal(matches, tc.matches) || !slices.Equal(context, tc.context) || n != len(tc.matches) {
			t.Errorf("%s: matches %v context %v n=%d; want %v %v", tc.name, matches, context, n, tc.matches, tc.context)
		}
	}
}

func filtered(t *testing.T, m Model, words string) Model {
	t.Helper()
	m = press(t, m, runeKey("/"))
	for _, r := range words { // each key's closed search lands, so no searching… marker is left
		m = press(t, m, runeKey(string(r)))
	}
	return press(t, m, enterKey())
}

// pick opens the picker on key and chooses its item (k wraps to the last).
func pick(t *testing.T, m Model, k string, item string) Model {
	t.Helper()
	m = press(t, m, runeKey(k))
	for range 10 {
		if len(m.modals) == 0 || m.modals[0].items[m.modals[0].cur] == item {
			break
		}
		m = press(t, m, runeKey("j"))
	}
	return press(t, m, enterKey())
}

func TestGraphFilterHeaderAndEscUnwind(t *testing.T) {
	m := graphModel(t, 120, 30)
	if v := body(m); strings.Contains(v, " of ") {
		t.Fatalf("bare Graph shows a filter header:\n%s", v)
	}
	m = filtered(t, m, "paused")
	if v := body(m); !strings.Contains(v, "/ paused   1 of 7") || strings.Contains(v, "In flight build") {
		t.Errorf("filtered view:\n%s", v)
	}
	m = pick(t, m, "s", "closed")
	m = pick(t, m, "f", "build")
	if v := body(m); !strings.Contains(v, "/ paused · closed · build   0 of 7") {
		t.Errorf("header with facets:\n%s", v)
	}
	m = press(t, m, escKey())
	if q := gr(m).q; q.words != "" || q.status != "closed" || q.kind != contract.KindBuild {
		t.Fatalf("first esc clears the text only: %+v", q)
	}
	m = press(t, m, escKey())
	if q := gr(m).q; q.status != "unclosed" || q.kind != contract.KindBuild {
		t.Fatalf("second esc resets the status to unclosed: %+v", q)
	}
	m = press(t, m, escKey())
	if gr(m).q != baseline || strings.Contains(body(m), " of ") {
		t.Errorf("esc unwound everything but the header stayed:\n%s", body(m))
	}
}

func TestGraphFilterTypesLiveAndEscReverts(t *testing.T) {
	m := graphModel(t, 120, 30)
	m = typeText(t, press(t, m, runeKey("/")), "run")
	if v := body(m); strings.Contains(v, "Paused build") || !strings.Contains(v, "In flight build") || !strings.Contains(v, "1 of 7") {
		t.Errorf("live filter:\n%s", v)
	}
	if bar := lines(m.View()); !strings.Contains(bar[len(bar)-1], "enter keep") || !strings.Contains(bar[len(bar)-1], "esc revert") {
		t.Errorf("typing hints: %q", bar[len(bar)-1])
	}
	m = press(t, m, escKey())
	if g := gr(m); g.typing || g.q.active() || !strings.Contains(body(m), "Paused build") {
		t.Errorf("esc reverts the text: %+v", g.q)
	}
}

func TestGraphFilterLiveTypingShowsClosedMatchAtBaseline(t *testing.T) {
	m := typeText(t, press(t, graphModel(t, 120, 30), runeKey("/")), "oldb")
	if v := body(m); !strings.Contains(v, "Old closed build") || !strings.Contains(v, "1 of 7") {
		t.Errorf("live typing at the baseline must show the closed match:\n%s", v)
	}
}

// Context rows are real tree rows: selectable, dimmed, uncounted.
func TestGraphFilterContextRowsAreSelectable(t *testing.T) {
	m := filtered(t, graphModel(t, 120, 30), "paused")
	g := gr(m)
	var ctx []string
	for _, r := range g.rows() {
		if r.context {
			ctx = append(ctx, r.beadID)
		}
	}
	if !slices.Equal(ctx, []string{"ini", "ch"}) {
		t.Fatalf("context = %v", ctx)
	}
	if got := selectBead(t, m, "ini"); sel(got) != "ini" {
		t.Errorf("a context row is not selectable")
	}
}

// Every group with a match is forced open, space still folds, and clearing the
// query restores the folds from before it (ADR 0143 §4).
func TestGraphFilterFoldSnapshotRestore(t *testing.T) {
	m := graphModel(t, 120, 30)
	before := maps.Clone(gr(m).folds())
	m = filtered(t, m, "paused")
	if v := body(m); !strings.Contains(v, "Paused build") {
		t.Errorf("a group with a match is forced open:\n%s", v)
	}
	m = press(t, selectBead(t, m, "ch"), runeKey(" ")) // space still folds
	if v := body(m); strings.Contains(v, "Paused build") {
		t.Errorf("space did not fold under a query:\n%s", v)
	}
	m = press(t, m, escKey())
	if !maps.Equal(gr(m).folds(), before) || !strings.Contains(body(m), "Paused build") {
		t.Errorf("clearing the query did not restore the folds: %v want %v", gr(m).folds(), before)
	}
	// and a refresh mid-query keeps its folds
	m = press(t, selectBead(t, filtered(t, m, "paused"), "ch"), runeKey(" "))
	m, _ = send(t, m, loadedMsg{beads: stubBeads(), live: map[string]bool{}, sessions: map[string]session.Info{}})
	if strings.Contains(body(m), "Paused build") {
		t.Errorf("a refresh reopened the folded group")
	}
}

// A new query starts with every group open: a fold made under the last one doesn't carry (FR40).
func TestGraphFilterNewQueryResetsFolds(t *testing.T) {
	m := filtered(t, graphModel(t, 120, 30), "paused")
	m = press(t, selectBead(t, m, "ch"), runeKey(" "))
	if strings.Contains(body(m), "Paused build") {
		t.Fatal("setup: space did not fold")
	}
	m = press(t, typeText(t, press(t, m, runeKey("/")), " build"), enterKey())
	if !strings.Contains(body(m), "Paused build") {
		t.Errorf("the fold outlived its query:\n%s", body(m))
	}
}

func TestGraphFilterSelectionFollowsBead(t *testing.T) {
	m := filtered(t, graphModel(t, 120, 30), "build")
	m = selectBead(t, m, "run")
	reload := func(m Model, bs []bd.Bead) Model {
		m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{}, sessions: map[string]session.Info{}})
		return m
	}
	// another bead leaves: the selection stays on run
	m = reload(m, slices.DeleteFunc(stubBeads(), func(b bd.Bead) bool { return b.ID == "paused" }))
	if sel(m) != "run" {
		t.Errorf("selection lost its bead: %s", sel(m))
	}
	// run stops matching: the row now at its old index (clamped) is selected
	rows := gr(m).rows()
	at := indexRow(rows, "run")
	bs := stubBeads()
	for i := range bs {
		if bs[i].ID == "run" {
			bs[i].Title = "In flight task"
		}
		if bs[i].ID == "paused" {
			bs[i].Title = "Paused task"
		}
		if bs[i].ID == "long" { // keeps a match, so there is a neighbour
			bs[i].Title = "Long build"
		}
	}
	m = reload(m, bs)
	next := gr(m).rows()
	if want := next[min(at, len(next)-1)].sel; sel(m) != want {
		t.Errorf("neighbour = %s, want %s", sel(m), want)
	}
}

func TestGraphFilterSurvivesTabsAndKeepsVanishedFacet(t *testing.T) {
	m := pick(t, graphModel(t, 120, 30), "f", "chart")
	m = press(t, press(t, m, runeKey("5")), runeKey("4")) // away to Initiatives (5), back to Graph (4)
	if g := gr(m); g.q.kind != contract.KindChart || !strings.Contains(body(m), "chart") {
		t.Fatalf("the query did not survive a tab switch: %+v", g.q)
	}
	var bs []bd.Bead
	for _, b := range stubBeads() {
		if b.Kind != contract.KindChart {
			bs = append(bs, b)
		}
	}
	m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{}, sessions: map[string]session.Info{}})
	if g := gr(m); g.q.kind != contract.KindChart || !strings.Contains(body(m), "0 of") {
		t.Errorf("a vanished facet value must stay active: %+v\n%s", g.q, body(m))
	}
	m = press(t, m, runeKey("f"))
	if !slices.Contains(m.modals[0].items, "chart") || m.modals[0].items[m.modals[0].cur] != "chart" {
		t.Errorf("kind picker should keep the active value: %v", m.modals[0].items)
	}
}

// G onto a bead the query hides, or shows only as a context row, scopes Graph to it:
// words = its id, status any, no kind (Spec "G reveal scopes Graph to the revealed bead").
func assertScoped(t *testing.T, m Model, id string) {
	t.Helper()
	g := gr(m)
	if g.q != (query{words: id, status: statusAny}) || g.typing || g.sel != id || m.status != "scoped Graph to "+id {
		t.Errorf("scoped reveal: q=%+v typing=%v sel=%s status=%q", g.q, g.typing, g.sel, m.status)
	}
}

func TestGraphRevealVisibleKeepsQuery(t *testing.T) {
	m := filtered(t, graphModel(t, 120, 30), "paused")
	m, _ = send(t, m, revealMsg{"paused"})
	if g := gr(m); g.q.words != "paused" || g.q.status != "unclosed" || g.sel != "paused" || m.status != "" {
		t.Errorf("revealing a visible match: q=%+v sel=%s status=%q", g.q, g.sel, m.status)
	}
}

func TestGraphRevealScopesClosedBead(t *testing.T) {
	m := filtered(t, graphModel(t, 120, 30), "paused")
	m, _ = send(t, m, revealMsg{"oldb"})
	assertScoped(t, m, "oldb")
	if gr(m).qfold["old"] {
		t.Errorf("the bead's group must be open: %v", gr(m).qfold)
	}
}

func TestGraphRevealScopesFromKeptWords(t *testing.T) {
	m := filtered(t, graphModel(t, 120, 30), "run")
	m, _ = send(t, m, revealMsg{"oldb"})
	assertScoped(t, m, "oldb")
}

func TestGraphRevealScopesPastKindFilter(t *testing.T) {
	m := pick(t, graphModel(t, 120, 30), "f", "build")
	m, _ = send(t, m, revealMsg{"old"}) // a closed Chart: the kind hides it
	assertScoped(t, m, "old")
}

func TestGraphRevealScopesWordsOnly(t *testing.T) {
	m := filtered(t, graphModel(t, 120, 30), "zzz")
	m, _ = send(t, m, revealMsg{"paused"})
	assertScoped(t, m, "paused")
}

func TestGraphRevealScopesNonDefaultStatus(t *testing.T) {
	m := pick(t, graphModel(t, 120, 30), "s", "any")
	m = filtered(t, m, "zzz")
	m, _ = send(t, m, revealMsg{"paused"})
	assertScoped(t, m, "paused")
}

func TestGraphRevealScopesContextRowBead(t *testing.T) {
	m := filtered(t, graphModel(t, 120, 30), "paused")
	g := gr(m)
	if !slices.ContainsFunc(g.groups, func(gr group) bool { return len(gr.ctx) > 0 }) {
		t.Skip("fixture shows no context rows")
	}
	var ctxID string
	for _, gp := range g.groups {
		for id := range gp.ctx {
			ctxID = id
		}
	}
	m, _ = send(t, m, revealMsg{ctxID})
	assertScoped(t, m, ctxID)
}

func TestGraphRevealEscReturnsToBaseline(t *testing.T) {
	m := filtered(t, graphModel(t, 120, 30), "zzz")
	m, _ = send(t, m, revealMsg{"paused"})
	m = press(t, m, esc())
	if gr(m).q != baseline {
		t.Errorf("esc after a scoped reveal: %+v", gr(m).q)
	}
}

func TestGraphRevealEditedScopeEscClearsOneFacet(t *testing.T) {
	m := filtered(t, graphModel(t, 120, 30), "zzz")
	m, _ = send(t, m, revealMsg{"paused"})
	m = pick(t, m, "s", "closed") // editing the scoped query drops the mark
	m = press(t, m, esc())
	if want := (query{words: "", status: "closed"}); gr(m).q != want {
		t.Errorf("first esc: %+v, want %+v", gr(m).q, want)
	}
	m = press(t, m, esc())
	if gr(m).q != baseline {
		t.Errorf("second esc: %+v", gr(m).q)
	}
}

func TestGraphRevealNotInGraph(t *testing.T) {
	m := filtered(t, graphModel(t, 120, 30), "zzz")
	m, cmd := send(t, m, revealMsg{"nope"}) // not loaded: fetched, and bd has no such bead
	m, _ = send(t, m, cmd())
	if g := gr(m); g.q.words != "zzz" || m.status != "nope: not in the graph" {
		t.Errorf("q=%+v status=%q", g.q, m.status)
	}
}

// At the default there is no header, closed groups and closed Initiative member
// chains are hidden, and a closed bead with a session stays (ADR 0175, ADR 0038 §2).
func TestGraphDefaultStatusUnclosed(t *testing.T) {
	beads := append(stubBeads(),
		bd.Bead{ID: "donech", Kind: contract.KindChart, Container: true, Status: "closed", Parent: "ini", Title: "Done chart", Created: epoch},
		bd.Bead{ID: "doneb", Kind: contract.KindBuild, Status: "closed", Parent: "donech", Title: "Done build", Created: epoch},
		bd.Bead{ID: "solo", Kind: contract.KindSignal, Status: "closed", Title: "Closed signal", Created: epoch},
	)
	m := sized(t, 120, 40)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: map[string]session.Info{}})
	m, _ = send(t, m, runeKey("4"))
	v := body(m)
	for _, gone := range []string{"Old chart", "Done chart", "Done build", "Closed signal", "Unparented", " of "} {
		if strings.Contains(v, gone) {
			t.Errorf("default Graph shows %q:\n%s", gone, v)
		}
	}
	if !strings.Contains(v, "Paused build") {
		t.Errorf("open work missing:\n%s", v)
	}
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: map[string]session.Info{"solo": {State: session.Exited}}})
	m = unfoldUnparented(t, m) // the Unparented group opens folded
	if v := body(m); !strings.Contains(v, "Closed signal") {
		t.Errorf("a closed bead with an exited session is hidden:\n%s", v)
	}
}

func TestGraphStatusPicker(t *testing.T) {
	m := graphModel(t, 120, 30)
	m = press(t, m, runeKey("s"))
	if d := m.modals[0]; d.items[d.cur] != "unclosed" {
		t.Errorf("picker marks %q at the default, want unclosed", d.items[d.cur])
	}
	m = press(t, m, escKey())
	m = pick(t, m, "s", "any")
	m, _ = send(t, m, loadedMsg{beads: stubBeads(), slowTier: true, req: m.reqSeq, live: map[string]bool{}, sessions: map[string]session.Info{}}) // the claim load lands
	if v := body(m); !strings.Contains(v, "any   ") || !strings.Contains(v, "Old closed build") {
		t.Errorf("any: header and expanded closed groups expected:\n%s", v)
	}
	if m = press(t, m, escKey()); gr(m).q != baseline {
		t.Errorf("esc yields %+v, want baseline", gr(m).q)
	}
	m = pick(t, m, "s", "any")
	m = pick(t, m, "s", "unclosed")
	if gr(m).q != baseline || strings.Contains(body(m), " of ") {
		t.Errorf("unclosed must return to the bare Graph:\n%s", body(m))
	}
	if before := m.View(); press(t, m, escKey()).View() != before {
		t.Error("esc at the default changed the frame")
	}
}

func TestGraphFilterHints(t *testing.T) {
	bar := func(m Model) string { l := lines(m.View()); return l[len(l)-1] }
	m := graphModel(t, 160, 30)
	b := bar(m)
	for _, want := range []string{"/ search", "s status", "f kind"} {
		if !strings.Contains(b, want) {
			t.Errorf("hints lack %q: %q", want, b)
		}
	}
	if strings.Contains(b, "esc clear") {
		t.Errorf("hints list a dead esc: %q", b)
	}
	f := filtered(t, m, "x")
	if b := bar(f); strings.Contains(b, "esc clear") { // ADR 0144 §7 lists it on Logs alone; ? still shows it
		t.Errorf("filtered hints list esc clear: %q", b)
	}
	if !slices.ContainsFunc(f.tabExtra(), func(b key.Binding) bool { return b.Help().Desc == "clear" }) {
		t.Error("? lacks esc clear")
	}
}

func TestGraphFilterBindingCoverage(t *testing.T) {
	for name, m := range map[string]Model{
		"text":     filtered(t, graphModel(t, 120, 30), "build"),
		"facet":    pick(t, graphModel(t, 120, 30), "s", "open"),
		"no match": filtered(t, graphModel(t, 120, 30), "zzz"),
	} {
		checkCoverage(t, "Graph "+name, m, true, "4")
	}
}

func TestGraphFilterRendersNoSubprocess(t *testing.T) {
	log := fakeTools(t)
	m := graphModel(t, 120, 40)
	for _, k := range []string{"s", "f"} {
		p := press(t, m, runeKey(k))
		if len(p.modals) != 1 {
			t.Fatalf("%s opened no picker", k)
		}
		p.View()
	}
	typeText(t, press(t, m, runeKey("/")), "ab").View()
	filtered(t, m, "build").View()
	if got, _ := os.ReadFile(log); len(got) > 0 {
		t.Fatalf("filtering started subprocesses:\n%s", got)
	}
}

// draftBeads is stubBeads plus a Draft Build under a published Chart, a Draft
// Chart with a Draft Survey, and a Draft member Signal under the Initiative.
func draftBeads() []bd.Bead {
	return append(stubBeads(),
		bd.Bead{ID: "dbuild", Kind: contract.KindBuild, Status: "deferred", Title: "Draft build", Parent: "ch", Created: epoch},
		bd.Bead{ID: "dch", Kind: contract.KindChart, Container: true, Status: "deferred", Title: "Draft chart", Created: epoch},
		bd.Bead{ID: "dsv", Kind: contract.KindSurvey, Status: "deferred", Title: "Draft survey", Parent: "dch", Created: epoch},
		bd.Bead{ID: "dsig", Kind: contract.KindSignal, Status: "deferred", Title: "Draft signal", Parent: "ini", Created: epoch},
	)
}

// Drafts show only under the deferred status facet, with ancestors as context;
// counts follow, and gates still count a Draft as open (Spec FR2, FR3, FR6, FR7, FR9, FR10).
func TestGraphHidesDrafts(t *testing.T) {
	m := sized(t, 120, 40)
	m, _ = send(t, m, runeKey("4")) // Graph rebuilds only while it is the tab
	m, _ = send(t, m, loadedMsg{beads: draftBeads(), live: map[string]bool{}, sessions: map[string]session.Info{}})
	g := gr(m)
	all := func(g graphTab) (ids []string) {
		for _, r := range navRows(g.groups, nil) {
			ids = append(ids, r.beadID)
		}
		return ids
	}
	for _, id := range []string{"dbuild", "dch", "dsv", "dsig"} {
		if slices.Contains(all(g), id) {
			t.Errorf("default Graph shows Draft %s", id)
		}
	}
	for _, st := range []string{"unclosed", "open"} {
		g2 := g
		g2.q.status = st
		if rows := all(g2.rebuild()); slices.Contains(rows, "dbuild") || slices.Contains(rows, "dsv") {
			t.Errorf("status %s shows a Draft: %v", st, rows)
		}
	}
	if c, tot := g.childCounts("ch"); c != 0 || tot != 3 {
		t.Errorf("childCounts(ch) = %d/%d; want 0/3", c, tot)
	}
	if l, n, _ := g.initiativeProgress("ini"); l != 0 || n != 1 {
		t.Errorf("initiative landed %d/%d; want 0/1", l, n)
	}
	if _, tot := g.gateChildCounts("ch"); tot != 4 {
		t.Errorf("gate counts %d members; want 4 (Draft still open)", tot)
	}
	if g.total != 7 {
		t.Errorf("total %d; want 7 published beads", g.total)
	}

	g.q.status = "deferred"
	g = g.rebuild()
	matches, context := shown(g.groups)
	if !slices.Equal(matches, []string{"dbuild", "dsig", "dch", "dsv"}) {
		t.Errorf("deferred matches %v", matches)
	}
	if !slices.Contains(context, "ch") || !slices.Contains(context, "ini") {
		t.Errorf("deferred context %v lacks ancestors", context)
	}
	if g.matched != 4 {
		t.Errorf("deferred N of M matched = %d; want 4", g.matched)
	}
	g = g.clearOne()
	if slices.Contains(all(g), "dsv") {
		t.Error("clearing the facet leaves Drafts shown")
	}
}

// Initiative landed N/M counts a chain only when its Checkpoint closed with a landed: Landing,
// however old; nothing to land:, obsolete: and open Checkpoints stay in M alone.
func TestInitiativeProgressCountsOnlyLandedCheckpoints(t *testing.T) {
	m := sized(t, 120, 40)
	m, _ = send(t, m, runeKey("4"))
	old := epoch.Add(-48 * time.Hour)
	beads := []bd.Bead{
		{ID: "ini", Kind: contract.KindInitiative, Container: true, Status: "open", Title: "Ship v2", Created: epoch},
		{ID: "cpa", Kind: contract.KindCheckpoint, Container: true, Status: "closed", Parent: "ini", Created: epoch},
		{ID: "lda", Kind: contract.KindLanding, Status: "closed", CloseReason: "landed: abc123", Parent: "cpa", Closed: old, Created: epoch},
		{ID: "cpb", Kind: contract.KindCheckpoint, Container: true, Status: "closed", CloseReason: "nothing to land: Task-only Spec", Parent: "ini", Created: epoch},
		{ID: "cpc", Kind: contract.KindCheckpoint, Container: true, Status: "closed", CloseReason: "obsolete: pivot", Parent: "ini", Created: epoch},
		{ID: "cpd", Kind: contract.KindCheckpoint, Container: true, Status: "open", Title: "Live phase", Parent: "ini", Created: epoch},
	}
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: map[string]session.Info{}})
	if l, n, live := gr(m).initiativeProgress("ini"); l != 1 || n != 4 {
		t.Errorf("initiative landed %d/%d (live %q); want 1/4", l, n, live)
	}
}
