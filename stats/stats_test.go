package stats

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/eventlog"
)

// endEv is one session end event; extra is raw JSON attrs, decoded as the event
// log decodes them (numbers as json.Number).
func endEv(at time.Time, msg, bead, parent, extra string) eventlog.Event {
	raw := map[string]any{}
	dec := json.NewDecoder(strings.NewReader("{" + extra + "}"))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		panic(err)
	}
	return eventlog.Event{Time: at, Msg: msg, Bead: bead, Parent: parent, Raw: raw}
}

const cost1 = `"dur_ms":60000,"cost_usd":1.5,"api_ms":30000,"models":{"claude-opus":{"cost_usd":1.5,"in":10,"out":20,"cache_read":1000,"cache_write":100}}`

func keys(rows []Row) []string {
	ks := []string{}
	for _, r := range rows {
		ks = append(ks, r.Key)
	}
	return ks
}

func TestSumScopeAndRetries(t *testing.T) {
	now := time.Now()
	evs := []eventlog.Event{
		endEv(now, "session.ended", "ws-ch", "", cost1),
		endEv(now, "session.failed", "ws-ch.1", "ws-ch", cost1),
		endEv(now, "session.ended", "ws-ch.1", "ws-ch", cost1),       // retry
		endEv(now, "hitl.exited", "ws-ch.1.1", "ws-ch.1", cost1),     // grandchild: in Scope, counted
		endEv(now, "effort.ended", "ws-ch.1", "ws-ch", `"dur_ms":5`), // not an input
		endEv(now, "session.ended", "ws-other", "", cost1),
	}
	total, rows := Sum(evs, Filter{Scope: map[string]string{"ws-ch": "ws-ch", "ws-ch.1": "ws-ch.1", "ws-ch.1.1": "ws-ch.1.1"}})
	if got := keys(rows); !reflect.DeepEqual(got, []string{"ws-ch.1", "ws-ch", "ws-ch.1.1"}) {
		t.Fatalf("rows = %v, want ws-ch.1 (costliest), then ws-ch, ws-ch.1.1 by key", got)
	}
	want := Row{Key: "ws-ch.1", Sessions: 2, Failed: 1, WallMs: 120000, APIms: 60000,
		Cost: Cost{CostUSD: 3, In: 20, Out: 40, CacheRead: 2000, CacheWrite: 200}}
	if rows[0] != want {
		t.Errorf("ws-ch.1 row = %+v, want %+v", rows[0], want)
	}
	if total.Key != "TOTAL" || total.Sessions != 4 || total.Failed != 1 || total.CostUSD != 6 {
		t.Errorf("total = %+v", total)
	}
}

func TestSumSince(t *testing.T) {
	now := time.Now()
	evs := []eventlog.Event{
		endEv(now.Add(-100*time.Hour), "session.ended", "mc-b", "", cost1),
		endEv(now.Add(-time.Hour), "session.ended", "mc-b", "", cost1),
	}
	for _, tc := range []struct {
		since time.Time
		want  int
	}{{now.Add(-72 * time.Hour), 1}, {time.Time{}, 2}, {now, 0}} {
		if total, _ := Sum(evs, Filter{Since: tc.since}); total.Sessions != tc.want {
			t.Errorf("since %v: total sessions = %d, want %d", tc.since, total.Sessions, tc.want)
		}
	}
}

func TestSumBy(t *testing.T) {
	d1 := time.Date(2026, 10, 1, 23, 30, 0, 0, time.Local)
	d2 := d1.Add(time.Hour) // the next local day
	two := `"dur_ms":1000,"cost_usd":3,"api_ms":500,"models":{"opus":{"cost_usd":2,"in":10,"out":20},"haiku":{"cost_usd":1,"in":1,"cache_read":5}}`
	evs := []eventlog.Event{
		endEv(d1, "session.ended", "mc-a", "", `"crewmate":"franky",`+cost1),
		endEv(d2, "session.ended", "mc-b", "", `"crewmate":"franky",`+two),
		endEv(d2, "session.failed", "mc-b", "", `"crewmate":"chopper","dur_ms":5,"models":{"opus":{"in":4}}`), // model entry without a cost
		endEv(d2, "hitl.exited", "mc-a", "", `"dur_ms":1000,"cost_usd":0.5`),                                  // no crewmate, no models
	}
	wantTotal := Row{Key: "TOTAL", Sessions: 4, Failed: 1, WallMs: 62005, APIms: 30500,
		Cost: Cost{CostUSD: 5, CostPartial: 1, In: 25, Out: 40, CacheRead: 1005, CacheWrite: 100}}
	for _, tc := range []struct {
		by   string
		keys []string
	}{
		{"", []string{"mc-b", "mc-a"}},
		{"crewmate", []string{"franky", "", "chopper"}},
		{"model", []string{"opus", "claude-opus", "haiku"}},
		{"day", []string{"2026-10-02", "2026-10-01"}},
	} {
		total, rows := Sum(evs, Filter{By: tc.by})
		if total != wantTotal {
			t.Errorf("by %q total = %+v, want %+v (TOTAL is the same under every By)", tc.by, total, wantTotal)
		}
		if got := keys(rows); !reflect.DeepEqual(got, tc.keys) {
			t.Errorf("by %q rows = %v, want %v (cost descending)", tc.by, got, tc.keys)
		}
	}

	_, rows := Sum(evs, Filter{By: "model"})
	if opus := rows[0]; opus.Cost != (Cost{CostUSD: 2, CostPartial: 1, In: 14, Out: 20}) || opus.Sessions != 0 {
		t.Errorf("opus row = %+v: only cost and tokens split by model", opus)
	}
	_, rows = Sum(evs, Filter{By: "day"})
	if rows[0].Sessions != 3 || rows[1].Sessions != 1 {
		t.Errorf("day rows = %+v", rows)
	}
}

// A session runs on one profile, so profile rows are full rows; no attr is the
// unnamed built-in.
func TestSumByProfile(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.Local)
	evs := []eventlog.Event{
		endEv(at, "session.ended", "mc-a", "", `"profile":"local-qwen",`+cost1),
		endEv(at, "session.failed", "mc-b", "", `"profile":"local-qwen","dur_ms":5,"api_ms":2`),
		endEv(at, "hitl.exited", "mc-c", "", `"dur_ms":1000,"cost_usd":0.5`),
	}
	_, rows := Sum(evs, Filter{By: "profile"})
	want := []Row{
		{Key: "local-qwen", Sessions: 2, Failed: 1, WallMs: 60005, APIms: 30002,
			Cost: Cost{CostUSD: 1.5, CostPartial: 1, In: 10, Out: 20, CacheRead: 1000, CacheWrite: 100}},
		{Key: "(built-in)", Sessions: 1, WallMs: 1000, Cost: Cost{CostUSD: 0.5}},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("by profile rows = %+v, want %+v", rows, want)
	}
}

func TestSumEmpty(t *testing.T) {
	total, rows := Sum(nil, Filter{Scope: map[string]string{"ws-none": "ws-none"}})
	if total != (Row{Key: "TOTAL"}) || rows == nil || len(rows) != 0 {
		t.Errorf("Sum(nil) = %+v, %#v, want an empty TOTAL and no rows", total, rows)
	}
}

func TestFormatters(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{Cost{CostUSD: 1.5}.USD(), "$1.50"},
		{Cost{CostUSD: 1.75, CostPartial: 1}.USD(), "$1.75+"}, // Partial cost
		{Dur(3_660_400), "1h1m0s"},
		{Dur(0), "0s"},
		{Tokens(950), "950"},
		{Tokens(1000), "1.0k"},
		{Tokens(2_345_678), "2.3M"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
	r := Row{Sessions: 2, Failed: 1, WallMs: 60000, APIms: 1000, Cost: Cost{CostUSD: 3}}
	if got := strings.Join(r.Cells(false), " "); got != "2 1 1m0s 1s $1.50 1s 50%" {
		t.Errorf("Cells = %q", got)
	}
	if got := strings.Join(r.Cells(true), " "); got != "– – – – – – –" {
		t.Errorf("model Cells = %q, want – (not attributed to one model)", got)
	}
}

// Kind is one per session, so kind rows are full rows; a session logged before
// kind was recorded is (unknown), and any Kind filter leaves it out.
func TestSumKind(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.Local)
	evs := []eventlog.Event{
		endEv(at, "session.ended", "mc-a", "", `"kind":"build","profile":"opus",`+cost1),
		endEv(at, "session.failed", "mc-b", "", `"kind":"build","profile":"sonnet","dur_ms":5,"cost_usd":0.5`),
		endEv(at, "session.ended", "mc-c", "", `"kind":"review","profile":"opus","dur_ms":7,"cost_usd":1`),
		endEv(at, "hitl.exited", "mc-d", "", `"profile":"opus","dur_ms":1000,"cost_usd":0.25`), // pre-kind
	}
	_, rows := Sum(evs, Filter{By: "kind"})
	want := []Row{
		{Key: "build", Sessions: 2, Failed: 1, WallMs: 60005, APIms: 30000,
			Cost: Cost{CostUSD: 2, In: 10, Out: 20, CacheRead: 1000, CacheWrite: 100}},
		{Key: "review", Sessions: 1, WallMs: 7, Cost: Cost{CostUSD: 1}},
		{Key: "(unknown)", Sessions: 1, WallMs: 1000, Cost: Cost{CostUSD: 0.25}},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("by kind rows = %+v, want %+v", rows, want)
	}

	total, rows := Sum(evs, Filter{Kind: "build", By: "profile"})
	if got := keys(rows); !reflect.DeepEqual(got, []string{"opus", "sonnet"}) || rows[0].Sessions != 1 || rows[1].Failed != 1 {
		t.Errorf("kind build by profile rows = %+v", rows)
	}
	if total.Sessions != 2 || total.CostUSD != 2 {
		t.Errorf("kind build total = %+v, want only the 2 build sessions", total)
	}
	for _, k := range []string{"(unknown)", "nosuchkind"} {
		if total, rows := Sum(evs, Filter{Kind: k}); total.Sessions != 0 || len(rows) != 0 {
			t.Errorf("kind %q = %+v, %v, want nothing", k, total, rows)
		}
	}
}

// FR6–7: averages are over the costed sessions only, marked + when some lack a
// cost; a zero denominator is –.
func TestCellsPerSession(t *testing.T) {
	for _, tc := range []struct {
		r    Row
		want string
	}{
		{Row{Sessions: 3, Failed: 1, APIms: 60000, Cost: Cost{CostUSD: 3, CostPartial: 1}}, "$1.50+ 30s 33%"}, // partial cost
		{Row{Sessions: 2, Failed: 2, Cost: Cost{CostPartial: 2}}, "– – 100%"},                                 // no costed session
		{Row{}, "– – –"},
	} {
		if got := strings.Join(tc.r.Cells(false)[4:], " "); got != tc.want {
			t.Errorf("%+v per-session cells = %q, want %q", tc.r, got, tc.want)
		}
	}
}

// ADR 0254 §3: a hitl.continued remainder joins its hitl.exited's row, adds
// tail_ms and cost, and is never a session; Partial cost counts once per session.
func TestSumHitlContinued(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.Local)
	evs := []eventlog.Event{
		endEv(at, "hitl.exited", "mc-a", "", `"session":"s1","kind":"grill","crewmate":"zoro","dur_ms":1000,"cost_usd":1,"api_ms":10,"models":{"m1":{"cost_usd":1,"in":1}}`),
		endEv(at.Add(time.Hour), "hitl.continued", "mc-a", "", `"session":"s1","kind":"grill","crewmate":"zoro","tail_ms":500,"cost_usd":0.5,"api_ms":5,"models":{"m1":{"cost_usd":0.5,"in":2}}`),
		endEv(at, "hitl.exited", "mc-b", "mc-a", `"session":"s2","kind":"grill","crewmate":"zoro","dur_ms":100`),
		endEv(at, "hitl.continued", "mc-b", "mc-a", `"session":"s2","kind":"grill","crewmate":"zoro","tail_ms":50,"cost_known":false`),
	}
	for _, by := range []string{"", "crewmate", "kind", "day"} {
		total, rows := Sum(evs, Filter{By: by})
		if total.Sessions != 2 || total.Failed != 0 || total.WallMs != 1650 || total.CostUSD != 1.5 || total.CostPartial != 1 || total.APIms != 15 {
			t.Errorf("by %q total = %+v", by, total)
		}
		var sum Row
		for _, r := range rows {
			sum.Sessions += r.Sessions
			sum.WallMs += r.WallMs
			sum.CostUSD += r.CostUSD
			sum.CostPartial += r.CostPartial
		}
		if sum.Sessions != 2 || sum.WallMs != 1650 || sum.CostUSD != 1.5 || sum.CostPartial != 1 {
			t.Errorf("by %q rows sum to %+v, want TOTAL", by, sum)
		}
	}
	if _, rows := Sum(evs, Filter{By: "model"}); len(rows) != 1 || rows[0].CostUSD != 1.5 || rows[0].In != 3 {
		t.Errorf("by model rows = %+v", rows)
	}
	if total, _ := Sum(evs, Filter{Scope: map[string]string{"mc-a": "mc-a", "mc-b": "mc-b"}}); total.Sessions != 2 || total.WallMs != 1650 {
		t.Errorf("chart scope total = %+v", total)
	}
	// --since straddle: the remainder counts by its own time.
	if total, _ := Sum(evs, Filter{Since: at.Add(time.Minute)}); total.Sessions != 0 || total.WallMs != 500 || total.CostUSD != 0.5 {
		t.Errorf("since total = %+v, want only the remainder", total)
	}
}

// FR10, FR12: a partial session's known portion stays out of the averages but in
// the totals; only a remainder repeats its session's Partial cost.
func TestSumPartialAverages(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.Local)
	evs := []eventlog.Event{
		endEv(at, "hitl.exited", "mc-a", "", `"session":"s1","dur_ms":1,"cost_usd":1,"api_ms":10`),
		endEv(at, "hitl.exited", "mc-a", "", `"session":"s2","dur_ms":1,"cost_usd":1,"api_ms":10`),
		endEv(at, "hitl.continued", "mc-a", "", `"session":"s2","tail_ms":1,"cost_known":false`),
	}
	total, _ := Sum(evs, Filter{})
	if total.CostUSD != 2 || total.CostPartial != 1 || total.Cells(false)[4] != "$1.00+" || total.Cells(false)[5] != "0s" {
		t.Errorf("total = %+v cells %v", total, total.Cells(false))
	}
	evs = []eventlog.Event{
		endEv(at, "hitl.failed", "mc-a", "", `"session":"s1","dur_ms":1`),
		endEv(at, "hitl.failed", "mc-a", "", `"session":"s1","dur_ms":1`),
	}
	if total, _ := Sum(evs, Filter{}); total.CostPartial != 2 {
		t.Errorf("two failed runs CostPartial = %d, want 2", total.CostPartial)
	}
}

// Sessions folds remainders into the latest end with their id, drops orphans, and
// counts exactly what Sum counts.
func TestSessions(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.Local)
	evs := []eventlog.Event{
		endEv(at, "hitl.exited", "mc-a", "", `"session":"s1","kind":"grill","dur_ms":1000,"cost_usd":1`),
		endEv(at.Add(time.Hour), "hitl.continued", "mc-a", "", `"session":"s1","kind":"grill","tail_ms":500,"cost_usd":0.5`),
		endEv(at.Add(2*time.Hour), "hitl.exited", "mc-a", "", `"session":"s1","kind":"grill","dur_ms":200,"cost_usd":2`), // resumed: a second entry
		endEv(at.Add(3*time.Hour), "hitl.continued", "mc-a", "", `"session":"s1","kind":"grill","tail_ms":50,"cost_known":false`),
		endEv(at, "session.ended", "mc-b", "", `"kind":"build","dur_ms":300,"cost_usd":0`),   // no id, known $0
		endEv(at, "session.failed", "mc-c", "", `"session":"s3","kind":"build"`),             // no dur_ms, no cost
		endEv(at, "hitl.continued", "mc-d", "", `"session":"gone","tail_ms":9,"cost_usd":9`), // orphan
	}
	for _, tc := range []struct {
		name string
		f    Filter
		want []Session
	}{
		{"all", Filter{}, []Session{{1500, 1.5, true}, {250, 2, false}, {300, 0, true}, {0, 0, false}}},
		{"kind", Filter{Kind: "build"}, []Session{{300, 0, true}, {0, 0, false}}},
		{"since straddles s1's end", Filter{Since: at.Add(30 * time.Minute)}, []Session{{250, 2, false}}},
		{"by ignored", Filter{By: "day"}, []Session{{1500, 1.5, true}, {250, 2, false}, {300, 0, true}, {0, 0, false}}},
	} {
		got := Sessions(evs, tc.f)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: Sessions = %+v, want %+v", tc.name, got, tc.want)
		}
		if total, _ := Sum(evs, tc.f); total.Sessions != len(got) {
			t.Errorf("%s: len %d != Sum sessions %d", tc.name, len(got), total.Sessions)
		}
	}
}

func TestSumScopeGroupsByKeyAndMembership(t *testing.T) {
	now := time.Now()
	evs := []eventlog.Event{
		endEv(now, "session.ended", "a", "", cost1),
		endEv(now, "session.ended", "a.1", "a", cost1),
		endEv(now, "session.ended", "b", "a", cost1), // Parent no longer matters
	}
	scope := map[string]string{"a": "stage-a", "a.1": "stage-a"}
	total, rows := Sum(evs, Filter{Scope: scope})
	if total.Sessions != 2 || len(rows) != 1 || rows[0].Key != "stage-a" || rows[0].Sessions != 2 {
		t.Errorf("scope rows = %+v total = %+v, want one stage-a row of 2", rows, total)
	}
	_, rows = Sum(evs, Filter{Scope: scope, By: "day"})
	if len(rows) != 1 || rows[0].Sessions != 2 {
		t.Errorf("By + Scope rows = %+v, want membership only", rows)
	}
	if total, _ := Sum(evs, Filter{}); total.Sessions != 3 {
		t.Errorf("nil Scope total = %+v, want everything", total)
	}
	if got := Sessions(evs, Filter{Scope: scope}); len(got) != 2 {
		t.Errorf("Sessions under Scope = %d, want 2", len(got))
	}
}

func TestSumMode(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.Local)
	evs := []eventlog.Event{
		endEv(at, "session.ended", "mc-a", "", `"session":"s1","dur_ms":1000,"cost_usd":1`),
		endEv(at, "session.failed", "mc-b", "", `"session":"s2","dur_ms":200,"cost_usd":2`),
		endEv(at, "hitl.exited", "mc-c", "", `"session":"s3","dur_ms":4000,"cost_usd":4`),
		endEv(at, "hitl.continued", "mc-c", "", `"session":"s3","tail_ms":500,"cost_usd":0.5`),
		endEv(at, "hitl.failed", "mc-d", "", `"session":"s4","dur_ms":80,"cost_usd":8`),
	}
	for _, c := range []struct {
		mode     string
		sessions int
		failed   int
		wall     int64
		cost     float64
	}{
		{"", 4, 2, 5780, 15.5},
		{"afk", 2, 1, 1200, 3},
		{"hitl", 2, 1, 4580, 12.5},
		{"bogus", 0, 0, 0, 0},
	} {
		total, _ := Sum(evs, Filter{Mode: c.mode})
		if total.Sessions != c.sessions || total.Failed != c.failed || total.WallMs != c.wall || total.CostUSD != c.cost {
			t.Errorf("mode %q total = %+v", c.mode, total)
		}
		ss := Sessions(evs, Filter{Mode: c.mode})
		var wall int64
		for _, s := range ss {
			wall += s.WallMs
		}
		if len(ss) != c.sessions || wall != c.wall {
			t.Errorf("mode %q sessions = %+v", c.mode, ss)
		}
	}
}
