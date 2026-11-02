package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

func inheritBeads(ownPrio, cpPrio int) []bd.Bead {
	return []bd.Bead{
		{ID: "cp", Title: "Urgent work", Status: "open", Kind: contract.KindCheckpoint, Container: true, Priority: cpPrio},
		{ID: "b", Title: "A build", Status: "open", Kind: contract.KindBuild, Priority: ownPrio, Parent: "cp"},
	}
}

const wantInherit = "dispatches as P0 from checkpoint Urgent work"

// Every drawer that renders a bead's header shows where an inherited priority comes from,
// and omits the line when the bead's own priority is effective.
func TestDrawerNamesInheritedPrioritySource(t *testing.T) {
	for _, c := range []struct {
		name     string
		own, cp  int
		wantLine bool
	}{{"ancestor effective", 2, 0, true}, {"own effective", 0, 2, false}, {"tie goes to own", 1, 1, false}} {
		t.Run(c.name, func(t *testing.T) {
			g := graphData{beads: inheritBeads(c.own, c.cp)}
			b := g.beadByID("b")
			texts := map[string]string{
				"defog": defogTab{g: g}.rowText(dRow{kind: dLeaf, key: "b", b: b, chart: g.beadByID("cp")}),
				"exec":  execTab{g: g}.rowText(eRow{kind: eMem, key: "b", b: b, cp: g.beadByID("cp")}),
			}
			for name, txt := range texts {
				if got := strings.Contains(ansi.Strip(txt), "dispatches as"); got != c.wantLine {
					t.Errorf("%s: inherit line present = %v, want %v\n%s", name, got, c.wantLine, ansi.Strip(txt))
				}
				if c.wantLine && !strings.Contains(ansi.Strip(txt), wantInherit) {
					t.Errorf("%s: missing %q in\n%s", name, wantInherit, ansi.Strip(txt))
				}
			}
		})
	}
}

func TestGraphDrawerNamesInheritedPrioritySource(t *testing.T) {
	for _, c := range []struct {
		own, cp  int
		wantLine bool
	}{{2, 0, true}, {0, 2, false}} {
		m := sized(t, 120, 40)
		m, _ = send(t, m, loadedMsg{beads: inheritBeads(c.own, c.cp), live: map[string]bool{}})
		m = selectBead(t, press(t, m, runeKey("4")), "b")
		txt := ansi.Strip(m.tabAt(tabGraph).(graphTab).detailBody())
		if got := strings.Contains(txt, wantInherit); got != c.wantLine {
			t.Errorf("own P%d under P%d: line present = %v, want %v\n%s", c.own, c.cp, got, c.wantLine, txt)
		}
	}
}

// +/- that leaves the effective level unchanged flashes the drawer line and says so in
// the status echo; one that changes the effective level does neither.
func TestBumpPriorityFlashesWhenEffectiveUnchanged(t *testing.T) {
	for _, c := range []struct {
		name     string
		own, d   int
		cp       int
		key      string
		flash    bool
		wantEcho string
	}{
		{"still ancestor-bound", 2, 0, 0, "+", true, "b → P1; still dispatches as P0 from checkpoint Urgent work"},
		{"effective changes", 2, 0, 3, "+", false, "b → P1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			calls := stubActions(t)
			_ = calls
			m := sized(t, 160, 40)
			m, _ = send(t, m, loadedMsg{beads: inheritBeads(c.own, c.cp), live: map[string]bool{}})
			m = selectBead(t, press(t, m, runeKey("4")), "b")
			m, cmd := send(t, m, runeKey(c.key))
			m = settle(t, m, cmd)
			if !strings.Contains(m.status, c.wantEcho) || (!c.flash && strings.Contains(m.status, "still")) {
				t.Errorf("status = %q, want %q", m.status, c.wantEcho)
			}
			if got := m.prioFlash == "b"; got != c.flash {
				t.Errorf("prioFlash = %q, flash want %v", m.prioFlash, c.flash)
			}
			if c.flash {
				m, _ = send(t, m, prioFlashMsg{"b", m.prioFlashSeq - 1}) // a stale press's tick
				if m.prioFlash != "b" {
					t.Errorf("stale tick cleared the flash")
				}
				m, _ = send(t, m, prioFlashMsg{"b", m.prioFlashSeq})
				if m.prioFlash != "" {
					t.Errorf("flash not cleared: %q", m.prioFlash)
				}
			}
		})
	}
}
