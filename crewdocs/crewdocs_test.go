package crewdocs

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// assertMatchesEmbed checks dir holds exactly the embedded files, byte for byte.
func assertMatchesEmbed(t *testing.T, dir string) {
	t.Helper()
	want := map[string]string{}
	fs.WalkDir(docs, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			want[p] = string(render(p, "main"))
		}
		return err
	})
	got := map[string]string{}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		if !d.IsDir() {
			b, _ := os.ReadFile(p)
			rel, _ := filepath.Rel(dir, p)
			got[filepath.ToSlash(rel)] = string(b)
		}
		return nil
	})
	if len(want) == 0 || len(got) != len(want) {
		t.Fatalf("got %d files, want %d", len(got), len(want))
	}
	for p, w := range want {
		if got[p] != w {
			t.Errorf("%s differs from embed", p)
		}
	}
}

// hashDirs lists <root>/mugicrew/crewdocs/ — only the one hash dir may remain.
func hashDirs(t *testing.T, root string) []os.DirEntry {
	t.Helper()
	es, err := os.ReadDir(filepath.Join(root, "mugicrew", "crewdocs"))
	if err != nil {
		t.Fatal(err)
	}
	return es
}

// Spec D6.4: N racing materializers converge on one byte-exact dir, no temp left.
func TestMaterializeConcurrent(t *testing.T) {
	root := t.TempDir()
	const n = 16
	dirs := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() { defer wg.Done(); dirs[i], errs[i] = materialize(root, "main") }()
	}
	wg.Wait()
	for i := range n {
		if errs[i] != nil || dirs[i] != dirs[0] {
			t.Fatalf("goroutine %d: dir=%q err=%v, want %q", i, dirs[i], errs[i], dirs[0])
		}
	}
	if want := filepath.Join(root, "mugicrew", "crewdocs", contentHash("main")); dirs[0] != want {
		t.Fatalf("dir = %q, want %q", dirs[0], want)
	}
	assertMatchesEmbed(t, dirs[0])
	if es := hashDirs(t, root); len(es) != 1 {
		t.Fatalf("leftover entries: %v", es)
	}
}

func TestMaterializeWipedCacheHeals(t *testing.T) {
	root := t.TempDir()
	dir, err := materialize(root, "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	again, err := materialize(root, "main")
	if err != nil || again != dir {
		t.Fatalf("again=%q err=%v, want %q", again, err, dir)
	}
	assertMatchesEmbed(t, again)
}

// Idempotence: a present hash dir is reused as is — a sentinel survives.
func TestMaterializeIdempotent(t *testing.T) {
	root := t.TempDir()
	dir, err := materialize(root, "main")
	if err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(dir, "sentinel")
	if err := os.WriteFile(sentinel, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	again, err := materialize(root, "main")
	if err != nil || again != dir {
		t.Fatalf("again=%q err=%v, want %q", again, err, dir)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatal("second call rewrote the dir")
	}
	if es := hashDirs(t, root); len(es) != 1 {
		t.Fatalf("entries: %v", es)
	}
}

// Spec slice 3: a crew doc's ref-naming git command carries the
// Workspace's mainline; main renders the docs as they read before the token.
func TestMaterializeFillsMainline(t *testing.T) {
	for _, mainline := range []string{"main", "trunk"} {
		dir, err := materialize(t.TempDir(), mainline)
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(dir, "adr-renumber.md"))
		if err != nil {
			t.Fatal(err)
		}
		if want := "git ls-tree --name-only " + mainline + " <its-dir>"; !strings.Contains(string(b), want) {
			t.Errorf("%s: adr-renumber.md lacks %q", mainline, want)
		}
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if b, _ := os.ReadFile(p); err == nil && !d.IsDir() && strings.Contains(string(b), "{{MAINLINE}}") {
				t.Errorf("%s: %s keeps an unfilled {{MAINLINE}}", mainline, p)
			}
			return err
		})
	}
	if contentHash("main") == contentHash("trunk") {
		t.Error("main and trunk renders share a hash dir")
	}
}

// Spec D9: the defog skeleton tends the Chart body through the locked
// verb, never a whole-description bd update.
func TestDefogSessionTendsChartThroughVerb(t *testing.T) {
	b, err := fs.ReadFile(docs, "defog-session.md")
	if err != nil {
		t.Fatal(err)
	}
	if s := string(b); !strings.Contains(s, "mugicrew chart append") || !strings.Contains(s, "mugicrew chart drop") || strings.Contains(s, "via bd update on the") {
		t.Errorf("defog-session.md must tend the Chart through mugicrew chart append|drop:\n%s", s)
	}
}

// Spec D4: task-body.md is the one Task body format producers and
// Brook read — the three markers, the web-UI inference, Ask: only when nothing
// is probeable, re-runnable Checks, and no ordering lines.
func TestTaskBodyFormat(t *testing.T) {
	b, err := fs.ReadFile(docs, "task-body.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"## Steps", "Do:", "Check:", "[secret]", "[elevated]", "[irreversible]", "a Do with no command", "`Ask:` replaces `Check:` only when nothing is probeable", "safe to re-run", "never carries `after:` or `before:`"} {
		if !strings.Contains(s, want) {
			t.Errorf("task-body.md missing %q", want)
		}
	}
}
