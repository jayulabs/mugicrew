package bd

import (
	"fmt"
	"slices"

	"jayulabs.org/mugicrew/contract"
)

// OpenChartConflict is the unclosed kind:chart-conflict bead blocking on id, when id is
// the escalation grilling that conflict drafted (Spec D6). While one is open the
// grilling closes only through its launch as answered; supersede, withdraw and obsolete
// all refuse it by this one predicate.
func OpenChartConflict(id string) (string, bool, error) {
	waiting, err := Blocking(id)
	if err != nil {
		return "", false, err
	}
	cc, open := openChartConflictIn(waiting)
	return cc, open, nil
}

// openChartConflictIn is the unclosed chart-conflict among waiting, the beads blocked on
// the grilling; withdraw's plan passes the ones it already read.
func openChartConflictIn(waiting []Bead) (string, bool) {
	i := slices.IndexFunc(waiting, func(b Bead) bool { return b.Kind == contract.KindChartConflict && b.Unclosed() })
	if i < 0 {
		return "", false
	}
	return waiting[i].ID, true
}

// ChartConflictRefusal is the error a close door gives an escalation grilling target
// whose chart-conflict cc is still open: obsolete, supersede and withdraw all say it.
func ChartConflictRefusal(verb, target, cc string) error {
	return fmt.Errorf("%s %s: its chart-conflict %s is still open; answer it through the launch, or withdraw the Chart to give up", verb, target, cc)
}

// refuseOpenEscalation is ChartConflictRefusal for a grilling t whose chart-conflict is
// open, and nil for any other bead.
func refuseOpenEscalation(verb string, t Bead) error {
	if t.Kind != contract.KindGrilling {
		return nil
	}
	cc, open, err := OpenChartConflict(t.ID)
	if err != nil {
		return err
	}
	if open {
		return ChartConflictRefusal(verb, t.ID, cc)
	}
	return nil
}

// IsDefogKind reports whether k is a defog bead kind the crew doors may close:
// grilling, research or prototype.
func IsDefogKind(k contract.Kind) bool {
	return k == contract.KindGrilling || k == contract.KindResearch || k == contract.KindPrototype
}

// Obsolete is the door for a defog bead a deciding session's decision made moot: it
// closes target obsolete: <decider>. target must be an unclosed grilling, research or
// prototype; decider an existing grilling, research, prototype or Survey other than
// target. An escalation grilling with its chart-conflict still open is refused. Every
// refusal comes before the write; the close is forced so an open blocker can't refuse it.
func Obsolete(target, decider string) error {
	if target == decider {
		return fmt.Errorf("obsolete %s: a bead cannot be made moot by itself", target)
	}
	t, err := Show(target)
	if err != nil {
		return err
	}
	if !IsDefogKind(t.Kind) {
		return fmt.Errorf("obsolete %s: a %s bead is not a defog bead (grilling, research or prototype)", target, t.Kind)
	}
	if !t.Unclosed() {
		return fmt.Errorf("obsolete %s: already closed", target)
	}
	d, err := Show(decider)
	if err != nil {
		return fmt.Errorf("obsolete %s: --by %s: %w", target, decider, err)
	}
	if !IsDefogKind(d.Kind) && d.Kind != contract.KindSurvey {
		return fmt.Errorf("obsolete %s: --by %s is a %s bead, not a grilling, research, prototype or Survey", target, decider, d.Kind)
	}
	if err := refuseOpenEscalation("obsolete", t); err != nil {
		return err
	}
	return core.Close([]string{target}, ObsoleteBy(decider), true)
}

// SupersedeDefog is the door for a defog bead whose question moved to another bead (a
// duplicate included): it closes target superseded: <with> and adds the supersedes edge
// through Retire. target must be an unclosed grilling, research or prototype, with an
// existing bead other than target; an escalation grilling with its chart-conflict still
// open is refused. A target already closed superseded: <with> passes through, so a
// re-run after a crash adds only the missing edge; any other closed target is refused.
// Every refusal comes before the write.
func SupersedeDefog(target, with string) error {
	if target == with {
		return fmt.Errorf("supersede %s: a bead cannot be replaced by itself", target)
	}
	t, err := Show(target)
	if err != nil {
		return err
	}
	if !IsDefogKind(t.Kind) {
		return fmt.Errorf("supersede %s: a %s bead is not a defog bead (grilling, research or prototype)", target, t.Kind)
	}
	reason := SupersededReason + " " + with
	if _, err := Show(with); err != nil {
		return fmt.Errorf("supersede %s: --with %s: %w", target, with, err)
	}
	if !t.Unclosed() {
		if t.CloseReason != reason {
			return fmt.Errorf("supersede %s: already closed", target)
		}
		return Retire(target, with, reason)
	}
	if err := refuseOpenEscalation("supersede", t); err != nil {
		return err
	}
	return Retire(target, with, reason)
}

// OpenChartConflictIn is OpenChartConflict over an already-read graph.
func OpenChartConflictIn(id string, all []Bead) (string, bool) {
	return openChartConflictIn(Filter(all, func(x Bead) bool { return slices.Contains(x.BlockedBy, id) }))
}
