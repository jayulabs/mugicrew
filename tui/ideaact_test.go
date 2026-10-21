package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/session"
)

func actKeys(m Model) string {
	var ks []string
	for _, r := range m.tabAt(tabIdeate).(ideaTab).gate.ideaAct() {
		ks = append(ks, r.key)
	}
	return strings.Join(ks, ",")
}

func TestIdeateActionableSignalBands(t *testing.T) {
	turn := session.Info{State: session.Alive, Waiting: session.WaitTurn}
	ask := session.Info{State: session.Alive, Waiting: session.WaitAsk}
	bs := []bd.Bead{
		sig("ready1", "open", 1, ""), sig("ready2", "open", 2, ""), sig("p3", "open", 3, ""),
		sig("turn", "in_progress", 2, ""), sig("ask", "in_progress", 1, ""), sig("live", "in_progress", 0, ""),
		sig("gone", "in_progress", 0, ""), sig("exit", "in_progress", 0, ""), sig("p3turn", "in_progress", 3, ""),
	}
	sess := map[string]session.Info{"turn": turn, "ask": ask, "live": {State: session.Alive}, "exit": {State: session.Exited}, "p3turn": turn}
	m, _ := drainedModel(t, bs, sess)
	// waiting first (beadCmp: priority), then ready; live, exited, P3 and Backlog out
	if got, want := actKeys(m), "act:ask,act:turn,act:p3turn,act:ready1,act:ready2"; got != want {
		t.Errorf("act %q, want %q", got, want)
	}
	if n := m.tabAt(tabIdeate).count(); n != 3 {
		t.Errorf("waiting count %d, want 3", n)
	}
	if lbl := ansi.Strip(tabLabel(tabIdeate, m.tabAt(tabIdeate))); lbl != "1 Ideate ●3" {
		t.Errorf("label %q", lbl)
	}
}

func TestIdeateActionableInitiativeTable(t *testing.T) {
	turn := session.Info{State: session.Alive, Waiting: session.WaitTurn}
	for _, c := range []struct {
		name    string
		drained bool
		sess    session.Info
		want    string
	}{
		{"alive waiting, drained", true, turn, "act:i"},
		{"alive waiting, not drained", false, turn, "act:i"},
		{"alive working, drained", true, session.Info{State: session.Alive}, ""},
		{"alive working, not drained", false, session.Info{State: session.Alive}, ""},
		{"none, drained", true, session.Info{}, "act:i"},
		{"exited, drained", true, session.Info{State: session.Exited}, "act:i"},
		{"exited, not drained", false, session.Info{State: session.Exited}, ""},
		{"none, not drained", false, session.Info{}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			bs := []bd.Bead{ini("i", "open", "Init", 1)}
			if !c.drained {
				bs = append(bs, bd.Bead{ID: "mem", Kind: "build", Status: "open", Parent: "i", Created: epoch})
			}
			m, _ := drainedModel(t, bs, map[string]session.Info{"i": c.sess})
			if got := actKeys(m); got != c.want {
				t.Errorf("act %q, want %q", got, c.want)
			}
		})
	}
	// a parked (P3) Initiative is Actionable only in the waiting band (ADR 0314 §2)
	for _, c := range []struct {
		name    string
		drained bool
		sess    session.Info
		want    string
	}{
		{"P3 alive waiting, drained", true, turn, "act:i"},
		{"P3 alive waiting, not drained", false, turn, "act:i"},
		{"P3 alive working", true, session.Info{State: session.Alive}, ""},
		{"P3 none, drained", true, session.Info{}, ""},
		{"P3 exited, drained", true, session.Info{State: session.Exited}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			bs := []bd.Bead{ini("i", "open", "Init", 3)}
			if !c.drained {
				bs = append(bs, bd.Bead{ID: "mem", Kind: "build", Status: "open", Parent: "i", Created: epoch})
			}
			m, _ := drainedModel(t, bs, map[string]session.Info{"i": c.sess})
			if got := actKeys(m); got != c.want {
				t.Errorf("act %q, want %q", got, c.want)
			}
		})
	}
	// an Initiative and a Signal merge under beadCmp within the frontier band
	m, _ := drainedModel(t, []bd.Bead{ini("i", "open", "Init", 1), sig("s0", "open", 0, ""), sig("s2", "open", 2, "")}, nil)
	if got, want := actKeys(m), "act:s0,act:i,act:s2"; got != want {
		t.Errorf("merge %q, want %q", got, want)
	}
}

func TestIdeateActionablePlaceholderAndTwin(t *testing.T) {
	m, _ := drainedModel(t, nil, nil)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Actionable (0)") || !strings.Contains(v, "nothing waits on you") {
		t.Errorf("empty:\n%s", v)
	}
	m, _ = drainedModel(t, []bd.Bead{sig("a", "open", 1, "")}, nil)
	var ks []string
	for _, r := range m.tabAt(tabIdeate).(ideaTab).rows() {
		ks = append(ks, r.key)
	}
	if got, want := strings.Join(ks, ","), ",s:act,act:a,s:live,s:live/none,s:tree,s:tree/none,i:,a,"+backlogKey; got != want {
		t.Errorf("rows %q, want %q", got, want)
	}
	// space folds the section; the count stays
	m = press(t, ideaAt(t, m, actKey), runeKey(" "))
	if v := ansi.Strip(m.View()); !strings.Contains(v, "▸ Actionable (1)") {
		t.Errorf("folded:\n%s", v)
	}
}

func TestIdeateCombinedInitiativeRow(t *testing.T) {
	m, _ := drainedModel(t, drainedBeads(), map[string]session.Info{"d": {State: session.Alive, Waiting: session.WaitTurn}})
	var row string
	for _, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if strings.Contains(l, "INITIATIVE") && strings.Contains(l, "all members closed") && strings.Contains(l, "P1 · Drained · d") {
			row = l
		}
	}
	if !strings.Contains(row, "P1 · Drained · d · all members closed") || !strings.Contains(row, "your turn") {
		t.Errorf("combined row %q", row)
	}
	// hints and detail match the drained hint row, enter matches the tree header
	m, calls := drainedModel(t, drainedBeads(), nil)
	m = ideaAt(t, m, "act:d")
	if got, want := hintsOf(m), hintsOf(ideaAt(t, m, "h:d")); got != want {
		t.Errorf("hints %q, want %q", got, want)
	}
	if v := ansi.Strip(press(t, m, runeKey("d")).View()); strings.Contains(v, "ON LAUNCH") {
		t.Errorf("detail shows ON LAUNCH:\n%s", v)
	}
	o, cmd := send(t, m, enterKey())
	settle(t, o, cmd)
	if got := strings.Join(*calls, ";"); !strings.Contains(got, "startSession d") {
		t.Errorf("enter calls %v", *calls)
	}
	// a live cut session attaches
	m, calls = drainedModel(t, drainedBeads(), map[string]session.Info{"d": {State: session.Alive, Waiting: session.WaitTurn}})
	o, cmd = send(t, ideaAt(t, m, "act:d"), enterKey())
	settle(t, o, cmd)
	if !slices.Contains(*calls, "switchSession d") {
		t.Errorf("attach: calls %v", *calls)
	}
	// paused asks y/n
	bs := drainedBeads()
	bs[0].Labels = []string{"paused:manual"}
	m, calls = drainedModel(t, bs, nil)
	o = press(t, ideaAt(t, m, "act:d"), enterKey())
	if len(o.modals) != 1 || len(*calls) != 0 {
		t.Errorf("paused: modals %d calls %v", len(o.modals), *calls)
	}
}

func TestIdeateBadgeActionableTones(t *testing.T) {
	label := func(m Model) string { return ansi.Strip(tabLabel(tabIdeate, m.tabAt(tabIdeate))) }
	// drained, no session: light blue only
	m, _ := drainedModel(t, []bd.Bead{ini("d", "open", "Drained", 1)}, nil)
	if c := m.tabAt(tabIdeate); c.count() != 0 || c.actN() != 1 {
		t.Errorf("count %d actN %d", c.count(), c.actN())
	}
	// a waiting cut session counts amber, folded or not
	m, _ = drainedModel(t, []bd.Bead{ini("d", "open", "Drained", 1)}, map[string]session.Info{"d": {State: session.Alive, Waiting: session.WaitTurn}})
	tab := m.tabAt(tabIdeate).(ideaTab)
	tab.flip = map[string]bool{"i:d": true, actKey: true}
	m.tabs[tabIdeate] = tab
	if got := label(m); got != "1 Ideate ●1" || m.tabAt(tabIdeate).count() != 1 {
		t.Errorf("label %q", got)
	}
}

func TestIdeateSelectionLeavingActionable(t *testing.T) {
	cursor := func(m Model) ideaRow { return m.tabAt(tabIdeate).(ideaTab).cursor() }
	// a ready Signal turns live on refresh: the cursor leaves act:a but not for '+ New Signal'
	m, _ := drainedModel(t, []bd.Bead{sig("a", "open", 1, "")}, nil)
	m = ideaAt(t, m, "act:a")
	m, _ = send(t, m, loadedMsg{beads: []bd.Bead{sig("a", "in_progress", 1, "")}, live: map[string]bool{}, sessions: map[string]session.Info{"a": {State: session.Alive}}})
	if r := cursor(m); r.create {
		t.Errorf("cursor fell to the create row, want the Signal's twin or group header")
	}
	// a combined Initiative row that moves to Live keeps the cursor on its Live row
	m, _ = drainedModel(t, drainedBeads(), nil)
	m = ideaAt(t, m, "act:d")
	m, _ = send(t, m, loadedMsg{beads: drainedBeads(), live: map[string]bool{}, sessions: map[string]session.Info{"d": {State: session.Alive}}})
	if r := cursor(m); r.key != "live:d" {
		t.Errorf("cursor %q, want live:d", r.key)
	}
}

func TestIdeateActionablePausedBand(t *testing.T) {
	turn := session.Info{State: session.Alive, Waiting: session.WaitTurn}
	paused := func(b bd.Bead, l ...string) bd.Bead { b.Labels = l; return b }
	blocked := sig("pblocked", "open", 1, "")
	blocked.Labels, blocked.BlockedBy = []string{"paused:manual"}, []string{"blocker"}
	bs := []bd.Bead{
		sig("ready", "open", 2, ""), paused(sig("pm", "open", 2, ""), "paused:manual"),
		paused(sig("pboth", "open", 1, ""), "paused:manual", "paused:failing"),
		paused(sig("plive", "in_progress", 1, ""), "paused:manual"),
		paused(sig("pturn", "in_progress", 1, ""), "paused:manual"),
		paused(sig("pexit", "in_progress", 1, ""), "paused:manual"),
		blocked, sig("blocker", "open", 4, ""),
		paused(ini("ip", "open", "Paused", 2), "paused:profile"),
		paused(ini("ib", "open", "Blocked", 2), "paused:manual"),
		paused(ini("il", "open", "Live", 2), "paused:manual"),
		{ID: "mem", Kind: "build", Status: "open", Parent: "ip", Created: epoch},
		{ID: "memb", Kind: "build", Status: "open", Parent: "ib", Created: epoch},
		{ID: "meml", Kind: "build", Status: "open", Parent: "il", Created: epoch},
	}
	for _, c := range []string{"paused:manual", "paused:failing", "paused:abandoned", "paused:foreign-hold", "paused:integrate-conflict", "paused:profile", "paused:context", "paused:recovering"} {
		bs = append(bs, paused(sig("c-"+c, "open", 2, ""), c))
	}
	for i := range bs {
		if bs[i].ID == "ib" {
			bs[i].BlockedBy = []string{"blocker"}
		}
	}
	sess := map[string]session.Info{"plive": {State: session.Alive}, "pturn": turn, "pexit": {State: session.Exited}, "il": {State: session.Alive}}
	m, _ := drainedModel(t, bs, sess)
	got := actKeys(m)
	// waiting (pturn), frontier (ready), then paused in beadCmp order; blocked, Live and exited stay out
	if !strings.HasPrefix(got, "act:pturn,act:ready,") {
		t.Errorf("act %q", got)
	}
	rest := strings.Split(strings.TrimPrefix(got, "act:pturn,act:ready,"), ",")
	for _, k := range []string{"act:pboth", "act:ip", "act:pm"} {
		if !slices.Contains(rest, k) {
			t.Errorf("%s missing from paused band %v", k, rest)
		}
	}
	for _, k := range []string{"act:pblocked", "act:ib", "act:il", "act:plive", "act:pexit"} {
		if slices.Contains(rest, k) {
			t.Errorf("%s in Actionable: %v", k, rest)
		}
	}
	if len(rest) != 3+8 {
		t.Errorf("paused band %v, want 11 rows (3 plus each cause)", rest)
	}
	tab := m.tabAt(tabIdeate)
	if tab.actN() != 2+len(rest) || tab.count() != 1 {
		t.Errorf("actN %d count %d", tab.actN(), tab.count())
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Actionable (13)") {
		t.Errorf("header:\n%s", v)
	}

	// inherited-only pause stays out; own + inherited is in
	pi := paused(ini("pi", "open", "Pinit", 1), "paused:manual")
	m, _ = drainedModel(t, []bd.Bead{pi, {ID: "mem", Kind: "build", Status: "open", Parent: "pi", Created: epoch},
		func() bd.Bead { b := sig("inh", "open", 1, ""); b.Parent = "pi"; return b }(),
		func() bd.Bead { b := paused(sig("own", "open", 2, ""), "paused:failing"); b.Parent = "pi"; return b }()}, nil)
	if got := actKeys(m); got != "act:pi,act:own" { // inh (inherited only) is out
		t.Errorf("inherited %q", got)
	}
}

func TestIdeateActionableBackloggedInitiativeGroup(t *testing.T) {
	// an own-paused, otherwise ready P3 Initiative is in no band, and enter still asks y/n
	pi := ini("pi", "open", "Parked", 3)
	pi.Labels = []string{"paused:manual"}
	m, calls := drainedModel(t, []bd.Bead{pi}, nil)
	if got := actKeys(m); got != "" {
		t.Errorf("paused P3 act %q, want none", got)
	}
	m = press(t, ideaAt(t, m, backlogKey), runeKey(" ")) // unfold Backlog
	o := press(t, ideaAt(t, m, "i:pi"), enterKey())
	if len(o.modals) != 1 || len(*calls) != 0 {
		t.Errorf("paused P3 enter: modals %d calls %v", len(o.modals), *calls)
	}
	// a backlogged Initiative's P1 member is in neither the frontier nor the paused band
	member := func(id string, status string, pri int, labels ...string) bd.Bead {
		b := sig(id, status, pri, "pi")
		b.Labels = labels
		return b
	}
	parked := ini("pi", "open", "Parked", 3)
	m, _ = drainedModel(t, []bd.Bead{parked, member("m1", "open", 1), member("m2", "open", 1, "paused:manual")}, nil)
	if got := actKeys(m); got != "" {
		t.Errorf("backlogged members act %q, want none", got)
	}
	// a sibling in triage keeps the group out of Backlog: the P1 member is back in the frontier
	sess := map[string]session.Info{"tri": {State: session.Alive, Waiting: session.WaitTurn}}
	m, _ = drainedModel(t, []bd.Bead{parked, member("m1", "open", 1), member("tri", "in_progress", 3)}, sess)
	if got, want := actKeys(m), "act:tri,act:m1"; got != want {
		t.Errorf("triage sibling act %q, want %q", got, want)
	}
	// badge: the light-blue count drops them and equals the Actionable header; amber is unchanged
	cut := map[string]session.Info{"pi": {State: session.Alive, Waiting: session.WaitTurn}}
	m, _ = drainedModel(t, []bd.Bead{parked, ini("q", "open", "Other", 4), sig("mq", "open", 1, "q"), ini("d", "open", "Drained", 1)}, cut)
	tab := m.tabAt(tabIdeate)
	if tab.actN() != 2 || tab.count() != 1 {
		t.Errorf("actN %d count %d, want 2 and 1", tab.actN(), tab.count())
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Actionable (2)") {
		t.Errorf("header:\n%s", v)
	}
}

// Spec mc-f22wp FR 5: the home-tab pass adds no duplicates, and a Backlog Signal stays out.
func TestIdeateActionableHomePausedBacklog(t *testing.T) {
	paused := func(b bd.Bead) bd.Bead { b.Labels = []string{"paused:manual"}; return b }
	m, _ := drainedModel(t, []bd.Bead{
		paused(sig("p1", "open", 1, "")), paused(sig("p3", "open", 3, "")), paused(sig("p4", "open", 4, "")),
	}, nil)
	if got := actKeys(m); got != "act:p1" {
		t.Errorf("act %q, want act:p1 once", got)
	}
}
