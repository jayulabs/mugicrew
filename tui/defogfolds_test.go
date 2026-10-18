package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// defogFoldModel is a Defog model whose fold file lives under common (a temp dir),
// opened on the Defog tab with beads loaded.
func defogFoldModel(t *testing.T, common string, extra ...bd.Bead) Model {
	t.Helper()
	old := commonDir
	commonDir = func() (string, error) { return common, nil }
	t.Cleanup(func() { commonDir = old })
	beads, sess := defogFixture()
	m := sized(t, 100, 40)
	m, _ = send(t, m, loadedMsg{beads: append(beads, extra...), live: map[string]bool{}, sessions: sess})
	return m
}

func defogFile(common string) string { return filepath.Join(common, "mugicrew", "folds.json") }

func seedDefogFolds(t *testing.T, common, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(defogFile(common)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defogFile(common), []byte(`{"defog":`+body+`}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readDefogFolds(t *testing.T, common string) string {
	return readSection(t, common, "defog")
}

// toHead moves past the Actionable section to the first Initiative header.
func toHead(t *testing.T, m Model) Model {
	t.Helper()
	for range 40 {
		if r, ok := m.tabAt(tabDefog).(defogTab).selRow(); ok && r.kind == dHead {
			return m
		}
		m = press(t, m, runeKey("j"))
	}
	t.Fatal("no Initiative header")
	return m
}

// toSummary opens the first Initiative and its first Chart, then moves to a
// '▸ N closed' row of the Defog tab.
func toSummary(t *testing.T, m Model) (Model, string) {
	t.Helper()
	m = press(t, m, runeKey("2"))
	m = press(t, toHead(t, m), runeKey(" "))
	m = press(t, press(t, m, runeKey("j")), runeKey(" "))
	for range 40 {
		if r, ok := m.tabAt(tabDefog).(defogTab).selRow(); ok && r.kind == dSum {
			return m, r.key
		}
		m = press(t, m, runeKey("j"))
	}
	t.Fatal("no closed row")
	return m, ""
}

func TestDefogFoldWritesOnlyNonDefaultKeys(t *testing.T) {
	dir := t.TempDir()
	m := defogFoldModel(t, dir)
	m = press(t, m, runeKey("2"))
	m = toHead(t, m)
	m = press(t, m, runeKey(" "))
	if got := readDefogFolds(t, dir); got != `{"i:ini":true}` {
		t.Errorf("after fold = %s", got)
	}
	m = press(t, m, runeKey(" "))
	if got := readDefogFolds(t, dir); got != `{}` {
		t.Errorf("after unfold = %s", got)
	}
	m, k := toSummary(t, m) // everything folded by default: opening each is the flip
	m = press(t, m, runeKey(" "))
	if got := readDefogFolds(t, dir); got != `{"c":true,"`+k+`":true,"i:ini":true}` {
		t.Errorf("after opening closed row = %s", got)
	}
}

func TestDefogFoldsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	m := defogFoldModel(t, dir)
	m = press(t, m, runeKey("2"))
	m = toHead(t, m)
	press(t, m, runeKey(" "))
	m2 := press(t, defogFoldModel(t, dir), runeKey("2"))
	if v := ansi.Strip(m2.View()); !strings.Contains(v, "CHART") {
		t.Errorf("restart did not restore the open Initiative:\n%s", v)
	}
}

func TestDefogFoldPrunesClosedAndGoneButNotNoInitiative(t *testing.T) {
	dir := t.TempDir()
	seedDefogFolds(t, dir, `{"i:gone":true,"i:old":true,"oldc/closed":true,"goneC":true,"i:":true,"c/closed":true}`)
	m := defogFoldModel(t, dir,
		dbead("old", contract.KindInitiative, "closed", "", 1),
		dbead("oldc", contract.KindChart, "closed", "", 1))
	m = press(t, m, runeKey("2"))
	m = toHead(t, m)
	press(t, m, runeKey(" ")) // ini
	if got := readDefogFolds(t, dir); got != `{"c/closed":true,"i:":true,"i:ini":true}` {
		t.Errorf("after save = %s", got)
	}
}

func TestDefogPrunedKeyStillFoldsThisSession(t *testing.T) {
	dir := t.TempDir()
	m := defogFoldModel(t, dir, dbead("old", contract.KindInitiative, "closed", "", 1), dbead("oc", contract.KindChart, "open", "old", 1))
	m = press(t, m, runeKey("2"))
	m = toHead(t, m)
	for range 40 { // down to the closed Initiative's header
		if r, _ := m.tabAt(tabDefog).(defogTab).selRow(); r.key == "i:old" {
			break
		}
		m = press(t, m, runeKey("j"))
	}
	m = press(t, m, runeKey(" "))
	if got := readDefogFolds(t, dir); got != `{}` {
		t.Errorf("file = %s, want closed key pruned", got)
	}
	if !m.tabAt(tabDefog).(defogTab).flip["i:old"] {
		t.Error("fold dropped from memory")
	}
}

func TestDefogFoldGarbageLoadsAsDefaultsAndIsOverwritten(t *testing.T) {
	dir := t.TempDir()
	seedDefogFolds(t, dir, "not json{")
	m := toHead(t, press(t, defogFoldModel(t, dir), runeKey("2")))
	if v := ansi.Strip(m.View()); strings.Contains(v, "▾ ○·  CHART") {
		t.Errorf("defaults not applied (folded):\n%s", v)
	}
	press(t, m, runeKey(" "))
	if got := readDefogFolds(t, dir); got != `{"i:ini":true}` {
		t.Errorf("after toggle = %s", got)
	}
}

func TestDefogFoldSaveFailureSaysSoAndStillFolds(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mugicrew"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	m := toHead(t, press(t, defogFoldModel(t, dir), runeKey("2")))
	m = press(t, m, runeKey(" "))
	if !strings.Contains(m.status, "restart") {
		t.Errorf("status = %q", m.status)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "CHART") {
		t.Errorf("fold not applied in memory:\n%s", v)
	}
}

func TestDefogFoldSaveNamesAnUnresolvedCommonDir(t *testing.T) {
	old := commonDir
	commonDir = func() (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { commonDir = old })
	beads, sess := defogFixture()
	m := sized(t, 100, 40)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: sess})
	m = press(t, m, runeKey("2"))
	m = toHead(t, m)
	m = press(t, m, runeKey(" "))
	if !strings.Contains(m.status, "common dir unresolved") {
		t.Errorf("status = %q", m.status)
	}
}
