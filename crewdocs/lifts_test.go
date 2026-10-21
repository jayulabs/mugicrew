package crewdocs_test

import (
	"strings"
	"testing"

	"jayulabs.org/mugicrew/promptguard"
)

func TestCrewDocLifts(t *testing.T) {
	docs := promptguard.Docs(t)
	for doc, wants := range map[string][]string{
		"signal-filing.md": {"Some filings are human-only", "Rollback", "mugicrew rebase", "Surgery", "Task mid-work"},
		"defog-session.md": {"Never edit an Initiative's body"},
	} {
		text := strings.Join(strings.Fields(docs[doc]), " ")
		for _, w := range wants {
			if !strings.Contains(text, w) {
				t.Errorf("%s lacks %q", doc, w)
			}
		}
	}
}
