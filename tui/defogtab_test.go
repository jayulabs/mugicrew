package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

func dbead(id string, k contract.Kind, status, parent string, prio int, mut ...func(*bd.Bead)) bd.Bead {
	b := bd.Bead{ID: id, Kind: k, Status: status, Parent: parent, Title: "t " + id, Priority: prio, Created: epoch}
	b.Container = k == contract.KindChart || k == contract.KindInitiative
	for _, f := range mut {
		f(&b)
	}
	return b
}

func labeled(l string) func(*bd.Bead)      { return func(b *bd.Bead) { b.Labels = append(b.Labels, l) } }
func blockedBy(id string) func(*bd.Bead)   { return func(b *bd.Bead) { b.BlockedBy = []string{id} } }
func newer(d time.Duration) func(*bd.Bead) { return func(b *bd.Bead) { b.Created = epoch.Add(d) } }

// dshape renders defogList rows as "# title", "C id", "L id", "A id" (open AFK), "+ n"
// for '▸ N closed' and "x id" for a closed child.
func dshape(rs []dRow) (out []string) {
	for _, r := range rs {
		switch {
		case r.kind == dHead:
			out = append(out, "# "+r.title)
		case r.kind == dChart:
			out = append(out, "C "+r.b.ID)
		case r.kind == dLeaf:
			out = append(out, "L "+r.b.ID)
		case r.kind == dSum:
			out = append(out, "+ "+string(rune('0'+len(r.members))))
		case r.afk:
			out = append(out, "A "+r.b.ID)
		default:
			out = append(out, "x "+r.b.ID)
		}
	}
	return
}

func TestDefogList(t *testing.T) {
	ev := dbead("ev", contract.KindResearch, "open", "c", 0)
	ev.Event = true
	cases := []struct {
		name  string
		beads []bd.Bead
		flip  map[string]bool
		want  string
	}{
		{"empty", nil, nil, "# No Initiative"},
		{"groups by beadCmp, No Initiative last, Charts by beadCmp",
			[]bd.Bead{
				dbead("i2", contract.KindInitiative, "open", "", 2), dbead("i1", contract.KindInitiative, "open", "", 1),
				dbead("loose", contract.KindChart, "open", "", 0),
				dbead("c2", contract.KindChart, "open", "i2", 1), dbead("c1b", contract.KindChart, "in_progress", "i1", 3), dbead("c1a", contract.KindChart, "open", "i1", 2),
			}, nil, "# t i1,C c1a,C c1b,# t i2,C c2,# No Initiative,C loose"},
		{"Drafts, defogged and closed Charts out",
			[]bd.Bead{
				dbead("a", contract.KindChart, "deferred", "", 1), dbead("b", contract.KindChart, "open", "", 1, labeled("chart:defogged")),
				dbead("c", contract.KindChart, "closed", "", 1), dbead("d", contract.KindChart, "open", "", 1),
			}, nil, "# No Initiative,C d"},
		{"Survey first, HITL before AFK, closed fold last",
			[]bd.Bead{
				dbead("c", contract.KindChart, "open", "", 1),
				dbead("r", contract.KindResearch, "open", "c", 0), dbead("g", contract.KindGrilling, "open", "c", 0),
				dbead("s", contract.KindSurvey, "open", "c", 3), dbead("p", contract.KindPrototype, "open", "c", 1),
				dbead("k1", contract.KindGrilling, "closed", "c", 1), dbead("k2", contract.KindResearch, "closed", "c", 1),
			}, nil, "# No Initiative,C c,L s,L g,L p,A r,+ 2"},
		{"closed fold omitted when none; Drafts, events and other parents' children out",
			[]bd.Bead{
				dbead("c", contract.KindChart, "open", "", 1), dbead("d", contract.KindChart, "open", "", 2),
				dbead("draft", contract.KindGrilling, "deferred", "c", 1), ev, dbead("other", contract.KindGrilling, "open", "d", 1),
			}, nil, "# No Initiative,C c,C d,L other"},
		{"expanded fold lists closed members as non-AFK children",
			[]bd.Bead{dbead("c", contract.KindChart, "open", "", 1), dbead("k1", contract.KindResearch, "closed", "c", 1, newer(time.Hour)), dbead("k2", contract.KindResearch, "closed", "c", 1)},
			map[string]bool{"c/closed": true}, "# No Initiative,C c,+ 2,x k1,x k2"},
		{"folded Chart and Initiative hide their rows",
			[]bd.Bead{dbead("i", contract.KindInitiative, "open", "", 1), dbead("c", contract.KindChart, "open", "i", 1), dbead("l", contract.KindGrilling, "open", "c", 1), dbead("d", contract.KindChart, "open", "i", 2)},
			map[string]bool{"c": true}, "# t i,C c,C d,# No Initiative"},
		{"folded Initiative", []bd.Bead{dbead("i", contract.KindInitiative, "open", "", 1), dbead("c", contract.KindChart, "open", "i", 1)}, map[string]bool{"i:i": true}, "# t i,# No Initiative"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var tree []dRow // the Actionable section has its own test
			for _, r := range oldDefogList(graphData{beads: c.beads}, c.flip) {
				if r.kind != dSec && r.kind != dPlace && r.kind != dAct && r.kind != dCW { // Charts has its own test
					tree = append(tree, r)
				}
			}
			if got := strings.Join(dshape(tree), ","); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestLeafState(t *testing.T) {
	up := func(w session.Waiting) session.Info { return session.Info{State: session.Alive, Waiting: w} }
	leaf := func(mut ...func(*bd.Bead)) bd.Bead { return dbead("l", contract.KindGrilling, "open", "c", 1, mut...) }
	cases := []struct {
		name  string
		beads []bd.Bead
		sess  session.Info
		launc bool
		want  lstate
	}{
		{"ready", []bd.Bead{leaf()}, session.Info{}, false, sReady},
		{"launching beats a session", []bd.Bead{leaf()}, up(session.WaitAsk), true, sLaunching},
		{"done", []bd.Bead{leaf(func(b *bd.Bead) { b.Status = "closed" })}, up(""), false, sDone},
		{"ask beats turn", []bd.Bead{leaf()}, up(session.WaitAsk), false, sAsk},
		{"turn", []bd.Bead{leaf()}, up(session.WaitTurn), false, sTurn},
		{"live", []bd.Bead{leaf()}, up(""), false, sLive},
		{"exited", []bd.Bead{leaf()}, session.Info{State: session.Exited}, false, sExited},
		{"blocked", []bd.Bead{leaf(blockedBy("o")), dbead("o", contract.KindResearch, "open", "c", 1)}, session.Info{}, false, sBlocked},
		{"blocked leaf that also has a session is live", []bd.Bead{leaf(blockedBy("o")), dbead("o", contract.KindResearch, "open", "c", 1)}, up(""), false, sLive},
		{"blocked beats paused", []bd.Bead{leaf(blockedBy("o"), labeled("paused:manual")), dbead("o", contract.KindResearch, "open", "c", 1)}, session.Info{}, false, sBlocked},
		{"own pause", []bd.Bead{leaf(labeled("paused:manual"))}, session.Info{}, false, sPaused},
		{"inherited pause", []bd.Bead{dbead("c", contract.KindChart, "open", "", 1, labeled("paused:manual")), leaf()}, session.Info{}, false, sPaused},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := graphData{beads: c.beads, sessions: map[string]session.Info{"l": c.sess}, launching: map[string]bool{"l": c.launc}}
			if got := g.leafState(g.beadByID("l")); got != c.want {
				t.Errorf("state = %d, want %d", got, c.want)
			}
		})
	}
}

func defogFixture() ([]bd.Bead, map[string]session.Info) {
	beads := []bd.Bead{
		dbead("ini", contract.KindInitiative, "open", "", 1),
		dbead("c", contract.KindChart, "open", "ini", 1),
		dbead("turn", contract.KindGrilling, "open", "c", 1),
		dbead("ready", contract.KindGrilling, "open", "c", 1),
		dbead("blk", contract.KindGrilling, "open", "c", 1, blockedBy("afk")),
		dbead("paused", contract.KindGrilling, "open", "c", 1, labeled("paused:manual")),
		dbead("launch", contract.KindGrilling, "open", "c", 1),
		dbead("live", contract.KindGrilling, "open", "c", 1),
		dbead("afk", contract.KindResearch, "open", "c", 1),
		dbead("closed", contract.KindResearch, "closed", "c", 1),
		dbead("dc", contract.KindChart, "open", "ini", 2, labeled("chart:defogged")),
		dbead("dcl", contract.KindGrilling, "open", "dc", 1),
		dbead("pc", contract.KindChart, "open", "ini", 3, labeled("paused:manual")),
		dbead("inh", contract.KindGrilling, "open", "pc", 1),
	}
	return beads, map[string]session.Info{
		"turn": {State: session.Alive, Waiting: session.WaitTurn},
		"live": {State: session.Alive},
	}
}

func defogModel(t *testing.T, w, h int) Model {
	beads, sess := defogFixture()
	m := sized(t, w, h)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: sess})
	m = treesOpen(m)
	m.launching = map[string]bool{"launch": true}
	return treesOpen(m)
}

// The badge counts only waiting leaves under listed Charts; a ready, unlaunched one
// doesn't count (ADR 0171 §5).
func TestDefogBadge(t *testing.T) {
	m := defogModel(t, 100, 30)
	if n := m.tabAt(tabDefog).count(); n != 1 { // turn; ready doesn't count
		t.Errorf("badge = %d, want 1", n)
	}
	if lbl := ansi.Strip(tabLabel(tabDefog, m.tabAt(tabDefog))); lbl != "2 Defog ●1" {
		t.Errorf("label = %q", lbl)
	}
}

func defogSel(m Model) string { return m.tabAt(tabDefog).(defogTab).sel }

func TestDefogViewSelectionAndFolds(t *testing.T) {
	m := defogModel(t, 100, 40)
	m = press(t, m, runeKey("2"))
	v := ansi.Strip(m.View())
	for _, want := range []string{"INITIATIVE", "t ini", "t turn", "your turn", "launching…", "⊘", "t afk", "▸ 1 closed", "t inh"} {
		if !strings.Contains(v, want) {
			t.Errorf("frame lacks %q:\n%s", want, v)
		}
	}
	if strings.Count(v, "t dc") != 1 || strings.Contains(v, "t dcl") { // the stamped Chart is in Charts only, with no tree row or leaves
		t.Errorf("a defogged Chart is in the tree:\n%s", v)
	}
	if got := defogSel(m); got != "" {
		t.Errorf("initial sel = %q, want the unset first row", got)
	}
	m = press(t, m, runeKey("j"))
	if defogSel(m) != "act:turn" { // Actionable's first row: the waiting leaf
		t.Errorf("j -> %q, want act:turn", defogSel(m))
	}
	m = press(t, m, runeKey("k"))
	m = press(t, m, runeKey("k"))
	if last := defogSel(m); last != "i:" { // wraps to the last row, the folded No Initiative header
		t.Errorf("k wrap -> %q, want i:", last)
	}
	// space folds the Chart; the fold and the selection survive a refresh
	m = defogAt(t, m, "c")
	m = press(t, m, runeKey(" "))
	beads, sess := defogFixture()
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: sess})
	if v := ansi.Strip(m.View()); strings.Contains(v, "t blk") || defogSel(m) != "c" {
		t.Errorf("fold or selection lost across refresh (sel %q):\n%s", defogSel(m), v)
	}
	// the selection stays on its bead when rows above it appear
	beads = append(beads, dbead("new", contract.KindInitiative, "open", "", 0), dbead("nc", contract.KindChart, "open", "new", 0))
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: sess})
	if defogSel(m) != "c" {
		t.Errorf("sel moved to %q", defogSel(m))
	}
}

func TestDefogEmptyAndFloor(t *testing.T) {
	m := sized(t, 100, 30)
	m, _ = send(t, m, loadedMsg{live: map[string]bool{}})
	if v := ansi.Strip(press(t, m, runeKey("2")).View()); !strings.Contains(v, "nothing waits on you") {
		t.Errorf("empty view:\n%s", v)
	}
	f := defogModel(t, floorCols, floorRows)
	f = press(t, f, runeKey("2"))
	ls := lines(f.View())
	if len(ls) != floorRows {
		t.Fatalf("frame is %d lines, want %d", len(ls), floorRows)
	}
	for _, l := range ls {
		if ansi.StringWidth(l) > floorCols {
			t.Errorf("line %q overflows %d cols", l, floorCols)
		}
	}
}

// defogActModel is the Defog fixture plus a blocked AFK bead, with every action seam
// stubbed so a stray launch or write cannot reach bd, git or tmux.
func defogActModel(t *testing.T) (Model, *[]string) {
	t.Helper()
	calls := stubActions(t)
	beads, sess := defogFixture()
	beads = append(beads, dbead("afkb", contract.KindResearch, "open", "c", 1, blockedBy("afk")),
		dbead("fail", contract.KindResearch, "open", "c", 1, labeled("paused:failing")),
		dbead("dr", contract.KindChart, "open", "ini", 4), dbead("drk", contract.KindResearch, "closed", "dr", 1),
		dbead("cla", contract.KindGrilling, "closed", "c", 1), dbead("cle", contract.KindGrilling, "closed", "c", 1))
	sess["cla"], sess["cle"] = session.Info{State: session.Alive}, session.Info{State: session.Exited}
	m := sized(t, 160, 40)
	m.crew = map[string]contract.Crewmate{"luffy": fakeCrew{}, "nami": fakeCrew{}, "franky": fakeCrew{}}
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: sess})
	m = treesOpen(m)
	return press(t, m, runeKey("2")), calls
}

func defogAt(t *testing.T, m Model, id string) Model {
	t.Helper()
	if defogSel(m) == "" { // the unset selection is the first row
		m = press(t, press(t, m, runeKey("j")), runeKey("k"))
	}
	for range 30 {
		if defogSel(m) == id {
			return m
		}
		m = press(t, m, runeKey("j"))
	}
	t.Fatalf("no Defog row %s; at %q", id, defogSel(m))
	return m
}

// enter launches a ready leaf, attaches a live one and runs an AFK row (y/n when it
// is blocked); each refusal names its reason and nothing starts a subprocess.
func TestDefogEnter(t *testing.T) {
	log := fakeTools(t)
	m, calls := defogActModel(t)
	for _, c := range []struct{ id, say string }{
		{"blk", "blk is blocked by t afk (afk) — not launching"},
		{"paused", "paused is paused (paused:manual) — not launching"},
		{"inh", "inh is paused by pc — not launching"},
		{"i:ini", "space folds"},
		{"c", "c: Chart not drained — 3/12 children closed"},
		{"c/closed", "space folds"},
	} {
		n, cmd := send(t, defogAt(t, m, c.id), enterKey())
		if cmd != nil || len(n.modals) != 0 || !strings.Contains(statusOf(n), c.say) {
			t.Errorf("%s: cmd=%v modals=%d status=%q, want %q", c.id, cmd != nil, len(n.modals), statusOf(n), c.say)
		}
	}
	n := press(t, defogAt(t, m, "c/closed"), runeKey(" "))
	if n, cmd := send(t, defogAt(t, n, "closed"), enterKey()); cmd != nil || !strings.Contains(statusOf(n), "closed — nothing to launch or run") {
		t.Errorf("closed child: cmd=%v status=%q", cmd != nil, statusOf(n))
	}
	m2 := press(t, defogAt(t, m, "c/closed"), runeKey(" ")) // closed members holding a session still attach or resume
	for id, verb := range map[string]string{"cla": "enter attach", "cle": "enter resume"} {
		n := defogAt(t, m2, id)
		if h := hintsOf(n); !strings.Contains(h, verb) {
			t.Errorf("%s: hints %q, want %q", id, h, verb)
		}
		if n, cmd := send(t, n, enterKey()); cmd == nil || len(n.modals) != 0 {
			t.Errorf("%s: cmd=%v modals=%d, want a started action", id, cmd != nil, len(n.modals))
		}
	}
	for _, id := range []string{"ready", "live", "afk"} { // launch, attach, run
		if n, cmd := send(t, defogAt(t, m, id), enterKey()); cmd == nil || len(n.modals) != 0 {
			t.Errorf("%s: cmd=%v modals=%d, want a started action", id, cmd != nil, len(n.modals))
		}
	}
	if n, cmd := send(t, defogAt(t, m, "dr"), enterKey()); cmd == nil || n.pending["dr"] == "" { // a drained Chart runs spec
		t.Errorf("drained Chart: cmd=%v pending=%v status=%q", cmd != nil, n.pending, statusOf(n))
	}
	if n, cmd := send(t, defogAt(t, m, "fail"), enterKey()); cmd == nil || n.pending["fail"] == "" { // retry on paused:failing
		t.Errorf("failing AFK: cmd=%v status=%q", cmd != nil, statusOf(n))
	}
	n, cmd := send(t, defogAt(t, m, "afkb"), enterKey())
	if cmd != nil || len(n.modals) != 1 || n.modals[0].title != "Run blocked bead now?" {
		t.Errorf("blocked AFK: cmd=%v modals=%+v", cmd != nil, n.modals)
	}
	n.View()
	// attach is the one synchronous route: it switches to the live session
	if got, _ := os.ReadFile(log); len(got) > 0 || strings.Join(*calls, ";") != "switchSession cla;switchSession live" {
		t.Errorf("subprocess or seam call: log=%q calls=%v", got, *calls)
	}
}

// x, +/- and E take Graph's paths on bead rows; n, e, z and t stay unbound.
func TestDefogSharedKeys(t *testing.T) {
	m, _ := defogActModel(t)
	if n, cmd := send(t, defogAt(t, m, "turn"), runeKey("x")); cmd != nil || len(n.modals) != 1 {
		t.Errorf("x on a session: cmd=%v modals=%d", cmd != nil, len(n.modals))
	}
	if _, cmd := send(t, defogAt(t, m, "ready"), runeKey("+")); cmd == nil {
		t.Error("+ did not start a priority write")
	}
	for _, k := range []string{"e", "z"} {
		if n, cmd := send(t, defogAt(t, m, "ready"), runeKey(k)); cmd != nil || len(n.modals) != 0 || n.View() != defogAt(t, m, "ready").View() {
			t.Errorf("%s is bound on Defog", k)
		}
	}
}

// The hints bar offers only what acts, in ADR 0156 §7's order, and ends with ? keys.
func TestDefogHints(t *testing.T) {
	m, _ := defogActModel(t)
	for _, c := range []struct{ id, want string }{
		{"ready", "enter launch · b bg · d detail · p pause · t transcript · +/- prio · J/K section"},
		{"turn", "enter attach · d detail · p pause · x drop · t transcript · +/- prio · J/K section"},
		{"blk", "d detail · p pause · t transcript · +/- prio · J/K section"},
		{"paused", "d detail · p unpause · t transcript · +/- prio · J/K section"},
		{"inh", "d detail · p pause · t transcript · +/- prio · J/K section"},
		{"afk", "enter run · d detail · p pause · t transcript · +/- prio · J/K section"},
		{"c", "space fold · d detail · p pause · t transcript · +/- prio · J/K section · r retrospective"},
		{"fail", "enter retry · d detail · p pause · t transcript · +/- prio · J/K section"},
		{"dr", "enter spec · space fold · d detail · p pause · t transcript · +/- prio · J/K section · r retrospective"},
		{"i:ini", "space fold · d detail · J/K section"},
		{"c/closed", "space fold · d detail · J/K section"},
	} {
		n := defogAt(t, m, c.id)
		if got := hintsOf(n); got != c.want {
			t.Errorf("%s: hints %q, want %q", c.id, got, c.want)
		}
		l := lines(n.View())
		if bar := strings.TrimRight(l[len(l)-1], " "); !strings.HasSuffix(bar, "? keys") {
			t.Errorf("%s: bar %q does not end with ? keys", c.id, bar)
		}
	}
	n := press(t, defogAt(t, m, "c/closed"), runeKey(" "))
	if got := hintsOf(defogAt(t, n, "closed")); got != "d detail · t transcript · J/K section" {
		t.Errorf("closed child: hints %q, want d detail · t transcript · J/K section", got)
	}
	o := press(t, defogAt(t, m, "ready"), runeKey("d"))
	if got := hintsOf(o); got != "enter launch · b bg · d close · pgup/pgdn scroll · p pause · t transcript · +/- prio · J/K section" {
		t.Errorf("detail open: hints %q", got)
	}
}

func TestDefogBindingCoverage(t *testing.T) {
	m, _ := defogActModel(t)
	for _, id := range []string{"i:ini", "c", "ready", "turn", "blk", "paused", "afk", "afkb", "c/closed"} {
		checkCoverage(t, "Defog "+id, defogAt(t, m, id), true, "2", "J", "K", "ctrl+down", "ctrl+up")
	}
	n := press(t, defogAt(t, m, "c/closed"), runeKey(" "))
	checkCoverage(t, "Defog closed child", defogAt(t, n, "closed"), true, "2", "J", "K", "ctrl+down", "ctrl+up")
}

const chartBody = "## Destination\nBuild the thing.\nIn and out.\n\n## Notes\nn1\nn2\n\n## Decisions-so-far\nd1\n\n## Not-yet-specified\nfog one\nfog two\n\n## Out-of-scope\noos\n"

// d shows each row type's detail in the lower half; the frame still fits.
func TestDefogDetail(t *testing.T) {
	m, _ := defogActModel(t)
	beads, sess := defogFixture()
	for i := range beads {
		switch beads[i].ID {
		case "c":
			beads[i].Description = chartBody
		case "ready":
			beads[i].Description = "the ready question" + strings.Repeat("\n\nmore", 80)
		case "closed":
			beads[i].CloseReason = "settled: use X"
			beads[i].Description = "the closed question"
		}
	}
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: sess})
	m = treesOpen(m)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 160, Height: 60})
	detail := func(id string) string {
		n := press(t, defogAt(t, m, id), runeKey("d"))
		if len(lines(n.View())) != 60 {
			t.Errorf("%s: frame is %d lines, want 60", id, len(lines(n.View())))
		}
		return ansi.Strip(n.View())
	}
	if d := detail("ready"); !strings.Contains(d, "○· GRILLING P1 · t ready · ready") {
		t.Errorf("heading is not the SBRN:\n%s", d)
	}
	for _, c := range []struct {
		id   string
		want []string
	}{
		{"c", []string{"P1", "HITL 6", "AFK 1", "refuses — c: Chart not drained", "Build the thing.", "In and out.", "fog one", "fog two", "Notes: 2 lines", "Decisions-so-far: 1 line", "Out-of-scope: 1 line"}},
		{"ready", []string{"GRILLING", "HITL", "HITL", "P1", "ready", "enter launch", "the ready question"}},
		{"blk", []string{"blocked by", "t afk (afk)", "refuses — blk is blocked"}},
		{"inh", []string{"manual via pc"}},
		{"afk", []string{"RESEARCH", "AFK", "enter run"}},
		{"c/closed", []string{"t closed", "— settled: use X"}},
	} {
		v := detail(c.id)
		for _, w := range c.want {
			if !strings.Contains(v, w) {
				t.Errorf("%s: detail lacks %q:\n%s", c.id, w, v)
			}
		}
	}
	n := press(t, defogAt(t, m, "c/closed"), runeKey(" "))
	n = press(t, defogAt(t, n, "closed"), runeKey("d"))
	if v := ansi.Strip(n.View()); !strings.Contains(v, "settled: use X") || !strings.Contains(v, "the closed question") {
		t.Errorf("closed child detail:\n%s", v)
	}
	// d closes, and the pane scrolls with pgdn
	o := press(t, defogAt(t, m, "ready"), runeKey("d"))
	if ansi.Strip(press(t, o, runeKey("d")).View()) != ansi.Strip(defogAt(t, m, "ready").View()) {
		t.Error("second d did not close the detail")
	}
	if ansi.Strip(press(t, o, tea.KeyMsg{Type: tea.KeyPgDown}).View()) == ansi.Strip(o.View()) {
		t.Error("pgdn did not scroll the detail")
	}
	f := defogModel(t, floorCols, floorRows)
	f = press(t, press(t, press(t, f, runeKey("2")), runeKey("j")), runeKey("d"))
	for _, l := range lines(f.View()) {
		if ansi.StringWidth(l) > floorCols {
			t.Errorf("line %q overflows %d cols", l, floorCols)
		}
	}
	if len(lines(f.View())) != floorRows {
		t.Errorf("floor frame is %d lines", len(lines(f.View())))
	}
}

// b is enter's background twin: it launches or resumes a HITL leaf (or resumes an AFK
// row's exited session) without attaching, and elsewhere gives enter's queue answer.
func TestDefogBg(t *testing.T) {
	fakeTools(t)
	calls := stubActions(t)
	beads, sess := defogFixture()
	beads = append(beads,
		dbead("exited", contract.KindGrilling, "open", "c", 1),
		dbead("afkl", contract.KindResearch, "open", "c", 1),
		dbead("afkx", contract.KindResearch, "open", "c", 1))
	sess["exited"] = session.Info{State: session.Exited}
	sess["afkl"] = session.Info{State: session.Alive}
	sess["afkx"] = session.Info{State: session.Exited}
	m := sized(t, 160, 40)
	m.crew = map[string]contract.Crewmate{"luffy": fakeCrew{}, "nami": fakeCrew{}, "franky": fakeCrew{}}
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: sess})
	m = treesOpen(m)
	m = press(t, m, runeKey("2"))
	for _, c := range []struct {
		id, say  string
		cmd, bar bool
	}{
		{"ready", "", true, true},
		{"exited", "", true, true},
		{"live", "already running — enter attaches", false, false},
		{"blk", "blk is blocked by t afk (afk) — not launching", false, false},
		{"paused", "paused is paused (paused:manual) — not launching", false, false},
		{"afk", "b is HITL-only — enter runs it", false, false},
		{"afkl", "already running — enter attaches", false, false},
		{"afkx", "", true, true},
		{"c", "Chart not drained", false, false},
		{"i:ini", "space folds", false, false},
		{"c/closed", "space folds", false, false},
	} {
		n := defogAt(t, m, c.id)
		if bar := strings.Contains(hintsOf(n), "b bg"); bar != c.bar {
			t.Errorf("%s: b bg in bar = %v, want %v (%q)", c.id, bar, c.bar, hintsOf(n))
		}
		n, cmd := send(t, n, runeKey("b"))
		if (cmd != nil) != c.cmd || len(n.modals) != 0 || !strings.Contains(statusOf(n), c.say) {
			t.Errorf("%s: cmd=%v modals=%d status=%q, want cmd=%v %q", c.id, cmd != nil, len(n.modals), statusOf(n), c.cmd, c.say)
		}
	}
	if got := hintsOf(defogAt(t, m, "ready")); got != "enter launch · b bg · d detail · p pause · t transcript · +/- prio · J/K section" {
		t.Errorf("ready hints %q", got)
	}
	n := press(t, defogAt(t, m, "c/closed"), runeKey(" "))
	n, cmd := send(t, defogAt(t, n, "closed"), runeKey("b"))
	if cmd != nil || !strings.Contains(statusOf(n), "closed — nothing to launch or run") {
		t.Errorf("closed child: cmd=%v status=%q", cmd != nil, statusOf(n))
	}
	if len(*calls) != 0 {
		t.Errorf("b attached: %v", *calls)
	}
}

// ? words b as the HITL background twin on Defog; Stats keeps group by.
func TestRefBgWording(t *testing.T) {
	m, _ := defogActModel(t)
	for _, id := range []string{"ready", "live", "blk", "c"} {
		n := defogAt(t, m, id)
		n.ref = true
		if v := n.View(); !strings.Contains(v, "bg launch/resume (HITL)") {
			t.Errorf("Defog %s: ? lacks b wording", id)
		}
	}
	g := press(t, m, runeKey("4"))
	g.ref = true
	if v := g.View(); !strings.Contains(v, "bg launch/resume (HITL)") {
		t.Errorf("Graph ? lacks b wording:\n%s", g.View())
	}
	s := press(t, m, runeKey("#"))
	s.ref = true
	if v := s.View(); !strings.Contains(v, "group by") || strings.Contains(v, "launch/resume (HITL)") {
		t.Errorf("Stats ? wrong:\n%s", v)
	}
}

// A refusing-paused AND blocked AFK row refuses with its pause hint: no dialog, no run.
func TestDefogEnterPausedBlockedAFKRefusesBeforeDialog(t *testing.T) {
	calls := stubActions(t)
	beads, sess := defogFixture()
	beads = append(beads, dbead("pafk", contract.KindResearch, "open", "c", 1, blockedBy("afk"), labeled("paused:manual")))
	m := sized(t, 160, 40)
	m.crew = map[string]contract.Crewmate{"luffy": fakeCrew{}, "nami": fakeCrew{}, "franky": fakeCrew{}}
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: sess})
	m = treesOpen(m)
	n, cmd := send(t, defogAt(t, press(t, m, runeKey("2")), "pafk"), enterKey())
	if cmd != nil || len(n.modals) != 0 || !strings.Contains(statusOf(n), "pafk is paused by hand") || len(*calls) != 0 {
		t.Errorf("cmd=%v modals=%d status=%q calls=%v", cmd != nil, len(n.modals), statusOf(n), *calls)
	}
}

// A leaf or child row's "P<n> title" follows closed > AFK mute > normal; the id
// stays soft grey either way, so an AFK child and a HITL leaf differ only by the mute.
func TestDefogMutesAFKTitles(t *testing.T) {
	forceANSI256(t)
	m := press(t, defogModel(t, 100, 30), runeKey("2"))
	m = press(t, defogAt(t, m, "c/closed"), runeKey(" "))
	v := unkept(m.View())
	for name, want := range map[string]string{
		"AFK child":    tinted(softSt, 1).Render("P1") + sep() + softSt.Render("t afk") + sep() + softSt.Render("afk"),
		"HITL leaf":    tinted(prio1Sty, 1).Render("P1") + sep() + "t ready" + sep() + softSt.Render("ready"),
		"closed child": tinted(closedSty, 1).Render("P1") + sep() + closedSty.Render("t closed") + sep() + softSt.Render("closed"),
	} {
		if !strings.Contains(v, want) {
			t.Errorf("%s: view lacks %q:\n%s", name, want, v)
		}
	}
}

// The queue-tab routing is callable from graphData alone, with no defogTab.
func TestRowRoutingWithoutDefogTab(t *testing.T) {
	var g graphData
	if why := g.rowEnterWhy(nil, false); why != "nothing to launch or run here — space folds" {
		t.Errorf("no-bead row: %q", why)
	}
	if got := g.rowEnterField(nil, false); got != "refuses — nothing to launch or run here — space folds" {
		t.Errorf("enterField on no-bead row: %q", got)
	}
}

// Graph shows no badge even with a waiting session.
func TestGraphNoBadge(t *testing.T) {
	m := defogModel(t, 100, 30)
	if n := m.tabAt(tabGraph).count(); n != 0 {
		t.Errorf("graph badge = %d, want 0", n)
	}
}

// blockerDefog: sel is blocked by vis (a drawn leaf), hid (under a closed-by-default
// Chart fold once folded), and gone (on no Defog row: its Chart is defogged).
func blockerDefog(t *testing.T, h int) Model {
	beads := []bd.Bead{
		dbead("ini", contract.KindInitiative, "open", "", 1),
		dbead("c", contract.KindChart, "open", "ini", 1),
		dbead("sel", contract.KindGrilling, "open", "c", 1, func(b *bd.Bead) { b.BlockedBy = []string{"vis", "hid", "gone", "nope"} }),
		dbead("vis", contract.KindGrilling, "open", "c", 1),
		dbead("free", contract.KindGrilling, "open", "c", 1),
		dbead("c2", contract.KindChart, "open", "ini", 2),
		dbead("hid", contract.KindGrilling, "open", "c2", 1),
		dbead("dc", contract.KindChart, "open", "ini", 3, labeled("chart:defogged")),
		dbead("gone", contract.KindGrilling, "open", "dc", 1),
	}
	m := sized(t, 100, h)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: map[string]session.Info{}})
	m = treesOpen(m)
	return defogAt(t, press(t, m, runeKey("2")), "sel")
}

func footerRow(v string) string { return strings.Join(footerRows(v), "\n") }

func TestDefogBlockerHighlight(t *testing.T) {
	forceANSI256(t)
	m := blockerDefog(t, 30)
	v := m.View()
	if !strings.Contains(v, blockerMk.Render("!")) || !strings.Contains(v, tinted(pinkFg, 1).Render("P1")+sep()+pinkFg.Render("t vis")) {
		t.Errorf("visible blocker not pinked:\n%s", v)
	}
	// hid is on a visible row; fold c2 and its count shows, and hid joins the footer
	if f := footerRow(v); strings.Contains(f, "· vis") || !strings.Contains(f, "gone") || strings.Contains(f, "nope") || strings.Contains(f, "· hid") {
		t.Errorf("footer = %q, want gone only (nope is missing from the snapshot, so closed)", f)
	}
	m = press(t, defogAt(t, m, "c2"), runeKey(" "))
	m = defogAt(t, m, "sel")
	v = m.View()
	if !strings.Contains(v, blockerMk.Render("1")) || footerRow(v) != "" && strings.Contains(footerRow(v), "· hid") { // hid shows in Actionable
		t.Errorf("folded Chart lacks count or footer repeats a visible hid:\n%s", v)
	}
	m = press(t, defogAt(t, m, "free"), runeKey("j")) // unblocked: blank gutter, no footer
	m = defogAt(t, m, "free")
	if v := m.View(); strings.Contains(v, blockerMk.Render("!")) || footerRow(v) != "" {
		t.Errorf("unblocked selection highlighted:\n%s", v)
	}
	if v := defogAt(t, m, "i:ini").View(); footerRow(v) != "" {
		t.Errorf("header selection got footer")
	}
}

// Scrolled off the window, a blocker joins the footer and the selection stays in view.
func TestDefogBlockerFooterScrolledOff(t *testing.T) {
	forceANSI256(t)
	m := blockerDefog(t, 12)
	m = defogAt(t, m, "sel")
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "t sel") || !strings.Contains(footerRow(v), "· hid") {
		t.Errorf("view:\n%s", v)
	}
}

// queueRowKey routes enter and b with whatever tab is active (no defogTab/execTab):
// a blocked or paused HITL leaf is refused with its reason, and b follows enter.
func TestQueueRowKeyWithoutQueueTab(t *testing.T) {
	m, _ := defogActModel(t)
	m = press(t, m, runeKey("1")) // the Graph tab: neither a defogTab nor an execTab
	g := m.gateData()
	for _, c := range []struct{ id, say string }{
		{"blk", "blk is blocked by t afk (afk) — not launching"},
		{"paused", "paused is paused (paused:manual) — not launching"},
	} {
		for _, k := range []tea.KeyMsg{enterKey(), runeKey("b")} {
			n, _, ok := m.queueRowKey(k, g.beadByID(c.id), true)
			if !ok || !strings.Contains(statusOf(n.(Model)), c.say) {
				t.Errorf("%s %q: ok=%v status %q, want %q", c.id, k.String(), ok, statusOf(n.(Model)), c.say)
			}
		}
	}
}

// Actionable lists the waiting leaf, the ready one, then the own-paused one; launching,
// live and blocked leaves, AFK children and drained Charts' leaves are out. Each row keeps its
// tree twin, and enter and the hints on it match the twin's.
func TestDefogActionable(t *testing.T) {
	m := defogModel(t, 100, 30)
	var ids []string
	for _, r := range m.tabAt(tabDefog).(defogTab).rows() {
		if r.kind == dAct || r.kind == dACW { // the own-paused Chart "pc" closes the paused band
			ids = append(ids, r.b.ID)
		}
	}
	if got := strings.Join(ids, ","); got != "turn,ready,paused,pc" {
		t.Errorf("Actionable = %s, want turn,ready,paused,pc", got)
	}
	if h := m.tabAt(tabDefog).(defogTab).rows()[0]; h.kind != dSec || h.title != "Actionable (4)" {
		t.Errorf("header = %+v", h)
	}
	if n := m.tabAt(tabDefog).actN(); n != 4 {
		t.Errorf("actN = %d, want 4", n)
	}
	m = press(t, m, runeKey("2"))
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "in → t c") {
		t.Errorf("no in → hint:\n%s", v)
	}
	for _, k := range []string{"turn", "ready"} {
		a, twin := defogAt(t, m, "act:"+k), defogAt(t, m, k)
		if hintsOf(a) != hintsOf(twin) || a.tabAt(tabDefog).(defogTab).detailText() == "" {
			t.Errorf("%s: hints %q vs twin %q", k, hintsOf(a), hintsOf(twin))
		}
		if _, ca := send(t, a, runeKey("enter")); (ca == nil) != func() bool { _, c := send(t, twin, runeKey("enter")); return c == nil }() {
			t.Errorf("%s: enter differs from its twin", k)
		}
	}
	// folding s:act hides its rows, not the count
	f := defogAt(t, m, actKey)
	f = press(t, f, runeKey(" "))
	if n := f.tabAt(tabDefog).actN(); n != 4 || f.tabAt(tabDefog).count() != 1 {
		t.Errorf("fold changed counts: actN %d, count %d", n, f.tabAt(tabDefog).count())
	}
}

func TestDefogActionableEmptyPlaceholder(t *testing.T) {
	g := graphData{beads: []bd.Bead{dbead("c", contract.KindChart, "open", "", 1), dbead("a", contract.KindResearch, "open", "c", 1)}}
	rows := oldDefogList(g, nil)
	if rows[0].kind != dSec || rows[0].title != "Actionable (0)" || rows[1].kind != dPlace {
		t.Errorf("rows = %+v", rows[:2])
	}
}

func TestDefogSaveKeepsFoldedActionable(t *testing.T) {
	dir := t.TempDir()
	old := commonDir
	commonDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { commonDir = old })
	tab := newDefog().(defogTab)
	tab.flip[actKey] = true
	if err := tab.save(); err != nil {
		t.Fatal(err)
	}
	if got := readSection(t, dir, "defog"); got != `{"s:act":true}` {
		t.Errorf("defog section = %s", got)
	}
}
