package crewdocs_test

import (
	"path"
	"regexp"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/promptguard"
)

// pin is the first line of a skill-derived Crew doc (ADR 0087).
var pin = regexp.MustCompile(`^<!-- from mattpocock/skills@[0-9a-f]{40} (\S+) · lock-hash ([0-9a-f]{64}) · (?:vendored|distilled) -->\n`)

// Spec D6.5: every pin's lock hash is its skill's computedHash in this repo's
// skills-lock.json, so a skill update without a re-sync of its doc fails here.
// A doc pins the skill whose directory holds the file it came from.
func TestPinsMatchSkillsLock(t *testing.T) {
	lock := promptguard.SkillsLock()
	if lock == nil {
		t.Skip("no skills-lock.json above the working dir")
	}
	pinned := 0
	for name, text := range promptguard.Docs(t) {
		if !strings.HasPrefix(text, "<!-- from ") {
			continue
		}
		m := pin.FindStringSubmatch(text)
		if m == nil {
			t.Errorf("crew doc %s: malformed pin line", name)
			continue
		}
		pinned++
		var skill *promptguard.LockedSkill
		for _, s := range lock {
			if path.Dir(s.SkillPath) == path.Dir(m[1]) {
				skill = &s
				break
			}
		}
		switch {
		case skill == nil:
			t.Errorf("crew doc %s pins %s, which no locked skill holds", name, m[1])
		case skill.ComputedHash != m[2]:
			t.Errorf("crew doc %s pins lock-hash %s, skills-lock.json has %s: re-sync it", name, m[2], skill.ComputedHash)
		}
	}
	if pinned == 0 {
		t.Error("no pinned crew doc found")
	}
}
