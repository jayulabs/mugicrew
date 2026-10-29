package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"jayulabs.org/mugicrew/backup"
	"jayulabs.org/mugicrew/eventlog"
)

const backupUsage = "usage: mugicrew backup push"

// runBackup is `mugicrew backup push` (ADR 0370): one push of the Bead Graph's
// unpushed commits to its backup remote. A held lock, a nothing-to-push run and
// a success exit 0; no backup remote and a failed push exit 1.
func runBackup(argv []string, w, errw io.Writer) int {
	if len(argv) != 1 || argv[0] != "push" {
		fmt.Fprintln(errw, backupUsage)
		return 1
	}
	out, err := backup.Sync(os.Getenv("BEADS_DIR"), eventlog.BackupCLI)
	switch {
	case out == backup.Held:
		fmt.Fprintln(w, "backup: another push is running; skipped")
	case err != nil && errors.Is(err, backup.ErrNoRemote):
		fmt.Fprintln(errw, err)
		return 1
	case err != nil:
		fmt.Fprintln(errw, "backup:", err)
		return 1
	}
	return 0
}
