package nami

import (
	"slices"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// ADR 0225 §5: a renumber Checkpoint's scope walks caused-by through renumber
// Checkpoints to the first non-renumber container; ordinary and Rollback-successor
// Checkpoints keep their own subtree.
func TestRenumberScope(t *testing.T) {
	subtree := func(id string) ([]string, error) { return []string{id + ".1"}, nil }
	// graph: r2 -> r1 (renumber, caused-by chart) -> ch ; ord -> spec ; rb -> rollback
	chart := map[string]string{"r1": "ch"}
	cp := map[string]string{"r2": "r1"}
	causedBy := func(id string, want contract.Kind) (string, bool, error) {
		m := map[contract.Kind]map[string]string{contract.KindChart: chart, contract.KindCheckpoint: cp}[want]
		p, ok := m[id]
		return p, ok, nil
	}
	for _, c := range []struct {
		id   string
		want []string
	}{
		{"r1", []string{"r1", "r1.1", "ch", "ch.1"}},
		{"r2", []string{"r2", "r2.1", "r1", "r1.1", "ch", "ch.1"}},
		{"ord", []string{"ord", "ord.1"}},
		{"rb", []string{"rb", "rb.1"}},
	} {
		got, widened, err := RenumberScope(bd.Bead{ID: c.id, Kind: contract.KindCheckpoint}, subtree, causedBy)
		if err != nil || !slices.Equal(got, c.want) || widened != (len(c.want) > 2) {
			t.Errorf("%s: got %v %v %v, want %v", c.id, got, widened, err, c.want)
		}
	}
}
