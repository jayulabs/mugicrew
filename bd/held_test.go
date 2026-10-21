package bd

import (
	"errors"
	"slices"
	"testing"

	"jayulabs.org/mugicrew/contract"
)

// HeldCheckpoint is the Task's one open, unlanded Checkpoint (FR 2–4): the Task's up
// blocks rows, kept to kind:checkpoint, open, and not landed by trailer; any other
// held kind is ignored. Dep list, show, SpecOf and Landed are stubbed.
func TestHeldCheckpoint(t *testing.T) {
	cp := func(id, status string) Bead { return Bead{ID: id, Kind: contract.KindCheckpoint, Status: status} }
	chart := Bead{ID: "c1", Kind: contract.KindChart, Status: "open"}
	rollback := Bead{ID: "r1", Kind: contract.KindRollback, Status: "open"}
	task := Bead{ID: "t2", Kind: contract.KindTask, Status: "open"}
	for _, c := range []struct {
		name    string
		up      []Bead
		landed  map[string]bool
		noSpec  bool
		want    string
		wantErr func(error) bool
	}{
		{name: "one open unlanded", up: []Bead{cp("cp1", "open")}, want: "cp1"},
		{name: "none", up: nil, wantErr: func(e error) bool { return errors.Is(e, ErrNoHeldCheckpoint) }},
		{name: "only other kinds", up: []Bead{chart, rollback, task}, wantErr: func(e error) bool { return errors.Is(e, ErrNoHeldCheckpoint) }},
		{name: "closed one is not held", up: []Bead{cp("cp1", "closed")}, wantErr: func(e error) bool { return errors.Is(e, ErrNoHeldCheckpoint) }},
		{name: "landed by trailer", up: []Bead{cp("cp1", "open")}, landed: map[string]bool{"cp1": true},
			wantErr: func(e error) bool { return errors.Is(e, ErrNoHeldCheckpoint) }},
		{name: "beside chart rollback and task", up: []Bead{chart, cp("cp1", "in_progress"), rollback, task}, want: "cp1"},
		{name: "landed one beside an unlanded", up: []Bead{cp("cp0", "open"), cp("cp1", "open")}, landed: map[string]bool{"cp0": true}, want: "cp1"},
		{name: "several", up: []Bead{cp("cp1", "open"), cp("cp2", "open")}, wantErr: func(e error) bool {
			var s *SeveralHeldError
			return errors.As(e, &s) && slices.Equal(s.IDs, []string{"cp1", "cp2"})
		}},
		{name: "no spec", up: []Bead{cp("cp1", "open")}, noSpec: true, wantErr: func(e error) bool {
			var s *NoSpecError
			return errors.As(e, &s) && s.ID == "cp1"
		}},
	} {
		r := HeldReads{
			Up: func(string) ([]Bead, error) { return c.up, nil },
			Show: func(id string) (Bead, error) {
				return c.up[slices.IndexFunc(c.up, func(b Bead) bool { return b.ID == id })], nil
			},
			SpecOf: func(string) (string, bool, error) { return "sp1", !c.noSpec, nil },
			Landed: func(b Bead) (bool, error) { return c.landed[b.ID], nil },
		}
		got, spec, err := HeldCheckpoint("t1", r)
		switch {
		case c.wantErr != nil:
			if err == nil || !c.wantErr(err) {
				t.Errorf("%s: err = %v, want the typed refusal", c.name, err)
			}
		case err != nil || got.ID != c.want || spec != "sp1":
			t.Errorf("%s: = %q, %q, %v; want %q, sp1", c.name, got.ID, spec, err, c.want)
		}
	}
}

func TestHeldCheckpointReadErrorsPropagate(t *testing.T) {
	boom := errors.New("boom")
	_, _, err := HeldCheckpoint("t1", HeldReads{Up: func(string) ([]Bead, error) { return nil, boom }})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want the read error", err)
	}
}
