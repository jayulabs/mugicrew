package tui

import (
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/eventlog"
)

func backupEv(msg string, at int64, errText string) eventlog.Event {
	raw := map[string]any{}
	ev := eventlog.Event{Msg: msg, Time: time.Unix(at, 0), Raw: raw}
	if errText != "" {
		raw["err"] = errText
		ev.Short = errText
	}
	return ev
}

func backupMarked(m Model) bool { return strings.Contains(header(m), "backup failed") }

func foldBackup(t *testing.T, m Model, evs ...eventlog.Event) Model {
	t.Helper()
	m, _ = send(t, m, logsMsg{path: "p", changed: true, events: evs, digest: evs})
	return m
}

// The latest backup.* event decides the status bar's mark (D5): failed shows it, a later
// pushed clears it, no backup events show nothing, and a rebuild from the same log agrees.
func TestBackupFailedMark(t *testing.T) {
	failed, pushed := backupEv("backup.failed", 10, "no common ancestor"), backupEv("backup.pushed", 20, "")
	if m := foldBackup(t, sized(t, 160, 30), backupEv("session.ended", 5, "")); backupMarked(m) {
		t.Errorf("no backup events: header %q is marked", header(m))
	}
	m := foldBackup(t, sized(t, 160, 30), pushed, failed)
	if !backupMarked(m) {
		t.Errorf("latest backup.failed: header %q lacks the mark", header(m))
	}
	if again := foldBackup(t, sized(t, 160, 30), pushed, failed); !backupMarked(again) {
		t.Errorf("rebuilt model: header %q lacks the mark", header(again))
	}
	cleared := foldBackup(t, sized(t, 160, 30), failed, pushed)
	if backupMarked(cleared) {
		t.Errorf("later backup.pushed: header %q still marked", header(cleared))
	}
	if !inDigest(failed) || !inDigest(pushed) {
		t.Error("backup.* events must be in the digest")
	}
	if got := cleared.backupFailed(); got {
		t.Errorf("backupFailed after a push = %v", got)
	}
}
