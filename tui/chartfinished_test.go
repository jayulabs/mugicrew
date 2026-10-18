package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

func TestChartOutcome(t *testing.T) {
	spec := dbead("sp", contract.KindSpec, "open", "", 1, causedBy("ch"))
	landing := dbead("l", contract.KindLanding, "closed", "ch", 1, closedWith("landed: abcdef0123456789"))
	cases := []struct {
		name, reason, word, sha string
		extra                   []bd.Bead
	}{
		{"landed with Spec", "", "landed", "abcdef0123456789", []bd.Bead{spec, landing}},
		{"handed back", "handed back: no", "handed back", "", nil},
		{"rejected", "rejected by human", "rejected", "", nil},
		{"empty reason", "", "superseded", "", nil},
		{"bare close, no Spec", "done", "closed", "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ch := dbead("ch", contract.KindChart, "closed", "", 1, closedWith(c.reason))
			g := graphData{beads: append([]bd.Bead{ch}, c.extra...)}
			word, sha, _ := g.chartOutcome(&g.beads[0])
			if word != c.word || sha != c.sha {
				t.Errorf("got %q %q, want %q %q", word, sha, c.word, c.sha)
			}
		})
	}
}

func TestFinishedSubsectionAndRows(t *testing.T) {
	now := time.Now()
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = time.Now })
	closedChart := func(id, reason string, age time.Duration) bd.Bead {
		return dbead(id, contract.KindChart, "closed", "", 1, closedWith(reason), closedOn(now.Add(-age)))
	}
	g := graphData{beads: []bd.Bead{
		closedChart("old", "handed back: x", 2*time.Hour),
		closedChart("new", "", time.Hour),
		closedChart("stale", "done", 25*time.Hour),
		dbead("sp", contract.KindSpec, "open", "", 1, causedBy("old")),
		dbead("l", contract.KindLanding, "closed", "old", 1, closedWith("landed: aaaaaaaabbbbbbbb")),
	}}
	fin := func(rows []dRow) (out []string) {
		for _, r := range rows {
			if r.kind == dFin {
				out = append(out, r.key)
			}
		}
		return
	}
	// folded on a fresh tab: header with count, no rows, not Actionable
	rows := g.defogList(nil)
	at := slices.IndexFunc(rows, func(r dRow) bool { return r.key == finKey })
	if at < 0 || rows[at].title != "Finished in last 24h (2)" || len(fin(rows)) != 0 {
		t.Fatalf("folded = %v", dshape(rows))
	}
	if len(g.defogAct()) != 0 || g.defogBadge() != 0 {
		t.Errorf("finished rows leaked into Actionable/badge")
	}
	// unfolded: newest first, 24h window on the Chart's Closed
	rows = g.defogList(map[string]bool{finKey: true})
	if got := strings.Join(fin(rows), ","); got != "fin:new,fin:old" {
		t.Errorf("rows = %s", got)
	}
	// header omitted at zero
	if rows := (graphData{}).defogList(nil); slices.ContainsFunc(rows, func(r dRow) bool { return r.key == finKey }) {
		t.Errorf("header at zero")
	}
	tab := defogTab{g: g}
	var old dRow
	for _, r := range rows {
		if r.key == "fin:old" {
			old = r
		}
	}
	if l, _ := tab.line(old, 12, "", "", nil); !strings.Contains(l, "landed aaaaaaaa · 2h") {
		t.Errorf("line %q", l)
	}
	if d := tab.rowText(old); !strings.Contains(d, "Spec sp is caused by the Chart") || !strings.Contains(d, "refuses — "+finRefuse) {
		t.Errorf("detail %q", d)
	}
	if tab.enterWhy(old) != "finished — nothing to launch" {
		t.Errorf("enterWhy %q", tab.enterWhy(old))
	}
}
