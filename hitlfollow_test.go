package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"jayulabs.org/mugicrew/claudecode"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
)

// hitl-follow writes one hitl.continued: the remainder over the baseline on a
// known cost, and the unknown-cost reasons (Spec mc-11b8 D5–D7, ADR 0254).
func TestHitlFollow(t *testing.T) {
	exited := time.Now().UTC()
	at := func(s int) string { return exited.Add(time.Duration(s) * time.Second).Format(time.RFC3339Nano) }
	costLine := func(usd float64, api int, model float64) string {
		return `{"type":"cost-state","totalCostUSD":` + ftoa(usd) + `,"totalAPIDuration":` + itoa(api) +
			`,"modelUsage":{"m":{"costUSD":` + ftoa(model) + `,"inputTokens":100,"outputTokens":10}}}`
	}
	msg := func(ts string) string { return `{"type":"assistant","timestamp":"` + ts + `"}` }
	hop := func(ts, to string) string {
		return `{"type":"continued-in","timestamp":"` + ts + `","continuedInSessionId":"` + to + `"}`
	}
	base, _ := json.Marshal(contract.Cost{Known: true, USD: 1, APIms: 1000,
		Models: map[string]contract.ModelCost{"m": {USD: 1, In: 40, Out: 4}}})

	run := func(t *testing.T, files map[string][]string, grow map[string]string) map[string]any {
		cfg := t.TempDir()
		t.Setenv("CLAUDE_CONFIG_DIR", cfg)
		os.MkdirAll(filepath.Join(cfg, "projects", "p"), 0o755)
		write := func(id string, lines ...string) {
			b := []byte{}
			for _, l := range lines {
				b = append(b, l+"\n"...)
			}
			os.WriteFile(filepath.Join(cfg, "projects", "p", id+".jsonl"), b, 0o644)
		}
		for id, lines := range files {
			write(id, lines...)
		}
		for id, line := range grow { // the destination's cost-state lands while the follower waits
			go func() {
				time.Sleep(60 * time.Millisecond)
				f, _ := os.OpenFile(filepath.Join(cfg, "projects", "p", id+".jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
				f.WriteString(line + "\n")
				f.Close()
			}()
		}
		log := filepath.Join(t.TempDir(), "events.jsonl")
		var out bytes.Buffer
		err := runHitlFollow([]string{"--bead=mc-x", "--session=src", "--kind=build", "--continued_in=d1",
			"--exited_at=" + exited.Format(time.RFC3339Nano), "--events=" + log, "--baseline=" + string(base)},
			claudecode.FollowWait{Idle: 400 * time.Millisecond, Ceiling: time.Hour, Poll: 5 * time.Millisecond}, &out)
		if err != nil {
			t.Fatal(err)
		}
		evs, _, _ := eventlog.Read(log)
		if len(evs) != 1 || evs[0].Msg != "hitl.continued" {
			t.Fatalf("events = %+v", evs)
		}
		return evs[0].Raw
	}
	num := func(r map[string]any, k string) float64 { f, _ := r[k].(json.Number).Float64(); return f }

	t.Run("known", func(t *testing.T) {
		r := run(t, map[string][]string{"d1": {msg(at(10)), msg(at(30))}}, map[string]string{"d1": costLine(3, 5000, 3)})
		if r["bead"] != "mc-x" || r["session"] != "src" || r["continued_in"] != "d1" || r["cost_known"] != true {
			t.Errorf("attrs = %v", r)
		}
		if num(r, "cost_usd") != 2 || num(r, "api_ms") != 4000 || num(r, "tail_ms") != 30000 {
			t.Errorf("cost/tail = %v", r)
		}
		if m := r["models"].(map[string]any)["m"].(map[string]any); num(m, "in") != 60 || num(m, "cost_usd") != 2 {
			t.Errorf("models = %v", r["models"])
		}
		if _, ok := r["dur_ms"]; ok {
			t.Error("dur_ms written")
		}
	})
	t.Run("two-hop chain", func(t *testing.T) {
		r := run(t, map[string][]string{
			"d1": {msg(at(10)), costLine(2, 2000, 2), hop(at(20), "d2")},
			"d2": {msg(at(50)), msg(at(70))},
		}, map[string]string{"d2": costLine(4, 7000, 4)})
		if r["continued_in"] != "d1" || num(r, "cost_usd") != 3 || num(r, "tail_ms") != 70000 {
			t.Errorf("chain = %v", r)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		r := run(t, map[string][]string{"d1": {msg(at(10))}}, nil)
		if r["cost_known"] != false || r["reason"] != "timeout" || num(r, "tail_ms") != 10000 {
			t.Errorf("timeout = %v", r)
		}
		if _, ok := r["cost_usd"]; ok {
			t.Error("cost_usd on unknown cost")
		}
	})
	t.Run("not found", func(t *testing.T) {
		r := run(t, nil, nil)
		if r["cost_known"] != false || r["reason"] != "not_found" || num(r, "tail_ms") != 0 {
			t.Errorf("not_found = %v", r)
		}
	})
}

func ftoa(f float64) string { b, _ := json.Marshal(f); return string(b) }
func itoa(i int) string     { b, _ := json.Marshal(i); return string(b) }
