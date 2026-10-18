package jinbe

import (
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/nami"
)

// requestedRig is a container's children as the graph holds them, with the
// Conflict filing and supersede edges recorded (ADR 0261).
type requestedRig struct {
	kids       []bd.Bead
	filed      []bd.Successor
	closed     map[string]string
	supersedes [][2]string
}

func rigRequested(t *testing.T, kind contract.Kind, status string) *requestedRig {
	t.Helper()
	r := &requestedRig{closed: map[string]string{}}
	r.kids = []bd.Bead{{ID: "ws-req", Kind: kind, Status: status, Description: "variant: " + nami.ConflictRequested}}
	oKids, oClose, oSpawn, oSup := childrenOfKind, closeBead, spawnChild, supersede
	t.Cleanup(func() { childrenOfKind, closeBead, spawnChild, supersede = oKids, oClose, oSpawn, oSup })
	childrenOfKind = func(_ string, k contract.Kind) ([]bd.Bead, error) {
		var out []bd.Bead
		for _, b := range r.kids {
			if b.Kind == k {
				out = append(out, b)
			}
		}
		return out, nil
	}
	closeBead = func(id, reason string) error {
		r.closed[id] = reason
		for i := range r.kids {
			if r.kids[i].ID == id {
				r.kids[i].Status, r.kids[i].CloseReason, r.kids[i].Closed = "closed", reason, time.Now()
			}
		}
		return nil
	}
	spawnChild = func(_ string, s bd.Successor) (string, bool, error) {
		for _, k := range r.kids {
			if k.Kind == s.Kind && k.Open() {
				return k.ID, false, nil
			}
		}
		r.filed = append(r.filed, s)
		r.kids = append(r.kids, bd.Bead{ID: "ws-conf", Kind: s.Kind, Status: "open", Description: s.Description, Created: time.Now().Add(time.Second)})
		return "ws-conf", true, nil
	}
	supersede = func(old, n string) error {
		for _, e := range r.supersedes {
			if e == [2]string{old, n} {
				return nil
			}
		}
		r.supersedes = append(r.supersedes, [2]string{old, n})
		return nil
	}
	return r
}

func TestFileConflictSupersedesUnclaimedRequestedRebase(t *testing.T) {
	head := func(string) (string, error) { return "tip", nil }
	cp := openCheckpoint()
	chart := bd.Bead{ID: "ws-ch", Kind: contract.KindChart, Status: "open", Title: "A chart"}
	for _, c := range []struct {
		bead bd.Bead
		kind contract.Kind
	}{{cp, contract.KindConflict}, {chart, contract.KindChartConflict}} {
		r := rigRequested(t, c.kind, "open")
		for i := 0; i < 2; i++ { // the re-run converges
			if err := fileConflict(c.bead, "newbase", nil, head, false); err != nil {
				t.Fatal(err)
			}
		}
		if len(r.filed) != 1 {
			t.Fatalf("%s: filed %d conflicts, want 1", c.kind, len(r.filed))
		}
		if got := r.closed["ws-req"]; got != "superseded: "+r.filed[0].Title {
			t.Errorf("%s: request closed %q", c.kind, got)
		}
		if len(r.supersedes) != 1 || r.supersedes[0] != [2]string{"ws-req", "ws-conf"} {
			t.Errorf("%s: supersede edges %v", c.kind, r.supersedes)
		}
	}
}

func TestFileConflictRecoversMissingSupersedeEdge(t *testing.T) {
	r := rigRequested(t, contract.KindConflict, "closed")
	title := "Checkpoint ws-cp conflicts with main"
	r.kids[0].CloseReason, r.kids[0].Closed = "superseded: "+title, time.Now()
	r.kids = append(r.kids, bd.Bead{ID: "ws-conf", Kind: contract.KindConflict, Status: "open", Created: time.Now().Add(time.Second)})
	if err := fileConflict(openCheckpoint(), "b", nil, func(string) (string, error) { return "tip", nil }, false); err != nil {
		t.Fatal(err)
	}
	if len(r.supersedes) != 1 || r.supersedes[0] != [2]string{"ws-req", "ws-conf"} {
		t.Errorf("supersede edges %v, want the missing edge added", r.supersedes)
	}
}

func TestFileConflictLeavesClaimedRequestedRebase(t *testing.T) {
	r := rigRequested(t, contract.KindConflict, "in_progress")
	if err := fileConflict(openCheckpoint(), "newbase", nil, func(string) (string, error) { return "tip", nil }, false); err != nil {
		t.Fatal(err)
	}
	if len(r.closed) != 0 || len(r.filed) != 0 || len(r.supersedes) != 0 {
		t.Errorf("closed %v filed %d edges %v, want the claimed request untouched", r.closed, len(r.filed), r.supersedes)
	}
}
