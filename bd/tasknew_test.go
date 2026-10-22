package bd

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

// taskWorld fakes bd for NewTask: beads by id as `bd show` rows, a Rollback's
// related Checkpoint, and a create that records its argv or fails with createErr.
func taskWorld(t *testing.T, createErr error) *[][]string {
	t.Helper()
	row := func(id, kind, status, parent string) map[string]any {
		typ := "task"
		if kind == "initiative" || kind == "checkpoint" || kind == "chart" {
			typ = "epic"
		}
		return map[string]any{"id": id, "title": "T-" + id, "status": status, "parent": parent,
			"labels": []string{"kind:" + kind}, "issue_type": typ}
	}
	world := map[string]map[string]any{
		"i1":     row("i1", "initiative", "open", ""),
		"i2":     row("i2", "initiative", "open", ""),
		"iclose": row("iclose", "initiative", "closed", ""),
		"cp":     row("cp", "checkpoint", "open", "i1"),
		"ch":     row("ch", "chart", "open", "i1"),
		"spec":   row("spec", "spec", "open", "i1"),
		"task":   row("task", "task", "open", "i1"),
		"cp2":    row("cp2", "checkpoint", "open", "i2"),
		"cpc":    row("cpc", "checkpoint", "closed", "iclose"),
		"rb":     row("rb", "rollback", "open", ""),
		"rbc":    row("rbc", "rollback", "open", ""),
		"bare":   row("bare", "checkpoint", "open", ""),
	}
	related := map[string]string{"rb": "cp", "rbc": "cpc"}
	var creates [][]string
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(args ...string) (string, error) {
		switch args[0] {
		case "show":
			r, ok := world[args[1]]
			if !ok {
				return "", errors.New("not found")
			}
			out, _ := json.Marshal([]any{r})
			return string(out), nil
		case "dep":
			if args[1] == "list" && slices.Contains(args, "related") {
				if to, ok := related[args[2]]; ok {
					out, _ := json.Marshal([]any{world[to]})
					return string(out), nil
				}
				return "[]", nil
			}
		case "create":
			if createErr != nil {
				return "", createErr
			}
			creates = append(creates, args)
			return "mc-new", nil
		}
		return "", errors.New("unexpected bd call " + strings.Join(args, " "))
	}
	return &creates
}

func argAfter(args []string, flag string) string {
	if i := slices.Index(args, flag); i >= 0 && i+1 < len(args) {
		return args[i+1]
	}
	return ""
}

// NewTask climbs each order target to its Initiative and joins only when exactly
// one open Initiative results; every other shape is born outside with its reason
// (ADR 0350, ADR 0348).
func TestNewTaskPlacement(t *testing.T) {
	for _, tc := range []struct {
		name          string
		after, before []string
		outside       bool
		parent, line  string
	}{
		{"after a Checkpoint", []string{"cp"}, nil, false, "i1", "Initiative: T-i1"},
		{"before a Chart", nil, []string{"ch"}, false, "i1", "Initiative: T-i1"},
		{"after a Spec", []string{"spec"}, nil, false, "i1", "Initiative: T-i1"},
		{"after a member Task", []string{"task"}, nil, false, "i1", "Initiative: T-i1"},
		{"after a Rollback", []string{"rb"}, nil, false, "i1", "Initiative: T-i1"},
		{"two targets, one Initiative", []string{"cp"}, []string{"task"}, false, "i1", "Initiative: T-i1"},
		{"two Initiatives", []string{"cp"}, []string{"cp2"}, false, "", "Initiative: outside (ordered against members of T-i1 and T-i2)"},
		{"open Initiative beside a closed one", []string{"cp"}, []string{"cpc"}, false, "i1", "Initiative: T-i1"},
		{"closed Initiative", []string{"cpc"}, nil, false, "", "Initiative: outside (T-iclose is closed)"},
		{"closed Initiative via Rollback", []string{"rbc"}, nil, false, "", "Initiative: outside (T-iclose is closed)"},
		{"target with no Initiative", []string{"bare"}, nil, false, "", "Initiative: outside (T-bare has no Initiative)"},
		{"unordered", nil, nil, false, "", "Initiative: outside (unordered)"},
		{"kept outside wins", []string{"cp"}, nil, true, "", "Initiative: outside (kept outside)"},
	} {
		creates := taskWorld(t, nil)
		id, line, err := NewTask(TaskFiling{Title: "Do it", Body: "Lead.\n\n## Steps\n\n1. x\n", DraftedBy: "me", After: tc.after, Before: tc.before, Outside: tc.outside})
		if err != nil || id != "mc-new" || line != tc.line {
			t.Errorf("%s: NewTask = (%q, %q, %v), want (mc-new, %q, nil)", tc.name, id, line, err, tc.line)
			continue
		}
		args := (*creates)[0]
		if got := argAfter(args, "--parent"); got != tc.parent {
			t.Errorf("%s: --parent = %q, want %q (%v)", tc.name, got, tc.parent, args)
		}
		if body := argAfter(args, "--description"); body != "Lead.\n"+tc.line+"\n\n## Steps\n\n1. x\n" {
			t.Errorf("%s: body = %q, want the fact line first after the lead", tc.name, body)
		}
	}
}

// One bd create carries kind:task, the Draft label and status, --no-inherit-labels and
// every order edge beside the provenance edge.
func TestNewTaskCreateArgv(t *testing.T) {
	creates := taskWorld(t, nil)
	if _, _, err := NewTask(TaskFiling{Title: "Do it", Body: "Lead.", DraftedBy: "me", After: []string{"cp"}, Before: []string{"task"}}); err != nil {
		t.Fatal(err)
	}
	if len(*creates) != 1 {
		t.Fatalf("creates = %d, want exactly one", len(*creates))
	}
	args := (*creates)[0]
	deps := strings.Split(argAfter(args, "--deps"), ",")
	for _, want := range []string{"blocked-by:cp", "blocks:task", "discovered-from:me"} {
		if !slices.Contains(deps, want) {
			t.Errorf("--deps %v lacks %q", deps, want)
		}
	}
	if got := argAfter(args, "--labels"); got != "kind:task,drafted-by:me" {
		t.Errorf("--labels = %q", got)
	}
	if argAfter(args, "--status") != "deferred" || argAfter(args, "--type") != "task" || !slices.Contains(args, "--no-inherit-labels") {
		t.Errorf("argv = %v: want type task, status deferred, --no-inherit-labels", args)
	}
}

// A body that opens at ## Steps still gets the fact line first, ahead of it.
func TestNewTaskBodyWithoutLead(t *testing.T) {
	creates := taskWorld(t, nil)
	if _, _, err := NewTask(TaskFiling{Title: "t", Body: "## Steps\n\n1. x\n", DraftedBy: "me", After: []string{"cp"}}); err != nil {
		t.Fatal(err)
	}
	if got, want := argAfter((*creates)[0], "--description"), "Initiative: T-i1\n\n## Steps\n\n1. x\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

// A fact line already in the body stays after the Initiative line, which comes first.
func TestNewTaskBodyWithFacts(t *testing.T) {
	creates := taskWorld(t, nil)
	if _, _, err := NewTask(TaskFiling{Title: "t", Body: "Lead.\nKey: v\n\n## Steps\n", DraftedBy: "me", After: []string{"cp"}}); err != nil {
		t.Fatal(err)
	}
	if got, want := argAfter((*creates)[0], "--description"), "Lead.\nInitiative: T-i1\nKey: v\n\n## Steps\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

// A refused edge returns bd's error, with no second, unordered create.
func TestNewTaskRefusedEdge(t *testing.T) {
	creates := taskWorld(t, errors.New("would create a cycle"))
	id, line, err := NewTask(TaskFiling{Title: "t", Body: "b", DraftedBy: "me", After: []string{"cp"}})
	if err == nil || !strings.Contains(err.Error(), "cycle") || id != "" || line != "" || len(*creates) != 0 {
		t.Errorf("NewTask = (%q, %q, %v), creates %d; want the cycle error and nothing filed", id, line, err, len(*creates))
	}
}

// A Hold Task joins the Initiative of the container it holds (a Rollback climbs through
// its rolled-back Checkpoint) and is born outside, with its reason, otherwise; the
// container stays its single blocks target.
func TestSpawnBlockingTaskPlacement(t *testing.T) {
	for name, c := range map[string]struct {
		container, parent, line string
	}{
		"member Checkpoint": {"cp", "i1", "Initiative: T-i1"},
		"member Chart":      {"ch", "i1", "Initiative: T-i1"},
		"open Rollback":     {"rb", "i1", "Initiative: T-i1"},
		"closed Rollback":   {"rbc", "", "Initiative: outside (T-iclose is closed)"},
		"non-member":        {"bare", "", "Initiative: outside (T-bare has no Initiative)"},
	} {
		creates := taskWorld(t, nil)
		fake := run
		run = func(args ...string) (string, error) {
			if args[0] == "dep" && args[1] == "list" && slices.Contains(args, "blocks") {
				return "[]", nil
			}
			return fake(args...)
		}
		id, created, err := SpawnBlockingTaskIfAbsent(c.container, "Closed MR: !3\n", "t", "Closed MR: !3\nFix: reopen.\n")
		if err != nil || !created || id != "mc-new" || len(*creates) != 1 {
			t.Fatalf("%s: got %q %v %v, %d creates", name, id, created, err, len(*creates))
		}
		args := (*creates)[0]
		if got := argAfter(args, "--parent"); got != c.parent {
			t.Errorf("%s: parent = %q, want %q", name, got, c.parent)
		}
		if got := argAfter(args, "--deps"); got != "blocks:"+c.container {
			t.Errorf("%s: deps = %q", name, got)
		}
		if body := argAfter(args, "--description"); !strings.HasPrefix(body, c.line+"\n") || !strings.Contains(body, "Closed MR: !3\n") {
			t.Errorf("%s: body = %q, want fact line %q", name, body, c.line)
		}
	}
}

// An existing Hold Task is found by (container, key) whatever its parent, so a re-run
// files no duplicate.
func TestSpawnBlockingTaskReRunFindsAParentedHold(t *testing.T) {
	taskWorld(t, nil)
	fake := run
	run = func(args ...string) (string, error) {
		switch {
		case args[0] == "create":
			t.Errorf("filed a duplicate: %v", args)
		case args[0] == "dep" && slices.Contains(args, "blocks"):
			return `[{"id":"held","status":"open","parent":"i1","labels":["kind:task"],"description":"Initiative: T-i1\nClosed MR: !3\n"}]`, nil
		}
		return fake(args...)
	}
	id, created, err := SpawnBlockingTaskIfAbsent("cp", "Closed MR: !3\n", "t", "b")
	if err != nil || created || id != "held" {
		t.Errorf("got %q %v %v", id, created, err)
	}
}

// A body that opens with a fact line has no lead: the Initiative line goes above it.
func TestNewTaskBodyOpeningWithFact(t *testing.T) {
	creates := taskWorld(t, nil)
	if _, _, err := NewTask(TaskFiling{Title: "t", Body: "Key: v\n\n## Steps\n", DraftedBy: "me", After: []string{"cp"}}); err != nil {
		t.Fatal(err)
	}
	if got, want := argAfter((*creates)[0], "--description"), "Initiative: T-i1\nKey: v\n\n## Steps\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}
