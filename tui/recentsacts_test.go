package tui

import (
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// drain runs cmd and every follow-up it yields (batches included), so a save that
// rides in a tea.Batch reaches the file.
func drain(t *testing.T, m Model, cmd tea.Cmd, depth int) Model {
	t.Helper()
	if cmd == nil || depth == 0 {
		return m
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		for _, c := range b {
			m = drain(t, m, c, depth)
		}
		return m
	}
	m, next := send(t, m, msg)
	return drain(t, m, next, depth-1)
}

func act(t *testing.T, m Model, ks ...tea.KeyMsg) Model {
	t.Helper()
	for _, k := range ks {
		var cmd tea.Cmd
		m, cmd = send(t, m, k)
		m = drain(t, m, cmd, 3)
	}
	return m
}

// Every act Recents counts and every one it must not (FR8–FR11), as one table: a new
// verb that forgets recordRecent is a missing row here. want "" means the seeded top
// stays; created beads are recorded by their own id.
func TestRecentsCountsActsNotRefusals(t *testing.T) {
	typed := func(t *testing.T, m Model, s string) Model { return typeText(t, m, s) }
	graphM := func(t *testing.T) Model {
		stubActions(t)
		m, _ := actModel(t)
		return m
	}
	formM := func(t *testing.T) Model {
		stubActions(t)
		seam(t, &closeBead, func(string, string) error { return nil })
		seam(t, &adoptChain, func(string, string) error { return nil })
		seam(t, &releaseChain, func(string) error { return nil })
		seam(t, &showBead, func(id string) (bd.Bead, error) { b, _ := formModel(t).pinned(id); return b, nil })
		seam(t, &newSignal, func(string, string) (string, error) { return "s9", nil })
		seam(t, &newSurgery, func(string, string, string, bool) (string, string, error) { return "sg", "cp", nil })
		m := formModel(t)
		m.crew = map[string]contract.Crewmate{"franky": fakeCrew{}}
		return m
	}
	y, n := runeKey("y"), runeKey("n")
	for _, c := range []struct {
		name  string
		build func(*testing.T) Model
		id    string // selected first ("" none)
		run   func(*testing.T, Model) Model
		want  string
	}{
		{"enter launches", graphM, "sig", func(t *testing.T, m Model) Model { return act(t, m, enterKey()) }, "sig"},
		{"b launches in the background", graphM, "sig", func(t *testing.T, m Model) Model { return act(t, m, runeKey("b")) }, "sig"},
		{"enter attaches", graphM, "sess", func(t *testing.T, m Model) Model { return act(t, m, enterKey()) }, "sess"},
		{"enter runs", graphM, "res", func(t *testing.T, m Model) Model { return act(t, m, enterKey()) }, "res"},
		{"enter then y launches a blocked bead", graphM, "blk", func(t *testing.T, m Model) Model { return act(t, m, enterKey(), y) }, "blk"},
		{"x then y drops", graphM, "sess", func(t *testing.T, m Model) Model { return act(t, m, runeKey("x"), y) }, "sess"},
		{"x then y aborts", graphM, "live", func(t *testing.T, m Model) Model { return act(t, m, runeKey("x"), y) }, "live"},
		{"x then y recovers", graphM, "dead", func(t *testing.T, m Model) Model { return act(t, m, runeKey("x"), y) }, "dead"},
		{"p pauses", graphM, "sig", func(t *testing.T, m Model) Model { return act(t, m, runeKey("p")) }, "sig"},
		{"+ raises", graphM, "sig", func(t *testing.T, m Model) Model { return act(t, m, runeKey("+")) }, "sig"},
		{"- lowers", graphM, "sig", func(t *testing.T, m Model) Model { return act(t, m, runeKey("-")) }, "sig"},
		{"z rejects", formM, "sig", func(t *testing.T, m Model) Model { return act(t, m, runeKey("z"), enterKey()) }, "sig"},
		{"e edits", formM, "long", func(t *testing.T, m Model) Model { return act(t, typed(t, act(t, m, runeKey("e")), "!"), ctrlS()) }, "long"},
		{"a adopts", formM, "sig", func(t *testing.T, m Model) Model { return act(t, m, runeKey("a"), enterKey()) }, "sig"},
		{"A releases", formM, "ch", func(t *testing.T, m Model) Model { return act(t, m, runeKey("A")) }, "ch"},
		{"n files a Signal: the created bead", formM, "long", func(t *testing.T, m Model) Model {
			return act(t, typed(t, act(t, m, runeKey("n"), runeKey("s")), "T"), ctrlS())
		}, "s9"},
		{"n u files a Surgery: the created bead", formM, "long", func(t *testing.T, m Model) Model {
			return act(t, typed(t, act(t, m, runeKey("n"), runeKey("u")), "Fix"), enterKey())
		}, "sg"},
		{"esc leaves the Initiative focus", func(t *testing.T) Model {
			return ideaFocus(t, recentsModel(t, focusFixture()))
		}, "", func(t *testing.T, m Model) Model { return act(t, m, escKey()) }, "ini"},

		{"gate refusal: x with nothing to drop", graphM, "sig", func(t *testing.T, m Model) Model { return act(t, m, runeKey("x")) }, ""},
		{"gate refusal: x on an Initiative", graphM, "ini", func(t *testing.T, m Model) Model { return act(t, m, runeKey("x")) }, ""},
		{"gate refusal: a second write on a pending bead", graphM, "sig", func(t *testing.T, m Model) Model {
			m.pending = map[string]string{"sig": "updating"}
			return act(t, m, runeKey("+"))
		}, ""},
		{"cancelled confirm: x then n", graphM, "sess", func(t *testing.T, m Model) Model { return act(t, m, runeKey("x"), n) }, ""},
		{"cancelled confirm: x then esc", graphM, "live", func(t *testing.T, m Model) Model { return act(t, m, runeKey("x"), escKey()) }, ""},
		{"cancelled confirm: blocked enter then n", graphM, "blk", func(t *testing.T, m Model) Model { return act(t, m, enterKey(), n) }, ""},
		{"cancelled form: z then esc", formM, "sig", func(t *testing.T, m Model) Model { return act(t, m, runeKey("z"), escKey()) }, ""},
		{"cancelled form: a then esc", formM, "sig", func(t *testing.T, m Model) Model { return act(t, m, runeKey("a"), escKey()) }, ""},
		{"failed create: n", func(t *testing.T) Model {
			m := formM(t)
			seam(t, &newSignal, func(string, string) (string, error) { return "", errTest })
			return m
		}, "long", func(t *testing.T, m Model) Model {
			return act(t, typed(t, act(t, m, runeKey("n"), runeKey("s")), "T"), ctrlS())
		}, ""},
		{"cursor moves", graphM, "sig", func(t *testing.T, m Model) Model { return act(t, m, runeKey("j"), runeKey("k")) }, ""},
		{"d inside a dialog", graphM, "sess", func(t *testing.T, m Model) Model { return act(t, m, runeKey("x"), runeKey("d"), escKey()) }, ""},
		{"the Recents jump", graphM, "sig", func(t *testing.T, m Model) Model {
			return act(t, m, runeKey("R"), runeKey("j"), enterKey())
		}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := recentsFile(t)
			seam(t, &recentsPath, func() string { return f })
			m := c.build(t)
			p := recentsPath() // recentsModel swaps in its own file
			seed := ""
			for _, b := range m.beads { // a bead the snapshot knows, so a save does not prune it
				if b.ID != c.want && b.ID != c.id && b.Kind != contract.KindInitiative {
					seed = b.ID
					break
				}
			}
			if c.id != "" {
				m = selectBead(t, m, c.id)
			}
			seedRecents(t, p, `["`+seed+`"]`)
			c.run(t, m)
			want := c.want
			if want == "" {
				want = seed
			}
			if got := loadRecents(p); len(got) == 0 || got[0] != want || (want != seed && !slices.Contains(got, seed)) {
				t.Errorf("Recents = %v, want %q on top", got, want)
			}
		})
	}
}
