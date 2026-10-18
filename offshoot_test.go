package main

import (
	"errors"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

type offshootCall struct{ title, body, by, cause, edge string }

// offshootWorld fakes the bd legs of `signal offshoot`: beads by id, and a
// create that refuses an ordered pair with ordered (when set) and records calls.
func offshootWorld(t *testing.T, beads map[string]bd.Bead, ordered error, unordered error) *[]offshootCall {
	t.Helper()
	var calls []offshootCall
	oS, oN := showBead, newOffshoot
	t.Cleanup(func() { showBead, newOffshoot = oS, oN })
	showBead = func(id string) (bd.Bead, error) {
		b, ok := beads[id]
		if !ok {
			return bd.Bead{}, errors.New("not found")
		}
		return b, nil
	}
	newOffshoot = func(title, body, by, cause, edge string) (string, error) {
		calls = append(calls, offshootCall{title, body, by, cause, edge})
		if edge != "" && ordered != nil {
			return "", ordered
		}
		if edge == "" && unordered != nil {
			return "", unordered
		}
		return "mc-new", nil
	}
	return &calls
}

func TestSignalOffshoot(t *testing.T) {
	cp := bd.Bead{ID: "cp", Kind: contract.KindCheckpoint, Status: "open", Title: "The Checkpoint"}
	ch := bd.Bead{ID: "ch", Kind: contract.KindChart, Status: "open", Title: "The Chart"}
	world := map[string]bd.Bead{
		"cp": cp, "ch": ch, "init": {ID: "init", Kind: contract.KindInitiative, Status: "open", Title: "Init"},
		"closed":   {ID: "closed", Kind: contract.KindCheckpoint, Status: "closed", Title: "Done CP"},
		"build":    {ID: "build", Kind: contract.KindBuild, Parent: "cp", Title: "A Build"},
		"defog":    {ID: "defog", Kind: contract.KindPrototype, Parent: "ch", Title: "A defog"},
		"survey":   {ID: "survey", Kind: contract.KindSurvey, Parent: "ch", Title: "A Survey"},
		"task":     {ID: "task", Kind: contract.KindTask, Parent: "init", Title: "A Task"},
		"disc":     {ID: "disc", Kind: contract.KindDiscussion, Parent: "init", Title: "A Discussion"},
		"orphan":   {ID: "orphan", Kind: contract.KindBuild, Title: "Orphan"},
		"inclosed": {ID: "inclosed", Kind: contract.KindBuild, Parent: "closed", Title: "Child of closed"},
	}
	errCycle := errors.New("bd create: would create a cycle")
	errClash := errors.New("bd create: already exists with type related")
	errHier := errors.New("bd create: cannot be blocked by its ancestor")
	cases := []struct {
		name, from, order string
		ordered           error
		wantEdge, wantOrd string
	}{
		{"build follow-up", "build", "follow-up", nil, "blocked-by:cp", "Order: follow-up of The Checkpoint"},
		{"build prerequisite", "build", "prerequisite", nil, "blocks:cp", "Order: prerequisite of The Checkpoint"},
		{"defog under chart", "defog", "follow-up", nil, "blocked-by:ch", "Order: follow-up of The Chart"},
		{"survey under chart", "survey", "follow-up", nil, "blocked-by:ch", "Order: follow-up of The Chart"},
		{"task has no container", "task", "follow-up", nil, "", "Order: unordered (A Task has no container)"},
		{"discussion has no container", "disc", "prerequisite", nil, "", "Order: unordered (A Discussion has no container)"},
		{"unparented", "orphan", "follow-up", nil, "", "Order: unordered (Orphan has no container)"},
		{"closed container", "inclosed", "follow-up", nil, "", "Order: unordered (Done CP is closed)"},
		{"cycle", "build", "prerequisite", errCycle, "", "Order: unordered (cycle)"},
		{"type clash", "build", "follow-up", errClash, "", "Order: unordered (type-clash)"},
		{"hierarchy", "defog", "prerequisite", errHier, "", "Order: unordered (hierarchy)"},
		{"none", "build", "none", nil, "", "Order: none"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calls := offshootWorld(t, world, c.ordered, nil)
			var out strings.Builder
			err := runSignalOffshoot([]string{"--from", c.from, "--drafted-by", "mc-me", "--order", c.order, "--title", "T", "--description", "D"}, &out)
			if err != nil {
				t.Fatal(err)
			}
			last := (*calls)[len(*calls)-1]
			wantBody := "D\n\n" + c.wantOrd
			if last.title != "T" || last.body != wantBody || last.by != "mc-me" || last.cause != c.from || last.edge != c.wantEdge {
				t.Errorf("created %+v, want body %q edge %q", last, wantBody, c.wantEdge)
			}
			if out.String() != "mc-new\n"+c.wantOrd+"\n" {
				t.Errorf("stdout %q", out.String())
			}
			if n := len(*calls); c.ordered != nil && n != 2 || c.ordered == nil && n != 1 {
				t.Errorf("%d creates", n)
			}
		})
	}
}

func TestSignalOffshootFailures(t *testing.T) {
	world := map[string]bd.Bead{
		"cp":    {ID: "cp", Kind: contract.KindCheckpoint, Status: "open", Title: "CP"},
		"build": {ID: "build", Kind: contract.KindBuild, Parent: "cp", Title: "B"},
	}
	good := []string{"--from", "build", "--drafted-by", "mc-me", "--order", "follow-up", "--title", "T", "--description", "D"}
	t.Run("unknown cause", func(t *testing.T) {
		calls := offshootWorld(t, world, nil, nil)
		if err := runSignalOffshoot([]string{"--from", "nope", "--drafted-by", "x", "--order", "none", "--title", "T", "--description", "D"}, &strings.Builder{}); err == nil || len(*calls) != 0 {
			t.Errorf("err %v, %d creates", err, len(*calls))
		}
	})
	t.Run("unclassified ordered error", func(t *testing.T) {
		calls := offshootWorld(t, world, errors.New("dolt exploded"), nil)
		if err := runSignalOffshoot(good, &strings.Builder{}); err == nil || len(*calls) != 1 {
			t.Errorf("err %v, %d creates", err, len(*calls))
		}
	})
	t.Run("unordered re-file fails", func(t *testing.T) {
		offshootWorld(t, world, errors.New("would create a cycle"), errors.New("boom"))
		if err := runSignalOffshoot(good, &strings.Builder{}); err == nil {
			t.Error("want error")
		}
	})
	t.Run("usage", func(t *testing.T) {
		offshootWorld(t, world, nil, nil)
		bad := [][]string{{}, good[2:], append(append([]string{}, good[:5]...), "--order", "sideways", "--title", "T", "--description", "D"), good[:8]}
		bad[2] = []string{"--from", "build", "--drafted-by", "x", "--order", "sideways", "--title", "T", "--description", "D"}
		for _, argv := range bad {
			if err := runSignalOffshoot(argv, &strings.Builder{}); err == nil || !strings.Contains(err.Error(), "usage") {
				t.Errorf("%v: err %v, want usage", argv, err)
			}
		}
	})
}
