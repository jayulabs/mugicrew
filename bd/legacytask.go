package bd

import (
	"fmt"
	"slices"

	"jayulabs.org/mugicrew/contract"
)

// LegacyTaskLabel is the pre-rename kind label (ADR 0354); LegacyTaskBeads finds
// what still carries it and RelabelLegacyTasks moves it to kind:errand (ADR 0355).
const LegacyTaskLabel = "kind:task"

// LegacyTaskBeads lists the beads still labelled kind:task in every status, closed
// ones included. Doctor, the loop's start refusal and the TUI dispatch all read it,
// so the three agree. dir is where bd resolves the graph from ("" for this cwd).
func LegacyTaskBeads(dir string) ([]Bead, error) {
	out, err := runIn(dir, "list", "--all", "-n", "0", "--label", LegacyTaskLabel, "--json")
	if err != nil {
		return nil, err
	}
	bs, err := parseBeads([]byte(out))
	if err != nil {
		return nil, err
	}
	// bd's label filter is the source; this keeps the answer exact if it ever widens.
	return slices.DeleteFunc(bs, func(b Bead) bool { return !b.HasLabel(LegacyTaskLabel) }), nil
}

// RelabelLegacyTasks gives every kind:task bead kind:errand and drops kind:task, in
// place and whatever its status, returning how many it changed. One label update per
// bead: it leaves closed_at, the claim and the body alone and adds no comment, so
// bd's label events are the only trace (ADR 0355). A bead already carrying
// kind:errand just loses kind:task; a second run finds nothing.
func RelabelLegacyTasks(dir string) (int, error) {
	beads, err := LegacyTaskBeads(dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, b := range beads {
		errandLabel := "kind:" + string(contract.KindErrand)
		args := []string{"update", b.ID, "--remove-label", LegacyTaskLabel}
		if !b.HasLabel(errandLabel) {
			args = append(args, "--add-label", errandLabel)
		}
		if _, err := runIn(dir, args...); err != nil {
			return n, fmt.Errorf("relabel %s: %w", b.ID, err)
		}
		n++
	}
	return n, nil
}

// RefuseLegacyTasks is the loop's start check and the TUI's dispatch check: nil when
// no bead carries kind:task, else an error naming the count and `mugicrew doctor fix`
// (ADR 0355). It only reads; relabelling unasked would write to the graph. It has no
// version flag, so a bead an old binary files after the cutover is caught too.
func RefuseLegacyTasks(dir string) error {
	beads, err := LegacyTaskBeads(dir)
	if err != nil {
		return err
	}
	if len(beads) == 0 {
		return nil
	}
	return fmt.Errorf("%d bead(s) still labelled kind:task, which mugicrew no longer reads: run `mugicrew doctor fix` to relabel them kind:errand", len(beads))
}
