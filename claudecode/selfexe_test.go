package claudecode

import (
	"strings"
	"testing"

	"jayulabs.org/mugicrew/selfexe"
)

func TestWaitingHooksUseStartupPath(t *testing.T) {
	defer selfexe.Stub("/startup/path/mugicrew")()
	got, err := waitingHooks()
	if err != nil || !strings.Contains(got, "'/startup/path/mugicrew' hitl-hook") {
		t.Errorf("waitingHooks = %q, %v; want the startup path in the hitl-hook command", got, err)
	}
}
