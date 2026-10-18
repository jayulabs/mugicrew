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

// CancelInitiative is `mugicrew initiative cancel` (Spec D9, FR33–34): over one
// snapshot it collects every unclosed member at any depth and refuses, listing
// them, if any is inFlight; otherwise it closes them leaves-first, then the
// Initiative, in one forced bd close with reason "cancelled: <initiative>"
// (members may block each other; the human already decided). Landed work stays.
func CancelInitiative(id string, inFlight func(Bead) bool) error {
	all, err := ListAll()
	if err != nil {
		return err
	}
	in, kids := index(all)
	ini, ok := in[id]
	if !ok || ini.Kind != contract.KindInitiative || ini.Status == "closed" {
		return fmt.Errorf("initiative cancel: %s is not an unclosed Initiative", id)
	}
	members := unclosedLeavesFirst(id, kids)
	if busy := filter(append(members[:len(members):len(members)], ini), inFlight); len(busy) > 0 {
		return refuse("cancel", id, "in-flight effort", busy)
	}
	reason := "cancelled: " + id
	if err := ForceCloseAll(append(IDs(members), id), reason); err != nil {
		return err
	}
	slog.Info("initiative.cancel", "bead", id, "closed", strings.Join(IDs(members), ","), "reason", reason)
	return nil
}

// WithdrawChain is `mugicrew initiative withdraw` (Spec D9, FR35–37): head must
// be an unclosed member chain head of an unclosed Initiative. It refuses,
// listing them, while an unclosed member of that Initiative is ordered after the
// chain (blocks/conditional-blocks on any bead in it) or while the chain runs;
// otherwise it closes the chain's unclosed beads leaves-first with reason
// withdrawn, spawning no successor. The last chain withdrawn leaves the
// Initiative drained.
// ponytail: the chain is head plus its subtree; closed predecessors' Ordering
// edges were carried onto the head at succession (bd.Carry), so they're not walked.
func WithdrawChain(head string, inFlight func(Bead) bool) error {
	all, err := ListAll()
	if err != nil {
		return err
	}
	in, kids := index(all)
	h, ok := in[head]
	if !ok || h.Status == "closed" || h.Kind == contract.KindInitiative ||
		in[h.Parent].Kind != contract.KindInitiative || in[h.Parent].Status == "closed" {
		return fmt.Errorf("initiative withdraw: %s is not an unclosed member chain head of an unclosed Initiative", head)
	}
	chain := map[string]bool{head: true}
	for _, b := range subtree(head, kids) {
		chain[b.ID] = true
	}
	var after []Bead
	for _, m := range unclosedLeavesFirst(h.Parent, kids) {
		if !chain[m.ID] && slices.ContainsFunc(m.BlockedBy, func(id string) bool { return chain[id] }) {
			after = append(after, m)
		}
	}
	if len(after) > 0 {
		return refuse("withdraw", head, "members ordered after the chain", after)
	}
	closing := append(unclosedLeavesFirst(head, kids), h)
	if busy := filter(closing, inFlight); len(busy) > 0 {
		return refuse("withdraw", head, "in-flight effort", busy)
	}
	if err := ForceCloseAll(IDs(closing), "withdrawn"); err != nil {
		return err
	}
	slog.Info("initiative.withdraw", "bead", head, "parent", h.Parent, "closed", strings.Join(IDs(closing), ","))
	return nil
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
	case requireOn():
		return refuse("release", head, ReleaseRequiredWhy(head), []Bead{h})
	}
	if _, err := run("update", head, "--parent", ""); err != nil {
		return err
	}
	slog.Info("initiative.release", "bead", head, "initiative", h.Parent)
	return nil
}

// requireOn reads Require Initiatives from the Workspace root, not the cwd.
func requireOn() bool {
	root, err := git.RepoRoot()
	return err == nil && cfglayer.InitiativesSetting(root).Required
}

// ReleaseRequiredWhy is release's refusal under Require Initiatives (ADR 0224): a
// chain may move between Initiatives but never drop out of one. The TUI's A shows it too.
func ReleaseRequiredWhy(head string) string {
	return "Require Initiatives is on: move it with mugicrew initiative adopt " + head + " <other>"
}

func refuse(verb, id, why string, blocking []Bead) error {
	slog.Warn("initiative."+verb+".refused", "bead", id, "reason", why, "blocking", strings.Join(IDs(blocking), ","))
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
	return filter(subtree(id, kids), func(b Bead) bool { return b.Status != "closed" })
}

func filter(bs []Bead, keep func(Bead) bool) []Bead {
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
