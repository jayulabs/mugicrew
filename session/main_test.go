package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	// Throwaway tmux servers get /bin/sh panes: killing one must not hang up a login shell mid-startup, which dumps core.
	os.Setenv("SHELL", "/bin/sh")
	os.Exit(m.Run())
}

// A throwaway -f /dev/null server takes /bin/sh as its default-shell, whatever the human's $SHELL.
func TestThrowawayServerDefaultShell(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	sock := filepath.Join(t.TempDir(), "s.sock")
	t.Cleanup(func() { tmuxCmd(sock, "kill-server").Run() })
	if out, err := tmuxCmd(sock, "-f", "/dev/null", "new-session", "-d", "-s", "mc-sh").CombinedOutput(); err != nil {
		t.Fatalf("tmux new-session: %v: %s", err, out)
	}
	out, err := tmuxCmd(sock, "show-options", "-gv", "default-shell").CombinedOutput()
	if err != nil {
		t.Fatalf("tmux show-options: %v: %s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "/bin/sh" {
		t.Errorf("default-shell = %q, want /bin/sh", got)
	}
}
