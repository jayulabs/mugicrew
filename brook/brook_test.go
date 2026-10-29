package brook

import (
	"errors"
	"fmt"
	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/jira"
	"log/slog"
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

// launch renders Brook's Errand session as the TUI does: through WithClosingLine.
func launch(t *testing.T, kind contract.Kind) (*stubHarness, error) {
	t.Helper()
	h := &stubHarness{}
	_, err := Brook{}.Launch(contract.WithClosingLine(stubCtx{h}), contract.Bead{ID: "ws-task", Kind: kind})
	return h, err
}

// Launch(errand) is one agent session anchored to the Errand (Spec D2).
func TestLaunchErrandComposesSession(t *testing.T) {
	h, err := launch(t, contract.KindErrand)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if h.inv.BeadID != "ws-task" {
		t.Errorf("Invocation must anchor to the Errand id, got %q", h.inv.BeadID)
	}
}

// Brook owns only errand: any other kind is a loud error, as in Franky.
func TestLaunchNonErrandIsLoudError(t *testing.T) {
	for _, k := range contract.Kinds {
		if k == contract.KindErrand {
			continue
		}
		if _, err := launch(t, k); err == nil {
			t.Errorf("Launch(%q) must be a loud error, got nil", k)
		}
	}
}

// Errand is HITL-only: the loop never runs one (FR19).
func TestRunIsLoudHITLError(t *testing.T) {
	if err := (Brook{}).Run(stubCtx{&stubHarness{}}, contract.Bead{ID: "ws-errand", Kind: contract.KindErrand}); err == nil {
		t.Fatal("Run of an Errand must return a loud error, got nil")
	}
}

// Spec D1: the guide closes an Errand completed, and an Errand Go already
// closed is done at resume (step 0) and at Done (re-read, no second close).
func TestErrandPromptClosesCompletedAndHandlesClosed(t *testing.T) {
	h, err := launch(t, contract.KindErrand)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	p := h.inv.Prompt
	for _, want := range []string{
		"bd close ws-task --reason completed",
		"0. Run `bd show ws-task` first. If the Errand is already closed",
		"say no step needs walking, and end the session\n   without writing any record",
		"re-read it with `bd show ws-task`. If it is already closed, say\n   so, name its close reason",
		"do not close it again",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("Errand prompt missing %q", want)
		}
	}
}

// The rendered guide prompt pins the settled behaviour (Spec D3): the
// bead, the closing line, the three markers, the shell-agnostic hidden prompt,
// the pass/stuck/amended comment shapes and the never-record list (FR 32).
func TestErrandPromptPinsGuide(t *testing.T) {
	h, err := launch(t, contract.KindErrand)
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
		"filed with\n--order none",
		"bd close ws-task",
		"crew doc errand-body.md",
		"`r` on that Chart's or Checkpoint's row in the TUI",
		"Never run the mugicrew rebase verb yourself",
		"never merge or rebase a container branch by hand",
		"hand off to the Stuck exit",
		"merge a merge request on the Forge",
		contract.DraftInstruction("ws-task"),
		"mugicrew pivot raise ws-task",
		"git diff main...checkpoint/<id>", "git show checkpoint/<id>:<path>",
		"unsure: Signal",
		"Raised by Errand ws-task, step N (<title>)", "Unblocks Errand ws-task step N",
		"*raise*", "*file an offshoot Signal instead*", "*neither*",
		"freezes the whole Checkpoint",
		"raised pivot <id> + Critique <id> on Checkpoint <id>, wait for Critique <id> to close",
		"never both",
		"bd dep list <cp> --direction=down -t blocks --json",
		"names the SAME FR", "--merge-into <pivot> <evidence-file> --critique <file>",
		"raised Critique <id> merged into pivot <id> on Checkpoint <id>, wait for Critique <id> to close",
		"enter on its row resumes it",
		"pivot <id> dismissed", "amended:",
		"`X` in the TUI, or `mugicrew withdraw ws-task`, never a hand\n`bd close`",
		"withdrawing the Errand releases that Checkpoint's land",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("Errand prompt missing %q", want)
		}
	}
	if strings.Contains(p, "read -p") && !strings.Contains(p, "bash -c 'read") {
		t.Error("a hidden prompt must be wrapped in bash -c, never a bare read -p (breaks under zsh)")
	}
	if strings.Count(p, contract.ClosingLine()) != 1 {
		t.Error("the closing line must appear exactly once")
	}
}

// Spec D6 over the rendered Errand prompt: no skill call, no ADR
// citation, no kind-prefixed title, every crew-doc reference resolves.
func TestErrandPromptSkillFree(t *testing.T) {
	got := errandPrompt("mc-x")
	promptguard.Check(t, "task", got)
	if refs := promptguard.CrewDocRefs(got); !slices.Contains(refs, "errand-body.md") {
		t.Errorf("Errand prompt reads no crew doc errand-body.md (refs %q)", refs)
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
	if err != nil || len(pub) != 1 || pub[0] != "completed: jira ABC-1 reached Closed (Done)" {
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
	err, pub, direct, _ := latchRun(t, watches, true, jira.Item{}, errors.New("boom"))
	if err == nil || len(pub)+len(direct) != 0 {
		t.Fatalf("err=%v published=%q direct=%q", err, pub, direct)
	}
	for _, want := range []string{"jira ABC-1 could not be read", "boom", "gone", "forbidden", "unreachable", "mugicrew withdraw ws-l"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
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
	for _, k := range []contract.Kind{contract.KindErrand, contract.KindBuild} {
		if err := (Brook{}).Run(stubCtx{}, contract.Bead{ID: "x", Kind: k}); err == nil {
			t.Errorf("Run(%q) must error", k)
		}
	}
}

const mrWatches = "Watches: mr gitlab.com/grp/sub/proj!7\nhttps://gitlab.com/grp/sub/proj/-/merge_requests/7\n\ntitle"

// mrRun drives Run over an MR latch carrying labels; it returns the Run error, the
// published closes and the pause/unpause calls ("pause unmet", "unpause unmet").
func mrRun(t *testing.T, openBlocked bool, state string, rerr error, labels ...string) (err error, published, writes []string, reads int) {
	t.Helper()
	latchRun(t, mrWatches, openBlocked, jira.Item{}, nil) // installs the bd fakes
	oPause, oUnpause, oRead := pauseBead, unpauseBead, readMR
	t.Cleanup(func() { pauseBead, unpauseBead, readMR = oPause, oUnpause, oRead })
	pauseBead = func(id, c string) error { writes = append(writes, "pause "+c); return nil }
	unpauseBead = func(id, c string) error { writes = append(writes, "unpause "+c); return nil }
	showBead = func(id string) (bd.Bead, error) {
		return bd.Bead{ID: id, Kind: contract.KindLatch, Description: mrWatches, Labels: labels}, nil
	}
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
	boom := errors.New("boom")
	for _, tc := range []struct {
		name       string
		state      string
		rerr       error
		labels     []string
		wantErr    string
		wantClosed []string
		wantWrites []string
		wantCause  string // cause= on the event
		wantEvent  string // effort.paused / effort.resumed
		wantAttrs  []string
	}{
		{name: "merged", state: forge.StateMerged, wantClosed: []string{"completed: mr grp/sub/proj!7 merged"}},
		{name: "opened", state: forge.StateOpened},
		{name: "opened lifts unmet", state: forge.StateOpened, labels: []string{"paused:unmet"}, wantWrites: []string{"unpause unmet"}, wantCause: "unmet", wantEvent: "effort.resumed"},
		{name: "closed pauses unmet", state: forge.StateClosed, wantWrites: []string{"pause unmet"}, wantCause: "unmet", wantEvent: "effort.paused", wantAttrs: []string{"grp/sub/proj!7", "state=closed"}},
		{name: "not found pauses unmet", rerr: fmt.Errorf("get: %w", forge.ErrNotFound), wantWrites: []string{"pause unmet"}, wantCause: "unmet", wantEvent: "effort.paused", wantAttrs: []string{"grp/sub/proj!7", "state=not-found"}},
		{name: "other state", state: "locked", wantErr: "unexpected state"},
		{name: "other read error", rerr: boom, wantErr: "boom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evs := captureEvents(t)
			err, pub, writes, _ := mrRun(t, true, tc.state, tc.rerr, tc.labels...)
			if tc.wantErr == "" && err != nil || tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("err=%v, want %q", err, tc.wantErr)
			}
			if !slices.Equal(pub, tc.wantClosed) || !slices.Equal(writes, tc.wantWrites) {
				t.Fatalf("closed=%q writes=%q, want %q %q", pub, writes, tc.wantClosed, tc.wantWrites)
			}
			if tc.wantCause != "" && !strings.Contains(evs.String(), "cause="+tc.wantCause) {
				t.Errorf("events %q lack cause=%s", evs.String(), tc.wantCause)
			}
			if tc.wantEvent != "" && !strings.Contains(evs.String(), tc.wantEvent) {
				t.Errorf("events %q lack %s", evs.String(), tc.wantEvent)
			}
			for _, w := range tc.wantAttrs {
				if !strings.Contains(evs.String(), w) {
					t.Errorf("events %q lack %q", evs.String(), w)
				}
			}
		})
	}
	err, pub, _, reads := mrRun(t, false, forge.StateMerged, nil)
	if err != nil || reads != 0 || len(pub) != 1 || pub[0] != "orphaned: no open bead waits on mr grp/sub/proj!7" {
		t.Fatalf("orphan: %v %v %d", err, pub, reads)
	}
}

// captureEvents routes slog to the returned buffer for the test.
func captureEvents(t *testing.T) *strings.Builder {
	t.Helper()
	buf := &strings.Builder{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return buf
}

func TestWatchesParse(t *testing.T) {
	if s, i, err := Watches(mrWatches); err != nil || s != "mr" || i != "gitlab.com/grp/sub/proj!7" {
		t.Fatalf("%s %s %v", s, i, err)
	}
}
