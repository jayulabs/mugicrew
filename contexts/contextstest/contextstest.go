// Package contextstest builds a throwaway Workspace holding one nested child repo,
// for tests that drive the repo = context paths against real git.
package contextstest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Label is the context: label of a bead living in the child.
const Label = "context:kid"

// Git runs git in dir with a test identity and returns its trimmed output.
func Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@e.com", "-c", "user.name=t"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// Workspace makes a Workspace repo declaring context kid as repo = "kid", with the
// child repo inside it (ignored, one root commit each), and chdirs there.
func Workspace(t *testing.T) (ws, kid string) {
	t.Helper()
	ws = t.TempDir()
	Git(t, ws, "init", "-q", "-b", "main")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(ws, ".mugicrew"), 0o755))
	must(os.WriteFile(filepath.Join(ws, ".mugicrew", "config.toml"), []byte("[context.kid]\nrepo = \"kid\"\n"), 0o644))
	must(os.WriteFile(filepath.Join(ws, ".gitignore"), []byte("kid/\n"), 0o644))
	Git(t, ws, "add", ".")
	Git(t, ws, "commit", "-q", "-m", "root")
	kid = filepath.Join(ws, "kid")
	must(os.Mkdir(kid, 0o755))
	Git(t, kid, "init", "-q", "-b", "main")
	Git(t, kid, "commit", "-q", "--allow-empty", "-m", "root")
	t.Chdir(ws)
	return ws, kid
}
