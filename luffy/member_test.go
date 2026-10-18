package luffy

import (
	"errors"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

func memberFixture(t *testing.T) {
	t.Helper()
	ini := bd.Bead{ID: "ws-ini", Kind: contract.KindInitiative, Status: "open", Title: "Big effort",
		Description: "## Done when\n\nEverything ships.\n\n## Phases\n\n1. first (under way)\n2. second\n\n## Notes\n\nnote\n"}
	all := []bd.Bead{ini,
		{ID: "ws-sig", Kind: contract.KindSignal, Status: "open", Title: "me", Parent: "ws-ini"},
		{ID: "ws-b", Kind: contract.KindChart, Status: "open", Title: "chart sib", Parent: "ws-ini", Labels: []string{"context:api"}},
		{ID: "ws-a", Kind: contract.KindSignal, Status: "open", Title: "signal sib", Parent: "ws-ini"},
		{ID: "ws-z", Kind: contract.KindSpec, Status: "closed", Title: "closed sib", Parent: "ws-ini"},
		{ID: "ws-other", Kind: contract.KindSignal, Status: "open", Title: "elsewhere", Parent: "ws-x"},
	}
	stubRequire(t, false, "ws-ini")
	swap(t, &listAll, func() ([]bd.Bead, error) { return all, nil })
}

func TestLaunchMemberSectionForInitiativeParentedSignal(t *testing.T) {
	memberFixture(t)
	got := launchPrompt(t)
	rest := got[strings.Index(got, "## Member of an Initiative"):]
	for _, want := range []string{"ws-ini — Big effort", "Everything ships.", "1. first (under way)", "2. second",
		"- ws-a — signal sib [undecided] (signal)", "- ws-b — chart sib [api] (chart)",
		`bd close ws-sig --reason "later:`, "bd supersede ws-sig --with <sibling>"} {
		if !strings.Contains(rest, want) {
			t.Errorf("section missing %q:\n%s", want, rest)
		}
	}
	for _, bad := range []string{"ws-z", "ws-other", "- ws-sig", "note\n"} {
		if strings.Contains(rest, bad) {
			t.Errorf("section must not contain %q", bad)
		}
	}
	if !strings.Contains(rest, "Never `bd supersede <signal> --with <initiative>`") {
		t.Error("section must forbid superseding with the Initiative")
	}
	if strings.Index(rest, "ws-a —") > strings.Index(rest, "ws-b —") {
		t.Error("siblings sorted by ID")
	}
}

func TestLaunchLooseSignalHasNoMemberSection(t *testing.T) {
	memberFixture(t)
	stubRequire(t, false, "")
	if strings.Contains(launchPrompt(t), "Member of an Initiative") {
		t.Error("loose Signal must render today's prompt")
	}
}

func TestLaunchMemberSectionDroppedOnFailingRead(t *testing.T) {
	memberFixture(t)
	swap(t, &listAll, func() ([]bd.Bead, error) { return nil, errors.New("boom") })
	if strings.Contains(launchPrompt(t), "Member of an Initiative") {
		t.Error("a failing read drops the section, not the launch")
	}
}

func TestLaunchMemberSectionBelongOffer(t *testing.T) {
	memberFixture(t)
	off := launchPrompt(t)
	off = off[strings.Index(off, "## Member of an Initiative"):]
	if !strings.Contains(off, "mugicrew initiative release ws-sig") || strings.Contains(off, "initiative adopt") {
		t.Errorf("setting off offers release only:\n%s", off)
	}

	other := bd.Bead{ID: "ws-ini2", Kind: contract.KindInitiative, Status: "open", Title: "Other", Description: "## Done when\n\nOther done.\n"}
	all, _ := listAll()
	stubRequire(t, true, "ws-ini")
	swap(t, &listAll, func() ([]bd.Bead, error) { return append(all, other), nil })
	on := launchPrompt(t)
	rest := on[strings.Index(on, "## Member of an Initiative"):]
	for _, want := range []string{"mugicrew initiative adopt ws-sig <initiativeID>", "ws-ini2 — Other", "Other done.", "REJECT"} {
		if !strings.Contains(rest, want) {
			t.Errorf("missing %q:\n%s", want, rest)
		}
	}
	if strings.Contains(rest, "initiative release") {
		t.Error("release must not be offered under the setting")
	}
	if strings.Contains(rest[strings.Index(rest, "Open Initiatives"):], "ws-ini —") {
		t.Error("current parent excluded from the move list")
	}
}
