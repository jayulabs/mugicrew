package session

import (
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/bd/bdtest"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
)

// The Survey, grilling and prototype tails give a defog Draft filed without
// --deps the discovered-from edge before it publishes; a Draft born with it is not
// written twice, another bead's Draft is untouched, and a grilling discovered-from
// a Build-like raiser is no pivot and no carrier.
func TestDefogTailsTieDraftsToTheirSession(t *testing.T) {
	for _, kind := range []contract.Kind{contract.KindSurvey, contract.KindGrilling, contract.KindPrototype} {
		t.Run(string(kind), func(t *testing.T) {
			eventlogtest.Capture(t)
			f := bdtest.New(t,
				bdtest.Claimed(bd.Bead{ID: "t-chart", Kind: contract.KindChart}),
				bdtest.Claimed(bd.Bead{ID: "t-s", Kind: kind, Parent: "t-chart"}),
				bdtest.Draft("t-s", bd.Bead{ID: "t-bare", Kind: contract.KindGrilling, Parent: "t-chart"}),
				bdtest.Draft("t-s", bd.Bead{ID: "t-born", Kind: contract.KindResearch, Parent: "t-chart"}),
				bdtest.Draft("t-s", bd.Bead{ID: "t-proto", Kind: contract.KindPrototype, Parent: "t-chart"}),
				bdtest.Draft("t-other", bd.Bead{ID: "t-theirs", Kind: contract.KindGrilling, Parent: "t-chart"}),
			)
			if err := bd.DiscoveredFrom("t-born", "t-s"); err != nil {
				t.Fatal(err)
			}
			before := len(f.Calls())
			if err := PublishDrafts("t-s", nil); err != nil {
				t.Fatal(err)
			}
			up, err := f.Edges("t-s", bd.Up, "discovered-from")
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, r := range up {
				got = append(got, r.Bead.ID)
			}
			slices.Sort(got)
			if want := []string{"t-bare", "t-born", "t-proto"}; !slices.Equal(got, want) {
				t.Errorf("discovered-from t-s = %v, want %v", got, want)
			}
			adds := 0
			for _, c := range f.Calls()[before:] {
				if strings.HasPrefix(c, "AddEdge ") {
					adds++
				}
			}
			if adds != 2 {
				t.Errorf("edge writes = %d, want 2 (calls %v)", adds, f.Calls()[before:])
			}
		})
	}
}

// A grilling discovered-from a Build-like raiser is no pivot, and CarriersFrom
// excludes defog beads.
func TestDefogDiscoveredFromIsNoPivotNorCarrier(t *testing.T) {
	bdtest.New(t,
		bd.Bead{ID: "t-build", Kind: contract.KindBuild, Status: "in_progress"},
		bd.Bead{ID: "t-g", Kind: contract.KindGrilling, Status: "open"},
	)
	if err := bd.DiscoveredFrom("t-g", "t-build"); err != nil {
		t.Fatal(err)
	}
	if got, err := bd.PivotsDiscoveredFrom("t-build"); err != nil || len(got) != 0 {
		t.Errorf("PivotsDiscoveredFrom = %v, %v; want none", got, err)
	}
	if got, err := bd.CarriersFrom("t-build"); err != nil || len(got) != 0 {
		t.Errorf("CarriersFrom = %v, %v; want none", got, err)
	}
}
