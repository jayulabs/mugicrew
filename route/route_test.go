package route

import (
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// Route is the pure kind+parent → destination decision (Spec table), plus
// the container each committing kind integrates onto and its integrate mode (Spec
// D1: squash, replace for chart-conflict and conflict). These cases are the ticket's acceptance criteria: every kind in the table
// resolves to the right placement and branch/ref, asserted with fake beads and
// no git subprocess.
func TestRouteCoversEveryKindInTable(t *testing.T) {
	orig := ParentKind
	t.Cleanup(func() { ParentKind = orig })
	ParentKind = func(id string) (contract.Kind, error) {
		if id == "ws-chart" {
			return contract.KindChart, nil
		}
		return contract.KindCheckpoint, nil
	}
	cases := []struct {
		name string
		bead bd.Bead
		want Destination
	}{
		{
			name: "the AFK judge's tree → worktree of the Chart's own effort branch, integrating onto its OWN chart",
			bead: bd.Bead{ID: "ws-chart", Kind: contract.KindChart},
			want: Destination{Placement: Worktree, Branch: "effort/ws-chart", Container: "chart/ws-chart"},
		},
		{
			name: "survey → worktree of its own effort branch, integrating onto its PARENT chart",
			bead: bd.Bead{ID: "ws-s", Kind: contract.KindSurvey, Parent: "ws-chart"},
			want: Destination{Placement: Worktree, Branch: "effort/ws-s", Container: "chart/ws-chart"},
		},
		{
			name: "grilling → worktree of its own effort branch, integrating onto its PARENT chart",
			bead: bd.Bead{ID: "ws-grill", Kind: contract.KindGrilling, Parent: "ws-chart"},
			want: Destination{Placement: Worktree, Branch: "effort/ws-grill", Container: "chart/ws-chart"},
		},
		{
			name: "research → worktree of its own effort branch, integrating onto its PARENT chart",
			bead: bd.Bead{ID: "ws-res", Kind: contract.KindResearch, Parent: "ws-chart"},
			want: Destination{Placement: Worktree, Branch: "effort/ws-res", Container: "chart/ws-chart"},
		},
		{
			name: "prototype → worktree of its own effort branch, integrating onto its PARENT chart, carrying the side ref",
			bead: bd.Bead{ID: "ws-proto", Kind: contract.KindPrototype, Parent: "ws-chart"},
			want: Destination{Placement: Worktree, Branch: "effort/ws-proto", Ref: "refs/mugicrew/prototypes/ws-chart", Container: "chart/ws-chart"},
		},
		{
			name: "build → worktree of its own effort branch, integrating onto its PARENT checkpoint",
			bead: bd.Bead{ID: "ws-build", Kind: contract.KindBuild, Parent: "ws-cp"},
			want: Destination{Placement: Worktree, Branch: "effort/ws-build", Container: "checkpoint/ws-cp"},
		},
		{
			name: "critique → worktree of its own effort branch, integrating onto its PARENT checkpoint",
			bead: bd.Bead{ID: "ws-crit", Kind: contract.KindCritique, Parent: "ws-cp"},
			want: Destination{Placement: Worktree, Branch: "effort/ws-crit", Container: "checkpoint/ws-cp"},
		},
		{
			name: "Base-pinned critique → replaces its PARENT checkpoint at the pinned Tip",
			bead: bd.Bead{ID: "ws-crit", Kind: contract.KindCritique, Parent: "ws-cp", Description: "Base: b4se\nTip: t1p"},
			want: Destination{Placement: Worktree, Branch: "effort/ws-crit", Container: "checkpoint/ws-cp", Mode: Replace, Tip: "t1p"},
		},
		{
			name: "conflict → worktree of its own effort branch cut at the pinned Tip, replacing its PARENT checkpoint",
			bead: bd.Bead{ID: "ws-conf", Kind: contract.KindConflict, Parent: "ws-cp", Description: "Base: b4se\nTip: t1p"},
			want: Destination{Placement: Worktree, Branch: "effort/ws-conf", Container: "checkpoint/ws-cp", Mode: Replace, Tip: "t1p"},
		},
		{
			name: "requested checkpoint rebase → Replace at the pinned Tip, like any conflict",
			bead: bd.Bead{ID: "ws-conf", Kind: contract.KindConflict, Parent: "ws-cp", Description: "requested rebase\nBase: b4se\nTip: t1p\nvariant: requested"},
			want: Destination{Placement: Worktree, Branch: "effort/ws-conf", Container: "checkpoint/ws-cp", Mode: Replace, Tip: "t1p"},
		},
		{
			name: "requested chart rebase → Replace at the pinned Tip",
			bead: bd.Bead{ID: "ws-cc", Kind: contract.KindChartConflict, Parent: "ws-chart", Description: "Base: b4se\nTip: t1p\nvariant: requested"},
			want: Destination{Placement: Worktree, Branch: "effort/ws-cc", Container: "chart/ws-chart", Mode: Replace, Tip: "t1p"},
		},
		{
			name: "surgery → worktree of its own effort branch, integrating onto its PARENT checkpoint",
			bead: bd.Bead{ID: "ws-surg", Kind: contract.KindSurgery, Parent: "ws-cp"},
			want: Destination{Placement: Worktree, Branch: "effort/ws-surg", Container: "checkpoint/ws-cp"},
		},
		{
			name: "chart land (defogged) → detached tree at its OWN chart tip, integrating onto nothing",
			bead: bd.Bead{ID: "ws-chart", Kind: contract.KindChart, Labels: []string{contract.LabelDefogged}},
			want: Destination{Placement: Detached, Branch: "chart/ws-chart"},
		},
		{
			name: "checkpoint land → detached tree at its OWN checkpoint tip, integrating onto nothing",
			bead: bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint},
			want: Destination{Placement: Detached, Branch: "checkpoint/ws-cp"},
		},
		{
			name: "landing → detached tree at its PARENT checkpoint tip",
			bead: bd.Bead{ID: "ws-land", Kind: contract.KindLanding, Parent: "ws-cp"},
			want: Destination{Placement: Detached, Branch: "checkpoint/ws-cp"},
		},
		{
			name: "landing → detached tree at its PARENT chart tip",
			bead: bd.Bead{ID: "ws-land", Kind: contract.KindLanding, Parent: "ws-chart"},
			want: Destination{Placement: Detached, Branch: "chart/ws-chart"},
		},
		{
			name: "signal triage → repo root",
			bead: bd.Bead{ID: "ws-sig", Kind: contract.KindSignal},
			want: Destination{Placement: RepoRoot},
		},
		{
			name: "initiative → repo root: no branch, no worktree, no land (FR46)",
			bead: bd.Bead{ID: "ws-init", Kind: contract.KindInitiative},
			want: Destination{Placement: RepoRoot},
		},
		{
			name: "spec slicing → repo root",
			bead: bd.Bead{ID: "ws-spec", Kind: contract.KindSpec},
			want: Destination{Placement: RepoRoot},
		},
		{
			name: "chart-conflict → worktree of its own effort branch cut at the pinned Tip, replacing its PARENT chart",
			bead: bd.Bead{ID: "ws-cc", Kind: contract.KindChartConflict, Parent: "ws-chart", Description: "Base: b4se\nTip: t1p"},
			want: Destination{Placement: Worktree, Branch: "effort/ws-cc", Container: "chart/ws-chart", Mode: Replace, Tip: "t1p"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Route(c.bead)
			if err != nil {
				t.Fatalf("Route(%+v) unexpected error: %v", c.bead, err)
			}
			if got != c.want {
				t.Errorf("Route(%+v) = %+v, want %+v", c.bead, got, c.want)
			}
		})
	}
}

// The defog-leaf parent-Chart resolver reads bd.Bead.Parent (FR6): the same
// research kind routed to two different parents yields two different containers,
// proving the container is a function of the membership edge, not the id.
func TestRouteResolvesParentChartFromMembershipEdge(t *testing.T) {
	for _, k := range []contract.Kind{contract.KindResearch, contract.KindPrototype, contract.KindChartConflict} {
		a, _ := Route(bd.Bead{ID: "mc-x", Kind: k, Parent: "mc-a"})
		b, _ := Route(bd.Bead{ID: "mc-x", Kind: k, Parent: "mc-b"})
		if a.Container != "chart/mc-a" || b.Container != "chart/mc-b" {
			t.Errorf("%s: parent resolver ignored bd.Bead.Parent: got %q and %q", k, a.Container, b.Container)
		}
	}
}

// An unknown/unwired kind falls through to the repo-root baseline rather than
// mis-routing into a container tree.
func TestRouteUnknownKindDefaultsToRepoRoot(t *testing.T) {
	got, err := Route(bd.Bead{ID: "mc-x", Kind: contract.Kind("unowned")})
	if err != nil {
		t.Fatalf("unknown kind unexpected error: %v", err)
	}
	if got.Placement != RepoRoot {
		t.Errorf("unknown kind should default to RepoRoot, got %+v", got)
	}
}

// A defog leaf missing its parent membership edge (FR6) is malformed input, not
// contention: Route fails loud so it never derives branch "chart/", whose
// exit-128 worktree-add git rejects as an invalid ref on every tick.
// Every parent-derived kind must guard.
func TestRouteEmptyParentIsHardError(t *testing.T) {
	for _, k := range []contract.Kind{contract.KindGrilling, contract.KindSurvey, contract.KindResearch, contract.KindPrototype, contract.KindChartConflict, contract.KindBuild, contract.KindCritique, contract.KindConflict, contract.KindSurgery} {
		got, err := Route(bd.Bead{ID: "mc-orphan", Kind: k}) // Parent: ""
		if err == nil {
			t.Errorf("Route(%s, no parent) = %+v, want hard error", k, got)
		}
	}
}

// OnEffort is the shared "works on its own effort/<id>" predicate: a leaf is, a
// land step (detached) and a repo-root bead are not, and a route error is not
// either but surfaces.
func TestOnEffort(t *testing.T) {
	for _, c := range []struct {
		bead    bd.Bead
		want    bool
		wantErr bool
	}{
		{bd.Bead{ID: "mc-b", Kind: contract.KindBuild, Parent: "ws-cp"}, true, false},
		{bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint}, false, false},
		{bd.Bead{ID: "mc-s", Kind: contract.KindSignal}, false, false},
		{bd.Bead{ID: "mc-b", Kind: contract.KindBuild}, false, true},
	} {
		d, ok, err := OnEffort(c.bead)
		if ok != c.want || (err != nil) != c.wantErr || (ok && d.Branch != "effort/"+c.bead.ID) {
			t.Errorf("OnEffort(%s %s) = %+v, %v, %v; want %v, err %v", c.bead.Kind, c.bead.ID, d, ok, err, c.want, c.wantErr)
		}
	}
}
