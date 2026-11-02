package bd

import (
	"slices"
	"strconv"
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

func TestDepArgRendersBlocksReversedAndRefusesFrom(t *testing.T) {
	for _, tc := range []struct {
		d    Edge
		want string
	}{
		{Edge{To: "x", Type: "blocks"}, "blocked-by:x"},
		{Edge{To: "x"}, "blocked-by:x"},
		{Edge{To: "x", Type: "conditional-blocks"}, "conditional-blocks:x"},
		{Edge{To: "x", Type: "caused-by"}, "caused-by:x"},
		{Edge{To: "x", Type: "discovered-from"}, "discovered-from:x"},
	} {
		if got, err := depArg(tc.d); err != nil || got != tc.want {
			t.Errorf("depArg(%+v) = %q, %v; want %q", tc.d, got, err, tc.want)
		}
	}
	if _, err := depArg(Edge{From: "y", To: "x", Type: "related"}); err == nil {
		t.Error("depArg with From set succeeded, want a refusal")
	}
}

func TestCLICreateRendersDepsAsOneEntryEach(t *testing.T) {
	orun := run
	t.Cleanup(func() { run = orun })
	var got []string
	run = func(args ...string) (string, error) { got = args; return "x-1", nil }
	_, err := cli{}.Create(CreateSpec{Title: "t", Deps: []Edge{{To: "a", Type: "blocks"}, {To: "b", Type: "caused-by"}}})
	if err != nil || !slices.Contains(got, "blocked-by:a,caused-by:b") {
		t.Errorf("argv = %v, %v; want --deps blocked-by:a,caused-by:b", got, err)
	}
	called := false
	run = func(args ...string) (string, error) { called = true; return "", nil }
	if _, err := (cli{}).Create(CreateSpec{Title: "t", Deps: []Edge{{From: "z", To: "a", Type: "related"}}}); err == nil || called {
		t.Errorf("Create with From set = %v, exec %v; want a refusal before bd runs", err, called)
	}
}

func TestCLICreatePassesPriorityOnlyWhenSet(t *testing.T) {
	orun := run
	t.Cleanup(func() { run = orun })
	var got []string
	run = func(args ...string) (string, error) { got = args; return "x-1", nil }
	for _, p := range []int{0, 2, 4} {
		if _, err := (cli{}).Create(CreateSpec{Title: "t", Priority: &p}); err != nil {
			t.Fatal(err)
		}
		i := slices.Index(got, "--priority")
		if i < 0 || i+1 >= len(got) || got[i+1] != strconv.Itoa(p) {
			t.Errorf("Priority P%d: argv = %v; want --priority %d", p, got, p)
		}
	}
	if _, err := (cli{}).Create(CreateSpec{Title: "t"}); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(got, "--priority") {
		t.Errorf("unset Priority: argv = %v; want no --priority", got)
	}
}
