package bd

import (
	"slices"
	"strings"
	"testing"
)

// The batch exec is sealed with the rest of the CLI: a script piped to bd batch must never
// reach the live graph while a non-cli core is installed.
func TestSealRefusesBatchExec(t *testing.T) {
	t.Cleanup(Use(nil))
	if err := runBatchOnce("close x\n"); err == nil || !strings.Contains(err.Error(), "batch") || !strings.Contains(err.Error(), "sealed") {
		t.Fatalf("runBatchOnce = %v, want a seal refusal naming batch", err)
	}
}

// The cli Unclaim is CAS-guarded on the pinned actor: bd unclaim <id> --if-assignee <actor>.
func TestCliUnclaimCarriesTheIfAssigneeGuard(t *testing.T) {
	var argv []string
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(args ...string) (string, error) { argv = args; return "", nil }
	t.Cleanup(PinActor("alice"))
	if err := Unclaim("mc-1"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"unclaim", "mc-1", "--if-assignee", "alice"}; !slices.Equal(argv, want) {
		t.Errorf("argv = %v, want %v", argv, want)
	}
}

func TestCliBatchRefusesUnknownVerb(t *testing.T) {
	err := cli{}.Batch([]Op{{Verb: "bogus", ID: "x"}})
	if err == nil || !strings.Contains(err.Error(), "not modelled") {
		t.Fatalf("want not-modelled error, got %v", err)
	}
}
