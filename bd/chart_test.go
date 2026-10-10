package bd

import (
	"encoding/json"
	"fmt"
	"os/exec"
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
