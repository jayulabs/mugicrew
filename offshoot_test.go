package main

import (
	"errors"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

type offshootCall struct{ title, body, by, cause, edge, parent string }

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
	newOffshoot = func(title, body, by, cause, edge, parent string) (string, error) {
		calls = append(calls, offshootCall{title, body, by, cause, edge, parent})
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
	cp := bd.Bead{ID: "cp", Kind: contract.KindCheckpoint, Status: "open", Title: "The Checkpoint", Parent: "init"}
	ch := bd.Bead{ID: "ch", Kind: contract.KindChart, Status: "open", Title: "The Chart"}
	world := map[string]bd.Bead{
		"cp": cp, "ch": ch, "init": {ID: "init", Kind: contract.KindInitiative, Status: "open", Title: "Init"},
		"closed":       {ID: "closed", Kind: contract.KindCheckpoint, Status: "closed", Title: "Done CP", Parent: "init"},
		"build":        {ID: "build", Kind: contract.KindBuild, Parent: "cp", Title: "A Build"},
		"defog":        {ID: "defog", Kind: contract.KindPrototype, Parent: "ch", Title: "A defog"},
		"survey":       {ID: "survey", Kind: contract.KindSurvey, Parent: "ch", Title: "A Survey"},
		"errand":       {ID: "errand", Kind: contract.KindErrand, Parent: "init", Title: "An Errand"},
		"disc":         {ID: "disc", Kind: contract.KindDiscussion, Parent: "init", Title: "A Discussion"},
		"orphan":       {ID: "orphan", Kind: contract.KindBuild, Title: "Orphan"},
		"inclosed":     {ID: "inclosed", Kind: contract.KindBuild, Parent: "closed", Title: "Child of closed"},
		"cinit":        {ID: "cinit", Kind: contract.KindInitiative, Status: "closed", Title: "Old Init"},
		"cpc":          {ID: "cpc", Kind: contract.KindCheckpoint, Status: "open", Title: "CP in closed Init", Parent: "cinit"},
		"buildc":       {ID: "buildc", Kind: contract.KindBuild, Parent: "cpc", Title: "Build in closed Init"},
		"cpo":          {ID: "cpo", Kind: contract.KindCheckpoint, Status: "open", Title: "CP outside"},
		"buildo":       {ID: "buildo", Kind: contract.KindBuild, Parent: "cpo", Title: "Build outside"},
		"closederrand": {ID: "closederrand", Kind: contract.KindErrand, Status: "closed", Parent: "init", Title: "Done Errand"},
		"outerrand":    {ID: "outerrand", Kind: contract.KindErrand, Title: "Outside Errand"},
		"memsig":       {ID: "memsig", Kind: contract.KindSignal, Status: "open", Parent: "init", Title: "A member Signal"},
		"member":       {ID: "member", Kind: contract.KindBuild, Parent: "cp", Title: "Member"},
	}
	errCycle := errors.New("bd create: would create a cycle")
	errClash := errors.New("bd create: already exists with type related")
	errHier := errors.New("bd create: cannot be blocked by its ancestor")
	const in = "Initiative: Init"
	none := func(t string) string { return "Initiative: outside (" + t + " has no Initiative)" }
	cases := []struct {
		name, from, order string
		outside           string
		ordered           error
		wantEdge, wantOrd string
		wantParent        string
		wantPlace         string
	}{
		{"build follow-up", "build", "follow-up", "", nil, "blocked-by:cp", "Order: follow-up of The Checkpoint", "init", in},
		{"build prerequisite", "build", "prerequisite", "", nil, "blocks:cp", "Order: prerequisite of The Checkpoint", "init", in},
		{"defog under chart", "defog", "follow-up", "", nil, "blocked-by:ch", "Order: follow-up of The Chart", "", none("The Chart")},
		{"survey under chart", "survey", "follow-up", "", nil, "blocked-by:ch", "Order: follow-up of The Chart", "", none("The Chart")},
		{"member errand follow-up", "errand", "follow-up", "", nil, "blocked-by:errand", "Order: follow-up of An Errand", "init", in},
		{"member errand prerequisite", "errand", "prerequisite", "", nil, "blocks:errand", "Order: prerequisite of An Errand", "init", in},
		{"closed errand joins", "closederrand", "follow-up", "", nil, "", "Order: unordered (Done Errand has no container)", "init", in},
		{"member signal joins", "memsig", "follow-up", "", nil, "", "Order: unordered (A member Signal has no container)", "init", in},
		{"outside errand", "outerrand", "follow-up", "", nil, "", "Order: unordered (Outside Errand has no container)", "", none("Outside Errand")},
		{"discussion stays outside", "disc", "prerequisite", "", nil, "", "Order: unordered (A Discussion has no container)", "", none("A Discussion")},
		{"unparented", "orphan", "follow-up", "", nil, "", "Order: unordered (Orphan has no container)", "", none("Orphan")},
		{"closed container joins", "inclosed", "follow-up", "", nil, "", "Order: unordered (Done CP is closed)", "init", in},
		{"closed initiative", "buildc", "follow-up", "", nil, "blocked-by:cpc", "Order: follow-up of CP in closed Init", "", "Initiative: outside (Old Init is closed)"},
		{"checkpoint outside initiative", "buildo", "prerequisite", "", nil, "blocks:cpo", "Order: prerequisite of CP outside", "", none("CP outside")},
		{"cycle keeps parent", "build", "prerequisite", "", errCycle, "", "Order: unordered (cycle)", "init", in},
		{"type clash keeps parent", "build", "follow-up", "", errClash, "", "Order: unordered (type-clash)", "init", in},
		{"hierarchy keeps parent", "member", "prerequisite", "", errHier, "", "Order: unordered (hierarchy)", "init", in},
		{"none joins", "build", "none", "", nil, "", "Order: none", "init", in},
		{"none of errand", "errand", "none", "", nil, "", "Order: none", "init", in},
		{"none under chart", "defog", "none", "", nil, "", "Order: none", "", none("The Chart")},
		{"none of discussion", "disc", "none", "", nil, "", "Order: none", "", none("A Discussion")},
		{"outside open initiative", "build", "none", "unrelated", nil, "", "Order: none", "", "Initiative: outside (filer: unrelated)"},
		{"outside keeps edge", "build", "follow-up", "unrelated", nil, "blocked-by:cp", "Order: follow-up of The Checkpoint", "", "Initiative: outside (filer: unrelated)"},
		{"outside closed initiative", "buildc", "none", "unrelated", nil, "", "Order: none", "", "Initiative: outside (Old Init is closed)"},
		{"outside no initiative", "orphan", "none", "unrelated", nil, "", "Order: none", "", none("Orphan")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calls := offshootWorld(t, world, c.ordered, nil)
			var out strings.Builder
			argv := []string{"--from", c.from, "--drafted-by", "mc-me", "--order", c.order, "--title", "T", "--description", "D"}
			if c.outside != "" {
				argv = append(argv, "--outside", c.outside)
			}
			err := runSignalOffshoot(argv, &out)
			if err != nil {
				t.Fatal(err)
			}
			last := (*calls)[len(*calls)-1]
			wantBody := "D\n\n" + c.wantOrd + "\n" + c.wantPlace
			if last.title != "T" || last.body != wantBody || last.by != "mc-me" || last.cause != c.from || last.edge != c.wantEdge || last.parent != c.wantParent {
				t.Errorf("created %+v, want body %q edge %q parent %q", last, wantBody, c.wantEdge, c.wantParent)
			}
			if out.String() != "mc-new\n"+c.wantOrd+"\n"+c.wantPlace+"\n" {
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
	t.Run("empty outside", func(t *testing.T) {
		calls := offshootWorld(t, world, nil, nil)
		err := runSignalOffshoot(append(append([]string{}, good...), "--outside", ""), &strings.Builder{})
		if err == nil || !strings.Contains(err.Error(), "usage") || !strings.Contains(err.Error(), "--outside") || len(*calls) != 0 {
			t.Errorf("err %v, %d creates", err, len(*calls))
		}
	})
	t.Run("unreadable parent", func(t *testing.T) {
		offshootWorld(t, map[string]bd.Bead{"x": {ID: "x", Kind: contract.KindBuild, Parent: "gone", Title: "X"}}, nil, nil)
		if err := runSignalOffshoot([]string{"--from", "x", "--drafted-by", "m", "--order", "none", "--title", "T", "--description", "D"}, &strings.Builder{}); err == nil {
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

func TestSignalOffshootDryRun(t *testing.T) {
	world := map[string]bd.Bead{
		"cp":     {ID: "cp", Kind: contract.KindCheckpoint, Status: "open", Title: "The Checkpoint", Parent: "init"},
		"init":   {ID: "init", Kind: contract.KindInitiative, Status: "open", Title: "Init", Description: "## Done when\n\nUsers can ship.\n\n## Phases\n- one"},
		"build":  {ID: "build", Kind: contract.KindBuild, Parent: "cp", Title: "A Build"},
		"orphan": {ID: "orphan", Kind: contract.KindBuild, Title: "Orphan"},
	}
	run := func(from string) (string, *[]offshootCall) {
		calls := offshootWorld(t, world, nil, nil)
		var out strings.Builder
		argv := []string{"--from", from, "--drafted-by", "mc-me", "--order", "follow-up", "--title", "T", "--description", "D", "--dry-run"}
		if err := runSignalOffshoot(argv, &out); err != nil {
			t.Fatal(err)
		}
		return out.String(), calls
	}
	out, calls := run("build")
	if len(*calls) != 0 {
		t.Errorf("dry run created %v", *calls)
	}
	for _, want := range []string{"Order: follow-up of The Checkpoint\n", "Initiative: Init\n", "Users can ship."} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "- one") {
		t.Errorf("Done when must stop at the next section:\n%s", out)
	}
	out, calls = run("orphan")
	if len(*calls) != 0 || strings.Contains(out, "Done when") || !strings.Contains(out, "Initiative: outside (Orphan has no Initiative)") {
		t.Errorf("no-Initiative dry run: %q, %v", out, *calls)
	}
}
