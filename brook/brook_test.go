package brook

import (
	"errors"
	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/jira"
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
func (h *stubHarness) ResumeCommand(string, bool) (contract.AgentCommand, error) {
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
		"signal-filing.md",
		"bd close ws-task",
		"crew doc task-body.md",
		"`r` on that Chart's or Checkpoint's row in the TUI",
		"Never run the mugicrew rebase verb yourself",
		"never merge or rebase a container branch by hand",
		"hand off to the Stuck exit",
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

type pubCtx struct {
	stubCtx
	closed *[]string
}

func (p pubCtx) CloseOnPublish(r string)     { *p.closed = append(*p.closed, r) }
func (p pubCtx) EdgeOnPublish(contract.Edge) {}

// latchRun drives Run over a latch with fakes for the bd reads, the Jira read and
// the direct close; it returns the Run error, the Publisher closes and direct closes.
func latchRun(t *testing.T, body string, openBlocked bool, item jira.Item, jerr error) (err error, published, direct []string, reads int) {
	t.Helper()
	oShow, oOpen, oSite, oRead, oClose := showBead, hasOpenBlocked, jiraSite, readJira, closeBead
	t.Cleanup(func() { showBead, hasOpenBlocked, jiraSite, readJira, closeBead = oShow, oOpen, oSite, oRead, oClose })
	showBead = func(id string) (bd.Bead, error) {
		return bd.Bead{ID: id, Kind: contract.KindLatch, Title: "Jira ABC-1 is done", Description: body}, nil
	}
	hasOpenBlocked = func(string) (bool, error) { return openBlocked, nil }
	jiraSite = func() string { return "acme.atlassian.net" }
	readJira = func(key, site string) (jira.Item, error) { reads++; return item, jerr }
	closeBead = func(id, r string) error { direct = append(direct, r); return nil }
	err = Brook{}.Run(pubCtx{closed: &published}, contract.Bead{ID: "ws-l", Kind: contract.KindLatch})
	return
}

const watches = "Watches: jira ABC-1\nhttps://acme.atlassian.net/browse/ABC-1\n\nsummary"

func TestLatchOrphanClosesWithoutJiraRead(t *testing.T) {
	err, pub, _, reads := latchRun(t, watches, false, jira.Item{}, nil)
	if err != nil || reads != 0 || len(pub) != 1 || pub[0] != "orphaned: no open bead waits on jira ABC-1" {
		t.Fatalf("err=%v reads=%d published=%q", err, reads, pub)
	}
}

func TestLatchDoneClosesWithReason(t *testing.T) {
	err, pub, _, _ := latchRun(t, watches, true, jira.Item{Key: "ABC-1", Status: "Closed", Category: "done"}, nil)
	if err != nil || len(pub) != 1 || pub[0] != "jira ABC-1 reached Closed (Done)" {
		t.Fatalf("err=%v published=%q", err, pub)
	}
}

func TestLatchNotDoneStaysOpen(t *testing.T) {
	err, pub, direct, reads := latchRun(t, watches, true, jira.Item{Status: "In Progress", Category: "indeterminate"}, nil)
	if err != nil || reads != 1 || len(pub)+len(direct) != 0 {
		t.Fatalf("err=%v reads=%d published=%q direct=%q", err, reads, pub, direct)
	}
}

func TestLatchReadErrorIsReturned(t *testing.T) {
	err, pub, _, _ := latchRun(t, watches, true, jira.Item{}, errors.New("boom"))
	if err == nil || len(pub) != 0 {
		t.Fatalf("err=%v published=%q", err, pub)
	}
}

func TestLatchBadWatchesLineIsError(t *testing.T) {
	for _, body := range []string{"", "no watches here", "Watches: github ABC-1", "Watches: jira"} {
		if err, _, _, reads := latchRun(t, body, true, jira.Item{}, nil); err == nil || reads != 0 {
			t.Errorf("body %q: err=%v reads=%d", body, err, reads)
		}
	}
}

func TestLatchClosesDirectlyWithoutPublisher(t *testing.T) {
	latchRun(t, watches, true, jira.Item{}, nil) // installs the fakes
	var direct []string
	closeBead = func(id, r string) error { direct = append(direct, r); return nil }
	readJira = func(string, string) (jira.Item, error) { return jira.Item{Status: "Done", Category: "done"}, nil }
	if err := (Brook{}).Run(stubCtx{}, contract.Bead{ID: "ws-l", Kind: contract.KindLatch}); err != nil || len(direct) != 1 {
		t.Fatalf("err=%v direct=%q", err, direct)
	}
}

func TestRunStillErrorsForOtherKinds(t *testing.T) {
	for _, k := range []contract.Kind{contract.KindTask, contract.KindBuild} {
		if err := (Brook{}).Run(stubCtx{}, contract.Bead{ID: "x", Kind: k}); err == nil {
			t.Errorf("Run(%q) must error", k)
		}
	}
}

const mrWatches = "Watches: mr gitlab.com/grp/sub/proj!7\nhttps://gitlab.com/grp/sub/proj/-/merge_requests/7\n\ntitle"

func mrRun(t *testing.T, openBlocked bool, state string, rerr error) (err error, published []string, reads int) {
	t.Helper()
	latchRun(t, mrWatches, openBlocked, jira.Item{}, nil) // installs the bd fakes
	showBead = func(id string) (bd.Bead, error) { return bd.Bead{ID: id, Description: mrWatches}, nil }
	readMR = func(p forge.Project, iid int) (*forge.MR, error) {
		reads++
		if p.Path() != "grp/sub/proj" || iid != 7 {
			t.Fatalf("read %v!%d", p, iid)
		}
		return &forge.MR{State: state}, rerr
	}
	err = Brook{}.Run(pubCtx{closed: &published}, contract.Bead{ID: "ws-l", Kind: contract.KindLatch})
	return
}

func TestMRLatch(t *testing.T) {
	err, pub, _ := mrRun(t, true, forge.StateMerged, nil)
	if err != nil || len(pub) != 1 || pub[0] != "mr grp/sub/proj!7 merged" {
		t.Fatalf("merged: %v %v", err, pub)
	}
	if err, pub, _ = mrRun(t, true, forge.StateOpened, nil); err != nil || len(pub) != 0 {
		t.Fatalf("opened: %v %v", err, pub)
	}
	if err, pub, _ = mrRun(t, true, forge.StateClosed, nil); err == nil || !strings.Contains(err.Error(), "closed") || len(pub) != 0 {
		t.Fatalf("closed: %v %v", err, pub)
	}
	if err, _, _ = mrRun(t, true, "locked", nil); err == nil {
		t.Fatal("other state must error")
	}
	if err, _, _ = mrRun(t, true, "", errors.New("boom")); err == nil || err.Error() != "boom" {
		t.Fatalf("read error: %v", err)
	}
	err, pub, reads := mrRun(t, false, forge.StateMerged, nil)
	if err != nil || reads != 0 || len(pub) != 1 || pub[0] != "orphaned: no open bead waits on mr grp/sub/proj!7" {
		t.Fatalf("orphan: %v %v %d", err, pub, reads)
	}
}

func TestWatchesParse(t *testing.T) {
	if s, i, err := Watches(mrWatches); err != nil || s != "mr" || i != "gitlab.com/grp/sub/proj!7" {
		t.Fatalf("%s %s %v", s, i, err)
	}
}
