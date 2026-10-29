package bd

import "slices"

// InProgressAssignedTo lists the in_progress beads whose assignee is exactly name:
// the claims made under a bare user name before the Bead actor gained a Device
// (ADR 0376 §5). dir is where bd resolves the graph from ("" for this cwd).
func InProgressAssignedTo(dir, name string) ([]Bead, error) {
	out, err := runIn(dir, "list", "--status", "in_progress", "--assignee", name, "-n", "0", "--json")
	if err != nil {
		return nil, err
	}
	bs, err := parseBeads([]byte(out))
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(bs, func(b Bead) bool { return b.Status != "in_progress" || b.Assignee != name }), nil
}
