// Package stats sums Session cost and Agent time from the event log's session
// end events (ADR 0061) under the cost contract (ADR 0029). It is pure and
// observe-only (ADR 0033): callers read the log and resolve a bead's scope (bd supplies it; stats imports no bd), stats only adds it up, so the
// mugicrew stats verb and the TUI share one implementation of totals.
package stats

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"jayulabs.org/mugicrew/eventlog"
)

// Filter picks the end events Sum counts and how it groups them.
type Filter struct {
	Scope map[string]string // event bead id -> row key; only these beads count (nil = every bead)
	Since time.Time         // zero = the whole log
	Kind  string            // only sessions whose kind attr equals it; "" = every kind
	Mode  string            // "" = both | "afk" | "hitl"; any other value matches nothing
	By    string            // "" (per bead) | "crewmate" | "model" | "profile" | "kind" | "day"
}

// match reports whether ev is an end event (or hitl.continued remainder) f counts.
func (f Filter) match(ev eventlog.Event) bool {
	switch ev.Msg {
	case "session.ended", "session.failed", "hitl.exited", "hitl.failed", "hitl.continued":
	default:
		return false
	}
	if _, in := f.Scope[ev.Bead]; (f.Scope != nil && !in) || ev.Time.Before(f.Since) {
		return false
	}
	hitl := strings.HasPrefix(ev.Msg, "hitl.")
	if (f.Mode == "afk" && hitl) || (f.Mode == "hitl" && !hitl) || (f.Mode != "" && f.Mode != "afk" && f.Mode != "hitl") {
		return false
	}
	kind, _ := ev.Raw["kind"].(string)
	return f.Kind == "" || kind == f.Kind
}

// Session is one counted session's Agent time and Session cost: one per
// non-continued end event Sum counts, its hitl.continued remainders folded in.
type Session struct {
	WallMs  int64   // dur_ms plus every joined tail_ms
	CostUSD float64 // known portion
	Known   bool    // false: Partial cost (no cost on its end, or a costless remainder)
}

// Sessions is evs' sessions matching f (f.By is ignored), in log order; len
// equals Sum's total.Sessions. Orphan remainders are dropped.
func Sessions(evs []eventlog.Event, f Filter) []Session {
	var out []Session
	latest := map[string]int{} // session id -> index of its latest entry
	for _, ev := range evs {
		if !f.match(ev) {
			continue
		}
		id, _ := ev.Raw["session"].(string)
		_, hasCost := ev.Raw["cost_usd"]
		if ev.Msg != "hitl.continued" {
			out = append(out, Session{WallMs: int64(num(ev.Raw["dur_ms"])), CostUSD: num(ev.Raw["cost_usd"]), Known: hasCost})
			if id != "" {
				latest[id] = len(out) - 1
			}
			continue
		}
		i, ok := latest[id]
		if !ok || id == "" {
			continue
		}
		out[i].WallMs += int64(num(ev.Raw["tail_ms"]))
		if hasCost {
			out[i].CostUSD += num(ev.Raw["cost_usd"])
		} else {
			out[i].Known = false
		}
	}
	return out
}

// Row is one group's (or TOTAL's) sums over its session end events.
type Row struct {
	Key      string `json:"key"`
	Sessions int    `json:"sessions"`
	Failed   int    `json:"failed"`
	WallMs   int64  `json:"wall_ms"` // Agent time
	APIms    int64  `json:"api_ms"`
	Cost
	// Known cost and API time of the Partial-cost sessions, left out of the
	// average numerators though in the totals above (FR10).
	partCost float64
	partAPI  int64
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
	// Each session's cost state per group, so a hitl.continued without a cost counts
	// its session's Partial cost once (FR12).
	partial := map[*Row]map[string]*sess{}
	for _, ev := range evs {
		if !f.match(ev) {
			continue
		}
		kind, _ := ev.Raw["kind"].(string)
		total.add(ev, partial)
		switch f.By {
		case "":
			key := ev.Bead
			if f.Scope != nil {
				key = f.Scope[ev.Bead]
			}
			row(key).add(ev, partial)
		case "crewmate":
			cm, _ := ev.Raw["crewmate"].(string)
			row(cm).add(ev, partial)
		case "profile":
			// One profile per session, so a full row; no attr is the unnamed built-in.
			p, _ := ev.Raw["profile"].(string)
			if p == "" {
				p = "(built-in)"
			}
			row(p).add(ev, partial)
		case "kind":
			// One kind per session, so a full row; no attr predates kind being logged.
			if kind == "" {
				kind = "(unknown)"
			}
			row(kind).add(ev, partial)
		case "day":
			row(ev.Time.In(time.Local).Format("2006-01-02")).add(ev, partial)
		case "model":
			// A session without models has nothing to split under: it counts in TOTAL only.
			for k, m := range models(ev) {
				r := row(k)
				r.addModel(m, r.modelFirst(ev, m, partial))
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
// A hitl.continued remainder (ADR 0254) adds its tail_ms and cost but is never a
// session of its own. A session whose remainder lacks a cost counts Partial once,
// and its known portion leaves the average numerators (FR10, FR12).
func (r *Row) add(ev eventlog.Event, seen map[*Row]map[string]*sess) {
	cont := ev.Msg == "hitl.continued"
	if cont {
		r.WallMs += int64(num(ev.Raw["tail_ms"]))
	} else {
		r.Sessions++
		if ev.Msg == "session.failed" || ev.Msg == "hitl.failed" {
			r.Failed++
		}
		r.WallMs += int64(num(ev.Raw["dur_ms"]))
	}
	api := int64(num(ev.Raw["api_ms"]))
	r.APIms += api
	_, hasCost := ev.Raw["cost_usd"]
	cost := num(ev.Raw["cost_usd"])
	s := r.sessOf(ev, seen, cont)
	if hasCost {
		r.CostUSD += cost
		switch {
		case !cont:
			// A run's own end starts its session's tally (when it has an id).
			if s != nil {
				s.cost, s.api = cost, api
			}
		case s == nil || s.partial:
			// A remainder of an unidentified or already-Partial session: not averaged.
			r.partCost, r.partAPI = r.partCost+cost, r.partAPI+api
		default:
			// A remainder of a fully-costed session joins its tally.
			s.cost, s.api = s.cost+cost, s.api+api
		}
	} else if !cont || s == nil {
		r.CostPartial++ // a run's own end, or an unidentified remainder
	} else if !s.partial {
		// The session's first costless remainder: its known portion leaves the averages.
		r.CostPartial++
		r.partCost, r.partAPI = r.partCost+s.cost, r.partAPI+s.api
		s.partial = true
	}
	for _, m := range models(ev) {
		r.addTokens(m)
	}
}

// sess is a session's cost state in one row, to tell a Partial cost already counted.
type sess struct {
	cost    float64
	api     int64
	partial bool
}

// sessOf is ev's session state in r: a fresh one for a run's own end event, the
// recorded one for a remainder (nil when none, or the session has no id).
func (r *Row) sessOf(ev eventlog.Event, seen map[*Row]map[string]*sess, cont bool) *sess {
	id, _ := ev.Raw["session"].(string)
	if id == "" {
		return nil
	}
	if seen[r] == nil {
		seen[r] = map[string]*sess{}
	}
	if !cont {
		_, known := ev.Raw["cost_usd"]
		seen[r][id] = &sess{partial: !known}
	}
	return seen[r][id]
}

// modelFirst reports whether a models entry without a cost is a new Partial cost
// in r; only a remainder can repeat its session's (see add).
func (r *Row) modelFirst(ev eventlog.Event, m map[string]any, seen map[*Row]map[string]*sess) bool {
	_, hasCost := m["cost_usd"]
	cont := ev.Msg == "hitl.continued"
	s := r.sessOf(ev, seen, cont)
	if !cont && s != nil {
		s.partial = !hasCost
	}
	if hasCost || (cont && s != nil && s.partial) {
		return false
	}
	if cont && s != nil {
		s.partial = true
	}
	return true
}

// addModel sums one session's models entry (its own cost_usd and tokens) into c;
// first is false when the session's Partial cost is already counted.
func (c *Cost) addModel(m map[string]any, first bool) {
	if _, ok := m["cost_usd"]; ok {
		c.CostUSD += num(m["cost_usd"])
	} else if first {
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

// Cells renders r's SESSIONS, FAILED, AGENT, API, AVG_COST, AVG_API and FAIL%
// cells; a model row's are –, as a session isn't attributed to one model. The
// averages are over the sessions with a known cost (+ when some lack one); a
// zero denominator is –.
func (r Row) Cells(model bool) []string {
	if model {
		return []string{"–", "–", "–", "–", "–", "–", "–"}
	}
	avgCost, avgAPI, failPct := "–", "–", "–"
	if n := r.Sessions - r.CostPartial; n > 0 {
		avgCost = Cost{CostUSD: (r.CostUSD - r.partCost) / float64(n), CostPartial: r.CostPartial}.USD()
		avgAPI = Dur((r.APIms - r.partAPI) / int64(n))
	}
	if r.Sessions > 0 {
		failPct = fmt.Sprintf("%.0f%%", 100*float64(r.Failed)/float64(r.Sessions))
	}
	return []string{fmt.Sprint(r.Sessions), fmt.Sprint(r.Failed), Dur(r.WallMs), Dur(r.APIms), avgCost, avgAPI, failPct}
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
