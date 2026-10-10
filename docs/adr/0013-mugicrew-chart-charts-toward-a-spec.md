# ADR 0013: mugicrew Chart charts toward exactly a Spec — defog is grilling/research/prototype; task reserved

Status: Accepted
Date: 2026-09-22

Amends: ADR 0002 (narrows the defog kind set from four to three, removing `task`;
pins the Chart's destination to the Spec).

## Context

Two symptoms, one root cause. Nami's charting prompt (`charting.md`) is the
mugicrew equivalent of `/wayfinder run`, and it inherited wayfinder's framing of
the goal as **"name the destination — what a done answer looks like."** Reaching
for *the answer* pulls charting toward **building** that answer: Nami decomposes
a Chart into implementation chunks and files them as `kind:task`, the nearest
label for "a unit of work to do." Observed directly on a real Chart, whose own
decisions read "full-view (task)" and "create-Signal (task)" — both plainly
coding tracks, not defog.

ADR 0002 defined `task` as the fourth defog kind ("manual prerequisite work that
must happen before a requirement can be clarified"). In practice no charting flow
produces one: `taskChecklist` is generic "read the body, do it by hand, close" —
indistinguishable from any other bead — and nothing upstream needs a manual,
non-coding prerequisite as a *defog* step. `task` is a kind in search of a use,
and its name collides with generic build-work, which is what makes the
mislabeling easy. (It was never in ADR 0002's canonical 11-kind roster either —
it appeared only in the membership and alternatives sections.)

Both symptoms share the same fix: the Chart's only destination is **exactly one
Spec** — which the code already enforces (`reconcileChart` hard-spawns one
`specSuccessor`, ADR 0008). Pinning that in the *prompt* removes the pull toward
implementation, and once the destination is "a Spec, not a built thing," the
`task` defog kind has nothing left to do.

## Decision

1. **A Chart charts toward exactly a Spec.** The destination Nami names is always
   the one Spec the defogged Chart spawns in succession (ADR 0008) — a **decision
   record**, never a built artifact, a deliverable, or an arbitrary end state. The
   `Destination` field of the wayfinder map means "what the Spec must decide,"
   not "what the finished thing looks like." Implementation stays downstream as
   `kind:build` under the Checkpoint the Spec cuts (spine: Chart → Spec →
   Checkpoint → Build, unchanged).

   > **Clarified (authorship):** naming the destination is charting's *first*
   > act; **writing** that decision record is its *last* — a speccing session
   > over the defogged Chart authors the Spec body (Nami), superseding the MVP
   > stub. Zoro then slices the authored Spec. See `apps/mugicrew/CONTEXT.md`.

2. **The defog kinds are `grilling` / `research` / `prototype`.** `task` is
   **removed from the defog set** and from Nami's ownership. A Chart parents only
   these three.

3. **`task` is reserved, not deleted.** The `KindTask` constant stays defined but
   unowned — parked alongside the other named-but-unwired kinds — so the name is
   held for a future use rather than repurposed.

   > **Amended by ADR 0113 and ADR 0115:** the reservation is spent. `task` is
   > now Brook's HITL kind: a free-standing walk-through of external-world
   > steps, ordered against Checkpoint lands by `blocks` edges. It is still not
   > a defog kind; §2 stands.

## Consequences

- `apps/mugicrew/nami/prompts/charting.md`: destination reframed to "exactly a
  Spec"; the four defog kinds become three (drop `task`); the existing "no impl
  children" guard points at the Spec-only destination.
- `apps/mugicrew/CONTEXT.md`: Nami owns `grilling`/`research`/`chart-conflict`
  (no `task`); the Task kind entry becomes a reserved note; the Fog collective is
  Grilling/Research/Prototype.
- `apps/mugicrew/contract/contract.go`: `KindTask` moves to the reserved bucket.
- `apps/mugicrew/registry/`: the `KindTask → nami` route is removed (Nami's
  registry kinds drop from five to four).
- `apps/mugicrew/nami/nami.go`: `task` unwired from `Launch`; the remaining
  checklist user is `chart-conflict`.
- ADR 0002's membership and defog-methods lines carry an inline amendment note.
- The `chart:defogged` completion mechanic, the Chart branch/land, and the
  Chart → Spec succession are all unchanged — this narrows *what a Chart may
  contain* and *how its destination is framed*, not the spine.

## Alternatives considered

- **Keep `task`, strengthen the prompt's guard prose.** Rejected: the guard
  ("Never create an 'implement X' child") was already explicit and pinned by a
  test, and the mislabeling happened anyway. Prose can't beat a kind whose name
  *is* the generic word for build-work; removing the kind removes the collision.
- **Delete `KindTask` entirely.** Rejected: the name is a natural fit for a
  future manual/chore leaf; reserving it costs one unused constant and keeps the
  option open (YAGNI cuts the wiring, not the name).
- **Leave the destination framing wayfinder-generic.** Rejected: "reach the
  answer" is exactly what produced the impl beads; the mugicrew invariant is
  narrower than wayfinder's (one Chart resolves to one Spec, ADR 0008), and the
  prompt should say so.
