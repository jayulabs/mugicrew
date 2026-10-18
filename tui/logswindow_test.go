package tui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"path/filepath"
	"testing"

	"jayulabs.org/mugicrew/eventlog"
)

// Logs opens on the newest 1000 events, scrolling past the oldest loads 1000 more and
// keeps them, the selection stays pinned by Key across a head extension and a page,
// and a rotation keeps the loaded rows (ADR 0231 §1).
func TestLogsWindowPagesOlderAndKeepsRowsAcrossRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.log")
	line := func(i int) string {
		return fmt.Sprintf(`{"time":"2026-01-01T00:%02d:%02d.%06dZ","level":"INFO","msg":"e%d","pid":"1"}`+"\n", i/3600%60, i/60%60, i%60, i)
	}
	write := func(flag, lo, hi int) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|flag, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		for i := lo; i < hi; i++ {
			f.WriteString(line(i))
		}
	}
	load := func(m Model) Model {
		t.Helper()
		m, _ = send(t, m, m.loadLogs()().(logsMsg))
		return m
	}
	page := func(m Model) Model { // j on the oldest row, then the page it asks for
		t.Helper()
		m, cmd := send(t, m, runeKey("j"))
		if cmd == nil {
			t.Fatal("j past the oldest row asked for nothing")
		}
		m, cmd = send(t, m, cmd())
		return settle(t, m, cmd)
	}
	write(os.O_TRUNC, 0, 2500)
	m := New(nil, nil).WithLog(eventlog.New(path, func(string) {}))
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	m = load(m)
	m, _ = send(t, m, runeKey("$"))
	if len(m.logs) != 1000 || m.logs[999].Msg != "e2499" {
		t.Fatalf("window %d events, newest %q", len(m.logs), m.logs[len(m.logs)-1].Msg)
	}
	oldest := m.logs[0].Key()

	// the selection on a middle row survives a head extension
	l := logs(m)
	l.sel = m.logs[500].Key()
	m.tabs[tabLogs] = l
	write(os.O_APPEND, 2500, 2503)
	m = load(m)
	if len(m.logs) != 1003 || logs(m).sel != m.logs[500].Key() {
		t.Fatalf("after extend: %d events, sel %v", len(m.logs), logs(m).sel)
	}

	// scrolling past the oldest row loads 1000 more and moves on to the newest of them
	l = logs(m)
	l.sel = oldest
	m.tabs[tabLogs] = l
	m = page(m)
	if len(m.logs) != 2003 || m.logs[0].Msg != "e500" || logs(m).sel != m.logs[999].Key() {
		t.Fatalf("after page: %d events, first %q, sel %v", len(m.logs), m.logs[0].Msg, logs(m).sel)
	}
	m = load(m) // the grown window stays after a no-op tick and a head extension
	write(os.O_APPEND, 2503, 2505)
	m = load(m)
	if len(m.logs) != 2005 {
		t.Fatalf("window shrank to %d", len(m.logs))
	}

	// a rotation loses and duplicates no loaded rows
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	write(os.O_TRUNC, 2505, 2510)
	m = load(m)
	seen := map[string]bool{}
	for _, ev := range m.logs {
		if seen[ev.Msg] {
			t.Fatalf("%s duplicated", ev.Msg)
		}
		seen[ev.Msg] = true
	}
	if len(m.logs) != 2010 || m.logs[0].Msg != "e500" || m.logs[2009].Msg != "e2509" {
		t.Fatalf("after rotation: %d events %s..%s", len(m.logs), m.logs[0].Msg, m.logs[len(m.logs)-1].Msg)
	}
	// the older read continues across it: the cursor sat in the old live file, now .1
	l = logs(m)
	l.sel = m.logs[0].Key()
	m.tabs[tabLogs] = l
	m = page(m)
	if len(m.logs) != 2510 || m.logs[0].Msg != "e0" {
		t.Fatalf("after rotated page: %d events, first %q", len(m.logs), m.logs[0].Msg)
	}
	if !m.logCur.Done {
		t.Fatalf("cursor %+v", m.logCur)
	}
}

// A page whose newest event is a hidden DEBUG one leaves the selection on the newest
// event the page shows, not the top row.
func TestLogsPagingSkipsHiddenDebugAtPageHead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1500; i++ {
		lvl := "INFO"
		if i == 499 {
			lvl = "DEBUG"
		}
		fmt.Fprintf(f, `{"time":"2026-01-01T00:%02d:%02d.%06dZ","level":"%s","msg":"e%d","pid":"1"}`+"\n", i/3600%60, i/60%60, i%60, lvl, i)
	}
	f.Close()
	m := New(nil, nil).WithLog(eventlog.New(path, func(string) {}))
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	m, _ = send(t, m, m.loadLogs()().(logsMsg))
	m, _ = send(t, m, runeKey("$"))
	l := logs(m)
	l.sel = m.logs[0].Key()
	m.tabs[tabLogs] = l
	m, cmd := send(t, m, runeKey("j"))
	m, cmd = send(t, m, cmd())
	m = settle(t, m, cmd)
	if _, ev := logs(m).current(); ev == nil || ev.Msg != "e498" {
		t.Fatalf("cursor on %+v, want e498", ev)
	}
}

// Under a filter the header's m is the whole log, not the 1000-event window: 2500
// matches read "2500 of 2500", never "2500 of 1000".
func TestLogsFilterHeaderCountsWholeLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.log")
	var b []byte
	for i := 0; i < 2500; i++ {
		b = fmt.Appendf(b, `{"time":"2026-01-01T00:00:00.%06dZ","level":"INFO","msg":"session e%d","pid":"1"}`+"\n", i, i)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(nil, nil).WithLog(eventlog.New(path, func(string) {}))
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	m, _ = send(t, m, m.loadLogs()().(logsMsg))
	m, _ = send(t, m, runeKey("$"))
	m, _ = send(t, m, runeKey("/"))
	m, cmd := send(t, m, runeKey("s"))
	for cmd != nil {
		msg := cmd()
		m, cmd = send(t, m, msg)
	}
	if got := logs(m).headTotal(); got != 2500 || len(logs(m).rows()) != 2500 {
		t.Fatalf("header %d of %d, want 2500 of 2500", len(logs(m).rows()), got)
	}
}
