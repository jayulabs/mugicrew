package tui

import (
	"slices"
	"testing"

	"jayulabs.org/mugicrew/contract"
)

// The focus view joins the parity walk beside the other surfaces.
func init() {
	surfaces = append(surfaces, surface{
		name:  "Focus",
		model: focusVerbModel,
		cur: func(m Model) []string {
			var out []string
			for _, r := range m.focusRows() {
				out = append(out, r.key)
			}
			return out
		},
		go_: func(m Model, c string) Model {
			m.focus.sel, m.focus.off = c, 0
			return m
		},
		role: func(m Model, c string) rowRole {
			r, _ := m.focusSel()
			return m.focusRole(r)
		},
	})
}

// focusVerbModel is the focus view on ini with every section's closed rows and the
// Checkpoint unfolded, so the walk meets the header and members in each section; its
// Graph tab shows every status, for the reference.
func focusVerbModel(t *testing.T) (Model, *[]string) {
	t.Helper()
	t.Setenv("BROWSER", "x")
	bs := append(focusFixture(),
		dbead("gdone", contract.KindGrilling, "closed", "c", 1),
		dbead("memdone", contract.KindBuild, "closed", "cp", 1),
	)
	m, calls := focusModel(t, 160, 40, bs, nil, map[string]bool{})
	m = pick(t, press(t, m, runeKey("4")), "s", "any")
	m = ideaFocus(t, m)
	m.focus.flip = map[string]bool{}
	for range 3 { // unfolding a Checkpoint reveals its own '▸ N closed'
		for _, r := range m.focusRows() {
			if r.kind == fSum || r.kind == fBead && r.b.Kind == contract.KindCheckpoint {
				m.focus.flip[r.key] = true
			}
		}
	}
	return m, calls
}

// The focus header offers p, +/- and x; Defog and Execute members offer a/A.
func TestFocusRowsOfferEveryVerb(t *testing.T) {
	m, _ := focusVerbModel(t)
	for _, c := range []struct {
		row   string
		wants []string
	}{{"head", []string{"p", "+", "x"}}, {"c", []string{"a"}}, {"mem", []string{"a"}}} {
		got := verbSet(append(focusRowAt(m, c.row).tabBindings(), focusRowAt(m, c.row).tabExtra()...))
		for _, k := range c.wants {
			if !slices.Contains(got, k) {
				t.Errorf("%s offers %v, want %q", c.row, got, k)
			}
		}
	}
}

func focusRowAt(m Model, key string) Model {
	m.focus.sel, m.focus.off = key, 0
	return m
}
