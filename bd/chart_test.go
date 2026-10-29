package bd

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

const chartBody = `## Destination

Ship X.

## Notes

- read ADR 0001

## Decisions-so-far

- mc-c.1: use a lock

## Not-yet-specified

- how retries back off

## Out-of-scope
`

// fakeChart stands a one-bead store behind run: show returns the body, update
// --description replaces it. Each call is atomic, the read-then-write is not, so
// only the land lock keeps concurrent edits from losing a line.
func fakeChart(t *testing.T, body string) *string {
	t.Helper()
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	t.Chdir(repo) // the land lock lives under the repo's common dir
	orun := run
	t.Cleanup(func() { run = orun })
	var mu sync.Mutex
	run = func(args ...string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case args[0] == "show":
			j, _ := json.Marshal([]map[string]any{{"id": args[1], "status": "open", "labels": []string{"kind:chart"}, "description": body}})
			mu.Unlock()
			time.Sleep(2 * time.Millisecond) // widen the read-then-write window
			mu.Lock()
			return string(j), nil
		case args[0] == "update" && args[2] == "--description":
			body = args[3]
			return "", nil
		}
		return "", fmt.Errorf("unexpected bd %q", args)
	}
	return &body
}

// Spec D9: an edit to one section leaves every other section
// byte-identical, and drop removes exactly the named line.
func TestChartEditTouchesOneSection(t *testing.T) {
	body := fakeChart(t, chartBody)
	if err := EditChartBody("mc-c", "Decisions-so-far", "- mc-c.2: no retries", false); err != nil {
		t.Fatal(err)
	}
	if err := EditChartBody("mc-c", "Not-yet-specified", "- how retries back off", true); err != nil {
		t.Fatal(err)
	}
	if err := EditChartBody("mc-c", "Out-of-scope", "- Windows: no flock", false); err != nil {
		t.Fatal(err)
	}
	want := `## Destination

Ship X.

## Notes

- read ADR 0001

## Decisions-so-far

- mc-c.1: use a lock
- mc-c.2: no retries

## Not-yet-specified

## Out-of-scope

- Windows: no flock
`
	if *body != want {
		t.Errorf("body =\n%s\nwant\n%s", *body, want)
	}
}

// An unknown section, a missing line to drop, or a multi-line line is a loud
// error, and nothing is written.
func TestChartEditRefusesWithoutWriting(t *testing.T) {
	body := fakeChart(t, chartBody)
	for _, c := range []struct {
		section, line string
		drop          bool
	}{
		{"Decisions", "- x", false},
		{"Not-yet-specified", "- not there", true},
		{"Decisions-so-far", "- a\n- b", false},
		{"Decisions-so-far", " ", false},
	} {
		if err := EditChartBody("mc-c", c.section, c.line, c.drop); err == nil {
			t.Errorf("EditChartBody(%q, %q, drop=%v) = nil, want an error", c.section, c.line, c.drop)
		}
	}
	if *body != chartBody {
		t.Errorf("a refused edit wrote the body:\n%s", *body)
	}
}

// Spec FR 34: concurrent appends from N goroutines lose no line.
func TestChartAppendConcurrentLosesNothing(t *testing.T) {
	body := fakeChart(t, chartBody)
	const n = 12
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := EditChartBody("mc-c", "Decisions-so-far", fmt.Sprintf("- mc-c.%d: d", 10+i), false); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for i := range n {
		if line := fmt.Sprintf("- mc-c.%d: d", 10+i); !strings.Contains(*body, line+"\n") {
			t.Errorf("lost %q:\n%s", line, *body)
		}
	}
}

// ADR 0357: chart finish stamps only a Chart whose own claimed Survey settled it;
// each refusal is loud and writes nothing, and a re-run on a stamped Chart is a no-op.
func TestFinishChart(t *testing.T) {
	const body = "## Destination\n\nShip X.\n\n## Decisions-so-far\n\n- gist (mc-s)\n\n## Not-yet-specified\n\n## Out-of-scope\n"
	type row struct {
		name   string
		mutate func(chart, survey, other map[string]any)
		other  bool // a third child exists
		want   string
	}
	rows := []row{
		{name: "stamps"},
		{name: "already stamped", mutate: func(c, _, _ map[string]any) { c["labels"] = []string{"kind:chart", "chart:defogged"} }},
		{name: "foreign survey", mutate: func(_, s, _ map[string]any) { s["parent"] = "mc-other" }, want: "not a child"},
		{name: "not a survey", mutate: func(_, s, _ map[string]any) { s["labels"] = []string{"kind:grilling"} }, want: "not a survey"},
		{name: "unclaimed survey", mutate: func(_, s, _ map[string]any) { s["assignee"] = "" }, want: "claimed"},
		{name: "survey not in progress", mutate: func(_, s, _ map[string]any) { s["status"] = "open" }, want: "claimed"},
		{name: "closed chart", mutate: func(c, _, _ map[string]any) { c["status"] = "closed" }, want: "open chart"},
		{name: "unclosed child", other: true, want: "unclosed children"},
		{name: "unclosed draft", other: true, mutate: func(_, _, o map[string]any) { o["status"] = "deferred" }, want: "unclosed children"},
		{name: "fog written", mutate: func(c, _, _ map[string]any) {
			c["description"] = strings.Replace(body, "## Not-yet-specified\n", "## Not-yet-specified\n\n- how?\n", 1)
		}, want: "Not-yet-specified"},
		{name: "empty notes", mutate: func(_, s, _ map[string]any) { s["notes"] = " " }, want: "no notes"},
		{name: "no decision line", mutate: func(c, _, _ map[string]any) { c["description"] = strings.Replace(body, "(mc-s)", "", 1) }, want: "Decisions-so-far"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			repo := t.TempDir()
			if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
				t.Fatalf("git init: %v %s", err, out)
			}
			t.Chdir(repo)
			chart := map[string]any{"id": "mc-c", "status": "open", "labels": []string{"kind:chart"}, "description": body}
			survey := map[string]any{"id": "mc-s", "status": "in_progress", "assignee": "nami", "parent": "mc-c", "labels": []string{"kind:survey"}, "notes": "settled"}
			other := map[string]any{"id": "mc-o", "status": "open", "parent": "mc-c", "labels": []string{"kind:grilling"}}
			if r.mutate != nil {
				r.mutate(chart, survey, other)
			}
			kids := []map[string]any{survey}
			if r.other {
				kids = append(kids, other)
			}
			var writes [][]string
			orun := run
			t.Cleanup(func() { run = orun })
			run = func(args ...string) (string, error) {
				var v any
				switch {
				case args[0] == "show" && args[1] == "mc-c":
					v = []any{chart}
				case args[0] == "show" && args[1] == "mc-s":
					v = []any{survey}
				case args[0] == "list" || args[0] == "children":
					v = kids
				default:
					writes = append(writes, args)
					return "", nil
				}
				j, _ := json.Marshal(v)
				return string(j), nil
			}
			err := FinishChart("mc-c", "mc-s")
			if r.want != "" {
				if err == nil || !strings.Contains(err.Error(), r.want) {
					t.Fatalf("FinishChart = %v, want an error containing %q", err, r.want)
				}
				if len(writes) != 0 {
					t.Errorf("a refusal wrote %q", writes)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if r.name == "stamps" && !slices.Equal(slices.Concat(writes...), []string{"update", "mc-c", "--add-label", "chart:defogged"}) {
				t.Errorf("writes = %q, want one add-label chart:defogged", writes)
			}
			if r.name == "already stamped" && len(writes) != 0 {
				t.Errorf("an already-stamped Chart wrote %q", writes)
			}
		})
	}
}
