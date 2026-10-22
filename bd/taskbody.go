package bd

import (
	"strconv"
	"strings"
)

// TaskStep is one numbered step of a Task body in crew doc task-body.md's format: a
// title, markers, a Do, then a Check, or an Ask where nothing can be probed.
type TaskStep struct {
	Title   string
	Markers []string // "irreversible", "elevated", "secret"
	Do      string
	Check   string // a re-runnable command and its expected result
	Ask     string // used only when Check is empty
}

// TaskBody assembles a Hold Task body (ADR 0335): the lead paragraph, the fact lines
// (each a whole "Key: value" line), a blank line, ## Steps, then an optional trailing
// ## Diagnostics. Every key line a filer passes to SpawnBlockingTaskIfAbsent must be
// among the facts, so the idempotency match still finds the open Task.
func TaskBody(lead string, facts []string, steps []TaskStep, diagnostics string) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(lead, "\n") + "\n")
	for _, f := range facts {
		b.WriteString(strings.TrimRight(f, "\n") + "\n")
	}
	b.WriteString("\n## Steps\n")
	for i, s := range steps {
		b.WriteString("\n" + strconv.Itoa(i+1) + ". " + s.Title)
		for _, m := range s.Markers {
			b.WriteString("   [" + m + "]")
		}
		b.WriteString("\n   Do: " + s.Do + "\n")
		if s.Check != "" {
			b.WriteString("   Check: " + s.Check + "\n")
		} else {
			b.WriteString("   Ask: " + s.Ask + "\n")
		}
	}
	if diagnostics != "" {
		b.WriteString("\n## Diagnostics\n\n" + strings.TrimRight(diagnostics, "\n") + "\n")
	}
	return b.String()
}
