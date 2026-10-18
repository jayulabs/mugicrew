package main

import (
	"fmt"
	"io"

	"jayulabs.org/mugicrew/rebase"
)

// rebaseSeams is runRebase's filing seam; a var so main's tests script it.
var rebaseSeams = rebase.Real

// runRebase is `mugicrew rebase <container>` (ADR 0262): it files a requested rebase of
// an open Chart or Checkpoint and returns the exit code. A refusal or infrastructure
// failure is 1 and files nothing; filed, already-requested, open-conflict and
// already-on-mainline are 0.
func runRebase(argv []string, stdout, stderr io.Writer) int {
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
	default:
		fmt.Fprintln(stderr, "rebase: refused:", out.Reason)
		return 1
	}
	return 0
}
