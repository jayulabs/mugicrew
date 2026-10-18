package eventlog

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// scanFixture writes .1 and the live file, more than a chunk each so lines
// straddle chunk boundaries, with a partly written last live line.
func scanFixture(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "events.jsonl")
	line := func(i int) string {
		lvl, bead := "INFO", "mc-a"
		if i%7 == 0 {
			lvl = "ERROR"
		}
		if i%3 == 0 {
			bead = "mc-b"
		}
		return fmt.Sprintf(`{"time":"2026-01-01T00:00:%02d.%09dZ","level":"%s","msg":"Evt.%d","bead":"%s","pid":"1","note":"quote \" <x> %s"}`+"\n",
			i%60, i, lvl, i, bead, strings.Repeat("pad", i%50))
	}
	var old, live strings.Builder
	for i := 0; i < 4000; i++ {
		old.WriteString(line(i))
	}
	for i := 4000; i < 8000; i++ {
		live.WriteString(line(i))
	}
	live.WriteString("not json\n" + `{"msg":"part`)
	put(t, p+".1", old.String(), false)
	put(t, p, live.String(), false)
	return p
}

func TestScanPrefilterEqualsParseEveryLine(t *testing.T) {
	p := scanFixture(t)
	all, _, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		pre  []string
		ok   func(Event) bool
	}{
		{"words", []string{"evt.12", "pad"}, func(e Event) bool {
			return strings.Contains(strings.ToLower(e.Msg), "evt.12") && strings.Contains(e.Raw["note"].(string), "pad")
		}},
		{"L", []string{"mc-b"}, func(e Event) bool { return e.Bead == "mc-b" }},
		{"e", []string{`"level":"error"`}, func(e Event) bool { return e.Level == "ERROR" }},
		{"quote word", nil, func(e Event) bool {
			return strings.Contains(e.Raw["note"].(string), `"`) && strings.HasSuffix(e.Msg, "5")
		}},
		{"kind only", nil, func(e Event) bool { return e.Bead == "mc-a" && e.Msg[len(e.Msg)-1] == '1' }},
	}
	for _, c := range cases {
		var want []string
		for i := len(all) - 1; i >= 0; i-- {
			if c.ok(all[i]) {
				want = append(want, all[i].Msg)
			}
		}
		var got []string
		batches := 0
		err := Scan(p, c.pre, c.ok, func(evs []Event) bool {
			batches++
			got = append(got, msgs(evs)...)
			return true
		})
		if err != nil || !eq(got, want) || len(want) == 0 {
			t.Errorf("%s: got %d want %d (err %v)", c.name, len(got), len(want), err)
		}
		if c.name == "e" && batches < 2 {
			t.Errorf("matches should stream per chunk, got %d batches", batches)
		}
	}
}

func TestScanStopsWhenBatchSaysSo(t *testing.T) {
	p := scanFixture(t)
	n := 0
	_ = Scan(p, nil, func(Event) bool { return true }, func([]Event) bool { n++; return false })
	if n != 1 {
		t.Errorf("batch called %d times after returning false", n)
	}
}

func TestScanTallyRawParsesNothingPrefilteredOut(t *testing.T) {
	p := scanFixture(t)
	all, _, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	base := Base{Raw: true, Match: func(Event) bool { return true }}
	var want Tally
	for _, e := range all {
		want.Lines++
		if e.Level == "DEBUG" {
			want.Debug++
		}
	}
	scanParsed.Store(0)
	var got Tally
	err = ScanTally(p, []string{"no-such-word"}, func(Event) bool { return true }, func([]Event) bool { return true }, base, &got)
	if err != nil || scanParsed.Load() != 0 {
		t.Errorf("parsed %d lines (err %v), want 0", scanParsed.Load(), err)
	}
	if got.Lines != want.Lines+1 { // the fixture's "not json" line is raw-counted, not parsed
		t.Errorf("tally %+v want %+v (+1 for the junk line)", got, want)
	}
}
