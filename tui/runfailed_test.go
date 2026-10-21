package tui

import (
	"testing"

	"jayulabs.org/mugicrew/eventlog"
)

// run.failed is Logs-tab only (ADR 0331): outside the digest the flash and the
// rollups read.
func TestRunFailedIsNotInTheDigest(t *testing.T) {
	if inDigest(eventlog.Event{Msg: "run.failed"}) {
		t.Error("run.failed is in the digest")
	}
}
