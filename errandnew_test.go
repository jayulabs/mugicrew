package main

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
)

func fakeNewErrand(t *testing.T, id, line string, err error) *bd.ErrandFiling {
	t.Helper()
	var got bd.ErrandFiling
	o := newErrand
	t.Cleanup(func() { newErrand = o })
	newErrand = func(f bd.ErrandFiling) (string, string, error) { got = f; return id, line, err }
	return &got
}

// errand new prints the id then the placement line, for a joined Errand and a born-outside one alike.
func TestErrandNewPrintsIDThenPlacement(t *testing.T) {
	for _, line := range []string{"Initiative: Init", "Initiative: outside (unordered)"} {
		got := fakeNewErrand(t, "mc-9", line, nil)
		var out bytes.Buffer
		err := runErrandNew([]string{"--drafted-by", "me", "--after", "a", "--after", "b", "--before", "c", "--outside", "--title", "T", "--description", "D"}, &out)
		want := bd.ErrandFiling{Title: "T", Body: "D", DraftedBy: "me", After: []string{"a", "b"}, Before: []string{"c"}, Outside: true}
		if err != nil || out.String() != "mc-9\n"+line+"\n" || !reflect.DeepEqual(*got, want) {
			t.Errorf("runErrandNew = %v, stdout %q, filing %+v; want id, %q, %+v", err, out.String(), *got, line, want)
		}
	}
}

// --drafted-by, --title and --description are required; stray arguments are refused.
func TestErrandNewRequiresFlags(t *testing.T) {
	for _, argv := range [][]string{
		{"--title", "T", "--description", "D"},
		{"--drafted-by", "me", "--description", "D"},
		{"--drafted-by", "me", "--title", "T"},
		{"--drafted-by", "me", "--title", "T", "--description", "D", "extra"},
	} {
		fakeNewErrand(t, "mc-9", "x", nil)
		var out bytes.Buffer
		if err := runErrandNew(argv, &out); err == nil || out.Len() != 0 {
			t.Errorf("runErrandNew(%v) = %v, stdout %q; want a usage refusal", argv, err, out.String())
		}
	}
}

// A refused edge surfaces bd's reason as the error and prints nothing.
func TestErrandNewRefusedEdge(t *testing.T) {
	fakeNewErrand(t, "", "", errors.New("would create a cycle"))
	var out bytes.Buffer
	err := runErrandNew([]string{"--drafted-by", "me", "--after", "a", "--title", "T", "--description", "D"}, &out)
	if err == nil || out.Len() != 0 || !bytes.Contains([]byte(err.Error()), []byte("would create a cycle")) {
		t.Errorf("runErrandNew = %v, stdout %q; want bd's reason and no output", err, out.String())
	}
}

func TestTaskVerbRefusesAndNamesErrandNew(t *testing.T) {
	filed := false
	defer func(f func(bd.ErrandFiling) (string, string, error)) { newErrand = f }(newErrand)
	newErrand = func(bd.ErrandFiling) (string, string, error) { filed = true; return "", "", nil }
	for _, argv := range [][]string{{}, {"new", "--drafted-by", "b", "--title", "t", "--description", "d"}} {
		err := runRetiredTask(argv)
		if err == nil || !strings.Contains(err.Error(), "mugicrew errand new") {
			t.Errorf("task %v: err = %v, want a refusal naming mugicrew errand new", argv, err)
		}
	}
	if filed {
		t.Error("the retired task verb filed an Errand")
	}
}
