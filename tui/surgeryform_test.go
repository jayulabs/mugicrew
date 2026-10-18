package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// sgFixture has one Checkpoint per JoinVerdict refusal, a closed Checkpoint, a member of
// a closed Initiative and two open Initiatives.
func sgFixture() []bd.Bead {
	cp := func(id, parent string, st string) bd.Bead {
		return bd.Bead{ID: id, Kind: contract.KindCheckpoint, Container: true, Status: st, Title: "cp " + id, Parent: parent, Created: epoch}
	}
	ini := func(id, st string) bd.Bead {
		return bd.Bead{ID: id, Kind: contract.KindInitiative, Container: true, Status: st, Title: "ini " + id, Created: epoch}
	}
	kid := func(id, parent string, k contract.Kind) bd.Bead {
		return bd.Bead{ID: id, Kind: k, Status: "open", Title: id, Parent: parent, Created: epoch}
	}
	b1 := cp("b1", "iniB", "open")
	b1.BlockedBy = []string{"pv"}
	return []bd.Bead{
		ini("iniA", "open"), ini("iniB", "open"), ini("iniC", "closed"),
		cp("land", "", "open"), kid("ld", "land", contract.KindLanding),
		cp("free", "", "open"),
		cp("cini", "iniC", "open"),
		cp("dead", "", "closed"),
		cp("a1", "iniA", "open"),
		cp("a2", "iniA", "open"), kid("sgA2", "a2", contract.KindSurgery),
		b1, kid("pv", "", contract.KindPivot),
		cp("b2", "iniB", "open"), kid("cf", "b2", contract.KindConflict),
	}
}

func sgIDs(rows []sgRow) (ids []string) {
	for _, r := range rows {
		switch {
		case r.kind == sgGap:
			ids = append(ids, "")
		case r.kind == sgStandalone:
			ids = append(ids, "+")
		default:
			ids = append(ids, r.b.ID)
		}
	}
	return
}

func TestSurgeryRowsOrderAndRefusals(t *testing.T) {
	rows := surgeryRows(sgFixture(), false)
	want := []string{"+", "", "free", "cini", "land", "", "iniA", "a1", "a2", "iniB", "b1", "b2"}
	if got := sgIDs(rows); !slices.Equal(got, want) {
		t.Errorf("rows %v, want %v", got, want)
	}
	short := map[string]string{}
	for _, r := range rows {
		short[sgIDs([]sgRow{r})[0]] = r.short()
	}
	for id, w := range map[string]string{"free": "", "land": "landing", "a2": "surgery sgA2 open", "b1": "pivot gate", "b2": "branch rewriting"} {
		if short[id] != w {
			t.Errorf("%s: short %q, want %q", id, short[id], w)
		}
	}
	if got := sgIDs(surgeryRows(sgFixture(), true)); got[0] == "+" || got[0] == "" {
		t.Errorf("Require Initiatives lists the standalone row or a leading gap: %v", got)
	}
}

func TestSurgeryDefaultCursor(t *testing.T) {
	rows := surgeryRows(sgFixture(), false)
	req := surgeryRows(sgFixture(), true)
	for _, c := range []struct {
		name    string
		rows    []sgRow
		cp, ini string
		want    string
	}{
		{"over a Checkpoint", rows, "free", "", "free"},
		{"over a refused Checkpoint", rows, "land", "", "land"},
		{"over an Initiative", rows, "", "iniB", "iniB"},
		{"over nothing", rows, "", "", "+"},
		{"nothing under Require Initiatives", req, "", "", "free"},
		{"a closed Checkpoint is not listed", rows, "dead", "", "+"},
	} {
		if got := sgIDs(c.rows[surgeryCursor(c.rows, c.cp, c.ini) : surgeryCursor(c.rows, c.cp, c.ini)+1])[0]; got != c.want {
			t.Errorf("%s: cursor on %q, want %q", c.name, got, c.want)
		}
	}
}

// sgModel opens the form over the fixture (Graph tab, selection wherever the tab left it).
func sgModel(t *testing.T, req bool, extra ...bd.Bead) Model {
	t.Helper()
	m := sized(t, 120, 40)
	m.initiatives.Required = req
	m, _ = send(t, m, loadedMsg{beads: append(extra, sgFixture()...), live: map[string]bool{}, sessions: map[string]session.Info{}})
	return m
}

func sgTop(m Model) dialog { return m.modals[len(m.modals)-1] }

// sgCur is the id the form's pick sits on ("+" the standalone row).
func sgCur(m Model) string {
	d := sgTop(m)
	return sgIDs(d.sg.rows[d.sg.cur : d.sg.cur+1])[0]
}

// sgTo moves the pick to id.
func sgTo(t *testing.T, m Model, id string) Model {
	t.Helper()
	for range 40 {
		if sgCur(m) == id {
			return m
		}
		m = press(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	t.Fatalf("no target %s; at %s", id, sgCur(m))
	return m
}

func TestSurgeryFormViewShowsRowsAndMessageSlot(t *testing.T) {
	m := pressN(t, sgModel(t, false), "u")
	v := ansi.Strip(m.View())
	for _, want := range []string{"New Surgery", "+ New standalone Checkpoint", "✓ ", "⊘ landing", "⊘ pivot gate", "⊘ branch rewriting", "⊘ surgery sgA2 open"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "cp dead") || strings.Contains(v, "iniC") {
		t.Errorf("closed bead listed:\n%s", v)
	}
	for _, c := range []struct{ at, want string }{
		{"iniA", "this will create a new Checkpoint in iniA"},
		{"land", "cp land is landing"},
	} {
		if v := ansi.Strip(sgTo(t, m, c.at).View()); !strings.Contains(v, c.want) && !strings.Contains(v, strings.ReplaceAll(c.want, "cp land", "land")) {
			t.Errorf("at %s: no %q:\n%s", c.at, c.want, v)
		}
	}
	// a form error outranks the Initiative hint
	o := press(t, sgTo(t, m, "iniA"), enter())
	if v := ansi.Strip(o.View()); !strings.Contains(v, "create: title required") || strings.Contains(v, "this will create") {
		t.Errorf("error does not take the slot:\n%s", v)
	}
}

func TestSurgeryBoxNeverResizes(t *testing.T) {
	for _, h := range []int{40, 12} { // 12 scrolls the list
		m := pressN(t, sgModel(t, false), "u")
		m, _ = send(t, m, tea.WindowSizeMsg{Width: 120, Height: h})
		height := func(m Model) int {
			top, bot := -1, -1
			for i, l := range lines(m.View()) {
				if strings.Contains(l, "╭") && top < 0 {
					top = i
				}
				if strings.Contains(l, "╰") {
					bot = i
				}
			}
			return bot - top
		}
		want := height(m)
		for range len(sgTop(m).sg.rows) + 2 {
			m = press(t, m, tea.KeyMsg{Type: tea.KeyDown})
			if got := height(m); got != want {
				t.Errorf("h=%d: box height %d at %s, want %d", h, got, sgCur(m), want)
			}
		}
		m = press(t, m, enter()) // an error line
		if got := height(m); got != want {
			t.Errorf("h=%d: box height %d with an error, want %d", h, got, want)
		}
	}
}

func TestSurgeryKeys(t *testing.T) {
	m := pressN(t, sgModel(t, false), "u")
	if sgCur(m) != "+" || sgTop(m).field != 0 {
		t.Fatalf("opens on %s field %d", sgCur(m), sgTop(m).field)
	}
	m = typeText(t, m, "jk") // title takes j and k as text
	if sgCur(m) != "+" || sgTop(m).inputs[0].text != "jk" {
		t.Errorf("j/k in title: pick %s text %q", sgCur(m), sgTop(m).inputs[0].text)
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if sgCur(m) != "free" { // the gap is skipped
		t.Errorf("down from + landed on %q", sgCur(m))
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = press(t, press(t, m, runeKey("j")), runeKey("k"))
	if sgCur(m) != "free" || sgTop(m).inputs[0].text != "jk" {
		t.Errorf("j/k in target: pick %s text %q", sgCur(m), sgTop(m).inputs[0].text)
	}
	if o := press(t, m, esc()); len(o.modals) != 0 {
		t.Error("esc did not cancel")
	}
}

func fileModel(t *testing.T, req bool) (Model, *[]string) {
	t.Helper()
	var calls []string
	seam(t, &newSurgery, func(title, p, ini string, r bool) (string, string, error) {
		calls = append(calls, "new:"+title+":"+p+":"+ini)
		return "sg", "cpN", nil
	})
	seam(t, &newSurgeryOn, func(cp, title, p string) (string, error) {
		calls = append(calls, "on:"+cp+":"+title+":"+p)
		return "sg", nil
	})
	stubActions(t)
	m := pressN(t, sgModel(t, req), "u")
	m.crew = map[string]contract.Crewmate{"franky": fakeCrew{}}
	return m, &calls
}

func TestSurgeryFilesThroughTheTargetsSeam(t *testing.T) {
	for _, c := range []struct{ at, want string }{
		{"+", "new:Fix::"},
		{"iniA", "new:Fix::iniA"},
		{"a1", "on:a1:Fix:"},
	} {
		m, calls := fileModel(t, false)
		m = typeText(t, sgTo(t, m, c.at), "Fix")
		m, cmd := send(t, m, enter())
		if len(m.modals) != 0 {
			t.Errorf("%s: form still open", c.at)
		}
		m, _ = send(t, m, cmd())
		if !slices.Equal(*calls, []string{c.want}) || m.attachTo != "sg" {
			t.Errorf("%s: calls %v attachTo %q, want %q and a launch", c.at, *calls, m.attachTo, c.want)
		}
	}
}

func TestSurgeryEnterRefusals(t *testing.T) {
	m, calls := fileModel(t, false)
	m = typeText(t, sgTo(t, m, "land"), "Fix")
	m = press(t, m, enter())
	if len(m.modals) != 1 || !strings.Contains(sgTop(m).sg.err, "land is landing") || len(*calls) != 0 {
		t.Errorf("refused row: modals=%d err %q calls %v", len(m.modals), sgTop(m).sg.err, *calls)
	}
	m, _ = fileModel(t, false)
	m = press(t, sgTo(t, m, "free"), enter()) // no title
	if len(m.modals) != 1 || sgTop(m).sg.err != "create: title required" {
		t.Errorf("empty title: modals=%d err %q", len(m.modals), sgTop(m).sg.err)
	}
}

func TestSurgeryEnterOnOpenSurgeryResumes(t *testing.T) {
	m, calls := fileModel(t, false)
	m.beads = append(m.beads, bd.Bead{ID: "sgA2", Kind: contract.KindSurgery, Status: "in_progress", Title: "sgA2", Parent: "a2"})
	m, cmd := send(t, sgTo(t, m, "a2"), enter())
	if len(m.modals) != 0 || cmd == nil || !m.launching["sgA2"] || len(*calls) != 0 {
		t.Errorf("modals=%d cmd=%v launching=%v calls=%v", len(m.modals), cmd != nil, m.launching, *calls)
	}
}

// A join that went stale after the list was drawn refuses at file time, on the status line.
func TestSurgeryFileTimeRefusalGoesToStatus(t *testing.T) {
	m, _ := fileModel(t, false)
	seam(t, &newSurgeryOn, func(cp, title, p string) (string, error) {
		return "", bd.JoinRefusal("free is landing — no surgery while a Landing is live")
	})
	m = typeText(t, sgTo(t, m, "free"), "Fix")
	m, cmd := send(t, m, enter())
	m, reload := send(t, m, cmd())
	if s := statusOf(m); !strings.Contains(s, "free is landing") || strings.Contains(s, "u:") || reload == nil {
		t.Errorf("status %q reload %v", s, reload != nil)
	}
}

func TestSurgeryRowHiddenOnlyWhenListEmpty(t *testing.T) {
	has := func(m Model) bool {
		return slices.ContainsFunc(press(t, m, runeKey("n")).modals[0].opts, func(b key.Binding) bool { return slices.Contains(b.Keys(), "u") })
	}
	m := sgModel(t, true)
	if !has(m) || slices.Contains(sgIDs(surgeryRows(m.beads, true)), "+") {
		t.Error("Require Initiatives with open work: u row shown, standalone row hidden")
	}
	noOpen := slices.DeleteFunc(slices.Clone(m.beads), func(b bd.Bead) bool {
		return b.Open() && (b.Kind == contract.KindInitiative || b.Kind == contract.KindCheckpoint)
	})
	m, _ = send(t, m, loadedMsg{beads: noOpen, live: map[string]bool{}, sessions: map[string]session.Info{}})
	if has(m) {
		t.Error("u row shown with an empty target list")
	}
	m.initiatives.Required = false
	if !has(m) {
		t.Error("u row hidden though the standalone row exists")
	}
}

// The menu captures what the human was over, so the form opens on it.
func TestSurgeryOpensOverTheSelection(t *testing.T) {
	for _, c := range []struct{ sel, want string }{{"cp", "cp"}, {"cpb", "cp"}, {"ini", "ini"}} {
		m := pressN(t, selectBead(t, joinModel(t), c.sel), "u")
		if got := sgCur(m); got != c.want {
			t.Errorf("over %s: pick %q, want %q", c.sel, got, c.want)
		}
	}
	f := pressN(t, ideaFocus(t, recentsModel(t, focusFixture())), "u")
	if got := sgCur(f); got != f.focus.id {
		t.Errorf("focus view: pick %q, want %q", got, f.focus.id)
	}
}

// A reload may empty the target list between n and u: refuse instead of pushing a rowless form.
func TestSurgeryRefusesWhenListEmptiedUnderTheMenu(t *testing.T) {
	m := sized(t, 120, 40)
	m.initiatives.Required = true
	only := bd.Bead{ID: "only", Kind: contract.KindCheckpoint, Container: true, Status: "open", Title: "cp only", Created: epoch}
	m, _ = send(t, m, loadedMsg{beads: []bd.Bead{only}, live: map[string]bool{}, sessions: map[string]session.Info{}})
	m = press(t, m, runeKey("n"))
	only.Status = "closed"
	m.beads = []bd.Bead{only}
	m = press(t, m, runeKey("u"))
	if len(m.modals) != 0 {
		t.Errorf("form pushed over an empty target list: %d modals", len(m.modals))
	}
	if !strings.Contains(m.status, "no Surgery target") {
		t.Errorf("status %q, want no Surgery target", m.status)
	}
}
