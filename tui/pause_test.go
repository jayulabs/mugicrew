package tui

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/dispatch"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/loop"
	"jayulabs.org/mugicrew/session"
)

// pausedGraph loads a Chart of paused beads, plus bd's event beads, through a
// refresh — the path that hides event beads (ADR 0057 §3).
func pausedGraph(t *testing.T) Model {
	t.Helper()
	fh, mn := []string{"kind:unowned", "paused:foreign-hold"}, []string{"kind:unowned", "paused:manual"}
	m := New(nil, nil)
	m.w, m.h = 120, 40
	m.hideClosedRoots = false
	next, _ := m.Update(loadedMsg{beads: []bd.Bead{
		{ID: "ch1", Kind: contract.KindChart, Container: true, Title: "Chart: Gate", Status: "open"},
		{ID: "ext", Kind: contract.Kind("unowned"), Parent: "ch1", Status: "open", Title: "Plain open"},
		{ID: "fh", Kind: contract.Kind("unowned"), Labels: fh, Parent: "ch1", Status: "open", Title: "Held off"},
		{ID: "mn", Kind: contract.Kind("unowned"), Labels: mn, Parent: "ch1", Status: "open", Title: "Kept off"},
		{ID: "wip", Kind: contract.Kind("unowned"), Labels: mn, Parent: "ch1", Status: "in_progress", Title: "Still running"},
		{ID: "blk", Kind: contract.Kind("unowned"), Labels: mn, Parent: "ch1", Status: "open", Title: "Waits on ext", BlockedBy: []string{"ext"}},
		{ID: "two", Kind: contract.Kind("unowned"), Labels: append(slices.Clone(fh), "paused:manual"), Parent: "ch1", Status: "open", Title: "Doubly off"},
		{ID: "fl", Kind: contract.Kind("unowned"), Labels: []string{"kind:unowned", "paused:failing"}, Parent: "ch1", Status: "open", Title: "Keeps failing"},
		{ID: "mf", Kind: contract.Kind("unowned"), Labels: []string{"kind:unowned", "paused:failing", "paused:manual"}, Parent: "ch1", Status: "open", Title: "Kept and failing"},
		{ID: "ff", Kind: contract.Kind("unowned"), Labels: append(slices.Clone(fh), "paused:failing"), Parent: "ch1", Status: "open", Title: "Held and failing"},
		{ID: "all", Kind: contract.Kind("unowned"), Labels: append(slices.Clone(fh), "paused:failing", "paused:manual"), Parent: "ch1", Status: "open", Title: "Every cause"},
		{ID: "ch1.1", Event: true, Kind: contract.Kind("unowned"), Parent: "ch1", Status: "closed", Title: "State change: env → y"},
		{ID: "mn.1", Event: true, Parent: "mn", Status: "closed", Title: "State change: paused → manual"},
	}})
	return next.(Model)
}

func lineWith(t *testing.T, view, needle string) string {
	t.Helper()
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, needle) {
			return l
		}
	}
	t.Fatalf("no line containing %q in:\n%s", needle, view)
	return ""
}

// An open paused bead shows ‖ in its cause's colour where ○ would be; ⊘ and ◐
// keep precedence; event beads are neither rows nor counted.
func TestPausedGlyphAndHiddenEvents(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)
	m := pausedGraph(t)
	graph := m.graphView(m.paneHeight())
	yellow := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("‖")
	magenta := lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Render("‖")
	red := lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render("‖")
	for _, c := range []struct {
		needle, want string
		not          []string
	}{
		{"Held off", yellow, []string{"○", magenta}},
		{"Kept off", magenta, []string{"○", yellow}},
		{"Doubly off", magenta, []string{"○", yellow}},
		{"Keeps failing", red, []string{"○", yellow, magenta}},
		{"Kept and failing", magenta, []string{"○", red}},
		{"Held and failing", red, []string{"○", yellow}},
		{"Every cause", magenta, []string{"○", red, yellow}},
		{"Still running", "◐", []string{"‖"}},
		{"Waits on ext", "⊘", []string{"‖"}},
		{"Plain open", "○", []string{"‖"}},
	} {
		l := lineWith(t, graph, c.needle)
		if !strings.Contains(l, c.want) {
			t.Errorf("%q row lacks %q: %q", c.needle, c.want, l)
		}
		for _, n := range c.not {
			if strings.Contains(l, n) {
				t.Errorf("%q row has %q: %q", c.needle, n, l)
			}
		}
	}
	// Same title length, same row width: the pause costs no columns.
	if a, b := lipgloss.Width(lineWith(t, graph, "Held off")), lipgloss.Width(lineWith(t, graph, "Kept off")); a != b {
		t.Errorf("paused rows differ in width: %d vs %d", a, b)
	}
	if strings.Contains(graph, "State change") || len(m.beads) != 11 {
		t.Errorf("event beads leaked into the graph (beads=%d):\n%s", len(m.beads), graph)
	}
	if closed, total := m.childCounts("ch1"); closed != 0 || total != 10 {
		t.Errorf("childCounts(ch1) = %d/%d, want 0/10 (event beads uncounted)", closed, total)
	}
}

// The detail pane's paused line lists every cause, manual first, with no reason,
// actor or age (ADR 0057 amendment) — off the bead's labels, no bd call.
func TestPausedDetailLine(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	defer lipgloss.SetColorProfile(prev)
	origList := listAll
	defer func() { listAll = origList }()
	calls := 0
	listAll = func() ([]bd.Bead, error) { calls++; return nil, nil }

	m := pausedGraph(t)
	for _, c := range []struct{ id, want string }{
		{"mn", "paused      manual"},
		{"fh", "paused      foreign-hold"},
		{"two", "paused      manual · foreign-hold"},
		{"fl", "paused      failing"},
		{"ff", "paused      failing · foreign-hold"},
		{"all", "paused      manual · failing · foreign-hold"},
	} {
		d := m.detailView(m.beadByID(c.id), 0, 1, 30)
		if l := lineWith(t, d, "paused  "); strings.TrimRight(l, " ") != c.want {
			t.Errorf("%s detail paused line = %q, want %q", c.id, l, c.want)
		}
	}
	if d := m.detailView(m.beadByID("ext"), 0, 1, 30); strings.Contains(d, "paused") {
		t.Errorf("unpaused detail shows a paused line:\n%s", d)
	}
	if calls != 0 {
		t.Errorf("selecting made %d bd list calls, want 0", calls)
	}
}

// The tab bar counts paused beads as · N‖ only when N > 0, and fits the 40-col
// floor even in the worst case.
func TestTabBarPausedCount(t *testing.T) {
	m := pausedGraph(t)
	if got := m.tabBar(); !strings.Contains(got, " · 9‖ · loop off") {
		t.Errorf("tab bar = %q, want · 9‖ (failing beads included) before the loop segment (a doubly paused bead counts once)", got)
	}
	m.beads = slices.DeleteFunc(slices.Clone(m.beads), func(b bd.Bead) bool { return bd.Paused(b) })
	if got := m.tabBar(); strings.Contains(got, "‖") {
		t.Errorf("tab bar with nothing paused = %q, want no ‖", got)
	}

	// Worst case at the floor: two-digit Inbox, live and paused counts while draining.
	// With seven tabs only Logs, the shortest full name, still fits it (ADR 0072 §3).
	hitl := bd.Bead{Kind: contract.KindGrilling, Status: "open", Labels: []string{"paused:manual"}}
	beads := slices.Repeat([]bd.Bead{hitl}, 12)
	sessions := map[string]session.Info{}
	for i := range beads {
		beads[i].ID = string(rune('a' + i))
		sessions[beads[i].ID] = session.Info{State: session.Alive}
	}
	m.beads, m.sessions, m.loop, m.w, m.tab = beads, sessions, loopDraining, floorCols, tabLogs
	got := m.tabBar()
	if lipgloss.Width(got) > floorCols || !strings.Contains(got, "12‖") || !strings.Contains(got, "loop draining 2") || !strings.Contains(got, "I(12)") {
		t.Errorf("at %d cols: tab bar = %q (%d wide), want it to fit with 12‖ and the loop", floorCols, got, lipgloss.Width(got))
	}
}

// A bead whose run-now retry is in flight in this TUI drops out of N‖ at once and
// comes back if its run finishes still paused (Spec FR1–4); a run on a bead
// paused for another cause is still counted.
func TestTabBarPausedCountSkipsInFlightRetry(t *testing.T) {
	calls := stubSeams(t)
	f1 := bd.Bead{ID: "f1", Kind: contract.KindResearch, Parent: "chart1", Status: "open", Labels: []string{"paused:failing"}}
	f2 := bd.Bead{ID: "f2", Kind: contract.KindResearch, Parent: "chart1", Status: "open", Labels: []string{"paused:failing"}}
	readyFrontier = func() ([]bd.Bead, error) { return []bd.Bead{f1, f2}, nil }
	claimBead = func(b bd.Bead) error { *calls = append(*calls, "claimBead "+b.ID); return nil }
	m := New(nil, map[string]contract.Crewmate{"nami": &recordCrew{}})
	m.beads = []bd.Bead{f1, f2}
	m.rebuildGroups()
	m.selGraph = "f1"
	if got := m.tabBar(); !strings.Contains(got, " 2‖ ") {
		t.Fatalf("before retry: tab bar = %q, want 2‖", got)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	next, _ = next.Update(cmd())
	if m = next.(Model); len(m.runs) != 1 {
		t.Fatalf("runs = %+v; want f1's retry tracked", m.runs)
	}
	if got := m.tabBar(); !strings.Contains(got, " 1‖ ") {
		t.Errorf("retry in flight: tab bar = %q, want 1‖", got)
	}
	next, _ = m.Update(runDoneMsg{id: "f1", run: m.runs[0].ID, err: errors.New("boom")})
	next, _ = next.Update(loadedMsg{beads: []bd.Bead{f1, f2}})
	if m = next.(Model); !strings.Contains(m.tabBar(), " 2‖ ") {
		t.Errorf("run done, still paused: tab bar = %q, want 2‖", m.tabBar())
	}

	// A manual pause is not a retry cause: its in-flight run leaves it counted.
	mn := bd.Bead{ID: "mn", Kind: contract.KindResearch, Status: "open", Labels: []string{"paused:manual"}}
	m.beads = []bd.Bead{mn}
	m.runs = []dispatch.Inflight{{ID: "e1", Bead: mn}}
	if got := m.tabBar(); !strings.Contains(got, " 1‖ ") {
		t.Errorf("manual pause with a run: tab bar = %q, want 1‖", got)
	}
	// Leftover 0: the segment goes.
	m.beads, m.runs = []bd.Bead{f1}, []dispatch.Inflight{{ID: "e2", Bead: f1}}
	if got := m.tabBar(); strings.Contains(got, "‖") {
		t.Errorf("only paused bead retrying: tab bar = %q, want no ‖", got)
	}
}

// p toggles a bare manual pause from each of the four pause states (ADR 0057 §6): the footer
// names what it will do, the bd write matches, the status explains in-progress
// and container pauses, closed is refused, and the reloaded row shows the result.
func TestPauseToggle(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)
	origP, origU, origList, origSess := pauseBead, unpauseBead, listAll, readSessions
	defer func() { pauseBead, unpauseBead, listAll, readSessions = origP, origU, origList, origSess }()
	readSessions = func() (map[string]session.Info, error) { return nil, nil }
	magenta := lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Render("‖")
	yellow := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("‖")
	inProgress := lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render("◐")

	for _, c := range []struct {
		id, footer, write, status, glyph string
	}{
		{"ext", "p pause", "pause ext manual", "paused ext", magenta},
		{"mn", "p unpause", "unpause mn manual", "unpaused mn", dim.Render("○")},
		{"fh", "p pause", "pause fh manual", "paused fh — foreign-hold stays until the loop lifts it", magenta},
		{"two", "p unpause", "unpause two manual", "unpaused two", yellow},
		{"wip", "p unpause", "unpause wip manual", "unpaused wip", inProgress},
		{"run", "p pause", "pause run manual", "paused run — the running effort continues", inProgress},
		{"ch1", "p pause", "pause ch1 manual", "paused ch1 — its open members are out of dispatch too", magenta},
		{"done", "", "", "done: closed beads are never dispatched", ""},
	} {
		m := pausedGraph(t)
		next, _ := m.Update(loadedMsg{beads: append(slices.Clone(m.beads),
			bd.Bead{ID: "run", Kind: contract.Kind("unowned"), Parent: "ch1", Status: "in_progress", Title: "Running free"},
			bd.Bead{ID: "done", Kind: contract.Kind("unowned"), Parent: "ch1", Status: "closed", Title: "All done"})})
		m = next.(Model)
		m.selGraph = c.id
		b := *m.beadByID(c.id)

		if f := ansi.Strip(m.footer()); (c.footer == "" && strings.Contains(f, "p pause")) || (c.footer != "" && !strings.Contains(f, c.footer+" · +/- pri · e edit")) {
			t.Errorf("%s: footer = %q, want %q", c.id, f, c.footer)
		}

		var writes []string
		pauseBead = func(id, cause string) error {
			writes = append(writes, "pause "+id+" "+cause)
			return nil
		}
		unpauseBead = func(id, cause string) error { writes = append(writes, "unpause "+id+" "+cause); return nil }
		evs := eventlogtest.Capture(t)
		next, cmd := m.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
		m = next.(Model)
		if len(writes) != 0 {
			t.Errorf("%s: p wrote %q inside Update, want the write in its Cmd", c.id, writes)
		}
		if cmd != nil {
			next, cmd = m.Update(cmd())
			m = next.(Model)
		}
		if got := strings.Join(writes, ";"); got != c.write {
			t.Errorf("%s: bd writes = %q, want %q", c.id, got, c.write)
		}
		if m.status != c.status {
			t.Errorf("%s: status = %q, want %q", c.id, m.status, c.status)
		}
		if len(*evs) != 0 {
			t.Errorf("%s: p logged %v, want no events", c.id, eventlogtest.Names(*evs))
		}
		if c.write == "" {
			if cmd != nil {
				t.Errorf("%s: refused p still reloads", c.id)
			}
			continue
		}

		// Reload: bd now reports the toggled manual label; foreign-hold is untouched.
		b.Labels = slices.DeleteFunc(slices.Clone(b.Labels), func(l string) bool { return l == "paused:manual" })
		if strings.HasPrefix(c.write, "pause ") {
			b.Labels = append(b.Labels, "paused:manual")
		}
		reloaded := slices.Clone(m.beads)
		reloaded[slices.IndexFunc(reloaded, func(x bd.Bead) bool { return x.ID == c.id })] = b
		listAll = func() ([]bd.Bead, error) { return reloaded, nil }
		next, _ = m.Update(cmd())
		m = next.(Model)
		if g := m.stateGlyph(m.beadByID(c.id)); g != c.glyph {
			t.Errorf("%s: reloaded row glyph = %q, want %q", c.id, g, c.glyph)
		}
		if want := map[bool]string{true: "p unpause", false: "p pause"}[strings.HasPrefix(c.write, "pause ")]; !strings.Contains(ansi.Strip(m.footer()), want) {
			t.Errorf("%s: reloaded footer = %q, want %q", c.id, m.footer(), want)
		}
	}
}

// At the 40-col floor a sessioned bead's keys leave no room for p's hint, so it
// yields; the other bead keys survive, tightened to fit beside ? and q (ADR 0058 §5).
func TestPauseFooterFloor(t *testing.T) {
	m := pausedGraph(t)
	m.selGraph, m.w = "ext", floorCols
	m.sessions = map[string]session.Info{"ext": {State: session.Exited}}
	if got := m.footer(); lipgloss.Width(got) > floorCols || strings.Contains(got, "p pause") || !strings.Contains(got, "l resume x drop e edit L logs") {
		t.Errorf("footer at %d cols = %q (%d wide), want p dropped and the rest kept", floorCols, got, lipgloss.Width(got))
	}
}

// t on a foreign-hold pause is the lift (ADR 0057 §5): the footer reads retry
// hold; a refused cut (Dispatch logs effort.held) keeps the label, a clean cut
// removes it and logs effort.resumed. A manual pause hides t and refuses it, even
// beside foreign-hold.
func TestRunNowOnPausedBead(t *testing.T) {
	origReady, origClaim, origDisp, origIP, origU := readyFrontier, claimBead, runDispatch, inProgressBeads, unpauseBead
	defer func() {
		readyFrontier, claimBead, runDispatch, inProgressBeads, unpauseBead = origReady, origClaim, origDisp, origIP, origU
	}()
	inProgressBeads = func() ([]bd.Bead, error) { return nil, nil }

	fh := bd.Bead{ID: "fh", Kind: contract.KindResearch, Labels: []string{"paused:foreign-hold"}, Parent: "ch1", Status: "open"}
	mn := bd.Bead{ID: "mn", Kind: contract.KindResearch, Labels: []string{"paused:manual"}, Parent: "ch1", Status: "open"}
	readyFrontier = func() ([]bd.Bead, error) { return []bd.Bead{fh, mn}, nil }

	for _, c := range []struct {
		name   string
		cut    dispatch.Cut
		writes string
		events []string
	}{
		{"hold still present", dispatch.CutHeld, "", nil},
		{"hold gone", dispatch.CutOK, "unpause fh foreign-hold", []string{"effort.resumed"}},
	} {
		var writes []string
		unpauseBead = func(id, cause string) error { writes = append(writes, "unpause "+id+" "+cause); return nil }
		claimBead = func(bd.Bead) error { return nil }
		runDispatch = func(contract.Ctx, contract.Crewmate, bd.Bead, io.Writer, string, dispatch.InflightFunc) (dispatch.Outcome, error) {
			return dispatch.Outcome{Cut: c.cut, Branch: "chart/ch1"}, nil
		}
		m := New(nil, map[string]contract.Crewmate{"nami": &recordCrew{}})
		m.w, m.h = 120, 40
		m.beads = []bd.Bead{fh, mn}
		m.selGraph = "fh"
		if f := ansi.Strip(m.footer()); !strings.Contains(f, "t retry hold") {
			t.Errorf("%s: footer = %q, want t retry hold", c.name, f)
		}
		next, cmd := settleRunNow(m.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")}))
		if cmd == nil {
			t.Fatalf("%s: foreign-hold run-now must dispatch, status %q", c.name, next.(Model).status)
		}
		evs := eventlogtest.Capture(t)
		cmd()
		if got := strings.Join(writes, ";"); got != c.writes {
			t.Errorf("%s: bd writes = %q, want %q", c.name, got, c.writes)
		}
		if got := eventlogtest.Names(*evs); !slices.Equal(got, c.events) {
			t.Errorf("%s: events = %v, want %v", c.name, got, c.events)
		}
		if len(*evs) == 1 && ((*evs)[0].Attrs["branch"] != "chart/ch1" || (*evs)[0].Attrs["bead"] != "fh" || (*evs)[0].Attrs["cause"] != bd.PausedForeignHold) {
			t.Errorf("%s: effort.resumed attrs = %v", c.name, (*evs)[0].Attrs)
		}
	}

	claimed := false
	claimBead = func(bd.Bead) error { claimed = true; return nil }
	m := New(nil, map[string]contract.Crewmate{"nami": &recordCrew{}})
	m.w, m.h = 120, 40
	m.beads = []bd.Bead{fh, mn}
	m.selGraph = "mn"
	m.beads[0].Labels = nil // a manual pause newer than the last refresh still refuses
	m.selGraph = "fh"
	readyFrontier = func() ([]bd.Bead, error) { return []bd.Bead{{ID: "fh", Labels: mn.Labels}, mn}, nil }
	if next, cmd := settleRunNow(m.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})); cmd != nil || claimed || !strings.Contains(next.(Model).status, "bd label remove fh paused:manual") {
		t.Errorf("stale manual: must refuse without claiming, status %q", next.(Model).status)
	}
	m.selGraph = "mn"
	if hasKey(m.beadKeys(&m.beads[1]), "t") {
		t.Error("manual: footer must hide t")
	}
	next, cmd := settleRunNow(m.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")}))
	if cmd != nil || claimed {
		t.Error("manual: t must claim and dispatch nothing")
	}
	if s := next.(Model).status; !strings.Contains(s, "press p") || !strings.Contains(s, "bd label remove mn paused:manual") {
		t.Errorf("manual: status = %q, want the unpause hint", s)
	}

	// Both labels: manual wins — no t, and a press claims nothing.
	two := bd.Bead{ID: "two", Kind: contract.KindResearch, Labels: []string{"paused:foreign-hold", "paused:manual"}, Parent: "ch1", Status: "open"}
	readyFrontier = func() ([]bd.Bead, error) { return []bd.Bead{two}, nil }
	m.beads, m.selGraph = []bd.Bead{two}, "two"
	if hasKey(m.beadKeys(&m.beads[0]), "t") {
		t.Error("both: footer must hide t")
	}
	next, cmd = settleRunNow(m.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")}))
	if cmd != nil || claimed || !strings.Contains(next.(Model).status, "bd label remove two paused:manual") {
		t.Errorf("both: must refuse without claiming, status %q", next.(Model).status)
	}
}

// t on a failing pause reads retry (ahead of foreign-hold's retry hold) and is the
// lift (ADR 0078): a clean return removes paused:failing and logs effort.resumed
// {cause failing}; a failed one keeps it. Foreign-hold lifts on its own condition,
// a clean cut. Manual + failing still refuses t.
func TestRunNowOnFailingBead(t *testing.T) {
	origReady, origClaim, origDisp, origIP, origU := readyFrontier, claimBead, runDispatch, inProgressBeads, unpauseBead
	defer func() {
		readyFrontier, claimBead, runDispatch, inProgressBeads, unpauseBead = origReady, origClaim, origDisp, origIP, origU
	}()
	inProgressBeads = func() ([]bd.Bead, error) { return nil, nil }

	fl := bd.Bead{ID: "fl", Kind: contract.KindResearch, Labels: []string{"paused:failing"}, Parent: "ch1", Status: "open"}
	fb := bd.Bead{ID: "fb", Kind: contract.KindResearch, Labels: []string{"paused:foreign-hold", "paused:failing"}, Parent: "ch1", Status: "open"}
	readyFrontier = func() ([]bd.Bead, error) { return []bd.Bead{fl, fb}, nil }

	for _, c := range []struct {
		name   string
		bead   string
		err    error
		failFH bool // the foreign-hold unpause write errors
		writes string
		causes []string
	}{
		{"failing clean", "fl", nil, false, "unpause fl failing", []string{bd.PausedFailing}},
		{"failing failed", "fl", errors.New("boom"), false, "", nil},
		{"both failed return", "fb", errors.New("boom"), false, "unpause fb foreign-hold", []string{bd.PausedForeignHold}},
		{"both clean", "fb", nil, false, "unpause fb foreign-hold;unpause fb failing", []string{bd.PausedForeignHold, bd.PausedFailing}},
		{"both clean, hold write fails", "fb", nil, true, "unpause fb foreign-hold;unpause fb failing", []string{bd.PausedFailing}},
	} {
		var writes []string
		unpauseBead = func(id, cause string) error {
			writes = append(writes, "unpause "+id+" "+cause)
			if c.failFH && cause == bd.PausedForeignHold {
				return errors.New("bd down")
			}
			return nil
		}
		claimBead = func(bd.Bead) error { return nil }
		runDispatch = func(contract.Ctx, contract.Crewmate, bd.Bead, io.Writer, string, dispatch.InflightFunc) (dispatch.Outcome, error) {
			return dispatch.Outcome{Cut: dispatch.CutOK, Branch: "chart/ch1"}, c.err
		}
		m := New(nil, map[string]contract.Crewmate{"nami": &recordCrew{}})
		m.w, m.h = 120, 40
		m.beads = []bd.Bead{fl, fb}
		m.selGraph = c.bead
		if f := ansi.Strip(m.footer()); !strings.Contains(f, "t retry") || strings.Contains(f, "retry hold") {
			t.Errorf("%s: footer = %q, want t retry (not retry hold)", c.name, f)
		}
		next, cmd := settleRunNow(m.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")}))
		if cmd == nil {
			t.Fatalf("%s: failing run-now must dispatch, status %q", c.name, next.(Model).status)
		}
		evs := eventlogtest.Capture(t)
		cmd()
		if got := strings.Join(writes, ";"); got != c.writes {
			t.Errorf("%s: bd writes = %q, want %q", c.name, got, c.writes)
		}
		var causes []string
		for _, e := range *evs {
			if e.Name == "effort.resumed" && e.Attrs["bead"] == c.bead {
				causes = append(causes, fmt.Sprint(e.Attrs["cause"]))
			}
		}
		if !slices.Equal(causes, c.causes) {
			t.Errorf("%s: effort.resumed causes = %v, want %v (events %v)", c.name, causes, c.causes, eventlogtest.Names(*evs))
		}
	}

	// Manual + failing: manual wins — no t, and a press claims nothing.
	claimed := false
	claimBead = func(bd.Bead) error { claimed = true; return nil }
	mf := bd.Bead{ID: "mf", Kind: contract.KindResearch, Labels: []string{"paused:failing", "paused:manual"}, Parent: "ch1", Status: "open"}
	readyFrontier = func() ([]bd.Bead, error) { return []bd.Bead{mf}, nil }
	m := New(nil, map[string]contract.Crewmate{"nami": &recordCrew{}})
	m.w, m.h = 120, 40
	m.beads, m.selGraph = []bd.Bead{mf}, "mf"
	if hasKey(m.beadKeys(&m.beads[0]), "t") {
		t.Error("manual+failing: footer must hide t")
	}
	if next, cmd := settleRunNow(m.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})); cmd != nil || claimed || !strings.Contains(next.(Model).status, "bd label remove mf paused:manual") {
		t.Errorf("manual+failing: must refuse without claiming, status %q", next.(Model).status)
	}
}

// paused:abandoned (ADR 0082): an open row shows an orange (208) ‖ below manual
// and above failing, ◐ wins while in_progress, the detail line ranks it after
// manual, t is hidden and refused with the check-then-lift hint (after the own-
// manual hint), and p touches only manual.
func TestAbandonedPause(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)
	origReady, origClaim, origIP, origP, origU, origList := readyFrontier, claimBead, inProgressBeads, pauseBead, unpauseBead, listAll
	defer func() {
		readyFrontier, claimBead, inProgressBeads, pauseBead, unpauseBead, listAll = origReady, origClaim, origIP, origP, origU, origList
	}()
	ab, mn, fl := "paused:abandoned", "paused:manual", "paused:failing"
	beads := []bd.Bead{
		{ID: "ab", Kind: contract.KindResearch, Labels: []string{ab}, Parent: "ch1", Status: "open", Title: "Left behind"},
		{ID: "abf", Kind: contract.KindResearch, Labels: []string{fl, ab}, Parent: "ch1", Status: "open", Title: "Left and failing"},
		{ID: "abm", Kind: contract.KindResearch, Labels: []string{ab, mn}, Parent: "ch1", Status: "open", Title: "Left and kept"},
		{ID: "abw", Kind: contract.KindResearch, Labels: []string{ab}, Parent: "ch1", Status: "in_progress", Title: "Left running"},
	}
	m := New(nil, map[string]contract.Crewmate{"nami": &recordCrew{}})
	m.w, m.h = 120, 40
	m.beads = beads
	orange := lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Render("‖")
	magenta := lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Render("‖")
	inProgress := lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render("◐")
	for id, want := range map[string]string{"ab": orange, "abf": orange, "abm": magenta, "abw": inProgress} {
		if g := m.stateGlyph(m.beadByID(id)); g != want {
			t.Errorf("%s glyph = %q, want %q", id, g, want)
		}
	}
	lipgloss.SetColorProfile(termenv.Ascii)
	for id, want := range map[string]string{"ab": "paused      abandoned", "abf": "paused      abandoned · failing", "abm": "paused      manual · abandoned"} {
		if l := lineWith(t, m.detailView(m.beadByID(id), 0, 1, 30), "paused  "); strings.TrimRight(l, " ") != want {
			t.Errorf("%s detail paused line = %q, want %q", id, l, want)
		}
	}

	inProgressBeads = func() ([]bd.Bead, error) { return nil, nil }
	readyFrontier = func() ([]bd.Bead, error) { return beads[:3], nil }
	claimed := false
	claimBead = func(bd.Bead) error { claimed = true; return nil }
	for id, hint := range map[string]string{
		"ab":  "ab: abandoned effort may still be running; check, then 'bd label remove ab paused:abandoned'",
		"abf": "abf: abandoned effort may still be running; check, then 'bd label remove abf paused:abandoned'",
		"abm": "abm is paused by hand — press p or run 'bd label remove abm paused:manual' first",
	} {
		m.selGraph = id
		if hasKey(m.beadKeys(m.beadByID(id)), "t") {
			t.Errorf("%s: footer must hide t", id)
		}
		next, cmd := settleRunNow(m.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")}))
		if cmd != nil || claimed || next.(Model).status != hint {
			t.Errorf("%s: t must refuse without claiming; status %q, want %q", id, next.(Model).status, hint)
		}
	}

	listAll = func() ([]bd.Bead, error) { return beads, nil }
	for id, want := range map[string]string{"ab": "pause ab manual", "abm": "unpause abm manual"} {
		var writes []string
		pauseBead = func(id, cause string) error { writes = append(writes, "pause "+id+" "+cause); return nil }
		unpauseBead = func(id, cause string) error { writes = append(writes, "unpause "+id+" "+cause); return nil }
		m.tab, m.selGraph = tabGraph, id
		pressPause(t, m)
		if got := strings.Join(writes, ";"); got != want {
			t.Errorf("%s: p wrote %q, want %q (abandoned untouched)", id, got, want)
		}
	}
}

// An inherited pause (ADR 0084): an open member under a manual-paused container
// shows a dim magenta (127) ‖ unless it has its own cause; closed and ◐ rows are
// unchanged; the detail line names the nearest ancestor; t is hidden and refused
// with the undo hint, the same beads candidates() skips; N‖ counts own pauses only;
// and p's status texts tell the cascade.
func TestInheritedPause(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)
	origReady, origClaim, origIP, origPaused, origP, origU, origList := readyFrontier, claimBead, inProgressBeads, pausedBeads, pauseBead, unpauseBead, listAll
	defer func() {
		readyFrontier, claimBead, inProgressBeads, pausedBeads, pauseBead, unpauseBead, listAll = origReady, origClaim, origIP, origPaused, origP, origU, origList
	}()
	mn := []string{"paused:manual"}
	beads := []bd.Bead{
		{ID: "ch", Kind: contract.KindChart, Container: true, Labels: mn, Status: "open", Title: "Paused chart"},
		{ID: "cp", Kind: contract.KindCheckpoint, Container: true, Parent: "ch", Status: "in_progress", Title: "Mid checkpoint"},
		{ID: "lf", Kind: contract.KindResearch, Parent: "ch", Status: "open", Title: "Plain member"},
		{ID: "deep", Kind: contract.KindResearch, Parent: "cp", Status: "open", Title: "Deep member"},
		{ID: "fl", Kind: contract.KindResearch, Labels: []string{"paused:failing"}, Parent: "ch", Status: "open", Title: "Failing member"},
		{ID: "own", Kind: contract.KindResearch, Labels: mn, Parent: "ch", Status: "open", Title: "Own manual member"},
		{ID: "ab", Kind: contract.KindResearch, Labels: []string{"paused:abandoned"}, Parent: "ch", Status: "open", Title: "Abandoned member"},
		{ID: "wip", Kind: contract.Kind("unowned"), Parent: "ch", Status: "in_progress", Title: "Running member"},
		{ID: "done", Kind: contract.KindResearch, Parent: "ch", Status: "closed", Title: "Closed member"},
		{ID: "ch2", Kind: contract.KindChart, Container: true, Labels: []string{"paused:failing"}, Status: "open", Title: "Failing chart"},
		{ID: "free", Kind: contract.KindResearch, Parent: "ch2", Status: "open", Title: "Free member"},
	}
	m := New(nil, map[string]contract.Crewmate{"nami": &recordCrew{}})
	m.w, m.h = 120, 40
	m.beads = beads
	inherit := lipgloss.NewStyle().Foreground(lipgloss.Color("127")).Render("‖")
	for id, want := range map[string]string{
		"lf": inherit, "deep": inherit, "free": dim.Render("○"), "done": dimmer.Render("✓"),
		"fl":  lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render("‖"),
		"own": lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Render("‖"),
		"wip": lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render("◐"),
	} {
		if g := m.stateGlyph(m.beadByID(id)); g != want {
			t.Errorf("%s glyph = %q, want %q", id, g, want)
		}
	}
	if got := ansi.Strip(m.tabBar()); !strings.Contains(got, " · 5‖ · ") {
		t.Errorf("tab bar = %q, want · 5‖ (own pauses only: ch, fl, own, ab, ch2)", got)
	}
	lipgloss.SetColorProfile(termenv.Ascii)
	for id, want := range map[string]string{
		"lf": "paused      manual via ch", "deep": "paused      manual via ch",
		"fl": "paused      failing · manual via ch", "own": "paused      manual · manual via ch",
	} {
		if l := lineWith(t, m.detailView(m.beadByID(id), 0, 1, 30), "paused  "); strings.TrimRight(l, " ") != want {
			t.Errorf("%s detail paused line = %q, want %q", id, l, want)
		}
	}
	for _, id := range []string{"done", "free"} {
		if d := m.detailView(m.beadByID(id), 0, 1, 30); strings.Contains(d, "paused  ") {
			t.Errorf("%s detail shows a paused line:\n%s", id, d)
		}
	}

	// t: the ready frontier holds the open members; the press reads paused + in-progress.
	byID := func(ids ...string) (out []bd.Bead) {
		for _, id := range ids {
			out = append(out, *m.beadByID(id))
		}
		return out
	}
	ready := byID("lf", "deep", "fl", "own", "ab", "free")
	paused, inProgress := byID("ch", "fl", "own", "ab", "ch2"), byID("cp", "wip")
	readyFrontier = func() ([]bd.Bead, error) { return ready, nil }
	pausedBeads = func() ([]bd.Bead, error) { return paused, nil }
	inProgressBeads = func() ([]bd.Bead, error) { return inProgress, nil }
	claimed := ""
	claimBead = func(b bd.Bead) error { claimed = b.ID; return errors.New("stop here") }
	parity := loop.InheritedPause(ready, slices.Concat(paused, inProgress), func(id string) (bd.Bead, error) {
		return bd.Bead{}, errors.New("unexpected miss " + id)
	})
	for id, hint := range map[string]string{
		"lf":   "lf is paused via ch — select it and press p, or run 'bd label remove ch paused:manual'",
		"deep": "deep is paused via ch — select it and press p, or run 'bd label remove ch paused:manual'",
		"own":  "own is paused by hand — press p or run 'bd label remove own paused:manual' first",
		"ab":   "ab: abandoned effort may still be running; check, then 'bd label remove ab paused:abandoned'",
		"free": "",
	} {
		m.selGraph, claimed = id, ""
		if anc, err := parity(*m.beadByID(id)); err != nil || (anc != "") != (id != "free") {
			t.Errorf("%s: loop.InheritedPause = %q, %v", id, anc, err)
		}
		if hasKey(m.beadKeys(m.beadByID(id)), "t") == (hint != "") {
			t.Errorf("%s: footer t shown, want shown = %v", id, hint == "")
		}
		next, _ := settleRunNow(m.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")}))
		if hint == "" {
			if claimed != id {
				t.Errorf("%s: free member must reach the claim, status %q", id, next.(Model).status)
			}
		} else if claimed != "" || next.(Model).status != hint {
			t.Errorf("%s: t must refuse without claiming; status %q, want %q", id, next.(Model).status, hint)
		}
	}

	// A hop outside the reads and the snapshot goes to bd show; a failed show
	// refuses the press, never a silent "not paused" (as a failed lookup fails a tick).
	orphan := bd.Bead{ID: "orph", Kind: contract.KindResearch, Parent: "gone", Status: "open"}
	m.beads, ready = append(slices.Clone(beads), orphan), append(ready, orphan)
	origShow := showBead
	defer func() { showBead = origShow }()
	showBead = func(id string) (bd.Bead, error) { return bd.Bead{}, errors.New("no " + id) }
	m.selGraph, claimed = "orph", ""
	if next, _ := settleRunNow(m.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})); claimed != "" || next.(Model).status != "run-now: no gone" {
		t.Errorf("orph: failed show must refuse; status %q, claimed %q", next.(Model).status, claimed)
	}
	m.beads = beads

	// p status texts: pausing a container (idle or running), unpausing it, unpausing
	// a still-inherited member.
	listAll = func() ([]bd.Bead, error) { return beads, nil }
	pauseBead = func(string, string) error { return nil }
	unpauseBead = func(string, string) error { return nil }
	for id, want := range map[string]string{
		"ch2": "paused ch2 — its open members are out of dispatch too",
		"cp":  "paused cp — its open members are out of dispatch too; the running effort continues",
		"ch":  "unpaused ch — its members resume",
		"own": "unpaused own — still paused via ch",
	} {
		m.tab, m.selGraph = tabGraph, id
		if s := pressPause(t, m).status; s != want {
			t.Errorf("%s: p status = %q, want %q", id, s, want)
		}
	}
}

// Every ranked pause cause has its colour, so a new cause can't render unstyled.
func TestPauseStyleCoversRank(t *testing.T) {
	for _, c := range bd.PauseRank {
		if _, ok := pauseSty[c]; !ok {
			t.Errorf("pauseSty has no colour for %q", c)
		}
	}
}

// pressPause presses p, requires no bd write inside Update (ADR 0098 §1) and a
// pausing/unpausing status, then runs the Cmd and feeds its done message back.
func pressPause(t *testing.T, m Model) Model {
	t.Helper()
	b := m.selectedBead()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = next.(Model)
	if cmd == nil || m.pending[b.ID] == "" || (m.status != "pausing "+b.ID+"…" && m.status != "unpausing "+b.ID+"…") {
		t.Fatalf("%s: p press: cmd %v, pending %v, status %q; want a Cmd, pending and a …ing status", b.ID, cmd != nil, m.pending, m.status)
	}
	word := strings.Fields(m.status)[0] + "…" // the row says what the status says (ADR 0107 §1)
	if suf := ansi.Strip(m.rowSuffix(b)); !strings.HasSuffix(suf, "  "+word) {
		t.Errorf("%s: p press: row suffix %q; want %q", b.ID, suf, word)
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if suf := ansi.Strip(m.rowSuffix(b)); strings.Contains(suf, word) {
		t.Errorf("%s: p done: row suffix %q still reads %q", b.ID, suf, word)
	}
	return m
}

// A failed write shows "pause: <err>"/"unpause: <err>" and does not reload; a p
// on a bead with a write in flight is refused with a status and no Cmd, while
// another bead still proceeds (ADR 0098 §3).
func TestPauseOffTheLoop(t *testing.T) {
	origP, origU := pauseBead, unpauseBead
	defer func() { pauseBead, unpauseBead = origP, origU }()
	pauseBead = func(string, string) error { return errors.New("dolt busy") }
	unpauseBead = func(string, string) error { return errors.New("dolt locked") }
	for id, want := range map[string]string{"ext": "pause: dolt busy", "mn": "unpause: dolt locked"} {
		m := pausedGraph(t)
		m.selGraph = id
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
		next, reload := next.(Model).Update(cmd())
		if m = next.(Model); m.status != want || reload != nil || m.pending[id] != "" {
			t.Errorf("%s: failed p: status %q, reload %v, pending %v; want %q, none, cleared", id, m.status, reload != nil, m.pending, want)
		}
		if suf := ansi.Strip(m.rowSuffix(m.beadByID(id))); strings.Contains(suf, "…") || strings.Contains(suf, "✕") {
			t.Errorf("%s: failed p: row suffix %q; want the word cleared and no failure mark", id, suf)
		}
	}

	m := pausedGraph(t)
	m.selGraph = "ext"
	m.pending = map[string]string{"ext": "pausing"}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if cmd != nil || next.(Model).status != "ext: write in flight, wait" {
		t.Errorf("p on a pending bead: cmd %v, status %q; want refused", cmd != nil, next.(Model).status)
	}
	m.selGraph = "mn"
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")}); cmd == nil {
		t.Errorf("p on another bead must proceed while ext is pending")
	}
}

// · M waiting follows N live, counting alive waiting sessions whose bead isn't
// closed, and drops with live at narrow width (Spec D6).
func TestTabBarWaitingCount(t *testing.T) {
	m := New(nil, nil)
	m.beads = []bd.Bead{
		{ID: "a", Kind: contract.KindGrilling, Status: "in_progress"},
		{ID: "b", Kind: contract.KindGrilling, Status: "in_progress"},
		{ID: "c", Kind: contract.KindGrilling, Status: "in_progress"},
		{ID: "d", Kind: contract.KindSignal, Status: "closed"},
		{ID: "e", Kind: contract.KindGrilling, Status: "open"},
	}
	m.sessions = map[string]session.Info{
		"a": {State: session.Alive},
		"b": {State: session.Alive, Waiting: session.WaitTurn},
		"c": {State: session.Alive, Waiting: session.WaitAsk},
		"d": {State: session.Alive, Waiting: session.WaitTurn}, // done: not counted
		"e": {State: session.Exited, Waiting: session.WaitAsk}, // exited: not counted
	}
	m.w = 100
	if got := m.tabBar(); !strings.Contains(got, " · 4 live · 2 waiting · loop off") {
		t.Errorf("tab bar = %q, want · 4 live · 2 waiting before the loop", got)
	}
	m.w = 60
	if got := m.tabBar(); strings.Contains(got, "live") || strings.Contains(got, "waiting") {
		t.Errorf("narrow tab bar = %q, want waiting dropped with live", got)
	}
	m.w = 100
	m.sessions = map[string]session.Info{"a": {State: session.Alive}}
	if got := m.tabBar(); !strings.Contains(got, "1 live") || strings.Contains(got, "waiting") {
		t.Errorf("tab bar with none waiting = %q, want no waiting segment", got)
	}
}

// An integrate-conflict pause ranks manual > abandoned > integrate-conflict >
// failing > foreign-hold (ADR 0104 §4) and shows a cyan (44) ‖, its detail line
// naming the cause (§6).
func TestIntegrateConflictPause(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)
	ic := "paused:integrate-conflict"
	m := New(nil, nil)
	m.w, m.h = 120, 40
	next, _ := m.Update(loadedMsg{beads: []bd.Bead{
		{ID: "ch1", Kind: contract.KindChart, Container: true, Title: "Chart: Gate", Status: "open"},
		{ID: "ic", Kind: contract.Kind("unowned"), Labels: []string{ic}, Parent: "ch1", Status: "open", Title: "Stranded"},
		{ID: "icf", Kind: contract.Kind("unowned"), Labels: []string{"paused:failing", "paused:foreign-hold", ic}, Parent: "ch1", Status: "open", Title: "Stranded and failing"},
		{ID: "ab", Kind: contract.Kind("unowned"), Labels: []string{ic, "paused:abandoned"}, Parent: "ch1", Status: "open", Title: "Stranded and abandoned"},
		{ID: "mic", Kind: contract.Kind("unowned"), Labels: []string{ic, "paused:manual"}, Parent: "ch1", Status: "open", Title: "Stranded and kept"},
	}})
	m = next.(Model)
	cyan := lipgloss.NewStyle().Foreground(lipgloss.Color("44")).Render("‖")
	orange := lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Render("‖")
	magenta := lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Render("‖")
	graph := m.graphView(m.paneHeight())
	for needle, want := range map[string]string{"Stranded ": cyan, "Stranded and failing": cyan, "Stranded and abandoned": orange, "Stranded and kept": magenta} {
		if l := lineWith(t, graph, needle); !strings.Contains(l, want) {
			t.Errorf("%q row lacks %q: %q", needle, want, l)
		}
	}
	lipgloss.SetColorProfile(termenv.Ascii)
	for id, want := range map[string]string{
		"ic":  "paused      integrate-conflict",
		"icf": "paused      integrate-conflict · failing · foreign-hold",
		"ab":  "paused      abandoned · integrate-conflict",
		"mic": "paused      manual · integrate-conflict",
	} {
		d := m.detailView(m.beadByID(id), 0, 1, 30)
		if l := lineWith(t, d, "paused  "); strings.TrimRight(l, " ") != want {
			t.Errorf("%s detail paused line = %q, want %q", id, l, want)
		}
	}
}

// t on an AFK leaf paused integrate-conflict reads retry and dispatches (ADR 0104
// §3): a clean orphaned tree kept on effort/<id> is dropped before the claim, a
// dirty one refuses naming the tree, and a stale loop tree on an unpaused leaf is
// handled the same (ADR 0105 §2). A tree some in-flight effort or session holds is
// left for dispatch's held path. The run never lifts the pause: only integrate does.
func TestRunNowOnIntegrateConflict(t *testing.T) {
	origReady, origClaim, origDisp, origIP, origU, origOn, origDirty, origDrop, origSess, origList, origProbe := readyFrontier, claimBead, runDispatch, inProgressBeads, unpauseBead, git.WorktreeOn, treeDirty, dropOrphan, readSessions, listAll, probeLiveness
	defer func() {
		readyFrontier, claimBead, runDispatch, inProgressBeads, unpauseBead, git.WorktreeOn, treeDirty, dropOrphan, readSessions, listAll, probeLiveness = origReady, origClaim, origDisp, origIP, origU, origOn, origDirty, origDrop, origSess, origList, origProbe
	}()
	const wt = "/tmp/mugicrew-wt-7/wt"
	ic := bd.Bead{ID: "ic", Kind: contract.KindBuild, Labels: []string{"paused:integrate-conflict"}, Parent: "cp1", Status: "open"}
	plain := bd.Bead{ID: "pl", Kind: contract.KindBuild, Parent: "cp1", Status: "open"}
	inProgressBeads = func() ([]bd.Bead, error) { return nil, nil }
	readyFrontier = func() ([]bd.Bead, error) { return []bd.Bead{ic, plain}, nil }
	runDispatch = func(contract.Ctx, contract.Crewmate, bd.Bead, io.Writer, string, dispatch.InflightFunc) (dispatch.Outcome, error) {
		return dispatch.Outcome{Cut: dispatch.CutOK, Branch: "effort/ic"}, nil
	}
	probeLiveness = func([]string) map[string]bool { return nil }

	for _, c := range []struct {
		name, bead, holder string
		dirty              bool
		session            bool // a session has the tree as its @worktree
		prunable           bool // its directory is gone
		want               []string
		status             string // a refusal's status substring; "" dispatches
	}{
		{"no tree", "ic", "", false, false, false, []string{"claim ic"}, ""},
		{"clean kept tree", "ic", wt, false, false, false, []string{"drop " + wt, "claim ic"}, ""},
		{"dirty kept tree", "ic", wt, true, false, false, nil, wt},
		{"prunable kept tree", "ic", wt, true, false, true, []string{"drop " + wt, "claim ic"}, ""},
		{"stale loop tree, clean", "pl", wt, false, false, false, []string{"drop " + wt, "claim pl"}, ""},
		{"stale loop tree, dirty", "pl", wt, true, false, false, nil, wt},
		{"session's tree", "ic", wt, true, true, false, []string{"claim ic"}, ""},
		{"human checkout", "ic", "/home/me/repo-ic", true, false, false, []string{"claim ic"}, ""},
	} {
		var calls, writes []string
		var asked string
		git.WorktreeOn = func(branch string) (git.Holder, bool, error) {
			asked = branch
			return git.Holder{Path: c.holder, Prunable: c.prunable}, c.holder != "", nil
		}
		treeDirty = func(string) (bool, error) { return c.dirty, nil }
		dropOrphan = func(p string) error { calls = append(calls, "drop "+p); return nil }
		readSessions = func() (map[string]session.Info, error) {
			if c.session {
				return map[string]session.Info{"x": {Worktree: c.holder, State: session.Exited}}, nil
			}
			return nil, nil
		}
		listAll = func() ([]bd.Bead, error) { return nil, nil }
		claimBead = func(b bd.Bead) error { calls = append(calls, "claim "+b.ID); return nil }
		unpauseBead = func(id, cause string) error { writes = append(writes, "unpause "+id+" "+cause); return nil }
		m := New(nil, map[string]contract.Crewmate{"franky": &recordCrew{}})
		m.w, m.h = 120, 40
		m.beads, m.selGraph = []bd.Bead{ic, plain}, c.bead
		if f := ansi.Strip(m.footer()); c.bead == "ic" && (!strings.Contains(f, "t retry") || strings.Contains(f, "retry hold")) {
			t.Errorf("%s: footer = %q, want t retry", c.name, f)
		}
		next, cmd := settleRunNow(m.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")}))
		if s := next.(Model).status; (cmd == nil) != (c.status != "") || (c.status != "" && !strings.Contains(s, c.status)) {
			t.Errorf("%s: cmd %v, status %q; want refusal containing %q", c.name, cmd != nil, s, c.status)
		}
		if !slices.Equal(calls, c.want) {
			t.Errorf("%s: calls = %q, want %q", c.name, calls, c.want)
		}
		if asked != "effort/"+c.bead {
			t.Errorf("%s: holder looked up on %q, want effort/%s", c.name, asked, c.bead)
		}
		if cmd != nil {
			cmd()
			if len(writes) != 0 {
				t.Errorf("%s: a clean run wrote %q; only integrate lifts integrate-conflict", c.name, writes)
			}
		}
	}
}

// l on an HITL leaf paused integrate-conflict relaunches through the orphan confirm
// (ADR 0104 §3, ADR 0041 §3): the kept tree holding its effort/<id> is named, y
// drops it and launches on the reused branch.
func TestLaunchOnIntegrateConflict(t *testing.T) {
	origStart, origSwitch, origOn, origDrop, origSess, origProbe, origList := startSession, switchSession, git.WorktreeOn, dropOrphan, readSessions, probeLiveness, listAll
	defer func() {
		startSession, switchSession, git.WorktreeOn, dropOrphan, readSessions, probeLiveness, listAll = origStart, origSwitch, origOn, origDrop, origSess, origProbe, origList
	}()
	const wt = "/tmp/mugicrew-wt-5/wt"
	busy, starts := true, 0
	var asked string
	var dropped []string
	startSession = func(bd.Bead, contract.AgentCommand) error {
		starts++
		if busy {
			return exec.Command("sh", "-c", "exit 128").Run()
		}
		return nil
	}
	switchSession = func(string) error { return nil }
	git.WorktreeOn = func(branch string) (git.Holder, bool, error) { asked = branch; return git.Holder{Path: wt}, true, nil }
	dropOrphan = func(p string) error { dropped = append(dropped, p); busy = false; return nil }
	readSessions = func() (map[string]session.Info, error) { return nil, nil }
	probeLiveness = func([]string) map[string]bool { return nil }
	sg := bd.Bead{ID: "sg1", Kind: contract.KindSurgery, Labels: []string{"paused:integrate-conflict"}, Parent: "cp1", Status: "in_progress", Title: "Cut it"}
	listAll = func() ([]bd.Bead, error) { return []bd.Bead{sg}, nil }
	key := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

	m := New(nil, map[string]contract.Crewmate{"franky": &recordCrew{}})
	m.w, m.h = 200, 24
	m.beads, m.selGraph = []bd.Bead{sg}, sg.ID
	if hasKey(m.beadKeys(&m.beads[0]), "t") {
		t.Error("HITL leaf: footer must hide t")
	}
	m, cmd := settleLaunch(m.Update(key("l")))
	if m.editMode != editConfirmOrphan || cmd != nil || !strings.Contains(m.status, wt) {
		t.Fatalf("l: mode=%v cmd=%v status=%q, want the orphan confirm naming %s", m.editMode, cmd, m.status, wt)
	}
	if asked != "effort/sg1" {
		t.Errorf("holder looked up on %q, want effort/sg1", asked)
	}
	if m, cmd = settleLaunch(settleWrite(m.Update(key("y")))); !slices.Equal(dropped, []string{wt}) || starts != 2 || cmd == nil {
		t.Errorf("y: dropped=%q starts=%d cmd=%v status=%q, want the tree dropped then a launch", dropped, starts, cmd != nil, m.status)
	}
}

// The detail pane shows effort  effort/<id> when the refresh's one effort-ref read
// found the ref, and no effort line otherwise; selecting reads nothing (ADR 0104 §7).
func TestDetailEffortLine(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	defer lipgloss.SetColorProfile(prev)
	origRead := readEfforts
	defer func() { readEfforts = origRead }()
	reads := 0
	readEfforts = func() (map[string]bool, error) { reads++; return nil, nil }
	m := New(nil, nil)
	m.w, m.h = 120, 40
	next, _ := m.Update(loadedMsg{
		beads: []bd.Bead{
			{ID: "cp1", Kind: contract.KindCheckpoint, Container: true, Title: "Checkpoint", Status: "open"},
			{ID: "b1", Kind: contract.KindBuild, Parent: "cp1", Status: "closed", Title: "Built"},
			{ID: "b2", Kind: contract.KindBuild, Parent: "cp1", Status: "open", Title: "Unbuilt"},
		},
		efforts: map[string]bool{"b1": true},
	})
	m = next.(Model)
	if l := lineWith(t, m.detailView(m.beadByID("b1"), 0, 1, 30), "effort  "); strings.TrimRight(l, " ") != "effort      effort/b1" {
		t.Errorf("b1 effort line = %q, want effort      effort/b1", l)
	}
	if d := m.detailView(m.beadByID("b2"), 0, 1, 30); strings.Contains(d, "effort/") {
		t.Errorf("b2 holds no ref, yet its detail shows one:\n%s", d)
	}
	if reads != 0 {
		t.Errorf("rendering read the effort refs %d times, want 0", reads)
	}
}
