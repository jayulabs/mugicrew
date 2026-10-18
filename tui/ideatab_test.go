package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

func sig(id, status string, prio int, parent string) bd.Bead {
	return bd.Bead{ID: id, Kind: contract.KindSignal, Status: status, Title: "sig " + id, Priority: prio, Parent: parent, Created: epoch}
}

func ini(id, status, title string, prio int) bd.Bead {
	return bd.Bead{ID: id, Kind: contract.KindInitiative, Container: true, Status: status, Title: title, Priority: prio, Description: "## Done when\n- done\n", Created: epoch}
}

// shape renders ideaList rows as "+", "# header" and the Signal id.
func shape(rs []ideaRow) (out []string) {
	for _, r := range rs {
		switch {
		case r.create:
			out = append(out, "+")
		case r.backlog:
			out = append(out, fmt.Sprintf("backlog %d", r.n))
		case r.header():
			out = append(out, "# "+r.head)
		case r.cue:
			out = append(out, "hint "+r.init.ID)
		case r.place:
			out = append(out, ".")
		case r.sec:
			out = append(out, "# "+r.title)
		default:
			out = append(out, r.b.ID)
		}
	}
	return
}

func TestIdeaList(t *testing.T) {
	alive := session.Info{State: session.Alive}
	exited := session.Info{State: session.Exited}
	ev := sig("ev", "open", 0, "")
	ev.Event = true
	cases := []struct {
		name  string
		beads []bd.Bead
		sess  map[string]session.Info
		want  []string
	}{
		{"empty", nil, nil, []string{"+"}},
		{"drafts hidden, open kept", []bd.Bead{sig("a", "deferred", 0, ""), sig("b", "open", 1, "")}, nil, []string{"+", "# No Initiative", "b"}},
		{"in_progress needs a session", []bd.Bead{sig("a", "in_progress", 1, ""), sig("b", "in_progress", 1, ""), sig("c", "in_progress", 1, "")},
			map[string]session.Info{"a": alive, "b": exited}, []string{"+", "# No Initiative", "a", "b"}},
		{"closed and non-Signals and events out", []bd.Bead{sig("a", "closed", 0, ""), ev, {ID: "x", Kind: contract.KindBuild, Status: "open"}}, nil, []string{"+"}},
		{"initiative groups by beadCmp, closed marked, No Initiative last, drained listed with its hint",
			[]bd.Bead{ini("i2", "open", "Two", 2), ini("i1", "open", "One", 1), ini("i0", "closed", "Zero", 0), ini("ie", "open", "Empty", 0),
				sig("loose", "open", 0, ""), sig("odd", "open", 1, "somechart"), sig("s2", "open", 2, "i2"), sig("s1", "open", 2, "i1"), sig("s0", "open", 0, "i0"), sig("s1b", "open", 1, "i1")},
			nil, []string{"+", "# Empty", "hint ie", "# One", "s1b", "s1", "# Two", "s2", "# Zero", "s0", "# No Initiative", "loose", "odd"}},
		{"triage rows mix by priority", []bd.Bead{sig("a", "open", 2, ""), sig("b", "in_progress", 1, ""), sig("c", "open", 0, "")},
			map[string]session.Info{"b": alive}, []string{"+", "# No Initiative", "c", "b", "a"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shape(oldIdeaList(c.beads, c.sess, nil)); strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func ideaModel(t *testing.T, w, h int, beads []bd.Bead, sess map[string]session.Info) Model {
	return treesOpen(ideaModelFresh(t, w, h, beads, sess))
}

// ideaModelFresh is ideaModel with the default folds.
func ideaModelFresh(t *testing.T, w, h int, beads []bd.Bead, sess map[string]session.Info) Model {
	t.Helper()
	m := sized(t, w, h)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: sess})
	return m
}

func ideaSel(m Model) string { return m.tabAt(tabIdeate).(ideaTab).sel }

func mixed() []bd.Bead {
	a := sig("mc-1", "open", 1, "ini")
	a.Description = "# raw *markdown*\nbody"
	a.Labels = []string{"x"}
	return []bd.Bead{ini("ini", "open", "Ship v2", 1), a, sig("mc-long-22", "in_progress", 2, "ini"), sig("loose", "open", 2, "")}
}

func TestIdeateCursorLandsOnEveryHeader(t *testing.T) {
	m := ideaModel(t, 100, 24, mixed(), map[string]session.Info{"mc-long-22": {State: session.Alive}})
	var seen []string
	for range 12 {
		seen = append(seen, ideaSel(m))
		m = press(t, m, runeKey("j"))
	}
	if got := strings.Join(seen, ","); got != ",s:act,act:mc-1,act:loose,s:live,live:mc-long-22,s:tree,i:ini,mc-1,mc-long-22,i:,loose" {
		t.Errorf("down walk = %q", got)
	}
	if m = press(t, m, tea.KeyMsg{Type: tea.KeyUp}); ideaSel(m) != "loose" {
		t.Errorf("up from create wraps to %q", ideaSel(m))
	}
}

func TestIdeateEmptyState(t *testing.T) {
	m := ideaModel(t, 100, 24, nil, nil)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "no Initiatives") || !strings.Contains(v, "+ New Signal") {
		t.Errorf("empty view:\n%s", v)
	}
}

func TestIdeateRowAnatomy(t *testing.T) {
	m := ideaModel(t, 100, 24, mixed(), map[string]session.Info{"mc-long-22": {State: session.Alive, Waiting: session.WaitTurn}})
	v := ansi.Strip(m.View())
	for _, id := range []string{"mc-1", "mc-long-22", "loose"} {
		l := ideaLine(v, "sig "+id)
		if !strings.Contains(l, "SIGNAL") || !strings.Contains(l, "P") {
			t.Errorf("row lacks SIGNAL tag or priority: %q", l)
		}
	}
	l := ideaLine(v, "sig mc-long-22")
	if strings.Contains(l, "◐ triage") || !strings.Contains(l, "◐") || !strings.Contains(l, "your turn") {
		t.Errorf("in-triage row = %q", l)
	}
	if strings.Index(l, "your turn") < strings.LastIndex(l, "mc-long-22") {
		t.Errorf("waiting word not in the suffix: %q", l)
	}
}

func ideaLine(v, s string) string {
	for _, l := range strings.Split(v, "\n")[chromeH-2:] { // under the tab bar, whose labels carry names too
		if strings.Contains(l, s) {
			return l
		}
	}
	return ""
}

func TestIdeateDetailToggleAndScroll(t *testing.T) {
	bs := mixed()
	bs[1].Description = strings.Repeat("line of raw markdown\n", 40)
	m := ideaModel(t, 100, 30, bs, nil)
	m = ideaAt(t, m, "mc-1")
	m = press(t, m, runeKey("d"))
	v := ansi.Strip(m.View())
	for _, want := range []string{"P1", "Ship v2", "x", "line of raw markdown"} {
		if !strings.Contains(v, want) {
			t.Errorf("detail missing %q:\n%s", want, v)
		}
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	if m.tabAt(tabIdeate).(ideaTab).off == 0 {
		t.Error("pgdn did not scroll")
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.tabAt(tabIdeate).(ideaTab).off != 0 {
		t.Error("pgup did not scroll back")
	}
	m = press(t, m, runeKey("j"))
	if ideaSel(m) == "mc-1" {
		t.Error("j did not move the selection with the pane open")
	}
	m = press(t, m, runeKey("d"))
	if m.tabAt(tabIdeate).(ideaTab).open {
		t.Error("d did not close the pane")
	}
}

func TestIdeateBadgeCountsWaitingTriage(t *testing.T) {
	bs := []bd.Bead{sig("a", "in_progress", 1, ""), sig("b", "in_progress", 1, ""), sig("c", "in_progress", 1, ""), sig("d", "open", 1, ""), sig("e", "in_progress", 1, "")}
	sess := map[string]session.Info{
		"a": {State: session.Alive, Waiting: session.WaitTurn},
		"b": {State: session.Exited, Waiting: session.WaitTurn},
		"c": {State: session.Alive, Waiting: session.WaitAsk},
		"d": {State: session.Alive, Waiting: session.WaitTurn},                                        // open, not in triage
		"e": {State: session.Alive, Waiting: session.WaitTurn, AutoExit: time.Now().Add(time.Minute)}, // armed reads live
	}
	m := ideaModel(t, 100, 24, bs, sess)
	if n := m.tabAt(tabIdeate).count(); n != 2 {
		t.Errorf("count = %d, want 2", n)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "1 Ideate ●2") {
		t.Errorf("badge missing from tab bar:\n%s", v)
	}
}

func TestIdeateBadgeIgnoresBacklog(t *testing.T) {
	m := ideaModel(t, 100, 24, backlogBeads(), map[string]session.Info{"tri": {State: session.Alive, Waiting: session.WaitTurn}})
	if n := m.tabAt(tabIdeate).count(); n != 1 {
		t.Errorf("count = %d, want 1 (the in-triage P4 Signal only)", n)
	}
}

// keysModel is the Ideate tab over mixed() plus a blocked Signal, with stubbed seams.
func keysModel(t *testing.T) (Model, *[]string) {
	t.Helper()
	calls := stubActions(t)
	blk := sig("blk", "open", 2, "")
	blk.BlockedBy = []string{"dep"}
	bs := append(mixed(), blk, bd.Bead{ID: "dep", Kind: contract.KindBuild, Status: "open", Title: "dep", Created: epoch})
	for i := range bs {
		bs[i].Description += strings.Repeat("\n\nbody line", 40) // so the detail pane scrolls
	}
	m := sized(t, 160, 30)
	m.crew = map[string]contract.Crewmate{"luffy": fakeCrew{}}
	sess := map[string]session.Info{"mc-long-22": {State: session.Alive}}
	m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{}, sessions: sess})
	m = treesOpen(m)
	return m, calls
}

// harnessCtx is a Ctx whose Harness is nil: enough for resume's stubbed seam.
type harnessCtx struct{ contract.Ctx }

func (harnessCtx) Harness() contract.Harness { return nil }

func ideaAt(t *testing.T, m Model, id string) Model {
	t.Helper()
	for range 20 {
		if ideaSel(m) == id {
			return m
		}
		m = press(t, m, runeKey("j"))
	}
	t.Fatalf("no row %q; at %q", id, ideaSel(m))
	return m
}

func ideaHints(m Model) string {
	if len(m.modals) > 0 {
		return ""
	}
	return hintsOf(m)
}

func TestIdeateNewAndEditForms(t *testing.T) {
	m, _ := keysModel(t)
	for name, o := range map[string]Model{"enter": press(t, m, enterKey()), "n s": pressN(t, m, "s")} {
		if len(o.modals) != 1 || o.modals[0].kind != form || o.modals[0].sf.id != "" {
			t.Errorf("%s on the create row: modals %v", name, o.modals)
		}
	}
	if o := pressN(t, ideaAt(t, m, "mc-1"), "s"); len(o.modals) != 1 || o.modals[0].sf.id != "" {
		t.Error("n on a Signal does not open an empty form")
	}
	for _, id := range []string{"mc-1", "mc-long-22"} { // the second is in triage
		o := press(t, ideaAt(t, m, id), runeKey("e"))
		if len(o.modals) != 1 || o.modals[0].sf.id != id || o.modals[0].sf.title.text != "sig "+id {
			t.Errorf("e on %s: %v", id, o.modals)
		}
	}
	for _, k := range []string{"e", "z", "a", "A", "+", "-", "u", "t", "p", "x"} { // the create row takes enter, n and b (status only) alone
		if o, cmd := send(t, m, runeKey(k)); cmd != nil || len(o.modals) != 0 || o.status != m.status {
			t.Errorf("%s did something on the create row", k)
		}
	}
}

func TestIdeateEnterTriages(t *testing.T) {
	m, calls := keysModel(t)
	m, cmd := send(t, ideaAt(t, m, "mc-1"), enterKey())
	m = settle(t, m, cmd)
	if !has(*calls, "hitlClaim mc-1") || !has(*calls, "startSession mc-1") || !has(*calls, "switchSession mc-1") {
		t.Errorf("fresh Signal: calls %v", *calls)
	}

	m, calls = keysModel(t) // alive: attach, no claim
	m = press(t, ideaAt(t, m, "mc-long-22"), enterKey())
	if !has(*calls, "switchSession mc-long-22") || has(*calls, "hitlClaim") {
		t.Errorf("alive: %v", *calls)
	}

	m, calls = keysModel(t) // exited: resume in its kept worktree, no claim
	seam(t, &resumeSession, func(b bd.Bead, s session.Info, h contract.Harness) error {
		*calls = append(*calls, "resume "+b.ID)
		return nil
	})
	m.ctx = harnessCtx{}
	m, _ = send(t, m, loadedMsg{beads: m.beads, live: map[string]bool{}, sessions: map[string]session.Info{"mc-1": {State: session.Exited}}})
	m, cmd = send(t, ideaAt(t, m, "mc-1"), enterKey())
	if cmd == nil {
		t.Fatal("exited: no resume command")
	}
	settle(t, m, cmd)
	if !has(*calls, "resume mc-1") || has(*calls, "hitlClaim") {
		t.Errorf("exited: %v", *calls)
	}

	m, calls = keysModel(t) // open blockers ask first
	m = press(t, ideaAt(t, m, "blk"), enterKey())
	if len(m.modals) != 1 || m.modals[0].kind != confirm || has(*calls, "hitlClaim") {
		t.Errorf("blocked: modals %v calls %v", m.modals, *calls)
	}

	m, calls = keysModel(t) // a write in flight refuses
	m.pending = map[string]string{"mc-1": "updating"}
	m = press(t, ideaAt(t, m, "mc-1"), enterKey())
	if s := statusOf(m); !strings.Contains(s, "write in flight") || has(*calls, "hitlClaim") {
		t.Errorf("pending: status %q calls %v", s, *calls)
	}
}

func TestIdeateRejectOnlyOpenUnclaimed(t *testing.T) {
	var id string
	seam(t, &closeBead, func(i, r string) error { id = i; return nil })
	m, _ := keysModel(t)
	o := ideaAt(t, m, "mc-long-22")
	if strings.Contains(ideaHints(o), "reject") {
		t.Errorf("z hinted in triage: %s", ideaHints(o))
	}
	if o = press(t, o, runeKey("z")); len(o.modals) != 0 || !strings.Contains(statusOf(o), "not an open, unclaimed Signal") {
		t.Errorf("z in triage: modals %v status %q", o.modals, statusOf(o))
	}
	o = ideaAt(t, m, "mc-1")
	if !strings.Contains(ideaHints(o), "z reject") {
		t.Errorf("z not hinted: %s", ideaHints(o))
	}
	o = press(t, press(t, o, runeKey("z")), enterKey())
	if id != "mc-1" {
		t.Errorf("rejected %q", id)
	}
}

func TestIdeateSharedActions(t *testing.T) {
	m, calls := keysModel(t)
	m = press(t, ideaAt(t, m, "mc-1"), runeKey("+"))
	if !has(*calls, "updateBead mc-1 [--priority 0]") {
		t.Errorf("+: %v", *calls)
	}
	stubExec(t)
	t.Setenv("EXPLORER", "ls")
	if n := press(t, m, runeKey("E")); len(n.modals) != 0 {
		t.Errorf("E opened a dialog")
	}
	o := press(t, m, runeKey("a"))
	if len(o.modals) != 1 || o.modals[0].kind != picker {
		t.Errorf("a: %v", o.modals)
	}
	var released string
	seam(t, &releaseChain, func(h string) error { released = h; return nil })
	if o = press(t, m, runeKey("A")); released != "mc-1" || len(o.modals) != 0 {
		t.Errorf("A released %q, modals %v", released, o.modals)
	}
}

func TestIdeateHints(t *testing.T) {
	m, _ := keysModel(t)
	want := func(o Model, s string) {
		t.Helper()
		if got := hintsOf(o); got != s {
			t.Errorf("hints\n got %q\nwant %q", got, s)
		}
	}
	want(m, "enter new · J/K section")
	want(ideaAt(t, m, "i:ini"), "space fold · d detail · +/- prio · J/K section")
	want(press(t, ideaAt(t, m, "i:ini"), runeKey("d")), "space fold · d close · pgup/pgdn scroll · +/- prio · J/K section")
	want(ideaAt(t, m, "i:"), "space fold · J/K section")
	o := ideaAt(t, m, "mc-1") // open, unclaimed, in an Initiative
	want(o, "enter triage · b bg · d detail · z reject · p pause · t transcript · e edit · +/- prio · a/A adopt/release · J/K section · r retrospective")
	want(press(t, o, runeKey("d")), "enter triage · b bg · d close · pgup/pgdn scroll · z reject · p pause · t transcript · e edit · +/- prio · a/A adopt/release · J/K section · r retrospective")
	want(ideaAt(t, m, "loose"), "enter triage · b bg · d detail · z reject · p pause · t transcript · e edit · +/- prio · a adopt · J/K section · r retrospective")
	want(ideaAt(t, m, "mc-long-22"), "enter triage · d detail · p pause · x drop · t transcript · e edit · +/- prio · a/A adopt/release · J/K section · r retrospective")
	o = press(t, ideaAt(t, m, "mc-1"), runeKey("?"))
	for _, k := range []string{"enter", "triage", "reject", "adopt/release", "move"} {
		if v := ansi.Strip(o.View()); !strings.Contains(v, k) {
			t.Errorf("? lacks %q", k)
		}
	}
}

func TestIdeateBindingCoverage(t *testing.T) {
	for _, id := range []string{"", "mc-1", "loose", "mc-long-22", "blk"} {
		m, _ := keysModel(t)
		m = ideaAt(t, m, id)
		for _, open := range []bool{false, true} {
			if open && id != "" {
				m = press(t, press(t, m, runeKey("d")), tea.KeyMsg{Type: tea.KeyCtrlD}) // mid-scroll, so up and down both move
			}
			checkCoverage(t, "Ideate "+id, m, true, "1", "J", "K", "ctrl+down", "ctrl+up")
		}
	}
}

func backlogBeads() []bd.Bead {
	return []bd.Bead{ini("i", "open", "Init", 1), sig("n2", "open", 2, "i"), sig("b4", "open", 4, "i"), sig("b3old", "open", 3, ""),
		sig("tri", "in_progress", 4, ""), sig("b3new", "open", 3, "i")}
}

func TestIdeaListBacklog(t *testing.T) {
	bs := backlogBeads()
	bs[3].Created = epoch.Add(-1)
	sess := map[string]session.Info{"tri": {State: session.Alive}}
	if got := strings.Join(shape(oldIdeaList(bs, sess, nil)), ","); got != "+,# Init,n2,# No Initiative,tri,backlog 3" {
		t.Errorf("folded: %s", got)
	}
	rs := oldIdeaList(bs, sess, map[string]bool{backlogKey: true})
	if got := strings.Join(shape(rs), ","); got != "+,# Init,n2,# No Initiative,tri,backlog 3,b3new,b3old,b4" {
		t.Errorf("expanded: %s", got)
	}
	if rs[6].hint != "Init" || rs[7].hint != "" {
		t.Errorf("hints %q %q", rs[6].hint, rs[7].hint)
	}
	if got := strings.Join(shape(oldIdeaList([]bd.Bead{ini("i", "open", "Init", 1), sig("b", "open", 3, "i")}, nil, nil)), ","); got != "+,backlog 1" {
		t.Errorf("all-backlog initiative: %s", got)
	}
	if got := strings.Join(shape(oldIdeaList(bs, sess, map[string]bool{"i:i": true, "i:": true})), ","); got != "+,# Init,# No Initiative,backlog 3" {
		t.Errorf("folded groups are headers alone: %s", got)
	}
}

func TestIdeateFoldWithSpace(t *testing.T) {
	m := ideaModel(t, 100, 24, backlogBeads(), map[string]session.Info{"tri": {State: session.Alive}})
	m = ideaAt(t, m, backlogKey)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "▸ Backlog (3)") || strings.Contains(v, "b4") {
		t.Errorf("folded view:\n%s", v)
	}
	if o := pressN(t, m, "s"); len(o.modals) != 1 {
		t.Error("n on the header does not open the form")
	}
	if o := press(t, m, enterKey()); len(o.modals) != 0 || strings.Contains(ansi.Strip(o.View()), "b4") || !strings.Contains(statusOf(o), "space folds") {
		t.Errorf("enter on the Backlog: modals %v status %q", o.modals, statusOf(o))
	}
	m = press(t, m, runeKey(" "))
	if ideaSel(m) != backlogKey {
		t.Errorf("cursor left the header: %q", ideaSel(m))
	}
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "▾ Backlog (3)") || !strings.Contains(v, "sig b4") || !strings.Contains(v, "· Init") {
		t.Errorf("expanded view:\n%s", v)
	}
	if m = press(t, m, runeKey(" ")); strings.Contains(ansi.Strip(m.View()), "b4") {
		t.Error("space did not refold the Backlog")
	}

	// an Initiative: (N) only while folded; No Initiative's count (N) always shows
	for _, h := range []struct{ key, title string }{{"i:i", "Init "}} {
		m = ideaAt(t, m, h.key)
		if v := ansi.Strip(m.View()); !strings.Contains(v, "▾ ") || strings.Contains(ideaLine(v, h.title), "(") {
			t.Errorf("%s unfolded: %q", h.title, ideaLine(v, h.title))
		}
		m = press(t, m, runeKey(" "))
		if l := ideaLine(ansi.Strip(m.View()), h.title); !strings.Contains(l, "▸") || !strings.Contains(l, "(1)") {
			t.Errorf("%s folded: %q", h.title, l)
		}
		m = press(t, m, runeKey(" "))
		if ideaSel(m) != h.key || strings.Contains(ideaLine(ansi.Strip(m.View()), h.title), "(") {
			t.Errorf("%s refolded wrong: sel %q", h.title, ideaSel(m))
		}
	}
	if l := ideaLine(ansi.Strip(ideaAt(t, m, "i:").View()), noInitiative); !strings.Contains(l, "No Initiative (1)") {
		t.Errorf("No Initiative header lacks its count: %q", l)
	}
	// enter on No Initiative says space folds
	if o := press(t, ideaAt(t, m, "i:"), enterKey()); !strings.Contains(statusOf(o), "space folds") {
		t.Errorf("status %q", statusOf(o))
	}
}

func TestIdeateClosedInitiativeHeader(t *testing.T) {
	m := ideaModel(t, 100, 24, []bd.Bead{ini("z", "closed", "Zed", 1), sig("s", "open", 1, "z")}, nil)
	if l := ideaLine(ansi.Strip(m.View()), "Zed"); !strings.Contains(l, "INITIATIVE") || !strings.Contains(l, "Zed · z (closed)") { // FR15: the mark follows the id
		t.Errorf("header %q", l)
	}
}

func TestIdeateFoldMovesCursorToHeader(t *testing.T) {
	m := ideaModel(t, 100, 24, backlogBeads(), nil)
	m = press(t, ideaAt(t, m, "n2"), runeKey(" ")) // space on a Signal does not fold
	if ideaSel(m) != "n2" {
		t.Fatalf("space moved the cursor to %q", ideaSel(m))
	}
	// fold the selected Signal's group out from under it
	tab := m.tabAt(tabIdeate).(ideaTab)
	tab.flip = map[string]bool{} // Initiative i folded by default
	m.tabs[tabIdeate] = tab
	if got := cursorKey(m); got != "i:i" {
		t.Errorf("fold: cursor on %q", got)
	}
	// a Signal that leaves the list falls back to the create row
	bs := backlogBeads()
	bs[1].Status = "closed"
	m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{}})
	m = treesOpen(m)
	if got := cursorKey(m); got != "" {
		t.Errorf("closed Signal: cursor on %q", got)
	}
}

func TestIdeateBadgeIgnoresFolds(t *testing.T) {
	m := ideaModel(t, 100, 24, mixed(), map[string]session.Info{"mc-long-22": {State: session.Alive, Waiting: session.WaitTurn}})
	tab := m.tabAt(tabIdeate).(ideaTab)
	tab.flip = map[string]bool{"i:ini": true, "i:": true, backlogKey: false}
	m.tabs[tabIdeate] = tab
	if n := m.tabAt(tabIdeate).count(); n != 1 {
		t.Errorf("count = %d with every group folded, want 1", n)
	}
}

func TestIdeateBacklogSignalKeys(t *testing.T) {
	m := ideaModel(t, 100, 24, backlogBeads(), nil)
	m = press(t, press(t, ideaAt(t, m, backlogKey), runeKey(" ")), runeKey("j"))
	if got, want := hintsOf(m), "enter triage · b bg · d detail · z reject · p pause · t transcript · e edit · +/- prio · a adopt · J/K section · r retrospective"; got != want {
		t.Errorf("backlog Signal hints %q, want %q", got, want)
	}
}

// reprio presses key k on the Signal id in the Ideate tab; updateBead is stubbed.
func reprio(t *testing.T, m Model, id string, k tea.KeyMsg) Model {
	t.Helper()
	seam(t, &updateBead, func(string, []string) error { return nil })
	for i := 0; ideaSel(m) != id; i++ {
		if i > 20 {
			t.Fatalf("cannot reach %s", id)
		}
		m = press(t, m, runeKey("j"))
	}
	return press(t, m, k)
}

// cursorKey is the key of the row the cursor is on: "" for the create row.
func cursorKey(m Model) string {
	t := m.tabAt(tabIdeate).(ideaTab)
	return t.cursor().key
}

func TestIdeateReprioritiseKeepsCursor(t *testing.T) {
	model := func(unfold bool) Model {
		m := ideaModel(t, 100, 24, backlogBeads(), nil)
		if unfold {
			m = press(t, ideaAt(t, m, backlogKey), runeKey(" "))
		}
		return m
	}
	for _, c := range []struct {
		name   string
		unfold bool
		id     string
		k      string
		want   string
	}{
		{"raise to P2 lands under its group, which appears folded", true, "b3old", "+", "i:"},
		{"lower into expanded Backlog", true, "n2", "-", "n2"},
		{"lower into folded Backlog", false, "n2", "-", backlogKey},
	} {
		if got := cursorKey(reprio(t, model(c.unfold), c.id, runeKey(c.k))); got != c.want {
			t.Errorf("%s: cursor on %q, want %q", c.name, got, c.want)
		}
	}
	// expanding after a fold-fallback keeps the cursor on the header
	m := press(t, reprio(t, model(false), "n2", runeKey("-")), runeKey(" "))
	if got := cursorKey(m); got != backlogKey || ideaFolded(m.tabAt(tabIdeate).(ideaTab).flip, backlogKey) {
		t.Errorf("expand after fallback: cursor on %q", got)
	}
}

// b is enter's background twin on Idea: it starts or resumes Luffy's triage without
// attaching, asks enter's y/n on a blocked or paused Signal, and is never dead.
func TestIdeateBg(t *testing.T) {
	calls := stubActions(t)
	seam(t, &resumeSession, func(b bd.Bead, s session.Info, h contract.Harness) error {
		*calls = append(*calls, "resume "+b.ID)
		return nil
	})
	blk := sig("blk", "open", 2, "")
	blk.BlockedBy = []string{"dep"}
	pz := sig("pz", "open", 2, "")
	pz.Labels = []string{"paused:manual"}
	bs := append(backlogBeads(), blk, pz, bd.Bead{ID: "dep", Kind: contract.KindBuild, Status: "open", Title: "dep", Created: epoch})
	sess := map[string]session.Info{"tri": {State: session.Alive}, "b3old": {State: session.Exited}}
	m := ideaModel(t, 160, 30, bs, sess)
	m.ctx = harnessCtx{}
	m.crew = map[string]contract.Crewmate{"luffy": fakeCrew{}}
	for _, c := range []struct {
		id, say, call string
		modal         bool
	}{
		{"n2", "started", "startSession n2", false},
		{"blk", "", "", true},
		{"tri", "already running — enter attaches", "", false},
		{"", "nothing to launch here", "", false},
		{backlogKey, "nothing to launch here", "", false},
	} {
		*calls = nil
		n := ideaAt(t, m, c.id)
		n, cmd := send(t, n, runeKey("b"))
		if cmd != nil {
			n = settle(t, n, cmd)
		}
		if (len(n.modals) == 1) != c.modal || !strings.Contains(statusOf(n), c.say) || has(*calls, "switchSession") || (c.call != "" && !has(*calls, c.call)) || (c.call == "" && len(*calls) != 0) {
			t.Errorf("%q: modals %d status %q calls %v", c.id, len(n.modals), statusOf(n), *calls)
		}
	}
	// paused: b asks enter's y/n, launching nothing until confirmed
	*calls = nil
	pb, pcmd := send(t, ideaAt(t, m, "pz"), runeKey("b"))
	if pcmd != nil || len(pb.modals) != 1 || pb.modals[0].title != "Launch paused bead?" || len(*calls) != 0 {
		t.Errorf("paused: cmd %v modals %+v calls %v", pcmd != nil, pb.modals, *calls)
	}
	// a Backlog Signal and an exited session resume or start in the background
	n := press(t, ideaAt(t, m, backlogKey), runeKey(" "))
	for _, id := range []string{"b4", "b3old"} {
		*calls = nil
		o, cmd := send(t, ideaAt(t, n, id), runeKey("b"))
		if cmd != nil {
			o = settle(t, o, cmd)
		}
		if !strings.Contains(statusOf(o), "started") || has(*calls, "switchSession") || len(*calls) == 0 {
			t.Errorf("backlog %s: status %q calls %v", id, statusOf(o), *calls)
		}
	}
	// the bar: b bg right after enter triage, hidden on a live session
	if got, want := hintsOf(ideaAt(t, m, "n2")), "enter triage · b bg · d detail · z reject · p pause · t transcript · e edit · +/- prio · a/A adopt/release · J/K section · r retrospective"; got != want {
		t.Errorf("bar %q, want %q", got, want)
	}
	if got := hintsOf(ideaAt(t, m, "tri")); strings.Contains(got, "b bg") {
		t.Errorf("live bar shows b bg: %q", got)
	}
	for _, id := range []string{"", backlogKey, "tri"} {
		o := ideaAt(t, m, id)
		o.ref = true
		if v := ansi.Strip(o.View()); !strings.Contains(v, "bg launch/resume (HITL)") {
			t.Errorf("? lacks b on %q", id)
		}
	}
}

func TestIdeateX(t *testing.T) {
	m, _ := keysModel(t)
	o := press(t, ideaAt(t, m, "mc-long-22"), runeKey("x"))
	if len(o.modals) != 1 || o.modals[0].title != "Drop session?" {
		t.Errorf("x on a session: %v", o.modals)
	}
	for _, id := range []string{"", "mc-1"} { // create row; Signal without a session
		o = press(t, ideaAt(t, m, id), runeKey("x"))
		if len(o.modals) != 0 || strings.Contains(hintsOf(o), "x ") {
			t.Errorf("x on %q: modals %v, hints %q", id, o.modals, hintsOf(o))
		}
	}
}

func TestIdeateInitiativeHeaderEnterAndDetail(t *testing.T) {
	m, calls := keysModel(t)
	m = ideaAt(t, m, "i:ini")
	n := len(*calls)
	o := press(t, m, enterKey())
	if !strings.Contains(statusOf(o), "members still open · cut from Graph") || len(o.modals) != 0 || len(*calls) != n {
		t.Errorf("enter: status %q modals %d calls %v", statusOf(o), len(o.modals), *calls)
	}
	o = press(t, m, runeKey("d"))
	if v := ansi.Strip(o.View()); !o.tabAt(tabIdeate).(ideaTab).open || !strings.Contains(v, "INITIATIVE") || !strings.Contains(v, "parent      —") {
		t.Errorf("detail on the Initiative:\n%s", v)
	}
	if o = press(t, o, runeKey("d")); o.tabAt(tabIdeate).(ideaTab).open {
		t.Error("d did not close the pane")
	}
	if v := ansi.Strip(press(t, ideaAt(t, m, "mc-1"), runeKey("d")).View()); !strings.Contains(v, "parent      Ship v2") {
		t.Errorf("Signal parent line:\n%s", v)
	}
	for _, k := range []string{"i:"} {
		if h := ideaAt(t, m, k); press(t, h, runeKey("d")).tabAt(tabIdeate).(ideaTab).open {
			t.Errorf("d opened detail on %q", k)
		}
	}
}

// A selected Signal that left the list (closed, or in_progress with its session gone)
// falls back to the create row even when its group header is still there.
func TestIdeateCursorFallsBackToCreateRowWhenSignalLeaves(t *testing.T) {
	bs := func() []bd.Bead {
		return []bd.Bead{ini("i", "open", "Init", 1), sig("a", "open", 1, "i"), sig("b", "open", 1, "i"), sig("c", "in_progress", 1, "i")}
	}
	sess := map[string]session.Info{"c": {State: session.Alive}}
	for _, c := range []struct {
		name, id string
		edit     func([]bd.Bead)
		sess     map[string]session.Info
	}{
		{"closed", "a", func(b []bd.Bead) { b[1].Status = "closed" }, sess},
		{"session gone", "c", func([]bd.Bead) {}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := ideaAt(t, ideaModel(t, 100, 24, bs(), sess), c.id)
			next := bs()
			c.edit(next)
			m, _ = send(t, m, loadedMsg{beads: next, live: map[string]bool{}, sessions: c.sess})
			if got := cursorKey(m); got != "" {
				t.Errorf("cursor on %q, want the create row", got)
			}
		})
	}
}

// blockerBeads: sel is blocked by a visible Signal, one under a folded-able Initiative,
// a Build Ideate doesn't list, and a bead missing from the snapshot.
func blockerBeads() []bd.Bead {
	sel := sig("sel", "open", 0, "")
	sel.BlockedBy = []string{"vis", "inini", "bld", "ghost"}
	bld := bd.Bead{ID: "bld", Kind: contract.KindBuild, Status: "open", Title: "A build", Created: epoch}
	return []bd.Bead{sel, sig("vis", "open", 1, ""), ini("ini", "open", "Init", 1), sig("inini", "open", 1, "ini"), bld}
}

func ideaMoveTo(t *testing.T, m Model, key string) Model {
	t.Helper()
	for range 20 {
		if ideaSel(m) == key {
			return m
		}
		m = press(t, m, runeKey("j"))
	}
	t.Fatalf("cursor never reached %q", key)
	return m
}

func TestIdeateBlockerHighlightAndFooter(t *testing.T) {
	m := ideaMoveTo(t, ideaModel(t, 100, 40, blockerBeads(), nil), "sel")
	raw := m.View()
	v := ansi.Strip(raw)
	if l := ideaLine(v, "sig vis"); !strings.Contains(l, "!") {
		t.Errorf("visible blocker row lacks !: %q", l)
	}
	if !strings.Contains(raw, pinkFg.Render("sig vis")) {
		t.Error("visible blocker title is not pink")
	}
	foot := strings.Join(footerRows(v), "\n")
	if strings.Contains(foot, "sig vis") || !strings.Contains(foot, "A build") || strings.Contains(foot, "ghost") {
		t.Errorf("footer = %q", foot)
	}
	if strings.Contains(ideaLine(v, "+ New Signal"), "!") || !strings.HasPrefix(ideaLine(v, "+ New Signal"), "   ") {
		t.Errorf("create row gutter: %q", ideaLine(v, "+ New Signal"))
	}
}

func TestIdeateFoldedHeaderCountsAndFooterListsHidden(t *testing.T) {
	m := press(t, ideaModel(t, 100, 24, blockerBeads(), nil), runeKey("j"))
	m = press(t, m, runeKey(" ")) // fold Actionable, so no blocker has a visible twin
	m = ideaMoveTo(t, m, "sel")
	m = press(t, m, runeKey("k")) // up to a header
	for ideaSel(m) != "i:ini" {
		m = press(t, m, runeKey("k"))
	}
	m = press(t, m, runeKey(" ")) // fold Init
	m = ideaMoveTo(t, m, "sel")
	v := ansi.Strip(m.View())
	if l := ideaLine(v, "▸ ○·  INITIATIVE"); !strings.Contains(l, "1") || !strings.HasPrefix(strings.TrimSpace(l), "1") {
		t.Errorf("folded header gutter: %q", l)
	}
	if foot := strings.Join(footerRows(v), "\n"); !strings.Contains(foot, "sig inini") {
		t.Errorf("footer = %q", foot)
	}
}

func TestIdeateNoBlockersNoGutterNoFooter(t *testing.T) {
	m := ideaModel(t, 100, 24, mixed(), nil)
	m = ideaMoveTo(t, m, "mc-1")
	v := ansi.Strip(m.View())
	if strings.Contains(v, "!") {
		t.Errorf("unblocked selection decorated:\n%s", v)
	}
}

func TestIdeateHeaderSelectionHasNoFooter(t *testing.T) {
	m := ideaModel(t, 100, 24, blockerBeads(), nil) // cursor on + New Signal
	if v := ansi.Strip(m.View()); strings.Contains(v, "!") {
		t.Errorf("create row selection decorated:\n%s", v)
	}
}

func TestIdeateGapUnderCreateRow(t *testing.T) {
	ls := strings.Split(ansi.Strip(ideaModel(t, 100, 24, mixed(), nil).View()), "\n")
	for i, l := range ls {
		if strings.Contains(l, "+ New Signal") {
			if strings.TrimSpace(ls[i+1]) != "" || strings.TrimSpace(ls[i+2]) == "" {
				t.Errorf("want '+ New Signal', a blank line, then the groups:\n%s", strings.Join(ls[i:i+3], "\n"))
			}
			return
		}
	}
	t.Fatal("no '+ New Signal' line")
}

func TestIdeateBadgeCountsWaitingInitiative(t *testing.T) {
	drained := func() []bd.Bead {
		return []bd.Bead{ini("i", "open", "Init", 1), {ID: "m", Kind: contract.KindBuild, Status: "closed", Parent: "i", Created: epoch}}
	}
	waiting := session.Info{State: session.Alive, Waiting: session.WaitTurn}
	for _, c := range []struct {
		name string
		sess map[string]session.Info
		want int
	}{
		{"alive waiting", map[string]session.Info{"i": waiting}, 1},
		{"no session", nil, 0},
		{"exited", map[string]session.Info{"i": {State: session.Exited, Waiting: session.WaitTurn}}, 0},
		{"armed auto-exit reads live", map[string]session.Info{"i": {State: session.Alive, Waiting: session.WaitTurn, AutoExit: time.Now().Add(time.Minute)}}, 0},
		{"armed ask reads live", map[string]session.Info{"i": {State: session.Alive, Waiting: session.WaitAsk, AutoExit: time.Now().Add(time.Minute)}}, 0},
		{"closing turn reads live", map[string]session.Info{"i": {State: session.Alive, Waiting: session.WaitTurn, Closing: true}}, 0},
		{"closing ask reads live", map[string]session.Info{"i": {State: session.Alive, Waiting: session.WaitAsk, Closing: true}}, 0},
	} {
		m := ideaModel(t, 100, 24, drained(), c.sess)
		if n := m.tabAt(tabIdeate).count(); n != c.want {
			t.Errorf("%s: count = %d, want %d", c.name, n, c.want)
		}
	}
	m := ideaModel(t, 100, 24, drained(), map[string]session.Info{"i": waiting})
	tab := m.tabAt(tabIdeate).(ideaTab)
	tab.flip = map[string]bool{"i:i": true}
	m.tabs[tabIdeate] = tab
	if n := m.tabAt(tabIdeate).count(); n != 1 {
		t.Errorf("folded: count = %d, want 1", n)
	}
	paused := drained()
	paused[0].Labels = []string{"paused"}
	m = ideaModel(t, 100, 24, paused, map[string]session.Info{"i": waiting})
	if n := m.tabAt(tabIdeate).count(); n != 1 {
		t.Errorf("paused: count = %d, want 1", n)
	}
}

// p toggles paused:manual on a Signal as Graph's p does; the bar hints it before x; the
// create row and Initiative header take nothing.
func TestIdeatePause(t *testing.T) {
	m, calls := keysModel(t)
	o, cmd := send(t, ideaAt(t, m, "mc-long-22"), runeKey("p"))
	o = settle(t, o, cmd)
	if !has(*calls, "pauseBead mc-long-22") || !strings.Contains(statusOf(o), "paused mc-long-22") {
		t.Errorf("pause: calls %v status %q", *calls, statusOf(o))
	}
	if got := hintsOf(ideaAt(t, m, "mc-long-22")); !strings.Contains(got, "p pause · x drop") {
		t.Errorf("bar %q lacks p pause before x", got)
	}

	bs := mixed()
	bs[1].Labels = []string{"paused:manual"}
	m = ideaModel(t, 160, 30, bs, nil)
	if got := hintsOf(ideaAt(t, m, "mc-1")); !strings.Contains(got, "p unpause") {
		t.Errorf("bar %q lacks p unpause", got)
	}
	o, cmd = send(t, ideaAt(t, m, "mc-1"), runeKey("p"))
	o = settle(t, o, cmd)
	if !has(*calls, "unpauseBead mc-1") || !strings.Contains(statusOf(o), "unpaused mc-1") {
		t.Errorf("unpause: calls %v status %q", *calls, statusOf(o))
	}

	n := len(*calls)
	for _, id := range []string{"", "i:ini"} {
		if o, cmd := send(t, ideaAt(t, m, id), runeKey("p")); cmd != nil || len(*calls) != n || o.status != m.status {
			t.Errorf("p on %q did something", id)
		}
	}
}

// A Closing session is never Waiting: a Signal and an Initiative's cut session leave
// Actionable for Live, showing closing… and no waiting word.
func TestIdeateClosingReadsLiveNotActionable(t *testing.T) {
	for _, w := range []session.Waiting{session.WaitTurn, session.WaitAsk} {
		closing := session.Info{State: session.Alive, Waiting: w, Closing: true}
		m := ideaModel(t, 100, 24, mixed(), map[string]session.Info{"mc-long-22": closing, "ini": closing})
		var keys []string
		for _, r := range m.tabAt(tabIdeate).(ideaTab).rows() {
			keys = append(keys, r.key)
		}
		got := strings.Join(keys, ",")
		for _, k := range []string{"act:mc-long-22", "act:ini"} {
			if strings.Contains(got, k) {
				t.Errorf("%s: %s still Actionable: %s", w, k, got)
			}
		}
		if !strings.Contains(got, "live:mc-long-22") {
			t.Errorf("%s: Signal not Live: %s", w, got)
		}
		v := ansi.Strip(m.View())
		if l := ideaLine(v, "sig mc-long-22"); !strings.Contains(l, "closing…") || strings.Contains(l, "your turn") || strings.Contains(l, "needs approval") {
			t.Errorf("%s: row = %q", w, l)
		}
	}
}

// +/- on a row that stands for an Initiative bumps the Initiative through the shared
// bumpPriority; rows that stand for no bead stay inert and unhinted.
func TestIdeateInitiativeRowsReprioritise(t *testing.T) {
	var calls []string
	seam(t, &updateBead, func(id string, args []string) error {
		calls = append(calls, id+" "+strings.Join(args, " "))
		return nil
	})
	members := func(i bd.Bead) []bd.Bead {
		return []bd.Bead{i, sig("mc-1", "open", 1, i.ID)}
	}
	drained := func(i bd.Bead) []bd.Bead {
		return []bd.Bead{i, {ID: "m", Kind: contract.KindBuild, Status: "closed", Parent: i.ID, Created: epoch}}
	}
	for _, c := range []struct {
		name  string
		beads []bd.Bead
		row   string
		k     string
		want  string
	}{
		{"header +", members(ini("i", "open", "Init", 2)), "i:i", "+", "i --priority 1"},
		{"header =", members(ini("i", "open", "Init", 2)), "i:i", "=", "i --priority 1"},
		{"header -", members(ini("i", "open", "Init", 2)), "i:i", "-", "i --priority 3"},
		{"drained hint row", drained(ini("i", "open", "Init", 2)), "h:i", "+", "i --priority 1"},
		{"drained header", drained(ini("i", "open", "Init", 2)), "i:i", "-", "i --priority 3"},
		{"combined row", drained(ini("i", "open", "Init", 2)), "act:i", "+", "i --priority 1"},
		{"P0 + stops", members(ini("i", "open", "Init", 0)), "i:i", "+", ""},
		{"P4 - stops", members(ini("i", "open", "Init", 4)), "i:i", "-", ""},
		{"closed refused", members(ini("i", "closed", "Init", 2)), "i:i", "+", ""},
	} {
		calls = nil
		m := ideaAt(t, ideaModel(t, 100, 24, c.beads, nil), c.row)
		if h := hintsOf(m); !strings.Contains(h, "+/- prio") {
			t.Errorf("%s: hints %q lack +/- prio", c.name, h)
		}
		o, cmd := send(t, m, runeKey(c.k))
		settle(t, o, cmd)
		got := strings.Join(calls, ";")
		if got != c.want {
			t.Errorf("%s: updateBead %q, want %q", c.name, got, c.want)
		}
		if c.want == "" && !strings.Contains(o.status, "already P") && !strings.Contains(o.status, "closed — priority unchanged") {
			t.Errorf("%s: status %q", c.name, o.status)
		}
	}
	m := ideaModel(t, 100, 24, append(members(ini("i", "open", "Init", 2)), sig("loose", "open", 2, "")), nil)
	for _, row := range []string{"i:", ""} {
		m := ideaAt(t, m, row)
		if h := hintsOf(m); strings.Contains(h, "prio") {
			t.Errorf("%q hints %q offer prio", row, h)
		}
		calls = nil
		if o, cmd := send(t, m, runeKey("+")); cmd != nil || len(calls) != 0 || o.status != m.status {
			t.Errorf("+ on %q did something", row)
		}
	}
}
