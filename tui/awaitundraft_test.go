package tui

import (
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/loop"
	"jayulabs.org/mugicrew/registry"
)

const undraftBody = "MR: !7\nMR link: " + mrURL

func undraftBeads() []bd.Bead {
	return []bd.Bead{
		dbead("cp", contract.KindCheckpoint, "open", "", 1, isContainer),
		dbead("wa", contract.KindApproval, "open", "cp", 1, func(b *bd.Bead) { b.Description = undraftBody }),
	}
}

// A waiting Approval is AFK under gitlab, so it is no HITL candidate, yet Execute still lists
// it, reads "awaiting un-draft" and keeps it off the badge (Spec mc-afsi5.3 FR 28).
func TestExecListsAWaitingApprovalAsAwaitingUndraft(t *testing.T) {
	registry.SetGitlab(true)
	t.Cleanup(registry.ResetModeSnapshots)
	g := graphData{beads: undraftBeads()}
	if got := g.leafState(&g.beads[1]); got != sUndraft || leafWords[got] != "awaiting un-draft" {
		t.Fatalf("leafState = %v (%q)", got, leafWords[got])
	}
	rows := g.actionable()
	if len(rows) != 1 || rows[0].b.ID != "wa" {
		t.Fatalf("actionable lists %d rows, want the waiting Approval", len(rows))
	}
	if d := (execTab{g: g}).rowText(rows[0]); !strings.Contains(d, "awaiting un-draft") {
		t.Errorf("detail lacks the awaiting un-draft state:\n%s", d)
	}
	if g.execBadge() != 0 {
		t.Errorf("badge = %d, want 0: the un-draft is on GitLab", g.execBadge())
	}
	if isHITL(&g.beads[1]) {
		t.Error("the waiting Approval is a HITL candidate under gitlab")
	}
}

// An AFK Approval with no MR: line stays out of Actionable.
func TestExecOmitsAnAFKApprovalWithoutAnMRLine(t *testing.T) {
	g := graphData{beads: undraftBeads()}
	g.beads[1].Description = ""
	registry.SetGitlab(true)
	t.Cleanup(registry.ResetModeSnapshots)
	if rows := g.actionable(); len(rows) != 0 {
		t.Errorf("actionable = %d rows, want 0", len(rows))
	}
}

// w opens the waiting Approval's MR link (ADR 0360).
func TestWOpensAWaitingApprovalsMR(t *testing.T) {
	ran := stubLaunch(t, nil)
	t.Setenv("BROWSER", "x")
	stubCommonDir(t, t.TempDir())
	stubActions(t)
	m := sized(t, 160, 40)
	m.landName = "gitlab"
	m, _ = send(t, m, loadedMsg{beads: undraftBeads(), live: map[string]bool{}})
	m = execAt(t, press(t, m, runeKey("3")), "act:wa")
	runW(t, m)
	if len(*ran) != 1 || !strings.Contains(strings.Join((*ran)[0], " "), mrURL) {
		t.Errorf("ran %v, want the MR link opened", *ran)
	}
}

// A waiting fallback Approval (auto-approve:failed plus an MR: line) is still a HITL
// candidate, yet Enter offers no Luffy session and run-now does not say it can launch
// one: the next step is the un-draft on GitLab (Spec mc-afsi5.3 FR 28).
func TestWaitingFallbackApprovalOffersNoHITLSession(t *testing.T) {
	registry.SetGitlab(true)
	t.Cleanup(registry.ResetModeSnapshots)
	bs := undraftBeads()
	bs[1].Labels = []string{registry.AutoApproveFailedLabel}
	g := graphData{beads: bs}
	if !isHITL(&g.beads[1]) {
		t.Fatal("the fallback Approval is not HITL; the test would prove nothing")
	}
	rows := g.actionable()
	if len(rows) != 1 || rows[0].leaf() {
		t.Fatalf("rows = %d, leaf = %v; want the waiting Approval, not launchable", len(rows), len(rows) == 1 && rows[0].leaf())
	}
	if h := causeHint(g.beads[1], loop.Verdict{Cause: loop.CauseNotCandidate}); strings.Contains(h, "press enter to launch") {
		t.Errorf("run-now hint offers a session: %q", h)
	}
}
