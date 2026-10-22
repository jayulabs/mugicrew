package luffy

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/promptguard"
	"jayulabs.org/mugicrew/registry"
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

func (h *stubHarness) ResumeCommand(string, bool) (contract.AgentCommand, error) {
	return contract.AgentCommand{}, nil
}

type stubCtx struct{ h contract.Harness }

func (c stubCtx) Harness() contract.Harness { return c.h }

// Launch(approval) → one agent-variety command anchored to the bead, whose prompt
// carries the decision-packet assembly (diff + clean-review evidence + critique
// history), the advisory HEAD-vs-pin staleness note, and the approve-only close
// contract. Pinned so an edit can't silently drop a leg of the contract.
func TestLaunchApprovalComposesSession(t *testing.T) {
	stubGraph(t, "", nil)
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
		"crew doc approval-body.md",             // the Decision Packet template
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

// Spec FR10: the description Luffy writes keeps the Commit: pin line verbatim
// and first, then a blank line, then the Decision Packet, in one bd update.
func TestApprovalPromptWritesPinFirstThenPacket(t *testing.T) {
	p := approvalPrompt("ws-appr", false, nil, false)
	for _, want := range []string{"bd update ws-appr --body-file <body-file>", "Commit: <sha>", "verbatim", "blank line", "Decision Packet"} {
		if !strings.Contains(p, want) {
			t.Errorf("approval prompt missing %q", want)
		}
	}
	if i, j := strings.Index(p, "Commit: <sha> line"), strings.Index(p, "blank line"); i < 0 || j < i {
		t.Errorf("prompt must put the Commit: line before the blank line and packet")
	}
}

// Spec mc-dbxb FR1, FR7, FR9, FR10: the approval prompt shows the packet in a
// sibling doc pane and degrades to inline-only.
func TestApprovalPromptShowsPacketInGlowPane(t *testing.T) {
	p := approvalPrompt("ws-appr", false, nil, false)
	if strings.Contains(p, "%!") || strings.Contains(p, "--description") {
		t.Errorf("approval prompt has a format artifact or the inline --description:\n%s", p)
	}
	p = strings.Join(strings.Fields(p), " ")
	for _, want := range []string{
		`tmux -S "${MUGICREW_HITL_PANE%%,*}" split-window -t "${MUGICREW_HITL_PANE#*,}" -h -l 40% -P -F '#{pane_id}' "${MUGICREW_MD_RENDERER:-mugicrew doc-pane} '<body-file>'"`,
		`tmux -S "${MUGICREW_HITL_PANE%%,*}" respawn-pane -k -t <doc-pane-id> "${MUGICREW_MD_RENDERER:-mugicrew doc-pane} '<body-file>'"`,
		"split again as on first presentation",
		"If MUGICREW_HITL_PANE is empty or unset, run no tmux at all",
		"If the renderer is not on PATH, skip the pane and say so in one line naming the renderer you looked for",
		"continue inline-only",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("approval prompt missing %q", want)
		}
	}
}

// Spec slice 3: on a Workspace whose trunk is not main, the packet's
// diff commands name the detected mainline, and no {{…}} token is left unfilled.
func TestLaunchApprovalNamesDetectedMainline(t *testing.T) {
	t.Chdir(t.TempDir())
	stubGraph(t, "", nil)
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

// Any kind but a Signal or an Approval is no Run kind: a loud error.
func TestRunOtherKindIsLoudError(t *testing.T) {
	if err := (Luffy{}).Run(stubCtx{&stubHarness{}}, contract.Bead{ID: "ws-rev", Kind: contract.KindReview}); err == nil {
		t.Fatal("Run of an unowned kind must return a loud error, got nil")
	}
}

const (
	afkPin  = "0123abc"
	afkLine = "auto-approved (AFK, Workspace auto-approve on)"
)

// afkPacket is a written Decision Packet: the pin first, then the sections.
func afkPacket(pin, history string) string {
	return git.CommitPin(pin) + "\n\n## Summary\nbody\n\n### Approval History\nFirst Approval.\n" + history + "\n"
}

// afkRun is the fake bd and harness an Approval Run drives: the Approval starts
// open with only its pin; the session "writes" body, returns out and fails with err.
type afkRun struct {
	body   string // the Approval description after the session
	out    string
	err    error
	closed bool // the Approval is closed before Run
	runs   []contract.Invocation
	closes []string // CloseOnPublish id+" "+reason
}

func stubAFK(t *testing.T, r *afkRun) {
	t.Helper()
	stubGraph(t, "ws-spec", nil)
	swap(t, &show, func(id string) (bd.Bead, error) {
		b := bd.Bead{ID: id, Kind: contract.KindApproval, Parent: "ws-cp", Status: "open", Description: git.CommitPin(afkPin)}
		if r.closed {
			b.Status = "closed"
		}
		if len(r.runs) > 0 {
			b.Description = r.body
		}
		return b, nil
	})
	swap(t, &closeOnPublish, func(_ contract.Ctx, id, reason string, _ []bd.Edge) error {
		r.closes = append(r.closes, id+" "+reason)
		return nil
	})
}

type afkHarness struct{ r *afkRun }

func (h afkHarness) Run(_ contract.Ctx, inv contract.Invocation) (contract.Result, error) {
	h.r.runs = append(h.r.runs, inv)
	return contract.Result{Output: h.r.out}, h.r.err
}
func (afkHarness) LaunchCommand(contract.Invocation) (contract.LaunchCommand, error) { return nil, nil }
func (afkHarness) ResumeCommand(string, bool) (contract.AgentCommand, error) {
	return contract.AgentCommand{}, nil
}

func runAFK(r *afkRun) error {
	stubCtxH := stubCtx{afkHarness{r}}
	return (Luffy{}).Run(stubCtxH, contract.Bead{ID: "ws-appr", Kind: contract.KindApproval})
}

func TestRunApprovalAlreadyClosedDoesNothing(t *testing.T) {
	r := &afkRun{closed: true}
	stubAFK(t, r)
	if err := runAFK(r); err != nil || len(r.runs) != 0 || len(r.closes) != 0 {
		t.Errorf("closed Approval: err %v, runs %d, closes %v; want nothing", err, len(r.runs), r.closes)
	}
}

func TestRunApprovalGoodSessionClosesApproved(t *testing.T) {
	r := &afkRun{body: afkPacket(afkPin, afkLine), out: "writing\nApproved: ships the footer tint\n\n"}
	stubAFK(t, r)
	if err := runAFK(r); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(r.runs) != 1 || r.runs[0].BeadID != "ws-appr" || !strings.Contains(r.runs[0].Prompt, "--body-file") {
		t.Errorf("want one AFK session anchored to the Approval, got %+v", r.runs)
	}
	if len(r.closes) != 1 || r.closes[0] != "ws-appr approved" {
		t.Errorf("closes = %v, want [ws-appr approved]", r.closes)
	}
}

func TestRunApprovalBadSessionErrorsWithoutClose(t *testing.T) {
	good := afkPacket(afkPin, afkLine)
	cases := map[string]afkRun{
		"no outcome line":       {body: good, out: "all done"},
		"empty output":          {body: good},
		"outcome not last":      {body: good, out: "Approved: x\nthanks"},
		"moved pin":             {body: afkPacket("deadbee", afkLine), out: "Approved: x"},
		"pin not first":         {body: "intro\n" + good, out: "Approved: x"},
		"packet unwritten":      {body: git.CommitPin(afkPin), out: "Approved: x"},
		"no history line":       {body: afkPacket(afkPin, "nothing"), out: "Approved: x"},
		"history line not last": {body: good + "### Next\nmore\n", out: "Approved: x"},
		"harness error":         {body: good, out: "Approved: x", err: errors.New("boom")},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := c
			stubAFK(t, &r)
			if err := runAFK(&r); err == nil {
				t.Error("want an error")
			}
			if len(r.closes) != 0 {
				t.Errorf("closes = %v, want none", r.closes)
			}
		})
	}
}

// The AFK prompt takes no claim, opens no pane, has no human gate, never reworks
// or closes; it ends the History and the session on fixed lines.
func TestApprovalPromptAFK(t *testing.T) {
	p := approvalPromptAFK("ws-appr", true, nil)
	for _, want := range []string{
		"Commit:", "git diff main", "HEAD advanced since this was reviewed",
		afkLine, "bd update ws-appr --body-file", "Approved: <one-line gist>",
		contract.DraftInstruction("ws-appr"), "--order",
	} {
		if want == "git diff main" {
			want = "git diff {{MAINLINE}}"
		}
		if !strings.Contains(p, want) {
			t.Errorf("AFK prompt missing %q", want)
		}
	}
	for _, bad := range []string{"--claim", "pane", "bd close", "rework", "explicit", "%!", "%["} {
		if strings.Contains(p, bad) {
			t.Errorf("AFK prompt has %q:\n%s", bad, p)
		}
	}
	promptguard.Check(t, "approval-afk", p)
}

// The HITL prompt renders as it did before the core/tail split.
func TestApprovalPromptHITLUnchanged(t *testing.T) {
	for file, got := range map[string]string{
		"testdata/approval_plain.txt":   approvalPrompt("ws-appr", false, nil, false),
		"testdata/approval_surgery.txt": approvalPrompt("ws-appr", true, []bd.Bead{{ID: "s1", Title: "T"}}, false),
	} {
		want, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Errorf("%s: HITL approval prompt changed", file)
		}
	}
}

// Run on a Signal is the reconcile verb's re-entry: it re-reads the bead and
// reconciles it, spawning the one Chart.
func TestRunSignalReconciles(t *testing.T) {
	g := stubSignalSeams(t)
	swap(t, &show, func(string) (bd.Bead, error) { return openSignal(), nil })
	if err := (Luffy{}).Run(stubCtx{&stubHarness{}}, contract.Bead{ID: "ws-sig", Kind: contract.KindSignal}); err != nil {
		t.Fatalf("Run of a Signal: %v", err)
	}
	if g.spawns != 1 {
		t.Errorf("Run of a Signal spawned %d successors, want the one Chart", g.spawns)
	}
}

// Run reconciles through its receiver, so reconcile --context reaches the Chart.
func TestRunCarriesContextToChart(t *testing.T) {
	declare(t, map[string]string{"api": "apps/api/"})
	g := stubSignalSeams(t)
	swap(t, &show, func(string) (bd.Bead, error) { return openSignal(), nil })
	if err := (Luffy{Context: "api"}).Run(stubCtx{&stubHarness{}}, contract.Bead{ID: "ws-sig", Kind: contract.KindSignal}); err != nil {
		t.Fatal(err)
	}
	if got := g.bodies["ws-chart"].Context; got != "api" {
		t.Errorf("Chart context = %q, want api", got)
	}
}

// The prompt renders with the bead id and carries no stray % that fmt.Sprintf
// would turn into %!(...) — guards a broken embed or a literal %.
func TestPromptRenders(t *testing.T) {
	p := approvalPrompt("ws-appr", false, nil, false)
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
	p := approvalPrompt("ws-appr", false, nil, false)
	promptguard.Check(t, "approval", p)
	for _, dup := range []string{"by their title", "bare id", "authorization, not authorship"} {
		if strings.Contains(p, dup) {
			t.Errorf("approval prompt repeats preamble text %q", dup)
		}
	}
}

// stubGraph stands in for the bd reads Launch makes: the Approval's parent is
// ws-cp, which serves spec (if non-empty) and holds the given Surgeries.
func stubGraph(t *testing.T, spec string, surgeries []bd.Bead) {
	t.Helper()
	oShow, oSpec, oSurgeries := show, specOf, surgeriesOf
	t.Cleanup(func() { show, specOf, surgeriesOf = oShow, oSpec, oSurgeries })
	show = func(id string) (bd.Bead, error) {
		return bd.Bead{ID: id, Kind: contract.KindApproval, Parent: "ws-cp"}, nil
	}
	specOf = func(cp string) (string, bool, error) {
		if cp != "ws-cp" {
			t.Errorf("SpecOf(%q), want the Approval's parent ws-cp", cp)
		}
		return spec, spec != "", nil
	}
	surgeriesOf = func(cp string) ([]bd.Bead, error) {
		if cp != "ws-cp" {
			t.Errorf("SurgeriesOf(%q), want the Approval's parent ws-cp", cp)
		}
		return surgeries, nil
	}
}

// Spec D7 (FR 38): the packet carries one Surgery line per Surgery, in filing
// order; none leaves it unchanged.
func TestLaunchApprovalNamesSurgeries(t *testing.T) {
	one := []bd.Bead{{ID: "ws-s1", Title: "tint the footer"}}
	two := append(one, bd.Bead{ID: "ws-s2", Title: "widen the gutter"})
	launch := func(spec string, surgeries []bd.Bead) string {
		stubGraph(t, spec, surgeries)
		h := &stubHarness{}
		if _, err := (Luffy{}).Launch(stubCtx{h}, contract.Bead{ID: "ws-appr", Kind: contract.KindApproval}); err != nil {
			t.Fatalf("Launch: %v", err)
		}
		return h.inv.Prompt
	}
	l1, l2 := "Surgery: ws-s1 — tint the footer", "Surgery: ws-s2 — widen the gutter"

	if p := launch("", one); !strings.Contains(p, "open the packet with") || !strings.Contains(p, l1) {
		t.Errorf("surgery Checkpoint, one Surgery: want the line opening the packet:\n%s", p)
	}
	p := launch("", two)
	if i, j := strings.Index(p, l1), strings.Index(p, l2); i < 0 || j < i {
		t.Errorf("surgery Checkpoint, two Surgeries: want both lines in filing order:\n%s", p)
	}
	p = launch("ws-spec", two)
	h, i, j := strings.Index(p, "Surgeries on this Checkpoint:"), strings.Index(p, l1), strings.Index(p, l2)
	if h < 0 || i < h || j < i {
		t.Errorf("Spec'd Checkpoint: want the lines under the heading in order:\n%s", p)
	}
	if strings.Contains(p, "open the packet with") {
		t.Errorf("Spec'd Checkpoint: lines follow the Spec, they do not open the packet:\n%s", p)
	}
	for _, spec := range []string{"", "ws-spec"} {
		if p := launch(spec, nil); strings.Contains(p, "so an approver who did not dictate the code reads its intent.") || strings.Contains(p, "%!") {
			t.Errorf("no Surgeries (spec %q) must leave the packet unchanged:\n%s", spec, p)
		}
	}
}

// Launch on an Initiative routes to the cut session (Spec D8, FR20–28): read the
// drained phase against Done when; on the human's "done" close it, final; else
// file the next member Signal as a Draft child drafted-by the Initiative, with an
// explicit yes for a phase outside Done when; mark Phases, add Notes; name the
// owning Chart on an early cut; never parent defog to it nor edit a Chart.
func TestLaunchInitiativeComposesCutSession(t *testing.T) {
	stubInitiativeBody(t, "## Done when\n- x\n")
	h := &stubHarness{}
	if _, err := (Luffy{}).Launch(stubCtx{h}, contract.Bead{ID: "ws-ini", Kind: contract.KindInitiative}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	p := h.inv.Prompt
	if h.inv.BeadID != "ws-ini" {
		t.Errorf("Invocation must anchor to the Initiative, got %q", h.inv.BeadID)
	}
	for _, want := range []string{
		"cut session",
		"## Done when",
		`"done"`,
		"bd close ws-ini",
		"never reopened",
		"crew doc signal-filing.md",
		"Initiative member form",
		"explicit yes",
		"## Phases",
		"## Notes",
		"early cut",
		"owns",
		"Never parent a defog bead",
		"Never edit a Chart",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("cut-session prompt missing %q", want)
		}
	}
	for _, bad := range append(firstBodyAnchors, "one line at a time") {
		if strings.Contains(p, bad) {
			t.Errorf("bodied cut prompt carries %q", bad)
		}
	}
	promptguard.Check(t, "cut", p)
	if strings.Contains(p, "%!") {
		t.Errorf("cut-session prompt has a stray %%:\n%s", p)
	}
}

var firstBodyAnchors = []string{"no body yet", "cancelled: <why>", "write nothing"}

func stubInitiativeBody(t *testing.T, body string) {
	swap(t, &show, func(id string) (bd.Bead, error) {
		return bd.Bead{ID: id, Kind: contract.KindInitiative, Description: body}, nil
	})
}

// A bodyless Initiative's cut session settles Done when, Phases, Notes, writes
// the body before any member, then offers phase 1.
func TestLaunchBodylessInitiativeSettlesBodyFirst(t *testing.T) {
	stubInitiativeBody(t, "just a seed")
	h := &stubHarness{}
	if _, err := (Luffy{}).Launch(stubCtx{h}, contract.Bead{ID: "ws-ini", Kind: contract.KindInitiative}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	p := h.inv.Prompt
	for _, want := range firstBodyAnchors {
		if !strings.Contains(p, want) {
			t.Errorf("first-body prompt missing %q", want)
		}
	}
	first := strings.Index(p, "no body yet")
	done, phases, notes := strings.Index(p[first:], "Done when"), strings.Index(p[first:], "Phases"), strings.Index(p[first:], "Notes")
	if !(0 <= done && done < phases && phases < notes) {
		t.Errorf("order must be Done when, Phases, Notes: %d %d %d", done, phases, notes)
	}
	if body, drained := strings.Index(p, "before filing any member"), strings.Index(p, "## Read the drained"); body < 0 || body > drained {
		t.Errorf("body write must precede members, inside the first-body block")
	}
	if !strings.Contains(p, "bd update ws-ini --description") {
		t.Error("body write must name the id before the --description flag")
	}
	if strings.Contains(p, "one line at a time") {
		t.Error("cut prompt still says one line at a time")
	}
	if !strings.Contains(p, "offer phase 1") {
		t.Error("first-body prompt must offer phase 1 after the body")
	}
	promptguard.Check(t, "cut", p)
	if strings.Contains(p, "%!") {
		t.Errorf("stray %%:\n%s", p)
	}
}

func TestLaunchInitiativeFailsOnShowError(t *testing.T) {
	swap(t, &show, func(string) (bd.Bead, error) { return bd.Bead{}, errors.New("read failed") })
	if _, err := (Luffy{}).Launch(stubCtx{&stubHarness{}}, contract.Bead{ID: "ws-ini", Kind: contract.KindInitiative}); err == nil {
		t.Error("a failed body read must fail the launch")
	}
}

// Approval's focus line is the labelled Checkpoint's alone; a no-context
// Workspace never reads the container (ADR 0157).
func TestLaunchApprovalFocus(t *testing.T) {
	launch := func(label string, noShow bool) string {
		stubGraph(t, "", nil)
		inner := show
		show = func(id string) (bd.Bead, error) {
			if id == "ws-cp" {
				if noShow {
					t.Fatal("bd show for the container on a no-context Workspace")
				}
				return bd.Bead{ID: id, Labels: []string{label}}, nil
			}
			return inner(id)
		}
		h := &stubHarness{}
		if _, err := (Luffy{}).Launch(stubCtx{h}, contract.Bead{ID: "ws-appr", Kind: contract.KindApproval}); err != nil {
			t.Fatal(err)
		}
		return h.inv.Prompt
	}
	repo := func(cfg string) {
		d := t.TempDir()
		exec.Command("git", "init", "-q", d).Run()
		os.MkdirAll(d+"/.mugicrew", 0o755)
		os.WriteFile(d+"/.mugicrew/config.toml", []byte(cfg), 0o644)
		t.Chdir(d)
	}
	repo("")
	plain := launch("context:a", true)
	repo("[context.a]\npath = \"apps/a\"\n")
	if got := launch("kind:checkpoint", false); got != plain {
		t.Error("unlabelled container changed the prompt")
	}
	if got := launch("context:a", false); got != plain+"\n\n"+contexts.FocusLine(map[string]string{"a": "apps/a"}, bd.Bead{Labels: []string{"context:a"}}) {
		t.Errorf("labelled prompt is not old + focus line")
	}
}

// Spec FR11, FR12: the Approval may raise an offshoot Signal through
// the crew doc, and rework goes only through the verb.
func TestApprovalPromptRaisesSignalThroughCrewDoc(t *testing.T) {
	p := strings.Join(strings.Fields(approvalPrompt("ws-appr", false, nil, false)), " ")
	for _, want := range []string{"signal-filing.md", "discovered-from the Approval", "except raising an offshoot Signal on the human's yes", "mugicrew approval rework"} {
		if !strings.Contains(p, want) {
			t.Errorf("approval prompt missing %q", want)
		}
	}
}

func TestCutPromptCrossRepoSplit(t *testing.T) {
	swap(t, &repoContexts, func() map[string]string { return nil })
	if got, want := cutPrompt("mc-x", false), fmt.Sprintf(cutTmpl, "mc-x", contract.DraftInstruction("mc-x"), "", ""); !strings.HasPrefix(got, want) || strings.Contains(got, "Cross-repo") || got != want+placingMembers("mc-x") {
		t.Fatal("cut prompt with no repo contexts must carry no split slot")
	}
	swap(t, &repoContexts, func() map[string]string { return map[string]string{"api": "proj-a"} })
	got := cutPrompt("mc-x", false)
	for _, w := range []string{crossRepoSplit, "no edge between Charts"} {
		if !strings.Contains(got, w) {
			t.Errorf("cut prompt with repo contexts lacks %q", w)
		}
	}
}

// Spec D7: the appended section is present with or without a body or repo contexts.
func TestCutPromptPlacingMembers(t *testing.T) {
	for _, ctxs := range []map[string]string{nil, {"api": "proj-a"}} {
		swap(t, &repoContexts, func() map[string]string { return ctxs })
		for _, bodyless := range []bool{false, true} {
			got := strings.Join(strings.Fields(cutPrompt("mc-x", bodyless)), " ")
			for _, w := range []string{"## Placing members", "bd list --parent mc-x --all", "each adopted unclosed Chart, Spec or Checkpoint head into an under-way phase line, never a later one", "later: <title> (from <signal>)", "fresh member Signal", "cites the closed Signal", "Never reopen the closed Signal"} {
				if !strings.Contains(got, w) {
					t.Errorf("cut prompt (bodyless=%v, contexts=%v) lacks %q", bodyless, ctxs, w)
				}
			}
		}
	}
}

// A Workspace with no repo = contexts pays no git read for the cut prompt (ADR 0157).
func TestRepoContextsNoReposNoGitCall(t *testing.T) {
	swap(t, &git.RepoRoot, func() (string, error) { t.Error("git.RepoRoot called"); return "", nil })
	if got := repoContexts(); got != nil {
		t.Fatalf("repoContexts() = %v, want nil", got)
	}
}

// Spec D9 / ADR 0341: a HITL Approval that fell back from AFK auto-approve tells
// Luffy to record it in Approval History; an unmarked one is unchanged.
func TestLaunchApprovalCarriesFallbackLineOnlyWhenMarked(t *testing.T) {
	const line = "auto-approve failed (AFK, 3 attempts); resolved HITL"
	launch := func(labels ...string) string {
		stubGraph(t, "", nil)
		inner := show
		show = func(id string) (bd.Bead, error) {
			b, err := inner(id)
			b.Labels = labels
			return b, err
		}
		h := &stubHarness{}
		if _, err := (Luffy{}).Launch(stubCtx{h}, contract.Bead{ID: "ws-appr", Kind: contract.KindApproval}); err != nil {
			t.Fatalf("Launch: %v", err)
		}
		return h.inv.Prompt
	}
	if p := launch(registry.AutoApproveFailedLabel); !strings.Contains(p, line) {
		t.Errorf("marked Approval prompt lacks %q", line)
	}
	if p := launch(); strings.Contains(p, line) || strings.Contains(p, "fell back") {
		t.Errorf("unmarked Approval prompt carries the fallback line")
	}
}

// The unattended prompt has no human to answer, and its core joins the tail with
// a blank line.
func TestApprovalPromptAFKHasNoHumanReferenceAndJoinsWithBlankLine(t *testing.T) {
	p := approvalPromptAFK("ws-appr", true, nil)
	if strings.Contains(p, "the human asks") {
		t.Error("AFK prompt still tells the session to answer the human")
	}
	if !strings.Contains(p, "then the Decision Packet.\n\nKeep one body file") {
		t.Errorf("AFK prompt core/tail join lacks a blank line:\n%s", p)
	}
}
