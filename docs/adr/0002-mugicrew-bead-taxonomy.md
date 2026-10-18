# ADR 0002: mugicrew bead taxonomy — kinds, edges, and derived state

Status: Accepted
Date: 2026-09-19

## Context

The mugicrew ontology needs a concrete scheme for four things on top
of bd: how a bead's **kind** is represented, how a bead **routes** to its owning
crewmate, which **edges** carry the graph's relationships, and where **lifecycle
state** lives. bd constrains the design (established empirically):

- The dependency-edge `type` is a **closed enum**
  (`blocks | tracks | related | parent-child | discovered-from | until |
  caused-by | validates | relates-to | supersedes`). There is **no** `deps.custom`
  config — custom edge types cannot be invented.
- Only `blocks` / `conditional-blocks` gate `bd ready`; every other edge is a
  pure semantic link. `parent-child` does **not** block.
- Labels are `<dimension>:<value>` by convention, richly queryable
  (`--label-pattern`, `bd query`); inheritance is a manual `propagate`, not
  dynamic.
- Lifecycle can be richer than open/closed via custom statuses or `bd set-state`
  dimensions — but those are opt-in mechanisms, not required.

The taxonomy sits on the settled frame (ADR 0001): chain-not-mutation, the spine
`Signal → Chart → Spec → Checkpoint → Build`, and Checkpoint = epic = the branch.

> **Amended by ADR 0009:** *both* container kinds are branches — `chart` is a
> branch too (charting artifacts merge on defog; prototypes go to a side ref).
> "Checkpoint = the branch" becomes "container kinds are branches."

> **Amended by ADR 0135:** `initiative` is a third container kind with **no**
> branch and no land — only `chart` and `checkpoint` are branches. It parents
> every bead of its member chains and is `caused-by` the Signal triaged into it.

## Decision

### Kind is a label; bd type is derived from it

Every runtime bead carries exactly one ontology label, **`kind:<value>`**, the
single **routing key**. The 11 kinds:

    signal · chart · grilling · research · prototype · spec ·
    checkpoint · build · review · critique · approval

> **Amended by ADR 0047:** `rollback` joins the list — a human-filed leaf owned
> by Jinbe (his first leaf kind), entering the graph unparented like `signal`.

> **Amended by ADR 0048:** a `checkpoint` may be `caused-by` a `rollback`, not
> only a `spec` (readers reach the Spec through it with `SpecOf`), and Jinbe —
> not Zoro — is the steward of that successor Checkpoint.

bd's own **issue type** is *derived* from kind, not a free axis: **`epic`** for
the two **container** kinds (`chart`, `checkpoint`), **`task`** for every other
(**leaf**) kind. Kind is a label rather than a custom bd type so it stays
queryable and orthogonal to bd's type machinery, and so `checkpoint` can be a bd
`epic` (for container/rollup semantics) without a name collision.

### Owner is derived from kind — nothing stored

A bead's owning crewmate is a pure function of its kind, so there is **no
`owner:*` label**. Routing is a kind-scoped query
(`bd ready --parent <map> --label-pattern 'kind:build'` → Franky's queue).

> **Amended:** the owner-from-kind function is stated as an
> invariant — every kind is owned by **exactly one** crewmate (`kind → crewmate`
> total and single-valued), a crewmate may own **many** kinds, and no kind is
> **co-owned**. Co-ownership would make this routing query ambiguous, so it is
> forbidden by construction.

- **Routing owner** — whose queue an open **leaf** bead lands in.
- **Steward** — the crewmate that *creates* a **container** (`chart` → Nami,
  `checkpoint` → Zoro). Documentation-only; never a routing target while the
  container still has open children.
  > **Amended by ADR 0003:** a *drained* container (open, all children closed)
  > **is** routed — to its **handler** (`chart` → Nami, `checkpoint` → Jinbe),
  > which extends owner-from-kind to container kinds. Steward (creates) and
  > handler (drains) may differ.
- **Operator** — anyone who acts on a bead. Equals the owner for every leaf;
  diverges only on the `checkpoint` (created by Zoro, operated by
  Franky/Usopp/Luffy through their own child beads, closed by Jinbe's merge).

### Edges map onto the closed enum — no custom edges

| relationship | meaning | bd edge | blocks `ready`? |
|---|---|---|---|
| **membership** | a container holds a member | `parent-child` | no |
| **ordering** | frontier / must-precede | `blocks` | **yes** |
| **succession** | same unit of work, next stage | `caused-by` | no |
| **offshoot** | new work found *mid-work* | `discovered-from` | no |
| **obsoletion** | a bead proven wrong/stale, replaced | `supersedes` | no |

- **Succession** is the spine: `signal → chart → spec → checkpoint`, each bead
  `caused-by` its predecessor. The predecessor closes but is kept for lineage.
  > **Amended by ADR 0008:** every succession edge is **1:1**, including
  > `chart → spec` (originally "one or more Specs"). Fan-out is only an offshoot
  > (`discovered-from`) or the `spec → build` slice.
- **Membership**: a `chart` parents its defog beads
  (`grilling`/`research`/`prototype`); a `checkpoint` parents
  `build`/`review`/`critique`/`approval`.
  > **Amended by ADR 0013:** the defog set drops `task` (was a fourth defog
  > kind). A Chart charts toward a Spec, not toward manual prerequisite work;
  > `task` is reserved (named but unowned) for a future use.
- **Offshoot** is reserved for genuine mid-work discovery (a Build turns up a
  defect → a `critique` `discovered-from` it), distinct from succession.
- **Obsoletion** uses bd's first-class `bd supersede <old> --with <new>`, which
  closes the obsolete bead and records the edge (e.g. a Checkpoint proves the
  Spec wrong → supersede the old Spec).

### Lifecycle state is derived, not stored

Because progress is the chain advancing (spawn next, close self), **every bead's
per-bead lifecycle is just `open → closed`**. No custom statuses, no `state:*`
dimension. The only rich lifecycle — the persistent `checkpoint` branch — is
**derived** from the graph by a waterfall (first match wins, so states are
mutually exclusive by construction):

| state | condition |
|---|---|
| **merged** | epic **closed** |
| **approved** | epic open · approval bead **closed** |
| **in-review** | epic open · **≥1 review bead exists** · not approved |
| **cut** | epic open · **no review bead yet** · not approved |

Keying `in-review` off a review bead *existing* (not being currently open) keeps
the branch `in-review` across the whole review/fix loop rather than flapping.
`review-ready` = all builds closed + no open critique.

### Skeleton is atomic; the rest spawns on demand

The steward lays only the statically-derivable skeleton — the **Checkpoint epic
+ its Build beads** (sliced from the Spec) + the cut branch, created together.
`review` / `critique` / `approval` beads are **spawned on demand** by the actor
whose gate opened, never pre-created. This is why there is no `uncut` branch
state (the Checkpoint is born already `cut` with open builds) and why an open
approval bead can never falsely read as a lifecycle state.

### Label inventory

`kind:*` is the **only** ontology dimension on a runtime bead. `wayfinder:*`
labels are **charting-only** (machinery for building a map) and never appear on
the runtime bead graph.

## Consequences

- Reading "what state is this branch in" costs a small structural query, not a
  label lookup — the price of zero state to keep in sync.
- Routing, ownership, bd type, and branch state are all **derived** from kind +
  graph shape; the only authored fact per bead is its `kind:*` label.
- `caused-by` is the mildest semantic stretch of the three genesis edges (it is
  true — the prior stage's completion causes the next), accepted because the
  closed enum leaves it once `supersedes`/`discovered-from` take their natural
  meanings.
- The trigger *mechanism* that spawns on-demand beads (poll / event /
  orchestrator) is deliberately out of scope here — it is decided by Activation
  & orchestration, which this taxonomy unblocks.

## Alternatives considered

- **Kind as a custom bd issue type.** Rejected: collides with `checkpoint` =
  `epic`, needs config, and loses label query power.
- **Custom edge types (`derived-from`, `reviews`).** Impossible — the edge enum
  is closed and has no `deps.custom`.
- **`supersedes` for succession.** Rejected: `supersedes` means *obsolete/replaced*
  and has a purpose-built `bd supersede` command; using it for healthy
  stage-advance would conflate a re-do with normal flow.
- **Stored branch state** (custom statuses or a `state:*` dimension). Rejected:
  every state is structurally derivable, and the checkpoint is multi-actor by
  design, so any owner-driven stored state would drift.
- **`fog` / `cloud` as kinds.** Rejected: `cloud` had no distinct owner, edge,
  or lifecycle — sharpness is a scheduling property (blocked vs ready), not a
  kind. `fog` is a collective glossary noun for a Chart's defog children; the
  defog *methods* (`grilling`/`research`/`prototype`, mirroring wayfinder's
  ticket types; `task` was a fourth but is reserved-unowned per ADR 0013) are
  the real routing kinds.
