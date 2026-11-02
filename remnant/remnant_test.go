package remnant

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

type fake struct {
	beads      map[string]bd.Bead
	blockers   []bd.Bead
	children   []bd.Bead
	renum      []bd.Bead
	moves      map[string][]string
	branches   map[string]bool
	appended   []string
	closed     map[string]string
	forced     []string
	deleted    []string
	filed      []string // title\nbody
	filedTitle string
	blocked    [][2]string
	pivots     []bd.Bead
	kept       map[string]string
	hasRespin  bool
}

func newFake(body string) *fake {
	f := &fake{beads: map[string]bd.Bead{}, branches: map[string]bool{}, closed: map[string]string{}}
	f.beads["r"] = bd.Bead{ID: "r", Title: "Remnant of Cp", Kind: contract.KindRemnant, Status: "open", Description: body}
	f.beads["e1"] = bd.Bead{ID: "e1", Kind: contract.KindErrand, Status: "open"}
	f.beads["cp"] = bd.Bead{ID: "cp", Title: "Cp", Kind: contract.KindCheckpoint, Status: "closed"}
	return f
}

func (f *fake) seams() Seams {
	return Seams{
		Show: func(id string) (bd.Bead, error) {
			b, ok := f.beads[id]
			if !ok {
				return b, errors.New("no " + id)
			}
			if r, ok := f.closed[id]; ok {
				b.Status, b.CloseReason = "closed", r
			}
			return b, nil
		},
		Landed:       func(string) (string, bool, error) { return "cp", true, nil },
		OpenBlockers: func(string) ([]bd.Bead, error) { return f.blockers, nil },
		OpenChildren: func(string) ([]bd.Bead, error) { return f.children, nil },
		OpenPivots:   func(string) ([]bd.Bead, error) { return f.pivots, nil },
		AppendLines: func(id string, ls ...string) error {
			f.appended = append(f.appended, ls...)
			b := f.beads[id]
			b.Description += "\n" + strings.Join(ls, "\n")
			f.beads[id] = b
			return nil
		},
		ForceCloseAll: func(ids []string, reason string) error {
			f.forced = append(f.forced, ids...)
			for _, id := range ids {
				f.closed[id] = reason
			}
			return nil
		},
		Close:     func(id, reason string) error { f.closed[id] = reason; return nil },
		Renumbers: func(string) ([]bd.Bead, error) { return f.renum, nil },
		Moves:     func(sha string) ([]string, error) { return f.moves[sha], nil },
		NewOffshoot: func(title, body, rem string) (string, error) {
			f.filed = append(f.filed, title+"\n"+body)
			f.filedTitle = title
			return "sig1", nil
		},
		FindOffshoot: func(rem, title string) (string, bool, error) {
			return "sig1", f.filedTitle == title, nil
		},
		Kept:         func(b bd.Bead) (string, error) { return f.kept[b.ID], nil },
		Block:        func(rem, kid string) error { f.blocked = append(f.blocked, [2]string{rem, kid}); return nil },
		HasBranch:    func(n string) (bool, error) { return f.branches[n], nil },
		HasRespin:    func(string) (bool, error) { return f.hasRespin, nil },
		DeleteBranch: func(n string) error { f.deleted = append(f.deleted, n); return nil },
	}
}

const body = "Remnant of cp\nLanded-tip: abc\nStranded: abc..def\nKept: effort/k1 c1..h1\nChild: k1 build Do — obsolete: container landed; carried by r\nErrand: e1 Hold (a before-land hold)"

func TestDropDeletesExactlyTheListedRefs(t *testing.T) {
	f := newFake(body)
	f.branches = map[string]bool{"checkpoint/cp": true, "effort/k1": true, "effort/other": true}
	f.beads["e1"] = bd.Bead{ID: "e1", Kind: contract.KindErrand, Status: "open"}
	res, err := Drop(f.seams(), "r")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"checkpoint/cp", "effort/k1"}; !slices.Equal(f.deleted, want) {
		t.Fatalf("deleted %v want %v", f.deleted, want)
	}
	if f.closed["r"] != "dropped" {
		t.Fatalf("remnant close %q", f.closed["r"])
	}
	if f.closed["e1"] != "obsolete: dropped by r" || len(f.filed) != 0 {
		t.Fatalf("errand close %q filed %v", f.closed["e1"], f.filed)
	}
	if len(res.Deleted) != 2 {
		t.Fatalf("res %+v", res)
	}
}

func TestDropSkipsAMissingBranch(t *testing.T) {
	f := newFake(body)
	f.branches = map[string]bool{"effort/k1": true}
	if _, err := Drop(f.seams(), "r"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.deleted, []string{"effort/k1"}) {
		t.Fatalf("deleted %v", f.deleted)
	}
}

func TestOffshootFilesOneSignalNamingEverything(t *testing.T) {
	f := newFake(body)
	f.beads["e1"] = bd.Bead{ID: "e1", Kind: contract.KindErrand, Status: "open"}
	f.moves = map[string][]string{"s1": {"docs/adr/0405-a.md → docs/adr/0410-a.md"}}
	f.renum = []bd.Bead{
		{Title: "Renumber 0405 from cp", CloseReason: "landed: s1", Description: "Renumber Checkpoint\n- docs/adr/0405-a.md clashes with docs/adr/0405-b.md\n"},
		{Title: "Renumber 0500 from cp", Description: "Renumber Checkpoint\n- docs/adr/0500-a.md clashes with docs/adr/0500-b.md\n"},
	}
	res, err := Offshoot(f.seams(), "r")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.filed) != 1 || res.Signal != "sig1" {
		t.Fatalf("filed %v", f.filed)
	}
	for _, want := range []string{"Child: k1 build Do", "Kept: effort/k1 c1..h1", "Stranded: abc..def", "Renumber Checkpoint: Renumber 0405 from cp", "Move: docs/adr/0405-a.md → docs/adr/0410-a.md", "Planned: docs/adr/0500-a.md clashes with docs/adr/0500-b.md"} {
		if !strings.Contains(f.filed[0], want) {
			t.Errorf("signal lacks %q:\n%s", want, f.filed[0])
		}
	}
	if strings.Contains(f.filed[0], "Move: docs/adr/0405-a.md clashes") || strings.Contains(f.filed[0], "Move: docs/adr/0500") {
		t.Errorf("a clash is mislabelled a move:\n%s", f.filed[0])
	}
	if f.closed["r"] != "offshoot: sig1" || f.closed["e1"] != "obsolete: offshoot by r" || len(f.deleted) != 0 {
		t.Fatalf("closed %v deleted %v", f.closed, f.deleted)
	}
}

// A Remnant written before appendMissing skipped repeats can hold two Kept: lines for a
// branch; the newer one is copied (last-wins keeps those parsing, FR16).
func TestOffshootCopiesTheLastKeptLineOfADuplicatedBranch(t *testing.T) {
	f := newFake(body + "\nKept: effort/k1 c2..h2")
	if _, err := Offshoot(f.seams(), "r"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.filed[0], "Kept: effort/k1 c2..h2") || strings.Contains(f.filed[0], "c1..h1") {
		t.Fatalf("not last-wins:\n%s", f.filed[0])
	}
}

func TestRefusals(t *testing.T) {
	for name, tc := range map[string]func(*fake){
		"blocked":     func(f *fake) { f.blockers = []bd.Bead{{Title: "Running"}} },
		"not remnant": func(f *fake) { b := f.beads["r"]; b.Kind = contract.KindBuild; f.beads["r"] = b },
		"closed":      func(f *fake) { f.closed["r"] = "dropped" },
	} {
		for exit, run := range map[string]func(Seams, string) (Result, error){"drop": Drop, "offshoot": Offshoot} {
			f := newFake(body)
			tc(f)
			if _, err := run(f.seams(), "r"); err == nil {
				t.Errorf("%s/%s: not refused", name, exit)
			}
			if len(f.deleted)+len(f.filed)+len(f.forced) != 0 || (f.closed["r"] != "" && name != "closed") {
				t.Errorf("%s/%s: acted", name, exit)
			}
		}
	}
}

func TestLateUnstartedChildIsSweptAndListedFirst(t *testing.T) {
	f := newFake(body)
	f.children = []bd.Bead{
		{ID: "late", Kind: contract.KindCritique, Title: "Defect", Status: "open"},
		{ID: "lnd", Kind: contract.KindLanding, Title: "Landing", Status: "open"},
	}
	if _, err := Offshoot(f.seams(), "r"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.forced, []string{"late"}) {
		t.Fatalf("forced %v", f.forced)
	}
	if f.closed["late"] != "obsolete: container landed; carried by r" {
		t.Fatalf("reason %q", f.closed["late"])
	}
	if !strings.Contains(f.filed[0], "Child: late critique Defect") {
		t.Fatalf("not listed:\n%s", f.filed[0])
	}
}

func TestPausedUnassignedChildHoldsTheRemnantUnswept(t *testing.T) {
	f := newFake(body)
	f.children = []bd.Bead{
		{ID: "paused", Kind: contract.KindBuild, Title: "Paused", Status: "open", Labels: []string{"paused:integrate-conflict"}},
	}
	if _, err := Offshoot(f.seams(), "r"); err == nil {
		t.Fatal("not refused")
	}
	if len(f.forced) != 0 {
		t.Fatalf("swept %v", f.forced)
	}
}

func TestRunningLateChildRefusesTheExitAndGainsTheHold(t *testing.T) {
	f := newFake(body)
	f.children = []bd.Bead{{ID: "run", Kind: contract.KindBuild, Title: "Running", Status: "in_progress"}}
	for exit, run := range map[string]func(Seams, string) (Result, error){"drop": Drop, "offshoot": Offshoot} {
		f.blocked = nil
		_, err := run(f.seams(), "r")
		if err == nil || !strings.Contains(err.Error(), "blocked by Running") {
			t.Fatalf("%s: err %v", exit, err)
		}
		if len(f.blocked) != 1 || f.blocked[0] != [2]string{"r", "run"} {
			t.Fatalf("%s: blocked %v", exit, f.blocked)
		}
		if len(f.deleted)+len(f.filed) != 0 || f.closed["r"] != "" {
			t.Fatalf("%s: acted", exit)
		}
	}
}

// A pivot raised on the landed Checkpoint after the Remnant was filed gains the hold
// fileRemnant gives its early pivots, and refuses every exit until it is resolved.
func TestOpenPivotRefusesEveryExitAndGainsTheHold(t *testing.T) {
	f := newFake(body)
	f.pivots = []bd.Bead{{ID: "pv", Kind: contract.KindPivot, Title: "FR 3 is wrong", Status: "open"}}
	for exit, run := range map[string]func(Seams, string) (Result, error){"drop": Drop, "offshoot": Offshoot} {
		f.blocked = nil
		_, err := run(f.seams(), "r")
		if err == nil || !strings.Contains(err.Error(), "blocked by FR 3 is wrong") {
			t.Fatalf("%s: err %v", exit, err)
		}
		if len(f.blocked) != 1 || f.blocked[0] != [2]string{"r", "pv"} {
			t.Fatalf("%s: blocked %v", exit, f.blocked)
		}
		if len(f.deleted)+len(f.filed) != 0 || f.closed["r"] != "" {
			t.Fatalf("%s: acted", exit)
		}
	}
}

func TestSweptChildWithAKeptBranchListsItsKeptLineFirstAndDropDeletesIt(t *testing.T) {
	f := newFake(body)
	f.kept = map[string]string{"late": "Kept: effort/late c9..h9"}
	f.branches["effort/late"], f.branches["refs/mugicrew/cut/late"] = true, true
	f.children = []bd.Bead{{ID: "late", Kind: contract.KindBuild, Title: "Do", Status: "open"}}
	if _, err := Drop(f.seams(), "r"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.beads["r"].Description, "Kept: effort/late c9..h9\nChild: late build Do") {
		t.Fatalf("Kept: not listed before Child:\n%s", f.beads["r"].Description)
	}
	if !slices.Contains(f.deleted, "effort/late") {
		t.Fatalf("deleted %v lacks effort/late", f.deleted)
	}
}

func TestOffshootRerunAfterAFailedCloseReusesTheSignal(t *testing.T) {
	f := newFake(body)
	s := f.seams()
	fail := true
	closeFn := s.Close
	s.Close = func(id, reason string) error {
		if id == "e1" && fail {
			fail = false
			return errors.New("bd down")
		}
		return closeFn(id, reason)
	}
	if _, err := Offshoot(s, "r"); err == nil {
		t.Fatal("first run should fail")
	}
	res, err := Offshoot(s, "r")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.filed) != 1 || res.Signal != "sig1" || f.closed["r"] != "offshoot: sig1" {
		t.Fatalf("filed %v res %+v closed %v", f.filed, res, f.closed)
	}
}

// A respin Draft a crashed respin left is stranded once its Remnant closes by another
// exit, so drop and offshoot refuse while one exists and name respin as the way out.
func TestDropAndOffshootRefuseWhileARespinDraftExists(t *testing.T) {
	for name, exit := range map[string]func(Seams, string) (Result, error){"drop": Drop, "offshoot": Offshoot} {
		f := newFake(body)
		f.hasRespin = true
		f.branches = map[string]bool{"checkpoint/cp": true}
		_, err := exit(f.seams(), "r")
		if err == nil || !strings.Contains(err.Error(), "respin") {
			t.Fatalf("%s: err = %v, want a refusal naming respin", name, err)
		}
		if len(f.closed) != 0 || len(f.deleted) != 0 || len(f.filed) != 0 || len(f.appended) != 0 {
			t.Errorf("%s: refused exit had effects: closed %v deleted %v filed %v appended %v", name, f.closed, f.deleted, f.filed, f.appended)
		}
	}
}

// ChildLines puts the Kept: line before the Child: line, and leaves it out when empty.
func TestChildLinesPutsKeptBeforeChild(t *testing.T) {
	k := bd.Bead{ID: "mc-a", Kind: "build", Title: "T"}
	kept := func(bd.Bead) (string, error) { return "Kept: effort/mc-a c1..h1", nil }
	got, err := ChildLines(k, "r", kept)
	if err != nil || len(got) != 2 || got[0] != "Kept: effort/mc-a c1..h1" || got[1] != ChildLine(k, "r") {
		t.Fatalf("ChildLines = %q, %v", got, err)
	}
	got, err = ChildLines(k, "r", func(bd.Bead) (string, error) { return "", nil })
	if err != nil || len(got) != 1 || got[0] != ChildLine(k, "r") {
		t.Fatalf("empty kept: ChildLines = %q, %v", got, err)
	}
}
