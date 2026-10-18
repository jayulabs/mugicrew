package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

const mdDesc = "## What to build\n\n" +
	"Ship the **renderer** with `code` and a long enough paragraph that it has to wrap inside the drawer: one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen sixteen seventeen eighteen nineteen twenty.\n\n" +
	"- a bullet\n  - nested bullet\n"

func mdBeads() []bd.Bead {
	return []bd.Bead{
		{ID: "ini", Kind: contract.KindInitiative, Container: true, Status: "open", Title: "Ship v2", Description: "## Done when\n- done\n\n" + mdDesc, Created: epoch},
		{ID: "md", Kind: contract.KindBuild, Status: "open", Title: "Markdown build", Description: mdDesc, Created: epoch},
		{ID: "plain", Kind: contract.KindBuild, Status: "open", Title: "Markdown build", Created: epoch},
	}
}

func TestDrawerContentWidthIsDrawerVPWidth(t *testing.T) {
	for _, w := range []int{1, 2, 40, 120} {
		if got := drawerVP(true, w, 20, "x", 0).Width; got != drawerContentW(w) {
			t.Errorf("w=%d: drawerVP width %d, drawerContentW %d", w, got, drawerContentW(w))
		}
	}
	if mdBody("", 80) != "" {
		t.Error("an empty body should render empty")
	}
}

func TestGraphDetailRendersBodyAsMarkdown(t *testing.T) {
	g := graphTab{graphData: graphData{beads: mdBeads(), w: 100, h: 30}, sel: "md"}
	got := ansi.Strip(g.detailText())
	if !strings.Contains(got, "What to build") || strings.Contains(got, "##") {
		t.Errorf("heading not rendered:\n%s", got)
	}
	// the header field block is unchanged: the same bead without a body shares its prefix
	g.sel = "plain"
	head := ansi.Strip(g.detailText())
	head = head[:strings.Index(head, "age")]
	g.sel = "md"
	if !strings.HasPrefix(ansi.Strip(g.detailText()), strings.Replace(head, "plain", "md", 1)) {
		t.Errorf("header block changed:\n%s", got)
	}
	for _, l := range strings.Split(g.detailText(), "\n") {
		if ansi.StringWidth(l) > 98 {
			t.Errorf("line wider than the drawer: %q", l)
		}
	}
}

func TestDrawerVPKeepsRenderedLineCount(t *testing.T) {
	for _, w := range []int{40, 61, 100} {
		text := mdBody(mdDesc, w)
		n := len(strings.Split(text, "\n"))
		if got := drawerVP(true, w, 40, text, 0).TotalLineCount(); got != n {
			t.Errorf("w=%d: drawerVP re-wrapped %d lines into %d", w, n, got)
		}
	}
}

func TestInitiativesDrawerRendersAtRealWidth(t *testing.T) {
	m := initsModel(t, mdBeads(), nil)
	m = press(t, press(t, m, runeKey("j")), runeKey("d")) // onto the row
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "What to build") || strings.Contains(v, "## What") {
		t.Errorf("Initiatives drawer not rendered:\n%s", v)
	}
	if !strings.Contains(v, "one two three four") { // wrapped at the drawer width, not 1
		t.Errorf("prose wrapped too narrowly:\n%s", v)
	}
}

func defogDetail(t *testing.T, beads []bd.Bead, id string) string {
	t.Helper()
	m := fresh(t, "2", beads, nil)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 160, Height: 60})
	m = treesOpen(m)
	return ansi.Strip(defogAt(t, m, id).tabAt(tabDefog).(defogTab).detailText())
}

func TestChartSectionsKeepBlankLines(t *testing.T) {
	secs, _ := chartSections("preamble\n## Destination\n\none\n\ntwo\n## Notes\nx\n")
	if got := strings.Join(secs["destination"], "|"); got != "|one||two" {
		t.Errorf("destination lines = %q", got)
	}
}

func TestDefogDetailRendersBodiesAsMarkdown(t *testing.T) {
	chart := "## Destination\n\nfirst para\n\nsecond para\n\n## Not-yet-specified\n\nfog one\n\nfog two\n\n## Notes\n\nn1\n\nn2\n"
	beads := []bd.Bead{
		{ID: "ini", Kind: contract.KindInitiative, Container: true, Status: "open", Title: "Ship v2", Description: "## Done when\n- done", Created: epoch},
		{ID: "c", Kind: contract.KindChart, Container: true, Status: "open", Title: "chart", Parent: "ini", Description: chart, Created: epoch},
		{ID: "r", Kind: contract.KindGrilling, Status: "open", Title: "ready", Parent: "c", Description: "## What to build\nbody", Created: epoch},
	}
	d := defogDetail(t, beads, "c")
	for _, want := range []string{"fog 2 ·", "Notes: 2 lines"} {
		if !strings.Contains(d, want) {
			t.Errorf("chart detail lacks %q:\n%s", want, d)
		}
	}
	if !regexp.MustCompile(`first para\s*\n\s*\n\s*second para`).MatchString(d) {
		t.Errorf("paragraphs not separated:\n%s", d)
	}
	if d := defogDetail(t, beads, "r"); !strings.Contains(d, "What to build") || strings.Contains(d, "##") {
		t.Errorf("bead body not rendered:\n%s", d)
	}
	if d := defogDetail(t, beads, "i:ini"); !strings.Contains(d, "enter does nothing on an Initiative row (space folds).") || !strings.Contains(d, "Done when") || strings.Contains(d, "##") {
		t.Errorf("initiative body not rendered:\n%s", d)
	}
}

func TestExecuteDetailRendersBodyAsMarkdown(t *testing.T) {
	bs := mdBeads()
	tab := execTab{g: graphData{beads: bs, w: 100, h: 30}}
	got := ansi.Strip(tab.rowText(eRow{kind: eMem, b: &bs[1]}))
	if !strings.Contains(got, "What to build") || strings.Contains(got, "##") {
		t.Errorf("bead body not rendered:\n%s", got)
	}
	got = ansi.Strip(tab.rowText(eRow{kind: eSec, key: "i:ini", title: "Ship v2", init: &bs[0]}))
	if !strings.Contains(got, "Done when") || strings.Contains(got, "##") {
		t.Errorf("Initiative body not rendered:\n%s", got)
	}
	if !strings.Contains(got, "enter does nothing on an Initiative row (space folds).\n\n") {
		t.Errorf("fixed line changed:\n%s", got)
	}
}

func TestIdeateDetailRendersBodyAsMarkdown(t *testing.T) {
	bs := mdBeads()
	tab := ideaTab{ideaData: ideaData{beads: bs, gate: graphData{beads: bs, w: 100}, w: 100, h: 30}}
	got := ansi.Strip(tab.beadText(&bs[1]))
	if !strings.Contains(got, "What to build") || strings.Contains(got, "##") {
		t.Errorf("Ideate body not rendered:\n%s", got)
	}
	if !strings.HasPrefix(got, "○· BUILD P0 · Markdown build · md\n") || !strings.Contains(got, "\nlabels      —\n\n── ROLLUP · all retained log ─") {
		t.Errorf("header block changed:\n%s", got)
	}
}
