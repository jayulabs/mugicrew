// Package promptguard holds the acceptance guards (Spec D6) every
// crewmate package's tests run over the text a session receives: its rendered
// prompts, the Crewmate preamble and the Crew docs. Only tests import it; the
// binary never does.
package promptguard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"jayulabs.org/mugicrew/crewdocs"
)

var (
	skillTool = regexp.MustCompile(`(?i)\bskill tool\b`)
	// A /<name> slash command: a slash opening a word (not a path segment like
	// docs/adr/, not a path like /tmp/x).
	slashCmd = regexp.MustCompile("(?:^|[\\s\"'(`*\\[])(/[A-Za-z][A-Za-z0-9-]*)(?:[^A-Za-z0-9/-]|$)")
	adrCite  = regexp.MustCompile(`(?i)\bADRs?[ -]?\d{2,4}\b`)
	// A --title template opening with a contract kind's display name and a
	// colon: the kind:* label carries the kind, never the title.
	// ponytail: kind names hand-listed; contract's internal tests import
	// promptguard, so deriving them from contract would be an import cycle.
	kindTitle = regexp.MustCompile(`(?i)--title\s+"(?:Signal|Chart|Chart-conflict|Spec|Checkpoint|Build|Review|Forge Review|Critique|Conflict|Pivot|Approval|Rollback|Grilling|Research|Prototype|Surgery|Task):`)
	// CrewDocRef is the parseable crew-doc reference form: "crew doc <name>.md",
	// as in a prompt's "Read crew doc <name>.md", where a line wrap may fall
	// between "crew doc" and the name.
	CrewDocRef = regexp.MustCompile(`\bcrew doc\s+([A-Za-z0-9_/-]+\.md)\b`)
	// RetiredPhrasings are wordings that read a shipped glossary term (Held,
	// Carry) in a sense the glossary contradicts; no prompt or crew doc may
	// say them.
	RetiredPhrasings = []string{"Land held by", "Carry moves", "Land the finding"}
)

// SkillCalls lists every skill call in text: "Skill tool", any /<name> slash
// command, and "<name> skill" for each skill in this repo's skills-lock.json.
// Not a bare "skill" grep: "Notes (domain, skills, …)" passes.
func SkillCalls(text string) []string {
	var hits []string
	hits = append(hits, skillTool.FindAllString(text, -1)...)
	for _, m := range slashCmd.FindAllStringSubmatch(text, -1) {
		if m[1] != "/exit" { // the harness's built-in quit, not a skill
			hits = append(hits, m[1])
		}
	}
	if re := namedSkill(); re != nil {
		hits = append(hits, re.FindAllString(text, -1)...)
	}
	return hits
}

// ADRCitations lists every "ADR NNNN" citation in text.
func ADRCitations(text string) []string { return adrCite.FindAllString(text, -1) }

// bareADR is "ADR NNNN" as bead text cites an ADR by number.
var bareADR = regexp.MustCompile(`\bADR \d{4}\b`)

// BareADRCitations lists every "ADR NNNN" in text that is not followed by its
// slug, " (<slug>)": bead text Go renders must cite each ADR as
// ADR <n> (<slug>) so a foreign Workspace's crewmate can tell what it decided.
func BareADRCitations(text string) []string {
	var hits []string
	for _, loc := range bareADR.FindAllStringIndex(text, -1) {
		if !strings.HasPrefix(text[loc[1]:], " (") {
			hits = append(hits, text[loc[0]:loc[1]])
		}
	}
	return hits
}

// KindPrefixedTitles lists every kind-prefixed --title template in text.
func KindPrefixedTitles(text string) []string { return kindTitle.FindAllString(text, -1) }

// CrewDocRefs lists the crew docs text names, in order.
func CrewDocRefs(text string) []string {
	var refs []string
	for _, m := range CrewDocRef.FindAllStringSubmatch(text, -1) {
		refs = append(refs, m[1])
	}
	return refs
}

// Check runs every guard over one rendered text a session receives: no skill
// call, no ADR citation, no kind-prefixed --title template, and every crew-doc
// reference resolves to a Crew doc.
func Check(t testing.TB, name, text string) {
	t.Helper()
	if hits := SkillCalls(text); len(hits) > 0 {
		t.Errorf("%s: skill calls %q", name, hits)
	}
	if hits := ADRCitations(text); len(hits) > 0 {
		t.Errorf("%s: ADR citations %q", name, hits)
	}
	if hits := KindPrefixedTitles(text); len(hits) > 0 {
		t.Errorf("%s: kind-prefixed titles %q", name, hits)
	}
	for _, p := range RetiredPhrasings {
		if strings.Contains(text, p) {
			t.Errorf("%s: retired phrasing %q", name, p)
		}
	}
	docs := Docs(t)
	for _, ref := range CrewDocRefs(text) {
		if _, ok := docs[ref]; !ok {
			t.Errorf("%s: crew doc %s does not exist", name, ref)
		}
	}
}

// Docs reads every Crew doc, by its path under the Crew docs dir.
func Docs(t testing.TB) map[string]string {
	t.Helper()
	dir, err := crewdocs.Dir()
	if err != nil {
		t.Fatal(err)
	}
	docs := map[string]string{}
	err = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		rel, _ := filepath.Rel(dir, p)
		docs[filepath.ToSlash(rel)] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return docs
}

// namedSkill matches "<name> skill" for any locked skill; nil when none is found.
var namedSkill = sync.OnceValue(func() *regexp.Regexp {
	names := skillNames()
	if len(names) == 0 {
		return nil
	}
	for i, n := range names {
		names[i] = regexp.QuoteMeta(n)
	}
	return regexp.MustCompile(`(?i)\b(?:` + strings.Join(names, "|") + `) skill\b`)
})

// skillNames lists the skills in this repo's skills-lock.json, sorted.
func skillNames() []string {
	lock := SkillsLock()
	if lock == nil {
		return nil
	}
	names := make([]string, 0, len(lock))
	for n := range lock {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// LockedSkill is one skills-lock.json entry.
type LockedSkill struct {
	SkillPath    string
	ComputedHash string
}

// SkillsLock reads the nearest skills-lock.json above the working directory,
// by skill name; nil when there is none.
// ponytail: none found (the nix check sandbox holds only apps/mugicrew) ⇒ the
// "<name> skill" check and the pin lock-hash check are off; the "Skill tool"
// and slash-command checks still run.
func SkillsLock() map[string]LockedSkill {
	dir, _ := os.Getwd()
	for {
		b, err := os.ReadFile(filepath.Join(dir, "skills-lock.json"))
		if err == nil {
			var lock struct{ Skills map[string]LockedSkill }
			if err := json.Unmarshal(b, &lock); err != nil {
				panic("skills-lock.json: " + err.Error())
			}
			return lock.Skills
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

// IntegratesBeforeClose reports whether a leaf prompt names `mugicrew integrate`
// ahead of any `bd close` (Spec D14): a leaf's work reaches its container
// before its bead closes.
func IntegratesBeforeClose(text string) bool {
	i := strings.Index(text, "mugicrew integrate")
	c := strings.Index(text, "bd close")
	return i >= 0 && (c < 0 || i < c)
}
