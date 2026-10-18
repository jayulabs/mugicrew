package nami

import (
	"strings"
	"testing"

	"jayulabs.org/mugicrew/git"
)

func TestRenumberBodyNamesOtherContainerWhenKnown(t *testing.T) {
	body := renumberBody("mc-l", "sha", []git.PostLandClash{
		{Added: "docs/adr/0009-b.md", Others: []string{"docs/adr/0009-a.md"}, LandedBy: []string{"mc-a"}},
		{Added: "docs/adr/0010-b.md", Others: []string{"docs/adr/0010-a.md"}},
	})
	for _, want := range []string{"clashes with docs/adr/0009-a.md (landed by mc-a)\n", "clashes with docs/adr/0010-a.md\n"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
}
