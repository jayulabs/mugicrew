package contract

import (
	"regexp"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/crewdocs"
	"jayulabs.org/mugicrew/promptguard"
)

// Spec D6.5: the preamble stays a preamble — ≤ 2 KB, one {{CREWDOCS}}, static
// (no fmt verbs), no ADR citation, no skill call.
func TestPreambleGuards(t *testing.T) {
	if n := len(preamble); n > 2048 {
		t.Errorf("CREWMATE.md is %d bytes, cap 2048", n)
	}
	if n := strings.Count(preamble, "{{CREWDOCS}}"); n != 1 {
		t.Errorf("CREWMATE.md has %d {{CREWDOCS}} tokens, want 1", n)
	}
	if v := regexp.MustCompile(`%[-+#0-9.*\[\]]*[a-zA-Z%]`).FindAllString(preamble, -1); len(v) > 0 {
		t.Errorf("CREWMATE.md has fmt verbs %q", v)
	}
	promptguard.Check(t, "CREWMATE.md", preamble)
}

// NewInvocation opens the kind's prompt with the preamble and separator, the
// Crew docs dir filled in and no {{…}} token left.
func TestNewInvocationFoldsPreamble(t *testing.T) {
	dir, err := crewdocs.Dir()
	if err != nil {
		t.Fatal(err)
	}
	inv, err := NewInvocation("mc-x", "KIND BODY")
	if err != nil {
		t.Fatal(err)
	}
	if inv.BeadID != "mc-x" {
		t.Errorf("BeadID = %q", inv.BeadID)
	}
	if !strings.HasPrefix(inv.Prompt, "You are a mugicrew crewmate") || !strings.HasSuffix(inv.Prompt, "\n\n---\n\nKIND BODY") {
		t.Errorf("prompt is not preamble + separator + body:\n%s", inv.Prompt)
	}
	if !strings.Contains(inv.Prompt, dir) {
		t.Errorf("prompt does not name the Crew docs dir %s", dir)
	}
	if strings.Contains(inv.Prompt, "{{") {
		t.Errorf("unreplaced token in prompt:\n%s", inv.Prompt)
	}
}

// HITL: the ClosingLine still lands after the kind's body, not the preamble.
func TestClosingLineFollowsBody(t *testing.T) {
	h := &recordingHarness{}
	inv, err := NewInvocation("mc-x", "KIND BODY")
	if err != nil {
		t.Fatal(err)
	}
	WithClosingLine(harnessCtx{h}).Harness().LaunchCommand(inv)
	if !strings.HasSuffix(h.inv.Prompt, "KIND BODY\n\n"+ClosingLine()) || !strings.HasPrefix(h.inv.Prompt, "You are a mugicrew crewmate") {
		t.Errorf("got %q", h.inv.Prompt)
	}
}

type recordingHarness struct {
	Harness
	inv Invocation
}

func (h *recordingHarness) LaunchCommand(inv Invocation) (LaunchCommand, error) {
	h.inv = inv
	return nil, nil
}

type harnessCtx struct{ h Harness }

func (c harnessCtx) Harness() Harness { return c.h }

// Spec slice 7: lazy domain docs in mugicrew's format, the Workspace's
// CONTRIBUTING.md then README.md named (never inlined), and the precedence.
func TestPreambleWorkspaceConventions(t *testing.T) {
	flat := strings.Join(strings.Fields(preamble), " ")
	if strings.Contains(flat, "don't invent glossary files") {
		t.Error("preamble still forbids creating glossary files")
	}
	for _, want := range []string{"lazily", "GLOSSARY-FORMAT.md", "ADR-FORMAT.md", "your prompt wins", "Workspace's docs win", "crew doc vocabulary.md", "once per session", "(domain-modeling.md, GLOSSARY-FORMAT.md, ADR-FORMAT.md)", "build/test commands they give"} {
		if !strings.Contains(flat, want) {
			t.Errorf("preamble lacks %q", want)
		}
	}
	if c, r := strings.Index(flat, "CONTRIBUTING.md"), strings.Index(flat, "README.md"); c < 0 || r < c {
		t.Error("preamble must name CONTRIBUTING.md, then README.md")
	}
	for _, want := range []string{".mugicrew/CREW.md", "human-authored", "file a Signal"} {
		if !strings.Contains(flat, want) {
			t.Errorf("preamble lacks %q", want)
		}
	}
}

// Spec FR1-FR3: ADRs are cited in bead text by number plus slug, or by path.
func TestPreambleCitesADRBySlugOrPath(t *testing.T) {
	flat := strings.Join(strings.Fields(preamble), " ")
	for _, want := range []string{"Cite an ADR in bead text", "(<slug>)", "by repo path", "never by bare number", "filename stem after NNNN-", "cut short with …"} {
		if !strings.Contains(flat, want) {
			t.Errorf("preamble lacks %q", want)
		}
	}
}
