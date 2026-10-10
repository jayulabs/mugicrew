package brook

import (
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/promptguard"
)

// stubHarness records the Invocation Launch composes, so the prompt is asserted
// without spawning claude. Mirrors sanji_test's stubHarness.
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

// launch renders Brook's Task session as the TUI does: through WithClosingLine.
func launch(t *testing.T, kind contract.Kind) (*stubHarness, error) {
	t.Helper()
	h := &stubHarness{}
	_, err := Brook{}.Launch(contract.WithClosingLine(stubCtx{h}), contract.Bead{ID: "ws-task", Kind: kind})
	return h, err
}

// Launch(task) is one agent session anchored to the Task (Spec D2).
func TestLaunchTaskComposesSession(t *testing.T) {
	h, err := launch(t, contract.KindTask)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if h.inv.BeadID != "ws-task" {
		t.Errorf("Invocation must anchor to the Task id, got %q", h.inv.BeadID)
	}
}

// Brook owns only task: any other kind is a loud error, as in Franky.
func TestLaunchNonTaskIsLoudError(t *testing.T) {
	for _, k := range contract.Kinds {
		if k == contract.KindTask {
			continue
		}
		if _, err := launch(t, k); err == nil {
			t.Errorf("Launch(%q) must be a loud error, got nil", k)
		}
	}
}

// Task is HITL-only: the loop never runs one (FR19).
func TestRunIsLoudHITLError(t *testing.T) {
	if err := (Brook{}).Run(stubCtx{&stubHarness{}}, contract.Bead{ID: "ws-task", Kind: contract.KindTask}); err == nil {
		t.Fatal("Run of a Task must return a loud error, got nil")
	}
}

// The rendered guide prompt pins the settled behaviour (Spec D3): the
// bead, the closing line, the three markers, the shell-agnostic hidden prompt,
// the pass/stuck/amended comment shapes and the never-record list (FR 32).
func TestTaskPromptPinsGuide(t *testing.T) {
	h, err := launch(t, contract.KindTask)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	p := h.inv.Prompt
	for _, want := range []string{
		"bd show ws-task",
		contract.ClosingLine(),
		"[irreversible]", "[elevated]", "[secret]",
		"bash -c '",
		`bd comments add ws-task "step N (<title>) passed: <check gist> → <value-free observation>"`,
		`bd comments add ws-task "step N (<title>) stuck: <what failed and what was tried>"`,
		`bd comments add ws-task "step N amended: <why>"`,
		"Never record a secret value, token, password, private key, credentialed URL or raw check output",
		"discovered-from:ws-task",
		"bd close ws-task",
		"crew doc task-body.md",
		contract.DraftInstruction("ws-task"),
	} {
		if !strings.Contains(p, want) {
			t.Errorf("Task prompt missing %q", want)
		}
	}
	if strings.Contains(p, "read -p") && !strings.Contains(p, "bash -c 'read") {
		t.Error("a hidden prompt must be wrapped in bash -c, never a bare read -p (breaks under zsh)")
	}
	if strings.Count(p, contract.ClosingLine()) != 1 {
		t.Error("the closing line must appear exactly once")
	}
}

// Spec D6 over the rendered Task prompt: no skill call, no ADR
// citation, no kind-prefixed title, every crew-doc reference resolves.
func TestTaskPromptSkillFree(t *testing.T) {
	got := taskPrompt("mc-x")
	promptguard.Check(t, "task", got)
	if refs := promptguard.CrewDocRefs(got); !slices.Contains(refs, "task-body.md") {
		t.Errorf("Task prompt reads no crew doc task-body.md (refs %q)", refs)
	}
}
