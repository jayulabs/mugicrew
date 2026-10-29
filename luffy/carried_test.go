package luffy

import (
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

func carriedFrom(id, causedBy, body string, at time.Time) bd.Bead {
	return bd.Bead{ID: id, Kind: contract.KindApproval, Status: "closed", CloseReason: "approved", Created: at, CausedBy: causedBy,
		Description: bd.CarriedApprovalBody("old", "abc", "", "1234567", []bd.Rename{{Path: "d/0395-x.md", Old: "0394", New: "0395"}})}
}

func TestDecideReapprovalReadsOriginOfCarriedCutoff(t *testing.T) {
	humanBody := git.CommitPin("abc") + "\n\n### Approval History\n\nfirst.\n"
	afkBody := humanBody + afkLine + "\n"
	later := cutoffAt.Add(time.Hour)
	self := bd.Bead{ID: "ws-appr", Description: humanBody}
	cases := []struct {
		name   string
		origin string
		want   reapproval
	}{
		{"AFK origin does not vouch", afkBody, reapproval{}},
		{"human origin vouches", humanBody, reapproval{skip: true, history: "approved (MR already un-drafted, Spec unchanged)"}},
	}
	for _, c := range cases {
		origin := priorApproval("approved", c.origin, cutoffAt)
		origin.ID = "origin"
		carried := carriedFrom("carry", "origin", "", later)
		if got := decideReapproval(contract.KindCheckpoint, self, []bd.Bead{origin, carried}, nil); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestDecideReapprovalChartComparesPatchIDAgainstCarriedCutoff(t *testing.T) {
	origin := priorApproval("approved", git.CommitPin("abc")+"\n"+git.PatchIDPin("p1")+"\n\n### Approval History\n\nfirst.\n", cutoffAt)
	origin.ID = "origin"
	carried := carriedFrom("carry", "origin", "", cutoffAt.Add(time.Hour))
	carried.Description = bd.CarriedApprovalBody("old", "abc", "p2", "1234567", nil)
	self := bd.Bead{ID: "ws-appr", Description: git.CommitPin("abc") + "\n" + git.PatchIDPin("p2") + "\n"}
	got := decideReapproval(contract.KindChart, self, []bd.Bead{origin, carried}, nil)
	if !got.skip {
		t.Errorf("carried cutoff with equal Patch-id should skip, got %+v", got)
	}
}

func TestCarriedLapsSinceLastNonCarriedStamp(t *testing.T) {
	t0 := cutoffAt
	old := carriedFrom("c0", "h0", "", t0)
	human := priorApproval("approved", "x", t0.Add(time.Hour))
	human.ID = "h1"
	c1 := carriedFrom("c1", "h1", "", t0.Add(2*time.Hour))
	c2 := carriedFrom("c2", "c1", "", t0.Add(3*time.Hour))
	rework := priorApproval("rework: x", "x", t0.Add(4*time.Hour))
	lines := carriedLaps([]bd.Bead{c2, old, human, c1, rework}, "self")
	if len(lines) != 2 {
		t.Fatalf("want 2 laps since the human stamp, got %v", lines)
	}
	p := withCarried("PROMPT", lines)
	if !strings.Contains(p, "Approval History") || strings.Count(p, "carried (renumber-only") != 2 {
		t.Errorf("prompt does not list both laps: %q", p)
	}
	if withCarried("PROMPT", nil) != "PROMPT" {
		t.Errorf("no laps must leave the prompt unchanged")
	}
}
