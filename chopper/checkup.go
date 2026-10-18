package chopper

import (
	_ "embed"
	"errors"
	"fmt"
	"os"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/registry"
)

// Chopper is the Checkup crewmate (ADR 0272): a human-started HITL session that
// diagnoses and repairs one broken bead. It carries no state; the harness arrives
// on the ctx, mirroring Robin. The deterministic Recover code in this package is
// separate and runs no session.
type Chopper struct{}

// Chopper satisfies the crewmate contract.
var _ contract.Crewmate = Chopper{}

// Run is the AFK path, never reached: a Checkup needs a human in the room.
func (Chopper) Run(_ contract.Ctx, bead contract.Bead) error {
	return fmt.Errorf("chopper.Run: kind %q is HITL (checkup), not a loop kind — open it via Launch", bead.Kind)
}

// Launch composes the one HITL command a human opens for a Checkup. A
// non-checkup kind is a loud error.
func (Chopper) Launch(ctx contract.Ctx, bead contract.Bead) (contract.LaunchCommand, error) {
	if bead.Kind != contract.KindCheckup {
		return nil, fmt.Errorf("chopper.Launch: kind %q is not a launchable HITL kind", bead.Kind)
	}
	e, err := registry.Lookup(bead.Kind)
	if err != nil {
		return nil, err
	}
	inv, err := contract.NewInvocation(bead.ID, checkupPrompt(bead.ID))
	if err != nil {
		return nil, err
	}
	inv.ReadOnly = e.ReadOnly
	return ctx.Harness().LaunchCommand(inv)
}

//go:embed prompts/checkup.md
var checkupTmpl string

// checkupPrompt is Chopper's procedure over one Checkup bead (Spec D2). Pure —
// asserted through the Launch seam test.
func checkupPrompt(id string) string {
	return fmt.Sprintf(checkupTmpl, id, contract.DraftInstruction(id))
}

// CheckupRefusal is the one launch check the checkup verb and the TUI's c share
// (ADR 0274 §4): nil to go, else the refusal that names the next step. In order:
// the target is a Checkup; a live AFK effort holds it; a HITL session is open on
// it; its dead claim has a clean recover verdict. An ambiguous or refused verdict
// goes, those state faults being the Checkup's, as does a closed target. The
// verdict is recover's own inventory and judgment, not a copy.
func CheckupRefusal(target bd.Bead) error {
	id, title := target.ID, target.Title
	switch {
	case target.Kind == contract.KindCheckup:
		return fmt.Errorf("%s is a Checkup: it has nothing to check up", id)
	case target.Status == "closed", bd.PausedBy(target, bd.PausedCheckup): // a fenced target is re-attached, not refused
		return nil
	}
	inv, err := Take(id)
	if err != nil {
		return err
	}
	hs, err := look(Opts{})
	if err != nil {
		return err
	}
	v, jerr := judgeIn(hs, inv, Opts{}, lineage(os.Getpid()), false)
	var rf *Refused
	alive := errors.As(jerr, &rf) && rf.Verdict == Alive
	switch s := hs.sessions[id]; {
	case alive && !s.Has():
		return fmt.Errorf("an effort is running on %s: abort it (x) first", title)
	case s.Has():
		return fmt.Errorf("a session is open on %s", title)
	case jerr == nil && v == Dead && inv.stranded():
		return errors.New("stranded by a dead effort: recover it (x)")
	}
	return nil
}

// CheckupFiling is FileCheckup's reads and writes, so each caller's tests script them.
type CheckupFiling struct {
	Refusal func(target bd.Bead) error
	OpenOn  func(target string) (bd.Bead, bool, error)
	New     func(target bd.Bead, problem string) (string, error)
	Fence   func(id, cause string) error
}

// FileCheckup is the create-and-fence step the verb and the TUI's c share: refuse,
// find or file target's Checkup, fence target with paused:checkup. It returns the
// Checkup's id; the caller launches or attaches.
func FileCheckup(x CheckupFiling, target bd.Bead, problem string) (string, error) {
	if err := x.Refusal(target); err != nil {
		return "", err
	}
	ck, found, err := x.OpenOn(target.ID)
	if err != nil {
		return "", err
	}
	id := ck.ID
	if !found {
		if id, err = x.New(target, problem); err != nil {
			if id != "" {
				return "", fmt.Errorf("filed Checkup %s but its edge to %s failed (close it or add the related edge by hand): %w", id, target.ID, err)
			}
			return "", err
		}
	}
	if err := x.Fence(target.ID, bd.PausedCheckup); err != nil {
		return "", fmt.Errorf("Checkup %s is filed but fencing %s failed (re-run to attach): %w", id, target.ID, err)
	}
	return id, nil
}
