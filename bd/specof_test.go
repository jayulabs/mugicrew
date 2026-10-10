package bd

import (
	"testing"

	"jayulabs.org/mugicrew/contract"
)

// specOf walks Checkpoint →caused-by→ Spec, or through a Rollback: Checkpoint
// →caused-by→ Rollback →related→ old Checkpoint, repeated (ADR 0048 §2). The graph
// is a fake: causedBy keyed "<id>/<kind>", related keyed by Rollback id.
func TestSpecOfWalksThroughRollbacks(t *testing.T) {
	for _, c := range []struct {
		name     string
		from     string
		causedBy map[string]string
		related  map[string]string
		want     string
		wantOK   bool
	}{
		{"spec-caused", "cp1", map[string]string{"cp1/spec": "sp"}, nil, "sp", true},
		{"one rollback", "cp2",
			map[string]string{"cp1/spec": "sp", "cp2/rollback": "rb1"},
			map[string]string{"rb1": "cp1"}, "sp", true},
		{"chained rollbacks", "cp3",
			map[string]string{"cp1/spec": "sp", "cp2/rollback": "rb1", "cp3/rollback": "rb2"},
			map[string]string{"rb1": "cp1", "rb2": "cp2"}, "sp", true},
		{"neither edge", "cp1", nil, nil, "", false},
		{"rollback with no target", "cp3",
			map[string]string{"cp3/rollback": "rb2"}, nil, "", false},
		{"rollback cycle", "cp3",
			map[string]string{"cp3/rollback": "rb1", "cp4/rollback": "rb2"},
			map[string]string{"rb1": "cp4", "rb2": "cp3"}, "", false},
	} {
		causedBy := func(id string, k contract.Kind) (string, bool, error) {
			p, ok := c.causedBy[id+"/"+string(k)]
			return p, ok, nil
		}
		target := func(id string) (string, bool, error) {
			cp, ok := c.related[id]
			return cp, ok, nil
		}
		got, ok, err := specOf(c.from, causedBy, target)
		if err != nil || got != c.want || ok != c.wantOK {
			t.Errorf("%s: specOf(%s) = %q, %v, %v; want %q, %v, nil", c.name, c.from, got, ok, err, c.want, c.wantOK)
		}
	}
}
