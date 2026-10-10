package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/loop"
	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/session"
)

// TestMain keeps tests that don't capture events from appending to the real repo's
// event log, and from reading the real repo's loop lock.
func TestMain(m *testing.M) {
	eventlogtest.Discard()
	readLoop = func() (looplock.Status, error) { return looplock.Status{State: looplock.Off}, nil }
	setBars = func(map[string]bool, map[string]session.Info) error { return nil }                  // never touch a real tmux server
	pauseBead = func(string, string) error { return errors.New("pauseBead not stubbed") }          // never label a real bead
	pausedBeads = func() ([]bd.Bead, error) { return nil, nil }                                    // never read a real bd
	inProgressBeads = func() ([]bd.Bead, error) { return nil, nil }                                // never read a real bd
	openBeads = func() ([]bd.Bead, error) { return nil, nil }                                      // never read a real bd
	showBead = func(string) (bd.Bead, error) { return bd.Bead{}, nil }                             // never read a real bd; an unknown hop ends the walk
	hitlClaim = func(string) error { return nil }                                                  // never claim a real bead
	hitlUnclaim = func(string) error { return nil }                                                // never release a real bead
	listAll = func() ([]bd.Bead, error) { return nil, errors.New("listAll not stubbed") }          // never read a real bd
	readEfforts = func() (map[string]bool, error) { return nil, nil }                              // never read a real repo
	readPool = func() loop.Setting { return loop.Setting{N: loop.DefaultPool, Source: "default"} } // never read a real repo
	readAutoExit = func() loop.AutoExit { return loop.AutoExit{D: loop.DefaultAutoExit, Source: "default"} }
	git.WorktreeOn = func(string) (git.Holder, bool, error) { return git.Holder{}, false, nil } // never read a real repo's trees
	os.Exit(m.Run())
}

// A fixture event log: .1 holds the oldest event, the live file the rest plus a
// garbage line and a Debug record.
func logFixture(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "events.jsonl")
	os.WriteFile(p+".1", []byte(`{"time":"2026-09-29T10:00:00Z","level":"INFO","msg":"effort.started","bead":"old1","effort":"e0"}`+"\n"), 0o644)
	os.WriteFile(p, []byte(
		`{"time":"2026-09-29T11:00:00Z","level":"INFO","msg":"effort.started","bead":"b1","effort":"e1","pid":42}`+"\n"+
			"not json {\n"+
			`{"time":"2026-09-29T11:00:01Z","level":"DEBUG","msg":"effort.busy","bead":"b2"}`+"\n"+
			`{"time":"2026-09-29T11:05:00Z","level":"ERROR","msg":"effort.failed","bead":"b1","effort":"e1","exit":1,"dur_ms":1234567,"err":"reconcile: land\nrebase conflict"}`+"\n"), 0o644)
	return p
}

func loadedLogsModel(t *testing.T, path string) Model {
	t.Helper()
	m := New(nil, nil).WithLog(eventlog.New(path, nil))
	next, _ := m.Update(m.loadLogs()())
	m = next.(Model)
	m.tab, m.w, m.h = tabLogs, 100, 24
	return m
}

// The tab reads .1 then the live file in append order, lists newest first, hides
// Debug, and counts the unparseable line visibly.
func TestLogsTabNewestFirstSkipsGarbage(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	defer lipgloss.SetColorProfile(prev)

	m := loadedLogsModel(t, logFixture(t))
	if len(m.logs) != 4 || m.logSkipped != 1 {
		t.Fatalf("read %d events, %d skipped; want 4 and 1", len(m.logs), m.logSkipped)
	}
	v := m.View()
	failed, started, old := strings.Index(v, "effort.failed b1"), strings.Index(v, "effort.started b1"), strings.Index(v, "effort.started old1")
	if failed < 0 || started < 0 || old < 0 || !(failed < started && started < old) {
		t.Errorf("want newest-first failed < started < old(.1), got %d %d %d:\n%s", failed, started, old, v)
	}
	if strings.Contains(v, "effort.busy") {
		t.Error("Debug records must be hidden")
	}
	if !strings.Contains(v, "1 unparsed lines") {
		t.Errorf("skipped count not shown:\n%s", v)
	}
	// A started with no end is just a row: no liveness verdict is drawn (ADR 0033 §3).
	if strings.Contains(v, "alive") || strings.Contains(v, "dead") {
		t.Error("the Logs tab must never infer liveness")
	}
}

// The detail pane shows every attr of the selected event, a multi-line err intact,
// and integers unmangled; j moves the selection to the next-older event.
func TestLogsDetailShowsAllAttrs(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	defer lipgloss.SetColorProfile(prev)

	m := loadedLogsModel(t, logFixture(t))
	d := m.logDetail()
	for _, want := range []string{"effort.failed", "ERROR", "e1", "1234567", "exit", "reconcile: land", "rebase conflict"} {
		if !strings.Contains(d, want) {
			t.Errorf("detail missing %q:\n%s", want, d)
		}
	}
	next, _ := m.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if d := next.(Model).logDetail(); !strings.Contains(d, "effort.started") || !strings.Contains(d, "42") {
		t.Errorf("j should select the next-older event (started, pid 42):\n%s", d)
	}
}

// The tick re-reads only when the files' stat changed.
func TestLogsNoRereadWhenStatUnchanged(t *testing.T) {
	path := logFixture(t)
	m := loadedLogsModel(t, path)
	reads := 0
	origRead := readLog
	readLog = func(p string) ([]eventlog.Event, int, error) { reads++; return eventlog.Read(p) }
	defer func() { readLog = origRead }()

	next, _ := m.Update(m.loadLogs()())
	m = next.(Model)
	if reads != 0 || len(m.logs) != 4 {
		t.Errorf("unchanged stat re-read the log (%d reads) or dropped events (%d)", reads, len(m.logs))
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"time":"2026-09-29T12:00:00Z","level":"INFO","msg":"effort.ended","bead":"b3"}` + "\n")
	f.Close()
	next, _ = m.Update(m.loadLogs()())
	if reads != 1 || len(next.(Model).logs) != 5 {
		t.Errorf("a changed stat must re-read once: reads=%d events=%d", reads, len(next.(Model).logs))
	}
}

// A missing live file is "no events yet" with its path; a log that can't be read
// or resolved says "event log unavailable" with the reason (FR51).
func TestLogsEmptyStateNoEventsYet(t *testing.T) {
	p := filepath.Join(t.TempDir(), "events.jsonl")
	m := loadedLogsModel(t, p)
	if v := m.logDetail(); !strings.Contains(v, "no events yet — "+p) || strings.Contains(v, "unavailable") {
		t.Errorf("want the no-events state naming %s:\n%s", p, v)
	}
	if !strings.Contains(m.View(), "no events yet") {
		t.Errorf("the master row must say so too:\n%s", m.View())
	}
	// A log whose events are all filtered out is not empty.
	m = loadedLogsModel(t, filterFixture(t))
	m.logScope = "nope"
	if d := m.logDetail(); !strings.Contains(d, "no matching events") {
		t.Errorf("filtered-out log must not claim no events:\n%s", d)
	}
}

func TestLogsEmptyStateUnavailable(t *testing.T) {
	p := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.Mkdir(p, 0o755); err != nil { // the live "file" is a directory: reads fail, not ENOENT
		t.Fatal(err)
	}
	m := loadedLogsModel(t, p)
	if v := m.logDetail(); !strings.Contains(v, "event log unavailable:") || strings.Contains(v, "no events yet") {
		t.Errorf("an unreadable log must not look empty:\n%s", v)
	}

	t.Chdir(t.TempDir()) // outside any repo: the path can't resolve
	m = m.WithLog(eventlog.Open(nil))
	next, _ := m.Update(m.loadLogs()())
	if v := next.(Model).logDetail(); !strings.Contains(v, "event log unavailable: git rev-parse") {
		t.Errorf("an unresolvable path must say why:\n%s", v)
	}
}

// The emitter's one-time warning reaches the status line once, on the tick, even
// with the Logs tab never opened.
func TestLogsWarningOnStatusLine(t *testing.T) {
	l := eventlog.New("", nil)
	l.Logger("tui").Info("effort.started") // unresolvable: fires the warning
	m := New(nil, nil).WithLog(l)
	next, _ := m.Update(tickMsg{})
	m = next.(Model)
	if !strings.HasPrefix(m.status, "mugicrew: event log unavailable") || !m.logWarned {
		t.Errorf("status = %q", m.status)
	}
	m.status = ""
	next, _ = m.Update(tickMsg{})
	if next.(Model).status != "" {
		t.Error("the warning must surface only once")
	}
}

// Every tick re-reads the log (stat-gated) whatever the tab, so the finished
// flash sees a hitl.exited (ADR 0067 §2; supersedes FR46's lazy read), and F5
// reloads it with the beads while r is unbound (Spec FR17-18).
func TestLogsReadEveryTick(t *testing.T) {
	m := New(nil, nil).WithLog(eventlog.New(logFixture(t), nil))
	next, cmd := m.Update(tickMsg{})
	if n := len(cmd().(tea.BatchMsg)); n != 3 {
		t.Errorf("tick batches %d cmds, want load+logs+tick", n)
	}
	// embedded-Dolt bd serializes: a tick while that load is in flight must not stack another
	if _, cmd = next.Update(tickMsg{}); len(cmd().(tea.BatchMsg)) != 2 {
		t.Errorf("a tick while a load is in flight must skip the load")
	}
	origList := listAll
	defer func() { listAll = origList }()
	listAll = func() ([]bd.Bead, error) { return nil, nil }
	next, cmd = m.updateKey(tea.KeyMsg{Type: tea.KeyF5})
	if n := len(cmd().(tea.BatchMsg)); n != 2 || next.(Model).status != "refreshing…" {
		t.Errorf("F5 batches %d cmds (status %q), want load+logs", n, next.(Model).status)
	}
	for _, tb := range []tab{tabGraph, tabInbox, tabSearch, tabHistory, tabLogs} {
		m.tab = tb
		if next, cmd = m.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}); cmd != nil || next.(Model).status != "" {
			t.Errorf("%v: r must do nothing, got cmd=%v status=%q", tb, cmd != nil, next.(Model).status)
		}
		if hasKey(m.viewKeys(), "r") || hasKey(m.viewKeys(), "F5") {
			t.Errorf("%v: the hint bar must not show refresh", tb)
		}
	}
}

// A clean hitl.exited the tick reads flashes `✓ <id> finished` once; the next
// tick doesn't repeat it, history at startup never flashes, and a hitl.exited
// carrying err (a failed CLI triage launch) is no finish (ADR 0067 §2).
func TestFinishedFlashOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	write := func(line string) {
		f, _ := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		f.WriteString(line + "\n")
		f.Close()
	}
	m := New(nil, nil).WithLog(eventlog.New(path, nil))
	read := func() {
		next, _ := m.Update(m.loadLogs()())
		m = next.(Model)
	}
	read() // no log file yet: still seeds, so the first exit written below flashes
	write(`{"time":"2026-10-05T10:00:00Z","level":"INFO","msg":"hitl.exited","bead":"ws-first"}`)
	read()
	if m.status != "✓ ws-first finished" {
		t.Fatalf("a TUI started before any log must flash the first exit: %q", m.status)
	}
	m = New(nil, nil).WithLog(eventlog.New(path, nil))
	read()
	if m.status != "" {
		t.Fatalf("history must not flash at startup: %q", m.status)
	}
	write(`{"time":"2026-10-05T11:00:00Z","level":"INFO","msg":"hitl.exited","bead":"mc-a","exit":0}`)
	write(`{"time":"2026-10-05T11:00:01Z","level":"INFO","msg":"hitl.exited","bead":"mc-f","exit":2,"err":"exit status 2"}`)
	write(`{"time":"2026-10-05T11:00:02Z","level":"INFO","msg":"hitl.failed","bead":"mc-g"}`)
	write(`{"time":"2026-10-05T11:00:03Z","level":"INFO","msg":"hitl.exited","bead":"mc-d","exit":0,"dropped":true}`)
	m.editMode, m.status = editConfirmDrop, "drop?"
	read()
	if m.status != "drop?" {
		t.Fatalf("an open confirm keeps its prompt; got %q", m.status)
	}
	m.editMode = editNone
	read()
	if m.status != "✓ mc-a finished" {
		t.Fatalf("status = %q, want ✓ mc-a finished only (no failure, no drop)", m.status)
	}
	m.status = ""
	write(`{"time":"2026-10-05T11:00:03Z","level":"INFO","msg":"effort.ended","bead":"mc-b"}`)
	read()
	if m.status != "" {
		t.Errorf("the next read must not repeat the flash: %q", m.status)
	}
	if v := m.View(); strings.Contains(v, "detached") {
		t.Errorf("no human-facing label says detached:\n%s", v)
	}
}

// A session.failed's result group reads as indented sub-keys, not a JSON blob, and
// its multi-line stderr tail keeps one line per row under its key (ADR 0029 §4).
func TestLogsDetailRendersSessionPayload(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	defer lipgloss.SetColorProfile(prev)

	p := filepath.Join(t.TempDir(), "events.jsonl")
	os.WriteFile(p, []byte(`{"time":"2026-09-29T11:00:00Z","level":"ERROR","msg":"session.failed","bead":"b1","effort":"e1",`+
		`"session":"0b6f…","exit":1,"dur_ms":900,"result":{"subtype":"error_max_turns","terminal_reason":"max_turns","is_error":true,`+
		`"errors":["Reached maximum number of turns (1)"],"result":""},"stderr":"warn: first\n[claude-code:boom] second\n"}`+"\n"), 0o644)
	m := loadedLogsModel(t, p)
	lines := strings.Split(m.logDetail(), "\n")
	row := func(prefix string) string {
		for _, l := range lines {
			if strings.HasPrefix(strings.TrimRight(l, " "), prefix) {
				return l
			}
		}
		t.Errorf("no detail row starting %q:\n%s", prefix, strings.Join(lines, "\n"))
		return ""
	}
	if strings.TrimSpace(row("result")) != "result" {
		t.Error("the result group gets a header row of its own")
	}
	for _, sub := range []string{"subtype", "terminal_reason", "is_error", "errors"} {
		row("  " + sub)
	}
	if r := row("  terminal_reason"); !strings.HasSuffix(r, "max_turns") {
		t.Errorf("terminal_reason row = %q", r)
	}
	st, second := row("stderr"), row(strings.Repeat(" ", strings.Index(row("stderr"), "warn"))+"[claude-code:boom]")
	if !strings.HasSuffix(st, "warn: first") || second == "" {
		t.Errorf("stderr must keep its lines aligned under its value column:\n%s", strings.Join(lines, "\n"))
	}
}

// A failed row carries its short error so failures triage from the list (FR42):
// err's first ": " segment, else result.subtype.
func TestLogsRowShowsShortError(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	defer lipgloss.SetColorProfile(prev)

	p := filepath.Join(t.TempDir(), "events.jsonl")
	os.WriteFile(p, []byte(
		`{"time":"2026-09-29T11:00:00Z","level":"ERROR","msg":"effort.failed","bead":"b1","err":"reconcile: land\nrebase conflict"}`+"\n"+
			`{"time":"2026-09-29T11:01:00Z","level":"ERROR","msg":"session.failed","bead":"b2","result":{"subtype":"error_max_turns"}}`+"\n"), 0o644)
	m := loadedLogsModel(t, p)
	m.w = 200 // widen the master column; even so the subtype may arrive truncated
	v := m.View()
	for _, want := range []string{"effort.failed b1 reconcile", "session.failed b2 error_max"} {
		if !strings.Contains(v, want) {
			t.Errorf("row missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "b1 reconcile: land") {
		t.Error("only err's first segment belongs on the row")
	}
}

// A mixed-level, mixed-bead log: chart ch1 with children c1, c2 (c2's child g1 is
// a grandchild, knowingly not caught by Chart scope, ADR 0029 §3), plus b9 outside.
func filterFixture(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "events.jsonl")
	var b strings.Builder
	for _, l := range []struct{ level, msg, bead, parent string }{
		{"INFO", "claim", "ch1", ""},
		{"INFO", "effort.started", "c1", "ch1"},
		{"DEBUG", "effort.busy", "c1", "ch1"},
		{"ERROR", "effort.failed", "c2", "ch1"},
		{"INFO", "effort.started", "g1", "c2"},
		{"WARN", "land.skipped", "b9", ""},
		{"ERROR", "session.failed", "b9", ""},
	} {
		fmt.Fprintf(&b, `{"time":"2026-09-29T11:00:00Z","level":%q,"msg":%q,"bead":%q,"parent":%q}`+"\n", l.level, l.msg, l.bead, l.parent)
	}
	os.WriteFile(p, []byte(b.String()), 0o644)
	return p
}

// visible is the Logs rows as "msg bead", newest first.
func visible(m Model) []string {
	var out []string
	for _, r := range m.logRows() {
		out = append(out, m.logs[r].Msg+" "+m.logs[r].Bead)
	}
	return out
}

func press(t *testing.T, m Model, k string) Model {
	t.Helper()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	if k == "esc" {
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	}
	next, _ := m.updateKey(msg)
	return next.(Model)
}

// d reveals then re-hides Debug rows; e shows only Error rows and toggles back.
func TestLogsDebugAndErrorsToggles(t *testing.T) {
	m := loadedLogsModel(t, filterFixture(t))
	all := len(visible(m))
	if all != 6 || slices.Contains(visible(m), "effort.busy c1") {
		t.Fatalf("default hides Debug only: %v", visible(m))
	}
	m = press(t, m, "d")
	if !slices.Contains(visible(m), "effort.busy c1") || !strings.Contains(m.tabBar(), "+debug") {
		t.Errorf("d must reveal Debug and say so in the tab: %v %q", visible(m), m.tabBar())
	}
	if m = press(t, m, "d"); len(visible(m)) != all {
		t.Errorf("d again must re-hide Debug: %v", visible(m))
	}
	m = press(t, m, "e")
	if got := visible(m); !slices.Equal(got, []string{"session.failed b9", "effort.failed c2"}) || !strings.Contains(m.tabBar(), "errors only") {
		t.Errorf("e = errors only, newest first: %v %q", got, m.tabBar())
	}
	if m.editMode != editNone {
		t.Error("e on Logs must not open the bead editor")
	}
	if m = press(t, m, "e"); len(visible(m)) != all {
		t.Errorf("e again restores all levels: %v", visible(m))
	}
}

// L on a Graph leaf scopes to that bead; on a Chart to its own and direct
// children's events; esc clears the scope.
func TestLogsScopeFromGraph(t *testing.T) {
	m := loadedLogsModel(t, filterFixture(t))
	m.beads = []bd.Bead{
		{ID: "ch1", Kind: contract.KindChart, Container: true, Status: "open", Title: "Chart"},
		{ID: "c1", Kind: contract.KindResearch, Parent: "ch1", Status: "open", Title: "leaf"},
		{ID: "c2", Kind: contract.KindResearch, Parent: "ch1", Status: "open", Title: "leaf2"},
	}
	m.rebuildGroups()
	m.tab = tabGraph

	m.selGraph = "c1"
	leaf := press(t, m, "L")
	if leaf.tab != tabLogs || !slices.Equal(visible(leaf), []string{"effort.started c1"}) || !strings.Contains(leaf.View(), "scope:c1") {
		t.Errorf("L on a leaf: tab=%v rows=%v view=%q", leaf.tab, visible(leaf), leaf.View())
	}

	m.selGraph = "ch1"
	chart := press(t, m, "L")
	if got := visible(chart); !slices.Equal(got, []string{"effort.failed c2", "effort.started c1", "claim ch1"}) {
		t.Errorf("L on a Chart = own + direct children, no grandchild: %v", got)
	}
	// The scope lives in the filter bar now (Spec FR8), not the tab bar.
	if !strings.Contains(chart.View(), "FILTER  scope:ch1+children") || !strings.Contains(chart.tabBar(), "Logs▼") ||
		strings.Contains(chart.tabBar(), "scope") || !strings.Contains(chart.footer(), "esc clear") {
		t.Errorf("scope must be visible and clearable: bar=%q footer=%q", chart.tabBar(), chart.footer())
	}
	cleared := press(t, chart, "esc")
	if len(visible(cleared)) != 6 || strings.Contains(cleared.View(), "FILTER") || strings.Contains(cleared.tabBar(), "▼") || strings.Contains(cleared.footer(), "esc") {
		t.Errorf("esc restores the full list: %v %q", visible(cleared), cleared.tabBar())
	}

	// Off the Graph, L plain-jumps to Logs and keeps any scope (ADR 0058 §1).
	chart.tab = tabInbox
	if in := press(t, chart, "L"); in.tab != tabLogs || in.logScope != "ch1" || !in.logScopeKids {
		t.Errorf("L from Inbox: tab=%v scope=%q kids=%v, want Logs keeping ch1+children", in.tab, in.logScope, in.logScopeKids)
	}
}

// Selection is anchored to the event's identity: a reload that appends newer
// events (or a rotation that re-bases indices) keeps the selected event.
func TestLogsSelectionSurvivesReload(t *testing.T) {
	path := logFixture(t)
	m := loadedLogsModel(t, path)
	next, _ := m.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = next.(Model)
	if _, ev := m.selectedLog(); ev == nil || ev.Msg != "effort.started" {
		t.Fatalf("j should select effort.started, got %+v", ev)
	}
	// Rotate: the live file becomes .1 (dropping the old .1), then a newer event lands.
	os.Rename(path, path+".1")
	os.WriteFile(path, []byte(`{"time":"2026-09-29T12:00:00Z","level":"INFO","msg":"effort.ended","bead":"b3"}`+"\n"), 0o644)
	next, _ = m.Update(m.loadLogs()())
	m = next.(Model)
	if _, ev := m.selectedLog(); ev == nil || ev.Msg != "effort.started" || ev.Bead != "b1" {
		t.Errorf("selection moved off effort.started b1 across a reload: %+v", ev)
	}

	// Never moved: the newest shown on first load is pinned, so a newer event
	// landing does not swap the pane under the reader (FR45).
	path = logFixture(t)
	m = loadedLogsModel(t, path)
	_, first := m.selectedLog()
	want := first.Key()
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"time":"2026-09-29T13:00:00Z","level":"INFO","msg":"effort.ended","bead":"b4"}` + "\n")
	f.Close()
	next, _ = m.Update(m.loadLogs()())
	m = next.(Model)
	if _, ev := m.selectedLog(); ev == nil || ev.Key() != want {
		t.Errorf("unmoved selection swapped to the newer event across a reload: %+v", ev)
	}

	// A filter hiding the selection across a reload doesn't lose it.
	m.logScope = "b4" // hides the b1 selection
	f, _ = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"time":"2026-09-29T14:00:00Z","level":"INFO","msg":"effort.ended","bead":"b5"}` + "\n")
	f.Close()
	next, _ = m.Update(m.loadLogs()())
	m = next.(Model)
	m.logScope = ""
	if _, ev := m.selectedLog(); ev == nil || ev.Key() != want {
		t.Errorf("a filtered-out selection was re-pinned across a reload: %+v", ev)
	}
}

// filterLogs is filterFixture with beads loaded: ch1 a chart, c1/c2 research,
// g1 a surgery; b9 is unknown to the graph.
func filterLogs(t *testing.T) Model {
	t.Helper()
	m := loadedLogsModel(t, filterFixture(t))
	m.beads = []bd.Bead{
		{ID: "ch1", Kind: contract.KindChart, Container: true, Status: "open"},
		{ID: "c1", Kind: contract.KindResearch, Parent: "ch1", Status: "open"},
		{ID: "c2", Kind: contract.KindResearch, Parent: "ch1", Status: "open"},
		{ID: "g1", Kind: contract.KindSurgery, Parent: "c2", Status: "open"},
	}
	return m
}

// f on Logs: an event's kind is its bead's; the cycle is over the kinds present,
// and under a kind an event with no bead or an unknown one (b9) is hidden (FR3).
func TestLogsKindFilter(t *testing.T) {
	m := filterLogs(t)
	m.logs = append(m.logs, eventlog.Event{Level: "INFO", Msg: "loop.started"}) // no bead
	var got []contract.Kind
	for range 4 {
		m = press(t, m, "f")
		got = append(got, m.filters[tabLogs].kind)
	}
	want := []contract.Kind{contract.KindChart, contract.KindResearch, contract.KindSurgery, ""}
	if !slices.Equal(got, want) {
		t.Errorf("f steps = %v, want %v", got, want)
	}
	m = press(t, press(t, m, "f"), "f")                                                           // research
	if got := visible(m); !slices.Equal(got, []string{"effort.failed c2", "effort.started c1"}) { // no b9, no loop.started
		t.Errorf("kind:research rows = %v", got)
	}
	if !strings.Contains(m.View(), "FILTER  kind:research  — 2 of 7") || !strings.Contains(m.tabBar(), "Logs▼") {
		t.Errorf("filter bar / ▼ missing:\n%s", m.View())
	}
}

// / on Logs matches msg + bead id + rendered attrs, case-insensitively; kind and
// search must both match (FR4–5).
func TestLogsSearch(t *testing.T) {
	m := filterLogs(t)
	m.logs[1].Attrs = append(m.logs[1].Attrs, eventlog.Attr{K: "branch", V: "Feature/X"})
	if got := visible(search(t, m, "feature/x", false)); !slices.Equal(got, []string{"effort.started c1"}) {
		t.Errorf("attr match = %v", got)
	}
	if got := visible(search(t, m, "^effort.*c2", false)); !slices.Equal(got, []string{"effort.started g1", "effort.failed c2"}) {
		t.Errorf("msg + bead match = %v", got)
	}
	m = search(t, m, "effort", false)
	m.filters[tabLogs].kind = contract.KindSurgery
	if got := visible(m); !slices.Equal(got, []string{"effort.started g1"}) {
		t.Errorf("kind + search = %v", got)
	}
}

// esc on Logs clears the search, then the kind, then the bead scope (FR7).
func TestLogsEscOrder(t *testing.T) {
	m := filterLogs(t)
	m.logScope, m.logScopeKids = "ch1", true
	m = press(t, m, "f")
	m = search(t, m, "effort", false)
	if got := strings.Join(m.logTerms(), "  "); got != "kind:chart  /effort/  scope:ch1+children" || !strings.Contains(m.View(), "FILTER  kind:chart  /effort/") {
		t.Errorf("filter bar terms = %q:\n%s", got, m.View())
	}
	if m = press(t, m, "esc"); m.filters[tabLogs].re != nil || m.filters[tabLogs].kind == "" || m.logScope == "" {
		t.Fatalf("first esc clears only the search: %+v scope=%q", m.filters[tabLogs], m.logScope)
	}
	if m = press(t, m, "esc"); m.filters[tabLogs].kind != "" || m.logScope == "" {
		t.Fatalf("second esc clears only the kind: %+v scope=%q", m.filters[tabLogs], m.logScope)
	}
	if m = press(t, m, "esc"); m.logScope != "" || m.logScopeKids {
		t.Fatalf("third esc clears the scope: %q", m.logScope)
	}
}
