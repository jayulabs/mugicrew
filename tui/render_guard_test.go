package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/claudecode"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/franky"
	"jayulabs.org/mugicrew/jinbe"
	"jayulabs.org/mugicrew/luffy"
	"jayulabs.org/mugicrew/nami"
	"jayulabs.org/mugicrew/sanji"
	"jayulabs.org/mugicrew/usopp"
	"jayulabs.org/mugicrew/zoro"
)

// Rendering runs no subprocess (ADR 0101 §4): with recording bd, git and tmux
// fakes first on PATH and the production crew wired, View with any kind selected
// in the Graph detail starts none of them. PATH, not a seam var, so a subprocess
// from any package is caught.
func TestViewRunsNoSubprocess(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(bin, "calls.log")
	for _, name := range []string{"bd", "git", "tmux"} {
		script := "#!/bin/sh\necho \"" + name + " $*\" >> '" + log + "'\nexit 0\n"
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	crew := map[string]contract.Crewmate{ // main.crew()
		"nami": nami.Nami{}, "zoro": zoro.Zoro{}, "sanji": sanji.Sanji{}, "franky": franky.Franky{},
		"usopp": usopp.Usopp{}, "luffy": luffy.Luffy{}, "jinbe": jinbe.Jinbe{},
	}
	beads := []bd.Bead{
		{ID: "ch", Kind: contract.KindChart, Container: true, Title: "Chart", Status: "open"},
		{ID: "cp", Kind: contract.KindCheckpoint, Container: true, Title: "Checkpoint", Status: "open"},
		{ID: "done", Kind: contract.KindGrilling, Parent: "ch", Title: "Closed", Status: "closed"},
	}
	for _, k := range []contract.Kind{
		contract.KindSignal, contract.KindGrilling, contract.KindResearch, contract.KindChartConflict,
		contract.KindSpec, contract.KindPivot, contract.KindPrototype, contract.KindBuild,
		contract.KindCritique, contract.KindConflict, contract.KindReview, contract.KindApproval,
		contract.KindRollback, contract.KindSurgery,
	} {
		beads = append(beads, bd.Bead{ID: "b-" + string(k), Kind: k, Parent: "cp", Title: string(k), Status: "open"})
	}
	m := New(harnessCtx{claudecode.New("")}, crew)
	m.w, m.h = 120, 40
	m.hideClosedRoots = false
	next, _ := m.Update(loadedMsg{beads: beads})
	m = next.(Model)
	m.tab = tabGraph

	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, b := range beads {
		m.selGraph = b.ID
		m.View()
		if got, _ := os.ReadFile(log); len(got) > 0 {
			t.Fatalf("View with %s (%s) selected started subprocesses:\n%s", b.ID, b.Kind, got)
		}
	}
	m.selGraph = "b-approval"
	if v := m.View(); !strings.Contains(v, "Luffy opens an agent session for approval") {
		t.Errorf("Approval detail lacks its static launch preview:\n%s", v)
	}
}
