package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCredentialFill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	const url = "https://gl.example.com/ns/p.git"

	if err := CredentialFill("git@gl.example.com:ns/p.git"); !errors.Is(err, ErrCredentialNA) {
		t.Errorf("scp: %v", err)
	}
	if err := CredentialFill("ssh://git@gl.example.com/ns/p.git"); !errors.Is(err, ErrCredentialNA) {
		t.Errorf("ssh: %v", err)
	}

	// An askpass (GIT_ASKPASS, or SSH_ASKPASS via git's fallthrough) must never run.
	marker := filepath.Join(home, "askpass-ran")
	askpass := filepath.Join(home, "askpass.sh")
	if err := os.WriteFile(askpass, []byte("#!/bin/sh\ntouch "+marker+"\necho pw\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_ASKPASS", askpass)
	t.Setenv("SSH_ASKPASS", askpass)

	start := time.Now()
	if err := CredentialFill(url); err == nil || errors.Is(err, ErrCredentialNA) || time.Since(start) > 10*time.Second {
		t.Errorf("no helper: want fast failure, got %v after %v", err, time.Since(start))
	}

	if _, err := os.Stat(marker); err == nil {
		t.Error("no helper: askpass ran")
	}

	helper := filepath.Join(home, "helper.sh")
	write := func(path, body string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	cfg := func() { write(filepath.Join(home, "gitconfig"), "[credential]\n\thelper = "+helper+"\n", 0o644) }
	write(helper, "#!/bin/sh\necho username=u\necho password=s3cret-pw\n", 0o755)
	cfg()
	if err := CredentialFill(url); err != nil {
		t.Fatal(err)
	}

	// A failing helper still emits the password on stdout; the error must not carry it.
	write(helper, "#!/bin/sh\necho username=u\necho password=s3cret-pw\nexit 1\n", 0o755)
	if err := CredentialFill(url); err != nil && strings.Contains(err.Error(), "s3cret-pw") {
		t.Errorf("error leaks the password: %v", err)
	}

	// CredentialFillEnv runs from dir with the env appended: the helper sees both.
	dir := t.TempDir()
	write(helper, "#!/bin/sh\necho username=u\necho password=s\necho \"$MARK $(pwd -P)\" > "+filepath.Join(home, "seen")+"\n", 0o755)
	if err := CredentialFillEnv(url, dir, []string{"MARK=x"}); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(dir)
	if seen, _ := os.ReadFile(filepath.Join(home, "seen")); strings.TrimSpace(string(seen)) != "x "+real {
		t.Errorf("helper saw %q, want env x and cwd %s", seen, real)
	}
}
