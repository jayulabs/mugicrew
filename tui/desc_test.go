package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
)

func TestTitleSlug(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Fix the bug", "fix-the-bug"},
		{"  --TUI: retry-aware (pause) count!!  ", "tui-retry-aware-pause-count"},
		{"café über naïve", "caf-ber-na-ve"},
		{"日本語", ""},
		{"", ""},
		{"Description temp file named after the bead's id and title", "description-temp-file-named-after-the-be"},
		{strings.Repeat("a", 39) + " b", strings.Repeat("a", 39)}, // cut lands on the hyphen
	} {
		if got := titleSlug(c.in); got != c.want {
			t.Errorf("titleSlug(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestEditDescriptionTempFile: the $EDITOR temp file is named after the bead
// (mugicrew-<id>-<slug>-*.md, or mugicrew-<id>-*.md for an empty slug) and holds
// the description byte for byte.
func TestEditDescriptionTempFile(t *testing.T) {
	t.Setenv("EDITOR", "true")
	var path string
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		path = args[len(args)-1]
		return exec.Command("true")
	}
	t.Cleanup(func() { execCommand = orig })

	for _, c := range []struct{ title, pattern string }{
		{"Fix the bug!", "mugicrew-ws-ab.1-fix-the-bug-*.md"},
		{"日本語", "mugicrew-ws-ab.1-*.md"},
	} {
		desc := "line one\n\n  ünïcode body\n"
		var m Model
		if m.editDescription(&bd.Bead{ID: "ws-ab.1", Title: c.title, Description: desc}) == nil {
			t.Fatalf("%q: no cmd, status %q", c.title, m.status)
		}
		t.Cleanup(func() { os.Remove(path) })
		if ok, _ := filepath.Match(c.pattern, filepath.Base(path)); !ok {
			t.Errorf("%q: temp file %q, want %s", c.title, filepath.Base(path), c.pattern)
		}
		if strings.Count(filepath.Base(path), "-") != strings.Count(c.pattern, "-") {
			t.Errorf("%q: temp file %q has extra parts, want %s", c.title, filepath.Base(path), c.pattern)
		}
		if body, err := os.ReadFile(path); err != nil || string(body) != desc {
			t.Errorf("%q: body %q (%v), want %q", c.title, body, err, desc)
		}
	}
}
