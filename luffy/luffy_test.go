package luffy

import (
	"os/exec"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/promptguard"
)

// stubHarness records the Invocation the Launch path composes so we can assert
// the prompt without spawning claude. Mirrors sanji_test's stubHarness.
type stubHarness struct{ inv contract.Invocation }

func (h *stubHarness) Run(contract.Ctx, contract.Invocation) (contract.Result, error) {
	return contract.Result{}, nil
}
func (h *stubHarness) LaunchCommand(inv contract.Invocation) (contract.LaunchCommand, error) {
	h.inv = inv
	return contract.AgentCommand{Argv: []string{"claude", inv.Prompt}}, nil
}

func (h *stubHarness) ResumeCommand(string) (contract.AgentCommand, error) {
	return contract.AgentCommand{}, nil
}

type stubCtx struct{ h contract.Harness }

func (c stubCtx) Harness() contract.Harness { return c.h }

// Launch(approval) → one agent-variety command anchored to the bead, whose prompt
// carries the decision-packet assembly (diff + clean-review evidence + critique
// history), the advisory HEAD-vs-pin staleness note, and the approve-only close
// contract. Pinned so an edit can't silently drop a leg of the contract.
func TestLaunchApprovalComposesSession(t *testing.T) {
	stubGraph(t, "")
	h := &stubHarness{}
	lc, err := Luffy{}.Launch(stubCtx{h}, contract.Bead{ID: "ws-appr", Kind: contract.KindApproval})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if _, ok := lc.(contract.AgentCommand); !ok {
		t.Fatalf("Launch must return the agent variety, got %T", lc)
	}
	if h.inv.BeadID != "ws-appr" {
		t.Errorf("Invocation must anchor to the approval id, got %q", h.inv.BeadID)
	}
	p := h.inv.Prompt
	for _, want := range []string{
		"ws-appr",                               // anchored to the bead
		"git diff main...checkpoint/",           // the diff leg of the packet (FR32)
		"kind:review",                           // clean-review evidence (FR32)
		"kind:critique",                         // critique history / rework trail (FR32)
		"Commit:",                               // the inherited pin the packet is about
		"HEAD advanced since this was reviewed", // advisory staleness, no gate (FR35)
		"bd close ws-appr --reason=approved",    // approve-only close (FR34/FR36)
		"explicit",                              // ... only on the human's explicit OK (FR34)
		"open",                                  // decline = leave it open, fail-safe (FR36)
	} {
		if !strings.Contains(p, want) {
			t.Errorf("approval prompt missing %q", want)
		}
	}
	// The pin is read-only: the prompt must never author a re-pin.
	if strings.Contains(p, "CommitPin") || strings.Contains(p, "--notes") {
		t.Errorf("approval prompt must not alter the inherited pin: %q", p)
	}
}

// Spec slice 3: on a Workspace whose trunk is not main, the packet's
// diff commands name the detected mainline, and no {{…}} token is left unfilled.
func TestLaunchApprovalNamesDetectedMainline(t *testing.T) {
	t.Chdir(t.TempDir())
	stubGraph(t, "")
	for _, args := range [][]string{{"init", "-q"}, {"symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	h := &stubHarness{}
	if _, err := (Luffy{}).Launch(stubCtx{h}, contract.Bead{ID: "ws-appr", Kind: contract.KindApproval}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	p := h.inv.Prompt
	for _, want := range []string{"git diff --stat trunk...checkpoint/", "git diff trunk...checkpoint/"} {
		if !strings.Contains(p, want) {
			t.Errorf("approval prompt missing %q", want)
		}
	}
	if strings.Contains(p, "main...") || strings.Contains(p, "{{") {
		t.Errorf("approval prompt names main or keeps a token:\n%s", p)
	}
}

// A non-approval kind is not launchable — Launch is a loud error, mirroring
// Sanji's Launch default arm.
func TestLaunchNonApprovalIsLoudError(t *testing.T) {
	if _, err := (Luffy{}).Launch(stubCtx{&stubHarness{}}, contract.Bead{ID: "x", Kind: contract.KindReview}); err == nil {
		t.Fatal("Launch of a non-approval kind must return a loud error, got nil")
	}
}

// approval is HITL, so the loop must never dispatch it: Run is a loud error, not
// loop work (FR33), mirroring Sanji's loud Run default.
func TestRunIsLoudHITLError(t *testing.T) {
	if err := (Luffy{}).Run(stubCtx{&stubHarness{}}, contract.Bead{ID: "ws-appr", Kind: contract.KindApproval}); err == nil {
		t.Fatal("Run of a HITL kind must return a loud error, got nil")
	}
}

// The prompt renders with the bead id and carries no stray % that fmt.Sprintf
// would turn into %!(...) — guards a broken embed or a literal %.
func TestPromptRenders(t *testing.T) {
	p := approvalPrompt("ws-appr", bd.Bead{})
	if !strings.Contains(p, "ws-appr") {
		t.Errorf("approval prompt missing the bead id")
	}
	if strings.Contains(p, "%!") {
		t.Errorf("approval prompt has a stray %%: %q", p)
	}
}

// Spec D6 over the approval prompt as rendered: no skill call, no ADR
// citation, every crew-doc reference resolves, and none of the shared stance the
// Crewmate preamble now carries is repeated.
func TestApprovalPromptSkillFree(t *testing.T) {
	p := approvalPrompt("ws-appr", bd.Bead{})
	promptguard.Check(t, "approval", p)
	for _, dup := range []string{"by their title", "bare id", "authorization, not authorship"} {
		if strings.Contains(p, dup) {
			t.Errorf("approval prompt repeats preamble text %q", dup)
		}
	}
}

// stubGraph stands in for the bd reads Launch makes: the Approval's parent is
// ws-cp, and surgery (if non-empty) is ws-cp's Surgery, titled "tint the footer".
func stubGraph(t *testing.T, surgery string) {
	t.Helper()
	oShow, oSurgery := show, surgeryOf
	t.Cleanup(func() { show, surgeryOf = oShow, oSurgery })
	show = func(id string) (bd.Bead, error) {
		if id == surgery {
			return bd.Bead{ID: id, Kind: contract.KindSurgery, Title: "tint the footer"}, nil
		}
		return bd.Bead{ID: id, Kind: contract.KindApproval, Parent: "ws-cp"}, nil
	}
	surgeryOf = func(cp string) (string, bool, error) {
		if cp != "ws-cp" {
			t.Errorf("SurgeryOf(%q), want the Approval's parent ws-cp", cp)
		}
		return surgery, surgery != "", nil
	}
}

// Spec D8 (FR 39): a surgery Checkpoint's packet names its Surgery; any
// other Checkpoint's packet is unchanged.
func TestLaunchApprovalNamesSurgery(t *testing.T) {
	launch := func(surgery string) string {
		stubGraph(t, surgery)
		h := &stubHarness{}
		if _, err := (Luffy{}).Launch(stubCtx{h}, contract.Bead{ID: "ws-appr", Kind: contract.KindApproval}); err != nil {
			t.Fatalf("Launch: %v", err)
		}
		return h.inv.Prompt
	}
	if p := launch("ws-surg"); !strings.Contains(p, "Surgery: ws-surg — tint the footer") {
		t.Errorf("surgery packet missing the Surgery line:\n%s", p)
	}
	if p := launch(""); strings.Contains(p, "Surgery") || strings.Contains(p, "%!") {
		t.Errorf("a Checkpoint with no Surgery must leave the packet unchanged:\n%s", p)
	}
}
