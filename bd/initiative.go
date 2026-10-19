package bd

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// RefusedError is a guarded Initiative act refused with nothing written (Spec
// D9): Blocking names the beads in the way.
type RefusedError struct {
	What, Why string
	Blocking  []Bead
}

func (e *RefusedError) Error() string {
	names := make([]string, len(e.Blocking))
	for i, b := range e.Blocking {
		names[i] = b.ID + " " + b.Title
	}
	return fmt.Sprintf("%s refused: %s:\n  %s", e.What, e.Why, strings.Join(names, "\n  "))
}

// AdoptChain is `mugicrew initiative adopt` (Spec D5, FR13–17): it refuses a
// closed head, an Initiative head (Initiatives never nest), a head inside a
// mugicrew container other than an Initiative (a Build, a defog bead), and a
// target that is not an open Initiative; otherwise one bd update --parent
// re-parents the head alone, replacing any prior Initiative, so a move is one
// call. Closed predecessors keep their place. Adoption is a human act.
func AdoptChain(head, initiative string) error {
	all, err := ListAll()
	if err != nil {
		return err
	}
	in, _ := index(all)
	h, ok := in[head]
	if !ok {
		return fmt.Errorf("initiative adopt: no bead %s", head)
	}
	ini, ok := in[initiative]
	if !ok {
		ini = Bead{ID: initiative}
	}
	switch p := in[h.Parent]; {
	case h.Status == "closed":
		return refuse("adopt", head, "the head is closed", []Bead{h})
	case h.Kind == contract.KindInitiative:
		return refuse("adopt", head, "Initiatives never nest", []Bead{h})
	case p.Kind != "" && p.Kind != contract.KindInitiative:
		return refuse("adopt", head, "not a chain head: it is a member of", []Bead{p})
	case ini.Kind != contract.KindInitiative || ini.Status == "closed":
		return refuse("adopt", head, "the target is not an open Initiative", []Bead{ini})
	}
	if _, err := run("update", head, "--parent", initiative); err != nil {
		return err
	}
	slog.Info("initiative.adopt", "bead", head, "initiative", initiative, "from", h.Parent)
	return nil
}

// requireOpenInitiative refuses, with AdoptChain's wording, an id that is not an open Initiative.
func requireOpenInitiative(verb, id string) error {
	all, err := ListAll()
	if err != nil {
		return err
	}
	in, _ := index(all)
	ini, ok := in[id]
	if !ok {
		ini = Bead{ID: id}
	}
	if ini.Kind != contract.KindInitiative || ini.Status == "closed" {
		return refuse(verb, id, "the target is not an open Initiative", []Bead{ini})
	}
	return nil
}

// ReleaseChain is `mugicrew initiative release` (Spec D5, FR16): it clears an
// unclosed member chain head's parent, refusing a closed head or a bead that
// is no Initiative's member.
func ReleaseChain(head string) error {
	all, err := ListAll()
	if err != nil {
		return err
	}
	in, _ := index(all)
	h, ok := in[head]
	switch {
	case !ok:
		return fmt.Errorf("initiative release: no bead %s", head)
	case h.Status == "closed":
		return refuse("release", head, "the head is closed", []Bead{h})
	case in[h.Parent].Kind != contract.KindInitiative:
		return refuse("release", head, "not a member of an Initiative", []Bead{h})
	case RequireInitiativesOn():
		return refuse("release", head, ReleaseRequiredWhy(head), []Bead{h})
	}
	if _, err := run("update", head, "--parent", ""); err != nil {
		return err
	}
	slog.Info("initiative.release", "bead", head, "initiative", h.Parent)
	return nil
}

// RequireInitiativesOn reports Require Initiatives (ADR 0224) for the Workspace,
// read from the Workspace root, not the cwd.
func RequireInitiativesOn() bool {
	root, err := git.RepoRoot()
	return err == nil && cfglayer.InitiativesSetting(root).Required
}

// ReleaseRequiredWhy is release's refusal under Require Initiatives (ADR 0224): a
// chain may move between Initiatives but never drop out of one. The TUI's A shows it too.
func ReleaseRequiredWhy(head string) string {
	return "Require Initiatives is on: move it with mugicrew initiative adopt " + head + " <other>"
}

func refuse(verb, id, why string, blocking []Bead) error {
	slog.Warn("initiative."+verb+".refused", "bead", id, "reason", why, "blocking", IDs(blocking))
	return &RefusedError{What: "initiative " + verb + " " + id, Why: why, Blocking: blocking}
}

// index maps a snapshot by id and by parent, children in snapshot order.
func index(all []Bead) (map[string]Bead, map[string][]Bead) {
	in, kids := map[string]Bead{}, map[string][]Bead{}
	for _, b := range all {
		in[b.ID] = b
		if b.Parent != "" {
			kids[b.Parent] = append(kids[b.Parent], b)
		}
	}
	return in, kids
}

// subtree is every descendant of id, post-order (each bead after its children).
func subtree(id string, kids map[string][]Bead) []Bead {
	var out []Bead
	for _, k := range kids[id] {
		out = append(append(out, subtree(k.ID, kids)...), k)
	}
	return out
}

// unclosedLeavesFirst is id's unclosed descendants, each after its children.
// Unclosed is anything not closed: open, in_progress, blocked, deferred (D6).
func unclosedLeavesFirst(id string, kids map[string][]Bead) []Bead {
	return Filter(subtree(id, kids), func(b Bead) bool { return b.Status != "closed" })
}

// Filter is the beads of bs for which keep holds, in order.
func Filter(bs []Bead, keep func(Bead) bool) []Bead {
	var out []Bead
	for _, b := range bs {
		if keep(b) {
			out = append(out, b)
		}
	}
	return out
}

// InitiativeState is an Initiative's derived state (Spec D6, ADR 0133): never
// stored, so it can't drift.
type InitiativeState string

const (
	InitiativeClosed   InitiativeState = "closed"
	InitiativeDrained  InitiativeState = "drained"
	InitiativeUnderWay InitiativeState = "under way"
)

// HasDoneWhen reports whether an Initiative body carries the "## Done when"
// heading; the one parse of it (Spec D1), so a bodyless Initiative is defined once.
func HasDoneWhen(body string) bool {
	_, _, ok := strings.Cut(body, "## Done when")
	return ok
}

// StateOf derives ini's state over the snapshot all, first match wins: closed;
// drained when no child is unclosed (open, in_progress, blocked and deferred
// Drafts all count); else under way.
func StateOf(ini Bead, all []Bead) InitiativeState {
	switch {
	case ini.Status == "closed":
		return InitiativeClosed
	case slices.ContainsFunc(all, func(b Bead) bool { return b.Parent == ini.ID && b.Status != "closed" }):
		return InitiativeUnderWay
	default:
		return InitiativeDrained
	}
}
