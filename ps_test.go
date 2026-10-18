package main

import (
	"bytes"
	"encoding/json"
	"io"
	"jayulabs.org/mugicrew/git"
	"os"
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// stubPs points the ps verb's bd read and probe at fakes; the probe records the
// ids it was asked about so the test can assert one call covers every row.
func stubPs(t *testing.T, beads []bd.Bead, alive map[string]bool) *[][]string {
	t.Helper()
	oi, op := inProgress, probe
	t.Cleanup(func() { inProgress, probe = oi, op })
	var calls [][]string
	inProgress = func() ([]bd.Bead, error) { return beads, nil }
	probe = func(ids []string) map[string]bool {
		calls = append(calls, ids)
		out := map[string]bool{}
		for _, id := range ids {
			out[id] = alive[id]
		}
		return out
	}
	return &calls
}

func TestPsJSON(t *testing.T) {
	claimed := time.Now().Add(-3 * time.Hour)
	calls := stubPs(t, []bd.Bead{
		{ID: "mc-b", Kind: contract.KindBuild, Status: "in_progress", Parent: "ws-cp", Title: "a build", Assignee: "loop", Started: claimed},
		{ID: "mc-r", Kind: contract.KindResearch, Status: "in_progress", Parent: "ws-ch"},
		{ID: "mc-z", Kind: contract.KindSpec, Status: "in_progress"},
		{ID: "ws-sig", Kind: contract.KindSignal, Status: "in_progress"}, // HITL claim: no verdict
		{ID: "mc-o", Kind: contract.KindBuild, Status: "open", Parent: "ws-cp"},
		{ID: "mc-x", Status: "in_progress"},                               // non-mugicrew bead
		{ID: "ws-np", Kind: contract.KindCritique, Status: "in_progress"}, // unroutable: no parent edge
	}, map[string]bool{"mc-b": true})

	var out bytes.Buffer
	if err := runPs([]string{"--json"}, &out); err != nil {
		t.Fatalf("ps --json: %v", err)
	}
	var rows []map[string]string
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("ps --json is not a JSON array: %v\n%s", err, out.String())
	}
	got := map[string]map[string]string{}
	for _, r := range rows {
		got[r["id"]] = r
	}
	if len(got) != 4 || got["mc-b"] == nil || got["mc-r"] == nil || got["mc-z"] == nil || got["ws-np"] == nil {
		t.Fatalf("rows = %v, want only the in_progress AFK efforts mc-b, mc-r, mc-z, ws-np", rows)
	}
	for _, k := range []string{"id", "title", "kind", "assignee", "command", "started_at", "age", "liveness"} {
		if _, ok := got["mc-b"][k]; !ok {
			t.Errorf("row missing key %q: %v", k, got["mc-b"])
		}
	}
	b := got["mc-b"]
	if b["liveness"] != "alive" || got["mc-r"]["liveness"] != "dead" {
		t.Errorf("liveness = %q/%q, want alive/dead", b["liveness"], got["mc-r"]["liveness"])
	}
	if b["title"] != "a build" || b["kind"] != "build" || b["assignee"] != "loop" || b["age"] != "3h" {
		t.Errorf("mc-b row = %v", b)
	}
	if b["started_at"] != claimed.UTC().Format(time.RFC3339) || got["mc-z"]["started_at"] != "" || got["mc-z"]["age"] != "—" {
		t.Errorf("started_at/age = %q, %q/%q", b["started_at"], got["mc-z"]["started_at"], got["mc-z"]["age"])
	}
	// Worktree-routed kinds are re-entered via reconcile; repo-root kinds run claude directly.
	if b["command"] != "mugicrew reconcile mc-b" || got["mc-r"]["command"] != "mugicrew reconcile mc-r" {
		t.Errorf("worktree command = %q / %q", b["command"], got["mc-r"]["command"])
	}
	if got["mc-z"]["command"] != "claude -p … --name mc-z" {
		t.Errorf("repo-root command = %q", got["mc-z"]["command"])
	}
	if got["ws-np"]["command"] != "?" {
		t.Errorf("unroutable command = %q, want ?", got["ws-np"]["command"])
	}
	if len(*calls) != 1 || len((*calls)[0]) != 4 {
		t.Errorf("probe calls = %v, want one call over all four efforts", *calls)
	}
}

func TestPsTable(t *testing.T) {
	stubPs(t, []bd.Bead{{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "in_progress", Title: "land it"}}, nil)
	var out bytes.Buffer
	if err := runPs(nil, &out); err != nil {
		t.Fatalf("ps: %v", err)
	}
	s := out.String()
	for _, want := range []string{"LIVENESS", "ws-cp", "dead", "mugicrew reconcile ws-cp", "land it"} {
		if !strings.Contains(s, want) {
			t.Errorf("table missing %q:\n%s", want, s)
		}
	}
}

// No efforts is an empty JSON array, not null — jq-friendly.
func TestPsJSONEmpty(t *testing.T) {
	stubPs(t, nil, nil)
	var out bytes.Buffer
	if err := runPs([]string{"--json"}, &out); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "[]" {
		t.Errorf("empty ps --json = %q, want []", out.String())
	}
}

// A bare shell (no BEADS_DIR) still reads the lock dir the dispatcher took: ps and
// abort resolve the Bead Graph themselves, so the liveness reads see it.
func TestPsAndAbortResolveGraphWithoutBeadsDir(t *testing.T) {
	const graph = "/g/.beads"
	t.Setenv("BEADS_DIR", "")
	orig, origRoot := graphDir, git.RepoRoot
	t.Cleanup(func() { graphDir, git.RepoRoot = orig, origRoot })
	git.RepoRoot = func() (string, error) { return "/w", nil }
	graphDir = func(string) (string, error) { return graph, nil }

	var seen string
	stubPs(t, []bd.Bead{{ID: "mc-b", Kind: contract.KindBuild, Status: "in_progress", Parent: "ws-cp"}}, nil)
	probe = func(ids []string) map[string]bool { seen = os.Getenv("BEADS_DIR"); return nil }
	if err := runPs(nil, io.Discard); err != nil || seen != graph {
		t.Errorf("ps probed under BEADS_DIR %q, %v; want %q", seen, err, graph)
	}

	t.Setenv("BEADS_DIR", "")
	seen = ""
	oa := abortEffort
	t.Cleanup(func() { abortEffort = oa })
	abortEffort = func(string, string) ([]int, bool, error) { seen = os.Getenv("BEADS_DIR"); return nil, true, nil }
	if err := runAbort([]string{"mc-b"}, io.Discard); err != nil || seen != graph {
		t.Errorf("abort ran under BEADS_DIR %q, %v; want %q", seen, err, graph)
	}
}
