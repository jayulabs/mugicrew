package chopper

import (
	"errors"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
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
		`mugicrew checkup close ws-chk --reason "repaired:`,
		`mugicrew checkup close ws-chk --reason "not a bead fault:`,
		"lifts the fence",
		contract.DraftInstruction("ws-chk"),
		contract.ClosingLine(),
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	for _, banned := range []string{"--remove-label paused:checkup", "bd close ws-chk"} {
		if strings.Contains(p, banned) {
			t.Errorf("prompt still carries %q", banned)
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
	errand := func(status string, labels ...string) bd.Bead {
		return bd.Bead{ID: "mc-a", Title: "Odd", Kind: contract.KindErrand, Status: status, Labels: labels}
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
		{"open HITL session", world{bead: errand("in_progress"), sess: session.Info{Session: "mc-a", State: session.Alive}},
			"a session is open on Odd"},
		{"exited HITL session", world{bead: errand("in_progress"), sess: session.Info{Session: "mc-a", State: session.Exited}},
			"a session is open on Odd"},
		{"dead claim, clean verdict", world{bead: titled(stranded()), tree: "/t"},
			"stranded by a dead effort: recover it (x)"},
		{"dead claim, repo-root orphan", world{bead: bd.Bead{ID: "mc-a", Title: "Odd", Kind: contract.KindSpec, Status: "in_progress"}}, ""},
		{"dead claim, refused verdict", world{bead: bd.Bead{ID: "mc-a", Title: "Odd", Kind: "bogus", Status: "in_progress"}}, ""},
		{"a Checkup", world{bead: bd.Bead{ID: "mc-a", Title: "Odd", Kind: contract.KindCheckup, Status: "open"}},
			"mc-a is a Checkup: it has nothing to check up"},
		{"already fenced by an open Checkup", world{bead: errand("open", "paused:checkup")}, ""},
		{"closed", world{bead: errand("closed")}, ""},
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

// closing scripts CloseCheckup's seams: held names the targets another open Checkup holds.
func closing(held map[string]bool, closeErr error) (CheckupClosing, *[]string) {
	var calls []string
	return CheckupClosing{
		Close: func(id, r string) error { calls = append(calls, "close "+id+" "+r); return closeErr },
		OpenOn: func(t string) (bd.Bead, bool, error) {
			return bd.Bead{ID: "other"}, held[t], nil
		},
		Unpause: func(id, cause string) error { calls = append(calls, "unpause "+id+" "+cause); return nil },
	}, &calls
}

func TestCloseCheckup(t *testing.T) {
	ck := bd.Bead{ID: "ck", Related: []string{"a", "b"}}
	for _, c := range []struct {
		name     string
		ck       bd.Bead
		held     map[string]bool
		closeErr error
		want     string
	}{
		{"sole Checkup lifts the fence", bd.Bead{ID: "ck", Related: []string{"a"}}, nil, nil, "close ck repaired: x;unpause a " + bd.PausedCheckup},
		{"a second open Checkup keeps it", bd.Bead{ID: "ck", Related: []string{"a"}}, map[string]bool{"a": true}, nil, "close ck repaired: x"},
		{"only the free target lifts", ck, map[string]bool{"a": true}, nil, "close ck repaired: x;unpause b " + bd.PausedCheckup},
		{"a failed close lifts nothing", ck, nil, errors.New("boom"), "close ck repaired: x"},
	} {
		t.Run(c.name, func(t *testing.T) {
			x, calls := closing(c.held, c.closeErr)
			err := CloseCheckup(x, c.ck, "repaired: x")
			if (err != nil) != (c.closeErr != nil) {
				t.Errorf("err = %v", err)
			}
			if got := strings.Join(*calls, ";"); got != c.want {
				t.Errorf("calls = %q, want %q", got, c.want)
			}
		})
	}
}
