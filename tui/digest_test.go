package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
)

// The digest feeds landEv and Stats exactly as a full read would, across a head
// extension and a rotation (a full re-read).
func TestDigestMatchesFullRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.log")
	write := func(flag int, lines ...string) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|flag, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		for _, l := range lines {
			f.WriteString(l + "\n")
		}
	}
	noise := `{"time":"2026-01-01T00:00:01Z","msg":"bd.call","bead":"x"}`
	landed := func(msg, bead string) string {
		return `{"time":"2026-01-01T00:00:02Z","msg":"` + msg + `","bead":"` + bead + `"}`
	}
	m := New(nil, nil).WithLog(eventlog.New(path, func(string) {}))
	check := func(step string) {
		t.Helper()
		full, _, _ := eventlog.Read(path)
		want := landEvents(full)
		if len(m.landEv) != len(want) {
			t.Fatalf("%s: landEv = %v, want %v", step, m.landEv, want)
		}
		for k, v := range want {
			if m.landEv[k] != v {
				t.Fatalf("%s: landEv = %v, want %v", step, m.landEv, want)
			}
		}
		var n int
		for _, ev := range full {
			if inDigest(ev) {
				n++
			}
		}
		if len(m.digest) != n || len(m.digest) >= len(m.logs) {
			t.Fatalf("%s: digest %d, want %d of %d events", step, len(m.digest), n, len(m.logs))
		}
	}
	read := func() Model { // the window, then the whole-log digest a full or rotated read leaves to its own message
		next, cmd := send(t, m, m.loadLogs()().(logsMsg))
		if cmd != nil {
			next, _ = send(t, next, cmd().(digestMsg))
		}
		return next
	}
	write(os.O_TRUNC, noise, landed("land.failed", "a"), noise)
	m = read()
	check("first read")
	write(os.O_APPEND, noise, landed("land.landed", "a"), landed("gate.opened", "b"))
	m = read()
	check("extend")
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	write(os.O_TRUNC, landed("effort.paused", "c"), noise)
	m = read()
	check("rotation")
}

// A logs read that lands before the whole-log digest must not seed the finished-flash
// from the window alone: the digest then flashing every older hitl.exited would break
// FR26. An exit appended after the digest lands still flashes once.
func TestFinishedFlashWaitsForDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.log")
	lines := []string{`{"time":"2026-01-01T00:00:00Z","msg":"hitl.exited","bead":"old"}`}
	for range 1100 {
		lines = append(lines, `{"time":"2026-01-01T00:00:01Z","msg":"bd.call","bead":"x"}`)
	}
	write := func(flag int, lines ...string) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|flag, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		f.WriteString(strings.Join(lines, "\n") + "\n")
	}
	write(os.O_TRUNC, lines...)
	m := New(nil, nil).WithLog(eventlog.New(path, func(string) {}))
	m, cmd := send(t, m, m.loadLogs()().(logsMsg))
	if cmd == nil {
		t.Fatal("full read left no digest cmd")
	}
	m, _ = send(t, m, m.loadLogs()().(logsMsg)) // unchanged, before the digest
	m, _ = send(t, m, cmd().(digestMsg))
	if strings.Contains(m.status, "finished") {
		t.Fatalf("status = %q, want no flash for history", m.status)
	}
	write(os.O_APPEND, `{"time":"2026-01-01T00:00:09Z","msg":"hitl.exited","bead":"new"}`)
	m, _ = send(t, m, m.loadLogs()().(logsMsg))
	if !strings.Contains(m.status, "finished") {
		t.Fatalf("status = %q, want a flash for the new exit", m.status)
	}
}

// The f picker lists the kind of a bead whose only events are older than the window,
// once the digest lands, and leaves out a kind no event names.
func TestKindPickerSeesBeadsOlderThanWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.log")
	lines := []string{`{"time":"2026-01-01T00:00:00Z","msg":"bd.call","bead":"old"}`}
	for range 1100 {
		lines = append(lines, `{"time":"2026-01-01T00:00:01Z","msg":"bd.call","bead":"x"}`)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(nil, nil).WithLog(eventlog.New(path, func(string) {}))
	m, cmd := send(t, m, m.loadLogs()().(logsMsg))
	m, _ = send(t, m, cmd().(digestMsg))
	m.logKindOf = map[string]contract.Kind{"old": contract.Kind("initiative"), "x": contract.Kind("task"), "none": contract.Kind("signal")}
	got := m.tabAt(tabLogs).(logsTab).kindsPresent()
	if len(got) != 2 || got[0] != "initiative" || got[1] != "task" {
		t.Fatalf("kindsPresent = %v, want [initiative task]", got)
	}
}
