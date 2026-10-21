package bd

import (
	"errors"
	"strings"

	"jayulabs.org/mugicrew/contract"
)

// ErrNoHeldCheckpoint is HeldCheckpoint's refusal when the Task holds no open,
// unlanded Checkpoint.
var ErrNoHeldCheckpoint = errors.New("holds no open, unlanded Checkpoint")

// SeveralHeldError is HeldCheckpoint's refusal when the Task holds more than one.
type SeveralHeldError struct{ IDs []string }

func (e *SeveralHeldError) Error() string {
	return "holds several open, unlanded Checkpoints (" + strings.Join(e.IDs, ", ") + ")"
}

// NoSpecError is HeldCheckpoint's refusal when the one held Checkpoint has no Spec.
type NoSpecError struct{ ID string }

func (e *NoSpecError) Error() string { return "its held Checkpoint " + e.ID + " has no Spec" }

// HeldReads are the reads HeldCheckpoint makes, so the verb's probe and the publish
// re-check share one resolver and fake only their own seams.
type HeldReads struct {
	// Up lists the beads the Task blocks (its up blocks edges).
	Up     func(task string) ([]Bead, error)
	Show   func(id string) (Bead, error)
	SpecOf func(checkpoint string) (string, bool, error)
	// Landed reports whether the Checkpoint's trailer is on its repo's mainline.
	Landed func(cp Bead) (bool, error)
}

// LiveHeldReads is HeldReads over bd, with landed supplied by the caller (it needs the
// Checkpoint's repo).
func LiveHeldReads(landed func(Bead) (bool, error)) HeldReads {
	return HeldReads{
		Up:     func(task string) ([]Bead, error) { return depTargets(task, "up", "blocks") },
		Show:   Show,
		SpecOf: SpecOf,
		Landed: landed,
	}
}

// HeldCheckpoint is the Task's one open, unlanded Checkpoint and its Spec: the Task's
// up blocks targets kept to kind:checkpoint, kept open, dropped when their land
// trailer is on mainline. Other held kinds (Chart, Rollback, another Task) are
// ignored. It returns ErrNoHeldCheckpoint, *SeveralHeldError or *NoSpecError rather
// than pick a first match.
func HeldCheckpoint(task string, r HeldReads) (cp Bead, spec string, err error) {
	up, err := r.Up(task)
	if err != nil {
		return Bead{}, "", err
	}
	var held []Bead
	for _, row := range up {
		if row.Kind != contract.KindCheckpoint {
			continue
		}
		b, err := r.Show(row.ID)
		if err != nil {
			return Bead{}, "", err
		}
		if b.Kind != contract.KindCheckpoint || !b.Open() {
			continue
		}
		landed, err := r.Landed(b)
		if err != nil {
			return Bead{}, "", err
		}
		if !landed {
			held = append(held, b)
		}
	}
	switch len(held) {
	case 0:
		return Bead{}, "", ErrNoHeldCheckpoint
	case 1:
	default:
		return Bead{}, "", &SeveralHeldError{IDs: IDs(held)}
	}
	spec, ok, err := r.SpecOf(held[0].ID)
	if err != nil {
		return Bead{}, "", err
	}
	if !ok {
		return Bead{}, "", &NoSpecError{ID: held[0].ID}
	}
	return held[0], spec, nil
}
