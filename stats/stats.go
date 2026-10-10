// Package stats sums Session cost and Agent time from the event log's session
// end events (ADR 0061) under the cost contract (ADR 0029). It is pure and
// observe-only (ADR 0033): callers read the log, stats only adds it up, so the
// mugicrew stats verb and the TUI share one implementation of totals.
package stats

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"jayulabs.org/mugicrew/eventlog"
)

// Filter picks the end events Sum counts and how it groups them.
type Filter struct {
	Bead  string    // the bead and its direct children (the Logs tab's Chart scope); "" = all
	Since time.Time // zero = the whole log
	By    string    // "" (per bead) | "crewmate" | "model" | "day"
}

// Row is one group's (or TOTAL's) sums over its session end events.
type Row struct {
	Key      string `json:"key"`
	Sessions int    `json:"sessions"`
	Failed   int    `json:"failed"`
	WallMs   int64  `json:"wall_ms"` // Agent time
	APIms    int64  `json:"api_ms"`
	Cost
}

// ModelRow is a By "model" row: a session isn't attributed to one model, so only
// cost and tokens split, and the rest is omitted from JSON (never 0).
type ModelRow struct {
	Key string `json:"key"`
	Cost
}

// Cost is the part of a row that splits across a session's models.
type Cost struct {
	CostUSD     float64 `json:"cost_usd"` // known Session cost only
	CostPartial int     `json:"cost_partial_sessions"`
	In          int64   `json:"in"`
	Out         int64   `json:"out"`
	CacheRead   int64   `json:"cache_read"`
	CacheWrite  int64   `json:"cache_write"`
}

// Sum adds up evs' session end events matching f: total over every matched
// session, and rows grouped by f.By, cost descending. TOTAL is independent of
// By, so model rows may sum short of it.
func Sum(evs []eventlog.Event, f Filter) (total Row, rows []Row) {
	total = Row{Key: "TOTAL"}
	groups := map[string]*Row{}
	row := func(key string) *Row {
		if groups[key] == nil {
			groups[key] = &Row{Key: key}
		}
		return groups[key]
	}
	for _, ev := range evs {
		switch ev.Msg {
		case "session.ended", "session.failed", "hitl.exited", "hitl.failed":
		default:
			continue
		}
		// The Logs tab's Chart scope: the bead and its direct children, no bd lookup.
		if (f.Bead != "" && ev.Bead != f.Bead && ev.Parent != f.Bead) || ev.Time.Before(f.Since) {
			continue
		}
		total.add(ev)
		switch f.By {
		case "":
			row(ev.Bead).add(ev)
		case "crewmate":
			cm, _ := ev.Raw["crewmate"].(string)
			row(cm).add(ev)
		case "day":
			row(ev.Time.In(time.Local).Format("2006-01-02")).add(ev)
		case "model":
			// A session without models has nothing to split under: it counts in TOTAL only.
			for k, m := range models(ev) {
				row(k).addModel(m)
			}
		}
	}
	rows = []Row{}
	for _, r := range groups {
		rows = append(rows, *r)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CostUSD != rows[j].CostUSD {
			return rows[i].CostUSD > rows[j].CostUSD
		}
		return rows[i].Key < rows[j].Key
	})
	return total, rows
}

// add sums one end event's attrs into r.
func (r *Row) add(ev eventlog.Event) {
	r.Sessions++
	if ev.Msg == "session.failed" || ev.Msg == "hitl.failed" {
		r.Failed++
	}
	r.WallMs += int64(num(ev.Raw["dur_ms"]))
	r.APIms += int64(num(ev.Raw["api_ms"]))
	if _, ok := ev.Raw["cost_usd"]; ok {
		r.CostUSD += num(ev.Raw["cost_usd"])
	} else {
		r.CostPartial++
	}
	for _, m := range models(ev) {
		r.addTokens(m)
	}
}

// addModel sums one session's models entry (its own cost_usd and tokens) into c.
func (c *Cost) addModel(m map[string]any) {
	if _, ok := m["cost_usd"]; ok {
		c.CostUSD += num(m["cost_usd"])
	} else {
		c.CostPartial++
	}
	c.addTokens(m)
}

func (c *Cost) addTokens(m map[string]any) {
	c.In += int64(num(m["in"]))
	c.Out += int64(num(m["out"]))
	c.CacheRead += int64(num(m["cache_read"]))
	c.CacheWrite += int64(num(m["cache_write"]))
}

// models is an end event's per-model usage, keyed by model.
func models(ev eventlog.Event) map[string]map[string]any {
	ms := map[string]map[string]any{}
	raw, _ := ev.Raw["models"].(map[string]any)
	for k, m := range raw {
		ms[k], _ = m.(map[string]any)
	}
	return ms
}

// num is a decoded JSON number; anything else (absent, mistyped) is 0.
func num(v any) float64 {
	n, _ := v.(json.Number)
	f, _ := n.Float64()
	return f
}

// USD renders the known cost, marked + when it is a Partial cost.
func (c Cost) USD() string {
	s := fmt.Sprintf("$%.2f", c.CostUSD)
	if c.CostPartial > 0 {
		s += "+"
	}
	return s
}

// Cells renders r's SESSIONS, FAILED, AGENT and API cells; a model row's are –,
// as a session isn't attributed to one model.
func (r Row) Cells(model bool) []string {
	if model {
		return []string{"–", "–", "–", "–"}
	}
	return []string{fmt.Sprint(r.Sessions), fmt.Sprint(r.Failed), Dur(r.WallMs), Dur(r.APIms)}
}

// Dur renders milliseconds as a whole-second duration (1h1m0s).
func Dur(ms int64) string {
	return (time.Duration(ms) * time.Millisecond).Round(time.Second).String()
}

// Tokens renders a token count compactly: 950, 1.0k, 2.3M.
func Tokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}
