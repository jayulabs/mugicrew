package bd

import "testing"

func TestAppendMissingKeysKeptAndChildLinesOnChildID(t *testing.T) {
	const base = "Landed-tip: abc\nKept: effort/k1 c1..h1\nChild: k1 build Do — why"
	for _, tc := range []struct {
		name string
		line string
		want string // appended line, "" when skipped
	}{
		{"kept with changed head", "Kept: effort/k1 c1..h9", ""},
		{"child with changed tail", "Child: k1 build Do — other", ""},
		{"kept for a new child", "Kept: effort/k2 c2..h2", "Kept: effort/k2 c2..h2"},
		{"child for a new child", "Child: k2 build Two — x", "Child: k2 build Two — x"},
		{"non-keyed duplicate", "Landed-tip: abc", ""},
		{"non-keyed changed", "Landed-tip: def", "Landed-tip: def"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := appendMissing(base, []string{tc.line})
			want := base
			if tc.want != "" {
				want += "\n" + tc.want
			}
			if got != want || changed != (tc.want != "") {
				t.Fatalf("got %q changed=%v, want %q", got, changed, want)
			}
		})
	}
}

func TestAppendMissingKeysPerPrefix(t *testing.T) {
	got, _ := appendMissing("Kept: effort/k1 c..h", []string{"Child: k1 build T — r"})
	if want := "Kept: effort/k1 c..h\nChild: k1 build T — r"; got != want {
		t.Fatalf("Kept suppressed Child: %q", got)
	}
	got, _ = appendMissing("Child: k1 build T — r", []string{"Kept: effort/k1 c..h"})
	if want := "Child: k1 build T — r\nKept: effort/k1 c..h"; got != want {
		t.Fatalf("Child suppressed Kept: %q", got)
	}
}
