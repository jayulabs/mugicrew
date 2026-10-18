package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/session"
)

func ev(i int, level, msg, bead, parent string) eventlog.Event {
	return eventlog.Event{
		Time: epoch.Add(time.Duration(i) * time.Minute), Level: level, Msg: msg, Bead: bead, Parent: parent, Pid: "1",
		Attrs: []eventlog.Attr{{K: "time", V: "t" + msg}, {K: "note", V: "line one\nline two"}},
	}
}

func stubEvents() []eventlog.Event {
	return []eventlog.Event{
		ev(1, "INFO", "effort.started", "paused", "ch"),
		ev(2, "DEBUG", "probe.tick", "", ""),
		ev(3, "ERROR", "effort.failed", "run", "ch"),
		ev(4, "INFO", "chart.landed", "ch", "ini"),
		ev(5, "WARN", "loop.slow", "oldb", "old"),
	}
}

// logsModel is a Model on the Logs tab with stubBeads and stubEvents loaded.
func logsModel(t *testing.T, w, h int) Model {
	t.Helper()
	m := sized(t, w, h)
	m, _ = send(t, m, loadedMsg{beads: stubBeads(), live: map[string]bool{}, sessions: map[string]session.Info{}})
	m, _ = send(t, m, logsMsg{path: writeLog(t, stubEvents()), changed: true, events: stubEvents(), skipped: 2})
	m, _ = send(t, m, runeKey("$"))
	return m
}

// writeLog is evs as a JSONL event log in a temp dir: the file the filter scan reads.
func writeLog(t *testing.T, evs []eventlog.Event) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "events.jsonl")
	var b strings.Builder
	for _, e := range evs {
		j, _ := json.Marshal(map[string]string{"time": e.Time.Format(time.RFC3339Nano), "level": e.Level, "msg": e.Msg, "bead": e.Bead, "parent": e.Parent, "pid": e.Pid, "note": "line one\nline two"})
		b.Write(append(j, '\n'))
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// typeScan types s into the open / input, letting each key's scan finish.
func typeScan(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m = press(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func logs(m Model) logsTab { return m.tabAt(tabLogs).(logsTab) }

func body(m Model) string { return ansi.Strip(m.View()) }

func TestLogsRendersV1RowsAndHeader(t *testing.T) {
	v := body(logsModel(t, 120, 30))
	for _, want := range []string{"effort.started paused", "ERROR", "effort.failed run", "chart.landed ch", "loop.slow oldb", "2 unparsed lines"} {
		if !strings.Contains(v, want) {
			t.Errorf("Logs lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "probe.tick") {
		t.Errorf("debug event shown by default:\n%s", v)
	}
	if got := header(logsModel(t, 120, 30)); strings.Contains(got, "unparsed") {
		t.Errorf("unparsed indicator leaked into the shell header: %q", got)
	}
	// newest first
	if strings.Index(v, "loop.slow") > strings.Index(v, "effort.started") {
		t.Errorf("not newest first:\n%s", v)
	}
}

func TestLogsLevelKeys(t *testing.T) {
	m := press(t, logsModel(t, 120, 30), runeKey("v"))
	if v := body(m); !strings.Contains(v, "probe.tick") || !strings.Contains(v, "+debug") {
		t.Errorf("v shows debug + indicator:\n%s", v)
	}
	m = press(t, press(t, m, runeKey("v")), runeKey("e"))
	v := body(m)
	if !strings.Contains(v, "errors only") || !strings.Contains(v, "effort.failed") || strings.Contains(v, "chart.landed") {
		t.Errorf("e shows errors only:\n%s", v)
	}
}

func TestLogsWordFilterAndEscUnwind(t *testing.T) {
	m := logsModel(t, 120, 30)
	m = typeScan(t, press(t, m, runeKey("/")), "EFFORT run")
	if v := body(m); !strings.Contains(v, "effort.failed") || strings.Contains(v, "effort.started") || !strings.Contains(v, "1 of 4") {
		t.Errorf("live words filter:\n%s", v)
	}
	// q, digits and tab are text while typing
	m = typeScan(t, m, "q6")
	if got := logs(m).query(); got != "EFFORT runq6" || m.tab != tabLogs {
		t.Errorf("typed keys leaked to the shell: query %q tab %d", got, m.tab)
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEsc}) // revert
	if l := logs(m); l.typing || l.words != "" || len(l.rows()) != 4 {
		t.Errorf("esc reverts the input: %+v", l)
	}
	m = typeScan(t, press(t, m, runeKey("/")), "effort")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // keep
	if l := logs(m); l.typing || l.words != "effort" {
		t.Errorf("enter keeps the words: %+v", l)
	}
	// kind picker: pick the second item
	m, cmd := send(t, m, runeKey("f"))
	m = settle(t, m, cmd)
	if len(m.modals) != 1 {
		t.Fatal("f opens no picker")
	}
	m = press(t, m, runeKey("j"))
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	kind := logs(m).kind
	if kind == "" {
		t.Fatal("picker set no kind")
	}
	m = gotoKey(t, selectLogs(m), "l") // scope to a bead
	l := logs(m)
	if l.scope == "" {
		t.Fatal("L set no scope")
	}
	esc := tea.KeyMsg{Type: tea.KeyEsc}
	m = press(t, m, esc)
	if l := logs(m); l.words != "" || l.kind != kind || l.scope == "" {
		t.Errorf("first esc clears text only: %+v", l)
	}
	m = press(t, m, esc)
	if l := logs(m); l.kind != "" || l.scope == "" {
		t.Errorf("second esc clears kind: %+v", l)
	}
	m = press(t, m, esc)
	if l := logs(m); l.scope != "" {
		t.Errorf("third esc clears scope: %+v", l)
	}
}

// selectLogs leaves the selection on a stub event with a bead.
func selectLogs(m Model) Model {
	l := logs(m)
	for _, e := range l.events {
		if e.Msg == "effort.failed" {
			l.sel = e.Key()
		}
	}
	m.tabs[tabLogs] = l
	return m
}

func TestLogsScopeFromGraphIncludesChildren(t *testing.T) {
	m := selectBead(t, graphModel(t, 120, 30), "ch")
	m, _ = send(t, m, logsMsg{path: writeLog(t, stubEvents()), changed: true, events: stubEvents()})
	m = gotoKey(t, m, "l")
	if m.tab != tabLogs {
		t.Fatalf("g l left tab %d", m.tab)
	}
	v := body(m)
	for _, want := range []string{"scope:ch+children", "effort.started", "effort.failed", "chart.landed", "3 of 3"} {
		if !strings.Contains(v, want) {
			t.Errorf("scoped Logs lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "loop.slow") {
		t.Errorf("out-of-scope event shown:\n%s", v)
	}
	// a leaf scopes to itself alone
	m = selectBead(t, press(t, m, runeKey("4")), "run")
	m = gotoKey(t, m, "l")
	if l := logs(m); l.scope != "run" || l.kids || len(l.rows()) != 1 {
		t.Errorf("leaf scope: %+v rows %d", l, len(l.rows()))
	}
}

func TestLogsRevealAndScopeFromEvent(t *testing.T) {
	m := selectLogs(logsModel(t, 120, 30))
	m = gotoKey(t, m, "r")
	if m.tab != tabGraph || sel(m) != "run" {
		t.Errorf("g r: tab %d selection %q, want Graph on run", m.tab, sel(m))
	}
	// a closed root the default hides is shown for g r
	m = logsModel(t, 120, 30)
	m = press(t, m, runeKey("4"))
	m = press(t, m, runeKey("$"))
	l := logs(m)
	l.sel = l.events[4].Key() // loop.slow on oldb, under the hidden closed root
	m.tabs[tabLogs] = l
	m = gotoKey(t, m, "r")
	if m.tab != tabGraph || sel(m) != "oldb" || m.tabAt(tabGraph).(graphTab).q.status != statusAny {
		t.Errorf("g r on a hidden bead: tab %d sel %q", m.tab, sel(m))
	}
	// a bead the graph lacks stays on Logs and says so
	m = press(t, logsModel(t, 120, 30), runeKey("$"))
	l = logs(m)
	ghost := ev(9, "INFO", "ghost.event", "gone", "")
	m, _ = send(t, m, logsMsg{path: "/log", changed: true, events: append(l.events, ghost)})
	l = logs(m)
	l.sel = ghost.Key()
	m.tabs[tabLogs] = l
	// a bead the snapshot lacks is no selection: the menu offers the tab options only
	menu := press(t, m, runeKey("g")).View()
	if !strings.Contains(menu, "top") || strings.Contains(menu, "reveal") || strings.Contains(menu, "logs") {
		t.Errorf("menu on a missing bead:\n%s", menu)
	}
	for _, k := range []string{"G", "L"} {
		if n := press(t, m, runeKey(k)); n.tab != tabLogs {
			t.Errorf("%s left Logs", k)
		}
	}
}

func TestLogsDetailAndEnter(t *testing.T) {
	m := selectLogs(logsModel(t, 120, 30))
	before := m.View()
	m, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || m.View() != before {
		t.Error("enter did something")
	}
	m = press(t, m, runeKey("d"))
	if v := body(m); !strings.Contains(v, "note") || !strings.Contains(v, "line two") {
		t.Errorf("d lacks the detail:\n%s", v)
	}
	m = press(t, m, runeKey("d"))
	if v := body(m); strings.Contains(v, "line two") {
		t.Errorf("d did not close the detail:\n%s", v)
	}
	m = press(t, press(t, m, runeKey("d")), tea.KeyMsg{Type: tea.KeyEsc})
	if logs(m).open {
		t.Error("esc did not close the detail")
	}
}

func TestLogsSelectionPinnedByKey(t *testing.T) {
	m := logsModel(t, 120, 30)
	l := logs(m)
	_, top := l.current()
	pinned := top.Key()
	more := append(stubEvents(), ev(10, "INFO", "newer.event", "", ""))
	m, _ = send(t, m, logsMsg{path: "/log", changed: true, events: more})
	if _, got := logs(m).current(); got.Key() != pinned {
		t.Errorf("selection moved to %s when a newer event landed", got.Msg)
	}
	m = press(t, m, runeKey("k")) // up one: toward the newer
	if _, got := logs(m).current(); got.Msg != "newer.event" {
		t.Errorf("k selected %s", got.Msg)
	}
}

func TestLogsEmptyStatesAndUnchangedSkip(t *testing.T) {
	m := sized(t, 100, 24)
	m, _ = send(t, m, runeKey("$"))
	if !strings.Contains(body(m), "no events yet") {
		t.Errorf("empty:\n%s", body(m))
	}
	m, _ = send(t, m, logsMsg{path: "/log", changed: true, err: os.ErrPermission})
	if !strings.Contains(body(m), "event log unavailable") {
		t.Errorf("unreadable:\n%s", body(m))
	}

	// an unchanged stat re-reads nothing
	dir := t.TempDir()
	path := filepath.Join(dir, "events.log")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := eventlog.New(path, func(string) {})
	reads := 0
	old := newestLog
	newestLog = func(p string, n int) ([]eventlog.Event, int, eventlog.State, eventlog.Cursor, error) {
		reads++
		return old(p, n)
	}
	defer func() { newestLog = old }()
	m = New(nil, nil).WithLog(l)
	msg := m.loadLogs()().(logsMsg)
	m.logSig, m.logOff = msg.sig, msg.off
	m.loadLogs()()
	if reads != 1 {
		t.Errorf("read the log %d times, want 1 (unchanged stat skips)", reads)
	}
}

func TestLogsHintsAndBindingCoverage(t *testing.T) {
	bar := func(m Model) string { l := lines(m.View()); return l[len(l)-1] }
	m := selectLogs(logsModel(t, 160, 30))
	b := bar(m)
	for _, want := range []string{"d detail", "/ search", "f kind", "v debug", "e errors", "g goto"} {
		if !strings.Contains(b, want) {
			t.Errorf("hints lack %q: %q", want, b)
		}
	}
	if strings.Contains(b, "esc clear") || strings.Contains(b, "enter") {
		t.Errorf("hints list a dead key: %q", b)
	}
	if b := bar(typeText(t, press(t, m, runeKey("/")), "x")); !strings.Contains(b, "enter keep") || !strings.Contains(b, "esc revert") {
		t.Errorf("input hints = %q", b)
	}
	if b := bar(press(t, m, runeKey("v"))); !strings.Contains(b, "v -debug") {
		t.Errorf("v label = %q", b)
	}
	long := ev(11, "INFO", "long.event", "run", "")
	long.Attrs = []eventlog.Attr{{K: "stderr", V: numbered(80)}}
	scrollable := logsModel(t, 120, 30)
	scrollable, _ = send(t, scrollable, logsMsg{path: "/log", changed: true, events: append(stubEvents(), long)})
	scrollable = scrollLong(t, scrollable, long)
	narrow := logsModel(t, 60, 30)
	narrow, _ = send(t, narrow, logsMsg{path: "/log", changed: true, events: append(stubEvents(), long)})
	narrow = scrollLong(t, narrow, long)
	for name, m := range map[string]Model{
		"scrolled":        scrollable,
		"scrolled single": narrow,
		"rows":            selectLogs(logsModel(t, 120, 30)),
		"filtered":        press(t, selectLogs(logsModel(t, 120, 30)), runeKey("e")),
		"no bead":         press(t, logsModel(t, 120, 30), runeKey("j")), // the newest has a bead; see below
	} {
		if name == "no bead" {
			l := logs(m)
			l.debug = true
			l.sel = l.events[1].Key() // probe.tick, no bead
			m.tabs[tabLogs] = l
		}
		checkCoverage(t, "Logs "+name, m, true, "$")
	}
}

func TestLogsRendersNoSubprocess(t *testing.T) {
	logf := fakeTools(t)
	m := selectLogs(logsModel(t, 120, 40))
	m.View()
	m = press(t, m, runeKey("d"))
	m.View()
	if got, _ := os.ReadFile(logf); len(got) > 0 {
		t.Fatalf("Logs rendering started subprocesses:\n%s", got)
	}
}

func TestLogsFrameFits(t *testing.T) {
	for _, sz := range [][2]int{{40, 12}, {60, 20}, {90, 24}, {200, 50}} {
		for _, open := range []bool{false, true} {
			m := selectLogs(logsModel(t, sz[0], sz[1]))
			if open {
				m = press(t, m, runeKey("d"))
			}
			ls := lines(m.View())
			if len(ls) != sz[1] {
				t.Errorf("%v open=%v: %d lines", sz, open, len(ls))
			}
		}
	}
}

// scrollLong opens the long event's detail and scrolls it half a page.
func scrollLong(t *testing.T, m Model, long eventlog.Event) Model {
	t.Helper()
	l := logs(m)
	l.sel = long.Key()
	m.tabs[tabLogs] = l
	return press(t, press(t, m, runeKey("d")), tea.KeyMsg{Type: tea.KeyCtrlD})
}

// numbered is n distinct lines, so scrolling visibly moves the detail.
func numbered(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "stderr line %d\n", i)
	}
	return b.String()
}

// d opens detail as the h/2 bottom drawer at every width: header, list, ─ rule, detail.
func TestLogsDetailIsABottomDrawer(t *testing.T) {
	for _, w := range []int{70, 120} {
		m := press(t, selectLogs(logsModel(t, w, 30)), runeKey("d"))
		ls := lines(ansi.Strip(m.View()))
		rule, note := -1, -1
		for i, l := range ls {
			if strings.HasPrefix(l, strings.Repeat("─", w)) && rule < 0 {
				rule = i
			}
			if strings.Contains(l, "line two") {
				note = i
			}
		}
		if rule < 0 || note < rule {
			t.Fatalf("w=%d: no rule above detail (rule %d, detail %d):\n%s", w, rule, note, ansi.Strip(m.View()))
		}
		if v := ansi.Strip(m.View()); !strings.Contains(v, "2 unparsed lines") || strings.Contains(v, "[1/") || !strings.Contains(v, "❯") {
			t.Errorf("w=%d: header/selection/[i/n] wrong:\n%s", w, v)
		}
	}
}

func TestLogsExtendAppendsKeepsPinAndSumsSkipped(t *testing.T) {
	m := logsModel(t, 120, 30)
	m, _ = send(t, m, logsMsg{path: "/log", changed: true, events: stubEvents(), skipped: 2})
	n := len(m.logs)
	_, top := logs(m).current()
	pinned := top.Key()
	m, _ = send(t, m, logsMsg{path: "/log", changed: true, extend: true, skipped: 1, events: []eventlog.Event{ev(10, "INFO", "newer.event", "", "")}})
	if len(m.logs) != n+1 || m.logSkipped != 3 {
		t.Errorf("logs=%d (want %d) skipped=%d (want 3)", len(m.logs), n+1, m.logSkipped)
	}
	if _, got := logs(m).current(); got.Key() != pinned {
		t.Errorf("selection moved to %s on an extend", got.Msg)
	}
}

func TestLogsOverlappingExtendsAppendOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.log")
	write := func(flag int, line string) {
		f, err := os.OpenFile(path, flag|os.O_WRONLY|os.O_CREATE, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		f.WriteString(line)
	}
	write(os.O_TRUNC, "{\"msg\":\"a\"}\n")
	m := New(nil, nil).WithLog(eventlog.New(path, func(string) {}))
	m, _ = send(t, m, m.loadLogs()().(logsMsg))
	write(os.O_APPEND, "{\"msg\":\"b\"}\n")
	// two loads built from the same state read the same appended bytes
	a, b := m.loadLogs()().(logsMsg), m.loadLogs()().(logsMsg)
	m, _ = send(t, m, a)
	m, _ = send(t, m, b)
	if len(m.logs) != 2 {
		t.Errorf("logs=%d, want 2: the overlapping extend appended twice", len(m.logs))
	}
}

func TestLogsFilterStreamsNewestFirstUnderSearching(t *testing.T) {
	m := logsModel(t, 120, 30)
	m, _ = send(t, m, runeKey("/"))
	m, cmd := send(t, m, runeKey("e")) // keys return at once: the scan is off the event loop
	if v := body(m); !strings.Contains(v, "searching…") || strings.Contains(v, "effort.started") {
		t.Fatalf("before any result:\n%s", v)
	}
	m = settle(t, m, cmd)
	v := body(m)
	if strings.Contains(v, "searching…") || !strings.Contains(v, "effort.started") || !strings.Contains(v, "effort.failed") {
		t.Fatalf("after the scan:\n%s", v)
	}
	if strings.Index(v, "effort.failed") > strings.Index(v, "effort.started") {
		t.Errorf("streamed matches not newest first:\n%s", v)
	}
}

func TestLogsNewerFilterDropsOlderScan(t *testing.T) {
	m := logsModel(t, 120, 30)
	m, _ = send(t, m, runeKey("/"))
	m, old := send(t, m, runeKey("e"))
	m, cmd := send(t, m, runeKey("z")) // "ez" matches nothing
	m = settle(t, m, old)              // the superseded scan's message arrives late
	if rows := logs(m).rows(); len(rows) != 0 || !logs(m).scan.searching {
		t.Fatalf("stale results leaked: %d rows, searching %v", len(rows), logs(m).scan.searching)
	}
	m = settle(t, m, cmd)
	if l := logs(m); len(l.rows()) != 0 || l.scan.searching {
		t.Errorf("newer scan: %d rows, searching %v", len(l.rows()), l.scan.searching)
	}
}

func TestLogsSelectionPinnedByKeyWhileResultsStream(t *testing.T) {
	m := selectLogs(logsModel(t, 120, 30))
	want := logs(m).sel
	m = press(t, press(t, m, runeKey("/")), runeKey("o"))
	m = press(t, m, runeKey("r"))
	if l := logs(m); l.sel != want {
		t.Errorf("selection moved: %v, want %v", l.sel, want)
	}
	if _, ev := logs(m).current(); ev == nil || ev.Msg != "effort.failed" {
		t.Errorf("cursor left its event: %+v", ev)
	}
}

func TestLogsOlderPageUnderFilterKeepsSelection(t *testing.T) {
	m := logsModel(t, 120, 30)
	m, _ = send(t, m, runeKey("e"))
	l := logs(m)
	l.scan.found = slices.Repeat(m.logs[:1:1], len(m.logs)+5) // more matches than the window holds
	l.sel = l.scan.found[0].Key()
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabLogs] = l
	page := []eventlog.Event{{Msg: "older", Level: "INFO"}}
	m = m.foldOlder(olderLogsMsg{from: m.logCur, evs: page}) // must not panic
	if got := logs(m).sel; got != l.sel {
		t.Errorf("selection moved to %v, want %v", got, l.sel)
	}
}

// A closed bead outside the snapshot still has its kind: f lists it and filters its
// old events (FR24; ADR 0232), read off the event loop.
func TestLogsKindFilterSeesUnloadedClosedBeads(t *testing.T) {
	seam(t, &readKinds, func() (map[string]contract.Kind, error) {
		return map[string]contract.Kind{"oldb": contract.KindCritique}, nil
	})
	m := sized(t, 120, 30)
	var beads []bd.Bead
	for _, b := range stubBeads() {
		if b.ID != "oldb" {
			beads = append(beads, b)
		}
	}
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: map[string]session.Info{}})
	m, _ = send(t, m, logsMsg{path: writeLog(t, stubEvents()), changed: true, events: stubEvents()})
	m, _ = send(t, m, runeKey("$"))
	m, cmd := send(t, m, runeKey("f"))
	m = settle(t, m, cmd)
	if len(m.modals) != 1 || !slices.Contains(m.modals[0].items, string(contract.KindCritique)) {
		t.Fatalf("picker lacks the unloaded bead's kind: %+v", m.modals)
	}
	for range slices.Index(m.modals[0].items, string(contract.KindCritique)) {
		m = press(t, m, runeKey("j"))
	}
	m, cmd = send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = settle(t, m, cmd)
	if l := logs(m); l.kind != contract.KindCritique || len(l.rows()) != 1 || l.ev(l.rows()[0]).Bead != "oldb" {
		t.Fatalf("f %s found %d rows", l.kind, len(l.rows()))
	}
}

func TestLogsRowsStampDateClockZone(t *testing.T) {
	defer func(l *time.Location) { time.Local = l }(time.Local)
	evs := append(stubEvents(), ev(24*60+6, "INFO", "next.day", "", "")) // a second date
	for _, tc := range []struct {
		zone       *time.Location
		day1, day2 string
	}{
		{time.FixedZone("PDT", -7*3600), "2025-12-31 17:01:00 PDT", "2026-01-01 17:06:00 PDT"},
		{time.FixedZone("+0530", 5*3600+1800), "2026-01-01 05:31:00 +0530", "2026-01-02 05:36:00 +0530"},
	} {
		time.Local = tc.zone
		for _, w := range []int{40, 60, 120} {
			m := sized(t, w, 30)
			m, _ = send(t, m, loadedMsg{beads: stubBeads(), live: map[string]bool{}, sessions: map[string]session.Info{}})
			m, _ = send(t, m, logsMsg{path: writeLog(t, evs), changed: true, events: evs})
			m, _ = send(t, m, runeKey("$"))
			for name, v := range map[string]string{"plain": body(m), "selected": body(selectLogs(m))} {
				if w == 120 {
					for _, want := range []string{tc.day1, tc.day2} {
						if !strings.Contains(v, want) {
							t.Errorf("%s %s: Logs lacks stamp %q:\n%s", tc.zone, name, want, v)
						}
					}
				}
				for _, ln := range lines(v) {
					if ansi.StringWidth(ln) > w {
						t.Errorf("%s %s w=%d: row wider than the tab: %q", tc.zone, name, w, ln)
					}
				}
			}
		}
	}
}
