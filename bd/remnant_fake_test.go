package bd_test

import (
	"strings"
	"sync"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/bd/bdtest"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// Concurrent appends to one Remnant body each land: the read-then-write runs under the
// land lock (ADR 0070 §1), so no writer's lines are lost to a stale read.
func TestAppendLinesConcurrentWritersLoseNothing(t *testing.T) {
	// The land lock lives under the git-common-dir; a sandboxed build runs outside any repo.
	common := t.TempDir()
	was := git.CommonDir
	git.CommonDir = func() (string, error) { return common, nil }
	t.Cleanup(func() { git.CommonDir = was })
	f := bdtest.New(t, bd.Bead{ID: "t-rem", Status: "open", Kind: contract.KindRemnant, Description: "head"})
	const n = 8
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := bd.AppendLines("t-rem", "Child: "+string(rune('a'+i))); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	body := f.Bead("t-rem").Description
	for i := range n {
		if l := "Child: " + string(rune('a'+i)); !strings.Contains(body, l) {
			t.Errorf("body lost %q: %q", l, body)
		}
	}
}
