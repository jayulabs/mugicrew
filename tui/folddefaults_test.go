package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/session"
)

// The pre-ADR 0179 defaults, kept so existing tab tests drive an open tree: Initiative
// and Chart headers open; Checkpoints, the Backlog and '▸ N closed' folded.

func ideaOpen(beads []bd.Bead, sess map[string]session.Info) map[string]bool {
	all := expandAll(func(f map[string]bool) []ideaRow { return ideaList(beads, sess, f) },
		func(f map[string]bool, r ideaRow) bool { return r.foldable() && ideaFolded(f, r.key) }, func(r ideaRow) string { return r.key })
	open := map[string]bool{}
	for _, r := range ideaList(beads, sess, all) {
		if r.header() && r.key != backlogKey {
			open[r.key] = true
		}
	}
	return open
}

func defogOpen(g graphData) map[string]bool {
	all := expandAll(g.defogList, func(f map[string]bool, r dRow) bool { return r.foldable() && collapsed(f, r) }, func(r dRow) string { return r.key })
	open := map[string]bool{}
	for _, r := range g.defogList(all) {
		if r.kind == dHead || r.kind == dChart {
			open[r.key] = true
		}
	}
	return open
}

func execOpen(g graphData) map[string]bool {
	open := map[string]bool{}
	for _, r := range g.execList(nil) {
		if r.kind == eSec && r.key != actKey && r.key != liveKey && r.key != specsKey && r.key != cpsKey && r.key != treeKey {
			open[r.key] = true
		}
	}
	return open
}

// xorFlip is delta applied on top of base: the flips a pre-ADR 0179 test meant.
func xorFlip(base, delta map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k := range base {
		out[k] = true
	}
	for k, v := range delta {
		if v == out[k] {
			delete(out, k)
		} else {
			out[k] = !out[k]
		}
	}
	return out
}

// treesOpen opens the trees on the three list tabs of a loaded model.
func treesOpen(m Model) Model {
	g := m.gateData()
	ide := m.tabs[tabIdeate].(ideaTab)
	ide.flip = ideaOpen(m.beads, m.sessions)
	m.tabs[tabIdeate] = ide
	def := m.tabs[tabDefog].(defogTab)
	def.flip = defogOpen(g)
	m.tabs[tabDefog] = def
	ex := m.tabs[tabExecute].(execTab)
	ex.flip = execOpen(g)
	m.tabs[tabExecute] = ex
	return m
}

// fresh is a model on the tab with the default folds (no treesOpen).
func fresh(t *testing.T, tab string, beads []bd.Bead, sess map[string]session.Info) Model {
	t.Helper()
	stubCommonDir(t, t.TempDir())
	m := sized(t, 120, 40)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: sess})
	return press(t, m, runeKey(tab))
}

func screen(m Model) string { return ansi.Strip(m.View()) }

func TestFreshTabsFoldEveryContainerBelowActionable(t *testing.T) {
	db, ds := defogFixture()
	rows := fresh(t, "2", db, ds).tabAt(tabDefog).(defogTab).rows()
	var got []string
	for _, r := range rows {
		got = append(got, r.key)
	}
	if g := strings.Join(got, ","); g != "s:act,act:turn,act:ready,act:launch,act:paused,act:pc,s:live,live:live,s:charts,charts:pc,charts:c,charts:dc,s:tree,i:ini,i:" {
		t.Errorf("defog rows = %v", got)
	}
	em := fresh(t, "3", execFixture(), execSessions())
	var ek []string
	for _, r := range em.tabAt(tabExecute).(execTab).rows() {
		if r.kind == eSec || r.kind == eCP {
			ek = append(ek, r.key)
		}
	}
	if got := strings.Join(ek, ","); !strings.HasPrefix(got, "s:act,s:live,s:specs,s:cps,s:tree,i:i1,i:i2") || strings.Contains(got, "cpA") {
		t.Errorf("execute headers = %s", got)
	}
	im := fresh(t, "1", mixed(), map[string]session.Info{"mc-long-22": {State: session.Alive}})
	var ik []string
	for _, r := range im.tabAt(tabIdeate).(ideaTab).rows() {
		ik = append(ik, r.key)
	}
	if g := strings.Join(ik, ","); g != ",s:act,act:mc-1,act:loose,s:live,live:mc-long-22,s:tree,i:ini,i:,"+backlogKey {
		t.Errorf("ideate rows = %v", ik)
	}
}

func slicesHas(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func TestFoldedTrailers(t *testing.T) {
	db, ds := defogFixture()
	v := screen(fresh(t, "2", db, ds))
	if !strings.Contains(v, "ini · 10 open · 2 live") {
		t.Errorf("defog head:\n%s", v)
	}
	im := fresh(t, "1", mixed(), map[string]session.Info{"mc-long-22": {State: session.Alive}})
	v = screen(im)
	if !strings.Contains(v, "Ship v2 · ini (2) · 2 open · 1 live") || !strings.Contains(v, "No Initiative (1) · 1 open") {
		t.Errorf("ideate:\n%s", v)
	}
	dr := []bd.Bead{ini("d", "open", "Done", 1), sig("x", "closed", 1, "d")}
	v = screen(fresh(t, "1", dr, nil))
	if !strings.Contains(v, "Done · d · drained") || strings.Contains(v, "0 open") {
		t.Errorf("drained:\n%s", v)
	}
	v = screen(fresh(t, "3", execFixture(), execSessions()))
	for _, want := range []string{"i1 · 10 open", "i2 · 1 open", "No Initiative (2) · 2 open"} {
		if !strings.Contains(v, want) {
			t.Errorf("execute missing %q:\n%s", want, v)
		}
	}
}

// A zero part is dropped, a bare title when both are zero; deferred and event beads
// are not open.
func TestTrailerDropsZeroParts(t *testing.T) {
	g := graphData{sessions: map[string]session.Info{"l": {State: session.Alive}}}
	open, live := sig("o", "open", 1, ""), sig("l", "open", 1, "")
	closed, draft, ev := sig("c", "closed", 1, ""), sig("d", "deferred", 1, ""), sig("e", "open", 1, "")
	ev.Event = true
	for _, c := range []struct {
		hidden []bd.Bead
		want   string
	}{
		{[]bd.Bead{open}, " · 1 open"},
		{[]bd.Bead{live}, " · 1 open · 1 live"},
		{[]bd.Bead{closed, draft, ev}, ""},
	} {
		if got := ansi.Strip(g.trailer(c.hidden, false, false)); got != c.want {
			t.Errorf("trailer(%d beads) = %q, want %q", len(c.hidden), got, c.want)
		}
	}
	if got := ansi.Strip(g.trailer([]bd.Bead{live}, false, true)); got != " · drained · 1 live" {
		t.Errorf("drained = %q", got)
	}
}

// oldIdeaList, oldDefogList and oldExecList read a flip as a delta from the pre-ADR 0179
// defaults, as the existing list tests mean it.
func oldIdeaList(beads []bd.Bead, sess map[string]session.Info, flip map[string]bool) []ideaRow {
	return ideaList(beads, sess, xorFlip(ideaOpen(beads, sess), flip))
}

func oldDefogList(g graphData, flip map[string]bool) []dRow {
	return g.defogList(xorFlip(defogOpen(g), flip))
}

func oldExecList(g graphData, flip map[string]bool) []eRow {
	return g.execList(xorFlip(execOpen(g), flip))
}

// Blocker gutters on folded rows read the fully expanded list: the selected waiting
// leaf's blocker sits inside a folded Chart, and the Initiative head above still marks it.
func TestFoldedDefogGutterSeesHiddenBlocker(t *testing.T) {
	db, ds := defogFixture()
	for i := range db {
		if db[i].ID == "turn" {
			db[i].BlockedBy = []string{"afk"}
		}
	}
	m := fresh(t, "2", db, ds)
	m = press(t, m, runeKey("j")) // act:turn
	if v := screen(m); !strings.Contains(v, "1  ▸ ") {
		t.Errorf("no gutter count on the folded head:\n%s", v)
	}
}

// Badges ignore folds: every container folded counts the same as every one open.
func TestBadgesIgnoreFoldedContainers(t *testing.T) {
	db, ds := defogFixture()
	for tab, name := range map[int]string{tabDefog: "Defog", tabExecute: "Execute", tabIdeate: "Ideate"} {
		beads, sess := db, ds
		switch tab {
		case tabExecute:
			beads, sess = execFixture(), execSessions()
		case tabIdeate:
			beads, sess = mixed(), map[string]session.Info{"mc-long-22": {State: session.Alive, Waiting: session.WaitTurn}}
		}
		m := fresh(t, "1", beads, sess)
		folded := ansi.Strip(tabLabel(tab, m.tabAt(tab)))
		opened := ansi.Strip(tabLabel(tab, treesOpen(m).tabAt(tab)))
		if folded != opened || !strings.Contains(folded, "●") {
			t.Errorf("%s badge folded %q, open %q", name, folded, opened)
		}
	}
}
