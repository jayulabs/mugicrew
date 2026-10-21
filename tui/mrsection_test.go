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

func TestMRSectionRowsAndPlacement(t *testing.T) {
	g := mrGraph("mergeable")
	if got := mrShape(g); !slices.Equal(got, []string{"w1", "w2", "w3", "w4"}) {
		t.Errorf("rows %v: want one per open non-Draft Watch, Chart's and nested included, by container", got)
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
	if rows := g.execList(map[string]bool{}); !strings.Contains(rows[slices.IndexFunc(rows, func(r eRow) bool { return r.key == mrsKey })].title, "(4)") {
		t.Errorf("header not counted like Live")
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
	if l := text(g, "w4"); strings.Contains(l, "!") || !strings.Contains(l, "t ch") {
		t.Errorf("link-less Watch line %q should still list", l)
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
