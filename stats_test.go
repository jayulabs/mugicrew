package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
)

// stubStatsLog points the stats verb at a temp events.jsonl holding lines.
func stubStatsLog(t *testing.T, lines ...string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "events.jsonl")
	if lines != nil {
		if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := statsLog
	t.Cleanup(func() { statsLog = old })
	statsLog = func() (string, error) { return p, nil }
	stubLineage(t, bd.NewLineage(nil))
}

// stubLineage points the stats verb's bd lineage read at l.
func stubLineage(t *testing.T, l *bd.Lineage) {
	t.Helper()
	old := statsLineage
	t.Cleanup(func() { statsLineage = old })
	statsLineage = func() (*bd.Lineage, error) { return l, nil }
}

// endEv is one end event line; extra is raw JSON attrs appended to the record.
func endEv(at time.Time, msg, bead, parent, extra string) string {
	s := `{"time":"` + at.Format(time.RFC3339Nano) + `","level":"INFO","msg":"` + msg + `","bead":"` + bead + `"`
	if parent != "" {
		s += `,"parent":"` + parent + `"`
	}
	if extra != "" {
		s += "," + extra
	}
	return s + "}"
}

const cost1 = `"dur_ms":60000,"cost_usd":1.5,"api_ms":30000,"models":{"claude-opus":{"cost_usd":1.5,"in":10,"out":20,"cache_read":1000,"cache_write":100}}`

type statsJSON struct {
	Rows  []map[string]any `json:"rows"`
	Total map[string]any   `json:"total"`
}

func runStatsJSON(t *testing.T, argv ...string) statsJSON {
	t.Helper()
	var out bytes.Buffer
	if err := runStats(append(argv, "--json"), &out); err != nil {
		t.Fatalf("stats %v: %v", argv, err)
	}
	var got statsJSON
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	return got
}

func TestStatsChartScopeAndRetries(t *testing.T) {
	now := time.Now()
	stubStatsLog(t,
		endEv(now, "session.ended", "ws-ch", "", cost1),
		endEv(now, "session.failed", "ws-ch.1", "ws-ch", cost1),
		endEv(now, "session.ended", "ws-ch.1", "ws-ch", cost1),       // retry
		endEv(now, "hitl.exited", "ws-ch.1.1", "ws-ch.1", cost1),     // grandchild: counted
		endEv(now, "effort.ended", "ws-ch.1", "ws-ch", `"dur_ms":5`), // not an input
		endEv(now, "session.ended", "ws-other", "", cost1),
		`not json`,
	)
	stubLineage(t, bd.NewLineage([][]string{
		{"e", "ws-ch.1", "ws-ch", "parent-child"},
		{"e", "ws-ch.1.1", "ws-ch.1", "parent-child"},
	}))
	got := runStatsJSON(t, "ws-ch")
	if len(got.Rows) != 3 || got.Rows[0]["key"] != "ws-ch.1" || got.Rows[1]["key"] != "ws-ch" || got.Rows[2]["key"] != "ws-ch.1.1" {
		t.Fatalf("rows = %v, want ws-ch.1 (costliest), ws-ch, then the counted grandchild", got.Rows)
	}
	r := got.Rows[0]
	if r["sessions"] != 2.0 || r["failed"] != 1.0 || r["wall_ms"] != 120000.0 || r["api_ms"] != 60000.0 ||
		r["cost_usd"] != 3.0 || r["cost_partial_sessions"] != 0.0 ||
		r["in"] != 20.0 || r["out"] != 40.0 || r["cache_read"] != 2000.0 || r["cache_write"] != 200.0 {
		t.Errorf("ws-ch.1 row = %v", r)
	}
	if got.Total["key"] != "TOTAL" || got.Total["sessions"] != 4.0 || got.Total["failed"] != 1.0 || got.Total["cost_usd"] != 6.0 {
		t.Errorf("total = %v", got.Total)
	}
}

func TestStatsSince(t *testing.T) {
	now := time.Now()
	stubStatsLog(t,
		endEv(now.Add(-100*time.Hour), "session.ended", "mc-b", "", cost1),
		endEv(now.Add(-time.Hour), "session.ended", "mc-b", "", cost1),
	)
	if got := runStatsJSON(t, "--since", "72h"); got.Total["sessions"] != 1.0 || got.Total["cost_usd"] != 1.5 {
		t.Errorf("--since 72h total = %v, want 1 session", got.Total)
	}
	day := now.Add(-50 * time.Hour).Format("2006-01-02")
	if got := runStatsJSON(t, "mc-b", "--since", day); got.Total["sessions"] != 1.0 {
		t.Errorf("--since %s total = %v, want 1 session", day, got.Total)
	}
	if got := runStatsJSON(t); got.Total["sessions"] != 2.0 {
		t.Errorf("no --since total = %v, want the whole log", got.Total)
	}
}

func TestStatsTablePartialAndSort(t *testing.T) {
	now := time.Now()
	stubStatsLog(t,
		endEv(now, "session.ended", "ws-cheap", "", `"dur_ms":1000,"cost_usd":0.25,"api_ms":500`),
		endEv(now, "session.ended", "ws-dear", "", cost1),
		endEv(now, "hitl.exited", "ws-dear", "", `"dur_ms":3600000`), // pre-enrichment: no cost
	)
	var out bytes.Buffer
	if err := runStats(nil, &out); err != nil {
		t.Fatalf("stats: %v", err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("table = %q", out.String())
	}
	if f := strings.Fields(lines[0]); strings.Join(f, " ") != "BEAD SESSIONS FAILED WALL API COST AVG_COST AVG_API FAIL% IN OUT CACHE_R CACHE_W" {
		t.Errorf("header = %q", lines[0])
	}
	dear, cheap, total := strings.Fields(lines[1]), strings.Fields(lines[2]), strings.Fields(lines[3])
	if dear[0] != "ws-dear" || dear[1] != "2" || dear[3] != "1h1m0s" || strings.Join(dear[5:9], " ") != "$1.50+ $1.50+ 30s 0%" || dear[11] != "1.0k" {
		t.Errorf("ws-dear row = %q", lines[1])
	}
	if cheap[0] != "ws-cheap" || strings.Join(cheap[5:9], " ") != "$0.25 $0.25 1s 0%" {
		t.Errorf("ws-cheap row = %q", lines[2])
	}
	if total[0] != "TOTAL" || strings.Join(total[5:9], " ") != "$1.75+ $0.88+ 15s 0%" {
		t.Errorf("TOTAL row = %q", lines[3])
	}
	if lines[4] != "+ 1 sessions without a cost" {
		t.Errorf("footnote = %q", lines[4])
	}
	// FR9: JSON rows keep their raw-number shape, no derived fields.
	raw := []string{"api_ms", "cache_read", "cache_write", "cost_partial_sessions", "cost_usd", "failed", "in", "key", "out", "sessions", "wall_ms"}
	for _, r := range append(runStatsJSON(t).Rows, runStatsJSON(t).Total) {
		if got := slices.Sorted(maps.Keys(r)); !reflect.DeepEqual(got, raw) {
			t.Errorf("JSON row fields = %v, want %v", got, raw)
		}
	}
}

func TestStatsEmpty(t *testing.T) {
	for name, lines := range map[string][]string{"missing": nil, "empty file": {}, "empty scope": {endEv(time.Now(), "session.ended", "mc-x", "", cost1)}} {
		t.Run(name, func(t *testing.T) {
			stubStatsLog(t, lines...)
			var out bytes.Buffer
			if err := runStats([]string{"ws-none", "--json"}, &out); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), `"rows": []`) || !strings.Contains(out.String(), `"key": "TOTAL"`) {
				t.Errorf("json = %s", out.String())
			}
			out.Reset()
			if err := runStats([]string{"ws-none"}, &out); err != nil {
				t.Fatal(err)
			}
			if lines := strings.Split(strings.TrimSpace(out.String()), "\n"); len(lines) != 2 || !strings.HasPrefix(lines[1], "TOTAL") {
				t.Errorf("table = %q, want header and TOTAL", out.String())
			}
		})
	}
}

func TestStatsUsage(t *testing.T) {
	stubStatsLog(t)
	for _, argv := range [][]string{{"--bogus"}, {"--since"}, {"--since", "yesterday"}, {"a", "b"}} {
		if err := runStats(argv, &bytes.Buffer{}); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Errorf("stats %v err = %v, want usage", argv, err)
		}
	}
}

func TestStatsByModel(t *testing.T) {
	now := time.Now()
	two := `"dur_ms":1000,"cost_usd":3,"api_ms":500,"models":{"opus":{"cost_usd":2,"in":10,"out":20},"haiku":{"cost_usd":1,"in":1,"cache_read":5}}`
	stubStatsLog(t,
		endEv(now, "session.ended", "mc-a", "", two),
		endEv(now, "session.failed", "mc-b", "", `"dur_ms":1000,"models":{"opus":{"in":4}}`), // model entry without a cost
		endEv(now, "hitl.exited", "mc-c", "", `"dur_ms":1000,"cost_usd":0.5`),                // no models: TOTAL only
	)
	got := runStatsJSON(t, "--by", "model")
	if len(got.Rows) != 2 || got.Rows[0]["key"] != "opus" || got.Rows[1]["key"] != "haiku" {
		t.Fatalf("rows = %v, want opus then haiku", got.Rows)
	}
	opus := got.Rows[0]
	if opus["cost_usd"] != 2.0 || opus["cost_partial_sessions"] != 1.0 || opus["in"] != 14.0 || opus["out"] != 20.0 {
		t.Errorf("opus row = %v", opus)
	}
	for _, f := range []string{"sessions", "failed", "wall_ms", "api_ms"} {
		if _, ok := opus[f]; ok {
			t.Errorf("opus row has unsplittable %s: %v", f, opus)
		}
	}
	if got.Total["sessions"] != 3.0 || got.Total["failed"] != 1.0 || got.Total["wall_ms"] != 3000.0 ||
		got.Total["cost_usd"] != 3.5 || got.Total["cost_partial_sessions"] != 1.0 || got.Total["in"] != 15.0 {
		t.Errorf("total = %v", got.Total)
	}
	if def := runStatsJSON(t); !reflect.DeepEqual(def.Total, got.Total) {
		t.Errorf("--by model total %v != default total %v", got.Total, def.Total)
	}

	var out bytes.Buffer
	if err := runStats([]string{"--by", "model"}, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if strings.Fields(lines[0])[0] != "MODEL" {
		t.Errorf("header = %q", lines[0])
	}
	if f := strings.Fields(lines[1]); strings.Join(f[:9], " ") != "opus – – – – $2.00+ – – –" {
		t.Errorf("opus line = %q", lines[1])
	}
	if f := strings.Fields(lines[3]); f[0] != "TOTAL" || f[1] != "3" || f[5] != "$3.50+" {
		t.Errorf("TOTAL line = %q", lines[3])
	}
}

func TestStatsByDayAndCrewmate(t *testing.T) {
	d1 := time.Date(2026, 10, 1, 23, 30, 0, 0, time.Local)
	d2 := d1.Add(time.Hour) // the next local day
	stubStatsLog(t,
		endEv(d1, "session.ended", "mc-a", "", `"crewmate":"franky",`+cost1),
		endEv(d2, "session.ended", "mc-b", "", `"crewmate":"franky",`+cost1),
		endEv(d2, "hitl.exited", "mc-a", "", `"crewmate":"chopper","dur_ms":5,"cost_usd":0.1`),
	)
	got := runStatsJSON(t, "--by", "day")
	if len(got.Rows) != 2 || got.Rows[0]["key"] != "2026-10-02" || got.Rows[0]["sessions"] != 2.0 ||
		got.Rows[1]["key"] != "2026-10-01" || got.Rows[1]["sessions"] != 1.0 {
		t.Errorf("--by day rows = %v", got.Rows)
	}
	got = runStatsJSON(t, "--by", "crewmate")
	if len(got.Rows) != 2 || got.Rows[0]["key"] != "franky" || got.Rows[0]["cost_usd"] != 3.0 ||
		got.Rows[1]["key"] != "chopper" || got.Rows[1]["wall_ms"] != 5.0 {
		t.Errorf("--by crewmate rows = %v", got.Rows)
	}
	var out bytes.Buffer
	if err := runStats([]string{"--by", "crewmate"}, &out); err != nil || !strings.HasPrefix(out.String(), "CREWMATE ") {
		t.Errorf("--by crewmate table = %q, %v", out.String(), err)
	}
}

func TestStatsByProfile(t *testing.T) {
	at := time.Now()
	stubStatsLog(t,
		endEv(at, "session.ended", "mc-a", "", `"profile":"local-qwen",`+cost1),
		endEv(at, "session.failed", "mc-b", "", `"profile":"local-qwen","dur_ms":5`),
		endEv(at, "hitl.exited", "mc-c", "", `"dur_ms":1000,"cost_usd":0.1`),
	)
	got := runStatsJSON(t, "--by", "profile")
	if len(got.Rows) != 2 || got.Rows[0]["key"] != "local-qwen" || got.Rows[0]["sessions"] != 2.0 || got.Rows[0]["failed"] != 1.0 ||
		got.Rows[1]["key"] != "(built-in)" || got.Rows[1]["wall_ms"] != 1000.0 {
		t.Errorf("--by profile rows = %v", got.Rows)
	}
	var out bytes.Buffer
	if err := runStats([]string{"--by", "profile"}, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out.String(), "\n")
	if !strings.HasPrefix(lines[0], "PROFILE ") || strings.Join(strings.Fields(lines[1])[:6], " ") != "local-qwen 2 1 1m0s 30s $1.50+" ||
		strings.Join(strings.Fields(lines[2])[:6], " ") != "(built-in) 1 0 1s 0s $0.10" {
		t.Errorf("--by profile table: want full cells and (built-in):\n%s", out.String())
	}
}

func TestStatsByWithScopeAndSince(t *testing.T) {
	now := time.Now()
	stubStatsLog(t,
		endEv(now, "session.ended", "ws-ch.1", "ws-ch", cost1),
		endEv(now.Add(-100*time.Hour), "session.ended", "ws-ch.1", "ws-ch", cost1), // outside --since
		endEv(now, "session.ended", "ws-other", "", cost1),                         // outside scope
	)
	stubLineage(t, bd.NewLineage([][]string{{"e", "ws-ch.1", "ws-ch", "parent-child"}}))
	got := runStatsJSON(t, "ws-ch", "--since", "72h", "--by", "model")
	if len(got.Rows) != 1 || got.Rows[0]["key"] != "claude-opus" || got.Rows[0]["cost_usd"] != 1.5 || got.Total["sessions"] != 1.0 {
		t.Errorf("rows = %v total = %v", got.Rows, got.Total)
	}
}

func TestStatsByUsage(t *testing.T) {
	stubStatsLog(t)
	for _, argv := range [][]string{{"--by", "model", "--by", "day"}, {"--by", "bead"}, {"--by"}} {
		if err := runStats(argv, &bytes.Buffer{}); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Errorf("stats %v err = %v, want usage", argv, err)
		}
	}
}

func TestStatsKind(t *testing.T) {
	now := time.Now()
	stubStatsLog(t,
		endEv(now, "session.ended", "mc-a", "", `"kind":"build","profile":"opus",`+cost1),
		endEv(now, "session.failed", "mc-b", "", `"kind":"build","profile":"sonnet","dur_ms":5,"cost_usd":0.5`),
		endEv(now, "session.ended", "mc-c", "", `"kind":"review","profile":"opus","dur_ms":7,"cost_usd":1`),
		endEv(now, "hitl.exited", "mc-d", "", `"profile":"opus","dur_ms":1000,"cost_usd":0.25`), // pre-kind
	)
	got := runStatsJSON(t, "--by", "kind")
	if len(got.Rows) != 3 || got.Rows[0]["key"] != "build" || got.Rows[0]["sessions"] != 2.0 ||
		got.Rows[2]["key"] != "(unknown)" || got.Total["sessions"] != 4.0 {
		t.Errorf("--by kind = %v total %v", got.Rows, got.Total)
	}
	got = runStatsJSON(t, "--kind", "build", "--by", "profile")
	if len(got.Rows) != 2 || got.Rows[0]["key"] != "opus" || got.Rows[1]["key"] != "sonnet" || got.Rows[1]["failed"] != 1.0 ||
		got.Total["sessions"] != 2.0 || got.Total["cost_usd"] != 2.0 {
		t.Errorf("--kind build --by profile = %v total %v", got.Rows, got.Total)
	}
	if got = runStatsJSON(t, "mc-c", "--kind", "review", "--since", "1h"); got.Total["sessions"] != 1.0 {
		t.Errorf("mc-c --kind review --since 1h total = %v", got.Total)
	}
	if got = runStatsJSON(t, "--kind", "nosuchkind"); got.Total["sessions"] != 0.0 || len(got.Rows) != 0 {
		t.Errorf("--kind nosuchkind = %v total %v, want nothing", got.Rows, got.Total)
	}

	var out bytes.Buffer
	if err := runStats([]string{"--by", "kind"}, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out.String(), "\n")
	if !strings.HasPrefix(lines[0], "KIND ") || strings.Fields(lines[1])[0] != "build" || strings.Fields(lines[3])[0] != "(unknown)" {
		t.Errorf("--by kind table:\n%s", out.String())
	}
	out.Reset()
	if err := runStats([]string{"--kind", "build"}, &out); err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(out.String()), "\n"); len(lines) != 4 || strings.Fields(lines[3])[1] != "2" {
		t.Errorf("--kind build table:\n%s", out.String())
	}

	for _, argv := range [][]string{{"--kind", "build", "--kind", "review"}, {"--kind"}, {"--by", "kinds"}} {
		if err := runStats(argv, &bytes.Buffer{}); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Errorf("stats %v err = %v, want usage", argv, err)
		}
	}
}

func TestStatsLineageSeam(t *testing.T) {
	stubStatsLog(t, endEv(time.Now(), "session.ended", "mc-gone", "", cost1))
	// An unknown bead scopes to itself alone.
	if got := runStatsJSON(t, "mc-gone"); got.Total["sessions"] != 1.0 {
		t.Errorf("unknown bead total = %v, want itself summed", got.Total)
	}
	// A failed bd read exits 1 with bd's error, no fallback to the log.
	old := statsLineage
	t.Cleanup(func() { statsLineage = old })
	statsLineage = func() (*bd.Lineage, error) { return nil, errors.New("bd: boom") }
	if err := runStats([]string{"mc-gone"}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "bd: boom") {
		t.Errorf("bd failure err = %v, want bd's error", err)
	}
	// No bead means no bd call.
	statsLineage = func() (*bd.Lineage, error) { t.Fatal("lineage read with no bead"); return nil, nil }
	if got := runStatsJSON(t); got.Total["sessions"] != 1.0 {
		t.Errorf("whole-log total = %v", got.Total)
	}
}

// spineFixture: sig -> ch (with ch.1) -> sp -> cp (no sessions); cp has a pivot pv.
func spineFixture(t *testing.T) {
	now := time.Now()
	stubStatsLog(t,
		endEv(now, "session.ended", "sig", "", cost1),
		endEv(now, "session.ended", "ch.1", "ch", cost1),
		endEv(now, "session.ended", "sp", "", cost1),
		endEv(now, "session.ended", "pv", "", cost1),
		endEv(now, "session.ended", "other", "", cost1),
	)
	stubLineage(t, bd.NewLineage([][]string{
		{"b", "sig", "kind:signal", "1"}, {"b", "ch", "kind:chart", "2"}, {"b", "sp", "kind:spec", "3"},
		{"b", "cp", "kind:checkpoint", "4"}, {"b", "pv", "kind:pivot", "5"},
		{"e", "ch", "sig", "caused-by"}, {"e", "sp", "ch", "caused-by"}, {"e", "cp", "sp", "caused-by"},
		{"e", "ch.1", "ch", "parent-child"}, {"e", "cp", "pv", "blocks"},
	}))
}

func TestStatsSpine(t *testing.T) {
	spineFixture(t)
	got := runStatsJSON(t, "ch.1", "--spine")
	var keys, kinds []any
	for _, r := range got.Rows {
		keys, kinds = append(keys, r["key"]), append(kinds, r["kind"])
	}
	if !reflect.DeepEqual(keys, []any{"sig", "ch", "sp", "cp", "pv"}) || !reflect.DeepEqual(kinds, []any{"signal", "chart", "spec", "checkpoint", "pivot"}) {
		t.Fatalf("rows = %v %v, want spine order with kinds", keys, kinds)
	}
	if got.Rows[1]["sessions"] != 1.0 || got.Rows[3]["sessions"] != 0.0 || got.Total["sessions"] != 4.0 || got.Total["key"] != "TOTAL" {
		t.Errorf("rows/total = %v / %v: ch counts its child, empty cp kept, other excluded", got.Rows, got.Total)
	}
	var out bytes.Buffer
	if err := runStats([]string{"sp", "--spine"}, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if f := strings.Fields(lines[0]); f[0] != "BEAD" || f[1] != "KIND" || len(lines) != 7 || strings.Fields(lines[6])[0] != "TOTAL" {
		t.Errorf("table = %q", out.String())
	}
}

func TestStatsSpineBy(t *testing.T) {
	spineFixture(t)
	got := runStatsJSON(t, "sp", "--spine", "--by", "kind")
	if got.Total["sessions"] != 4.0 || len(got.Rows) != 1 || got.Rows[0]["kind"] != nil {
		t.Errorf("--spine --by = %v total %v, want group rows over the whole set, no kind", got.Rows, got.Total)
	}
}

func TestStatsSpineErrors(t *testing.T) {
	spineFixture(t)
	for _, argv := range [][]string{{"--spine"}, {"gone", "--spine"}} {
		err := runStats(argv, &bytes.Buffer{})
		var ec exitCode
		if !errors.As(err, &ec) || ec.code != 2 {
			t.Errorf("stats %v err = %v, want exit 2", argv, err)
		}
	}
	if err := runStats([]string{"gone", "--spine"}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "gone is not on a spine") {
		t.Errorf("err = %v", err)
	}
	statsLineage = func() (*bd.Lineage, error) { return nil, errors.New("bd down") }
	if err := runStats([]string{"sp"}, &bytes.Buffer{}); err == nil || errors.As(err, new(exitCode)) {
		t.Errorf("bd failure err = %v, want plain (exit 1)", err)
	}
	statsLineage = func() (*bd.Lineage, error) { t.Fatal("no bead must not read bd"); return nil, nil }
	if err := runStats(nil, &bytes.Buffer{}); err != nil {
		t.Error(err)
	}
}
