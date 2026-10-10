package bd

import (
	"fmt"
	"slices"
	"strings"

	"jayulabs.org/mugicrew/contract"
)

// Pivot graph plumbing (Spec "Zoro pivot triage", Impl Decision 2; ADR 0043): every
// leg that raises, gates, surfaces, or launches a kind:pivot reads and writes it
// through these helpers. The gate is the Checkpoint blocks→pivot edge (FR15: the
// single index); the pivot carries related→Spec and discovered-from→each raiser.
//
// Build-time check (Impl Decision 3): bd accepts a second
// discovered-from edge from an existing pivot to a later raiser, so a merged raise
// needs no comment-marker fallback. bd holds ONE edge per ordered pair: adding a
// different type over an existing edge errors — Blocks/Related do not guard that
// (today's pairs never collide; Supersede owns the remove-then-add).

// depTargets lists the beads one dependency hop from id along edges of typ (every
// type when typ is "") — "down" is what id depends on, "up" is what depends on id.
// `bd dep list --json` rows share the bead object shape, so parseBeads reads them.
func depTargets(id, direction, typ string) ([]Bead, error) {
	args := []string{"dep", "list", id, "--direction=" + direction, "--json"}
	if typ != "" {
		args = append(args, "-t", typ)
	}
	out, err := run(args...)
	if err != nil {
		return nil, err
	}
	return parseBeads([]byte(out))
}

// depAddArgsIfAbsent is the pure argv for `from depends-on to` of type typ, or nil
// when existing (from's down-targets of that type) already holds it — the
// idempotency guard, so a re-run tail adds no edge.
func depAddArgsIfAbsent(existing []Bead, from, to, typ string) []string {
	if hasTarget(existing, to) {
		return nil
	}
	return []string{"dep", "add", from, to, "-t", typ}
}

// hasTarget reports whether a depTargets read holds the bead id.
func hasTarget(targets []Bead, id string) bool {
	for _, b := range targets {
		if b.ID == id {
			return true
		}
	}
	return false
}

func addEdgeIfAbsent(from, to, typ string) error {
	existing, err := depTargets(from, "down", typ)
	if err != nil {
		return err
	}
	if args := depAddArgsIfAbsent(existing, from, to, typ); args != nil {
		_, err = run(args...)
	}
	return err
}

// HasEdge reports whether from already depends on to along an edge of typ — the
// guard a flow handing final edges to a publish batch filters on, since one
// failing batch line rolls back the whole publish.
func HasEdge(from, to, typ string) (bool, error) {
	existing, err := depTargets(from, "down", typ)
	return hasTarget(existing, to), err
}

// RaisePivot is the Go half of the split raise (ADR 0043 §3–4), shared by Franky
// (Build/Conflict) and Usopp (Review): for each open pivot discovered-from bead —
// fresh (a Draft by bead), or an existing same-FR pivot the agent merged into —
// hand Checkpoint blocks→pivot, pivot related→specID and bead's close 'pivoted:
// <pivot>' to the publish, so the pivot and its gate become visible in one batch
// and the Checkpoint is never briefly unfrozen (Spec D6; FR 24). Edges already
// present are not handed over (one failing batch line rolls back the publish), so
// a merged raise and a re-dispatch both converge. A Spec-less Checkpoint (specID
// "") errors before anything is written (Spec D7): no gate freezes it behind a
// pivot nobody can resolve, and bead's failure path pauses it for the human.
// ponytail: a merged-into pivot that was another creator's Draft would dangle if
// that creator's effort failed and its tail deleted it (FR18). Siblings run
// concurrently (ADR 0102), so the serializer is the pivot merge rule (Spec
// D10, ADR 0103): the build and review prompts merge only into a pivot the
// Checkpoint's blocks-edge query lists (published) or their own prior raise, and
// otherwise raise their own; a duplicate is accepted and the sweep closes it
// superseded:. Guard (publish the merged pivot here) if that rule ever loosens.
func RaisePivot(ctx contract.Ctx, bead Bead, specID string, pivots []Bead) error {
	ids := IDs(pivots)
	if specID == "" {
		return fmt.Errorf("checkpoint %s has no Spec (caused-by, or through a Rollback) to relate pivot(s) %v to", bead.Parent, ids)
	}
	var edges []Edge
	for _, id := range ids {
		if err := edgeIfAbsent(&edges, bead.Parent, id, "blocks"); err != nil {
			return err
		}
	}
	for _, id := range ids {
		if err := edgeIfAbsent(&edges, id, specID, "related"); err != nil {
			return err
		}
	}
	return CloseOnPublish(ctx, bead.ID, "pivoted: "+strings.Join(ids, ", "), edges)
}

// edgeIfAbsent appends from→to of typ to edges unless bd already holds it.
func edgeIfAbsent(edges *[]Edge, from, to, typ string) error {
	ok, err := HasEdge(from, to, typ)
	if err == nil && !ok {
		*edges = append(*edges, Edge{From: from, To: to, Type: typ})
	}
	return err
}

// Blocks makes blocked depend on blocker (a blocks edge) unless it already does —
// the Checkpoint gate: Blocks(checkpoint, pivot).
func Blocks(blocked, blocker string) error { return addEdgeIfAbsent(blocked, blocker, "blocks") }

// Related adds a related edge from → to unless present — Related(pivot, spec).
func Related(from, to string) error { return addEdgeIfAbsent(from, to, "related") }

// OpenPivots is the Checkpoint's direct blocks targets filtered to open
// kind:pivot — the one read behind both gate predicates, Usopp's dedupe, and
// the sweep (FR15). The TUI's Inbox lead grouping reads the same blocks edge off
// its snapshot (tui.pivotGate) instead.
func OpenPivots(checkpointID string) ([]Bead, error) {
	return OpenBlockersOfKind(checkpointID, contract.KindPivot)
}

// OpenBlockersOfKind is id's open blocks targets of kind — a chart-conflict's
// open grilling blockers, the guard that keeps a re-run from stacking a second
// escalation (ADR 0079).
func OpenBlockersOfKind(id string, kind contract.Kind) ([]Bead, error) {
	return openOfKindAlong(id, "down", "blocks", kind)
}

// OpenBlockers is id's open blocks targets of any kind — the land hold (Spec
// D6): the loop's and Jinbe's land guard and the TUI's t refusal read it,
// and name the blockers by title (the I/O-free preview reads its snapshot twin).
func OpenBlockers(id string) ([]Bead, error) {
	targets, err := depTargets(id, "down", "blocks")
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(targets, func(b Bead) bool { return !b.Open() }), nil
}

// PivotsDiscoveredFrom is the open pivots discovered-from a raiser bead — what the
// Franky/Usopp tails read after the agent returns (raise wiring, Impl Decision 3).
func PivotsDiscoveredFrom(id string) ([]Bead, error) {
	return openOfKindAlong(id, "up", "discovered-from", contract.KindPivot)
}

func openOfKindAlong(id, direction, typ string, kind contract.Kind) ([]Bead, error) {
	targets, err := depTargets(id, direction, typ)
	if err != nil {
		return nil, err
	}
	return openChildrenOfKind(targets, kind), nil
}

// PivotCheckpoint walks the gate edge in reverse: the Checkpoint a pivot blocks.
func PivotCheckpoint(pivotID string) (string, bool, error) {
	return kindAlong(pivotID, "up", "blocks", contract.KindCheckpoint)
}

// PivotSpec is the Spec a pivot is related to — Zoro.Launch's second resolved id.
func PivotSpec(pivotID string) (string, bool, error) {
	return kindAlong(pivotID, "down", "related", contract.KindSpec)
}

func kindAlong(id, direction, typ string, want contract.Kind) (string, bool, error) {
	targets, err := depTargets(id, direction, typ)
	if err != nil {
		return "", false, err
	}
	found, ok := firstOfKind(targets, want)
	return found, ok, nil
}

// firstOfKind picks the first bead of the wanted kind, any status — a reverse
// lookup wants the edge's target even once it closed.
func firstOfKind(beads []Bead, want contract.Kind) (string, bool) {
	for _, b := range beads {
		if b.Kind == want {
			return b.ID, true
		}
	}
	return "", false
}

// supersedeArgs is the pure argv sequence for Supersede(old, new): nil when old
// already carries supersedes→new (the re-run no-op); otherwise a `dep remove` first
// when old→new already exists as another type (bd holds one edge per ordered pair
// and rejects supersede over it), then the supersede.
func supersedeArgs(superseded, all []Bead, old, newID string) [][]string {
	if hasTarget(superseded, newID) {
		return nil
	}
	var cmds [][]string
	if hasTarget(all, newID) {
		cmds = append(cmds, []string{"dep", "remove", old, newID})
	}
	return append(cmds, []string{"supersede", old, "--with", newID})
}

// Supersede records old supersedes→newID (FR47, ADR 0043 §8). Build-time check:
// `bd supersede` on an already-closed old (reconcileSpec closed the
// Spec before the pivot was raised) adds the edge and leaves it closed — no
// dep-add fallback needed.
func Supersede(old, newID string) error {
	superseded, err := depTargets(old, "down", "supersedes")
	if err != nil {
		return err
	}
	all, err := depTargets(old, "down", "")
	if err != nil {
		return err
	}
	for _, args := range supersedeArgs(superseded, all, old, newID) {
		if _, err := run(args...); err != nil {
			return err
		}
	}
	return nil
}

// discoveredCreateArgs is successorCreateArgs plus a discovered-from edge to the
// origin (the pivot a sweep acts for) — the key SpawnDiscoveredIfAbsent finds it by.
func discoveredCreateArgs(originID, predID string, s Successor) []string {
	return successorCreateArgs(predID, s, "discovered-from:"+originID)
}

// SpawnDiscoveredIfAbsent creates a bead of s.Kind caused-by predID and
// discovered-from originID, unless one of that kind (any status) is already
// discovered-from originID. The sweep's create leg: the predecessor already has a
// successor of the kind (the Spec or Chart being replaced), so
// SpawnSuccessorIfAbsent's caused-by guard would find the old one.
func SpawnDiscoveredIfAbsent(originID, predID string, s Successor) (id string, created bool, err error) {
	found, err := depTargets(originID, "up", "discovered-from")
	if err != nil {
		return "", false, err
	}
	if id, ok := firstOfKind(found, s.Kind); ok {
		return id, false, nil
	}
	id, err = run(discoveredCreateArgs(originID, predID, s)...)
	return id, err == nil, err
}

// OpenChildBeads is a container's open children of every kind — the members a
// sweep closes out.
func OpenChildBeads(id string) ([]Bead, error) {
	kids, err := childBeads(id)
	if err != nil {
		return nil, err
	}
	var out []Bead
	for _, k := range kids {
		if k.Open() {
			out = append(out, k)
		}
	}
	return out, nil
}

// ForceClose closes over bd's refusals (an open blocker, open children) — the
// sweep retiring a frozen Checkpoint and its members, already decided by a human.
func ForceClose(id, reason string) error {
	_, err := run("close", id, "--force", "--reason", reason)
	return err
}
