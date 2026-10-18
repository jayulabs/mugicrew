package tui

import "strings"

// matchWords is the word matcher Graph and Logs share (ADR 0143 §2): the query is
// split on whitespace and each word must appear in text, case-insensitively and
// in any order. No regex; an empty query matches everything.
func matchWords(query, text string) bool {
	text = strings.ToLower(text)
	for _, w := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}
