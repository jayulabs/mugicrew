package contract

import "strings"

// Section is one "## " block of a Chart body, read over its lines: lines[Head] is the
// heading and lines[Head+1:End] the section's lines. Name is the heading text, trimmed.
type Section struct {
	Name      string
	Head, End int
}

// ChartSections is the one reader of a Chart body's "## " sections, shared by bd's
// section editor, the TUI's chart view and ChartSummary. Lines before the first
// heading belong to no section.
func ChartSections(lines []string) []Section {
	var secs []Section
	for i, l := range lines {
		if h, ok := strings.CutPrefix(l, "## "); ok {
			if n := len(secs); n > 0 {
				secs[n-1].End = i
			}
			secs = append(secs, Section{Name: strings.TrimSpace(h), Head: i, End: len(lines)})
		}
	}
	return secs
}

// ChartSummary is a Chart's Land MR body, built mechanically from its body: the
// Destination section, then the Decisions-so-far section, each copied verbatim
// (trimmed of surrounding blank lines) and separated by a blank line. A section that
// is missing or empty is skipped, so a Chart with no decisions yet still has a summary.
func ChartSummary(body string) string {
	lines := strings.Split(body, "\n")
	secs := ChartSections(lines)
	var parts []string
	for _, name := range []string{"Destination", "Decisions-so-far"} {
		for _, s := range secs {
			if strings.EqualFold(s.Name, name) {
				if t := strings.TrimSpace(strings.Join(lines[s.Head+1:s.End], "\n")); t != "" {
					parts = append(parts, t)
				}
				break
			}
		}
	}
	return strings.Join(parts, "\n\n")
}
