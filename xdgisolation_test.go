package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Go's os.UserConfigDir / os.UserCacheDir ignore XDG on darwin, so a test that
// isolates through XDG alone passes on Linux and fails on macOS. The guard reads
// every _test.go file for such isolation.
var (
	// setsXDG matches t.Setenv or a KEY=VALUE env entry for the two XDG variables
	// that os.User*Dir resolves. XDG_STATE_HOME and XDG_RUNTIME_DIR are not resolved
	// through it, so they are not matched.
	setsXDG = regexp.MustCompile(`"XDG_(CONFIG|CACHE)_HOME(=|")`)
	// setsHome matches the same forms for HOME.
	setsHome = regexp.MustCompile(`"HOME(=|")`)
)

// isolateHelperDir holds the shared isolation helper; its files are exempt.
const isolateHelperDir = "internal/isolate/"

// xdgOnlyIsolation returns "file:line: text" for each XDG_CONFIG_HOME /
// XDG_CACHE_HOME set in a _test.go file that never sets HOME, naming the helper.
func xdgOnlyIsolation(t *testing.T, root fs.FS) []string {
	t.Helper()
	var out []string
	err := fs.WalkDir(root, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") ||
			strings.HasPrefix(path, isolateHelperDir) {
			return err
		}
		b, err := fs.ReadFile(root, path)
		if err != nil {
			return err
		}
		if setsHome.Match(b) {
			return nil
		}
		for i, line := range strings.Split(string(b), "\n") {
			if setsXDG.MatchString(line) {
				out = append(out, path+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line)+
					" sets XDG without HOME, which darwin ignores; isolate through internal/isolate (isolate.Dirs or isolate.Env)")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// No test isolates the config or cache dir through XDG alone.
func TestNoXDGOnlyIsolation(t *testing.T) {
	if got := xdgOnlyIsolation(t, os.DirFS(".")); len(got) > 0 {
		t.Errorf("XDG-only config/cache isolation:\n%s", strings.Join(got, "\n"))
	}
}

// The guard flags XDG-only isolation with its line, and passes HOME-alongside
// and helper-using files.
func TestNoXDGOnlyIsolationFixtures(t *testing.T) {
	dir := t.TempDir()
	for path, body := range map[string]string{
		"a/cache_test.go": `package a
func f(t T) {
	t.Setenv("XDG_CACHE_HOME", d)
}
`,
		"a/env_test.go": `package a
var e = []string{
	"XDG_CONFIG_HOME=" + d,
}
`,
		"a/both_test.go": `package a
func f(t T) {
	t.Setenv("HOME", h)
	t.Setenv("XDG_CONFIG_HOME", d)
}
`,
		"a/helper_test.go": `package a
func f(t T) { cfg, _ := isolate.Dirs(t) }
`,
		"a/state_test.go": `package a
func f(t T) { t.Setenv("XDG_STATE_HOME", d) }
`,
		"a/prod.go": `package a
var x = "XDG_CACHE_HOME"
`,
		"internal/isolate/isolate_test.go": `package isolate
func f(t T) { t.Setenv("XDG_CACHE_HOME", d) }
`,
	} {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := strings.Join(xdgOnlyIsolation(t, os.DirFS(dir)), "\n")
	for _, want := range []string{"a/cache_test.go:3:", "a/env_test.go:3:", "internal/isolate"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	for _, not := range []string{"both_test", "helper_test", "state_test", "prod.go", "internal/isolate/isolate_test"} {
		if strings.Contains(got, not) {
			t.Errorf("%s should not be flagged: %q", not, got)
		}
	}
}
