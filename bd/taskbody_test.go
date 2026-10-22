package bd

import "testing"

func TestTaskBodyLayout(t *testing.T) {
	got := TaskBody("Lead.\n", []string{"Key: 1\n", "Link: u"}, []TaskStep{
		{Title: "Do it.", Markers: []string{"irreversible"}, Do: "act", Check: "`x` prints `y`."},
		{Title: "Confirm.", Do: "think", Ask: "is it?"},
	}, "tail\n")
	want := "Lead.\nKey: 1\nLink: u\n\n## Steps\n\n" +
		"1. Do it.   [irreversible]\n   Do: act\n   Check: `x` prints `y`.\n\n" +
		"2. Confirm.\n   Do: think\n   Ask: is it?\n\n" +
		"## Diagnostics\n\ntail\n"
	if got != want {
		t.Errorf("TaskBody =\n%q\nwant\n%q", got, want)
	}
	if got := TaskBody("L", nil, []TaskStep{{Title: "t", Do: "d", Ask: "a"}}, ""); got != "L\n\n## Steps\n\n1. t\n   Do: d\n   Ask: a\n" {
		t.Errorf("no diagnostics: %q", got)
	}
}
