package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

func causedBy(id string) func(*bd.Bead) { return func(b *bd.Bead) { b.CausedBy = id } }

func TestSpecOfCP(t *testing.T) {
	cp := contract.KindCheckpoint
	g := graphData{beads: []bd.Bead{
		dbead("spec", contract.KindSpec, "open", "", 1),
		dbead("c1", cp, "closed", "", 1, causedBy("spec")),
		dbead("rb", contract.KindRollback, "open", "", 1, func(b *bd.Bead) { b.Related = []string{"c1"} }),
		dbead("c2", cp, "open", "", 1, causedBy("rb")),
		dbead("sig", contract.KindSignal, "closed", "", 1),
		dbead("c3", cp, "open", "", 1, causedBy("sig")),
		dbead("c4", cp, "open", "", 1, causedBy("gone")),
		dbead("c5", cp, "open", "", 1),
	}}
	for id, want := range map[string]string{"c1": "spec", "c2": "spec", "c3": "", "c4": "", "c5": ""} {
		got := ""
		if s := g.specOfCP(g.beadByID(id)); s != nil {
			got = s.ID
		}
		if got != want {
			t.Errorf("specOfCP(%s) = %q, want %q", id, got, want)
		}
	}
}

func execDetailFixture() []bd.Bead {
	return append(execFixture(),
		dbead("spec", contract.KindSpec, "closed", "", 1),
		dbead("rb", contract.KindRollback, "open", "", 1, func(b *bd.Bead) { b.Related = []string{"cpA"} }),
		dbead("cpS", contract.KindCheckpoint, "open", "i1", 1, isContainer, causedBy("rb")),
	)
}

func TestExecDetail(t *testing.T) {
	beads := execDetailFixture()
	for i := range beads {
		if beads[i].ID == "cpA" {
			beads[i].CausedBy = "spec"
			beads[i].Description = "cp body"
		}
		if beads[i].ID == "sp" {
			beads[i].Description = "spec body"
		}
	}
	m, _ := execModel(t, beads)
	m = unfolded(t, m)
	for _, c := range []struct {
		key  string
		want []string
	}{
		{"cpA", []string{"status", "members", "before land", "tb", "after land", "ta", "spec", "spec ("}},
		{"cpS", []string{"spec ("}}, // a Rollback successor shows the rolled-back Checkpoint's Spec
		{"cs", []string{"spec        none", "gates", "pg"}},
		{"sp", []string{"specified — no Checkpoint cut", "spec body", "enter"}},
		{"sp2", []string{"blocked by", "sp"}},
		{"ap", []string{"state", "checkpoint", "member"}},
		{"side:tb", []string{"before land"}},
		{"side:pg", []string{"gates"}},
		{"i:i1", []string{"enter does nothing"}},
		{"cpA/closed", []string{"closed under cpA", "cl"}},
	} {
		n := execAt(t, m, c.key)
		d := ansi.Strip(n.tabAt(tabExecute).(execTab).detailText())
		for _, w := range c.want {
			if !strings.Contains(d, w) {
				t.Errorf("%s detail lacks %q:\n%s", c.key, w, d)
			}
		}
	}
}

func TestExecDetailEnterLineMatchesPress(t *testing.T) {
	m, _ := execModel(t, execFixture())
	m = unfolded(t, m)
	for _, k := range []string{"blk", "pz", "ap", "sp", "sp2", "cpA", "cl"} {
		tab := execAt(t, m, k).tabAt(tabExecute).(execTab)
		r, _ := tab.selRow()
		want := tab.g.rowEnterField(r.b, r.leaf())
		if d := ansi.Strip(tab.detailText()); !strings.Contains(d, want) {
			t.Errorf("%s: detail lacks enter line %q:\n%s", k, want, d)
		}
	}
}

func TestExecDetailToggleAndHints(t *testing.T) {
	m, _ := execModel(t, execFixture())
	m = execAt(t, unfolded(t, m), "ap")
	m = press(t, m, runeKey("d"))
	if !m.tabAt(tabExecute).(execTab).open {
		t.Fatal("d did not open the detail")
	}
	var hs []string
	for _, b := range m.tabAt(tabExecute).(execTab).bindings() {
		hs = append(hs, b.Help().Desc)
	}
	if j := strings.Join(hs, ","); !strings.Contains(j, "close") || !strings.Contains(j, "scroll") || strings.Contains(j, "detail") {
		t.Errorf("open hints = %s", j)
	}
	m = press(t, m, runeKey("d"))
	if m.tabAt(tabExecute).(execTab).open {
		t.Error("d did not shut the detail")
	}
}

// The first line of an Execute detail is the bead's SBRN with the dot slot, an Initiative group's included.
func TestExecDetailHeadingIsSBRN(t *testing.T) {
	m, _ := execModel(t, execDetailFixture())
	m = unfolded(t, m)
	for key, want := range map[string]string{"i:i1": "○·  INITIATIVE  P1 · t i1 · i1", "ap": "○· APPROVAL P1 · t ap · ap"} {
		d := ansi.Strip(execAt(t, m, key).tabAt(tabExecute).(execTab).detailText())
		if first, _, _ := strings.Cut(d, "\n"); first != want {
			t.Errorf("%s first line %q, want %q", key, first, want)
		}
	}
}
