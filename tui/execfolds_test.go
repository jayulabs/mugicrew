package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// execFoldModel is an Execute model whose fold file lives under common (a temp dir),
// on the Execute tab with beads loaded.
func execFoldModel(t *testing.T, common string, extra ...bd.Bead) Model {
	t.Helper()
	stubCommonDir(t, common)
	m, _ := execModelFresh(t, append(execFixture(), extra...))
	return press(t, m, runeKey("3"))
}

func execFile(common string) string { return filepath.Join(common, "mugicrew", "folds.json") }

func seedExecFolds(t *testing.T, common, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(execFile(common)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(execFile(common), []byte(`{"execute":`+body+`}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readExecFolds(t *testing.T, common string) string {
	return readSection(t, common, "execute")
}

func TestExecFoldWritesOnlyNonDefaultKeysAndSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	m := execFoldModel(t, dir)
	m = press(t, execAt(t, m, "s:act"), runeKey(" ")) // fold Actionable
	if got := readExecFolds(t, dir); got != `{"s:act":true}` {
		t.Errorf("after fold = %s", got)
	}
	m = press(t, execAt(t, m, "i:i1"), runeKey(" ")) // open an Initiative: folded by default
	m = press(t, execAt(t, m, "cpA"), runeKey(" "))  // and its Checkpoint
	if got := readExecFolds(t, dir); got != `{"cpA":true,"i:i1":true,"s:act":true}` {
		t.Errorf("after opening cpA = %s", got)
	}
	press(t, execAt(t, m, "s:act"), runeKey(" ")) // back to the default: key deleted
	if got := readExecFolds(t, dir); got != `{"cpA":true,"i:i1":true}` {
		t.Errorf("after unfold = %s", got)
	}
	f := execFoldModel(t, dir).tabAt(tabExecute).(execTab).flip
	if len(f) != 2 || !f["cpA"] || !f["i:i1"] {
		t.Errorf("restarted flip = %v", f)
	}
}

func TestExecFoldPrunesClosedAndGoneButNotActionableOrNoInitiative(t *testing.T) {
	dir := t.TempDir()
	seedExecFolds(t, dir, `{"i:gone":true,"i:old":true,"oldcp/closed":true,"goneCP":true,"i:":true,"s:act":true,"cpA/closed":true}`)
	m := execFoldModel(t, dir,
		dbead("old", contract.KindInitiative, "closed", "", 1),
		dbead("oldcp", contract.KindCheckpoint, "closed", "", 1, isContainer))
	press(t, execAt(t, m, "i:i1"), runeKey(" "))
	if got := readExecFolds(t, dir); got != `{"cpA/closed":true,"i:":true,"i:i1":true,"s:act":true}` {
		t.Errorf("after save = %s", got)
	}
}

func TestExecPrunedKeyStillFoldsThisSession(t *testing.T) {
	dir := t.TempDir()
	m := execFoldModel(t, dir, dbead("ic", contract.KindInitiative, "closed", "", 1),
		dbead("cz", contract.KindCheckpoint, "open", "ic", 1, isContainer))
	m = press(t, execAt(t, m, "i:ic"), runeKey(" "))
	if got := readExecFolds(t, dir); got != `{}` {
		t.Errorf("file = %s, want closed key pruned", got)
	}
	if !m.tabAt(tabExecute).(execTab).flip["i:ic"] {
		t.Error("fold dropped from memory")
	}
}

func TestExecFoldGarbageLoadsAsDefaultsAndIsOverwritten(t *testing.T) {
	dir := t.TempDir()
	seedExecFolds(t, dir, "not json{")
	m := execFoldModel(t, dir)
	press(t, execAt(t, m, "s:act"), runeKey(" "))
	if got := readExecFolds(t, dir); got != `{"s:act":true}` {
		t.Errorf("after toggle = %s", got)
	}
}

func TestExecFoldSaveFailureSaysSoAndStillFolds(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mugicrew"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	m := press(t, execAt(t, execFoldModel(t, dir), "s:act"), runeKey(" "))
	if !strings.Contains(m.status, "restart") {
		t.Errorf("status = %q", m.status)
	}
	if !m.tabAt(tabExecute).(execTab).flip["s:act"] {
		t.Error("fold not applied in memory")
	}
}

func TestExecFoldSaveNamesAnUnresolvedCommonDir(t *testing.T) {
	old := commonDir
	commonDir = func() (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { commonDir = old })
	m, _ := execModelHere(t, execFixture())
	m = press(t, execAt(t, m, "s:act"), runeKey(" "))
	if !strings.Contains(m.status, "common dir unresolved") {
		t.Errorf("status = %q", m.status)
	}
}
