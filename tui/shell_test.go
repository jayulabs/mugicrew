package tui

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/chopper"
	"jayulabs.org/mugicrew/contract"
)

var mirrorBeads []bd.Bead

func send(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	mirrorBeads = m.beads // the default bd stub mirrors the model, so a Graph-entry reload keeps a fixture
	if l, ok := msg.(loadedMsg); ok {
		mirrorBeads = l.beads
	}
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func runeKey(k string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)} }

func sized(t *testing.T, w, h int) Model {
	t.Helper()
	m, _ := send(t, New(nil, nil), tea.WindowSizeMsg{Width: w, Height: h})
	m.loading = false // idle: Init's startup load has landed
	return m
}

func lines(v string) []string { return strings.Split(ansi.Strip(v), "\n") }

// activeTab is the tab whose folder is open: its label sits in the tab bar's
// label row and the "keys — <Name>" reference title names it once ? is open.
func activeTab(t *testing.T, m Model) string {
	t.Helper()
	m, _ = send(t, m, runeKey("?"))
	for _, l := range lines(m.View()) {
		if _, name, ok := strings.Cut(l, "keys — "); ok {
			name, _, _ = strings.Cut(name, "│")
			return strings.TrimSpace(name)
		}
	}
	t.Fatal("no key reference title")
	return ""
}

// labels is the nine tabs' labels with no badge.
var labels = []string{"1 Ideate", "2 Defog", "3 Execute", "4 Graph", "5 Initiatives", "6 History", "# Stats", "$ Logs", "* Settings"}

// The tab bar slides a window of whole tabs that always holds the active one,
// with ‹/› exactly on the clipped sides (FR8); all tabs fitting shows neither.
func TestTabBarWindow(t *testing.T) {
	for _, c := range []struct {
		w, active       int
		left, right     bool
		visible, hidden []string
	}{
		{120, 0, false, false, labels, nil},
		{120, 8, false, false, labels, nil},
		{40, 0, false, true, []string{"1 Ideate", "2 Defog"}, []string{"* Settings"}},
		{40, 8, true, false, []string{"* Settings"}, []string{"1 Ideate"}},
		{40, 4, true, true, []string{"5 Initiatives"}, []string{"1 Ideate", "* Settings"}},
		{60, 5, true, true, []string{"5 Initiatives", "6 History"}, []string{"1 Ideate", "$ Logs"}},
		{40, 3, true, true, []string{"4 Graph"}, []string{"1 Ideate", "* Settings"}},
	} {
		bar := lines(tabBar(labels, c.active, c.w))
		if len(bar) != 3 {
			t.Fatalf("w=%d: tab bar is %d rows, want 3", c.w, len(bar))
		}
		for _, l := range bar {
			if lipgloss.Width(l) != c.w {
				t.Errorf("w=%d active=%d: row %q is %d wide", c.w, c.active, l, lipgloss.Width(l))
			}
		}
		row := bar[1]
		if strings.Contains(row, "‹") != c.left || strings.Contains(row, "›") != c.right {
			t.Errorf("w=%d active=%d: markers in %q, want ‹=%v ›=%v", c.w, c.active, row, c.left, c.right)
		}
		if !strings.Contains(row, labels[c.active]) {
			t.Errorf("w=%d: active %q not visible in %q", c.w, labels[c.active], row)
		}
		for _, l := range c.visible {
			if !strings.Contains(row, l) {
				t.Errorf("w=%d active=%d: %q missing from %q", c.w, c.active, l, row)
			}
		}
		for _, l := range c.hidden {
			if strings.Contains(row, l) {
				t.Errorf("w=%d active=%d: %q should be clipped from %q", c.w, c.active, l, row)
			}
		}
	}
}

// Every width from the floor up keeps the active tab visible.
func TestTabBarActiveAlwaysVisible(t *testing.T) {
	for w := floorCols; w <= 100; w++ {
		for a := range labels {
			if row := lines(tabBar(labels, a, w))[1]; !strings.Contains(row, labels[a]) {
				t.Fatalf("w=%d: active %q not in %q", w, labels[a], row)
			}
		}
	}
}

// The label is "<digit> <Name>" plus ●N when sessions are waiting on you (FR9, FR10).
func TestTabLabelBadge(t *testing.T) {
	if got := ansi.Strip(tabLabel(2, placeholder{name: "Execute"})); got != "3 Execute" {
		t.Errorf("zero count label = %q, want 3 Execute", got)
	}
	if got := ansi.Strip(tabLabel(1, counted{placeholder{name: "Defog"}, 3})); got != "2 Defog ●3" {
		t.Errorf("badged label = %q, want 2 Defog ●3", got)
	}
}

// Amber ●N while sessions wait, else light-blue ●N for the Actionable rows, else none.
func TestTabLabelTwoTone(t *testing.T) {
	forceANSI256(t)
	for _, c := range []struct {
		n, act int
		want   string
	}{
		{2, 5, "2 Defog " + badgeSt.Render("●2")},
		{0, 5, "2 Defog " + selMarker.Render("●5")},
		{0, 0, "2 Defog"},
	} {
		if got := tabLabel(1, actCounted{counted{placeholder{name: "Defog"}, c.n}, c.act}); got != c.want {
			t.Errorf("(%d,%d) label = %q, want %q", c.n, c.act, got, c.want)
		}
	}
}

// Bold green ●N for the Live rows only when nothing waits and nothing is Actionable.
func TestTabLabelLiveTone(t *testing.T) {
	forceANSI256(t)
	for _, c := range []struct {
		n, act, live int
		want         string
	}{
		{0, 0, 4, "3 Execute " + aliveSt.Bold(true).Render("●4")},
		{2, 0, 4, "3 Execute " + badgeSt.Render("●2")},
		{0, 5, 4, "3 Execute " + selMarker.Render("●5")},
		{0, 0, 0, "3 Execute"},
	} {
		if got := tabLabel(2, liveActCounted{actCounted{counted{placeholder{name: "Execute"}, c.n}, c.act}, c.live}); got != c.want {
			t.Errorf("(%d,%d,%d) label = %q, want %q", c.n, c.act, c.live, got, c.want)
		}
	}
}

type liveActCounted struct {
	actCounted
	l int
}

func (c liveActCounted) liveN() int { return c.l }

type actCounted struct {
	counted
	a int
}

func (c actCounted) actN() int { return c.a }

type counted struct {
	placeholder
	n int
}

func (c counted) count() int { return c.n }

// The frame is header + 3 tab rows + body + status + hints, every line within
// width, at the 40×12 floor and above, on every tab and with ? open (FR6–9, 13).
func TestFrameFitsAndBudget(t *testing.T) {
	for _, sz := range [][2]int{{40, 12}, {84, 24}, {200, 50}} {
		m := sized(t, sz[0], sz[1])
		for i := range tabNames {
			m, _ = send(t, m, runeKey(tabDefs[i].key))
			for _, open := range []bool{false, true} {
				mm := m
				if open {
					mm, _ = send(t, m, runeKey("?"))
				}
				ls := lines(mm.View())
				if len(ls) != sz[1] {
					t.Fatalf("%dx%d tab %d ?=%v: %d lines, want %d", sz[0], sz[1], i+1, open, len(ls), sz[1])
				}
				for _, l := range ls {
					if lipgloss.Width(l) > sz[0] {
						t.Errorf("%dx%d tab %d ?=%v: line %q is %d wide", sz[0], sz[1], i+1, open, l, lipgloss.Width(l))
					}
				}
				if open {
					continue
				}
				if !strings.HasPrefix(ls[0], " mugicrew") {
					t.Errorf("header = %q", ls[0])
				}
				if !strings.Contains(ls[2], tabNames[i]) {
					t.Errorf("tab row = %q, want %s", ls[2], tabNames[i])
				}
				if !strings.Contains(ls[len(ls)-1], "? keys") {
					t.Errorf("last line = %q, want the hints bar", ls[len(ls)-1])
				}
				body := strings.Join(ls[4:len(ls)-2], "\n")
				if i != tabIdeate && i != tabDefog && i != tabExecute && i != tabInits && i != tabSettings && i != tabLogs && i != tabStats && i != tabHistory && !strings.Contains(body, tabNames[i]) { // Ideate, Defog, Execute, Initiatives, Settings, Logs, Stats and History have their own tests
					t.Errorf("tab %s body lacks its placeholder line:\n%s", tabNames[i], body)
				}
			}
		}
	}
}

// Below the floor a notice replaces the frame (FR15).
func TestTooSmallNotice(t *testing.T) {
	for _, sz := range [][2]int{{39, 12}, {40, 11}, {20, 3}} {
		v := sized(t, sz[0], sz[1]).View()
		ls := lines(v)
		if !strings.Contains(ansi.Strip(v), "terminal too small") || len(ls) != sz[1] {
			t.Errorf("%dx%d: want a %d-line too-small notice, got:\n%s", sz[0], sz[1], sz[1], v)
		}
		for _, l := range ls {
			if lipgloss.Width(l) > sz[0] {
				t.Errorf("%dx%d: line %q too wide", sz[0], sz[1], l)
			}
		}
	}
}

// 1–6, # $ *, tab/⇧tab and ←/→ switch tabs, wrapping; letters never jump (FR16).
func TestShellSwitchesTabs(t *testing.T) {
	m := sized(t, 120, 30)
	steps := []struct {
		msg  tea.KeyMsg
		want string
	}{
		{runeKey("4"), "Graph"},
		{tea.KeyMsg{Type: tea.KeyTab}, "Initiatives"},
		{tea.KeyMsg{Type: tea.KeyRight}, "History"},
		{tea.KeyMsg{Type: tea.KeyRight}, "Stats"},
		{tea.KeyMsg{Type: tea.KeyShiftTab}, "History"},
		{tea.KeyMsg{Type: tea.KeyLeft}, "Initiatives"},
		{tea.KeyMsg{Type: tea.KeyLeft}, "Graph"},
		{runeKey("h"), "Execute"},
		{runeKey("l"), "Graph"},
		{runeKey("G"), "Graph"},
		{runeKey("L"), "Graph"},
		{runeKey("*"), "Settings"},
		{tea.KeyMsg{Type: tea.KeyTab}, "Ideate"},
		{tea.KeyMsg{Type: tea.KeyShiftTab}, "Settings"},
	}
	for _, s := range steps {
		m, _ = send(t, m, s.msg)
		if got := activeTab(t, m); got != s.want {
			t.Fatalf("after %s: tab %q, want %q", s.msg, got, s.want)
		}
	}
}

// q and ctrl+c quit at once (FR20).
func TestQuit(t *testing.T) {
	for _, k := range []tea.KeyMsg{runeKey("q"), {Type: tea.KeyCtrlC}} {
		_, cmd := send(t, sized(t, 80, 24), k)
		if cmd == nil {
			t.Fatalf("%s: no command", k)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%s did not quit", k)
		}
	}
}

// ? toggles a modal reference generated from the tab's and the shell's
// bindings; while it is open no shell key fires, and ?/esc close it (FR18, 21, 29).
func TestKeyReference(t *testing.T) {
	m := sized(t, 100, 25)
	m, _ = send(t, m, runeKey("4"))
	open, _ := send(t, m, runeKey("?"))
	v := ansi.Strip(open.View())
	for _, want := range []string{"keys — Graph", "1-6 # $ *", "tab/→/l", "next tab", "⇧tab/←/h", "prev tab", "quit", "?/esc"} {
		if !strings.Contains(v, want) {
			t.Errorf("reference lacks %q:\n%s", want, v)
		}
	}
	for _, k := range []tea.KeyMsg{runeKey("1"), {Type: tea.KeyTab}, {Type: tea.KeyLeft}, runeKey("o")} {
		next, cmd := send(t, open, k)
		if cmd != nil || next.View() != open.View() {
			t.Errorf("%s fired under the reference", k)
		}
	}
	for _, k := range []tea.KeyMsg{runeKey("?"), {Type: tea.KeyEsc}, runeKey("q")} {
		closed, _ := send(t, open, k)
		if closed.View() != m.View() {
			t.Errorf("%s did not close the reference", k)
		}
	}
}

// The hints bar lists the tab's bindings only, ends with ? keys, and drops
// verbs from the right — never ? keys — when narrow (FR27–30).
func TestHintsBar(t *testing.T) {
	verbs := []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "launch")),
		key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pause")),
		key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "drop session")),
	}
	wide := ansi.Strip(sectionedBar(verbs, 120, true))
	if !strings.Contains(wide, "enter launch") || !strings.Contains(wide, "x drop session") || !strings.HasSuffix(strings.TrimRight(wide, " "), "? keys") {
		t.Errorf("wide hints = %q", wide)
	}
	if strings.Contains(wide, "quit") || strings.Contains(wide, "next tab") {
		t.Errorf("shell keys leaked into hints: %q", wide)
	}
	narrow := ansi.Strip(sectionedBar(verbs, 30, true))
	if lipgloss.Width(narrow) != 30 || !strings.Contains(narrow, "enter launch") || strings.Contains(narrow, "drop session") ||
		!strings.HasSuffix(strings.TrimRight(narrow, " "), "? keys") || strings.Contains(narrow, "…") {
		t.Errorf("narrow hints = %q", narrow)
	}
	if tiny := ansi.Strip(sectionedBar(verbs, 10, true)); !strings.Contains(tiny, "?") || strings.Contains(tiny, "launch") {
		t.Errorf("tiny hints = %q", tiny)
	}
	// an empty Execute selects its Actionable header: fold and detail, no verbs
	exec, _ := send(t, sized(t, 80, 24), runeKey("3"))
	if bar := lines(exec.View()); !strings.HasPrefix(strings.TrimSpace(bar[len(bar)-1]), "g goto • n new • space fold") {
		t.Errorf("Execute hints = %q", bar[len(bar)-1])
	}
}

// candidateKeys is every key a human can press that a binding could name.
func candidateKeys() []tea.KeyMsg {
	var ks []tea.KeyMsg
	for r := rune(0x21); r < 0x7f; r++ {
		ks = append(ks, runeKey(string(r)))
	}
	for _, t := range []tea.KeyType{
		tea.KeySpace, tea.KeyEnter, tea.KeyEsc, tea.KeyTab, tea.KeyShiftTab, tea.KeyBackspace, tea.KeyDelete,
		tea.KeyUp, tea.KeyDown, tea.KeyLeft, tea.KeyRight, tea.KeyHome, tea.KeyEnd, tea.KeyPgUp, tea.KeyPgDown,
		tea.KeyCtrlC, tea.KeyCtrlD, tea.KeyCtrlU, tea.KeyCtrlUp, tea.KeyCtrlDown, tea.KeyF5, tea.KeyF12,
	} {
		ks = append(ks, tea.KeyMsg{Type: t})
	}
	return ks
}

// Bindings are the single source (FR30): in every mode, a key the shell or the
// tab handles is in that mode's binding set, and every bound key is handled.
// Handled means the frame changed or a command came back. A digit naming the
// current tab is bound but a no-op there. Later Builds extend the modes.
func TestBindingCoverage(t *testing.T) {
	base := sized(t, 120, 30)
	base, _ = send(t, base, loadedMsg{beads: stubBeads()}) // the Graph has rows to move, fold and open
	for i := range tabNames {
		for _, open := range []bool{false, true} {
			if i == tabSettings && !open { // its cursor-dependent modes: TestSettingsBindingCoverage
				continue
			}
			m, _ := send(t, base, runeKey(tabDefs[i].key))
			if open { // a short terminal, scrolled mid-way, so every scroll key moves the box
				m, _ = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 12})
				m, _ = send(t, m, runeKey("?"))
				m, _ = send(t, m, runeKey("j"))
				m, _ = send(t, m, runeKey("j"))
			}
			own := []string{tabDefs[i].key, "J", "K", "ctrl+down", "ctrl+up"} // section jump is a no-op at the first/last header
			if i == tabExecute {                                              // d detail is hinted; its pane is a later Build, so d is a no-op for now
				own = append(own, "d")
			}
			checkCoverage(t, tabNames[i]+" ?="+fmt.Sprint(open), m, !open, own...)
		}
	}
}

// checkCoverage asserts, over every candidate key, handled == bound in m's mode.
// own is the digit naming the current tab (bound but a no-op there).
func checkCoverage(t *testing.T, name string, m Model, checkOwn bool, own ...string) {
	t.Helper()
	bound := map[string]bool{}
	for _, b := range m.modeBindings() {
		for _, k := range b.Keys() {
			bound[k] = true
		}
	}
	for _, k := range candidateKeys() {
		next, cmd := send(t, m, k)
		handled := cmd != nil || next.View() != m.View()
		if k.String() == "y" && !handled { // the shell yank key is a no-op on a row with no bead
			delete(bound, "y")
			continue
		}
		if handled != bound[k.String()] && !(checkOwn && slices.Contains(own, k.String())) {
			t.Errorf("%s: key %q handled=%v bound=%v", name, k, handled, bound[k.String()])
		}
		delete(bound, k.String())
	}
	if len(bound) > 0 {
		t.Errorf("%s: bound keys no candidate covers: %v", name, slices.Sorted(maps.Keys(bound)))
	}
}

// Rendering runs no subprocess (ADR 0101 §4): with recording bd, git and tmux
// fakes first on PATH, View on every tab and with ? open starts none of them.
func TestViewRunsNoSubprocess(t *testing.T) {
	log := fakeTools(t)
	m := sized(t, 120, 40)
	for i := range tabNames {
		m, _ = send(t, m, runeKey(tabDefs[i].key))
		m.View()
		open, _ := send(t, m, runeKey("?"))
		open.View()
		if got, _ := os.ReadFile(log); len(got) > 0 {
			t.Fatalf("View on %s started subprocesses:\n%s", tabNames[i], got)
		}
	}
}

// The real action, quit and force-stop dialogs render over the frame without a
// subprocess either (ADR 0101 §4).
func TestActionDialogsRunNoSubprocess(t *testing.T) {
	log := fakeTools(t)
	m, _ := actModel(t)
	var open []Model
	for _, c := range []struct{ id, k string }{{"sess", "x"}, {"live", "x"}, {"blk", ""}, {"resblk", "enter"}} {
		k := runeKey(c.k)
		if c.k == "" || c.k == "enter" {
			k = enterKey()
		}
		d, _ := at(t, m, c.id, k)
		open = append(open, d)
	}
	q := m
	q.pending = map[string]string{"sig": "updating"}
	q, _ = send(t, q, runeKey("q"))
	f := m
	f.loop = loopDraining
	f, _ = send(t, f, runeKey("o"))
	open = append(open, q, f)
	for _, d := range open {
		if len(d.modals) != 1 {
			t.Fatalf("no dialog: %+v", d.modals)
		}
		d.View()
	}
	if got, _ := os.ReadFile(log); len(got) > 0 {
		t.Fatalf("dialog render started subprocesses:\n%s", got)
	}
}

// The recover, discard, orphan, Logs kind picker, Logs / and e → title dialogs
// render without a subprocess too (Testing Decisions: every modal).
func TestRemainingDialogsRunNoSubprocess(t *testing.T) {
	log := fakeTools(t)
	m := sized(t, 120, 40)
	inv := chopper.Inventory{Files: 2, Tree: "/wt"}
	m1 := m.push(promptDialog("Recover?", func(w int) []string { return []string{confirmRecoverPrompt("a bead", "dead", inv, w)} }, "b", nil))
	m2 := m.push(confirmDialog("Discard changes?", []string{confirmDiscardPrompt(inv)}, "b", nil))
	m3 := m.push(promptDialog("Drop orphaned worktree?", func(w int) []string { return []string{confirmOrphanPrompt("a bead", "/wt", w)} }, "b", nil))
	m4, _ := send(t, m, runeKey("$"))
	m4.logKindOf = map[string]contract.Kind{} // read already, so the picker opens at once
	m5, _ := m4.pickLogKind()
	m6, _ := send(t, m4, runeKey("/"))
	m7 := m.push(editSignalForm(bd.Bead{ID: "b", Title: "t"}))
	for i, d := range []Model{m1, m2, m3, m5, m6, m7} {
		if len(d.modals) != 1 && d.capturing() == false {
			t.Errorf("case %d: nothing open", i)
		}
		d.View()
	}
	if got, _ := os.ReadFile(log); len(got) > 0 {
		t.Fatalf("render started subprocesses:\n%s", got)
	}
}

// A launch mid-claim gates quit, and a second q never stacks a second Quit?.
func TestQuitAsksOnceWhileLaunching(t *testing.T) {
	m := sized(t, floorCols-1, floorRows-1)
	m.launching = map[string]bool{"b": true}
	m, _ = send(t, m, runeKey("q"))
	m, _ = send(t, m, runeKey("q"))
	if len(m.modals) != 1 {
		t.Fatalf("want one Quit? dialog, got %d", len(m.modals))
	}
}

// ctrl+c quits from a dialog's text input as from a tab's (FR20, FR21).
func TestCtrlCQuitsFromDialogInput(t *testing.T) {
	m := sized(t, 120, 30).push(inputDialog("Reason", []string{"text"}, "", nil))
	if _, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil || cmd() != tea.Quit() {
		t.Errorf("ctrl+c in a dialog input did not quit")
	}
}

// A dialog is drawn over the frame: the header and status line stay visible.
func TestDialogKeepsFrame(t *testing.T) {
	m := sized(t, 120, 30)
	m.status = "a status"
	v := ansi.Strip(m.push(confirmDialog("Sure?", nil, "", nil)).View())
	if !strings.Contains(v, "mugicrew") || !strings.Contains(v, "a status") || !strings.Contains(v, "Sure?") {
		t.Errorf("frame lost under the dialog:\n%s", v)
	}
}

// fakeTools puts recording bd, git and tmux first on PATH and returns the log
// they append to: a render that runs a subprocess leaves a line in it.
func fakeTools(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	log := filepath.Join(bin, "calls.log")
	for _, name := range []string{"bd", "git", "tmux"} {
		script := "#!/bin/sh\necho \"" + name + " $*\" >> '" + log + "'\nexit 0\n"
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// At the floor the ? reference scrolls its key list; its close line and counter stay.
func TestRefKeepsCloseLineAtFloor(t *testing.T) {
	m := sized(t, floorCols, floorRows)
	m, _ = send(t, m, loadedMsg{beads: stubBeads()})
	m, _ = send(t, m, runeKey("4"))
	if v := ansi.Strip(press(t, m, runeKey("?")).View()); !strings.Contains(v, "esc close") || !refCounter.MatchString(v) {
		t.Errorf("close line clipped:\n%s", v)
	}
}

// The hints bar follows the mode: a dialog, picker, form or / input shows only
// the keys that reach it, with no ? keys where ? is not live (FR24, ADR 0144 §7).
func TestHintsBarFollowsMode(t *testing.T) {
	var ran bool
	var picked string
	nop := func(m Model, _ dialog) (Model, tea.Cmd) { return m, nil }
	last := func(m Model) string { l := lines(m.View()); return strings.Join(strings.Fields(l[len(l)-1]), " ") }
	for name, c := range map[string]struct {
		m    Model
		want string
	}{
		"y/n":    {graphModel(t, 120, 30).push(testConfirm(&ran)), ""},
		"picker": {graphModel(t, 120, 30).push(testPicker(&picked)), ""},
		"form":   {graphModel(t, 120, 30).push(inputDialog("Reject", []string{"why"}, "", nop)), ""},
		"input":  {press(t, graphModel(t, 120, 30), runeKey("/")), "enter keep • esc revert"},
		"ref":    {press(t, graphModel(t, 120, 30), runeKey("?")), ""},
	} {
		if got := last(c.m); got != c.want {
			t.Errorf("%s hints = %q, want %q", name, got, c.want)
		}
	}
}

func TestCtrlCQuitsUnderReference(t *testing.T) {
	m, _ := send(t, sized(t, 80, 24), runeKey("?"))
	if _, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Error("ctrl+c did not quit under the reference")
	}
}

// A hint's key and label are one styled run: no unstyled cell between them,
// and a disabled binding is not shown.
func TestShortHintsHaveNoGap(t *testing.T) {
	forceANSI256(t)
	off := key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "hidden"))
	off.SetEnabled(false)
	raw := shortHelpView([]key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "launch")),
		off,
		key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pause")),
	})
	if strings.Contains(raw, "\x1b[0m ") {
		t.Errorf("unstyled cell after a style reset: %q", raw)
	}
	if got := ansi.Strip(raw); got != "enter launch • p pause" {
		t.Errorf("hints = %q", got)
	}
}

// ADR 0181 §1: 1-6 and # $ * jump; 7 and 8 are unbound; labels lead with their key.
func TestSymbolJumpKeys(t *testing.T) {
	for k, want := range map[string]int{"4": tabGraph, "5": tabInits, "6": tabHistory, "#": tabStats, "$": tabLogs, "*": tabSettings} {
		if m, _ := send(t, sized(t, 120, 30), runeKey(k)); m.tab != want {
			t.Errorf("%s -> tab %d, want %d", k, m.tab, want)
		}
	}
	for _, k := range []string{"7", "8"} {
		if m, _ := send(t, sized(t, 120, 30), runeKey(k)); m.tab != tabIdeate {
			t.Errorf("%s moved to tab %d", k, m.tab)
		}
	}
	for i, l := range []string{"# Stats", "$ Logs", "* Settings"} {
		if got := tabLabel(tabStats+i, placeholder{}); got != l {
			t.Errorf("label %q, want %q", got, l)
		}
	}
}

// An f press while a kinds read is in flight is queued, and opens the picker only if Logs is still the tab.
func TestLogKindPickQueuesBehindRead(t *testing.T) {
	m := sized(t, 120, 40)
	m, _ = send(t, m, runeKey("$"))
	m.logKindBusy = true
	m, cmd := m.readKinds(true)
	if cmd != nil || !m.logKindPick {
		t.Fatalf("pick not queued: cmd=%v pick=%v", cmd != nil, m.logKindPick)
	}
	away := m
	away.tab = tabStats
	away, _ = away.foldKinds(logKindsMsg{})
	if len(away.modals) != 0 || away.logKindPick {
		t.Fatalf("picker opened off Logs: %+v", away.modals)
	}
	m, _ = m.foldKinds(logKindsMsg{})
	if len(m.modals) != 1 {
		t.Fatalf("queued pick did not open: %+v", m.modals)
	}
}
