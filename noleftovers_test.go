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

// oldKindForms are the spellings of the retired Task kind (ADR 0354).
var oldKindForms = regexp.MustCompile("kind:task|KindTask|mugicrew task new|## Tasks|task: <id>")

// oldFormExemptions are the sites that must name an old form to detect or refuse
// it: per file, a line must contain one of the substrings to be exempt. Keep it
// narrow: by site, never a whole package.
var oldFormExemptions = map[string][]string{
	"errandnew.go":            {"mugicrew task is retired", "kind:task -> kind:errand"}, // task verb refusal, relabel report
	"doctor.go":               {"kind:task", "## Tasks", "task: <id>"},                  // relabel row/command, old-form Spec row
	"bd/legacytask.go":        {"kind:task"},                                            // the detector and relabeller
	"zoro/prompts/slicing.md": {"## Tasks"},                                             // the slicing prompt's refusal of the old form
	"tui/loop.go":             {"kind:task"},                                            // dispatch's start refusal predicate
}

// scannedFile reports whether the guard reads path: non-test Go files, prompts and crew docs.
func scannedFile(path string) bool {
	switch {
	case strings.HasSuffix(path, ".go"):
		return !strings.HasSuffix(path, "_test.go")
	case strings.HasSuffix(path, ".md"):
		return strings.HasPrefix(path, "crewdocs/") || strings.Contains(path, "/prompts/")
	}
	return false
}

// leftovers returns "file:line: text" for every old-form mention outside the exemptions.
func leftovers(t *testing.T, root fs.FS) []string {
	t.Helper()
	var out []string
	err := fs.WalkDir(root, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !scannedFile(path) {
			return err
		}
		b, err := fs.ReadFile(root, path)
		if err != nil {
			return err
		}
	lines:
		for i, line := range strings.Split(string(b), "\n") {
			if !oldKindForms.MatchString(line) {
				continue
			}
			for _, ok := range oldFormExemptions[path] {
				if strings.Contains(line, ok) {
					continue lines
				}
			}
			out = append(out, path+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// No non-test Go file, prompt or crew doc names the retired Task kind outside the
// code that must name it to detect or refuse it.
func TestNoTaskKindLeftovers(t *testing.T) {
	if got := leftovers(t, os.DirFS(".")); len(got) > 0 {
		t.Errorf("old Task-kind forms remain:\n%s", strings.Join(got, "\n"))
	}
}

// The guard fails when a stray old form appears in a non-exempt file.
func TestNoTaskKindLeftoversCatchesStray(t *testing.T) {
	dir := t.TempDir()
	for path, body := range map[string]string{
		"loop/stray.go":            "package loop\nconst l = \"kind:task\"\n",
		"brook/prompts/x.md":       "run mugicrew task new\n",
		"crewdocs/y.md":            "## Tasks\n",
		"doctor.go":                "// kind:task is exempt here\n",
		"loop/ok_test.go":          "kind:task\n",
		"docs/adr/0001-history.md": "kind:task\n",
	} {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := strings.Join(leftovers(t, os.DirFS(dir)), "\n")
	for _, want := range []string{"loop/stray.go", "brook/prompts/x.md", "crewdocs/y.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("stray in %s not caught: %q", want, got)
		}
	}
	for _, not := range []string{"doctor.go", "ok_test.go", "0001-history"} {
		if strings.Contains(got, not) {
			t.Errorf("%s should not be flagged: %q", not, got)
		}
	}
}
