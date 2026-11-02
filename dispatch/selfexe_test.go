package dispatch

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/selfexe"
)

func TestReconcileInWorktreeRunsStartupPath(t *testing.T) {
	dir := t.TempDir()
	rec := filepath.Join(dir, "argv")
	stub := filepath.Join(dir, "mugicrew")
	os.WriteFile(stub, []byte("#!/bin/sh\necho \"$@\" > "+rec+"\n"), 0o755)
	defer selfexe.Stub(stub)()
	if err := reconcileInWorktree(dir, "mc-x", "eff", io.Discard); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(rec); strings.TrimSpace(string(b)) != "reconcile mc-x" {
		t.Errorf("stub argv = %q; want reconcile mc-x", b)
	}
}
