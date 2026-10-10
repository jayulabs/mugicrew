package bd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// Successor describes a bead spawned from another — either a caused-by successor
// (SpawnSuccessorIfAbsent, spec §4.3) or a membership child of a container
// (SpawnChildIfAbsent, the edge differs; ADR 0011). Title/Description seed it; the
// charting session fills the real body (§8.1). Container picks the bd type (epic
// vs task, ADR 0002).
type Successor struct {
	Kind        contract.Kind
	Container   bool
	Title       string
	Description string
	DraftedBy   string // creator bead id: non-empty spawns a Draft (ADR 0059 §2) instead of an open bead
}

// Bead is what a read returns: the routing key (from labels[]) plus the fields
// a reconcile, the dispatch loop, or the TUI needs. status/title/labels drive
// reconcile and the loop (chart:defogged, §8.3); parent/priority/created/
// assignee/container are the TUI's grouping, ordering, and detail fields (§9).
type Bead struct {
	ID          string
	Kind        contract.Kind
	Status      string
	Title       string
	Description string // the bead body; reconcileChart reads it to gate the Spec-body-authored close (§8.4)
	Labels      []string
	Parent      string    // membership edge: the container this bead is a child of (§9.1 grouping)
	Priority    int       // bd priority 0–4
	Created     time.Time // freshest-first ordering in the Inbox (§9.2)
	Updated     time.Time // bd updated_at: the Search tab's default sort (Spec D1)
	Assignee    string    // the claimer (loop or a human), shown in detail
	Started     time.Time // bd started_at: when it was claimed — age-since-claim, never liveness (ADR 0025); zero if unclaimed
	Container   bool      // bd epic (a container kind, ADR 0002) → a Graph group head
	BlockedBy   []string  // Ordering edges: depends_on_id of each blocks / conditional-blocks dependency, in edge order
	Related     []string  // depends_on_id of each related edge, in edge order: a Rollback's target rides here (ADR 0047 §3)
	CloseReason string    // bd close_reason: why a closed bead was closed (bd defaults it to DefaultCloseReason); detail pane only
	CreatedBy   string    // bd created_by: an event bead's actor (ADR 0057 §2)
	Event       bool      // bd issue_type event: a state-change record, history not work (GLOSSARY.md "Event bead")
}

// FixForwardReason prefixes a Rollback's close reason when it fixed forward
// instead of reverting (ADR 0049 §4): Jinbe writes it, the History badge reads it.
const FixForwardReason = "fix-forward:"

// ObsoleteReason prefixes the close reason a confirm sweep gives the members and
// the Checkpoint it closes out (pivot confirmed or escalated).
const ObsoleteReason = "obsolete:"

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
// keeps one kind: label, and the bd type is derived from Container (ADR 0002).
// extraDeps ride the same --deps value (discoveredCreateArgs' re-run key).
func successorCreateArgs(predID string, s Successor, extraDeps ...string) []string {
	typ := "task"
	if s.Container {
		typ = "epic"
	}
	args := append([]string{"create", "--type", typ}, labelArgs(s)...)
	return append(args,
		"--no-inherit-labels",
		"--deps", strings.Join(append([]string{"caused-by:" + predID}, extraDeps...), ","), // new bead caused-by pred → new is the successor
		"--title", stripTitle(s.Title, "parent", predID), // a copied upstream prefix stops here (ADR 0097)
		"--description", s.Description,
		"--silent",
	)
}

// labelArgs is a create's single kind: label — plus, for a Draft, its
// drafted-by:<creator> label in the same --labels value and --status deferred, so
// the Draft is born unpublished in the one bd create (Spec D3).
func labelArgs(s Successor) []string {
	if s.DraftedBy == "" {
		return []string{"--labels", "kind:" + string(s.Kind)}
	}
	return []string{"--labels", "kind:" + string(s.Kind) + "," + contract.DraftedBy(s.DraftedBy), "--status", "deferred"}
}

// findKindInDeps scans a `bd dep list … -t caused-by --json` result for the first
// bead carrying kind:<want>. Direction is the caller's: SpawnSuccessorIfAbsent
// passes the --direction=up list to find a successor (so a re-run spawns none);
// CausedBy passes the --direction=down list to find a predecessor. The parse is
// direction-agnostic — it just picks the kind — so both roles share it.
func findKindInDeps(depListJSON []byte, want contract.Kind) (string, bool) {
	var deps []struct {
		ID     string   `json:"id"`
		Labels []string `json:"labels"`
	}
	if err := json.Unmarshal(depListJSON, &deps); err != nil {
		return "", false
	}
	label := "kind:" + string(want)
	for _, d := range deps {
		for _, l := range d.Labels {
			if l == label {
				return d.ID, true
			}
		}
	}
	return "", false
}

// CausedBy walks the caused-by edge DOWNWARD from id to its predecessor of the
// wanted kind — the mirror of SpawnSuccessorIfAbsent's upward scan: `bd dep list
// <id> --direction=down -t caused-by` lists what id depends on (its predecessor),
// reusing findKindInDeps to pick the one carrying kind:<want>. Zoro walks
// Spec→Chart here to derive the Checkpoint base; a headless Spec has
// no such predecessor and returns ("", false, nil).
func CausedBy(id string, want contract.Kind) (predID string, found bool, err error) {
	out, err := run("dep", "list", id, "--direction=down", "-t", "caused-by", "--json")
	if err != nil {
		return "", false, err
	}
	pred, ok := findKindInDeps([]byte(out), want)
	return pred, ok, nil
}

// RollbackTarget is the Checkpoint a Rollback is related to — the landed
// Checkpoint it undoes (ADR 0047), read the way PivotSpec reads a pivot's Spec.
func RollbackTarget(rollbackID string) (string, bool, error) {
	return kindAlong(rollbackID, "down", "related", contract.KindCheckpoint)
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
// a Rollback spawned (ADR 0048 §2) — the Spec of the Rollback's related target,
// repeated so a chain of Rollbacks still reaches the original. A Checkpoint with
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
		rb, ok, err := causedBy(id, contract.KindRollback)
		if err != nil || !ok {
			return "", false, err
		}
		next, ok, err := target(rb)
		if err != nil || !ok {
			return "", false, err
		}
		id = next // the rolled-back Checkpoint: repeat from it
	}
	return "", false, nil
}

// SuccessorOf walks the caused-by edge UPWARD from id to its successor of the
// wanted kind — CausedBy's mirror, and SpawnSuccessorIfAbsent's existence check.
// reconcileSignal reads it to pick the Signal's leg (Spec → fast lane, else Chart;
// ADR 0035 §10). No such successor returns ("", false, nil).
func SuccessorOf(id string, want contract.Kind) (succID string, found bool, err error) {
	out, err := run("dep", "list", id, "--direction=up", "-t", "caused-by", "--json")
	if err != nil {
		return "", false, err
	}
	succ, ok := findKindInDeps([]byte(out), want)
	return succ, ok, nil
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
	id, err = run(successorCreateArgs(predID, s)...)
	if err != nil {
		return "", false, err
	}
	return id, true, nil
}

// CarrySkip names a dependent a Carry left out and why: closed | hierarchy |
// cycle | type-clash. The json tags are the carry event's skipped attr shape.
type CarrySkip struct {
	Bead   string `json:"bead"`
	Reason string `json:"reason"`
}

// Carry adds "X depends on succ" (same type) for every bead X that pred orders
// via blocks/conditional-blocks, so X stays gated past pred's close (ADR 0085;
// Spec D1). Add-only — the old edge stays as lineage — and idempotent: a
// same-type re-add exits 0 and counts as moved. Closed dependents and succ itself
// are skipped; a dep add bd refuses for a known reason (carrySkipReason) is
// skipped; any other failure returns at once with moved holding what landed.
// Event-blind: callers record it (eventlog.RecordCarry).
func Carry(pred, succ string) (moved []string, skipped []CarrySkip, err error) {
	out, err := run("dep", "list", pred, "--direction=up", "--json")
	if err != nil {
		return nil, nil, err
	}
	var deps []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Type   string `json:"dependency_type"`
	}
	if err := json.Unmarshal([]byte(out), &deps); err != nil {
		return nil, nil, fmt.Errorf("bd: parse dep list: %w", err)
	}
	for _, d := range deps {
		switch {
		case d.Type != "blocks" && d.Type != "conditional-blocks":
			continue
		case d.Status == "closed": // dep list returns every status; deferred Drafts carry
			skipped = append(skipped, CarrySkip{d.ID, "closed"})
		case d.ID == succ:
			skipped = append(skipped, CarrySkip{d.ID, "hierarchy"})
		default:
			if _, err := run("dep", "add", d.ID, succ, "-t", d.Type); err != nil {
				reason := carrySkipReason(err)
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

// carrySkipReason classifies a refused carry dep add by bd 1.3.0's error text,
// or ""
// for an error that is not a known skip. Re-check these on a bd bump.
func carrySkipReason(err error) string {
	switch msg := err.Error(); {
	case strings.Contains(msg, "cannot be blocked by its ancestor"), strings.Contains(msg, "cannot be blocked by its descendant"):
		return "hierarchy" // DependencyHierarchyConflictError
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
	return append(args,
		"--no-inherit-labels",
		"--parent", parentID,
		"--title", stripTitle(s.Title, "parent", parentID),
		"--description", s.Description,
		"--silent",
	)
}

// openChildOfKind scans parsed children for an OPEN one of the wanted kind — the
// pure idempotency guard (mirrors findKindInDeps), so a re-run spawns none.
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
	id, err = SpawnChild(parentID, s)
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
		IssueType   string    `json:"issue_type"`
		CloseReason string    `json:"close_reason"`
		CreatedBy   string    `json:"created_by"`
		Deps        []struct {
			DependsOn string `json:"depends_on_id"`
			Type      string `json:"type"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(listJSON, &arr); err != nil {
		return nil, fmt.Errorf("bd: parse json: %w", err)
	}
	beads := make([]Bead, len(arr))
	for i, b := range arr {
		beads[i] = Bead{
			ID: b.ID, Kind: kindFromLabels(b.Labels), Status: b.Status, Title: b.Title, Description: b.Description, Labels: b.Labels,
			Parent: b.Parent, Priority: b.Priority, Created: b.Created, Updated: b.Updated, Assignee: b.Assignee, Started: b.Started,
			CloseReason: b.CloseReason, CreatedBy: b.CreatedBy,
			Container: b.IssueType == "epic", // container kind = bd epic (ADR 0002)
			Event:     b.IssueType == "event",
		}
		for _, d := range b.Deps {
			if d.Type == "blocks" || d.Type == "conditional-blocks" { // Ordering edges; related kept apart below, membership/lineage ignored
				beads[i].BlockedBy = append(beads[i].BlockedBy, d.DependsOn)
			}
			if d.Type == "related" {
				beads[i].Related = append(beads[i].Related, d.DependsOn)
			}
		}
	}
	return beads, nil
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

// Show reads a bead (spec §3.1 reads use --json).
func Show(id string) (Bead, error) {
	out, err := run("show", id, "--json")
	if err != nil {
		return Bead{}, err
	}
	return parseShow([]byte(out))
}

// Close closes a bead, optionally with a reason (the reject path, §7.3).
func Close(id, reason string) error {
	args := []string{"close", id}
	if reason != "" {
		args = append(args, "--reason", reason)
	}
	_, err := run(args...)
	return err
}

// Ready lists the open, unblocked frontier (`bd ready`, spec §6.1). The loop
// fences these to the registry itself; this returns them all with their kind
// and labels so the fence stays in Go (registry is the source of truth).
func Ready() ([]Bead, error) {
	out, err := run("ready", "--json")
	if err != nil {
		return nil, err
	}
	return parseBeads([]byte(out))
}

// OpenList lists every Open bead with its parent and blocks edges — the bead
// list the loop's frontier re-admit reads (Spec D6), since bd ready hides
// a blocked Checkpoint and its members both.
func OpenList() ([]Bead, error) {
	out, err := run("list", "--status", "open,in_progress,deferred", "-n", "0", "--json")
	if err != nil {
		return nil, err
	}
	return parseBeads([]byte(out))
}

// InProgress lists every in_progress bead — the loop's busy-count source, from
// which the AFK pool's inflight is re-derived each tick (spec §6.2/§6.4, ADR 0003
// stateless).
func InProgress() ([]Bead, error) {
	out, err := run("list", "--status", "in_progress", "--json")
	if err != nil {
		return nil, err
	}
	return parseBeads([]byte(out))
}

// childBeads reads a container's children (`bd children`, includes closed) — the
// one shell-out both the drained-Chart count and the open-child idempotency guard
// share (ADR 0011), split out so neither re-spells the read.
func childBeads(id string) ([]Bead, error) {
	out, err := run("children", id, "--json")
	if err != nil {
		return nil, err
	}
	return parseBeads([]byte(out))
}

// OpenChildren counts a container's still-open children — the drained-Chart test
// (§8.4: 0 open children AND chart:defogged). `bd children` includes closed
// children, so the open filter is applied here. A closed child still holding
// effort/<child> counts as open (Spec D7): its work is not on the container.
func OpenChildren(id string) (int, error) {
	open, held, err := DrainBlockers(id)
	return open + len(held), err
}

// effortRefs is the effort-branch read DrainBlockers takes, a var so a test runs
// without a live repo.
var effortRefs = git.EffortRefs

// childrenAndEfforts is the drain gates' shared read: id's children and the
// git.EffortRefs set.
func childrenAndEfforts(id string) ([]Bead, map[string]bool, error) {
	kids, err := childBeads(id)
	if err != nil {
		return nil, nil, err
	}
	efforts, err := effortRefs()
	return kids, efforts, err
}

// DrainBlockers is what keeps a container from draining (Spec D7): its
// open children, and held, its closed children still holding effort/<child>. The
// refs come from one for-each-ref per call, never one git call per child.
func DrainBlockers(id string) (open int, held []Bead, err error) {
	kids, efforts, err := childrenAndEfforts(id)
	if err != nil {
		return 0, nil, err
	}
	open, held = DrainBlockersOver(kids, efforts)
	return open, held, nil
}

// DrainBlockersOver is DrainBlockers over an already-read graph, as the TUI
// does at refresh: kids' open count, and the closed ones whose id is in efforts
// (the git.EffortRefs set).
func DrainBlockersOver(kids []Bead, efforts map[string]bool) (open int, held []Bead) {
	for _, k := range kids {
		switch {
		case k.Open():
			open++
		case efforts[k.ID]:
			held = append(held, k)
		}
	}
	return open, held
}

// ReviewDue reports whether a Checkpoint wants its bare Review (Spec D8,
// FRs 28–31): its members are drained (MembersDrainedOver), at least one of them
// is closed (a Checkpoint whose members aren't spawned yet isn't reviewed empty),
// no pivot is open, and no Approval, open or closed, pins the tip
// (approvedAtHead, git.ApprovedAtHead's shape, asked of checkpoint/<id>). A
// stale Approval doesn't count, so a tip moved past it gets a fresh Review. One
// children read, one effort-ref read and one pivot read.
func ReviewDue(checkpointID string, approvedAtHead func(approvalBody, branch string) bool) (bool, error) {
	kids, efforts, err := childrenAndEfforts(checkpointID)
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
// children, the git.EffortRefs set and its open pivots.
func ReviewDueOver(kids []Bead, efforts map[string]bool, pivots []Bead, atHead func(approvalBody string) bool) bool {
	if len(pivots) > 0 || !MembersDrainedOver(kids, efforts) {
		return false
	}
	members := 0
	for _, k := range kids {
		switch {
		case isMember(k.Kind):
			members++
		case k.Kind == contract.KindApproval && atHead(k.Description):
			return false
		}
	}
	return members > 0
}

// MembersDrained reports whether a Checkpoint's members are drained (Spec
// D7, FR 28): no Build, Critique, Conflict, Surgery or Review open or
// still holding effort/<id>. Both the Review spawn and the approved land gate on
// it, so neither runs without a member's work.
func MembersDrained(checkpointID string) (bool, error) {
	kids, efforts, err := childrenAndEfforts(checkpointID)
	if err != nil {
		return false, err
	}
	return MembersDrainedOver(kids, efforts), nil
}

// MembersDrainedOver is MembersDrained over an already-read graph.
func MembersDrainedOver(kids []Bead, efforts map[string]bool) bool {
	for _, k := range kids {
		if isMember(k.Kind) && (k.Open() || efforts[k.ID]) {
			return false
		}
	}
	return true
}

// isMember is whether a Checkpoint child of kind k carries work onto its branch.
func isMember(k contract.Kind) bool {
	switch k {
	case contract.KindBuild, contract.KindCritique, contract.KindConflict, contract.KindSurgery, contract.KindReview:
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
func Claim(id string) error {
	_, err := run("update", id, "--if-status", "open", "--status", "in_progress", "--assignee", actor())
	return err
}

// actor is the bd actor resolved once, so the CAS assignee is what a human's
// plain `bd unclaim` sees as "you" (ADR 0052 §2; bd has no whoami). A var so a
// test can pin it without racing the once-cache.
var actor = sync.OnceValue(func() string {
	return actorFrom(os.Getenv("BEADS_ACTOR"), func() string {
		name, _ := git.UserName() // unset → "", falls through to $USER
		return name
	}, os.Getenv("USER"))
})

// actorFrom is bd's resolution order: $BEADS_ACTOR, then git user.name, then
// $USER. git is a func so it is only shelled out when $BEADS_ACTOR is unset.
func actorFrom(beadsActor string, gitName func() string, unixUser string) string {
	if beadsActor != "" {
		return beadsActor
	}
	if g := gitName(); g != "" {
		return g
	}
	return unixUser
}

// Unclaim releases a claim: it clears the assignee and resets status to open, the
// inverse of Claim. A non-terminal handler (one that neither closes nor promotes
// the bead to a further status — the drained-Chart promote-or-stamp branch) calls
// it so the released bead re-enters `bd ready` and the next tick re-dispatches it;
// without it the bead sits in_progress, which `bd ready` excludes, forever.
func Unclaim(id string) error {
	_, err := run("unclaim", id)
	return err
}
