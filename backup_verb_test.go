package main

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"jayulabs.org/mugicrew/backup"
)

// backupWorld runs the verb in a temp git repo with backup.SQL scripted and the
// default slog logger writing JSON into events.
type backupWorld struct {
	remote   string // dolt_remotes' url column rows, "" for none
	unpushed string // dolt_log row: "N,date"
	pushErr  error
	// remoteErr and unpushedErr fail the dolt_remotes and dolt_log reads.
	remoteErr, unpushedErr error
	calls                  []string
	events                 bytes.Buffer
}

func (w *backupWorld) run(t *testing.T, holdLock bool, env map[string]string) (int, string, string) {
	t.Helper()
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	t.Chdir(repo)
	for k, v := range env {
		t.Setenv(k, v)
	}
	t.Setenv("DOLT_REMOTE_USER", env["DOLT_REMOTE_USER"])
	if holdLock {
		dir := filepath.Join(repo, ".git", "mugicrew")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(filepath.Join(dir, "backup.lock"), os.O_RDWR|os.O_CREATE, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			t.Fatal(err)
		}
	}
	origSQL, origLog := backup.SQL, slog.Default()
	t.Cleanup(func() { backup.SQL = origSQL; slog.SetDefault(origLog) })
	slog.SetDefault(slog.New(slog.NewJSONHandler(&w.events, nil)))
	backup.SQL = func(_, q string) (string, error) {
		w.calls = append(w.calls, q)
		switch {
		case strings.Contains(q, "dolt_remotes"):
			if w.remoteErr != nil {
				return "", w.remoteErr
			}
			if w.remote == "" {
				return "url\n", nil
			}
			return "url\n" + w.remote + "\n", nil
		case strings.Contains(q, "dolt_log"):
			if w.unpushedErr != nil {
				return "", w.unpushedErr
			}
			return "count(*),min(date)\n" + w.unpushed + "\n", nil
		}
		return "", w.pushErr
	}
	var out, errb bytes.Buffer
	rc := runBackup([]string{"push"}, &out, &errb)
	return rc, out.String() + errb.String(), w.events.String()
}

func TestBackupPushVerb(t *testing.T) {
	cases := []struct {
		name      string
		w         backupWorld
		hold      bool
		env       map[string]string
		rc        int
		msg       string // substring of output
		event     string // "" for none
		wantPush  bool
		wantInCmd string
	}{
		{name: "held lock", w: backupWorld{remote: "file:///x", unpushed: "2,2026-10-28 01:00:00"}, hold: true, rc: 0, msg: "another push"},
		{name: "no remote", w: backupWorld{}, rc: 1, msg: "install.md's Backup section"},
		{name: "nothing unpushed", w: backupWorld{remote: "file:///x", unpushed: "0,"}, rc: 0},
		{name: "success", w: backupWorld{remote: "file:///x", unpushed: "2,2026-10-28 01:00:00"}, rc: 0, event: "backup.pushed", wantPush: true},
		{name: "success as user", w: backupWorld{remote: "file:///x", unpushed: "2,2026-10-28 01:00:00"}, env: map[string]string{"DOLT_REMOTE_USER": "bob"}, rc: 0, event: "backup.pushed", wantPush: true, wantInCmd: "'--user', 'bob'"},
		{name: "remote unreadable", w: backupWorld{remoteErr: errors.New("no server")}, rc: 1, msg: "no server", event: "backup.failed"},
		{name: "unpushed unreadable", w: backupWorld{remote: "file:///x", unpushedErr: errors.New("unknown ref")}, rc: 1, msg: "unknown ref", event: "backup.failed"},
		{name: "failure", w: backupWorld{remote: "file:///x", unpushed: "2,2026-10-28 01:00:00", pushErr: errors.New("permission denied for https://bob:hunter2@host/db")}, rc: 1, msg: "permission denied", event: "backup.failed", wantPush: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rc, out, events := c.w.run(t, c.hold, c.env)
			if rc != c.rc {
				t.Errorf("rc = %d, want %d (%s)", rc, c.rc, out)
			}
			if !strings.Contains(out, c.msg) {
				t.Errorf("output %q lacks %q", out, c.msg)
			}
			if c.event == "" && events != "" {
				t.Errorf("unexpected event: %s", events)
			}
			if c.event != "" && !strings.Contains(events, `"msg":"`+c.event+`"`) {
				t.Errorf("events %q lack %s", events, c.event)
			}
			if c.event != "" && !strings.Contains(events, `"trigger":"cli"`) {
				t.Errorf("events %q lack the cli trigger", events)
			}
			pushed := false
			for _, q := range c.w.calls {
				if strings.Contains(q, "DOLT_PUSH") {
					pushed = true
					if !strings.Contains(q, c.wantInCmd) {
						t.Errorf("push %q lacks %q", q, c.wantInCmd)
					}
				}
			}
			if pushed != c.wantPush {
				t.Errorf("pushed = %v, want %v", pushed, c.wantPush)
			}
			if strings.Contains(out+events, "hunter2") {
				t.Errorf("a credential leaked: %s %s", out, events)
			}
		})
	}
}

func TestBackupUsage(t *testing.T) {
	var out, errb bytes.Buffer
	if rc := runBackup([]string{"pull"}, &out, &errb); rc != 1 || !strings.Contains(errb.String(), "usage") {
		t.Errorf("rc %d: %s", rc, errb.String())
	}
}
