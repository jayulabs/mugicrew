package session

import (
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/bd/bdtest"
)

// TestSweptRoutesThroughFake: Swept reads through showBead = bd.Show, so a seeded bdtest
// graph answers it with no session var stubbed.
func TestSweptRoutesThroughFake(t *testing.T) {
	bdtest.New(t,
		bd.Bead{ID: "s-cp", Status: "closed", CloseReason: bd.ObsoleteReason + " swept"},
		bd.Bead{ID: "s-member", Status: "closed", Parent: "s-cp", CloseReason: "done"},
		bd.Bead{ID: "s-live", Status: "closed", Parent: "s-open", CloseReason: "done"},
		bd.Bead{ID: "s-open", Status: "open"},
		bd.Bead{ID: "s-appr", Status: "closed", CloseReason: bd.ApprovalObsoletedReason},
	)
	for id, want := range map[string]bool{"s-cp": true, "s-member": true, "s-live": false, "s-appr": true, "s-unknown": false} {
		if got := Swept(id); got != want {
			t.Errorf("Swept(%s) = %v, want %v", id, got, want)
		}
	}
}
