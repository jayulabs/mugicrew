package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

func described(d string) func(*bd.Bead) { return func(b *bd.Bead) { b.Description = d } }

// mrGraph is a gitlab graph: Checkpoint cp (with an open Landing at status), a nested-repo
// Checkpoint cn, a Chart ch, and one Watch under each, plus the Watches that must not list.
func mrGraph(status string) graphData {
	cp, w := contract.KindCheckpoint, contract.KindMRWatch
	return graphData{landName: git.LandStrategyGitlab, beads: []bd.Bead{
		dbead("cp", cp, "open", "", 1, isContainer),
		dbead("cn", cp, "open", "", 2, isContainer, func(b *bd.Bead) { b.Labels = append(b.Labels, "context:web") }),
		dbead("ch", contract.KindChart, "open", "", 3),
		dbead("ln", contract.KindLanding, "open", "cp", 1, described("MR: !7\nMR status: "+status)),
		dbead("w1", w, "open", "cp", 1, described("MR: !7\nMR link: https://gl.example/g/p/-/merge_requests/7")),
		dbead("w2", w, "open", "cn", 1, described("MR: g/web!9")),
		dbead("w3", w, "open", "ch", 1, described("MR: !11")),
		dbead("w4", w, "open", "ch", 2),
		dbead("wd", w, "deferred", "cp", 2, labeled("drafted-by:x")),
		dbead("wc", w, "closed", "cp", 3, described("MR: !3")),
		dbead("wo", w, "open", "gone", 1, described("MR: !13")),
	}}
}

func mrShape(g graphData) []string {
	var out []string
	for _, r := range g.execList(map[string]bool{}) {
		if r.kind == eMR {
			out = append(out, r.b.ID)
		}
	}
	return out
}

// dmrShape is the Watch ids of Defog's Merge Requests rows.
func dmrShape(g graphData) []string {
	var out []string
	for _, r := range g.defogList(map[string]bool{}) {
		if r.kind == dMR {
			out = append(out, r.b.ID)
		}
	}
	return out
}

func TestMRSectionRowsAndPlacement(t *testing.T) {
	g := mrGraph("mergeable")
	if got := mrShape(g); !slices.Equal(got, []string{"w1", "w2"}) {
		t.Errorf("rows %v: want one per open non-Draft Checkpoint Watch, nested included, by container", got)
	}
	var keys []string
	for _, r := range g.execList(map[string]bool{}) {
		if r.isSection() {
			keys = append(keys, r.key)
		}
	}
	if i := slices.Index(keys, mrsKey); i < 1 || keys[i-1] != liveKey {
		t.Errorf("sections %v: s:mrs must sit right below s:live", keys)
	}
	if rows := g.execList(map[string]bool{}); !strings.Contains(rows[slices.IndexFunc(rows, func(r eRow) bool { return r.key == mrsKey })].title, "(2)") {
		t.Errorf("header not counted like Live")
	}
}

func TestDefogMRSectionRowsAndPlacement(t *testing.T) {
	g := mrGraph("mergeable")
	if got := dmrShape(g); !slices.Equal(got, []string{"w3", "w4"}) {
		t.Errorf("rows %v: want the Chart's Watches only; an unresolved parent shows on neither tab", got)
	}
	if slices.Contains(mrShape(g), "wo") {
		t.Error("a Watch whose parent is missing shows on Execute")
	}
	rows := g.defogList(map[string]bool{})
	var keys []string
	for _, r := range rows {
		if r.kind == dSec {
			keys = append(keys, r.key)
		}
	}
	if i := slices.Index(keys, mrsKey); i < 1 || keys[i-1] != liveKey || keys[i+1] != chartsKey {
		t.Errorf("sections %v: s:mrs must sit between s:live and s:charts", keys)
	}
	if i := slices.IndexFunc(rows, func(r dRow) bool { return r.key == mrsKey }); !strings.Contains(rows[i].title, "Merge Requests (2)") {
		t.Errorf("header %q", rows[i].title)
	}
	if rows := g.defogList(map[string]bool{mrsKey: true}); slices.ContainsFunc(rows, func(r dRow) bool { return r.kind == dMR }) {
		t.Error("flipping s:mrs should fold it")
	}
}

func TestDefogMRSectionGate(t *testing.T) {
	g := mrGraph("mergeable")
	g.landName = "local"
	if slices.ContainsFunc(g.defogList(nil), func(r dRow) bool { return r.key == mrsKey }) {
		t.Error("local shows no section")
	}
	g.landName, g.landErr = "", errors.New(`land_strategy "x" in c.toml is unusable`)
	rows := g.defogList(map[string]bool{})
	i := slices.IndexFunc(rows, func(r dRow) bool { return r.key == mrsKey })
	if i < 0 || rows[i+1].kind != dPlace || !strings.Contains(rows[i+1].title, "unusable") || len(dmrShape(g)) != 0 {
		t.Errorf("an error shows the section with one placeholder row carrying the text")
	}
	g = graphData{landName: git.LandStrategyGitlab}
	rows = g.defogList(map[string]bool{})
	i = slices.IndexFunc(rows, func(r dRow) bool { return r.key == mrsKey })
	if i < 0 || rows[i+1].key != mrsNone || rows[i+1].title != "" {
		t.Errorf("gitlab with no Watch shows the none placeholder")
	}
}

func TestDefogMRSectionFoldPersists(t *testing.T) {
	g := mrGraph("mergeable")
	flip := map[string]bool{mrsKey: true}
	tab := defogTab{g: g, flip: flip, path: t.TempDir() + "/folds"}
	if err := tab.save(); err != nil {
		t.Fatal(err)
	}
	if got := loadFolds(tab.path, "defog"); !got[mrsKey] {
		t.Errorf("s:mrs fold lost across a restart: %v", got)
	}
}

func TestExecuteListsNoChartLandPhase(t *testing.T) {
	g := mrGraph("mergeable")
	g.beads = append(g.beads,
		dbead("cl", contract.KindLanding, "open", "ch", 1, described("MR: !11")),
		dbead("fr", contract.KindForgeReview, "open", "ch", 2),
	)
	for _, r := range g.execList(map[string]bool{}) {
		if r.b != nil && (r.b.ID == "w3" || r.b.ID == "w4" || r.b.ID == "cl" || r.b.ID == "fr") {
			t.Errorf("Execute row %s (%s) is a Chart's land phase", r.key, r.b.ID)
		}
	}
}

func TestMRSectionFoldsAndNeverPrunes(t *testing.T) {
	g := mrGraph("mergeable")
	rows := g.execList(map[string]bool{mrsKey: true})
	if slices.ContainsFunc(rows, func(r eRow) bool { return r.kind == eMR }) {
		t.Error("flipping s:mrs should fold it")
	}
	if !isSectionKey(mrsKey) {
		t.Error("s:mrs is unfolded by default like its siblings")
	}
}

func TestMRSectionGate(t *testing.T) {
	g := mrGraph("mergeable")
	g.landName = "local"
	if got := mrShape(g); got != nil || slices.ContainsFunc(g.execList(nil), func(r eRow) bool { return r.key == mrsKey }) {
		t.Error("local shows no section")
	}
	g.landName, g.landErr = "", errors.New(`land_strategy "x" in c.toml is unusable`)
	rows := g.execList(map[string]bool{})
	i := slices.IndexFunc(rows, func(r eRow) bool { return r.key == mrsKey })
	if i < 0 || rows[i+1].kind != ePlace || !strings.Contains(rows[i+1].title, "unusable") || len(mrShape(g)) != 0 {
		t.Errorf("an error shows the section with one placeholder row carrying the text: %v", eshape(rows))
	}
	g = graphData{landName: git.LandStrategyGitlab}
	rows = g.execList(map[string]bool{})
	i = slices.IndexFunc(rows, func(r eRow) bool { return r.key == mrsKey })
	if i < 0 || rows[i+1].key != mrsNone {
		t.Errorf("gitlab with no Watch shows the none placeholder")
	}
}

func TestMRRowText(t *testing.T) {
	text := func(g graphData, id string) string {
		tab := execTab{g: g}
		for _, r := range g.execList(map[string]bool{}) {
			if r.kind == eMR && r.b.ID == id {
				l, _ := tab.line(r, 0, nil, "")
				return ansi.Strip(l)
			}
		}
		t.Fatalf("no row %s", id)
		return ""
	}
	g := mrGraph("mergeable")
	if l := text(g, "w1"); !strings.Contains(l, "!7") || !strings.Contains(l, "t cp") || !strings.Contains(l, "awaiting merge") {
		t.Errorf("w1 line %q: want ref, parent title, status word", l)
	}
	if l := text(g, "w2"); !strings.Contains(l, "web!9") {
		t.Errorf("nested line %q: want context-qualified ref", l)
	}
	dtext := func(g graphData, id string) string {
		tab := defogTab{g: g}
		for _, r := range g.defogList(map[string]bool{}) {
			if r.kind == dMR && r.b.ID == id {
				l, _ := tab.line(r, 0, "", "", nil)
				return ansi.Strip(l)
			}
		}
		t.Fatalf("no Defog row %s", id)
		return ""
	}
	if l := dtext(g, "w4"); strings.Contains(l, "!") || !strings.Contains(l, "t ch") {
		t.Errorf("link-less Chart Watch line %q should still list", l)
	}
	if l := dtext(g, "w3"); !strings.Contains(l, "!11") {
		t.Errorf("Chart Watch line %q: want its ref", l)
	}
	if l := text(g, "w2"); strings.Contains(l, "!") && !strings.Contains(l, "web!9") {
		t.Errorf("Checkpoint line %q", l)
	}
	g.beads[3].Labels = append(g.beads[3].Labels, "paused:manual")
	if l := text(g, "w1"); strings.Contains(l, "awaiting merge") {
		t.Errorf("paused Landing's status must be left out: %q", l)
	}
	g = mrGraph("weird_status")
	if l := text(g, "w1"); strings.Contains(l, "weird") {
		t.Errorf("unmapped status shown: %q", l)
	}
}

// An outliving Watch (its container closed) swaps the Landing status word for closing out and
// the nonzero counts from its own Close-outs lines.
func TestMRRowTextClosingOut(t *testing.T) {
	for body, want := range map[string]string{
		"MR: !7\n":                    "closing out",
		"MR: !7\nClose-outs due: 2\n": "closing out · 2 due",
		"MR: !7\nClose-outs failing: 1: reply: boom\n":          "closing out · 1 failing",
		"MR: !7\nClose-outs due: 2\nClose-outs failing: 1: x\n": "closing out · 2 due · 1 failing",
		"MR: !7\nClose-outs due: 0\nClose-outs failing: 0: \n":  "closing out",
	} {
		g := mrGraph("mergeable")
		g.beads[0].Status = "closed" // cp
		g.beads[4].Description = body
		l := ansi.Strip(g.mrLine(&g.beads[4], &g.beads[0]))
		if !strings.HasSuffix(l, " · "+want) || strings.Contains(l, "awaiting merge") {
			t.Errorf("%q: line %q, want suffix %q and no Landing word", body, l, want)
		}
	}
}

func TestMRDetailShowsLinkOnlyFromLine(t *testing.T) {
	g := mrGraph("mergeable")
	tab := execTab{g: g}
	for _, r := range g.execList(map[string]bool{}) {
		if r.kind != eMR {
			continue
		}
		d := ansi.Strip(tab.rowText(r))
		has := strings.Contains(d, "MR link  ") && strings.Contains(d, "https://gl.example/g/p/-/merge_requests/7")
		if (r.b.ID == "w1") != has {
			t.Errorf("%s detail link field = %v:\n%s", r.b.ID, has, d)
		}
	}
	dt := defogTab{g: g}
	for _, r := range g.defogList(map[string]bool{}) {
		if r.kind != dMR {
			continue
		}
		d := ansi.Strip(dt.rowText(r))
		if strings.Contains(d, "MR link  ") {
			t.Errorf("%s has no MR link: line, so no field:\n%s", r.b.ID, d)
		}
	}
	g.beads[g.beadIndex("w3")].Description = "MR: !11\nMR link: https://gl.example/g/p/-/merge_requests/11"
	for _, r := range g.defogList(map[string]bool{}) {
		if r.kind == dMR && r.b.ID == "w3" && (!strings.Contains(ansi.Strip(dt.rowText(r)), "MR link  ") || !strings.Contains(ansi.Strip(dt.rowText(r)), "https://gl.example/g/p/-/merge_requests/11")) {
			t.Errorf("w3 detail lacks the link field:\n%s", ansi.Strip(dt.rowText(r)))
		}
	}
	cp := g.beadByID("cp")
	if d := ansi.Strip(tab.rowText(eRow{kind: eCPS, b: cp, cp: cp})); strings.Contains(d, "gl.example") {
		t.Error("Checkpoint row stays URL-free")
	}
	if g.rollup(cp).Why+g.rollup(cp).label() == "" || strings.Contains(g.rollup(cp).Why+g.rollup(cp).label(), "gl.example") {
		t.Error("rollup stays URL-free")
	}
}

func TestLandStrategyReadOnSlowTierOnly(t *testing.T) {
	open, all := []bd.Bead{{ID: "a", Status: "open"}}, []bd.Bead{{ID: "a", Status: "open"}}
	tierStubs(t, &open, &all)
	slowReads(t)
	name := git.LandStrategyGitlab
	var err error
	seam(t, &readLandStrategy, func() (string, error) { return name, err })
	m := sized(t, 120, 30)
	m = runLoad(t, m, m.loadSlow())
	if m.landName != git.LandStrategyGitlab || m.gateData().landName != git.LandStrategyGitlab {
		t.Fatalf("slow load: %q", m.landName)
	}
	name, err = "local", errors.New("boom")
	m = runLoad(t, m, m.load())
	if m.landName != git.LandStrategyGitlab || m.landErr != nil {
		t.Errorf("a fast load overwrote the cached value: %q %v", m.landName, m.landErr)
	}
	m = runLoad(t, m, m.loadSlow())
	if m.landErr == nil || m.gateData().landErr == nil {
		t.Errorf("slow load did not carry the error")
	}
}
