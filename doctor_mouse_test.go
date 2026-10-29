package main

import (
	"errors"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/loop"
)

func TestDoctorTmuxMouse(t *testing.T) {
	const sock = "/repo/.git/mugicrew/tmux.sock"
	for _, c := range []struct {
		name    string
		on      bool
		env     string
		inside  bool
		read    string
		readErr error
		want    []string // substrings of the row, none: no row
	}{
		{name: "mouse mode off", on: false, env: "/tmp/tmux-1/default,1,0"},
		{name: "no outer tmux", on: true, want: []string{"ok  tmux mouse: no outer tmux"}},
		{name: "private socket", on: true, env: sock + ",1,0", inside: true,
			want: []string{"skip  tmux mouse: inside mugicrew's tmux server; the outer tmux cannot be read"}},
		{name: "outer mouse on", on: true, env: "/tmp/tmux-1/default,1,0", read: "on\n", want: []string{"ok  tmux mouse"}},
		{name: "outer mouse off", on: true, env: "/tmp/tmux-1/default,1,0", read: "off\n",
			want: []string{"warn  tmux mouse", "the tmux mugicrew runs inside has mouse on", "set -g mouse on", "tmux source-file ~/.tmux.conf"}},
		{name: "read error", on: true, env: "/tmp/tmux-1/default,1,0", readErr: errors.New("no server"),
			want: []string{"warn  tmux mouse: unreadable", "tmux show -gv mouse"}},
		{name: "unreadable output", on: true, env: "/tmp/tmux-1/default,1,0", read: "garbage",
			want: []string{"warn  tmux mouse: unreadable", "tmux show -gv mouse"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			doctorWS(t)
			oldM, oldE, oldI, oldR := doctorMouse, doctorEnv, doctorInsidePrivate, doctorTmuxMouse
			t.Cleanup(func() { doctorMouse, doctorEnv, doctorInsidePrivate, doctorTmuxMouse = oldM, oldE, oldI, oldR })
			doctorMouse = func(string) loop.Mouse { return loop.Mouse{On: c.on} }
			doctorEnv = func(k string) string {
				if k == "TMUX" {
					return c.env
				}
				return ""
			}
			doctorInsidePrivate = func(string) (bool, error) { return c.inside, nil }
			doctorTmuxMouse = func() (string, error) { return c.read, c.readErr }

			out, code := doctorRun(t)
			if code != 0 {
				t.Errorf("exit code %d, want 0 (the check never changes it): %s", code, out)
			}
			if len(c.want) == 0 {
				if strings.Contains(out, "tmux mouse") {
					t.Errorf("want no mouse row: %s", out)
				}
				return
			}
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("want %q in %s", w, out)
				}
			}
			if i, j := strings.Index(out, "tmux mouse"), strings.Index(out, "binary"); i < 0 || j < 0 || i > j {
				t.Errorf("mouse row should sit right after the tools rows, before binary: %s", out)
			}
		})
	}
}
