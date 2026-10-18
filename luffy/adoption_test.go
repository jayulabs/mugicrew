package luffy

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

func TestTriagePromptAdoptionOffer(t *testing.T) {
	base := triagePrompt("ws-sig", "", "", nil, nil)
	if got := triagePrompt("ws-sig", "", "", nil, []bd.Bead{}); got != base {
		t.Error("no offers must leave the prompt byte-identical")
	}
	ini := bd.Bead{ID: "ws-ini", Title: "Ship it", Description: "## Done when\n\nUsers can ship.\n\n## Phases\n- one"}
	rest, ok := strings.CutPrefix(triagePrompt("ws-sig", "", "", nil, []bd.Bead{ini}), base)
	if !ok {
		t.Fatal("the offer must be appended")
	}
	for _, want := range []string{"## Adoption offer", "ws-ini — Ship it", "Users can ship.", "mugicrew initiative adopt ws-sig <initiativeID>", "BEFORE any mugicrew reconcile", "mutually exclusive"} {
		if !strings.Contains(rest, want) {
			t.Errorf("offer missing %q:\n%s", want, rest)
		}
	}
	if strings.Contains(rest, "- one") {
		t.Error("Done when must stop at the next section")
	}
}

func TestLaunchOffersAdoptionOnlyForOffshootOfInitiativeMember(t *testing.T) {
	beads := map[string]bd.Bead{
		"ws-sig":   {ID: "ws-sig", Kind: contract.KindSignal},
		"ws-kid":   {ID: "ws-kid", Parent: "ws-ini"},
		"ws-ini":   {ID: "ws-ini", Kind: contract.KindInitiative, Status: "open", Title: "T", Description: "## Done when\nx"},
		"ws-plain": {ID: "ws-plain", Parent: "ws-other"},
		"ws-other": {ID: "ws-other", Kind: contract.KindChart, Status: "open"},
	}
	swap(t, &show, func(id string) (bd.Bead, error) { return beads[id], nil })
	srcs := map[string][]bd.Bead{"ws-sig": {{ID: "ws-kid"}}} // dep-list rows carry no Parent
	swap(t, &discoveredFrom, func(id string) ([]bd.Bead, error) { return srcs[id], nil })

	if got := adoptionOffers("ws-sig"); len(got) != 1 || got[0].ID != "ws-ini" {
		t.Errorf("offshoot offers = %v, want ws-ini", got)
	}
	sig := beads["ws-sig"]
	sig.Parent = "ws-ini"
	beads["ws-sig"] = sig
	if got := adoptionOffers("ws-sig"); got != nil {
		t.Errorf("parented Signal offers = %v, want none", got)
	}
	srcs["ws-plain-sig"] = []bd.Bead{{ID: "ws-plain"}}
	beads["ws-plain-sig"] = bd.Bead{ID: "ws-plain-sig"}
	if got := adoptionOffers("ws-plain-sig"); got != nil {
		t.Errorf("non-offshoot offers = %v, want none", got)
	}
	if got := adoptionOffers("ws-none"); got != nil {
		t.Errorf("sourceless Signal offers = %v, want none", got)
	}
}

func TestAdoptionOffersClimbDedupeAndSurviveErrors(t *testing.T) {
	beads := map[string]bd.Bead{
		"ws-sig":      {ID: "ws-sig"},
		"ws-a":        {ID: "ws-a", Kind: contract.KindInitiative, Status: "open", Description: "## Done when\nx"},
		"ws-b":        {ID: "ws-b", Kind: contract.KindInitiative, Status: "open", Description: "## Done when\nx"},
		"ws-done":     {ID: "ws-done", Kind: contract.KindInitiative, Status: "closed", Description: "## Done when\nx"},
		"ws-bodyless": {ID: "ws-bodyless", Kind: contract.KindInitiative, Status: "open", Description: "seed"},
		"ws-bl-kid":   {ID: "ws-bl-kid", Parent: "ws-bodyless"},
		"ws-chart":    {ID: "ws-chart", Kind: contract.KindChart, Parent: "ws-b"},
		"ws-defog":    {ID: "ws-defog", Parent: "ws-chart"},
		"ws-direct":   {ID: "ws-direct", Parent: "ws-a"},
		"ws-twin":     {ID: "ws-twin", Parent: "ws-a"},
		"ws-closed":   {ID: "ws-closed", Parent: "ws-done"},
		"ws-deep":     {ID: "ws-deep", Parent: "ws-defog"},
		"ws-bad":      {ID: "ws-bad", Parent: "ws-missing"},
	}
	swap(t, &show, func(id string) (bd.Bead, error) {
		b, ok := beads[id]
		if !ok {
			return bd.Bead{}, errors.New("read failed")
		}
		return b, nil
	})
	var srcs []bd.Bead
	swap(t, &discoveredFrom, func(string) ([]bd.Bead, error) { return srcs, nil })
	ids := func() (out []string) {
		for _, o := range adoptionOffers("ws-sig") {
			out = append(out, o.ID)
		}
		return
	}
	for _, c := range []struct {
		name string
		srcs []string
		want []string
	}{
		{"defog source", []string{"ws-defog"}, []string{"ws-b"}},
		{"drained open Initiative offered", []string{"ws-direct"}, []string{"ws-a"}},
		{"bodyless not offered", []string{"ws-bl-kid"}, nil},
		{"closed not offered", []string{"ws-closed"}, nil},
		{"two sources one offer", []string{"ws-direct", "ws-twin"}, []string{"ws-a"}},
		{"two initiatives sorted", []string{"ws-defog", "ws-direct"}, []string{"ws-a", "ws-b"}},
		{"too deep", []string{"ws-deep"}, nil},
		{"source read error skips source", []string{"ws-gone", "ws-direct"}, []string{"ws-a"}},
		{"read error skips source", []string{"ws-bad", "ws-direct"}, []string{"ws-a"}},
	} {
		srcs = nil
		for _, id := range c.srcs {
			srcs = append(srcs, bd.Bead{ID: id}) // dep-list rows carry no Parent
		}
		if got := ids(); !slices.Equal(got, c.want) {
			t.Errorf("%s: offers = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestLaunchSurvivesFailingOfferSeam(t *testing.T) {
	swap(t, &show, func(string) (bd.Bead, error) { return bd.Bead{}, errors.New("read failed") })
	h := &stubHarness{}
	if _, err := (Luffy{}).Launch(stubCtx{h}, contract.Bead{ID: "ws-sig", Kind: contract.KindSignal}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if !strings.HasSuffix(h.inv.Prompt, triagePrompt("ws-sig", "", "", declaredContexts(), nil)) {
		t.Error("a failing offer read must yield the base triage prompt")
	}
}
