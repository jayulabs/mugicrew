package session

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/selfexe"
)

// A stub binary at the startup path records its argv: integrateIn must run it,
// not a fresh os.Executable().
func TestIntegrateInRunsStartupPath(t *testing.T) {
	dir := t.TempDir()
	rec := filepath.Join(dir, "argv")
	stub := filepath.Join(dir, "mugicrew")
	os.WriteFile(stub, []byte("#!/bin/sh\necho \"$@\" > "+rec+"\nexit 0\n"), 0o755)
	defer selfexe.Stub(stub)()
	var out bytes.Buffer
	if code := integrateIn(dir, &out); code != ExitIntegrated {
		t.Fatalf("exit %d, out %q", code, out.String())
	}
	if b, _ := os.ReadFile(rec); strings.TrimSpace(string(b)) != "integrate" {
		t.Errorf("stub argv = %q; want integrate", b)
	}
}

func TestTmuxLaunchesRunStartupPath(t *testing.T) {
	defer selfexe.Stub("/startup/path/mugicrew")()
	if got := beadExe(); got != "/startup/path/mugicrew" {
		t.Errorf("beadExe = %q; want the startup path", got)
	}
}
