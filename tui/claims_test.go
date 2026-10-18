package tui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/session"
)

// claimStubs stubs a narrow slow-tier read (def) and a by-id store (byID), and
// returns the ids listByID was asked for.
func claimStubs(t *testing.T, open, def *[]bd.Bead, byID map[string]bd.Bead, recents []string) *[][]string {
	t.Helper()
	asked := new([][]string)
	seam(t, &listOpen, func() ([]bd.Bead, error) { return *open, nil })
	seam(t, &listAll, func() ([]bd.Bead, error) { return *def, nil })
	seam(t, &defaultIDs, func() ([]string, error) { return ids(*def), nil })
	seam(t, &listByID, func(ids []string) ([]bd.Bead, error) {
		*asked = append(*asked, ids)
		var out []bd.Bead
		for _, id := range ids {
			if b, ok := byID[id]; ok {
				out = append(out, b)
			}
			for _, b := range *def {
				if b.ID == id {
					out = append(out, b)
				}
			}
		}
		return out, nil
	})
	dir := t.TempDir()
	seam(t, &recentsPath, func() string { return filepath.Join(dir, "recents.json") })
	if recents != nil {
		os.WriteFile(recentsPath(), []byte(`["`+recents[0]+`"]`), 0o644)
	}
	return asked
}

func TestRecentsClaimKeepsUnloadedClosedBead(t *testing.T) {
	open := []bd.Bead{{ID: "a", Status: "open"}}
	def := []bd.Bead{}
	old := bd.Bead{ID: "old", Status: "closed"}
	asked := claimStubs(t, &open, &def, map[string]bd.Bead{"old": old}, []string{"old"})
	m := sized(t, 120, 30)
	m = runLoad(t, m, m.loadSlow())
	if got := ids(m.beads); !slices.Equal(got, []string{"a", "old"}) {
		t.Fatalf("slow load beads %v", got)
	}
	if len(*asked) != 1 || !slices.Equal((*asked)[0], []string{"old"}) {
		t.Fatalf("fetched %v", *asked)
	}
	m = runLoad(t, m, m.loadTier(false)) // 3 s loads keep it
	m = runLoad(t, m, m.loadTier(false))
	if got := ids(m.beads); !slices.Equal(got, []string{"a", "old"}) {
		t.Errorf("claimed bead lost across 3 s loads: %v", got)
	}
	// unclaimed: evicted at the next fold
	os.WriteFile(recentsPath(), []byte(`[]`), 0o644)
	m = runLoad(t, m, m.loadTier(false))
	if got := ids(m.beads); !slices.Equal(got, []string{"a"}) {
		t.Errorf("unclaimed bead not evicted: %v", got)
	}
}

func TestClaimMergeOpenBeatsDemandAndDeletedDrops(t *testing.T) {
	open := []bd.Bead{}
	def := []bd.Bead{}
	claimStubs(t, &open, &def, map[string]bd.Bead{"x": {ID: "x", Status: "closed", Title: "demand"}}, []string{"x"})
	m := sized(t, 120, 30)
	m = runLoad(t, m, m.loadSlow())
	// reopened: the fresh open copy wins, no drop-out
	open = []bd.Bead{{ID: "x", Status: "open", Title: "open"}}
	m = runLoad(t, m, m.loadTier(false))
	if len(m.beads) != 1 || m.beads[0].Title != "open" {
		t.Errorf("reopen lost: %+v", m.beads)
	}
	// deleted: a slow load no longer finds it
	open = nil
	seam(t, &listByID, func([]string) ([]bd.Bead, error) { return nil, nil })
	m = runLoad(t, m, m.loadSlow())
	if len(m.beads) != 0 {
		t.Errorf("deleted claimed bead stayed: %+v", m.beads)
	}
}

func TestSessionClaimFiresDoneFlip(t *testing.T) {
	open := []bd.Bead{{ID: "h", Status: "in_progress"}}
	def := []bd.Bead{}
	claimStubs(t, &open, &def, map[string]bd.Bead{"h": {ID: "h", Status: "closed"}}, nil)
	seam(t, &readSessions, func() (map[string]session.Info, error) {
		return map[string]session.Info{"h": {State: session.Alive}}, nil
	})
	var done map[string]bool
	seam(t, &setBars, func(d map[string]bool, _ map[string]string, _ map[string]session.Info) error { done = d; return nil })
	m := sized(t, 120, 30)
	m = runLoad(t, m, m.loadSlow())
	if done["h"] {
		t.Fatal("open bead flipped done")
	}
	open = nil // it closes; the slow tier does not bring it
	m = runLoad(t, m, m.loadSlow())
	if !done["h"] {
		t.Errorf("closed session bead missed its done-flip: %v", done)
	}
}
