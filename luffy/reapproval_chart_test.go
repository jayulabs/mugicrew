package luffy

import (
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

func TestDecideReapprovalChartArm(t *testing.T) {
	withID := func(id string) string {
		return git.CommitPin("abc") + "\n" + git.PatchIDPin(id) + "\n\n### Approval History\n\nfirst.\n"
	}
	noID := git.CommitPin("abc") + "\n\n### Approval History\n\nfirst.\n"
	prior := func(body string) bd.Bead { return priorApproval("approved", body, cutoffAt) }
	const unchanged = "approved (MR already un-drafted, Chart content unchanged)"
	const redraft = "re-drafted (Chart content changed since the approved Approval)"
	cases := []struct {
		name      string
		self      string
		approvals []bd.Bead
		want      reapproval
	}{
		{"equal ids skip", withID("p1"), []bd.Bead{prior(withID("p1"))}, reapproval{skip: true, history: unchanged}},
		{"differing ids re-draft", withID("p2"), []bd.Bead{prior(withID("p1"))}, reapproval{history: redraft}},
		{"missing id on self", noID, []bd.Bead{prior(withID("p1"))}, reapproval{history: redraft}},
		{"missing id on cutoff", withID("p1"), []bd.Bead{prior(noID)}, reapproval{history: redraft}},
		{"no approved cutoff", withID("p1"), []bd.Bead{priorApproval("rework: x", withID("p1"), cutoffAt)}, reapproval{}},
		{"auto-approved cutoff", withID("p1"), []bd.Bead{prior(withID("p1") + afkLine + "\n")}, reapproval{}},
	}
	for _, c := range cases {
		self := bd.Bead{ID: "ws-appr", Description: c.self}
		if got := decideReapproval(contract.KindChart, self, c.approvals, nil); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}
