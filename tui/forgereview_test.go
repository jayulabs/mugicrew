package tui

import (
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

func forgeReview(id, status, parent string, mut ...func(*bd.Bead)) bd.Bead {
	return dbead(id, contract.KindForgeReview, status, parent, 1, mut...)
}

// The forge review qualifier sits before retrying (ADR 0320 (a-forge-review-surfaces…)); a paused Forge Review reads paused.
func TestRollupForgeReviewQualifier(t *testing.T) {
	cp := func(kids ...bd.Bead) graphData {
		return graphData{
			beads:  append([]bd.Bead{dbead("cp", contract.KindCheckpoint, "open", "", 1, isContainer)}, kids...),
			landEv: map[string]string{"l": "land.failed"},
		}
	}
	land := dbead("l", contract.KindLanding, "open", "cp", 1, landBody("MR: !42\nMR status: discussions_not_resolved"))
	cases := []struct {
		name string
		g    graphData
		want string
	}{
		{"open", cp(land, forgeReview("f", "open", "cp")), "landing · !42 · threads open · forge review · retrying"},
		{"running", cp(land, forgeReview("f", "in_progress", "cp")), "landing · !42 · threads open · forge review · retrying"},
		{"failing", cp(land, forgeReview("f", "open", "cp", labeled("paused:failing"))), "landing · !42 · threads open · forge review paused · retrying"},
		{"manual", cp(land, forgeReview("f", "open", "cp", labeled("paused:manual"))), "landing · !42 · threads open · forge review paused · retrying"},
		{"limit pause is not a hold", cp(land, forgeReview("f", "open", "cp", labeled("paused:limit"))), "landing · !42 · threads open · forge review · retrying"},
		{"closed", cp(land, forgeReview("f", "closed", "cp")), "landing · !42 · threads open · retrying"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.g.rollup(c.g.beadByID("cp")).label(); got != c.want {
				t.Errorf("label = %q, want %q", got, c.want)
			}
		})
	}
}

func TestChartWordForgeReview(t *testing.T) {
	word := func(kids ...bd.Bead) string {
		g := graphData{beads: append([]bd.Bead{dbead("ch", contract.KindChart, "open", "", 1, isContainer)}, kids...)}
		return g.chartWord(g.beadByID("ch")).label()
	}
	if got := word(forgeReview("f", "in_progress", "ch")); strings.Contains(got, "defog") || !strings.HasSuffix(got, "forge review") {
		t.Errorf("open Forge Review: %q, want no defog word and a forge review qualifier", got)
	}
	if got := word(forgeReview("f", "open", "ch", labeled("paused:manual"))); !strings.HasSuffix(got, "forge review paused") {
		t.Errorf("paused Forge Review: %q", got)
	}
	if got := word(forgeReview("f", "closed", "ch")); strings.Contains(got, "forge review") {
		t.Errorf("closed Forge Review: %q", got)
	}
	land := dbead("l", contract.KindLanding, "open", "ch", 1)
	g := graphData{beads: []bd.Bead{dbead("ch", contract.KindChart, "open", "", 1, isContainer), land, forgeReview("f", "open", "ch")}, landEv: map[string]string{"l": "gate.infra"}}
	if got := g.chartWord(g.beadByID("ch")).label(); got != "landing · forge review · retrying" {
		t.Errorf("label = %q", got)
	}
}

func TestForgeReviewTagColourAndHome(t *testing.T) {
	b := forgeReview("f", "open", "ch")
	if _, ok := kindColors[contract.KindForgeReview]; !ok {
		t.Error("no kindColors entry")
	}
	if got := kindTag(&b); !strings.Contains(got, "FORGE-REVIEW") {
		t.Errorf("tag = %q", got)
	}
	chart, cp := dbead("ch", contract.KindChart, "open", "", 1, isContainer), dbead("cp", contract.KindCheckpoint, "open", "", 1, isContainer)
	if got := homeTab(b, []bd.Bead{chart, b}); got != tabDefog {
		t.Errorf("Chart-parented home = %d, want Defog", got)
	}
	b.Parent = "cp"
	if got := homeTab(b, []bd.Bead{cp, b}); got != tabExecute {
		t.Errorf("Checkpoint-parented home = %d, want Execute", got)
	}
}

func TestForgeReviewFields(t *testing.T) {
	body := "MR: !42\nRead through note: 9001\n"
	open := forgeReview("f", "open", "cp", landBody(body))
	closed := forgeReview("g", "closed", "cp", landBody(body), closedWith("filed 2, folded 1, skipped 3: read through note 9001"))
	join := func(b bd.Bead) string {
		var out []string
		for _, f := range forgeReviewFields(&b) {
			out = append(out, f[0]+"="+f[1])
		}
		return strings.Join(out, "|")
	}
	if got, want := join(open), "mr=!42|read through=9001"; got != want {
		t.Errorf("open = %q, want %q", got, want)
	}
	if got, want := join(closed), "mr=!42|read through=9001|counts=filed 2 · folded 1 · skipped 3"; got != want {
		t.Errorf("closed = %q, want %q", got, want)
	}
	if f := forgeReviewFields(&bd.Bead{Kind: contract.KindBuild, Description: body}); f != nil {
		t.Errorf("non-Forge Review fields = %v", f)
	}
}

func TestForgeReviewFieldsCloseOutsOnAContainer(t *testing.T) {
	field := func(body string) string {
		b := bd.Bead{Kind: contract.KindCheckpoint, Description: body}
		for _, f := range forgeReviewFields(&b) {
			if f[0] == "close-outs" {
				return f[1]
			}
		}
		return ""
	}
	for body, want := range map[string]string{
		"Spec: x\n":                                        "",
		"Close-outs due: 2\n":                              "2 due (await push)",
		"Close-outs failing: 1: reply: boom\n":             "(1 failing: reply: boom)",
		"Close-outs due: 2\nClose-outs failing: 1: boom\n": "2 due (await push) (1 failing: boom)",
	} {
		if got := field(body); got != want {
			t.Errorf("%q: close-outs = %q, want %q", body, got, want)
		}
	}
}

func TestForgeReviewFieldsSourceLinesOnAFiledBead(t *testing.T) {
	b := bd.Bead{Kind: contract.KindCritique, Description: "Source: !7 d1 11\n> quoted\n", Notes: "Source: !7 d2 12\nClosed out: d1"}
	got := forgeReviewFields(&b)
	want := [][2]string{{"source", "!7 d1 11"}, {"source", "!7 d2 12"}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("fields = %v, want %v", got, want)
	}
}
