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

func TestSumChartScopeAndRetries(t *testing.T) {
	now := time.Now()
	evs := []eventlog.Event{
		endEv(now, "session.ended", "ws-ch", "", cost1),
		endEv(now, "session.failed", "ws-ch.1", "ws-ch", cost1),
		endEv(now, "session.ended", "ws-ch.1", "ws-ch", cost1),       // retry
		endEv(now, "hitl.exited", "ws-ch.1.1", "ws-ch.1", cost1),     // grandchild: not counted
		endEv(now, "effort.ended", "ws-ch.1", "ws-ch", `"dur_ms":5`), // not an input
		endEv(now, "session.ended", "ws-other", "", cost1),
	}
	total, rows := Sum(evs, Filter{Bead: "ws-ch"})
	if got := keys(rows); !reflect.DeepEqual(got, []string{"ws-ch.1", "ws-ch"}) {
		t.Fatalf("rows = %v, want ws-ch.1 (costliest) then ws-ch", got)
	}
	want := Row{Key: "ws-ch.1", Sessions: 2, Failed: 1, WallMs: 120000, APIms: 60000,
		Cost: Cost{CostUSD: 3, In: 20, Out: 40, CacheRead: 2000, CacheWrite: 200}}
	if rows[0] != want {
		t.Errorf("ws-ch.1 row = %+v, want %+v", rows[0], want)
	}
	if total.Key != "TOTAL" || total.Sessions != 3 || total.Failed != 1 || total.CostUSD != 4.5 {
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

func TestSumEmpty(t *testing.T) {
	total, rows := Sum(nil, Filter{Bead: "ws-none"})
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
	r := Row{Sessions: 2, Failed: 1, WallMs: 60000, APIms: 1000}
	if got := strings.Join(r.Cells(false), " "); got != "2 1 1m0s 1s" {
		t.Errorf("Cells = %q", got)
	}
	if got := strings.Join(r.Cells(true), " "); got != "– – – –" {
		t.Errorf("model Cells = %q, want – (not attributed to one model)", got)
	}
}
