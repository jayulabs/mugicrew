package luffy

import (
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

func TestRespinSectionCarriesSeedLandedTipAndMembers(t *testing.T) {
	seedOf = func(string) (string, error) { return "seed123\n", nil }
	swap(t, &isRespin, func(string) (bool, error) { return true, nil })
	listChildren = func(string) ([]bd.Bead, error) {
		return []bd.Bead{
			{Title: "Refiled build", Kind: contract.KindBuild},
			{Title: "A review", Kind: contract.KindReview},
		}, nil
	}
	cp := bd.Bead{ID: "cp", Description: "Respin of Old, which a Forge merge landed at abc; Remnant r.\nLanded-tip: tip999\n"}
	got, _ := respinSection(cp)
	for _, want := range []string{"Landed-tip: tip999", "seed123", "Refiled build", "A respin's Packet", "approval-body.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("section missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "A review") {
		t.Errorf("non-member kinds listed:\n%s", got)
	}
}

func TestRespinSectionEmptyForOrdinaryCheckpoint(t *testing.T) {
	if got, _ := respinSection(bd.Bead{ID: "cp", Description: "Spec: x"}); got != "" {
		t.Errorf("non-respin section = %q, want empty", got)
	}
	if got, _ := withRespin("P", bd.Bead{ID: "cp", Description: "plain"}); got != "P" {
		t.Errorf("withRespin changed a non-respin prompt: %q", got)
	}
}
