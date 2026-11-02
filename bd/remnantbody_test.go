package bd

import (
	"reflect"
	"testing"
)

func TestParseRemnantBody(t *testing.T) {
	const hdr = "Remnant of cp, which a Forge merge landed at sq1.\n"
	tests := []struct {
		name                          string
		body                          string
		id, sha, tip, line, rng, head string
		kept                          []KeptEntry
		branches, errands, errandIDs  []string
		children                      []ChildEntry
	}{
		{
			name: "full body",
			body: hdr + "Landed-tip: t1\nStranded: t1..h1\nKept: effort/k1 c1..h1\nChild: k1 build Do — obsolete: x\nErrand: e1 Hold (a before-land hold)",
			id:   "cp", sha: "sq1", tip: "t1", line: "Stranded: t1..h1", rng: "t1..h1", head: "h1",
			kept:     []KeptEntry{{"effort/k1", "c1..h1", "effort/k1 c1..h1"}},
			branches: []string{"effort/k1"}, children: []ChildEntry{{"k1", "k1 build Do — obsolete: x"}}, errands: []string{"e1 Hold (a before-land hold)"}, errandIDs: []string{"e1"},
		},
		{
			name: "unknown tip",
			body: hdr + "Landed-tip: unknown\nStranded: unknown (the landed squash carries no Tip:)",
			id:   "cp", sha: "sq1", tip: "unknown", line: "Stranded: unknown (the landed squash carries no Tip:)",
		},
		{
			name: "no Stranded line",
			body: hdr + "Landed-tip: t1",
			id:   "cp", sha: "sq1", tip: "t1",
		},
		{
			name: "rewritten branch",
			body: hdr + "Landed-tip: t1\nStranded: branch rewritten after the landed pin; see git range-diff t1...h9",
			id:   "cp", sha: "sq1", tip: "t1", head: "h9",
			line: "Stranded: branch rewritten after the landed pin; see git range-diff t1...h9",
		},
		{
			name: "no cut ref and duplicate branches",
			body: hdr + "Kept: effort/k1 h1 (no cut ref)\nKept: effort/k2 c..h\nKept: effort/k1 c..h",
			id:   "cp", sha: "sq1",
			kept: []KeptEntry{
				{"effort/k1", "h1 (no cut ref)", "effort/k1 h1 (no cut ref)"},
				{"effort/k2", "c..h", "effort/k2 c..h"},
				{"effort/k1", "c..h", "effort/k1 c..h"},
			},
			branches: []string{"effort/k1", "effort/k2"},
		},
		{
			name: "errands",
			body: hdr + "Errand: e1 Hold (a)\nErrand: e2 Other\nErrand: \nErrand:e3",
			id:   "cp", sha: "sq1", errands: []string{"e1 Hold (a)", "e2 Other"}, errandIDs: []string{"e1", "e2"},
		},
		{name: "empty"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := ParseRemnantBody(tc.body)
			if r.LandedID() != tc.id || r.LandedSHA() != tc.sha || r.LandedTip() != tc.tip ||
				r.StrandedLine() != tc.line || r.StrandedRange() != tc.rng || r.StrandedHead() != tc.head {
				t.Fatalf("got id=%q sha=%q tip=%q line=%q rng=%q head=%q", r.LandedID(), r.LandedSHA(), r.LandedTip(), r.StrandedLine(), r.StrandedRange(), r.StrandedHead())
			}
			if !reflect.DeepEqual(r.Kept(), tc.kept) || !reflect.DeepEqual(r.KeptBranches(), tc.branches) ||
				!reflect.DeepEqual(r.Children(), tc.children) || !reflect.DeepEqual(r.Errands(), tc.errands) || !reflect.DeepEqual(r.ErrandIDs(), tc.errandIDs) {
				t.Fatalf("got kept=%v branches=%v children=%v errands=%v", r.Kept(), r.KeptBranches(), r.Children(), r.Errands())
			}
		})
	}
}

func TestRemnantBodyKeptFor(t *testing.T) {
	r := ParseRemnantBody("Kept: effort/a c..h\nKept: effort/a x..y")
	if e, ok := r.KeptFor("effort/a"); !ok || e.Rest != "x..y" {
		t.Fatalf("last entry wins: %+v %v", e, ok)
	}
	if _, ok := r.KeptFor("effort/b"); ok {
		t.Fatal("unknown branch found")
	}
}

// The formatters write the bytes Jinbe, KeptLine and the sweep write today, and the
// parse reads them back.
func TestFormatRemnantRoundTrip(t *testing.T) {
	got := FormatRemnantHeader("cp", "sq1", "t1", "Stranded: t1..h1")
	if want := "Remnant of cp, which a Forge merge landed at sq1.\nLanded-tip: t1\nStranded: t1..h1"; got != want {
		t.Fatalf("header:\n%s", got)
	}
	got = FormatRemnantHeader("cp", "sq1", "unknown", "Stranded: unknown (the landed squash carries no Tip:)")
	if want := "Remnant of cp, which a Forge merge landed at sq1.\nLanded-tip: unknown\nStranded: unknown (the landed squash carries no Tip:)"; got != want {
		t.Fatalf("unknown header:\n%s", got)
	}
	if got := FormatRemnantHeader("cp", "sq1", "t1", ""); got != "Remnant of cp, which a Forge merge landed at sq1.\nLanded-tip: t1" {
		t.Fatalf("no stranded:\n%s", got)
	}
	if got := FormatKeptLine("effort/k", "c1", "h1"); got != "Kept: effort/k c1..h1" {
		t.Fatal(got)
	}
	if got := FormatKeptNoCutLine("effort/k", "h1"); got != "Kept: effort/k h1 (no cut ref)" {
		t.Fatal(got)
	}
	if got := FormatChildLine("k build T — x"); got != "Child: k build T — x" {
		t.Fatal(got)
	}
	r := ParseRemnantBody(FormatRemnantHeader("cp", "sq1", "t1", "Stranded: t1..h1") + "\n" + FormatKeptLine("effort/k", "c1", "h1") + "\n" + FormatChildLine("k build T — x"))
	if r.LandedID() != "cp" || r.LandedSHA() != "sq1" || r.LandedTip() != "t1" || r.StrandedRange() != "t1..h1" ||
		len(r.Kept()) != 1 || len(r.Children()) != 1 {
		t.Fatalf("round trip: %+v", r)
	}
}
