package bd

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/contract"
)

func TestObsoleteByIsNotASweepReason(t *testing.T) {
	if got := ObsoleteBy("mc-d"); got != "obsolete: mc-d" {
		t.Fatalf("ObsoleteBy = %q", got)
	}
	if _, ok := ParseSweptReason(ObsoleteBy("mc-d")); ok {
		t.Error("a sweep parser matched the obsolete reason")
	}
}

// obsoleteCore stubs the graph for Obsolete: beads, and the beads blocking on each.
type obsoleteCore struct {
	Core
	beads      map[string]Bead
	waiting    map[string][]Bead
	closes     [][2]string
	forced     bool
	supersedes [][2]string
}

func (c *obsoleteCore) Show(id string) (Bead, error) {
	b, ok := c.beads[id]
	if !ok {
		return Bead{}, errors.New("not found: " + id)
	}
	return b, nil
}

func (c *obsoleteCore) Edges(id string, _ Dir, _ string) ([]EdgeRow, error) {
	var rows []EdgeRow
	for _, b := range c.waiting[id] {
		rows = append(rows, EdgeRow{Bead: b, Type: "blocks"})
	}
	return rows, nil
}

func (c *obsoleteCore) Close(ids []string, reason string, force bool) error {
	c.closes, c.forced = append(c.closes, [2]string{ids[0], reason}), force
	return nil
}

func newObsolete(t *testing.T, waiting map[string][]Bead, beads ...Bead) *obsoleteCore {
	t.Helper()
	c := &obsoleteCore{beads: map[string]Bead{}, waiting: waiting}
	for _, b := range beads {
		c.beads[b.ID] = b
	}
	t.Cleanup(Use(c))
	return c
}

func TestObsoleteClosesEachDefogKind(t *testing.T) {
	for _, k := range []contract.Kind{contract.KindGrilling, contract.KindResearch, contract.KindPrototype} {
		c := newObsolete(t, nil, Bead{ID: "t", Kind: k, Status: "open"}, Bead{ID: "d", Kind: contract.KindSurvey, Status: "in_progress"})
		if err := Obsolete("t", "d"); err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		if !reflect.DeepEqual(c.closes, [][2]string{{"t", "obsolete: d"}}) || !c.forced {
			t.Errorf("%s: closes = %v forced=%v", k, c.closes, c.forced)
		}
	}
}

func TestObsoleteRefusesBeforeAnyWrite(t *testing.T) {
	open := func(id string, k contract.Kind) Bead { return Bead{ID: id, Kind: k, Status: "open"} }
	cc := Bead{ID: "cc", Kind: contract.KindChartConflict, Status: "open"}
	for name, tc := range map[string]struct {
		target, decider string
		waiting         map[string][]Bead
		beads           []Bead
		want            string
	}{
		"spine":      {"t", "d", nil, []Bead{open("t", contract.KindSpec), open("d", contract.KindGrilling)}, "not a defog bead"},
		"survey":     {"t", "d", nil, []Bead{open("t", contract.KindSurvey), open("d", contract.KindGrilling)}, "not a defog bead"},
		"latch":      {"t", "d", nil, []Bead{open("t", contract.KindLatch), open("d", contract.KindGrilling)}, "not a defog bead"},
		"closed":     {"t", "d", nil, []Bead{{ID: "t", Kind: contract.KindGrilling, Status: "closed"}, open("d", contract.KindGrilling)}, "already closed"},
		"missing by": {"t", "d", nil, []Bead{open("t", contract.KindGrilling)}, "not found"},
		"self":       {"t", "t", nil, []Bead{open("t", contract.KindGrilling)}, "itself"},
		"wrong by":   {"t", "d", nil, []Bead{open("t", contract.KindGrilling), open("d", contract.KindBuild)}, "not a grilling"},
		"escalation": {"t", "d", map[string][]Bead{"t": {cc}}, []Bead{open("t", contract.KindGrilling), open("d", contract.KindGrilling)}, "answer it through the launch, or withdraw the Chart to give up"},
	} {
		c := newObsolete(t, tc.waiting, tc.beads...)
		err := Obsolete(tc.target, tc.decider)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
		if len(c.closes) != 0 {
			t.Errorf("%s: wrote %v", name, c.closes)
		}
	}
}

func TestObsoleteNamesTheOpenChartConflict(t *testing.T) {
	cc := Bead{ID: "cc-1", Kind: contract.KindChartConflict, Status: "open"}
	newObsolete(t, map[string][]Bead{"t": {cc}}, Bead{ID: "t", Kind: contract.KindGrilling, Status: "open"}, Bead{ID: "d", Kind: contract.KindGrilling, Status: "open"})
	if err := Obsolete("t", "d"); err == nil || !strings.Contains(err.Error(), "cc-1") {
		t.Errorf("err = %v, want it to name cc-1", err)
	}
}

func TestObsoleteAcceptsAnEscalationGrillingWhoseConflictClosed(t *testing.T) {
	cc := Bead{ID: "cc", Kind: contract.KindChartConflict, Status: "closed"}
	c := newObsolete(t, map[string][]Bead{"t": {cc}}, Bead{ID: "t", Kind: contract.KindGrilling, Status: "open"}, Bead{ID: "d", Kind: contract.KindGrilling, Status: "open"})
	if err := Obsolete("t", "d"); err != nil {
		t.Fatal(err)
	}
	if len(c.closes) != 1 {
		t.Errorf("closes = %v", c.closes)
	}
}

func (c *obsoleteCore) Supersede(id, with string) error {
	c.supersedes = append(c.supersedes, [2]string{id, with})
	return nil
}

func TestSupersedeDefogClosesEachDefogKindAndAddsTheEdge(t *testing.T) {
	for _, k := range []contract.Kind{contract.KindGrilling, contract.KindResearch, contract.KindPrototype} {
		c := newObsolete(t, nil, Bead{ID: "t", Kind: k, Status: "open"}, Bead{ID: "n", Kind: contract.KindGrilling, Status: "open"})
		if err := SupersedeDefog("t", "n"); err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		if !reflect.DeepEqual(c.closes, [][2]string{{"t", "superseded: n"}}) || !c.forced {
			t.Errorf("%s: closes = %v forced=%v", k, c.closes, c.forced)
		}
		if !reflect.DeepEqual(c.supersedes, [][2]string{{"t", "n"}}) {
			t.Errorf("%s: supersedes = %v", k, c.supersedes)
		}
	}
}

func TestSupersedeDefogRerunAddsOnlyTheMissingEdge(t *testing.T) {
	c := newObsolete(t, nil,
		Bead{ID: "t", Kind: contract.KindGrilling, Status: "closed", CloseReason: "superseded: n"},
		Bead{ID: "n", Kind: contract.KindGrilling, Status: "open"})
	if err := SupersedeDefog("t", "n"); err != nil {
		t.Fatal(err)
	}
	if len(c.closes) != 0 || len(c.supersedes) != 1 {
		t.Errorf("closes = %v supersedes = %v", c.closes, c.supersedes)
	}
}

func TestSupersedeDefogRefusesBeforeAnyWrite(t *testing.T) {
	open := func(id string, k contract.Kind) Bead { return Bead{ID: id, Kind: k, Status: "open"} }
	cc := Bead{ID: "cc", Kind: contract.KindChartConflict, Status: "open"}
	closed := func(reason string) Bead {
		return Bead{ID: "t", Kind: contract.KindGrilling, Status: "closed", CloseReason: reason}
	}
	for name, tc := range map[string]struct {
		with    string
		waiting map[string][]Bead
		beads   []Bead
		want    string
	}{
		"spine":             {"n", nil, []Bead{open("t", contract.KindSpec), open("n", contract.KindGrilling)}, "not a defog bead"},
		"survey":            {"n", nil, []Bead{open("t", contract.KindSurvey), open("n", contract.KindGrilling)}, "not a defog bead"},
		"latch":             {"n", nil, []Bead{open("t", contract.KindLatch), open("n", contract.KindGrilling)}, "not a defog bead"},
		"closed other":      {"n", nil, []Bead{closed("answered: x"), open("n", contract.KindGrilling)}, "already closed"},
		"closed other with": {"n", nil, []Bead{closed("superseded: z"), open("n", contract.KindGrilling)}, "already closed"},
		"missing with":      {"n", nil, []Bead{open("t", contract.KindGrilling)}, "not found"},
		"self":              {"t", nil, []Bead{open("t", contract.KindGrilling)}, "itself"},
		"escalation":        {"n", map[string][]Bead{"t": {cc}}, []Bead{open("t", contract.KindGrilling), open("n", contract.KindGrilling)}, "answer it through the launch, or withdraw the Chart to give up"},
	} {
		c := newObsolete(t, tc.waiting, tc.beads...)
		err := SupersedeDefog("t", tc.with)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
		if len(c.closes)+len(c.supersedes) != 0 {
			t.Errorf("%s: wrote %v %v", name, c.closes, c.supersedes)
		}
	}
}

func TestSupersedeDefogAcceptsAnEscalationGrillingWhoseConflictClosed(t *testing.T) {
	cc := Bead{ID: "cc", Kind: contract.KindChartConflict, Status: "closed"}
	c := newObsolete(t, map[string][]Bead{"t": {cc}}, Bead{ID: "t", Kind: contract.KindGrilling, Status: "open"}, Bead{ID: "n", Kind: contract.KindGrilling, Status: "open"})
	if err := SupersedeDefog("t", "n"); err != nil {
		t.Fatal(err)
	}
	if len(c.closes) != 1 {
		t.Errorf("closes = %v", c.closes)
	}
}
