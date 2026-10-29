package bd

import (
	"os/exec"
	"strings"
	"testing"
)

// A Hold Errand claimed by another actor closes under ForceClose and keeps its
// assignee; a plain Close of it is refused (Spec D3 amendment). Scratch graph only.
func TestForceCloseClaimedErrandOnScratchGraph(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("no bd")
	}
	dir := t.TempDir()
	t.Setenv("BEADS_DIR", dir+"/.beads") // the devShell's points at the real graph
	sh := func(args ...string) string {
		c := exec.Command("bd", args...)
		c.Dir = dir
		out, err := c.CombinedOutput()
		if err != nil {
			t.Skipf("bd %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	if err := exec.Command("git", "-C", dir, "init", "-q").Run(); err != nil {
		t.Skip("no git")
	}
	sh("init", "--prefix", "scr", "-q")
	if got := sh("config", "get", "issue_prefix"); !strings.HasPrefix(got, "scr") {
		t.Fatalf("not a scratch graph: %q", got)
	}
	id := sh("create", "hold", "--type", "task", "-l", "kind:errand", "--silent")
	id = strings.TrimSpace(id)
	sh("update", id, "--claim", "--actor", "other")
	t.Chdir(dir)
	if err := Close(id, "obsolete: x closed"); err == nil {
		t.Fatal("a plain Close of another actor's claim must be refused")
	}
	if err := ForceClose(id, "obsolete: x closed"); err != nil {
		t.Fatal(err)
	}
	b, err := Show(id)
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != "closed" || b.CloseReason != "obsolete: x closed" || b.Assignee != "other" {
		t.Fatalf("%+v", b)
	}
}
