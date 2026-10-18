package chopper

import (
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/promptguard"
	"jayulabs.org/mugicrew/session"
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
	h := &stubHarness{}
	_, err := Chopper{}.Launch(contract.WithClosingLine(stubCtx{h}), contract.Bead{ID: "ws-chk", Kind: kind})
	return h, err
}

func TestRunIsLoudHITLError(t *testing.T) {
	if err := (Chopper{}).Run(stubCtx{&stubHarness{}}, contract.Bead{ID: "ws-chk", Kind: contract.KindCheckup}); err == nil {
		t.Fatal("Run must return a loud error, got nil")
	}
}

func TestLaunchNonCheckupIsLoudError(t *testing.T) {
	for _, k := range contract.Kinds {
		if k == contract.KindCheckup {
			continue
		}
		if _, err := launch(k); err == nil {
			t.Errorf("Launch(%q) must be a loud error, got nil", k)
		}
	}
}

func TestLaunchCheckupComposesEditableSession(t *testing.T) {
	h, err := launch(contract.KindCheckup)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	p := h.inv.Prompt
	if h.inv.ReadOnly || h.inv.BeadID != "ws-chk" {
		t.Errorf("Invocation = {ReadOnly %v, BeadID %q}, want editable on ws-chk", h.inv.ReadOnly, h.inv.BeadID)
	}
	for _, want := range []string{
		"bd show ws-chk",
		"bd dep list ws-chk -t related",
		"--problem",
		"paused:*",
		".git/mugicrew/events.jsonl.1",
		`grep -hF '"bead":"<target-id>"'`,
		"tail -n 200",
		"--no-follow --no-pager --last",
		"Symptom, Evidence",
		"at most two\ncandidates",
		`explicit "yes"`,
		`bd note <bead> "Checkup ws-chk:`,
		"Never, whatever the human says",
		"mugicrew abort <bead>",
		"offshoot Signal",
		`--reason "repaired:`,
		`--reason "not a bead fault:`,
		"--remove-label paused:checkup",
		contract.DraftInstruction("ws-chk"),
		contract.ClosingLine(),
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	if n := len(p); n > 16<<10 {
		t.Errorf("prompt is %d bytes, over the 16 KiB HITL budget", n)
	}
}

func TestCheckupPromptSkillFree(t *testing.T) {
	promptguard.Check(t, "checkup", checkupPrompt("mc-x"))
}

// CheckupRefusal (ADR 0274 §4) over the host recover reads: one table, each row a
// target's state and the refusal it earns, "" for a launch that goes.
func TestCheckupRefusal(t *testing.T) {
	task := func(status string, labels ...string) bd.Bead {
		return bd.Bead{ID: "mc-a", Title: "Odd", Kind: contract.KindTask, Status: status, Labels: labels}
	}
	titled := func(b bd.Bead) bd.Bead { b.Title = "Odd"; return b }
	live := titled(stranded())
	live.Labels = nil // no abandoned pause: the probe decides
	tests := []struct {
		name string
		w    world
		want string
	}{
		{"live AFK effort", world{bead: live, probe: func(int) []int { return []int{42} }},
			"an effort is running on Odd: abort it (x) first"},
		{"open HITL session", world{bead: task("in_progress"), sess: session.Info{Session: "mc-a", State: session.Alive}},
			"a session is open on Odd"},
		{"exited HITL session", world{bead: task("in_progress"), sess: session.Info{Session: "mc-a", State: session.Exited}},
			"a session is open on Odd"},
		{"dead claim, clean verdict", world{bead: titled(stranded()), tree: "/t"},
			"stranded by a dead effort: recover it (x)"},
		{"dead claim, ambiguous verdict", world{bead: live, loop: looplock.On, tree: "/t"}, ""},
		{"dead claim, refused verdict", world{bead: bd.Bead{ID: "mc-a", Title: "Odd", Kind: "bogus", Status: "in_progress"}}, ""},
		{"a Checkup", world{bead: bd.Bead{ID: "mc-a", Title: "Odd", Kind: contract.KindCheckup, Status: "open"}},
			"mc-a is a Checkup: it has nothing to check up"},
		{"already fenced by an open Checkup", world{bead: task("open", "paused:checkup")}, ""},
		{"closed", world{bead: task("closed")}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := tc.w
			stub(t, &w)
			err := CheckupRefusal(w.bead)
			if got := errString(err); got != tc.want {
				t.Errorf("CheckupRefusal = %q, want %q", got, tc.want)
			}
		})
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
