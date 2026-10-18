package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contexts/contextstest"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// A run-now orphan drop checks the child tree of a nested effort through the
// container's context: label, since a leaf never carries one (Spec FR3).
func TestDropKeptTreeRefusesDirtyChildTree(t *testing.T) {
	origDirty, origDrop, origSess, origShow, origList, origProbe := treeDirty, dropOrphan, readSessions, contexts.ShowBead, listAll, probeLiveness
	t.Cleanup(func() {
		treeDirty, dropOrphan, readSessions, contexts.ShowBead, listAll, probeLiveness = origDirty, origDrop, origSess, origShow, origList, origProbe
	})
	_, kid := contextstest.Workspace(t)
	tmp, err := filepath.EvalSymlinks(t.TempDir()) // the spelling git reports, as the mint now gives
	if err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(tmp, "mugicrew-wt-7", "wt")
	contextstest.Git(t, kid, "worktree", "add", "-q", "-b", "effort/lf", filepath.Join(host, "kid"))
	contexts.ShowBead = func(id string) (bd.Bead, error) {
		return bd.Bead{ID: id, Labels: []string{contextstest.Label}}, nil
	}
	treeDirty = func(p string) (bool, error) { return p == filepath.Join(host, "kid"), nil }
	readSessions = func() (map[string]session.Info, error) { return nil, nil }
	listAll = func() ([]bd.Bead, error) { return nil, nil }
	probeLiveness = func([]string) map[string]bool { return nil }
	dropped := false
	dropOrphan = func(string) error { dropped = true; return nil }

	leaf := bd.Bead{ID: "lf", Kind: contract.KindBuild, Parent: "cp1", Status: "open"}
	if s := dropKeptTree(leaf); !strings.Contains(s, "uncommitted work") || dropped {
		t.Errorf("status %q, dropped %v; want a refusal and no drop", s, dropped)
	}
}
