package main

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"jayulabs.org/mugicrew/bd"
)

func fakeNewTask(t *testing.T, id, line string, err error) *bd.TaskFiling {
	t.Helper()
	var got bd.TaskFiling
	o := newTask
	t.Cleanup(func() { newTask = o })
	newTask = func(f bd.TaskFiling) (string, string, error) { got = f; return id, line, err }
	return &got
}

// task new prints the id then the placement line, for a joined Task and a born-outside one alike.
func TestTaskNewPrintsIDThenPlacement(t *testing.T) {
	for _, line := range []string{"Initiative: Init", "Initiative: outside (unordered)"} {
		got := fakeNewTask(t, "mc-9", line, nil)
		var out bytes.Buffer
		err := runTaskNew([]string{"--drafted-by", "me", "--after", "a", "--after", "b", "--before", "c", "--outside", "--title", "T", "--description", "D"}, &out)
		want := bd.TaskFiling{Title: "T", Body: "D", DraftedBy: "me", After: []string{"a", "b"}, Before: []string{"c"}, Outside: true}
		if err != nil || out.String() != "mc-9\n"+line+"\n" || !reflect.DeepEqual(*got, want) {
			t.Errorf("runTaskNew = %v, stdout %q, filing %+v; want id, %q, %+v", err, out.String(), *got, line, want)
		}
	}
}

// --drafted-by, --title and --description are required; stray arguments are refused.
func TestTaskNewRequiresFlags(t *testing.T) {
	for _, argv := range [][]string{
		{"--title", "T", "--description", "D"},
		{"--drafted-by", "me", "--description", "D"},
		{"--drafted-by", "me", "--title", "T"},
		{"--drafted-by", "me", "--title", "T", "--description", "D", "extra"},
	} {
		fakeNewTask(t, "mc-9", "x", nil)
		var out bytes.Buffer
		if err := runTaskNew(argv, &out); err == nil || out.Len() != 0 {
			t.Errorf("runTaskNew(%v) = %v, stdout %q; want a usage refusal", argv, err, out.String())
		}
	}
}

// A refused edge surfaces bd's reason as the error and prints nothing.
func TestTaskNewRefusedEdge(t *testing.T) {
	fakeNewTask(t, "", "", errors.New("would create a cycle"))
	var out bytes.Buffer
	err := runTaskNew([]string{"--drafted-by", "me", "--after", "a", "--title", "T", "--description", "D"}, &out)
	if err == nil || out.Len() != 0 || !bytes.Contains([]byte(err.Error()), []byte("would create a cycle")) {
		t.Errorf("runTaskNew = %v, stdout %q; want bd's reason and no output", err, out.String())
	}
}
