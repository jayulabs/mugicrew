package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/session"
)

// foldModel is an Ideate model whose fold file lives under common (a temp dir).
func foldModel(t *testing.T, common string, beads []bd.Bead) Model {
	t.Helper()
	old := commonDir
	commonDir = func() (string, error) { return common, nil }
	t.Cleanup(func() { commonDir = old })
	return ideaModelFresh(t, 100, 24, beads, map[string]session.Info{})
}

func foldFile(common string) string { return filepath.Join(common, "mugicrew", "folds.json") }

// seedFolds writes a fold file under common, failing the test if the fixture can't be made.
func seedFolds(t *testing.T, common, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(foldFile(common)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foldFile(common), []byte(`{"ideate":`+body+`}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFolds(t *testing.T, common string) string { return readSection(t, common, "ideate") }

// readSection is one tab's section of folds.json as JSON.
func readSection(t *testing.T, common, tab string) string {
	t.Helper()
	b, err := os.ReadFile(foldFile(common))
	if err != nil {
		t.Fatal(err)
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(b, &all); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(all[tab]))
}

// foldAct folds the Actionable section, so view text shows the tree alone.
func foldAct(t *testing.T, m Model) Model {
	t.Helper()
	return press(t, ideaAt(t, m, actKey), runeKey(" "))
}

func foldBeads() []bd.Bead {
	return []bd.Bead{ini("ini", "open", "Ship v2", 1), sig("a", "open", 1, "ini"), sig("loose", "open", 2, "")}
}

func TestIdeateFoldWritesOnlyNonDefaultKeys(t *testing.T) {
	dir := t.TempDir()
	m := foldModel(t, dir, foldBeads())
	m = ideaAt(t, m, "i:ini")
	m = press(t, m, runeKey(" "))
	if got := readFolds(t, dir); got != `{"i:ini":true}` {
		t.Errorf("after fold = %s", got)
	}
	m = press(t, m, runeKey(" "))
	if got := readFolds(t, dir); got != `{}` {
		t.Errorf("after unfold = %s", got)
	}
}

func TestIdeateFoldPrunesClosedInitiative(t *testing.T) {
	dir := t.TempDir()
	seedFolds(t, dir, `{"i:gone":true,"i:old":true}`)
	beads := append(foldBeads(), ini("old", "closed", "Old", 1))
	m := foldModel(t, dir, beads)
	m = ideaAt(t, m, "i:ini")
	press(t, m, runeKey(" "))
	if got := readFolds(t, dir); got != `{"i:ini":true}` {
		t.Errorf("after save = %s", got)
	}
}

func TestIdeateFoldGarbageLoadsAsDefaultsAndIsOverwritten(t *testing.T) {
	dir := t.TempDir()
	seedFolds(t, dir, "not json{")
	m := foldModel(t, dir, foldBeads())
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Ship v2 · ini (1) · 1 open") {
		t.Errorf("defaults not applied (folded, with trailer):\n%s", v)
	}
	m = ideaAt(t, m, "i:ini")
	press(t, m, runeKey(" "))
	if got := readFolds(t, dir); got != `{"i:ini":true}` {
		t.Errorf("after toggle = %s", got)
	}
}

func TestIdeateFoldSaveFailureSaysSoAndStillFolds(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "mugicrew") // a file where the directory belongs
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	m := foldAct(t, foldModel(t, dir, foldBeads()))
	m = ideaAt(t, m, "i:ini")
	m = press(t, m, runeKey(" "))
	if !strings.Contains(m.status, "restart") {
		t.Errorf("status = %q", m.status)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "sig a") {
		t.Errorf("fold not applied in memory:\n%s", v)
	}
}

func TestIdeateFoldsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	m := foldAct(t, foldModel(t, dir, foldBeads()))
	m = ideaAt(t, m, "i:ini")
	press(t, m, runeKey(" "))
	m2 := foldModel(t, dir, foldBeads())
	if v := ansi.Strip(m2.View()); !strings.Contains(v, "sig a") || strings.Contains(v, "sig loose") {
		t.Errorf("restart did not restore the open Initiative:\n%s", v)
	}
}

func TestIdeateSpaceFoldsClosedInitiativeButFileDropsItsKey(t *testing.T) {
	dir := t.TempDir()
	m := foldModel(t, dir, []bd.Bead{ini("z", "closed", "Zed", 1), sig("s", "open", 1, "z")})
	m = ideaAt(t, m, "i:z") // Zed (closed) header
	m = press(t, m, runeKey(" "))
	for _, r := range m.tabAt(tabIdeate).(ideaTab).rows() {
		if r.key == "s" {
			return // opened: the closed Initiative's Signal lists
		}
	}
	t.Errorf("closed Initiative did not open")
	if got := readFolds(t, dir); got != `{}` {
		t.Errorf("file = %s, want closed key pruned", got)
	}
}

func TestIdeateFoldSaveNamesAnUnresolvedCommonDir(t *testing.T) {
	old := commonDir
	commonDir = func() (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { commonDir = old })
	m := ideaModel(t, 100, 24, foldBeads(), map[string]session.Info{})
	m = ideaAt(t, m, "i:ini")
	m = press(t, m, runeKey(" "))
	if !strings.Contains(m.status, "common dir unresolved") {
		t.Errorf("status = %q", m.status)
	}
}

// Any fold file name under <common>/mugicrew/ round-trips and leaves no temp file behind.
func TestFoldFileNameIsAParameter(t *testing.T) {
	common := t.TempDir()
	old := commonDir
	commonDir = func() (string, error) { return common, nil }
	t.Cleanup(func() { commonDir = old })

	path := foldsPath(foldsFile)
	if want := filepath.Join(common, "mugicrew", "folds.json"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	if err := saveFolds(path, "defog", map[string]bool{"i:a": true}); err != nil {
		t.Fatal(err)
	}
	if got := loadFolds(path, "defog"); !got["i:a"] || len(got) != 1 {
		t.Fatalf("loaded %v", got)
	}
	if ents, _ := os.ReadDir(filepath.Dir(path)); len(ents) != 1 {
		t.Fatalf("stray files: %v", ents)
	}
}

// Three tabs saving interleaved keep each other's sections.
func TestFoldsTabsKeepEachOthersSections(t *testing.T) {
	common := t.TempDir()
	stubCommonDir(t, common)
	path := foldsPath(foldsFile)
	for _, s := range []struct{ tab, key string }{{"ideate", "i:a"}, {"defog", "b"}, {"execute", "s:act"}, {"ideate", "i:c"}} {
		flip := loadFolds(path, s.tab)
		flip[s.key] = true
		if err := saveFolds(path, s.tab, flip); err != nil {
			t.Fatal(err)
		}
	}
	for tab, want := range map[string]string{"ideate": `{"i:a":true,"i:c":true}`, "defog": `{"b":true}`, "execute": `{"s:act":true}`} {
		if got := readSection(t, common, tab); got != want {
			t.Errorf("%s = %s, want %s", tab, got, want)
		}
	}
}

// The Backlog key is never pruned, though no bead has that id.
func TestIdeateFoldKeepsBacklogKey(t *testing.T) {
	dir := t.TempDir()
	m := foldModel(t, dir, append(foldBeads(), sig("bk", "open", 4, "")))
	for range 10 { // down to the Backlog header
		if m.tabAt(tabIdeate).(ideaTab).cursor().key == backlogKey {
			break
		}
		m = press(t, m, runeKey("j"))
	}
	m = press(t, m, runeKey(" "))
	if got := readFolds(t, dir); got != `{"\u0000backlog":true}` {
		t.Errorf("file = %s", got)
	}
}

// The per-tab files of earlier builds are neither read nor touched.
func TestFoldsIgnoreOldPerTabFiles(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "mugicrew", "ideate-folds.json")
	os.MkdirAll(filepath.Dir(old), 0o755)
	os.WriteFile(old, []byte(`{"i:ini":true}`), 0o644)
	m := foldModel(t, dir, foldBeads())
	if m.tabAt(tabIdeate).(ideaTab).flip["i:ini"] {
		t.Error("old ideate-folds.json was read")
	}
	if b, _ := os.ReadFile(old); string(b) != `{"i:ini":true}` {
		t.Errorf("old file changed: %s", b)
	}
}
