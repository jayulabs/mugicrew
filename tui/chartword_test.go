package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

func TestChartWords(t *testing.T) {
	k := func(id string, kind contract.Kind, status string, mut ...func(*bd.Bead)) bd.Bead {
		return dbead(id, kind, status, "ch", 1, mut...)
	}
	sig := func(id string) bd.Bead { return dbead(id, contract.KindSignal, "open", "", 1) }
	own := func(mut ...func(*bd.Bead)) func(*bd.Bead) { // the Chart's own mutations
		return func(b *bd.Bead) {
			for _, m := range mut {
				m(b)
			}
		}
	}
	landed := k("l", contract.KindLanding, "closed", closedWith("landed: abc123"))
	cases := []struct {
		name  string
		chart func(*bd.Bead)
		kids  []bd.Bead
		extra []bd.Bead
		live  bool
		word  string
		rank  int
		also  string
	}{
		{"speccing", own(func(b *bd.Bead) { b.Status = "in_progress" }), []bd.Bead{landed}, nil, true, "speccing", 1, "landed"},
		{"landed", nil, []bd.Bead{landed}, nil, false, "landed", 2, ""},
		{"landed but the Chart in_progress with no live session", own(func(b *bd.Bead) { b.Status = "in_progress" }), []bd.Bead{landed}, nil, false, "landed", 2, ""},
		{"land failed", nil, []bd.Bead{k("l", contract.KindLanding, "open", labeled("paused:failing"))}, nil, false, "land failed", 3, "landing"},
		{"land paused", nil, []bd.Bead{k("l", contract.KindLanding, "open", labeled("paused:manual"))}, nil, false, "land paused", 3, "landing"},
		{"landing", nil, []bd.Bead{k("l", contract.KindLanding, "in_progress")}, nil, false, "landing", 4, ""},
		{"conflict", nil, []bd.Bead{k("c", contract.KindChartConflict, "open")}, nil, false, "conflict", 5, ""},
		{"blocked on Signal", own(blockedBy("s1")), nil, []bd.Bead{sig("s1")}, false, "blocked on Signal", 6, "landing due"},
		{"blocked on Signal +1", own(func(b *bd.Bead) { b.BlockedBy = []string{"s1", "s2"} }), nil, []bd.Bead{sig("s1"), sig("s2")}, false, "blocked on Signal +1", 6, "landing due"},
		{"failing", own(labeled("paused:failing")), nil, nil, false, "failing", 7, "landing due"},
		{"paused", own(labeled("paused:manual")), nil, nil, false, "paused", 7, "landing due"},
		{"idle", nil, []bd.Bead{k("a", contract.KindBuild, "open", blockedBy("b")), k("b", contract.KindBuild, "open", blockedBy("a"))}, nil, false, "idle ⚠", 17, ""},
		{"charting", nil, []bd.Bead{k("sv", contract.KindSurvey, "in_progress")}, nil, false, "survey due", 8, ""},
		{"your turn over defog queued", nil, []bd.Bead{k("g", contract.KindGrilling, "open"), k("p", contract.KindResearch, "open")}, nil, false, "your turn", 9, "defog queued"},
		{"defogging", nil, []bd.Bead{k("p", contract.KindResearch, "in_progress")}, nil, true, "defogging", 10, ""},
		{"fog blocked on Signal", nil, []bd.Bead{k("g", contract.KindGrilling, "open", blockedBy("s1")), k("p", contract.KindResearch, "open", blockedBy("s1"))}, []bd.Bead{sig("s1")}, false, "fog blocked on Signal", 11, ""},
		{"drafts pending", nil, []bd.Bead{k("d", contract.KindGrilling, "deferred")}, nil, false, "drafts pending", 12, ""},
		{"integrate due", nil, []bd.Bead{k("b", contract.KindBuild, "closed")}, nil, false, "integrate due", 13, "landing due"},
		{"judge due", own(func(b *bd.Bead) { b.Labels = nil }), nil, nil, false, "judge due", 15, ""},
		{"judging", own(func(b *bd.Bead) { b.Labels, b.Status = nil, "in_progress" }), nil, nil, true, "judging", 14, ""},
		{"landing due", nil, nil, nil, false, "landing due", 16, ""},
		{"blocked on over failing", own(blockedBy("s1"), labeled("paused:failing")), nil, []bd.Bead{sig("s1")}, false, "blocked on Signal", 6, "failing, landing due"},
		{"conflict over landing and blocked", own(blockedBy("s1")), []bd.Bead{k("c", contract.KindChartConflict, "open"), k("l", contract.KindLanding, "open")}, []bd.Bead{sig("s1")}, false, "landing", 4, "conflict, blocked on Signal"},
		{"a closed conflict or Landing without landed: reads landing due", nil, []bd.Bead{k("c", contract.KindChartConflict, "closed"), k("l", contract.KindLanding, "closed", closedWith("obsolete: conflict"))}, nil, false, "landing due", 16, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			beads := append([]bd.Bead{dbead("ch", contract.KindChart, "open", "", 1, isContainer, labeled("chart:defogged"), func(b *bd.Bead) {
				if c.chart != nil {
					c.chart(b)
				}
			})}, c.kids...)
			g := graphData{beads: append(beads, c.extra...), live: map[string]bool{"ch": c.live, "p": c.live}, efforts: map[string]bool{"b": true}}
			r := g.chartWord(g.beadByID("ch"))
			if r.Word != c.word || r.Rank != c.rank || strings.Join(r.Also, ", ") != c.also || r.Why == "" {
				t.Errorf("chartWord = %q rank %d also %v why %q; want %q rank %d also %q", r.Word, r.Rank, r.Also, r.Why, c.word, c.rank, c.also)
			}
		})
	}
}

func TestChartWordWhyNamesDriver(t *testing.T) {
	g := graphData{beads: []bd.Bead{
		dbead("ch", contract.KindChart, "open", "", 1, isContainer),
		dbead("lx", contract.KindLanding, "closed", "ch", 1, closedWith("landed: abc123")),
	}}
	if why := g.chartWord(g.beadByID("ch")).Why; !strings.Contains(why, "lx") || !strings.Contains(why, "landed: abc123") {
		t.Errorf("why = %q", why)
	}
}

func chartsFixture() []bd.Bead {
	return []bd.Bead{
		dbead("a", contract.KindChart, "open", "", 1),                             // idle ⚠ (15)
		dbead("st", contract.KindChart, "open", "", 2, labeled("chart:defogged")), // stamped, landing (4)
		dbead("stl", contract.KindLanding, "open", "st", 1),
		dbead("dr", contract.KindChart, "deferred", "", 1),                             // a Draft: out
		dbead("cl", contract.KindChart, "closed", "", 1),                               // closed: out
		dbead("b", contract.KindChart, "in_progress", "", 0, labeled("paused:manual")), // paused (7)
	}
}

func chartsKeys(g graphData, flip map[string]bool) string {
	var ks []string
	for _, r := range g.defogList(flip) {
		if r.kind == dCW {
			ks = append(ks, r.key)
		}
	}
	return strings.Join(ks, ",")
}

func TestDefogChartsSection(t *testing.T) {
	g := graphData{beads: chartsFixture()}
	var heads []string
	for _, r := range g.defogList(nil) {
		if r.kind == dSec {
			heads = append(heads, r.title)
		}
	}
	if got, want := strings.Join(heads, ","), "Actionable (0),Live (0),Charts (3),Initiatives (0)"; got != want {
		t.Errorf("sections = %s, want %s", got, want)
	}
	// stamped included, Draft and closed out; rank first (st landing 4, b paused 7, a judge due 15)
	if got := chartsKeys(g, nil); got != "charts:st,charts:b,charts:a" {
		t.Errorf("rows = %s", got)
	}
	// same rank sorts by beadCmp
	g.beads = append(g.beads, dbead("a0", contract.KindChart, "open", "", 0))
	if got := chartsKeys(g, nil); got != "charts:st,charts:b,charts:a0,charts:a" {
		t.Errorf("tie order = %s", got)
	}
	// folding hides the rows and keeps N
	folded := g.defogList(map[string]bool{chartsKey: true})
	if chartsKeys(g, map[string]bool{chartsKey: true}) != "" || folded[4].title != "Charts (4)" {
		t.Errorf("folded: %d rows, header %q", len(folded), folded[4].title)
	}
	// empty: a placeholder
	if rows := (graphData{}).defogList(nil); !slices.ContainsFunc(rows, func(r dRow) bool { return r.key == chartsNone }) {
		t.Error("no placeholder in the empty section")
	}
}

func TestDefogChartsNeverActionable(t *testing.T) {
	g := graphData{beads: chartsFixture()}
	for _, r := range g.defogAct() {
		if r.kind == dCW {
			t.Errorf("%s in Actionable", r.key)
		}
	}
	if g.defogBadge() != 0 {
		t.Errorf("badge = %d", g.defogBadge())
	}
}

func TestDefogChartsRowLineAndDetail(t *testing.T) {
	m := fresh(t, "2", chartsFixture(), nil)
	line := screen(m)
	if !strings.Contains(line, "t st · st · landing") || !strings.Contains(line, "· paused") || !strings.Contains(line, "judge due") {
		t.Errorf("rows lack the status words:\n%s", line)
	}
	tab := m.tabAt(tabDefog).(defogTab)
	var row dRow
	for _, r := range tab.rows() {
		if r.key == "charts:st" {
			row = r
		}
	}
	d := ansi.Strip(tab.rowText(row))
	for _, want := range []string{"chart word", "landing", "why", "stl"} {
		if !strings.Contains(d, want) {
			t.Errorf("detail lacks %q:\n%s", want, d)
		}
	}
	if a, b := tab.enterField(row), tab.enterField(dRow{kind: dChart, b: row.b}); a != b {
		t.Errorf("enter %q, want the tree row's %q", a, b)
	}
}

func TestDefogChartsFoldPersistsAndCursorFallsBack(t *testing.T) {
	dir := t.TempDir()
	m := defogFoldModel(t, dir, chartsFixture()...)
	m = press(t, m, runeKey("2"))
	press(t, defogAt(t, m, chartsKey), runeKey(" "))
	if got := readDefogFolds(t, dir); got != `{"s:charts":true}` {
		t.Errorf("folds = %s", got)
	}
	// a Charts row's Chart closes: the cursor lands on the section header, not the first row
	m = defogAt(t, m, "charts:a")
	beads := slices.Clone(chartsFixture())
	for i := range beads {
		if beads[i].ID == "a" {
			beads[i].Status = "closed"
		}
	}
	_, sess := defogFixture()
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: sess})
	if got := m.tabAt(tabDefog).(defogTab); got.rows()[got.selIndex(got.rows())].key != chartsKey {
		t.Errorf("cursor on %q", got.rows()[got.selIndex(got.rows())].key)
	}
}

func TestChartWordQualifiers(t *testing.T) {
	land := func(id, status string, mut ...func(*bd.Bead)) bd.Bead {
		return dbead(id, contract.KindLanding, status, "ch", 1, mut...)
	}
	obs, done := closedWith("obsolete: conflict"), closedWith("landed: abc123")
	cases := []struct {
		name string
		kids []bd.Bead
		ev   string
		want string
	}{
		{"one Landing has no round", []bd.Bead{land("l", "open")}, "", "landing"},
		{"round 2", []bd.Bead{land("a", "closed", done), land("b", "open")}, "", "landed · round 2"},
		{"re-land", []bd.Bead{land("a", "closed", obs)}, "", "judge due · re-land"},
		{"retrying on landing", []bd.Bead{land("l", "open")}, "gate.infra", "landing · retrying"},
		{"order", []bd.Bead{land("a", "closed", obs), land("b", "open")}, "land.failed", "landing · round 2 · re-land · retrying"},
		{"queued is not shown", []bd.Bead{land("l", "open")}, "land.queued", "landing"},
		{"retrying absent off landing", []bd.Bead{land("l", "open", labeled("paused:failing"))}, "land.failed", "land failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := graphData{beads: append([]bd.Bead{dbead("ch", contract.KindChart, "open", "", 1, isContainer)}, c.kids...), landEv: map[string]string{"b": c.ev, "l": c.ev}}
			if got := g.chartWord(g.beadByID("ch")).label(); got != c.want {
				t.Errorf("label = %q, want %q", got, c.want)
			}
		})
	}
}

// d shows why, and also only for a multi-match Chart; enter on a Charts row acts as on the Chart's tree row.
func TestDefogChartsDetailAlsoAndEnter(t *testing.T) {
	beads := append(chartsFixture(),
		dbead("m", contract.KindChart, "open", "", 1, labeled("chart:defogged")),
		dbead("ml", contract.KindLanding, "open", "m", 1),
		dbead("mc", contract.KindChartConflict, "open", "m", 1))
	m := fresh(t, "2", beads, nil)
	tab := m.tabAt(tabDefog).(defogTab)
	text := func(id string) string {
		for _, r := range tab.rows() {
			if r.key == "charts:"+id {
				return ansi.Strip(tab.rowText(r))
			}
		}
		t.Fatalf("no row %s", id)
		return ""
	}
	if d := text("m"); !strings.Contains(d, "why") || !strings.Contains(d, "also") || !strings.Contains(d, "conflict") {
		t.Errorf("two-match detail:\n%s", d)
	}
	if d := text("st"); !strings.Contains(d, "why") || strings.Contains(d, "also") {
		t.Errorf("single-match detail:\n%s", d)
	}
	g := m.gateData()
	for _, id := range []string{"b", "st"} { // paused, and stamped (no tree row)
		before := m
		before.status = ""
		got := defogAt(t, before, "charts:"+id)
		got, _ = send(t, got, enterKey())
		if why := g.rowEnterWhy(g.beadByID(id), false); why != "" && got.status != why {
			t.Errorf("%s: status %q, want %q", id, got.status, why)
		}
	}
}
