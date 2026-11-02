package main

import (
	"fmt"
	"io"
	"strings"

	"jayulabs.org/mugicrew/rebase"
	"jayulabs.org/mugicrew/session"
)

// rebaseSeams is runRebase's filing seam; a var so main's tests script it.
var rebaseSeams = rebase.Real

// runRebase is `mugicrew rebase <container>` (ADR 0262): it files a requested rebase of
// an open Chart or Checkpoint and returns the exit code. A refusal or infrastructure
// failure is 1 and files nothing, as does any call from a crewmate session (before
// the argument count is read); filed, already-requested, open-conflict and
// already-on-mainline are 0.
func runRebase(argv []string, stdout, stderr io.Writer) int {
	if session.CalledByAgent() {
		fmt.Fprintln(stderr, "rebase: refused: a requested rebase is human-only (ADR 0262 (a-requested-rebase-is-a-human-filed-conflict-variant…)); "+
			"file it from the TUI New menu's r row or run mugicrew rebase from a plain shell; an agent suggests one by an offshoot Signal")
		return 1
	}
	if len(argv) != 1 {
		fmt.Fprintln(stderr, "usage: mugicrew rebase <chart-or-checkpoint-id>")
		return 1
	}
	out, err := rebase.Request(rebaseSeams, argv[0])
	if err != nil {
		fmt.Fprintln(stderr, "rebase:", err)
		return 1
	}
	if out.Warning != "" {
		fmt.Fprintln(stderr, "warning:", out.Warning)
	}
	switch out.Result {
	case rebase.Filed:
		fmt.Fprintf(stdout, "filed: %s (%s)\n", out.Title, out.ID)
	case rebase.AlreadyRequested:
		fmt.Fprintf(stdout, "already requested: %s (%s)\n", out.Title, out.ID)
	case rebase.OpenConflict:
		fmt.Fprintf(stdout, "already being rebased by open conflict: %s (%s) Base: %s\n", out.Title, out.ID, out.Base)
	case rebase.AlreadyOnMain:
		fmt.Fprintf(stdout, "already on mainline: %s\n", out.Base)
		if out.MembersMayBePaused {
			fmt.Fprintln(stdout, rebase.MemberPauseHint)
		}
	default:
		fmt.Fprintln(stderr, "rebase: refused:", out.Reason)
		return 1
	}
	if len(out.Edged) > 0 {
		fmt.Fprintf(stdout, "%d build(s) now wait on it: %s\n", len(out.Edged), strings.Join(out.Edged, ", "))
	}
	return 0
}
