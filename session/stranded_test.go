package session

import (
	"errors"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/route"
)

// A landed-refused child's stranded-commits Signal names the range a reader can
// diff: squash mode from the container ref now (the landed squash's Tip: when
// the ref is gone), replace mode from the child's Base: pin; a nested effort
// names its Dir. The landing sha is the mainline trailer's, never the branch's.
func TestStrandedSignal(t *testing.T) {
	reads := landedReads{
		landedBase: func(trailer string) (string, error) {
			if trailer != "Checkpoint: mc-cp" {
				t.Errorf("trailer = %q", trailer)
			}
			return "land123", nil
		},
		head: func(ref string) (string, error) {
			if ref == "refs/heads/checkpoint/mc-cp" {
				return "head456", nil
			}
			return "", git.ErrMissingRef
		},
		landedTip: func(sha string) (string, bool) { return "tip789", sha == "land123" },
	}
	build := bd.Bead{ID: "mc-b", Title: "Do it", Kind: contract.KindBuild, Parent: "mc-cp"}
	crit := bd.Bead{ID: "mc-c", Title: "Fix it", Kind: contract.KindCritique, Parent: "mc-cp",
		Description: "Base: base111\nTip: tip222\n"}
	gone := reads
	gone.head = func(string) (string, error) { return "", git.ErrMissingRef }
	failing := reads
	failing.head = func(string) (string, error) { return "", errors.New("boom") }

	cases := []struct {
		name  string
		b     bd.Bead
		dir   string
		reads landedReads
		want  []string
		err   bool
	}{
		{"squash", build, "", reads, []string{
			"Do it: commits stranded after mc-cp landed",
			"mc-b (Do it) was open when mc-cp landed at land123; its integrate was refused.\n",
			"Landed: land123\n", "Branch: effort/mc-b\n", "Range: head456..effort/mc-b\n",
			"Fix: these commits are not on mainline. Land them as new work.\n"}, false},
		{"squash, ref missing", build, "", gone, []string{"Range: tip789..effort/mc-b\n"}, false},
		{"squash, ref missing, no Tip:", build, "", landedReads{reads.landedBase, gone.head, func(string) (string, bool) { return "", false }}, nil, true},
		{"squash, head read fails", build, "", failing, nil, true},
		{"replace", crit, "", reads, []string{"Range: base111..effort/mc-c\n", "Landed: land123\n"}, false},
		{"replace without Base:", bd.Bead{ID: "mc-c", Kind: contract.KindConflict, Parent: "mc-cp", Description: "Tip: t\n"}, "", reads, nil, true},
		{"nested", build, "svc", reads, []string{"Repo: svc\nLanded: land123\n"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := route.Route(c.b)
			if err != nil {
				t.Fatal(err)
			}
			s, err := strandedSignal(c.b, d, c.dir, c.reads)
			if (err != nil) != c.err {
				t.Fatalf("err = %v, want error %v", err, c.err)
			}
			for _, w := range c.want {
				if !strings.Contains(s.Title+"\n"+s.Body, w) {
					t.Errorf("missing %q in title %q body:\n%s", w, s.Title, s.Body)
				}
			}
			if c.dir == "" && strings.Contains(s.Body, "Repo:") {
				t.Errorf("non-nested body names a Repo:\n%s", s.Body)
			}
		})
	}
}

// settle releases an in_progress Initiative; closed and open ones, and in_progress
// Task, Surgery and Discussion, are left alone (Spec mc-so2z FR8, FR9).
func TestSettleInitiativeClaim(t *testing.T) {
	origShow, origRelease := showBead, releaseClaim
	defer func() { showBead, releaseClaim = origShow, origRelease }()
	for _, tc := range []struct {
		kind    contract.Kind
		status  string
		release bool
	}{
		{contract.KindInitiative, "in_progress", true},
		{contract.KindInitiative, "closed", false},
		{contract.KindInitiative, "open", false},
		{contract.KindTask, "in_progress", false},
		{contract.KindSurgery, "in_progress", false},
		{contract.KindDiscussion, "in_progress", false},
		{contract.KindCheckup, "in_progress", false},
	} {
		t.Run(string(tc.kind)+"/"+tc.status, func(t *testing.T) {
			showBead = func(id string) (bd.Bead, error) { return bd.Bead{ID: id, Kind: tc.kind, Status: tc.status}, nil }
			released := false
			releaseClaim = func(string) error { released = true; return nil }
			if _, err := settle("b"); err != nil || released != tc.release {
				t.Errorf("released = %v, err = %v, want released %v", released, err, tc.release)
			}
		})
	}
}
