package tui

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

func titled(s string) func(*bd.Bead) { return func(b *bd.Bead) { b.Title = s } }

// focusFixture is Initiative ini (Billing revamp) with Signals in every state, a
// Chart and a Checkpoint, a Task that holds the Checkpoint's land, and beads in no
// Initiative.
func focusFixture() []bd.Bead {
	bs := []bd.Bead{
		dbead("ini", contract.KindInitiative, "open", "", 1, titled("Billing revamp")),
		dbead("s1", contract.KindSignal, "open", "ini", 1),
		dbead("s2", contract.KindSignal, "open", "ini", 3),        // P3: Ideate's Backlog, but in the focus
		dbead("s3", contract.KindSignal, "in_progress", "ini", 2), // stranded: no session
		dbead("s4", contract.KindSignal, "open", "ini", 2, blockedBy("s1")),
		dbead("s5", contract.KindSignal, "closed", "ini", 2),
		dbead("s6", contract.KindSignal, "deferred", "ini", 2), // a Draft
		dbead("c", contract.KindChart, "open", "ini", 1),
		dbead("g", contract.KindGrilling, "open", "c", 1),
		dbead("cp", contract.KindCheckpoint, "open", "ini", 1, isContainer, blockedBy("task")),
		dbead("mem", contract.KindBuild, "open", "cp", 1),
		dbead("task", contract.KindTask, "open", "", 2),
		dbead("loose", contract.KindSignal, "open", "", 1),
		dbead("lc", contract.KindChart, "open", "", 2),
		dbead("lg", contract.KindGrilling, "open", "lc", 1),
		dbead("ini2", contract.KindInitiative, "open", "", 2, titled("Empty one")),
	}
	for i := range bs {
		if bs[i].Kind == contract.KindInitiative {
			bs[i].Description = "## Done when\n- done\n"
		}
		for n := range 40 { // so the drawer scrolls (a blank line apiece: Markdown joins soft breaks), each line distinct so a scroll shows
			bs[i].Description += fmt.Sprintf("\nbody line %d\n", n)
		}
	}
	return bs
}

func focusModel(t *testing.T, w, h int, beads []bd.Bead, sess map[string]session.Info, live map[string]bool) (Model, *[]string) {
	t.Helper()
	calls := stubActions(t)
	m := sized(t, w, h)
	m.crew = map[string]contract.Crewmate{"luffy": fakeCrew{}, "nami": fakeCrew{}, "franky": fakeCrew{}}
	m, _ = send(t, m, loadedMsg{slowTier: true, beads: beads, live: live, sessions: sess, efforts: map[string]bool{}})
	m = treesOpen(m)
	return m, calls
}

func defaultFocus(t *testing.T) (Model, *[]string) {
	return focusModel(t, 160, 40, focusFixture(), nil, map[string]bool{})
}

// ideaFocus is a model with the focus open on ini, from its Ideate header.
func ideaFocus(t *testing.T, m Model) Model {
	t.Helper()
	m = press(t, m, runeKey("1"))
	m = gotoFocus(t, ideaAt(t, m, "i:ini"))
	if !focused(m) {
		t.Fatalf("g f on the Ideate header did not open the focus:\n%s", ansi.Strip(m.View()))
	}
	return m
}

func focused(m Model) bool { return strings.Contains(backBarOf(m), "← esc") }

func backBarOf(m Model) string { return lines(m.View())[1] }

// cursorLine is the highlighted row's text.
func cursorLine(m Model) string {
	for _, l := range lines(m.View()) {
		if strings.HasPrefix(l, "❯") {
			return l
		}
	}
	return ""
}

// focusAt moves the focus cursor to the row whose text contains want.
func focusAt(t *testing.T, m Model, want string) Model {
	t.Helper()
	for range 30 {
		if strings.Contains(cursorLine(m), want) {
			return m
		}
		m = press(t, m, runeKey("j"))
	}
	t.Fatalf("no focus row %q; at %q", want, cursorLine(m))
	return m
}

var oldBackBarKey = regexp.MustCompile(`from \d `)

// i opens the Initiative's focus from the header, a member, a leaf, a Checkpoint member
// and a side Task, on each tab; the back bar names it and where esc returns.
func TestFocusEntry(t *testing.T) {
	m, _ := defaultFocus(t)
	exec := func(m Model) Model { // unfold cp so its member has a row
		return press(t, execAt(t, press(t, m, runeKey("3")), "cp"), runeKey(" "))
	}
	for _, c := range []struct {
		name, from string
		go_        func(Model) Model
	}{
		{"Graph head", "from Graph", func(m Model) Model { return selectBead(t, press(t, m, runeKey("4")), "ini") }},
		{"Graph member", "from Graph", func(m Model) Model { return selectBead(t, press(t, m, runeKey("4")), "mem") }},
		{"Ideate header", "from Ideate", func(m Model) Model { return ideaAt(t, press(t, m, runeKey("1")), "i:ini") }},
		{"Ideate Signal", "from Ideate", func(m Model) Model { return ideaAt(t, press(t, m, runeKey("1")), "s1") }},
		{"Defog header", "from Defog", func(m Model) Model { return defogAt(t, press(t, m, runeKey("2")), "i:ini") }},
		{"Defog Chart", "from Defog", func(m Model) Model { return defogAt(t, press(t, m, runeKey("2")), "c") }},
		{"Defog leaf", "from Defog", func(m Model) Model { return defogAt(t, press(t, m, runeKey("2")), "g") }},
		{"Execute header", "from Execute", func(m Model) Model { return execAt(t, press(t, m, runeKey("3")), "i:ini") }},
		{"Execute Checkpoint member", "from Execute", func(m Model) Model { return execAt(t, exec(m), "mem") }},
		{"Initiatives row", "from Initiatives", func(m Model) Model { return press(t, press(t, m, runeKey("5")), runeKey("j")) }},
		{"Execute side Task", "from Execute", func(m Model) Model { return execAt(t, press(t, m, runeKey("3")), "side:task") }},
	} {
		n := gotoFocus(t, c.go_(m))
		if bar := backBarOf(n); !strings.Contains(bar, "Billing revamp") || !strings.Contains(bar, c.from) {
			t.Errorf("%s: back bar %q, want Billing revamp and %q", c.name, bar, c.from)
		} else if oldBackBarKey.MatchString(bar) {
			t.Errorf("%s: back bar %q still carries the tab's number key", c.name, bar)
		}
	}
}

// A row in no Initiative refuses, and nothing opens; i does nothing inside the focus.
func TestFocusEntryRefusalAndNoNesting(t *testing.T) {
	m, _ := defaultFocus(t)
	for name, n := range map[string]Model{
		"Ideate loose Signal": ideaAt(t, press(t, m, runeKey("1")), "loose"),
		"Defog loose Chart":   defogAt(t, press(t, m, runeKey("2")), "lc"),
		"Graph loose Chart":   selectBead(t, press(t, m, runeKey("4")), "lc"),
	} {
		if menu := ansi.Strip(press(t, n, runeKey("g")).View()); gotoFocusRow.MatchString(menu) {
			t.Errorf("%s: g offers f focus:\n%s", name, menu)
		}
		if n = gotoFocus(t, n); focused(n) {
			t.Errorf("%s: g f opened a focus", name)
		}
	}
	f := ideaFocus(t, m)
	f = focusAt(t, f, "t s1")
	if n := press(t, f, runeKey("i")); n.View() != f.View() {
		t.Errorf("i inside the focus changed the frame:\n%s", ansi.Strip(n.View()))
	}
	if menu := ansi.Strip(press(t, f, runeKey("g")).View()); gotoFocusRow.MatchString(menu) { // no nesting (ADR 0174 §1)
		t.Errorf("g offers f focus inside the focus:\n%s", menu)
	}
}

// esc closes the drawer first, then leaves with the origin tab exactly as it was; a tab
// key leaves too; re-entry starts fresh.
func TestFocusLeaving(t *testing.T) {
	m, _ := defaultFocus(t)
	m = press(t, m, runeKey("1"))
	m = press(t, ideaAt(t, m, "s4"), runeKey("d")) // origin: cursor on s4 with its detail open
	origin := m.View()
	f := gotoFocus(t, m)
	f = focusAt(t, f, "t s1")
	f = press(t, f, runeKey("d"))
	if !strings.Contains(strings.Join(lines(f.View()), "\n"), "──────") {
		t.Fatalf("d did not open the drawer:\n%s", ansi.Strip(f.View()))
	}
	f = press(t, f, escKey())
	if !focused(f) || strings.Contains(strings.Join(lines(f.View())[3:], "\n"), "──────") {
		t.Errorf("first esc should close the drawer only:\n%s", ansi.Strip(f.View()))
	}
	f = press(t, f, escKey())
	if focused(f) || f.View() != origin {
		t.Errorf("second esc did not restore the origin tab:\n%s\nwant\n%s", ansi.Strip(f.View()), ansi.Strip(origin))
	}
	for _, k := range []string{"4", "tab"} {
		kk := runeKey(k)
		if k == "tab" {
			kk = tea.KeyMsg{Type: tea.KeyTab}
		}
		if n := press(t, gotoFocus(t, m), kk); focused(n) {
			t.Errorf("%s did not leave the focus", k)
		}
	}
	if n := press(t, gotoFocus(t, m), runeKey("4")); activeTab(t, n) != "Graph" {
		t.Errorf("4 landed on %s", activeTab(t, n))
	}
	// fresh on re-entry: the cursor is on the header and folds are at their defaults
	g := focusAt(t, gotoFocus(t, m), "t s1")
	g = press(t, focusAt(t, g, "1 closed"), runeKey(" "))
	g = gotoFocus(t, press(t, g, escKey()))
	if c := cursorLine(g); !strings.Contains(c, "Billing revamp") || strings.Contains(ansi.Strip(g.View()), "t s5") {
		t.Errorf("re-entry not fresh: cursor %q\n%s", c, ansi.Strip(g.View()))
	}
}

// The back bar counts only the Initiative's scope; ●N is waiting sessions alone; the
// header drops its counts while focused and no tab badge changes.
func TestFocusChrome(t *testing.T) {
	sess := map[string]session.Info{
		"s1":    {State: session.Alive, Waiting: session.WaitTurn},
		"g":     {State: session.Alive},
		"task":  {State: session.Alive, Waiting: session.WaitAsk}, // waits on the Checkpoint: in scope
		"loose": {State: session.Alive, Waiting: session.WaitTurn},
		"lg":    {State: session.Alive},
	}
	m, _ := focusModel(t, 160, 40, focusFixture(), sess, map[string]bool{"mem": true, "other": true})
	if h := lines(m.View())[0]; !strings.Contains(h, "5 HITL") || !strings.Contains(h, "2 AFK") {
		t.Fatalf("header = %q, want the global counts", h)
	}
	badges := func(m Model) (bs []string) {
		for i := range m.tabs {
			bs = append(bs, tabLabel(i, m.tabAt(i)))
		}
		return
	}
	before := badges(m)
	f := ideaFocus(t, m)
	bar := backBarOf(f)
	for _, want := range []string{"← esc back", "Billing revamp", "●2", "3 HITL", "1 AFK", "from Ideate"} {
		if !strings.Contains(bar, want) {
			t.Errorf("back bar %q lacks %q", bar, want)
		}
	}
	if strings.Contains(bar, "waiting") {
		t.Errorf("back bar %q shows N waiting", bar)
	}
	if h := lines(f.View())[0]; strings.Contains(h, "HITL") || strings.Contains(h, "AFK") || !strings.Contains(h, "loop") {
		t.Errorf("focused header = %q, want the loop segment alone", h)
	}
	if !slices.Equal(before, badges(f)) {
		t.Errorf("a tab badge changed: %v -> %v", before, badges(f))
	}
	quiet, _ := focusModel(t, 160, 40, focusFixture(), nil, map[string]bool{})
	if bar := backBarOf(ideaFocus(t, quiet)); strings.Contains(bar, "●") {
		t.Errorf("back bar %q shows ●N at zero", bar)
	}
	t.Run("title truncates before the counts", func(t *testing.T) {
		n, _ := focusModel(t, 50, 20, focusFixture(), sess, map[string]bool{})
		n = press(t, n, runeKey("1"))
		n = gotoFocus(t, ideaAt(t, n, "i:ini"))
		if bar := backBarOf(n); !strings.Contains(bar, "from Ideate") || !strings.Contains(bar, "HITL") {
			t.Errorf("narrow back bar %q lost its right side", bar)
		}
	})
}

// The frame is exactly h lines and no line is wider than w, with the focus open (and ?).
func TestFocusFrameFits(t *testing.T) {
	for _, sz := range [][2]int{{40, 12}, {84, 24}, {200, 50}} {
		m, _ := focusModel(t, sz[0], sz[1], focusFixture(), nil, map[string]bool{})
		m = ideaFocus(t, m)
		for _, mm := range []Model{m, press(t, m, runeKey("d")), press(t, m, runeKey("?"))} {
			ls := lines(mm.View())
			if len(ls) != sz[1] {
				t.Fatalf("%dx%d: %d lines", sz[0], sz[1], len(ls))
			}
			for _, l := range ls {
				if lipgloss.Width(l) > sz[0] {
					t.Errorf("%dx%d: line %q is %d wide", sz[0], sz[1], l, lipgloss.Width(l))
				}
			}
		}
		ls := lines(m.View())
		if !strings.HasPrefix(ls[0], " mugicrew") || !strings.Contains(ls[1], "← esc") || !strings.HasPrefix(ls[2], "───") || !strings.Contains(ls[len(ls)-1], "?") {
			t.Errorf("%dx%d: chrome is %q %q %q ... %q", sz[0], sz[1], ls[0], ls[1], ls[2], ls[len(ls)-1])
		}
	}
}

// Every Signal of the Initiative is listed in beadCmp order, P3 and stranded included,
// closed ones fold, Drafts and other Initiatives' beads are out, and the empty Defog
// and Execute sections show their headers and placeholders.
func TestFocusRows(t *testing.T) {
	m, _ := defaultFocus(t)
	f := ideaFocus(t, m)
	v := ansi.Strip(f.View())
	at := func(s string) int { return strings.Index(v, s) }
	order := []string{"Billing revamp", "Ideate  4 open", "t s1", "t s3", "t s4", "t s2", "▸ 1 closed", "Defog  1 open", "Execute  2 open", "t cp", "t task"}
	last := -1
	for _, s := range order {
		if i := at(s); i <= last {
			t.Errorf("%q at %d, want after %d:\n%s", s, i, last, v)
		} else {
			last = i
		}
	}
	for _, s := range []string{"t s5", "t s6", "t loose", "Empty one"} {
		if at(s) >= 0 {
			t.Errorf("frame lists %q:\n%s", s, v)
		}
	}
	f = press(t, focusAt(t, f, "1 closed"), runeKey(" "))
	if !strings.Contains(ansi.Strip(f.View()), "t s5") {
		t.Errorf("space did not unfold the closed Signal:\n%s", ansi.Strip(f.View()))
	}
	// an Initiative with no members shows three placeholders
	g := gotoFocus(t, selectBead(t, press(t, m, runeKey("4")), "ini2"))
	if v := ansi.Strip(g.View()); !strings.Contains(v, "no Signals") || !strings.Contains(v, "no Charts") || !strings.Contains(v, "no Specs or Checkpoints") {
		t.Errorf("empty Initiative lacks placeholders:\n%s", v)
	}
}

// j and k skip the blank rows between sections.
func TestFocusCursorSkipsGaps(t *testing.T) {
	m, _ := defaultFocus(t)
	f := ideaFocus(t, m)
	seen := map[string]bool{}
	for range 14 {
		seen[strings.TrimSpace(strings.TrimLeft(ansi.Strip(cursorLine(f)), "❯"))] = true
		f = press(t, f, runeKey("j"))
	}
	if seen[""] {
		t.Errorf("the cursor landed on a blank row: %v", seen)
	}
}

// An Ideate row routes as on Ideate: a blocked Signal gets the y/n, b on an alive
// session refuses. The header's enter launches Luffy's cut session; n, a, A and p do
// nothing; every hints bar ends esc back · ? keys.
func TestFocusKeys(t *testing.T) {
	sess := map[string]session.Info{"s1": {State: session.Alive}}
	m, calls := focusModel(t, 160, 40, focusFixture(), sess, map[string]bool{})
	f := ideaFocus(t, m)

	if h := hintsOf(f); h != "enter launch · b bg · d detail · X cancel · c checkup · t transcript · r retrospective · esc back" {
		t.Errorf("header hints = %q", h)
	}
	n, cmd := send(t, f, enterKey())
	_ = settle(t, n, cmd)
	if !slices.Contains(*calls, "startSession ini") {
		t.Errorf("header enter did not launch the cut session: %v", *calls)
	}

	s4 := focusAt(t, f, "t s4")
	if n, _ := send(t, s4, enterKey()); len(n.modals) != 1 || !strings.Contains(n.modals[0].title, "blocked") {
		t.Errorf("enter on a blocked Signal: modals %+v", n.modals)
	}
	s1 := focusAt(t, f, "t s1")
	if n, _ := send(t, s1, runeKey("b")); !strings.Contains(statusOf(n), "already running — enter attaches") {
		t.Errorf("b on a live Signal: %q", statusOf(n))
	}
	for _, id := range []string{"head", "t s1"} {
		at := f
		if id != "head" {
			at = s1
		}
		for _, k := range []string{"a", "A", "i"} {
			if n, cmd := send(t, at, runeKey(k)); cmd != nil || n.View() != at.View() {
				t.Errorf("%s: %s did something:\n%s", id, k, ansi.Strip(n.View()))
			}
		}
	}
	for _, at := range []Model{f, s1, s4, press(t, f, runeKey("d")), focusAt(t, f, "Defog")} {
		if h := hintsOf(at); !strings.HasSuffix(h, "esc back") {
			t.Errorf("hints %q do not end esc back", h)
		}
		l := lines(at.View())
		if bar := strings.TrimRight(l[len(l)-1], " "); !strings.HasSuffix(bar, "esc back | ? keys") {
			t.Errorf("bar %q does not end esc back | ? keys", bar)
		}
	}
	if h := hintsOf(s1); h != "enter triage · d detail · z reject · p pause · x drop · t transcript · e edit · +/- prio · X withdraw · c checkup · r retrospective · esc back" {
		t.Errorf("Signal hints = %q", h)
	}
	for k, want := range map[string]string{"e": "Edit", "z": "Reject"} {
		if n, _ := send(t, s4, runeKey(k)); len(n.modals) != 1 || !strings.Contains(n.modals[0].title, want) {
			t.Errorf("%s on a Signal: modals %+v", k, n.modals)
		}
	}
	stubExec(t)
	t.Setenv("EXPLORER", "ls")
	if n, _ := send(t, s4, runeKey("E")); len(n.modals) != 0 {
		t.Errorf("E on a Signal: modals %+v", n.modals)
	}
}

var isRef = regexp.MustCompile(`i +focus`)            // i in the ? reference
var gotoFocusRow = regexp.MustCompile(`(^|\s)f +focus`) // the goto menu row

// ? is titled for the focus and lists its keys, i not among them on any tab.
func TestFocusKeyReference(t *testing.T) {
	m, _ := defaultFocus(t)
	f := press(t, ideaFocus(t, m), runeKey("?"))
	v := ansi.Strip(f.View())
	if !strings.Contains(v, "keys — Ideate › focus") || !strings.Contains(v, "esc") || !strings.Contains(v, "j/k") || isRef.MatchString(v) {
		t.Errorf("focus reference:\n%s", v)
	}
	for _, tab := range []string{"1", "2", "3", "4"} {
		if isRef.MatchString(ansi.Strip(press(t, press(t, m, runeKey(tab)), runeKey("?")).View())) {
			t.Errorf("? on tab %s still lists i", tab)
		}
	}
}

// The view stays open when the Initiative closes, shows a line when it leaves the
// snapshot, and the cursor moves to the row above when its row is gone.
func TestFocusChanges(t *testing.T) {
	m, _ := defaultFocus(t)
	f := focusAt(t, ideaFocus(t, m), "t s3")

	without := func(drop string) []bd.Bead {
		return slices.DeleteFunc(focusFixture(), func(b bd.Bead) bool { return b.ID == drop })
	}
	n, _ := send(t, f, loadedMsg{beads: without("s3"), live: map[string]bool{}})
	if c := cursorLine(n); !strings.Contains(c, "t s1") {
		t.Errorf("cursor after its row vanished = %q, want t s1", c)
	}

	closed := focusFixture()
	for i := range closed {
		if closed[i].ID == "ini" || closed[i].Parent == "ini" && closed[i].Status != "deferred" {
			closed[i].Status = "closed"
		}
	}
	n, _ = send(t, f, loadedMsg{beads: closed, live: map[string]bool{}})
	if v := ansi.Strip(n.View()); !focused(n) || !strings.Contains(v, "▸ 5 closed") || strings.Contains(v, "t s1") {
		t.Errorf("closed Initiative:\n%s", v)
	}

	n, _ = send(t, f, loadedMsg{beads: without("ini"), live: map[string]bool{}})
	if v := ansi.Strip(n.View()); !strings.Contains(v, "the Initiative left the graph — esc returns") {
		t.Errorf("gone Initiative:\n%s", v)
	}
	if n = press(t, n, runeKey("j")); !focused(n) {
		t.Error("a key left the view when the Initiative was gone")
	}
	if n = press(t, n, escKey()); focused(n) {
		t.Error("esc did not pop the gone Initiative's view")
	}
}

// Handled == bound over every candidate key, on the header, a section, a Signal, a
// fold and a placeholder, with the drawer shut and open.
func TestFocusBindingCoverage(t *testing.T) {
	m, _ := defaultFocus(t)
	f := ideaFocus(t, m)
	for _, row := range []string{"Billing revamp", "Ideate", "t s1", "t s4", "1 closed", "t cp ", "t task"} {
		at := focusAt(t, f, row)
		checkCoverage(t, "focus "+row, at, false)
		open := press(t, press(t, at, runeKey("d")), tea.KeyMsg{Type: tea.KeyCtrlD}) // mid-scroll, so up and down both move
		checkCoverage(t, "focus "+row+" drawer", open, false)
	}
	checkCoverage(t, "focus gone", func() Model {
		n, _ := send(t, f, loadedMsg{beads: nil, live: map[string]bool{}})
		return n
	}(), false)
}

// Rendering and dialogs with the focus open start no subprocess (ADR 0101 §4).
func TestFocusRunsNoSubprocess(t *testing.T) {
	log := fakeTools(t)
	m, _ := defaultFocus(t)
	f := ideaFocus(t, m)
	f.View()
	press(t, f, runeKey("?")).View()
	press(t, f, runeKey("d")).View()
	if d, _ := send(t, focusAt(t, f, "t s4"), enterKey()); len(d.modals) == 1 {
		d.View()
	} else {
		t.Fatalf("no dialog: %+v", d.modals)
	}
	if got, _ := os.ReadFile(log); len(got) > 0 {
		t.Fatalf("the focus started subprocesses:\n%s", got)
	}
}

// The Defog section lists open and defogged-unlanded Charts with their leaves in the
// tab's order, folds the closed ones, refuses a blocked leaf and runs an AFK child now.
func TestFocusDefogSection(t *testing.T) {
	bs := focusFixture()
	bs = append(bs,
		dbead("dc", contract.KindChart, "open", "ini", 2, labeled("chart:defogged")),
		dbead("xc", contract.KindChart, "closed", "ini", 2),
		dbead("kc", contract.KindResearch, "closed", "c", 1),
		dbead("kb", contract.KindGrilling, "open", "c", 2, blockedBy("g")),
		dbead("ka", contract.KindBuild, "open", "c", 2),
	)
	m, _ := focusModel(t, 160, 40, bs, nil, map[string]bool{})
	f := gotoFocus(t, ideaAt(t, press(t, m, runeKey("1")), "i:ini"))
	text := ansi.Strip(f.View())
	order := []string{"Defog", "c ", "g", "kb", "ka", "1 closed", "dc", "defogged, awaiting land", "1 closed"}
	at := strings.Index(text, "Defog")
	for _, w := range order[1:] {
		j := strings.Index(text[at:], w)
		if j < 0 {
			t.Fatalf("Defog section lacks %q in order:\n%s", w, text)
		}
		at += j + len(w)
	}
	if strings.Contains(text, "kc") || strings.Contains(text, "xc") {
		t.Errorf("closed beads show before their folds open:\n%s", text)
	}
	f = press(t, focusAt(t, f, "1 closed"), runeKey(" "))
	_ = f

	b := focusAt(t, gotoFocus(t, ideaAt(t, press(t, m, runeKey("1")), "i:ini")), "kb")
	b = press(t, b, tea.KeyMsg{Type: tea.KeyEnter})
	if s := statusOf(b); !strings.Contains(s, "g") || strings.Contains(ansi.Strip(b.View()), "y/n") {
		t.Errorf("blocked leaf enter: status %q", s)
	}
	if h := lines(b.View()); !strings.HasSuffix(strings.TrimSpace(ansi.Strip(h[len(h)-1])), "esc back | ? keys") {
		t.Errorf("hints = %q", h[len(h)-1])
	}
	a := focusAt(t, b, "ka")
	if h := lines(a.View()); !strings.Contains(h[len(h)-1], "enter run") {
		t.Errorf("AFK child hints = %q", h[len(h)-1])
	}
	if _, cmd := send(t, a, tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil {
		t.Errorf("enter on an AFK child did not run it now")
	}
	d := press(t, b, runeKey("d"))
	if !strings.Contains(ansi.Strip(d.View()), "blocked by") {
		t.Errorf("d did not open the Defog detail:\n%s", ansi.Strip(d.View()))
	}
}

// Execute lists Checkpoints, then Specs, then side rows with their hints; Checkpoints
// start folded; closed Checkpoints and Specs and a Checkpoint's closed members fold;
// Drafts and unadopted Rollbacks are out.
func TestFocusExecuteRows(t *testing.T) {
	bs := append(focusFixture(),
		dbead("sp", contract.KindSpec, "open", "ini", 1),
		dbead("cp2", contract.KindCheckpoint, "closed", "ini", 1, isContainer),
		dbead("dcp", contract.KindCheckpoint, "deferred", "ini", 1, isContainer),
		dbead("rb", contract.KindCheckpoint, "open", "", 1, isContainer),
		dbead("done", contract.KindBuild, "closed", "cp", 1),
	)
	m, _ := focusModel(t, 160, 50, bs, nil, map[string]bool{})
	f := ideaFocus(t, m)
	v := ansi.Strip(f.View())
	last := -1
	for _, s := range []string{"Execute  3 open", "t cp ", "t sp ", "t task", "holds land of → t cp"} {
		if i := strings.Index(v, s); i <= last {
			t.Errorf("%q at %d, want after %d:\n%s", s, i, last, v)
		} else {
			last = i
		}
	}
	for _, s := range []string{"t mem", "t cp2", "t dcp", "t rb"} {
		if strings.Contains(v, s) {
			t.Errorf("frame lists %q:\n%s", s, v)
		}
	}
	f = press(t, focusAt(t, f, "t cp "), runeKey(" ")) // unfold the Checkpoint
	f = press(t, focusAt(t, f, "t mem"), runeKey("j"))
	if !strings.Contains(cursorLine(f), "1 closed") {
		t.Errorf("cursor at %q, want the Checkpoint's closed fold", cursorLine(f))
	}
	f = press(t, press(t, ideaFocus(t, m), runeKey("k")), runeKey(" ")) // k wraps to the section's closed fold
	if v := ansi.Strip(f.View()); !strings.Contains(v, "t cp2") {
		t.Errorf("section fold did not list the closed Checkpoint:\n%s", v)
	}
}

// A Task in X that holds Y's Checkpoint is in X's focus with the hint naming it, not in
// Y's, where the Checkpoint shows blocked and its detail names the blocker.
func TestFocusExecuteCrossInitiative(t *testing.T) {
	bs := []bd.Bead{
		dbead("x", contract.KindInitiative, "open", "", 1, titled("X side")),
		dbead("y", contract.KindInitiative, "open", "", 2, titled("Y side")),
		dbead("cpy", contract.KindCheckpoint, "open", "y", 1, isContainer, blockedBy("tx")),
		dbead("tx", contract.KindTask, "open", "x", 1),
	}
	m, _ := focusModel(t, 160, 40, bs, nil, map[string]bool{})
	open := func(id string) Model {
		return gotoFocus(t, selectBead(t, press(t, m, runeKey("4")), id))
	}
	xv := ansi.Strip(open("x").View())
	if !strings.Contains(xv, "t tx") || !strings.Contains(xv, "holds land of → t cpy") {
		t.Errorf("X's focus lacks the Task with its hint:\n%s", xv)
	}
	y := open("y")
	if yv := ansi.Strip(y.View()); strings.Contains(yv, "t tx") || !strings.Contains(yv, "t cpy") {
		t.Errorf("Y's focus:\n%s", yv)
	}
	y = press(t, focusAt(t, y, "t cpy"), runeKey("d"))
	if v := ansi.Strip(y.View()); !strings.Contains(v, "blocked by") || !strings.Contains(v, "tx") {
		t.Errorf("Checkpoint detail does not name its blocker:\n%s", v)
	}
	// g f on the side Task in Execute opens its own Initiative's focus
	e := execAt(t, press(t, m, runeKey("3")), "side:tx")
	if bar := backBarOf(gotoFocus(t, e)); !strings.Contains(bar, "X side") {
		t.Errorf("g f on the side Task: %q", bar)
	}
}

// A blocked Execute leaf's enter is refused, naming its blocker; n and a do nothing;
// hints end esc back.
func TestFocusExecuteKeys(t *testing.T) {
	bs := append(focusFixture(),
		dbead("ap", contract.KindApproval, "open", "cp", 1, blockedBy("mem")))
	m, calls := focusModel(t, 160, 50, bs, nil, map[string]bool{})
	f := ideaFocus(t, m)
	f = press(t, focusAt(t, f, "t cp "), runeKey(" "))
	f = focusAt(t, f, "t ap")
	n, cmd := send(t, f, enterKey())
	if cmd != nil || len(n.modals) != 0 || !strings.Contains(statusOf(n), "is blocked by") {
		t.Errorf("enter on a blocked leaf: status %q modals %v", statusOf(n), n.modals)
	}
	for _, k := range []string{"a", "A", "i"} {
		if n, cmd := send(t, f, runeKey(k)); cmd != nil || n.View() != f.View() {
			t.Errorf("%s did something:\n%s", k, ansi.Strip(n.View()))
		}
	}
	if h := hintsOf(f); !strings.HasSuffix(h, "esc back") || !strings.Contains(h, "d detail") {
		t.Errorf("hints = %q", h)
	}
	if len(*calls) != 0 {
		t.Errorf("calls %v", *calls)
	}
}

// The focus inherits the blocker highlight: a drawn blocker gets the ! gutter, also
// cp's Task drawn as a side row though it is in no Initiative.
func TestFocusBlockerHighlight(t *testing.T) {
	m, _ := defaultFocus(t)
	m = focusAt(t, ideaFocus(t, m), "s4") // blocked by s1, drawn
	marked := 0
	for _, l := range lines(m.View()) {
		if l = ansi.Strip(l); strings.HasPrefix(l, "!") != strings.Contains(l, " s1") {
			t.Errorf("gutter on wrong row: %q", l)
		} else if strings.HasPrefix(l, "!") {
			marked++
		}
	}
	if marked != 1 { // a drawn blocker is not repeated in the footer
		t.Errorf("s1 shown %d times", marked)
	}
	m = focusAt(t, m, "CHECKPOINT")
	for _, l := range lines(m.View()) {
		if l = ansi.Strip(l); strings.Contains(l, "t task") && !strings.HasPrefix(l, "!") {
			t.Errorf("side Task not marked: %q", l)
		}
	}
}

// p on a focus-view Ideate Signal toggles paused:manual as Graph's p does; the header ignores it.
func TestFocusIdeatePause(t *testing.T) {
	bs := focusFixture()
	bs[1].Labels = []string{"paused:manual"} // s1
	m, calls := focusModel(t, 160, 40, bs, nil, map[string]bool{})
	f := ideaFocus(t, m)
	s1 := focusAt(t, f, "t s1")
	if h := hintsOf(s1); !strings.Contains(h, "z reject · p unpause · t transcript · e edit") {
		t.Errorf("hints %q lack p unpause", h)
	}
	n, cmd := send(t, s1, runeKey("p"))
	n = settle(t, n, cmd)
	if !has(*calls, "unpauseBead s1") || !strings.Contains(statusOf(n), "unpaused s1") {
		t.Errorf("unpause: calls %v status %q", *calls, statusOf(n))
	}
	n, cmd = send(t, focusAt(t, f, "t s2"), runeKey("p"))
	n = settle(t, n, cmd)
	if !has(*calls, "pauseBead s2") || !strings.Contains(statusOf(n), "paused s2") {
		t.Errorf("pause: calls %v status %q", *calls, statusOf(n))
	}
	k := len(*calls)
	if _, cmd := send(t, f, runeKey("p")); cmd != nil || len(*calls) != k {
		t.Error("p on the focus header did something")
	}
}

// An open Chart in the focus draws ▾ and a space-folded one ▸: the glyph follows the
// focus's own fold default, not Defog's.
func TestFocusChartGlyphFollowsFocusFold(t *testing.T) {
	m, _ := defaultFocus(t)
	m = ideaFocus(t, m)
	m = focusAt(t, m, "CHART")
	if l := cursorLine(m); !strings.Contains(l, "▾") {
		t.Fatalf("open Chart should show ▾:\n%s", l)
	}
	m = press(t, m, runeKey(" "))
	m = focusAt(t, m, "CHART")
	if l := cursorLine(m); !strings.Contains(l, "▸") {
		t.Fatalf("folded Chart should show ▸:\n%s", l)
	}
}

// A focus row's prefix is gutter + indent + a 2-column caret slot, blank on a row that
// cannot fold, so a member Signal starts 2 columns right of the Ideate header's caret and
// the closed fold lines up with its leaf siblings.
func TestFocusIdeateRowsReserveCaretSlot(t *testing.T) {
	m, _ := defaultFocus(t)
	v := ansi.Strip(ideaFocus(t, m).View())
	col := func(s string) int {
		for _, l := range strings.Split(v, "\n") {
			if i := strings.Index(l, s); i >= 0 {
				return len([]rune(l[:i]))
			}
		}
		t.Fatalf("no line %q:\n%s", s, v)
		return 0
	}
	lead := func(s string) int { // where the row's first glyph starts, past the gutter
		for _, l := range strings.Split(v, "\n") {
			if strings.Contains(l, s) {
				return 1 + len(l[1:]) - len(strings.TrimLeft(l[1:], " "))
			}
		}
		t.Fatalf("no line %q", s)
		return 0
	}
	caret := col("▾ Ideate")
	if got, want := lead("t s1"), caret+4; got != want { // slot 2 under the header's caret, then the glyph
		t.Errorf("member Signal glyph at column %d, want %d (header caret %d)", got, want, caret)
	}
	if got, want := col("▸ 1 closed"), caret+2; got != want {
		t.Errorf("closed fold at column %d, want %d", got, want)
	}
}

// A Chart's leaf and AFK child in the focus view's Defog section start 2 columns right of
// the Chart's own glyph, 4 past its caret.
func TestFocusDefogChildIndent(t *testing.T) {
	bs := append(focusFixture(), dbead("ka", contract.KindBuild, "open", "c", 2))
	m, _ := focusModel(t, 160, 40, bs, nil, map[string]bool{})
	f := gotoFocus(t, ideaAt(t, press(t, m, runeKey("1")), "i:ini"))
	var chart, leaf, afk string
	for _, l := range lines(ansi.Strip(f.View())) {
		switch {
		case strings.Contains(l, "ka"):
			afk = l
		case strings.Contains(l, " g") && !strings.Contains(l, "goto"):
			leaf = l
		case strings.Contains(l, "▾ ") && strings.Contains(l, "c "):
			chart = l
		}
	}
	col := func(l string) int { return len(l) - len(strings.TrimLeft(l, " │")) }
	if chart == "" || leaf == "" || afk == "" {
		t.Fatalf("rows not found: %q %q %q", chart, leaf, afk)
	}
	if col(leaf) != col(chart)+4 || col(afk) != col(chart)+4 {
		t.Errorf("columns chart %d leaf %d afk %d:\n%q\n%q\n%q", col(chart), col(leaf), col(afk), chart, leaf, afk)
	}
}
