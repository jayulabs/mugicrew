package bd

import (
	"fmt"

	"jayulabs.org/mugicrew/contract"
)

// OffshootPlacement is the one cause → container → Initiative climb an offshoot Signal takes,
// shared by `signal offshoot` and the loop's spawn helpers so the two doors never place the
// same cause differently. The container is the cause's Checkpoint or Chart parent, or an
// unclosed member Errand itself; the Initiative is the container's parent in any status (a
// closed Initiative is not "no Initiative"), else the cause's own Initiative parent (a
// Discussion supplies none). It returns the Initiative parent id (empty when outside), the
// Initiative: line, the container found (zero when none) and the climbed Initiative (zero when
// none, whatever its status). show reads a bead by id.
func OffshootPlacement(cause Bead, show func(string) (Bead, error)) (parent, line string, container, init Bead, err error) {
	if cause.Parent != "" {
		p, err := show(cause.Parent)
		if err != nil {
			return "", "", Bead{}, Bead{}, fmt.Errorf("container of %s: %w", cause.ID, err)
		}
		switch {
		case p.Kind == contract.KindCheckpoint || p.Kind == contract.KindChart:
			container = p
		case p.Kind == contract.KindInitiative && cause.Kind != contract.KindDiscussion:
			init = p
			if cause.Kind == contract.KindErrand && cause.Unclosed() {
				container = cause
			}
		}
	}
	if container.ID != "" && init.ID == "" && container.Parent != "" {
		if init, err = ParentInitiative(container, show); err != nil {
			return "", "", Bead{}, Bead{}, fmt.Errorf("initiative %s: %w", container.Parent, err)
		}
	}
	orphan := cause.Title
	if container.ID != "" {
		orphan = container.Title
	}
	parent, line = InitiativePlacement(init, orphan)
	return parent, line, container, init, nil
}
