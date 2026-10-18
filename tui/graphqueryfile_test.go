package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gqFile(common string) string { return filepath.Join(common, "mugicrew", "graph-filter.json") }

func gqRead(t *testing.T, common string) string {
	t.Helper()
	b, err := os.ReadFile(gqFile(common))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func gqWrite(t *testing.T, common, s string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(gqFile(common)), 0o755)
	if err := os.WriteFile(gqFile(common), []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGraphFilterEnterSavesAndRestores(t *testing.T) {
	dir := t.TempDir()
	m := gfModel(t, dir, gfBeads())
	m = typeText(t, press(t, m, runeKey("/")), "paused")
	if gqRead(t, dir) != "" {
		t.Fatal("typing wrote a file")
	}
	m = press(t, m, enter())
	if got := gqRead(t, dir); got != `{"kind":"","status":"unclosed","words":"paused"}` {
		t.Fatalf("file = %s", got)
	}
	m2 := gfModel(t, dir, gfBeads()) // a restart
	if q := gr(m2).q; q.words != "paused" {
		t.Errorf("restored %+v", q)
	}
	if !strings.Contains(body(m2), "/ paused") {
		t.Errorf("header missing:\n%s", body(m2))
	}
}

func TestGraphFilterRevertWritesNothing(t *testing.T) {
	dir := t.TempDir()
	m := gfModel(t, dir, gfBeads())
	m = typeText(t, press(t, m, runeKey("/")), "paused")
	press(t, m, esc())
	if gqRead(t, dir) != "" {
		t.Error("esc revert wrote a file")
	}
}

func TestGraphFilterPicksAndClearWrite(t *testing.T) {
	dir := t.TempDir()
	m := gfModel(t, dir, gfBeads())
	m = pick(t, m, "s", "any")
	if got := gqRead(t, dir); !strings.Contains(got, `"status":"any"`) {
		t.Errorf("status pick: %s", got)
	}
	m = pick(t, m, "f", "build")
	if got := gqRead(t, dir); !strings.Contains(got, `"kind":"build"`) {
		t.Errorf("kind pick: %s", got)
	}
	m = press(t, m, esc()) // status first
	m = press(t, m, esc()) // then kind
	if got := gqRead(t, dir); got != `{"kind":"","status":"unclosed","words":""}` {
		t.Errorf("clear: %s", got)
	}
}

func TestGraphFilterScopedRevealWritesNothing(t *testing.T) {
	dir := t.TempDir()
	const saved = `{"words":"zzz","status":"unclosed","kind":""}`
	gqWrite(t, dir, saved)
	m := gfModel(t, dir, gfBeads())
	m, _ = send(t, m, revealMsg{"paused"})
	if got := gqRead(t, dir); got != saved {
		t.Errorf("reveal changed the query file: %s", got)
	}
}

func TestGraphFilterLoadValidation(t *testing.T) {
	for name, tc := range map[string]struct {
		file, words, status, kind string
	}{
		"garbage":    {"{nope", "", "unclosed", ""},
		"bad status": {`{"words":"x","status":"weird","kind":"build"}`, "x", "unclosed", "build"},
		"bad kind":   {`{"words":"x","status":"open","kind":"weird"}`, "x", "open", ""},
		"no beads":   {`{"words":"","status":"unclosed","kind":"signal"}`, "", "unclosed", "signal"},
	} {
		dir := t.TempDir()
		gqWrite(t, dir, tc.file)
		q := gr(gfModel(t, dir, nil)).q
		if q.words != tc.words || q.status != tc.status || string(q.kind) != tc.kind {
			t.Errorf("%s: loaded %+v", name, q)
		}
	}
}

func TestGraphFilterSaveFailureShows(t *testing.T) {
	dir := t.TempDir()
	m := gfModel(t, dir, gfBeads())
	os.WriteFile(filepath.Join(dir, "mugicrew"), nil, 0o644) // a file where the dir goes
	m = pick(t, m, "s", "any")
	if !strings.Contains(m.status, "filter won't survive") || gr(m).q.status != "any" {
		t.Errorf("status %q, q %+v", m.status, gr(m).q)
	}
}
