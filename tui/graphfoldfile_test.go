package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// gfModel is a Graph-tab Model over beads whose fold file lives under common.
func gfModel(t *testing.T, common string, beads []bd.Bead) Model {
	t.Helper()
	old := commonDir
	commonDir = func() (string, error) { return common, nil }
	t.Cleanup(func() { commonDir = old })
	m := sized(t, 120, 30)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: map[string]session.Info{}})
	m, _ = send(t, m, runeKey("4"))
	return m
}

func gfFile(common string) string { return filepath.Join(common, "mugicrew", "graph-folds.json") }

func gfRead(t *testing.T, common string) string {
	t.Helper()
	b, err := os.ReadFile(gfFile(common))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

// gfBeads is stubBeads plus an open Build under the closed root, so the baseline
// filter still shows that group.
func gfBeads() []bd.Bead {
	return append(stubBeads(), bd.Bead{ID: "oldopen", Kind: contract.KindBuild, Status: "open", Title: "Still open", Parent: "old", Created: epoch})
}

func space(t *testing.T, m Model) Model { return press(t, m, tea.KeyMsg{Type: tea.KeySpace}) }

func TestGraphFoldsOpenClosedRootPersists(t *testing.T) {
	dir := t.TempDir()
	m := gfModel(t, dir, gfBeads())
	if !gr(m).folds()["old"] || !gr(m).folds()[""] {
		t.Fatal("a closed root and Unparented fold with no file")
	}
	if _, err := os.Stat(gfFile(dir)); err == nil {
		t.Fatal("loading wrote a file")
	}
	m = selectBead(t, m, "old")
	m = space(t, m)
	if got := gfRead(t, dir); got != `{"old":false}` {
		t.Errorf("after opening = %s", got)
	}
	m = selectBead(t, m, "ini")
	m = space(t, m) // an unrelated save: the closed root's key stays
	if got := gfRead(t, dir); got != `{"ini":true,"old":false}` {
		t.Errorf("after a later save = %s", got)
	}
	m2 := gfModel(t, dir, gfBeads()) // restart
	if gr(m2).folds()["old"] || !gr(m2).folds()["ini"] {
		t.Errorf("restart lost the folds: %v", gr(m2).folds())
	}
}

func TestGraphFoldsPruneGoneBeadsNotEmptyKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(gfFile(dir)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gfFile(dir), []byte(`{"":false,"vanished":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := gfModel(t, dir, gfBeads())
	m = selectBead(t, m, "ini")
	space(t, m)
	if got := gfRead(t, dir); got != `{"":false,"ini":true}` {
		t.Errorf("after save = %s", got)
	}
}

func TestGraphFoldsLiveDefaultFoldsRootClosingLater(t *testing.T) {
	dir := t.TempDir()
	beads := stubBeads()
	m := gfModel(t, dir, beads)
	if gr(m).folds()["ch"] {
		t.Fatal("ini starts open")
	}
	closed := stubBeads()
	for i := range closed {
		if closed[i].ID == "ini" {
			closed[i].Status = "closed"
		}
	}
	m, _ = send(t, m, loadedMsg{beads: closed, live: map[string]bool{}, sessions: map[string]session.Info{}})
	if !gr(m).folds()["ini"] {
		t.Error("a root that closes folds on the next rebuild")
	}
}

func TestGraphFoldsWhileFilteringAreNotWritten(t *testing.T) {
	dir := t.TempDir()
	m := gfModel(t, dir, gfBeads())
	m = press(t, m, runeKey("/"))
	for _, r := range "build" {
		m = press(t, m, runeKey(string(r)))
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = selectBead(t, m, "ini")
	space(t, m)
	if _, err := os.Stat(gfFile(dir)); err == nil {
		t.Errorf("a fold under a filter wrote %s", gfRead(t, dir))
	}
}

func TestGraphRevealPersistsOpenedGroup(t *testing.T) {
	dir := t.TempDir()
	m := gfModel(t, dir, gfBeads())
	m = selectBead(t, m, "ini")
	m = space(t, m) // fold ini
	m, _ = send(t, m, revealMsg{"paused"})
	if got := gfRead(t, dir); got != `{}` {
		t.Errorf("after reveal = %s", got)
	}
	m, _ = send(t, m, revealMsg{"oldopen"})
	if got := gfRead(t, dir); got != `{"old":false}` {
		t.Errorf("after revealing under a closed root = %s", got)
	}
}

func TestGraphScopedRevealLeavesFoldsUntouched(t *testing.T) {
	dir := t.TempDir()
	m := gfModel(t, dir, gfBeads())
	m = selectBead(t, m, "ini")
	m = space(t, m) // fold ini
	before := gfRead(t, dir)
	gqWrite(t, dir, `{"words":"zzz","status":"unclosed","kind":""}`)
	m = gfModel(t, dir, gfBeads())
	m, _ = send(t, m, revealMsg{"oldopen"})
	if got := gfRead(t, dir); got != before {
		t.Errorf("scoped reveal changed folds.json: %s, was %s", got, before)
	}
}

func TestGraphFoldsGarbageReplacedAndSaveFailureReported(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(gfFile(dir)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gfFile(dir), []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := gfModel(t, dir, gfBeads())
	if !gr(m).folds()["old"] {
		t.Fatal("garbage loads as defaults")
	}
	m = selectBead(t, m, "ini")
	m = space(t, m)
	if got := gfRead(t, dir); got != `{"ini":true}` {
		t.Errorf("garbage not replaced: %s", got)
	}
	old := commonDir
	commonDir = func() (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { commonDir = old })
	m = sized(t, 120, 30)
	m, _ = send(t, m, loadedMsg{beads: stubBeads(), live: map[string]bool{}, sessions: map[string]session.Info{}})
	m, _ = send(t, m, runeKey("4"))
	m = selectBead(t, m, "ini")
	m = space(t, m)
	if !strings.Contains(m.status, "folds won't survive a restart") || !gr(m).folds()["ini"] {
		t.Errorf("save failure: status %q folds %v", m.status, gr(m).folds())
	}
}

func TestGraphFoldedRootStaysFoldedOnceClosed(t *testing.T) {
	dir := t.TempDir()
	m := gfModel(t, dir, stubBeads())
	m = selectBead(t, m, "ini")
	m = space(t, m) // fold the open root
	if got := gfRead(t, dir); got != `{"ini":true}` {
		t.Fatalf("after fold = %s", got)
	}
	closed := stubBeads()
	for i := range closed {
		if closed[i].ID == "ini" {
			closed[i].Status = "closed"
		}
	}
	m, _ = send(t, m, loadedMsg{beads: closed, live: map[string]bool{}, sessions: map[string]session.Info{}})
	if !gr(m).folds()["ini"] {
		t.Error("a folded root popped open once it closed")
	}
	m2 := gfModel(t, dir, closed) // restart
	if !gr(m2).folds()["ini"] {
		t.Error("restart unfolded the closed root")
	}
	m = space(t, m) // open it: differs from the closed default
	if got := gfRead(t, dir); got != `{"ini":false}` {
		t.Errorf("after opening = %s", got)
	}
	m = space(t, m) // folded again: equals the default, so the key drops
	if got := gfRead(t, dir); got != `{}` {
		t.Errorf("a touched key equal to the default must drop, got %s", got)
	}
}
