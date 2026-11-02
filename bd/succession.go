package bd

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// Successor describes a bead spawned from another — either a caused-by successor
// (SpawnSuccessorIfAbsent, spec §4.3) or a membership child of a container
// (SpawnChildIfAbsent, the edge differs; ADR 0011). Title/Description seed it; the
// charting session fills the real body (§8.1). Container picks the bd type (epic
// vs task, ADR 0002).
type Successor struct {
	Kind           contract.Kind
	Container      bool
	Title          string
	Description    string
	DraftedBy      string     // creator bead id: non-empty spawns a Draft (ADR 0059 §2) instead of an open bead
	Parent         string     // successor only: the Initiative it is born into (InitiativeOf, Spec D2); "" spawns it unparented
	CausedBy       string     // child only: a predecessor the member also succeeds (a Landing's Approval, ADR 0145 §4); "" adds no caused-by edge
	DiscoveredFrom string     // child only: provenance edge to the bead it was found from (a gate Critique's Landing); "" adds none
	Context        string     // successor container only: the context: label to carry; "" adds none
	Repo           string     // successor container only: the repo:<dir> stamp to copy; "" adds none
	ExternalRef    string     // bd external_ref the bead carries (an unparented create or a successor spawn); "" adds none
	Metadata       string     // bd metadata, a JSON object, set at create; "" adds none (CreateDraft only: a spawn's CreateSpec has no Metadata)
	PrioritySource string     // successor spawn only: the bead whose own priority is stamped at birth in place of the caused-by predecessor (a pivot's obsoleted Checkpoint, ADR 0419); "" stamps the predecessor
	Serializer     Serializer // a spawn's exclusion, copied onto its CreateSpec (ADR 0413); nil is a plain insert
}

// Meta is a bead's bd metadata: custom JSON values by key.
type Meta = map[string]json.RawMessage

// Bead is what a read returns: the routing key (from labels[]) plus the fields
// a reconcile, the dispatch loop, or the TUI needs. status/title/labels drive
// reconcile and the loop (chart:defogged, §8.3); parent/priority/created/
// assignee/container are the TUI's grouping, ordering, and detail fields (§9).
type Bead struct {
	ID             string
	Kind           contract.Kind
	Status         string
	Title          string
	Description    string // the bead body; reconcileChart reads it to gate the Spec-body-authored close (§8.4)
	Labels         []string
	Parent         string    // membership edge: the container this bead is a child of (§9.1 grouping)
	Priority       int       // bd priority 0–4
	Created        time.Time // freshest-first ordering in the Inbox (§9.2)
	Updated        time.Time // bd updated_at: the Search tab's default sort (Spec D1)
	Assignee       string    // the claimer (loop or a human), shown in detail
	Closed         time.Time // bd closed_at: when it was closed; zero while open
	Started        time.Time // bd started_at: when it was claimed — age-since-claim, never liveness (ADR 0025); zero if unclaimed
	Container      bool      // bd epic (a container kind, ADR 0002) → a Graph group head
	BlockedBy      []string  // Ordering edges: depends_on_id of each blocks / conditional-blocks dependency, in edge order
	CondBlockedBy  []string  // the conditional-blocks subset of BlockedBy: the eligibility verdict's blocks-only filter drops these
	Related        []string  // depends_on_id of each related edge, in edge order: a Rollback's target rides here (ADR 0047 §3)
	CausedBy       string    // depends_on_id of the caused-by edge: the predecessor, linking an Initiative's member chains in the Graph (Spec D10)
	DiscoveredFrom []string  // depends_on_id of each discovered-from edge, oldest edge first, ties by target id (bd's array order is a hash order)
	CloseReason    string    // bd close_reason: why a closed bead was closed (bd defaults it to DefaultCloseReason); detail pane only
	CreatedBy      string    // bd created_by: an event bead's actor (ADR 0057 §2)
	ExternalRef    string    // bd external_ref: the outside item this bead mirrors (jira-<KEY>); "" when none
	Metadata       Meta      // bd metadata (a raise's raise_checkpoint); nil when none
	Event          bool      // bd issue_type event: a state-change record, history not work (GLOSSARY.md "Event bead")

	Notes, Design, AcceptanceCriteria string // bd notes, design, acceptance_criteria: free-text fields beside the description
	CommentCount                      int    // bd comment_count: how many comments bd comments would list
}

// FixForwardReason prefixes a Rollback's close reason when it fixed forward
// instead of reverting (ADR 0049 §4): Jinbe writes it, the History badge reads it.
const FixForwardReason = "fix-forward:"

// HandedOffReason is a completed Rollback's close reason, naming the successor
// Checkpoint handOff created (ADR 0399 §1).
func HandedOffReason(successor string) string { return "handed off: " + successor }

// FixForwardCloseReason is a fixed-forward Rollback's close reason, naming its
// successor; it keeps the FixForwardReason prefix the History badge matches.
func FixForwardCloseReason(successor string) string { return FixForwardReason + " " + successor }

// ObsoleteReason prefixes the close reason a confirm sweep gives the members and
// the Checkpoint it closes out (pivot confirmed or escalated).
const ObsoleteReason = "obsolete:"

// The spine close words (ADR 0399): every spine close reason is <outcome>[: <detail>],
// built from these so each literal stays in Go source.
const (
	// DuplicateReason prefixes a duplicate's close reason: duplicate: <original id>.
	DuplicateReason = "duplicate:"
	// SupersededReason prefixes a replaced bead's close reason: superseded: <new id>.
	SupersededReason = "superseded:"
)

// Pivot close reasons (ADR 0414 §5, ADR 0396 §5): every outcome a pivot closes with is
// built here. The outcome word is dismissed, never rejected, which is ADR 0399's spine
// reject.
const (
	// ConfirmedReason closes a pivot a confirm sweep ran for.
	ConfirmedReason = "confirmed"
	// EscalatedReason closes a pivot an escalate sweep ran for.
	EscalatedReason = "escalated"
	// DismissedReason prefixes a dismissed pivot's close reason, and an Ask Critique's.
	DismissedReason = "dismissed:"
	// AmendedReason prefixes an amended pivot's close reason; Luffy's reapproval check matches it.
	AmendedReason = "amended:"
)

// DismissedCloseReason is a dismissed pivot's close reason (dismissed: <gist>), or an Ask
// Critique's (dismissed: <pivot>).
func DismissedCloseReason(detail string) string { return DismissedReason + " " + detail }

// AmendedCloseReason is an amended pivot's close reason: amended: <gist>.
func AmendedCloseReason(gist string) string { return AmendedReason + " " + gist }

// SiblingSupersededReason is a sibling pivot's close reason when the lead pivot's sweep
// retires it: superseded: <lead>.
func SiblingSupersededReason(lead string) string { return SupersededReason + " " + lead }

// ObsoleteBy is a defog bead's close reason when a deciding bead's decision made it
// moot: obsolete: <decider id>. It reuses ObsoleteReason's prefix, but no sweep parser
// matches it (they anchor on "Spec … superseded by …" / "… escalated to …").
func ObsoleteBy(decider string) string { return ObsoleteReason + " " + decider }

// RejectedReason is the one word every Signal reject closes with: bare, or
// followed by ": <why>" (ADR 0399). Luffy's prompt and TUI z both write it.
const RejectedReason = "rejected"

// SweptReason is a confirm or escalate sweep's close reason, parsed back: OldSpec is the swept
// Spec, Successor the corrected Spec (confirm) or the fresh Chart (Escalated), Pivot the
// pivot that drove it.
type SweptReason struct {
	OldSpec, Successor, Pivot string
	Escalated                 bool
}

// ObsoleteSuperseded is the close reason a confirm sweep gives the swept Checkpoint and
// its members.
func ObsoleteSuperseded(oldSpec, newSpec, pivotID string) string {
	return fmt.Sprintf("%s Spec %s superseded by %s (pivot %s confirmed)", ObsoleteReason, oldSpec, newSpec, pivotID)
}

// ObsoleteEscalated is the close reason an escalate sweep gives the swept Checkpoint and
// its members.
func ObsoleteEscalated(oldSpec, newChart, pivotID string) string {
	return fmt.Sprintf("%s Spec %s escalated to Chart %s (pivot %s escalated)", ObsoleteReason, oldSpec, newChart, pivotID)
}

var (
	supersededRe = regexp.MustCompile(`^obsolete: Spec (\S+) superseded by (\S+) \(pivot (\S+) confirmed\)$`)
	escalatedRe  = regexp.MustCompile(`^obsolete: Spec (\S+) escalated to Chart (\S+) \(pivot (\S+) escalated\)$`)
)

// ParseSweptReason reads back the two shapes ObsoleteSuperseded and ObsoleteEscalated
// write; any other reason (container landed, a retired Draft's, free text) is not ok.
func ParseSweptReason(reason string) (SweptReason, bool) {
	reason = strings.TrimSpace(reason)
	if m := supersededRe.FindStringSubmatch(reason); m != nil {
		return SweptReason{OldSpec: m[1], Successor: m[2], Pivot: m[3]}, true
	}
	if m := escalatedRe.FindStringSubmatch(reason); m != nil {
		return SweptReason{OldSpec: m[1], Successor: m[2], Pivot: m[3], Escalated: true}, true
	}
	return SweptReason{}, false
}

// NothingToLandReason prefixes the close reason of a Checkpoint whose Spec laid
// Errands and no Builds: there is nothing to land. It deliberately does not start
// with ObsoleteReason, which drives retireStragglers' cascade and the backstop's
// swept-parent handling.
const NothingToLandReason = "nothing to land:"

// ContainerLandedReason prefixes the close reason of a child whose integrate was
// refused because its container had already landed (ADR 0201). It carries
// ObsoleteReason, so a child so closed reads as swept; DrainBlockersOver skips it
// though its effort/<child> branch is kept.
const ContainerLandedReason = ObsoleteReason + " container landed"

// CarriedByReason is the close reason of a child whose stranded work the Remnant or
// offshoot Signal carrier now carries.
func CarriedByReason(carrier string) string { return ContainerLandedReason + "; carried by " + carrier }

// ApprovedReason is the close reason of an approved Approval. Approved means
// CloseReason equals it exactly (no prefix, no case-fold; empty is not approved),
// so a rework: or obsolete: close never reads as approval (ADR 0203 §5).
const ApprovedReason = "approved"

// ApprovalObsoletedReason is the close reason of an Approval obsoleted because
// its branch advanced past the commit it pins: Usopp's clean-Review tail on a
// Checkpoint, Nami's Chart step on a Chart. It carries ObsoleteReason, so the
// backstop and dispatch read it as swept while it never reads as approved.
const ApprovalObsoletedReason = ObsoleteReason + " branch advanced past the pinned commit"

// ApprovedClosed reports whether the bead is closed with exactly ApprovedReason.
func (b Bead) ApprovedClosed() bool { return !b.Open() && b.CloseReason == ApprovedReason }

// The outcome words of a Signal's success close (ADR 0399), each followed by the
// successor's id. The clean-exit publish writes them from the successor it found.
const (
	ChartedReason    = "charted:"
	SpeccedReason    = "specced:"
	InitiativeReason = "initiative:"
)

// The outcome words of a Survey's completed close, each followed by the parent
// Chart's id: the session seeded fog, finished the stamped Chart in place, or
// closed the Chart. The clean-exit publish writes them.
const (
	SeededReason     = "seeded:"
	DefoggedReason   = "defogged:"
	PushedBackReason = "pushed back:"
)

// The outcome words of a completed defog bead's close (ADR 0399): a grilling's
// answered:, a research's Wrote docs/research/ pointer, a prototype's prototyped:.
// WithdrawnReason is the human's drop of one defog bead. The readers of closed
// defog children count only the first three as decisions.
const (
	AnsweredReason   = "answered:"
	ResearchedReason = "Wrote docs/research/"
	PrototypedReason = "prototyped:"
	WithdrawnReason  = "withdrawn"
)

// LandedReason prefixes the close reason of a landed Landing and of its Checkpoint
// (ADR 0399): "landed: <sha>", the same sha on both. BuiltReason is a finished Build's,
// ResolvedReason a resolved Conflict's and CompletedReason a Critique fixed without a
// Rationale: comment (ADR 0415 (mugicrew-review-conflict-and-critique-completed…)).
const (
	LandedReason    = "landed:"
	BuiltReason     = "built"
	ResolvedReason  = "resolved"
	CompletedReason = "completed"
)

// The outcome words of a Review's completed close (ADR 0415): "clean: <approval id>",
// "critiqued: <critique ids>" and "held: pivot <pivot ids>".
const (
	CleanReason     = "clean:"
	CritiquedReason = "critiqued:"
	HeldReason      = "held: pivot"
)

// SlicedReason is the outcome word of a sliced Spec's close (ADR 0399), followed by
// the Checkpoint's id.
const SlicedReason = "sliced:"

// DefaultCloseReason is the close_reason bd records when `bd close` gets no --reason.
const DefaultCloseReason = "Closed"

// Open reports whether the bead is still actionable — the guard a reconcile
// closes behind. Owns bd's status vocabulary in one place so callers don't
// spell the literals.
// A Draft (status deferred) is open: it is still being set up, so it undrains
// its container (ADR 0059 §3).
func (b Bead) Open() bool {
	return b.Status == "open" || b.Status == "in_progress" || b.Status == "deferred"
}

// Draft reports whether the bead is an unpublished Draft (status deferred).
func (b Bead) Draft() bool { return b.Status == "deferred" }

// Unclosed reports whether the bead is anything but closed (blocked included,
// unlike Open).
func (b Bead) Unclosed() bool { return b.Status != "closed" }

// Titles is each bead's title, in order.
func Titles(bs []Bead) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.Title
	}
	return out
}

// IDs is each bead's id, in order.
func IDs(bs []Bead) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.ID
	}
	return out
}

// HasLabel reports whether the bead carries the given label — the loop's
// chart:defogged completion check (§8.3).
func (b Bead) HasLabel(want string) bool {
	for _, l := range b.Labels {
		if l == want {
			return true
		}
	}
	return false
}

// successorCreateArgs is the pure argv that spawns a successor — split out so the
// §3.1/§4.3 invariants are checkable without a DB: the caused-by edge is fixed
// here in the right direction (new bead caused-by <pred>), --no-inherit-labels
// keeps one kind: label, the bd type is derived from Container (ADR 0002), and
// --parent rides only when the successor joins an Initiative (Spec D2). extraDeps
// ride the same --deps value.
func successorCreateArgs(predID string, s Successor, extraDeps ...string) []string {
	typ := "task"
	if s.Container {
		typ = "epic"
	}
	args := append([]string{"create", "--type", typ}, labelArgs(s)...)
	if s.Parent != "" {
		args = append(args, "--parent", s.Parent)
	}
	if s.Metadata != "" {
		args = append(args, "--metadata", s.Metadata)
	}
	deps := extraDeps
	title := s.Title
	if predID != "" { // "" is a raise Draft: provenance edges only, no successor edge
		deps = append([]string{"caused-by:" + predID}, extraDeps...) // new bead caused-by pred → new is the successor
		title = stripTitle(title, "parent", predID)                  // a copied upstream prefix stops here (ADR 0097)
	}
	return append(append(args, externalRefArgs(s)...),
		"--no-inherit-labels",
		"--deps", strings.Join(deps, ","),
		"--title", title,
		"--description", s.Description,
		"--silent",
	)
}

// externalRefArgs is --external-ref <ref> when s carries one, else nothing.
func externalRefArgs(s Successor) []string {
	if s.ExternalRef == "" {
		return nil
	}
	return []string{"--external-ref", s.ExternalRef}
}

// labelArgs is a create's single kind: label — plus a context: label when set, and, for a Draft, its
// drafted-by:<creator> label in the same --labels value and --status deferred, so
// the Draft is born unpublished in the one bd create (Spec D3).
func labelArgs(s Successor) []string {
	args := []string{"--labels", strings.Join(spawnLabels(s), ",")}
	if s.DraftedBy != "" {
		args = append(args, "--status", "deferred")
	}
	return args
}

// spawnLabels is labelArgs' label set.
func spawnLabels(s Successor) []string {
	labels := []string{"kind:" + string(s.Kind)}
	if s.Context != "" {
		labels = append(labels, contextPrefix+s.Context)
	}
	if s.Repo != "" {
		labels = append(labels, repoPrefix+s.Repo)
	}
	if s.DraftedBy != "" {
		labels = append(labels, contract.DraftedBy(s.DraftedBy))
	}
	return labels
}

// errSpawnMetadata refuses a Spawn…IfAbsent whose Successor sets Metadata: a CreateSpec
// has none, so it would be lost silently. CreateDraft carries it.
var errSpawnMetadata = errors.New("a spawn cannot carry Successor.Metadata; use CreateDraft")

// successorSpec is successorCreateArgs' create as a CreateSpec: the same type, labels, parent,
// deps (caused-by pred first, then extra), title and Draft status, with s.Serializer.
// The two derivations are held together by TestSpecArgsParity.
func successorSpec(predID string, s Successor, extra ...Edge) (CreateSpec, error) {
	if s.Metadata != "" {
		return CreateSpec{}, errSpawnMetadata
	}
	title := s.Title
	var deps []Edge
	if predID != "" {
		deps = append(deps, Edge{Type: "caused-by", To: predID})
		title = stripTitle(title, "parent", predID)
	}
	return CreateSpec{Type: spawnType(s), Title: title, Description: s.Description, Parent: s.Parent,
		Labels: spawnLabels(s), Deps: append(deps, extra...), ExternalRef: s.ExternalRef,
		Deferred: s.DraftedBy != "", NoInherit: true, Serializer: s.Serializer}, nil
}

// childSpec is childCreateArgs' create as a CreateSpec.
func childSpec(parentID string, s Successor) (CreateSpec, error) {
	if s.Metadata != "" {
		return CreateSpec{}, errSpawnMetadata
	}
	var deps []Edge
	if s.CausedBy != "" {
		deps = append(deps, Edge{Type: "caused-by", To: s.CausedBy})
	}
	if s.DiscoveredFrom != "" {
		deps = append(deps, Edge{Type: "discovered-from", To: s.DiscoveredFrom})
	}
	return CreateSpec{Type: spawnType(s), Title: stripTitle(s.Title, "parent", parentID), Description: s.Description,
		Parent: parentID, Labels: spawnLabels(s), Deps: deps, Deferred: s.DraftedBy != "", NoInherit: true,
		Serializer: s.Serializer}, nil
}

// spawnType is the bd type of a spawn: epic for a container, else task (ADR 0002).
func spawnType(s Successor) string {
	if s.Container {
		return "epic"
	}
	return "task"
}

const contextPrefix = "context:"

// repoPrefix is the Repo stamp's label: repo:<dir>, the dir a nested container was cut
// in (ADR 0167).
const repoPrefix = "repo:"

// RepoStampOf is the bead's repo:<dir> stamp, "" when it carries none.
func RepoStampOf(b Bead) string {
	for _, l := range b.Labels {
		if dir, ok := strings.CutPrefix(l, repoPrefix); ok {
			return dir
		}
	}
	return ""
}

// StampRepo adds the repo:<dir> stamp to id.
func StampRepo(id, dir string) error { return core.AddLabels(id, repoPrefix+dir) }

// ContextOf is the bead's context: label value, "" when it carries none.
func ContextOf(b Bead) string {
	for _, l := range b.Labels {
		if name, ok := strings.CutPrefix(l, contextPrefix); ok {
			return name
		}
	}
	return ""
}

// CausedBy walks the caused-by edge DOWNWARD from id to its predecessor of the
// wanted kind — the mirror of SpawnSuccessorIfAbsent's upward scan: `bd dep list
// <id> --direction=down -t caused-by` lists what id depends on (its predecessor),
// read through Core.Edges, the first row of kind <want>. A failed read is an error,
// never "not found". Zoro walks Spec→Chart here to derive the Checkpoint base; a
// headless Spec has no such predecessor and returns ("", false, nil).
func CausedBy(id string, want contract.Kind) (predID string, found bool, err error) {
	return kindAlong(id, Down, "caused-by", want)
}

// RollbackTarget is the Checkpoint a Rollback is related to — the landed
// Checkpoint it undoes (ADR 0047), read the way PivotSpec reads a pivot's Spec.
func RollbackTarget(rollbackID string) (string, bool, error) {
	return kindAlong(rollbackID, Down, "related", contract.KindCheckpoint)
}

// RollbackTargetIn is RollbackTarget over a loaded snapshot, no bd call: the
// first of rb's related beads that is a Checkpoint.
func RollbackTargetIn(rb Bead, kinds map[string]contract.Kind) (string, bool) {
	for _, id := range rb.Related {
		if kinds[id] == contract.KindCheckpoint {
			return id, true
		}
	}
	return "", false
}

// SpecOf is the Spec a Checkpoint serves: its caused-by Spec, or — for a successor
// a Rollback spawned (ADR 0048 §2) or a respin a Remnant seeded — the Spec of the
// Rollback's or Remnant's related target, repeated so a chain still reaches the original. A Checkpoint with
// neither edge returns ("", false, nil).
func SpecOf(checkpointID string) (string, bool, error) {
	return specOf(checkpointID, CausedBy, RollbackTarget)
}

// specOf is SpecOf's walk over injected reads, so it is table-testable without bd.
// seen stops a malformed Rollback cycle as not-found instead of looping forever.
func specOf(id string, causedBy func(string, contract.Kind) (string, bool, error), target func(string) (string, bool, error)) (string, bool, error) {
	for seen := map[string]bool{}; !seen[id]; {
		seen[id] = true
		if spec, ok, err := causedBy(id, contract.KindSpec); err != nil || ok {
			return spec, ok, err
		}
		via, ok, err := causedBy(id, contract.KindRollback)
		if err == nil && !ok {
			via, ok, err = causedBy(id, contract.KindRemnant)
		}
		if err != nil || !ok {
			return "", false, err
		}
		next, ok, err := target(via)
		if err != nil || !ok {
			return "", false, err
		}
		id = next // the rolled-back or landed Checkpoint: repeat from it
	}
	return "", false, nil
}

// SuccessorOf walks the caused-by edge UPWARD from id to its successor of the
// wanted kind — CausedBy's mirror, and SpawnSuccessorIfAbsent's existence check.
// reconcileSignal reads it to pick the Signal's leg (Spec → fast lane, else Chart;
// ADR 0035 §10). No such successor returns ("", false, nil).
func SuccessorOf(id string, want contract.Kind) (succID string, found bool, err error) {
	return kindAlong(id, Up, "caused-by", want)
}

// InitiativeOf is the Initiative a successor of b is born into (Spec D2, FR11):
// b's parent if that parent is an unclosed kind:initiative, else "". Every
// successor spawn sets Successor.Parent through it, so a member chain stays
// under its Initiative; a closed one is never joined (FR32).
func InitiativeOf(b Bead) (string, error) {
	if b.Parent == "" {
		return "", nil
	}
	p, err := Show(b.Parent)
	if err != nil || p.Kind != contract.KindInitiative || !p.Unclosed() {
		return "", err
	}
	return p.ID, nil
}

// SpawnSuccessorIfAbsent is the ONLY sanctioned successor-spawn path (spec §4.3):
// check for an existing caused-by successor of the wanted kind → if present it is
// a no-op (idempotency by construction, safe for the stateless loop and re-run
// triage); else create one with the footgun flags and the fixed edge direction.
func SpawnSuccessorIfAbsent(predID string, s Successor) (id string, created bool, err error) {
	existing, ok, err := SuccessorOf(predID, s.Kind)
	if err != nil {
		return "", false, err
	}
	if ok {
		return existing, false, nil
	}
	spec, err := stampedSpec(predID, s)
	if err != nil {
		return "", false, err
	}
	id, err = core.Create(spec)
	if err != nil {
		return "", false, err
	}
	return id, true, nil
}

// stampedSpec is successorSpec with the birth priority stamped on, the one builder every
// create-path spawn goes through so a new seam cannot forget the stamp (ADR 0419).
func stampedSpec(predID string, s Successor, extra ...Edge) (CreateSpec, error) {
	spec, err := successorSpec(predID, s, extra...)
	if err != nil {
		return spec, err
	}
	spec.Priority, err = stampedPriority(predID, s)
	return spec, err
}

// stampedPriority is the birth priority of a successor (ADR 0419 (mugicrew-succession-stamps-the-predecessor-s-own…)):
// the OWN priority of s.PrioritySource, else of predID, read with a Show so a failed read
// fails the spawn rather than falling back to P2. With neither it is nil: bd's default
// applies. It is read only on the create path, so an existing successor is never rewritten.
func stampedPriority(predID string, s Successor) (*int, error) {
	src := cmp.Or(s.PrioritySource, predID)
	if src == "" {
		return nil, nil
	}
	b, err := Show(src)
	if err != nil {
		return nil, fmt.Errorf("read priority of %s: %w", src, err)
	}
	return &b.Priority, nil
}

// CreateDraft creates a successor of predID unconditionally (SpawnSuccessorIfAbsent
// guards one per kind; a reroute files several pivots and Critiques), with extraDeps
// ("type:target") beside the caused-by edge. The Parent, DraftedBy and Metadata of s
// apply as in a spawn.
func CreateDraft(predID string, s Successor, extraDeps ...string) (string, error) {
	return run(successorCreateArgs(predID, s, extraDeps...)...)
}

// CreateRaiseDraft creates a Draft by creator that is not its successor: no caused-by
// edge, only the given "type:target" deps (an Errand's raise is discovered-from the Errand,
// ADR 0326 §5). The Parent, DraftedBy and Metadata of s apply as in CreateDraft.
func CreateRaiseDraft(s Successor, deps ...string) (string, error) {
	return run(successorCreateArgs("", s, deps...)...)
}

// SetMetadata merges key → value (any JSON value) into id's bd metadata, keeping its
// other keys: bd's --set-metadata stores only strings, and --metadata replaces the object.
func SetMetadata(id, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return updateMetadata(id, func(meta Meta) bool {
		meta[key] = raw
		return true
	})
}

// ClearMetadata removes key from id's bd metadata, keeping its other keys. An
// absent key writes nothing.
func ClearMetadata(id, key string) error {
	return updateMetadata(id, func(meta Meta) bool {
		if _, ok := meta[key]; !ok {
			return false
		}
		delete(meta, key)
		return true
	})
}

// updateMetadata reads id's metadata, lets change edit it, and writes the whole object
// back when change reports it did.
func updateMetadata(id string, change func(Meta) bool) error {
	b, err := Show(id)
	if err != nil {
		return err
	}
	meta := b.Metadata
	if meta == nil {
		meta = Meta{}
	}
	if !change(meta) {
		return nil
	}
	blob, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = run("update", id, "--metadata", string(blob))
	return err
}

// CarrySkip names a dependent a Carry left out and why: closed | hierarchy |
// cycle | type-clash | ended (the successor's chain ended, so pred's dependents were
// released, EndedSkips) | initiative (a Latch onto an Initiative successor, which the
// carry guard produces, ADR 0348 §8). The json tags are the carry event's skipped attr shape.
type CarrySkip struct {
	Bead   string `json:"bead"`
	Reason string `json:"reason"`
}

// Carry adds "X depends on succ" (same type) for every bead X that pred orders
// via blocks/conditional-blocks, so X stays gated past pred's close (ADR 0085;
// Spec D1). Add-only — the old edge stays as lineage — and idempotent: a
// same-type re-add exits 0 and counts as moved. Closed dependents and succ itself
// are skipped; a dep add bd refuses for a known reason (CarrySkipReason) is
// skipped; any other failure returns at once with moved holding what landed.
// It also carries pred's open latch blockers onto succ (carryLatches).
// Event-blind: callers record it (eventlog.RecordCarry).
func Carry(pred, succ string) (moved []string, skipped []CarrySkip, err error) {
	moved, skipped, err = CarryOrdering(pred, succ)
	if err != nil {
		return moved, skipped, err
	}
	return carryLatches(pred, succ, moved, skipped)
}

// CarryTargets is Carry with a target per kind of edge: pred's ordering dependents
// go onto ordering, its open Latches onto latches (ADR 0322 §4: a reroute carries
// the one onto the host, the other onto its caused-by successor). Same rules as Carry.
func CarryTargets(pred, ordering, latches string) (moved []string, skipped []CarrySkip, err error) {
	moved, skipped, err = CarryOrdering(pred, ordering)
	if err != nil {
		return moved, skipped, err
	}
	return carryLatches(pred, latches, moved, skipped)
}

// CarryOrdering is Carry without the Latch step: only pred's blocks and
// conditional-blocks dependents move onto succ.
func CarryOrdering(pred, succ string) (moved []string, skipped []CarrySkip, err error) {
	rows, err := core.Edges(pred, Up, "")
	if err != nil {
		return nil, nil, err
	}
	for _, d := range rows {
		switch {
		case d.Type != "blocks" && d.Type != "conditional-blocks":
			continue
		case !d.Unclosed(): // the read returns every status; deferred Drafts carry
			skipped = append(skipped, CarrySkip{d.ID, "closed"})
		case d.ID == succ:
			skipped = append(skipped, CarrySkip{d.ID, "hierarchy"})
		default:
			if err := core.AddEdge(d.ID, succ, d.Type); err != nil {
				reason := CarrySkipReason(err)
				if reason == "" {
					return moved, skipped, err
				}
				skipped = append(skipped, CarrySkip{d.ID, reason})
				continue
			}
			moved = append(moved, d.ID)
		}
	}
	return moved, skipped, nil
}

// carryLatches also lists pred's incoming blockers and blocks succ with each open
// kind:latch among them (Spec D6; ADR 0223 §3), so a Jira wait outlives pred's
// close. Same add-only, idempotent, skip-classified rules as the outgoing carry.
// An Initiative successor takes none: each open Latch is skipped as initiative,
// since a Latch blocking an Initiative freezes its members (ADR 0348 §8). The
// successor's kind is read once, on the first open Latch; a failed read fails the carry.
func carryLatches(pred, succ string, moved []string, skipped []CarrySkip) ([]string, []CarrySkip, error) {
	rows, err := core.Edges(pred, Down, "blocks")
	if err != nil {
		return moved, skipped, err
	}
	initiative, read := false, false
	for _, d := range rows {
		if !d.Unclosed() || d.Kind != contract.KindLatch {
			continue
		}
		if !read {
			b, err := Show(succ)
			if err != nil {
				return moved, skipped, err
			}
			initiative, read = b.Kind == contract.KindInitiative, true
		}
		if initiative {
			skipped = append(skipped, CarrySkip{d.ID, "initiative"})
			continue
		}
		if err := core.AddEdge(succ, d.ID, "blocks"); err != nil {
			reason := CarrySkipReason(err)
			if reason == "" {
				return moved, skipped, err
			}
			skipped = append(skipped, CarrySkip{d.ID, reason})
			continue
		}
		moved = append(moved, d.ID)
	}
	return moved, skipped, nil
}

// CarrySkipReason classifies a refused carry dep add by bd 1.3.0's error text, or ""
// for an error that is not a known skip. Re-check these on a bd bump.
func CarrySkipReason(err error) string {
	switch msg := err.Error(); {
	case strings.Contains(msg, "cannot be blocked by its ancestor"), strings.Contains(msg, "cannot be blocked by its descendant"),
		strings.Contains(msg, "is already a child of"):
		return "hierarchy" // the first two are DependencyHierarchyConflictError's texts; the dotted-ID text is dep add's CLI pre-check, the same refusal
	case strings.Contains(msg, "would create a cycle"):
		return "cycle" // ErrDependencyCycle
	case strings.Contains(msg, "already exists with type"):
		return "type-clash" // DependencyTypeConflictError: the pair holds a non-blocking edge
	}
	return ""
}

// childCreateArgs is the pure argv that spawns a membership child under a
// container — the parent edge (not caused-by), so the child counts in
// OpenChildren and undrains its container (ADR 0011). Same footguns as a
// successor: --no-inherit-labels keeps the child's single kind: label, bd type
// from Container.
func childCreateArgs(parentID string, s Successor) []string {
	typ := "task"
	if s.Container {
		typ = "epic"
	}
	args := append([]string{"create", "--type", typ}, labelArgs(s)...)
	var deps []string
	if s.CausedBy != "" {
		deps = append(deps, "caused-by:"+s.CausedBy)
	}
	if s.DiscoveredFrom != "" {
		deps = append(deps, "discovered-from:"+s.DiscoveredFrom)
	}
	if len(deps) > 0 {
		args = append(args, "--deps", strings.Join(deps, ","))
	}
	return append(args,
		"--no-inherit-labels",
		"--parent", parentID,
		"--title", stripTitle(s.Title, "parent", parentID),
		"--description", s.Description,
		"--silent",
	)
}

// openChildOfKind scans parsed children for an OPEN one of the wanted kind — the
// pure idempotency guard (as firstOfKind is for deps), so a re-run spawns none.
func openChildOfKind(kids []Bead, want contract.Kind) (string, bool) {
	for _, k := range kids {
		if k.Kind == want && k.Open() {
			return k.ID, true
		}
	}
	return "", false
}

// SpawnChildIfAbsent spawns a membership child of s.Kind under parentID unless an
// OPEN child of that kind already exists (idempotency by construction — the same
// guarantee as SpawnSuccessorIfAbsent, but on the parent edge). Its purpose is
// the open child that undrains a container: reconcileChart files a chart-conflict
// child so the Chart is no longer drained and the loop stops re-dispatching the
// failing land (ADR 0011).
func SpawnChildIfAbsent(parentID string, s Successor) (id string, created bool, err error) {
	kids, err := childBeads(parentID)
	if err != nil {
		return "", false, err
	}
	if existing, ok := openChildOfKind(kids, s.Kind); ok {
		return existing, false, nil
	}
	spec, err := childSpec(parentID, s)
	if err != nil {
		return "", false, err
	}
	id, err = core.Create(spec)
	if err != nil {
		return "", false, err
	}
	return id, true, nil
}

// SpawnBlockingErrandIfAbsent files a Brook-owned HITL Errand that blocks containerID
// (the land hold), unless one of its open blocking Errands already carries key in its
// body: idempotent on (container, key), whatever the existing Errand's parent. It joins the
// Initiative of the container it holds (a Rollback climbs through the Checkpoint it
// undoes) and is born outside, with the reason in its body, when there is none (ADR 0348).
// A non-empty raiser is the Landing whose failure files it: the create's --deps adds
// discovered-from:<raiser> beside blocks:<container>, in the same write (ADR 0401).
func SpawnBlockingErrandIfAbsent(containerID, key, title, body, raiser string) (id string, created bool, err error) {
	open, err := OpenBlockersOfKind(containerID, contract.KindErrand)
	if err != nil {
		return "", false, err
	}
	for _, t := range open {
		if strings.Contains(t.Description, key) {
			return t.ID, false, nil
		}
	}
	parent, line, err := errandPlacement(ErrandFiling{Before: []string{containerID}})
	if err != nil {
		return "", false, err
	}
	// --deps is atomic with the create: no orphan Errand that blocks nothing
	s := Successor{Kind: contract.KindErrand, Parent: parent, Title: stripTitle(title, "kind", string(contract.KindErrand)), Description: withFactLine(body, line)}
	deps := []string{"blocks:" + containerID}
	if raiser != "" {
		deps = append(deps, "discovered-from:"+raiser)
	}
	id, err = run(successorCreateArgs("", s, deps...)...)
	if err != nil {
		return "", false, err
	}
	// a hold that outlives its closed Errand re-files related to it (ADR 0338);
	// the new body and the closed Errand stay untouched
	if prev, ok, err := latestClosedWithKey(containerID, key); err != nil {
		return id, true, err
	} else if ok {
		_, err = run("dep", "add", id, prev, "-t", "related")
		return id, true, err
	}
	return id, true, nil
}

// latestClosedWithKey is the id of the most recently closed Errand blocking
// containerID whose body carries key.
func latestClosedWithKey(containerID, key string) (string, bool, error) {
	targets, err := depTargets(containerID, "down", "blocks")
	if err != nil {
		return "", false, err
	}
	var best Bead
	found := false
	for _, t := range targets {
		if t.Kind != contract.KindErrand || t.Open() || !strings.Contains(t.Description, key) {
			continue
		}
		if !found || t.Closed.After(best.Closed) {
			best, found = t, true
		}
	}
	return best.ID, found, nil
}

// ChildrenOfKind reads a container's children of kind, in ANY status — closed
// and Drafts included (`bd children` lists both).
func ChildrenOfKind(id string, kind contract.Kind) ([]Bead, error) {
	kids, err := childBeads(id)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(kids, func(b Bead) bool { return b.Kind != kind }), nil
}

// ChildCount counts a container's children in any status.
func ChildCount(id string) (int, error) {
	kids, err := childBeads(id)
	return len(kids), err
}

// surveyChild is the one Survey shape (Spec mc-r832 D3): the Chart's charting
// leaf, a Draft by the Chart's own drafter — bd delete does not cascade, so a
// Survey with any other drafted-by would strand on a Chart-only delete or publish.
func surveyChild(title, drafter string) Successor {
	return Successor{Kind: contract.KindSurvey, Title: title, Description: contract.SurveyBody, DraftedBy: drafter, Serializer: UnderClaim(drafter)}
}

// SpawnSurveyIfAbsent seeds chartID's Survey as a Draft unless the Chart has a
// Survey in any status: SpawnChildIfAbsent's open-only guard would spawn a
// second once the first closed. The Chart must already exist (a Draft is fine).
// Both Chart creators (ACCEPT triage, pivot escalate) call it.
func SpawnSurveyIfAbsent(chartID, title, drafter string) (id string, created bool, err error) {
	surveys, err := ChildrenOfKind(chartID, contract.KindSurvey)
	if err != nil {
		return "", false, err
	}
	if len(surveys) > 0 {
		return surveys[0].ID, false, nil
	}
	spec, err := childSpec(chartID, surveyChild(title, drafter))
	if err != nil {
		return "", false, err
	}
	id, err = core.Create(spec)
	if err != nil {
		return "", false, err
	}
	return id, true, nil
}

// SpawnChild creates a membership child of s under parentID, unguarded — for a
// kind the container may already hold (a chart-conflict's grilling Draft beside
// the Chart's own defog grillings); the caller owns idempotency.
func SpawnChild(parentID string, s Successor) (string, error) {
	return run(childCreateArgs(parentID, s)...)
}

// parseBeads maps a `bd --json` array blob (ready/list/children/show all return
// the same object shape) to Beads: kind read from the kind:* label, the rest
// straight through.
func parseBeads(listJSON []byte) ([]Bead, error) {
	beads, _, err := parseBeadRows(listJSON)
	return beads, err
}

// parseBeadRows is parseBeads plus each row's top-level dependency_type, which only
// `bd dep list` rows carry (empty elsewhere), so Edges reads the blob once.
func parseBeadRows(listJSON []byte) ([]Bead, []string, error) {
	var arr []struct {
		ID          string    `json:"id"`
		Title       string    `json:"title"`
		Description string    `json:"description"`
		Status      string    `json:"status"`
		Labels      []string  `json:"labels"`
		Parent      string    `json:"parent"`
		Priority    int       `json:"priority"`
		Created     time.Time `json:"created_at"`
		Updated     time.Time `json:"updated_at"`
		Assignee    string    `json:"assignee"`
		Started     time.Time `json:"started_at"`
		Closed      time.Time `json:"closed_at"`
		IssueType   string    `json:"issue_type"`
		CloseReason string    `json:"close_reason"`
		CreatedBy   string    `json:"created_by"`
		ExternalRef string    `json:"external_ref"`
		Metadata    Meta      `json:"metadata"`
		Notes       string    `json:"notes"`
		Design      string    `json:"design"`
		Acceptance  string    `json:"acceptance_criteria"`
		Comments    int       `json:"comment_count"`
		DepType     string    `json:"dependency_type"`
		Deps        []struct {
			DependsOn string    `json:"depends_on_id"`
			Type      string    `json:"type"`
			Created   time.Time `json:"created_at"` // the edge's, in bd list rows only
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(listJSON, &arr); err != nil {
		return nil, nil, fmt.Errorf("bd: parse json: %w", err)
	}
	beads := make([]Bead, len(arr))
	types := make([]string, len(arr))
	for i, b := range arr {
		types[i] = b.DepType
		beads[i] = Bead{
			ID: b.ID, Kind: kindFromLabels(b.Labels), Status: b.Status, Title: b.Title, Description: b.Description, Labels: b.Labels,
			Parent: b.Parent, Priority: b.Priority, Created: b.Created, Updated: b.Updated, Assignee: b.Assignee, Started: b.Started, Closed: b.Closed,
			CloseReason: b.CloseReason, CreatedBy: b.CreatedBy, ExternalRef: b.ExternalRef, Metadata: b.Metadata,
			Container: b.IssueType == "epic", // container kind = bd epic (ADR 0002)
			Event:     b.IssueType == "event",
			Notes:     b.Notes, Design: b.Design, AcceptanceCriteria: b.Acceptance, CommentCount: b.Comments,
		}
		var df []dfEdge
		for _, d := range b.Deps {
			if d.Type == "discovered-from" {
				df = append(df, dfEdge{d.DependsOn, d.Created})
			}
			if d.Type == "blocks" || d.Type == "conditional-blocks" { // Ordering edges; related and caused-by kept apart below, membership ignored
				beads[i].BlockedBy = append(beads[i].BlockedBy, d.DependsOn)
				if d.Type == "conditional-blocks" {
					beads[i].CondBlockedBy = append(beads[i].CondBlockedBy, d.DependsOn)
				}
			}
			if d.Type == "related" {
				beads[i].Related = append(beads[i].Related, d.DependsOn)
			}
			if d.Type == "caused-by" {
				beads[i].CausedBy = d.DependsOn
			}
		}
		slices.SortFunc(df, func(x, y dfEdge) int {
			return cmp.Or(x.at.Compare(y.at), cmp.Compare(x.id, y.id))
		})
		for _, d := range df {
			beads[i].DiscoveredFrom = append(beads[i].DiscoveredFrom, d.id)
		}
	}
	return beads, types, nil
}

// dfEdge is a discovered-from edge: its target and the edge's created_at (Spec D1).
type dfEdge struct {
	id string
	at time.Time
}

// parseShow maps a `bd show <id> --json` blob (a one-element array) to a Bead.
func parseShow(showJSON []byte) (Bead, error) {
	beads, err := parseBeads(showJSON)
	if err != nil {
		return Bead{}, err
	}
	if len(beads) == 0 {
		return Bead{}, fmt.Errorf("bd show: no bead in result")
	}
	return beads[0], nil
}

// kindFromLabels pulls the single kind:* routing label (ADR 0002). Empty if the
// bead carries none (a non-mugicrew planning bead).
func kindFromLabels(labels []string) contract.Kind {
	for _, l := range labels {
		if len(l) > 5 && l[:5] == "kind:" {
			return contract.Kind(l[5:])
		}
	}
	return ""
}

// OpenBlocked reports whether any bead that id blocks is still unclosed; a
// deferred Draft counts (it is still being set up, as Open does).
func OpenBlocked(id string) (bool, error) {
	deps, err := core.Edges(id, Up, "")
	if err != nil {
		return false, err
	}
	for _, d := range deps {
		if (d.Type == "blocks" || d.Type == "conditional-blocks") && d.Status != "closed" {
			return true, nil
		}
	}
	return false, nil
}

// IsNotFound reports whether err is bd's refusal of a missing bead: show says "not found",
// update and unclaim say "no issue found matching".
func IsNotFound(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no issue found matching"))
}

// Show reads a bead (spec §3.1 reads use --json).
func Show(id string) (Bead, error) { return core.Show(id) }

// Close closes a bead, optionally with a reason (the reject path, §7.3).
func Close(id, reason string) error { return core.Close([]string{id}, reason, false) }

// AddBlocker makes blocked wait on blocker through a plain blocks edge; bd refuses a cycle.
func AddBlocker(blocked, blocker string) error {
	_, err := run("dep", "add", blocked, blocker, "-t", "blocks")
	return err
}

// RemoveBlocker drops the blocks edge that makes blocked wait on blocker; it never touches blocker.
func RemoveBlocker(blocked, blocker string) error {
	_, err := run("dep", "remove", blocked, blocker)
	return err
}

// Ready lists the open, unblocked frontier (`bd ready`, spec §6.1). The loop
// fences these to the registry itself; this returns them all with their kind
// and labels so the fence stays in Go (registry is the source of truth).
func Ready() ([]Bead, error) { return core.Ready() }

// ReadyAll is Ready without bd's default cap (`bd ready -n 0`): the exact frontier, which
// the TUI's row hint widens itself rather than deriving it from its snapshot.
func ReadyAll() ([]Bead, error) {
	out, err := run("ready", "--json", "-n", "0")
	if err != nil {
		return nil, err
	}
	return parseBeads([]byte(out))
}

// OpenList lists every Open bead with its parent and blocks edges — the bead
// list the loop's frontier re-admit reads (Spec D6), since bd ready hides
// a blocked Checkpoint and its members both.
func OpenList() ([]Bead, error) {
	return core.List(Query{Status: []string{"open", "in_progress", "deferred"}})
}

// InProgress lists every in_progress bead — the loop's busy-count source, from
// which the AFK pool's inflight is re-derived each tick (spec §6.2/§6.4, ADR 0003
// stateless).
func InProgress() ([]Bead, error) {
	return core.List(Query{Status: []string{"in_progress"}, Limit: defaultListCap})
}

// childBeads reads a container's children (`bd children`, includes closed) — the
// one shell-out both the drained-Chart count and the open-child idempotency guard
// share (ADR 0011), split out so neither re-spells the read.
func childBeads(id string) ([]Bead, error) { return core.List(Query{Parent: id, All: true}) }

// OpenChildren counts a container's still-open children — the drained-Chart test
// (§8.4: 0 open children AND chart:defogged). `bd children` includes closed
// children, so the open filter is applied here. A closed child still holding
// effort/<child> not yet integrated counts as open (Spec D7, amended by ADR 0394
// (a-closed-effort-whose-tip-is-its-cut-ref…)): its work is not on the container.
func OpenChildren(id string, efforts *git.EffortSet) (int, error) {
	open, held, err := DrainBlockers(id, efforts)
	return open + len(held), err
}

// effortRefs is the effort-branch read the drain gates fall back to when the caller
// hands no set, a var so a test runs without a live repo.
var effortRefs = EffortRefsAcross

// EffortRefsAcross is the effort-ref set of the cwd's repo plus every declared repo =
// dir's (Spec D8): a nested leaf's closed-but-unintegrated effort lives in its child.
// With no repo declared it is today's single for-each-ref, after one config read.
// The loop resolves it once per tick and hands it to the drain gates.
func EffortRefsAcross() (*git.EffortSet, error) {
	refs, _, err := RefsAcross()
	return refs, err
}

// RefsAcross is EffortRefsAcross plus the checkpoint/<id> branch tips (id → sha),
// each Checkpoint's from the repo that declares it: the TUI's one ref read per refresh.
func RefsAcross() (*git.EffortSet, map[string]string, error) {
	refs, tips, err := git.Repo("").BranchRefs()
	repos := cfglayer.Repos(".")
	if err != nil || len(repos) == 0 {
		return refs, tips, err
	}
	root, err := git.RepoRoot()
	if err != nil {
		return nil, nil, err
	}
	for _, dir := range repos {
		p := filepath.Join(root, dir)
		if !git.IsRepoRoot(p) { // a declared child not checked out holds no efforts
			continue
		}
		kidRefs, kidTips, err := git.Repo(p).BranchRefs()
		if err != nil {
			return nil, nil, err
		}
		refs.Merge(kidRefs)
		maps.Copy(tips, kidTips)
	}
	return refs, tips, nil
}

// childrenAndEfforts is the drain gates' shared read: id's children and the
// effort-ref set the caller passed (nil reads EffortRefsAcross itself).
func childrenAndEfforts(id string, efforts *git.EffortSet) ([]Bead, *git.EffortSet, error) {
	kids, err := childBeads(id)
	if err != nil {
		return nil, nil, err
	}
	if efforts == nil {
		efforts, err = effortRefs()
	}
	return kids, efforts, err
}

// DrainBlockers is what keeps a container from draining (Spec D7): its
// open children, and held, its closed children still holding unintegrated
// work on effort/<child> (ADR 0394 (a-closed-effort-whose-tip-is-its-cut-ref…): an
// effort at its cut ref holds none).
// efforts is the caller's effort-ref set (the loop's per-tick union); nil reads it
// once here. Never one git call per child.
func DrainBlockers(id string, efforts *git.EffortSet) (open int, held []Bead, err error) {
	kids, efforts, err := childrenAndEfforts(id, efforts)
	if err != nil {
		return 0, nil, err
	}
	open, held = DrainBlockersOver(kids, efforts)
	return open, held, nil
}

// DrainBlockersOver is DrainBlockers over an already-read graph, as the TUI
// does at refresh: kids' open count, and the closed ones still holding an unintegrated effort branch
// (the git.EffortSet). An open Landing is skipped: a Chart's own Landing
// never undrains it (ADR 0145), nor does a child closed ContainerLandedReason
// (ADR 0201): its kept branch is carried by a Signal. An open MR Watch is skipped too.
func DrainBlockersOver(kids []Bead, efforts *git.EffortSet) (open int, held []Bead) {
	for _, k := range kids {
		switch {
		case k.Open() && k.Kind.PacedLeaf():
		case k.Open():
			open++
		case efforts.Held(k.ID) && !strings.HasPrefix(k.CloseReason, ContainerLandedReason):
			held = append(held, k)
		}
	}
	return open, held
}

// ReviewDue reports whether a Checkpoint wants its bare Review (Spec D8,
// FRs 28–31): its members are drained (MembersDrainedOver), at least one of them
// is closed (a Checkpoint whose members aren't spawned yet isn't reviewed empty),
// no pivot is open, and no Approval that is open or closed approved pins the tip
// (approvedAtHead, git.ApprovedAtHead's shape, asked of checkpoint/<id>). A
// stale or rework/obsolete-closed Approval doesn't count, so a tip moved past it
// gets a fresh Review. One children read, one effort-ref read and one pivot read.
func ReviewDue(checkpointID string, efforts *git.EffortSet, approvedAtHead func(approvalBody, branch string) bool) (bool, error) {
	kids, efforts, err := childrenAndEfforts(checkpointID, efforts)
	if err != nil {
		return false, err
	}
	pivots, err := OpenPivots(checkpointID)
	if err != nil {
		return false, err
	}
	branch := git.BranchName(string(contract.KindCheckpoint), checkpointID)
	atHead := func(body string) bool { return approvedAtHead(body, branch) }
	return ReviewDueOver(kids, efforts, pivots, atHead), nil
}

// ReviewDueOver is ReviewDue over an already-read graph: the Checkpoint's
// children, the git.EffortSet and its open pivots.
func ReviewDueOver(kids []Bead, efforts *git.EffortSet, pivots []Bead, atHead func(approvalBody string) bool) bool {
	if len(pivots) > 0 || !MembersDrainedOver(kids, efforts) {
		return false
	}
	members := 0
	for _, k := range kids {
		switch {
		case IsMember(k.Kind):
			members++
		case k.Kind == contract.KindApproval && (k.Open() || k.ApprovedClosed()) && atHead(k.Description):
			return false
		}
	}
	return members > 0
}

// MembersDrained reports whether a Checkpoint's members are drained (Spec
// D7, FR 28): no Build, Critique, Conflict, Surgery or Review open or
// still holding unintegrated work on effort/<id> (ADR 0394). Both the Review spawn and the approved land gate on
// it, so neither runs without a member's work.
func MembersDrained(checkpointID string, efforts *git.EffortSet) (bool, error) {
	kids, efforts, err := childrenAndEfforts(checkpointID, efforts)
	if err != nil {
		return false, err
	}
	return MembersDrainedOver(kids, efforts), nil
}

// MembersDrainedOver is MembersDrained over an already-read graph.
func MembersDrainedOver(kids []Bead, efforts *git.EffortSet) bool {
	for _, k := range kids {
		if IsMember(k.Kind) && (k.Open() || efforts.Held(k.ID)) {
			return false
		}
	}
	return true
}

// IsMember is whether a Checkpoint child of kind k carries work onto its branch.
func IsMember(k contract.Kind) bool {
	switch k {
	case contract.KindBuild, contract.KindCritique, contract.KindConflict, contract.KindSurgery, contract.KindReview, contract.KindForgeReview:
		return true
	}
	return false
}

// HeldHint is a drain-gated press's refusal for closed children still holding
// effort branches (Spec FR 46): it names each, and both ways out.
func HeldHint(held []Bead) string {
	branches := make([]string, len(held))
	for i, b := range held {
		branches[i] = git.EffortBranch(b.ID)
	}
	return fmt.Sprintf("%s closed with unintegrated work on %s — run 'mugicrew integrate' in a tree on the branch, or 'git branch -D %s' to discard it",
		strings.Join(IDs(held), ", "), strings.Join(branches, ", "), strings.Join(branches, " "))
}

// Claim sets a bead in_progress atomically (spec §6.5: claim synchronously,
// before the goroutine dispatch, so the next tick's busy-count is truthful and
// re-dispatch is prevented). It is a status CAS (ADR 0052), not `--claim`: the
// loop and run-now share one bd actor, so an idempotent claim let both win. Only
// an open bead moves; any other status — even our own claim — fails with nothing
// written, and callers skip and record claim.failed. No lease is stamped.
func Claim(id string) error { return core.Claim(id, "open") }

// ErrClaimedSince is Claim's refusal when this actor already holds the bead (bd
// exits 13, a status CAS mismatch): the caller's snapshot was stale.
var ErrClaimedSince = errors.New("claimed since the last refresh")

// HeldError is Claim's refusal when another actor holds the bead (bd exits 1,
// "cannot reassign"). Err is bd's own error.
type HeldError struct {
	Holder string
	Err    error
}

func (e *HeldError) Error() string { return e.Err.Error() }
func (e *HeldError) Unwrap() error { return e.Err }

var heldBy = regexp.MustCompile(`cannot reassign \S+: held by "([^"]*)"`)

// classifyClaim turns bd's two claim refusals into *HeldError and ErrClaimedSince
// (texts pinned in docs/research/initiative-open-status-checks-under-a-cut-session-claim.md);
// any other error passes through unchanged.
func classifyClaim(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if m := heldBy.FindStringSubmatch(msg); m != nil {
		return &HeldError{Holder: m[1], Err: err}
	}
	if strings.Contains(msg, "status mismatch:") && strings.Contains(msg, `has status "in_progress", expected "open"`) {
		return fmt.Errorf("%w: %w", ErrClaimedSince, err)
	}
	return err
}

// resolved is the bd actor read from bd once per process, so the CAS assignee is
// what a human's plain `bd unclaim` sees as "you" (ADR 0376 §3, amending ADR 0052
// §2). A var so a test can pin it without racing the cache. A degraded read (bd
// down) is cached for the process lifetime on purpose: one actor per process, so
// a claim made under the bare name never turns foreign when bd comes back, and a
// render or ClaimedElsewhere check never re-runs bd in an outage. Restart after
// fixing bd.
var resolved = cachedActor()

func cachedActor() func() (string, bool) { return sync.OnceValues(readActor) }

// actor is the accessor Claim and ClaimedElsewhere share; a var so a test in this
// package can pin it.
var actor = func() string { name, _ := resolved(); return name }

// Actor is the bd actor this process claims as, and whether bd could not be read
// so it is the bare-name fallback (a degraded read). The doctor rows read this
// same value, so they report what the claim CAS writes.
func Actor() (name string, degraded bool) { return resolved() }

// readActor asks bd for its effective actor. bd 1.3.0 has no whoami,
// `bd context --json` omits the actor, and `bd config get actor` reports only the
// config.yaml step and ignores $BEADS_ACTOR. The narrowest surface that reports
// the full effective actor ($BEADS_ACTOR, config.yaml actor:, git user.name,
// $USER) is `bd create --dry-run --json`, whose created_by is that actor; it
// writes nothing but needs a resolvable Bead Graph (BEADS_DIR or a .beads
// above the cwd); outside one bd errors "no beads database found" and the read
// degrades (verified in a scratch graph and HOME with actor: me@box in
// config.yaml; $BEADS_ACTOR=zed@dev overrides it). If bd can't be read the bare
// name falls back, so a degraded bd never blocks a claim.
// The probe is --type chore, which has no required description sections: a
// Workspace with validation.on-create=error rejects the dry-run of a task
// ("missing required sections"), which would silently degrade to the bare name.
func readActor() (name string, degraded bool) {
	out, err := run("create", "--dry-run", "--json", "--title", "actor probe", "--type", "chore")
	if err == nil {
		var probe struct {
			CreatedBy string `json:"created_by"`
		}
		if json.Unmarshal([]byte(out), &probe) == nil && probe.CreatedBy != "" {
			return probe.CreatedBy, false
		}
	}
	return bareActor(), true
}

// bareActor is the degraded-read fallback only, never a substitute for asking bd:
// the bare name an unreadable bd would most likely have given.
func bareActor() string {
	if env := os.Getenv("BEADS_ACTOR"); env != "" {
		return env
	}
	if name, _ := git.UserName(); name != "" {
		return name
	}
	return os.Getenv("USER")
}

// PinActor makes this process's bd actor name, for a test outside the package; it
// returns the restore.
func PinActor(name string) (restore func()) {
	old := resolved
	resolved = func() (string, bool) { return name, false }
	return func() { resolved = old }
}

// ClaimedElsewhere is whether assignee names another bd actor than this one; an
// empty assignee is nobody's, so not elsewhere (ADR 0240).
func ClaimedElsewhere(assignee string) bool { return assignee != "" && assignee != actor() }

// Unclaim releases a claim: it clears the assignee and resets status to open, the
// inverse of Claim. A non-terminal handler (one that neither closes nor promotes
// the bead to a further status — the drained-Chart promote-or-stamp branch) calls
// it so the released bead re-enters `bd ready` and the next tick re-dispatches it;
// without it the bead sits in_progress, which `bd ready` excludes, forever.
//
// The CAS guard stays in here (ADR 0380): bd unclaim --if-assignee this actor, so a release
// never clears a claim someone else took in the window.
func Unclaim(id string) error { return core.Unclaim(id, Mine()) }

// LandingsOf reads a container's Landings, in any status.
func LandingsOf(id string) ([]Bead, error) { return ChildrenOfKind(id, contract.KindLanding) }

// NewestLanding is the Landing a stale-pin flag names: an open one, else the most
// recently closed (an obsolete Landing whose MR a human merged later, Spec FR 20).
func NewestLanding(landings []Bead) (Bead, bool) {
	var best Bead
	found := false
	for _, l := range landings {
		if !found || (l.Open() && !best.Open()) || (l.Open() == best.Open() && l.Closed.After(best.Closed)) {
			best, found = l, true
		}
	}
	return best, found
}

// CloseLandings closes each still-open Landing landed: <sha> through closer, behind the
// Open() guard so a stale or re-run read never double-closes.
func CloseLandings(landings []Bead, sha string, closer func(id, reason string) error) error {
	for _, l := range landings {
		if l.Open() {
			if err := closer(l.ID, LandedReason+" "+sha); err != nil {
				return err
			}
		}
	}
	return nil
}

// SubtreeIDs is every descendant id of a container, closed ones included, children
// before their own children.
// ponytail: one bd call per node, fine at container sizes; batch the child query if trees grow large.
func SubtreeIDs(id string) ([]string, error) {
	kids, err := childBeads(id)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, k := range kids {
		sub, err := SubtreeIDs(k.ID)
		if err != nil {
			return nil, err
		}
		out = append(append(out, k.ID), sub...)
	}
	return out, nil
}

// AwaitingUndraft reports whether the bead is an Approval waiting on the human's un-draft
// of its GitLab Draft MR (Spec FR 9): open, kind approval and its body records an MR:
// line, whatever its parent's kind. The MR: line is the one fact; no label mirrors it.
func AwaitingUndraft(b Bead) bool {
	return b.Kind == contract.KindApproval && b.Open() && git.RecordedMR(b.Description) > 0
}

// LiveHead finds where a bead's work lives now: id itself while it is unclosed, else
// its unclosed caused-by successor (any kind), else the bead it is superseded by
// (a duplicate's or a pivot Retire's supersedes edge), else its closed successors,
// backtracking from a dead end until one is unclosed.
// A chain that ended (landed, rejected, withdrawn) or cycles returns ("", false, nil);
// a failed read is an error, never "not found". mugicrew duplicate carries onto it.
func LiveHead(id string) (head string, found bool, err error) {
	return liveHead(id,
		func(id string) (Bead, error) { return Show(id) },
		func(id string) ([]Bead, error) { return depTargets(id, string(Up), "caused-by") },
		func(id string) ([]Bead, error) { return depTargets(id, string(Down), "supersedes") })
}

// liveHead is LiveHead's walk over injected reads, so it is table-testable without bd.
// It is a depth-first search that backtracks: at a closed bead it tries an unclosed
// caused-by successor, then the supersedes target, then the closed successors, so one
// dead-end branch (a pivot-superseded Spec's obsolete Checkpoint) does not end the
// chain. seen is shared across branches and ends a cycle as not found.
func liveHead(id string, show func(string) (Bead, error), successors, supersededBy func(string) ([]Bead, error)) (string, bool, error) {
	seen := map[string]bool{}
	var walk func(id string) (string, bool, error)
	walk = func(id string) (string, bool, error) {
		if seen[id] {
			return "", false, nil
		}
		seen[id] = true
		b, err := show(id)
		if err != nil {
			return "", false, err
		}
		if b.Unclosed() {
			return id, true, nil
		}
		next, err := successors(id)
		if err != nil {
			return "", false, err
		}
		sup, err := supersededBy(id)
		if err != nil {
			return "", false, err
		}
		var order []string
		for _, n := range next {
			if n.Unclosed() {
				order = append(order, n.ID)
			}
		}
		for _, n := range sup {
			order = append(order, n.ID)
		}
		for _, n := range next {
			if !n.Unclosed() {
				order = append(order, n.ID)
			}
		}
		for _, n := range order {
			if head, ok, err := walk(n); err != nil || ok {
				return head, ok, err
			}
		}
		return "", false, nil
	}
	return walk(id)
}

// EndedSkips lists pred's open blocks and conditional-blocks dependents as skipped
// ended: what a Carry would have moved had the target chain not ended, so the release
// is recorded and not silent. Read-only.
func EndedSkips(pred string) ([]CarrySkip, error) {
	rows, err := core.Edges(pred, Up, "")
	if err != nil {
		return nil, err
	}
	var skipped []CarrySkip
	for _, d := range rows {
		if (d.Type == "blocks" || d.Type == "conditional-blocks") && d.Unclosed() {
			skipped = append(skipped, CarrySkip{d.ID, "ended"})
		}
	}
	return skipped, nil
}
