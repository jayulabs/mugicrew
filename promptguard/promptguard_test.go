package promptguard

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestSkillCalls(t *testing.T) {
	for _, c := range []struct {
		text string
		bad  bool
	}{
		{"Use the Skill tool to load it.", true},
		{"Run `/code-review` over the diff.", true},
		{"then /implement it", true},
		{"run **/grilling** or [/Code-review]", true},
		{"Notes (domain, skills, ADRs)", false},
		{"read docs/adr/ and apps/mugicrew/", false},
		{"under /tmp/x/y", false},
		{"and/or", false},
		{"Read crew doc grilling.md", false},
		{"⏸ Surgery idle — /exit; l on its row resumes it.", false},
	} {
		if got := SkillCalls(c.text); (len(got) > 0) != c.bad {
			t.Errorf("SkillCalls(%q) = %q, want bad=%v", c.text, got, c.bad)
		}
	}
	if skillNames() == nil {
		t.Skip("no skills-lock.json above the working dir: the <name> skill check is off")
	}
	if got := SkillCalls("Use the grilling skill."); len(got) != 1 {
		t.Errorf(`"<name> skill" for a locked skill: got %q`, got)
	}
}

func TestADRCitations(t *testing.T) {
	if got := ADRCitations("per ADR 0086, ADR-0012 and ADRs 0080"); len(got) != 3 {
		t.Errorf("got %q", got)
	}
	if got := ADRCitations("read docs/adr/ and the ADRs"); len(got) != 0 {
		t.Errorf("got %q", got)
	}
}

func TestKindPrefixedTitles(t *testing.T) {
	for _, c := range []struct {
		text string
		bad  bool
	}{
		{`--title "Critique: x"`, true},
		{`--title "pivot: FR3: y"`, true},
		{`--title "Chart-conflict: z"`, true},
		{`--title "Surgery: w"`, true},
		{`--title "Task: v"`, true},
		{`--title "<what's wrong>"`, false},
		{`--title "<FR>: <what's wrong>"`, false},
		{"git commit -m \"x\n\nSpec: mc-1\"", false},
	} {
		if got := KindPrefixedTitles(c.text); (len(got) > 0) != c.bad {
			t.Errorf("KindPrefixedTitles(%q) = %q, want bad=%v", c.text, got, c.bad)
		}
	}
}

func TestCrewDocRefs(t *testing.T) {
	got := CrewDocRefs("Read crew doc grilling.md, then crew doc prototype/LOGIC.md. A crew doc <name>.md is a form. Read crew doc\n   spec-template.md.")
	if want := []string{"grilling.md", "prototype/LOGIC.md", "spec-template.md"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Spec D6.1: the skill-call ban runs over every Crew doc, as does the
// kind-prefixed title ban.
func TestCrewDocsCallNoSkill(t *testing.T) {
	for name, text := range Docs(t) {
		if hits := SkillCalls(text); len(hits) > 0 {
			t.Errorf("crew doc %s: skill calls %q", name, hits)
		}
		if hits := KindPrefixedTitles(text); len(hits) > 0 {
			t.Errorf("crew doc %s: kind-prefixed titles %q", name, hits)
		}
	}
}

// Spec D6.2: every crew-doc reference in the preamble (its index included), a
// prompt or a crew doc resolves, and every crew doc is referenced (no orphans).
func TestCrewDocRefsTwoWay(t *testing.T) {
	docs := Docs(t)
	texts := promptTexts(t)
	referenced := map[string]bool{}
	scan := func(from, text string) {
		for _, ref := range CrewDocRefs(text) {
			if _, ok := docs[ref]; !ok {
				t.Errorf("%s: crew doc %s does not exist", from, ref)
			}
			if ref != from {
				referenced[ref] = true
			}
		}
	}
	for p, text := range texts {
		scan(p, text)
	}
	for name, text := range docs {
		scan(name, text)
	}
	for name := range docs {
		if !referenced[name] {
			t.Errorf("crew doc %s is referenced by no prompt, preamble or other doc", name)
		}
	}
}

// Spec D6.1 and D6.5: none of the 13 prompt templates nor the preamble calls a
// skill or cites an ADR.
func TestPromptsAndPreambleSkillFree(t *testing.T) {
	for p, text := range promptTexts(t) {
		if hits := SkillCalls(text); len(hits) > 0 {
			t.Errorf("%s: skill calls %q", p, hits)
		}
		if hits := ADRCitations(text); len(hits) > 0 {
			t.Errorf("%s: ADR citations %q", p, hits)
		}
		if hits := KindPrefixedTitles(text); len(hits) > 0 {
			t.Errorf("%s: kind-prefixed titles %q", p, hits)
		}
	}
}

// promptTexts reads the 13 prompt templates and CREWMATE.md, by path.
func promptTexts(t *testing.T) map[string]string {
	t.Helper()
	texts := map[string]string{}
	paths, _ := filepath.Glob("../*/prompts/*.md")
	for _, p := range append(paths, "../contract/CREWMATE.md") {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		texts[p] = string(b)
	}
	if len(texts) < 14 {
		t.Fatalf("read %d prompts+preamble, want the 13 prompts and CREWMATE.md", len(texts))
	}
	return texts
}

// Spec D14: a leaf prompt names mugicrew integrate, ahead of any bd close.
func TestIntegratesBeforeClose(t *testing.T) {
	for _, c := range []struct {
		text string
		ok   bool
	}{
		{"commit, then run `mugicrew integrate`; Franky runs bd close after", true},
		{"run mugicrew integrate as your last step", true},
		{"bd close <id>, then mugicrew integrate", false},
		{"commit and bd close <id>", false},
	} {
		if got := IntegratesBeforeClose(c.text); got != c.ok {
			t.Errorf("IntegratesBeforeClose(%q) = %v, want %v", c.text, got, c.ok)
		}
	}
}

// Spec D10, FR 35: the build and review prompts merge only into a pivot the
// Checkpoint's blocks-edge query (or the agent's own prior raise) lists, never a
// sibling's unpublished Draft, and accept a duplicate the sweep closes superseded:.
func TestPivotMergeRule(t *testing.T) {
	texts := promptTexts(t)
	for _, p := range []string{"../franky/prompts/build.md", "../usopp/prompts/review.md"} {
		for _, want := range []string{"merge only into a pivot those lists show", "superseded:"} {
			if !strings.Contains(texts[p], want) {
				t.Errorf("%s does not state the pivot merge rule: missing %q", p, want)
			}
		}
	}
}

// Spec D4: every prompt or crew doc that tells an agent to run a parented
// bd create carries the shared re-run sentence beside each such command.
func TestParentedCreatesCarryRerunLine(t *testing.T) {
	const rerun = "If it fails with *already exists* or *1213*, re-run the same create: nothing was written, and the retry gets a fresh ID."
	files, _ := filepath.Glob("../*/prompts/*.md")
	docs, _ := filepath.Glob("../crewdocs/*.md")
	files = append(files, docs...)
	if len(files) < 20 {
		t.Fatalf("found only %d prompt/crew-doc files", len(files))
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		parented := 0
		for _, c := range bdCreate.FindAllString(string(b), -1) {
			if strings.Contains(c, "--parent") {
				parented++
			}
		}
		if got := strings.Count(string(b), rerun); got < parented {
			t.Errorf("%s: %d parented bd create(s), %d re-run line(s)", f, parented, got)
		}
	}
}

// bdCreate matches a bd create command through its backslash continuations.
var bdCreate = regexp.MustCompile(`bd create(?:\\\n|[^\n])*`)

// inlineSignalCreates returns the bd create commands (continuations included)
// in text that carry kind:signal.
func inlineSignalCreates(text string) []string {
	var out []string
	for _, c := range bdCreate.FindAllString(text, -1) {
		if strings.Contains(c, "kind:signal") {
			out = append(out, c)
		}
	}
	return out
}

func TestInlineSignalCreates(t *testing.T) {
	for _, c := range []struct {
		text string
		bad  bool
	}{
		{"bd create --type task --labels kind:signal,drafted-by:x", true},
		{"bd create --type task \\\n  -l kind:signal \\\n  --title t", true},
		{"bd create --type task --labels kind:critique,drafted-by:x", false},
		{"file a kind:signal bead; bd create is in the crew doc", false},
	} {
		if got := inlineSignalCreates(c.text); (len(got) > 0) != c.bad {
			t.Errorf("inlineSignalCreates(%q) = %q, want bad=%v", c.text, got, c.bad)
		}
	}
}

// Spec FR15: signal-filing.md is the one place a Signal's bd create lives; no
// prompt inlines its own. Crew docs are not scanned.
func TestPromptsInlineNoSignalCreate(t *testing.T) {
	for p, text := range promptTexts(t) {
		if hits := inlineSignalCreates(text); len(hits) > 0 {
			t.Errorf("%s: inline Signal create(s) %q; Read crew doc signal-filing.md instead", p, hits)
		}
	}
}
