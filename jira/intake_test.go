package jira

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/cfglayer"
)

const (
	siteCfg  = "[jira]\nsite = \"acme.atlassian.net\"\n"
	itemJSON = `{"key":"ABC-1","fields":{"summary":"Fix it","status":{"name":"Open","statusCategory":{"key":"new"}}}}`
)

// intakeWorkspace is a Workspace root whose shared config is cfg, with a fake acli
// on PATH printing out and the bd legs stubbed; it returns the root, the acli argv
// log and the Signals filed.
func intakeWorkspace(t *testing.T, cfg, out string) (root, argvLog string, filed *[][3]string) {
	t.Helper()
	root = t.TempDir()
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(root, ".mugicrew"), 0o755)
	os.WriteFile(filepath.Join(root, ".mugicrew", "config.toml"), []byte(cfg), 0o644)
	oN, oC, oS := NewSignalWithRef, CarrierOf, Supersede
	t.Cleanup(func() { NewSignalWithRef, CarrierOf, Supersede = oN, oC, oS })
	CarrierOf = func(string) (bd.Bead, bool, error) { return bd.Bead{}, false, nil }
	Supersede = func(string, string) error { t.Error("superseded with no live carrier"); return nil }
	filed = &[][3]string{}
	NewSignalWithRef = func(title, body, ref string) (string, error) {
		*filed = append(*filed, [3]string{title, body, ref})
		return "mc-new", nil
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "out"), []byte(out), 0o644)
	os.WriteFile(filepath.Join(dir, "acli"), []byte("#!/bin/sh\necho \"$*\" >> "+dir+"/argv.log\ncat "+dir+"/out\n"), 0o755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return root, filepath.Join(dir, "argv.log"), filed
}

func TestIntakeFilesARefCarryingSignal(t *testing.T) {
	root, log, filed := intakeWorkspace(t, siteCfg, itemJSON)
	res, err := Intake(root, "ABC-1")
	if err != nil || res != (IntakeResult{ID: "mc-new", Key: "ABC-1"}) {
		t.Fatalf("res %+v err %v", res, err)
	}
	wantBody := "Jira work item ABC-1: https://acme.atlassian.net/browse/ABC-1\n\nTriage reads this work item from Jira; this body only points at it:\n\n    acli jira workitem view ABC-1\n"
	if want := [][3]string{{"ABC-1: Fix it", wantBody, "jira-ABC-1"}}; !slices.Equal(*filed, want) {
		t.Errorf("filed %q, want %q", *filed, want)
	}
	if b, _ := os.ReadFile(log); string(b) != "jira workitem view ABC-1 --fields summary,status --json\n" {
		t.Errorf("acli argv log %q: want exactly one view call", b)
	}
}

func TestIntakeMovedKeyWins(t *testing.T) {
	root, _, filed := intakeWorkspace(t, siteCfg, `{"key":"NEW-9","fields":{"summary":"Moved","status":{"name":"Open","statusCategory":{"key":"new"}}}}`)
	res, err := Intake(root, "OLD-1")
	if err != nil || res.Key != "NEW-9" || res.ID != "mc-new" {
		t.Fatalf("res %+v err %v", res, err)
	}
	f := (*filed)[0]
	if f[0] != "NEW-9: Moved" || f[2] != "jira-NEW-9" || !strings.Contains(f[1], "https://acme.atlassian.net/browse/NEW-9") {
		t.Errorf("filed %q, want the returned key throughout", f)
	}
}

func TestIntakeReintakeSupersedesOntoCarrier(t *testing.T) {
	root, _, filed := intakeWorkspace(t, siteCfg, itemJSON)
	var asked string
	var edge [2]string
	CarrierOf = func(ref string) (bd.Bead, bool, error) { asked = ref; return bd.Bead{ID: "mc-old"}, true, nil }
	Supersede = func(old, n string) error { edge = [2]string{old, n}; return nil }
	res, err := Intake(root, "ABC-1")
	if err != nil || res != (IntakeResult{ID: "mc-new", Key: "ABC-1", Carrier: "mc-old"}) {
		t.Fatalf("res %+v err %v", res, err)
	}
	if asked != "jira-ABC-1" || len(*filed) != 1 || edge != [2]string{"mc-new", "mc-old"} {
		t.Errorf("asked %q, filed %d, edge %v", asked, len(*filed), edge)
	}
}

func TestIntakeAfterCarrierClosedStaysLive(t *testing.T) {
	root, _, _ := intakeWorkspace(t, siteCfg, itemJSON) // CarrierOf finds none; Supersede fails the test
	if res, err := Intake(root, "ABC-1"); err != nil || res.Carrier != "" {
		t.Errorf("res %+v err %v", res, err)
	}
}

func TestIntakeRefusesABadSiteBeforeAcliRuns(t *testing.T) {
	for name, cfg := range map[string]string{
		"no jira table": "gate = \"x\"\n",
		"no site":       "[jira]\n",
		"site with /":   "[jira]\nsite = \"acme.atlassian.net/x\"\n",
	} {
		root, log, filed := intakeWorkspace(t, cfg, itemJSON)
		res, err := Intake(root, "ABC-1")
		if err == nil || !strings.Contains(err.Error(), "site") || !strings.Contains(err.Error(), "config.toml") {
			t.Errorf("%s: err = %v, want a refusal naming the key and the config file", name, err)
		}
		if _, statErr := os.Stat(log); statErr == nil || len(*filed) != 0 || res != (IntakeResult{}) {
			t.Errorf("%s: acli ran, a Signal was filed, or res = %+v", name, res)
		}
	}
}

func TestIntakeBadSiteNamesTheLayerThatSuppliedIt(t *testing.T) {
	root, _, _ := intakeWorkspace(t, siteCfg, "")
	local := cfglayer.LocalPath(root)
	os.MkdirAll(filepath.Dir(local), 0o755)
	os.WriteFile(local, []byte("[jira]\n"), 0o644)
	if _, err := Intake(root, "ABC-1"); err == nil || !strings.Contains(err.Error(), local) {
		t.Errorf("err = %v, want it to name %s", err, local)
	}
}

func TestIntakeUnparseableOutputFilesNothing(t *testing.T) {
	root, _, filed := intakeWorkspace(t, siteCfg, "usage: acli")
	res, err := Intake(root, "ABC-1")
	if want := "intake jira ABC-1: acli gave unexpected output for ABC-1; it may be too old (check 'acli --version' and update)"; err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
	if len(*filed) != 0 || res != (IntakeResult{}) {
		t.Errorf("filed %v, res %+v, want nothing", *filed, res)
	}
}

func TestIntakeUnknownKeyFilesNothing(t *testing.T) {
	root, _, filed := intakeWorkspace(t, siteCfg, "")
	dir := strings.SplitN(os.Getenv("PATH"), ":", 2)[0]
	os.WriteFile(filepath.Join(dir, "acli"), []byte("#!/bin/sh\nif [ \"$2\" = view ]; then echo 'no such item' >&2; exit 1; fi\nexit 1\n"), 0o755)
	res, err := Intake(root, "NOPE-1")
	if err == nil || !strings.HasPrefix(err.Error(), "intake jira NOPE-1: ") || len(*filed) != 0 || res != (IntakeResult{}) {
		t.Errorf("res %+v err %v filed %v", res, err, *filed)
	}
}

func TestIntakeWrapsLookupAndFilingErrors(t *testing.T) {
	root, _, _ := intakeWorkspace(t, siteCfg, itemJSON)
	boom := errors.New("boom")
	CarrierOf = func(string) (bd.Bead, bool, error) { return bd.Bead{}, false, boom }
	if res, err := Intake(root, "ABC-1"); err == nil || err.Error() != "intake jira ABC-1: boom" || res != (IntakeResult{Key: "ABC-1"}) {
		t.Errorf("CarrierOf res %+v err = %v", res, err)
	}
	CarrierOf = func(string) (bd.Bead, bool, error) { return bd.Bead{}, false, nil }
	NewSignalWithRef = func(string, string, string) (string, error) { return "", boom }
	if res, err := Intake(root, "ABC-1"); err == nil || err.Error() != "intake jira ABC-1: boom" || res != (IntakeResult{Key: "ABC-1"}) {
		t.Errorf("NewSignalWithRef res %+v err = %v", res, err)
	}
}

func TestIntakePartialSupersedeFailureKeepsTheID(t *testing.T) {
	root, _, _ := intakeWorkspace(t, siteCfg, itemJSON)
	CarrierOf = func(string) (bd.Bead, bool, error) { return bd.Bead{ID: "mc-old"}, true, nil }
	Supersede = func(string, string) error { return errors.New("boom") }
	res, err := Intake(root, "ABC-1")
	if want := "intake jira ABC-1: filed mc-new but could not supersede it onto mc-old: boom"; err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
	if res.ID != "mc-new" || res.Carrier != "" {
		t.Errorf("res %+v, want ID set and no carrier", res)
	}
}
