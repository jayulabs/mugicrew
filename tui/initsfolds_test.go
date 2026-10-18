package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func initsFoldFile(common string) string { return filepath.Join(common, "mugicrew", "folds.json") }

func initsFoldModel(t *testing.T, common string) Model {
	t.Helper()
	old := commonDir
	commonDir = func() (string, error) { return common, nil }
	t.Cleanup(func() { commonDir = old })
	return initsModel(t, initsFixture(), nil)
}

func writeFolds(t *testing.T, common, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(initsFoldFile(common)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(initsFoldFile(common), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInitsFoldsPersistAndPreserveOtherSections(t *testing.T) {
	dir := t.TempDir()
	writeFolds(t, dir, `{"defog":{"x":true},"initiatives":{"gone":true}}`)
	m := initsFoldModel(t, dir)
	if v := initsBody(m); !strings.Contains(v, "Alpha") || strings.Contains(v, "Delta") {
		t.Fatalf("defaults: Open unfolded, Closed folded:\n%s", v)
	}
	m = space(t, m) // on the Open header: fold it
	m = press(t, m, runeKey("j"))
	b, _ := os.ReadFile(initsFoldFile(dir))
	if got := string(b); got != `{"defog":{"x":true},"initiatives":{"s:open":true}}` {
		t.Errorf("file = %s", got)
	}
	m2 := initsFoldModel(t, dir) // restart
	if v := initsBody(m2); strings.Contains(v, "Alpha") {
		t.Errorf("reload lost the Open fold:\n%s", v)
	}
	m2 = space(t, m2) // back to default deletes the key
	b, _ = os.ReadFile(initsFoldFile(dir))
	if got := string(b); got != `{"defog":{"x":true},"initiatives":{}}` {
		t.Errorf("after default = %s", got)
	}
}

func TestInitsFoldsWriteFailureKeepsFold(t *testing.T) {
	dir := t.TempDir()
	m := initsFoldModel(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "mugicrew"), nil, 0o644); err != nil { // a file where the dir goes
		t.Fatal(err)
	}
	m = space(t, m)
	if !strings.Contains(m.status, "folds won't survive a restart") {
		t.Errorf("status = %q", m.status)
	}
	if strings.Contains(initsBody(m), "Alpha") {
		t.Error("fold lost after a failed write")
	}
}
