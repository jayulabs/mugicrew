package robin

import (
	"errors"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/promptguard"
)

type stubHarness struct{ inv contract.Invocation }

func (h *stubHarness) Run(contract.Ctx, contract.Invocation) (contract.Result, error) {
	return contract.Result{}, nil
}
func (h *stubHarness) LaunchCommand(inv contract.Invocation) (contract.LaunchCommand, error) {
	h.inv = inv
	return contract.AgentCommand{Argv: []string{"claude", inv.Prompt}}, nil
}
func (h *stubHarness) ResumeCommand(string, bool) (contract.AgentCommand, error) {
	return contract.AgentCommand{}, nil
}

type stubCtx struct{ h contract.Harness }

func (c stubCtx) Harness() contract.Harness { return c.h }

func launch(kind contract.Kind) (*stubHarness, error) {
	return launchBody(kind, "plain topic", nil)
}

func launchBody(kind contract.Kind, body string, showErr error) (*stubHarness, error) {
	old := showBead
	defer func() { showBead = old }()
	showBead = func(string) (bd.Bead, error) { return bd.Bead{Description: body}, showErr }
	h := &stubHarness{}
	_, err := Robin{}.Launch(contract.WithClosingLine(stubCtx{h}), contract.Bead{ID: "ws-disc", Kind: kind})
	return h, err
}

func TestRunIsLoudHITLError(t *testing.T) {
	if err := (Robin{}).Run(stubCtx{&stubHarness{}}, contract.Bead{ID: "ws-disc", Kind: contract.KindDiscussion}); err == nil {
		t.Fatal("Run must return a loud error, got nil")
	}
}

func TestLaunchNonDiscussionIsLoudError(t *testing.T) {
	for _, k := range contract.Kinds {
		if k == contract.KindDiscussion {
			continue
		}
		if _, err := launch(k); err == nil {
			t.Errorf("Launch(%q) must be a loud error, got nil", k)
		}
	}
}

func TestLaunchDiscussionComposesReadOnlySession(t *testing.T) {
	h, err := launch(contract.KindDiscussion)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	p := h.inv.Prompt
	if !h.inv.ReadOnly || h.inv.BeadID != "ws-disc" {
		t.Errorf("Invocation = {ReadOnly %v, BeadID %q}, want read-only on ws-disc", h.inv.ReadOnly, h.inv.BeadID)
	}
	pre, _, _ := strings.Cut(p, "\n\n---\n\n")
	if pre == "" || !strings.HasPrefix(p, pre) || strings.HasPrefix(pre, "You are Robin") {
		t.Error("prompt must open with the CREWMATE preamble")
	}
	for _, want := range []string{
		"bd show ws-disc",
		contract.DraftInstruction("ws-disc"),
		"drafted-by:ws-disc",
		"signal-filing.md",
		"bd update ws-disc --title",
		"Without asking, on a bead labelled drafted-by:ws-disc: bd update --title,\n--description and --notes, and bd delete <id> --force",
		"rewrite the survivor",
		"combine their Sources lines",
		"bd delete <id> --force on the rest",
		"Report every edit, merge and discard in chat, naming the Draft by title",
		"bd create for a new Signal Draft only on the human's explicit yes",
		"changing a Draft's status or labels",
		"priority, type or edges",
		"writing to or deleting any bead not labelled drafted-by:ws-disc",
		"bd close ws-disc",
		contract.ClosingLine(),
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	for _, gone := range []string{"only on the human's yes", "propose\nthe edit in chat"} {
		if strings.Contains(p, gone) {
			t.Errorf("prompt still contains %q", gone)
		}
	}
	if n := len(p); n > 16<<10 {
		t.Errorf("prompt is %d bytes, over the 16 KiB HITL budget", n)
	}
}

func TestDiscussionPromptSkillFree(t *testing.T) {
	promptguard.Check(t, "discussion", discussionPrompt("mc-x"))
}

func TestLaunchPlainBodyIsTheDiscussionPrompt(t *testing.T) {
	for _, body := range []string{"plain topic", "> Target: abc\nSpine: a b"} {
		h, err := launchBody(contract.KindDiscussion, body, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, rest, _ := strings.Cut(h.inv.Prompt, "\n\n---\n\n"); rest != discussionPrompt("ws-disc")+"\n\n"+contract.ClosingLine() || strings.Contains(rest, "Retrospective framing") {
			t.Errorf("body %q: want the plain discussion prompt", body)
		}
	}
	if !strings.Contains(discussionPrompt("x"), "bd list, bd dep list and bd search") {
		t.Error("discussion prompt must name bd search")
	}
}

func TestLaunchRetrospectiveFramesTheDiscussion(t *testing.T) {
	h, err := launchBody(contract.KindDiscussion, "why\nTarget: mc-a\nSpine: mc-a mc-b", nil)
	if err != nil {
		t.Fatal(err)
	}
	p := h.inv.Prompt
	_, rest, _ := strings.Cut(p, "\n\n---\n\n")
	if !strings.HasPrefix(rest, retrospectiveTmpl+"\n"+discussionPrompt("ws-disc")) {
		t.Errorf("prompt must be framing + discussion prompt after the preamble:\n%s", p)
	}
	for _, want := range []string{"bd show", "wc -c", "40 %", "bd search", "Seen once:", "Already on the graph:", "bd close ws-disc"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	if n := len(p); n > 16<<10 {
		t.Errorf("composed prompt is %d bytes, over the 16 KiB HITL budget", n)
	}
}

func TestLaunchRetrospectiveWithoutSpineIsLoud(t *testing.T) {
	for _, body := range []string{"Target: mc-a", "Target: mc-a\nSpine:", "Target: mc-a\nSpine:   "} {
		if _, err := launchBody(contract.KindDiscussion, body, nil); err == nil {
			t.Errorf("body %q: want a loud error", body)
		}
	}
	if _, err := launchBody(contract.KindDiscussion, "", errors.New("bd down")); err == nil {
		t.Error("failed body read must be a loud error")
	}
}

func TestRetrospectiveFramingSkillFree(t *testing.T) {
	promptguard.Check(t, "retrospective", retrospectiveTmpl)
}

func TestRetrospectiveFramingDoesNotAskToSaveOutput(t *testing.T) {
	f := retrospectiveTmpl
	for _, bad := range []string{"saving", "save each", "write each"} {
		if strings.Contains(f, bad) {
			t.Errorf("framing must not tell Robin to write output (read-only): contains %q", bad)
		}
	}
	if !strings.Contains(f, "| wc -c") {
		t.Error("framing must measure by piping each render to wc -c")
	}
}
