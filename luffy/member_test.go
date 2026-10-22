package luffy

import (
	"errors"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// memberFixture seeds the Initiative and its members; sigBody is ws-sig's description.
func memberFixture(t *testing.T, sigBody string) {
	t.Helper()
	ini := bd.Bead{ID: "ws-ini", Kind: contract.KindInitiative, Status: "open", Title: "Big effort",
		Description: "## Done when\n\nEverything ships.\n\n## Phases\n\n1. first (under way)\n2. second\n\n## Notes\n\nnote\n"}
	all := []bd.Bead{ini,
		{ID: "ws-sig", Kind: contract.KindSignal, Status: "open", Title: "me", Parent: "ws-ini", Description: sigBody},
		{ID: "ws-b", Kind: contract.KindChart, Status: "open", Title: "chart sib", Parent: "ws-ini", Labels: []string{"context:api"}},
		{ID: "ws-a", Kind: contract.KindSignal, Status: "open", Title: "signal sib", Parent: "ws-ini"},
		{ID: "ws-z", Kind: contract.KindSpec, Status: "closed", Title: "closed sib", Parent: "ws-ini"},
		{ID: "ws-other", Kind: contract.KindSignal, Status: "open", Title: "elsewhere", Parent: "ws-x"},
	}
	stubRequire(t, false, "ws-ini")
	swap(t, &listAll, func() ([]bd.Bead, error) { return all, nil })
}

func TestLaunchMemberSectionForInitiativeParentedSignal(t *testing.T) {
	memberFixture(t, "")
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
	memberFixture(t, "")
	stubRequire(t, false, "")
	if strings.Contains(launchPrompt(t), "Member of an Initiative") {
		t.Error("loose Signal must render today's prompt")
	}
}

func TestLaunchMemberSectionDroppedOnFailingRead(t *testing.T) {
	memberFixture(t, "")
	swap(t, &listAll, func() ([]bd.Bead, error) { return nil, errors.New("boom") })
	if strings.Contains(launchPrompt(t), "Member of an Initiative") {
		t.Error("a failing read drops the section, not the launch")
	}
}

func TestLaunchMemberSectionBelongOffer(t *testing.T) {
	memberFixture(t, "")
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

// A member Signal is not open to REROUTE: it has Initiative-placement rules to follow.
func TestMemberSectionExcludesReroute(t *testing.T) {
	got := memberSection("ws-sig", bd.Bead{ID: "ws-ini"}, nil, false, nil)
	if !strings.Contains(got, "REROUTE is not open to a member") {
		t.Errorf("member section must exclude REROUTE:\n%s", got)
	}
}

// orderedFixture: ws-sig is ordered against container ws-ctr, a Checkpoint that is a
// direct Initiative child, named on Phases line 1.
func orderedFixture(t *testing.T, body string, edges func(string) (blockedBy, blocks []bd.Bead, err error)) {
	t.Helper()
	memberFixture(t, body)
	all, _ := listAll()
	all = append(all,
		bd.Bead{ID: "ws-ctr", Kind: contract.KindCheckpoint, Status: "open", Title: "cause container", Parent: "ws-ini"})
	swap(t, &listAll, func() ([]bd.Bead, error) { return all, nil })
	ini := all[0]
	ini.Description = "## Done when\n\nEverything ships.\n\n## Phases\n\n1. cause container [api] (under way)\n2. second\n"
	all[0] = ini
	swap(t, &orderEdges, edges)
	swap(t, &show, func(id string) (bd.Bead, error) {
		return bd.Bead{ID: id, Kind: contract.KindSignal, Parent: "ws-ini", Description: body}, nil
	})
}

func TestMemberSectionOrderedFollowUp(t *testing.T) {
	orderedFixture(t, "Order: follow-up of cause container", func(string) ([]bd.Bead, []bd.Bead, error) {
		return []bd.Bead{{ID: "ws-ctr"}}, nil, nil
	})
	got := launchPrompt(t)
	for _, want := range []string{"Ordered: follow-up of cause container (phase: 1. cause container [api] (under way))",
		"leave it blocked", "Never park it", "closed same-context sibling does not count"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

func TestMemberSectionOrderedPrerequisite(t *testing.T) {
	orderedFixture(t, "Order: prerequisite of cause container", func(string) ([]bd.Bead, []bd.Bead, error) {
		return nil, []bd.Bead{{ID: "ws-ctr"}}, nil
	})
	got := launchPrompt(t)
	for _, want := range []string{"Ordered: prerequisite of cause container (phase: 1. cause container [api] (under way))",
		"<title> [<context>, prerequisite of cause container]", "exception to ADR 0252 §3"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

func TestMemberSectionUnorderedByteIdentical(t *testing.T) {
	memberFixture(t, "")
	swap(t, &orderEdges, func(string) ([]bd.Bead, []bd.Bead, error) {
		return []bd.Bead{{ID: "ws-a"}}, []bd.Bead{{ID: "ws-b"}}, nil // edges to non-containers
	})
	all, _ := listAll()
	want := memberSection("ws-sig", all[0], []bd.Bead{all[3], all[2]}, false, nil)
	if got := launchPrompt(t); !strings.Contains(got, want) || strings.Contains(got, "Ordered:") {
		t.Errorf("an unordered member's section must be unchanged:\n%s", got)
	}
}

// An unclosed member Task is its own container (FR23): an offshoot ordered against
// it still renders the Ordered: line, and the phase falls back when none names it.
func TestMemberSectionOrderedAgainstMemberTask(t *testing.T) {
	orderedFixture(t, "Order: follow-up of member task", func(string) ([]bd.Bead, []bd.Bead, error) {
		return []bd.Bead{{ID: "ws-task"}}, nil, nil
	})
	all, _ := listAll()
	all = append(all, bd.Bead{ID: "ws-task", Kind: contract.KindTask, Status: "closed", Title: "member task", Parent: "ws-ini"})
	swap(t, &listAll, func() ([]bd.Bead, error) { return all, nil })
	got := launchPrompt(t)
	for _, want := range []string{"Ordered: follow-up of member task (phase: not named on Phases)", "Never park it"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}
