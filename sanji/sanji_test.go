package sanji

import (
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/promptguard"
)

// stubHarness records the Invocation the Launch path composes so we can assert
// the prompt without spawning claude. Mirrors nami_test's stubHarness.
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

// Launch(prototype) → one agent-variety command anchored to the bead, whose
// prompt reads the prototype Crew doc, the side-ref landing contract, and the
// bead-close contract
// (ADR 0017 §1/§2). Pinned so an edit can't silently drop a leg of the contract.
func TestLaunchPrototypeComposesSession(t *testing.T) {
	h := &stubHarness{}
	lc, err := Sanji{}.Launch(stubCtx{h}, contract.Bead{ID: "ws-proto", Kind: contract.KindPrototype})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if _, ok := lc.(contract.AgentCommand); !ok {
		t.Fatalf("Launch must return the agent variety, got %T", lc)
	}
	if h.inv.BeadID != "ws-proto" {
		t.Errorf("Invocation must anchor to the prototype id, got %q", h.inv.BeadID)
	}
	p := h.inv.Prompt
	for _, want := range []string{
		"ws-proto",                  // anchored to the bead
		"crew doc prototype.md",     // the prototype procedure (ADR 0017 §2)
		"refs/mugicrew/prototypes/", // side-ref landing (ADR 0009/0017 §3)
		"update-ref",                // lands HEAD to the ref, not a merge
		"worktree remove",           // scratch worktree removed after landing
		"bd close ws-proto",         // bead-close contract (ADR 0017 §4)
		"crew doc defog-session.md", // claim, orient, tend, record (gist to the Chart)
		"never",                     // guardrail against merging the throwaway
		"git worktree add --detach", // throwaway tree holds no branch (ADR 0071)
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prototype prompt missing %q", want)
		}
	}
	// The throwaway is cut at the wrapper-supplied temp-shaped path, not a fixed one.
	if strings.Contains(p, "mugicrew-prototype-") {
		t.Error("prototype prompt must not carry the fixed ../mugicrew-prototype-<chart-id> path")
	}
	_, rest, _ := strings.Cut(p, "--detach ")
	scratch, _, _ := strings.Cut(rest, " ")
	if !git.IsTempWorktree(scratch) || scratch != git.TempWorktreePath("ws-proto") {
		t.Errorf("throwaway path %q must be temp-shaped (git.IsTempWorktree)", scratch)
	}
}

// Spec D11 row 8: the prototype session drafts its graduated fog by the
// prototype bead (the shared block) BEFORE it closes that bead, so the close can
// never drain the Chart ahead of the fog it graduates.
func TestPrototypePromptDraftsFogBeforeClose(t *testing.T) {
	got := prototypePrompt("ws-proto", "/tmp/mugicrew-wt-1/wt")
	if !strings.Contains(got, contract.DraftInstruction("ws-proto")) {
		t.Errorf("prototype prompt must embed contract.DraftInstruction(prototype):\n%s", got)
	}
	draft := strings.Index(got, "-s deferred -l kind:<grilling|research|prototype>,drafted-by:ws-proto")
	closeAt := strings.Index(got, "bd close ws-proto")
	if draft < 0 || closeAt < 0 || draft > closeAt {
		t.Errorf("prototype prompt must create the fog Drafts (at %d) before the close (at %d):\n%s", draft, closeAt, got)
	}
}

// Spec D9: the prototype session tends the Chart body through the
// locked verb.
func TestPrototypePromptTendsChartThroughVerb(t *testing.T) {
	if got := prototypePrompt("ws-proto", "/tmp/mugicrew-wt-1/wt"); !strings.Contains(got, "mugicrew chart append") {
		t.Errorf("prototype prompt must tend the Chart through mugicrew chart append:\n%s", got)
	}
}

// A non-prototype kind is not launchable — Launch is a loud error, mirroring
// Nami's Launch default arm.
func TestLaunchNonPrototypeIsLoudError(t *testing.T) {
	if _, err := (Sanji{}).Launch(stubCtx{&stubHarness{}}, contract.Bead{ID: "x", Kind: contract.KindSpec}); err == nil {
		t.Fatal("Launch of a non-prototype kind must return a loud error, got nil")
	}
}

// prototype is HITL, so the loop must never dispatch it: Run is a loud error, not
// loop work (ADR 0017 §1), mirroring Zoro's loud Launch default.
func TestRunIsLoudHITLError(t *testing.T) {
	if err := (Sanji{}).Run(stubCtx{&stubHarness{}}, contract.Bead{ID: "ws-proto", Kind: contract.KindPrototype}); err == nil {
		t.Fatal("Run of a HITL kind must return a loud error, got nil")
	}
}

// Spec D6 over the HITL prototype prompt as rendered: no skill call, no
// ADR citation, every crew-doc reference resolves, the docs the session runs on
// are named, and it points at GLOSSARY, not the stale CONTEXT (FR 47).
func TestPrototypePromptSkillFree(t *testing.T) {
	got := prototypePrompt("mc-x", "/tmp/mugicrew-wt-1/wt")
	promptguard.Check(t, "prototype", got)
	refs := promptguard.CrewDocRefs(got)
	for _, d := range []string{"prototype.md", "defog-session.md", "chart-body.md", "ADR-FORMAT.md"} {
		if !slices.Contains(refs, d) {
			t.Errorf("prototype prompt reads no crew doc %s (refs %q)", d, refs)
		}
	}
	if strings.Contains(got, "CONTEXT") || !strings.Contains(got, "GLOSSARY") {
		t.Error("prototype prompt must say GLOSSARY, not CONTEXT")
	}
}

// FR 22: no Crew doc tells the session to land prototype output anywhere but
// the side ref: the prototype doc names no throwaway branch, and every capture
// or fold-into-real-code step names the side ref.
func TestPrototypeDocLandsOnSideRefOnly(t *testing.T) {
	doc := promptguard.Docs(t)["prototype.md"]
	if doc == "" {
		t.Fatal("no prototype.md crew doc")
	}
	if m := regexp.MustCompile(`(?i)throwaway branch|commit (it )?to|into main|main branch`).FindAllString(doc, -1); m != nil {
		t.Errorf("prototype.md lands output off the side ref: %q", m)
	}
	for _, step := range []string{"Capture it when done", "Capture the answer and the prototype", "Capture the answer and clean up"} {
		_, rest, ok := strings.Cut(doc, step)
		para, _, _ := strings.Cut(rest, "\n\n")
		if !ok || !strings.Contains(para, "side ref") {
			t.Errorf("prototype.md step %q must say land on the side ref:\n%s", step, para)
		}
	}
}

// Spec FR 36, FR 56: the prototype session works on effort/<id>, names
// mugicrew integrate before bd close, and cuts its throwaway's detached tree from
// the Chart, never the effort branch, so the throwaway carries no session commit.
func TestPrototypePromptIntegratesEffortBranch(t *testing.T) {
	p := prototypePrompt("mc-x", "/tmp/scratch")
	if !promptguard.IntegratesBeforeClose(p) {
		t.Error("prototype prompt must name mugicrew integrate before bd close")
	}
	for _, want := range []string{"effort/mc-x", "git worktree add --detach /tmp/scratch chart/<chart-id>"} {
		if !strings.Contains(p, want) {
			t.Errorf("prototype prompt lacks %q", want)
		}
	}
}

// ADR 0157: a labelled container appends exactly the focus line; no table or an
// unlabelled container leaves the prompt as today's, with no bd read when no table.
func TestLaunchPrototypeFocus(t *testing.T) {
	old := showBead
	t.Cleanup(func() { showBead = old })
	launch := func() string {
		h := &stubHarness{}
		if _, err := (Sanji{}).Launch(stubCtx{h}, contract.Bead{ID: "ws-proto", Kind: contract.KindPrototype}); err != nil {
			t.Fatal(err)
		}
		return h.inv.Prompt
	}
	repoDir := func(cfg string) {
		d := t.TempDir()
		if out, err := exec.Command("git", "init", "-q", d).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		os.MkdirAll(d+"/.mugicrew", 0o755)
		os.WriteFile(d+"/.mugicrew/config.toml", []byte(cfg), 0o644)
		t.Chdir(d)
	}
	showBead = func(string) (bd.Bead, error) { t.Fatal("bd show on a no-context Workspace"); return bd.Bead{}, nil }
	repoDir("")
	base := launch()
	label := ""
	showBead = func(id string) (bd.Bead, error) {
		if id == "ws-proto" {
			return bd.Bead{ID: id, Parent: "ws-chart"}, nil
		}
		return bd.Bead{ID: id, Labels: []string{label}}, nil
	}
	repoDir("[context.mugicrew]\npath = \"apps/mugicrew\"\n")
	label = "kind:chart"
	if got := launch(); got != base {
		t.Error("unlabelled container changed the prompt")
	}
	label = "context:mugicrew"
	want := base + "\n\n" + contexts.FocusLine(map[string]string{"mugicrew": "apps/mugicrew"}, bd.Bead{Labels: []string{label}})
	if got := launch(); got != want || got == base {
		t.Errorf("labelled prompt = old + focus line wanted, got %q", got)
	}
}

// Spec mc-u1g7y: every prototype is served in a stacked right column: a server
// pane split off the agent pane, and a browser-trigger pane split off the
// server pane, both tagged @protopane. The hand-over happens before the tree
// is removed.
func TestPrototypePromptServesInStackedColumn(t *testing.T) {
	p := strings.Join(strings.Fields(prototypePrompt("ws-proto", "/tmp/scratch")), " ")
	for _, w := range []string{
		`split-window -t "${MUGICREW_HITL_PANE#*,}" -h -l 40% -c /tmp/scratch -P -F '#{pane_id}' '<server command>'`,
		`split-window -t <server pane id> -v -c /tmp/scratch -P -F '#{pane_id}'`,
		`set-option -p -t <server pane id> @protopane 1`,
		`set-option -p -t <trigger pane id> @protopane 1`,
		"python3 -m http.server -b 127.0.0.1 <port> -d <dir of the demo>",
		"http://127.0.0.1:<port>/<demo>.html",
		"Press Enter to open",
		`in %s\n" "<target>" "$BROWSER"`,
		"applies to both the LOGIC and the UI branch",
		"the URL the dev server reports once it is up",
		"MUGICREW_HITL_PANE is set, $BROWSER is non-empty, and command -v succeeds on $BROWSER's first word",
		"otherwise open no trigger pane",
		"In every case print the target in the chat",
		"POSIX sh loop",
		"$BROWSER unquoted",
		"If MUGICREW_HITL_PANE is empty or unset, run no tmux at all",
		"carrying only the @protopane user option, and mugicrew does not track or reap them",
	} {
		if !strings.Contains(p, w) {
			t.Errorf("prototype prompt lacks %q:\n%s", w, p)
		}
	}
	for _, bad := range []string{"file:// URL", "inherits $TMUX", "open no pane", "only for the UI branch", "does not mark"} {
		if strings.Contains(p, bad) {
			t.Errorf("prototype prompt still says %q", bad)
		}
	}
	if !strings.Contains(p, "Open both panes below before the side-ref push and the tree removal") {
		t.Error("prompt must order the panes before the tree is removed")
	}
}

// The prototype prompt carries the integrate exit block spliced from the
// session exit constants (ADR 0328).
func TestPrototypePromptCarriesIntegrateExits(t *testing.T) {
	if got := prototypePrompt("ws-proto", "/tmp/mugicrew-wt-1/wt"); !strings.Contains(got, contract.IntegrateExits()) {
		t.Error("prototype prompt lacks the integrate exit block")
	}
}
