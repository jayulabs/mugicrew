# ADR 0015: mugicrew Zoro slicing prompt — a forked, AFK `to-tickets` driven by `reconcileSpec`

Status: Accepted
Date: 2026-09-23

Builds on: ADR 0002 (atomic skeleton), ADR 0008 (Spec→Build is the one-to-many
slice), ADR 0013 (Nami's speccing session is the precedent fork), and the
Zoro Chart's settled decisions (BUILD-BOUNDARY, SLICING MODE = AFK,
SLICE DEPTH = THIN). Records the input to the Spec for how Zoro's
Spec→Checkpoint skeleton-lay is *driven*.

## Context

Zoro slices a Spec into the Checkpoint's Build children (ADR 0008). The Chart
settled that this is a **judgment**, not a mechanical copy of a pre-enumerated
list (BUILD-BOUNDARY), and runs **AFK** (SLICING MODE):
the loop dispatches `Zoro.Run(spec)`, which — mirroring `reconcileChart` forking
a speccing session — forks a slicing agent to do the judgment. The
implementation-surface research confirms `reconcileSpec` owns an
idempotent scaffold (walk to landed base → `SpawnSuccessorIfAbsent(Checkpoint
epic)` → `CutBranch` → **slice into N Builds** → close the Spec) and flags the
slice step as *the one piece with no reusable helper*.

That slice step is agentic, and an agent needs a prompt. The prompt is planned
as a fork of the interactive `to-tickets` skill
(`.agents/skills/to-tickets/SKILL.md`), which breaks a plan into tracer-bullet
tickets with blocking edges. `to-tickets` is HITL (its step 4 quizzes the user)
and tracker-generic (emits "tickets" with "Blocked by" text). This ADR settles
the fork's shape: what survives, what the AFK adaptation changes, and where the
prompt lives.

## Decision

**1. Home & layout — an embedded in-repo prompt asset, mirroring Nami's speccing
fork, not a live `.agents/skills/` dependency.** The slicing prompt is
`zoro/prompts/slicing.md`, `//go:embed`-ed and seeded by `slicingPrompt(specID)`
— the exact shape of `nami/prompts/speccing.md` / `speccingPrompt(chartID)`
(`nami.go`). `to-tickets` is forked *once* for its substance, not symlinked: its
interactive shape does not survive the AFK adaptation, and a live skill
dependency would drift from the mugicrew ontology. Not inline in `Run` — the
sibling `.md` prompts are the established readable home.

**2. Publish seam — the slicing agent lays the Builds itself.** The prompt
instructs the forked agent to `bd create` each Build (`kind:build`,
`--no-inherit-labels`) as a child of the already-cut Checkpoint epic and to wire
the `blocks` ordering edges with `bd dep add <blocked> <blocker>` (direction per
research §3.2). This mirrors the speccing agent authoring the Spec body via
`bd update` — the agent that *judged* the slice and its ordering is the one that
writes it, so no plan-serialization contract and no `SpawnBuildsIfAbsent` Go
helper are introduced. Idempotency is two-layered: `reconcileSpec` guards the
fork, and the prompt itself does a **read-existing-children roll-forward**
(enumerate the Checkpoint's current `kind:build` children first; create only the
missing ones), so a crash mid-slice re-runs and fills the gaps rather than
duplicating. The epic spawn, branch cut, and Spec close stay in `reconcileSpec`
(Go), which brackets the fork.

**3. AFK quiz → an in-prompt self-review pass.** `to-tickets` step 4 (quiz the
human on granularity and edges) is dropped — there is no human in AFK. It is
replaced, not deleted: before publishing, the agent re-reads its own slice
against an explicit checklist — each Build a complete vertical slice? each
`blocks` edge genuinely gating? each Build sized for one fresh context window?
prefactor sequenced first? The **pivot** mechanism (ADR 0005) is the real
downstream correction for a bad slice, but it is out of scope here (deferred to
an offshoot Signal); a cheap in-prompt gate catches the obvious misfire before it
becomes N wrong Builds.

**4. Vertical-slice / tracer-bullet rules and the wide-refactor expand–contract
exception are kept in substance, adapted only in vocabulary.** These blocks
*define what a Build is* and *how a wide mechanical change is sequenced with
ordering edges* — squarely the prompt's job, not out of scope for skeleton-lay.
"Ticket" becomes "Build (`kind:build`)"; "Blocked by" text becomes a `bd dep
add` `blocks` edge. Only `blocks` edges gate `bd ready` (research §2.1), so the
ordering these rules produce is exactly the frontier the skeleton needs.

**5. Prefactoring becomes a prefactor Build, sequenced first.** `to-tickets`
step 2 tells the agent to prefactor the code ("make the change easy, then make
the easy change"). Zoro lays beads and never executes code — Franky executes
Builds — so when the slice needs a prefactor, Zoro emits it as its own
`kind:build` at the head of the chain, with the dependent Builds `blocks`-edged
behind it. The instinct is kept; the doing moves to where it belongs.

## Consequences

- Zoro's implementation gains one prompt asset (`zoro/prompts/slicing.md`) and
  one embed accessor (`slicingPrompt`), structurally identical to Nami's
  speccing pair — no new prompt-plumbing pattern.
- `reconcileSpec`'s step 4 is "fork the slicing agent"; the agent owns the Build
  creates and edge wiring, so the only genuinely-new bd helper the skeleton
  needs is the pivot-side `Blocks`/`Supersede` wrappers (research §3), **not**
  a `SpawnBuildsIfAbsent` — the roll-forward lives in the prompt.
- The fork is a point-in-time adaptation: `to-tickets` can evolve independently
  without touching Zoro, and vice-versa.
- Authoring `slicing.md` and `slicingPrompt` is downstream implementation work
  (a Build under Zoro's own Checkpoint), not part of resolving this fog.

## Alternatives considered

- **Fork `to-tickets` as a live skill under `.agents/skills/`.** Rejected: its
  HITL shape does not survive AFK, and a live dependency drifts from the
  ontology. Nami already set the "fork once into an embedded prompt" precedent
  (ADR 0013).
- **Agent emits a structured slice plan; `reconcileSpec` Go parses it and calls
  `SpawnBuildsIfAbsent`.** Rejected: invents a serialization contract and splits
  "judge the edges" from "wire the edges" across the Go/agent seam, against the
  speccing precedent where the judging agent does its own `bd` writes.
- **Drop the quiz with nothing in its place.** Rejected: leans entirely on the
  downstream pivot to catch a bad slice, spending N wrong Builds first; a cheap
  in-prompt self-review is a strictly better gate.
- **Drop the vertical-slice / expand–contract rules as "not skeleton-lay."**
  Rejected: they *are* the definition of a Build and its ordering — the prompt's
  entire job.
