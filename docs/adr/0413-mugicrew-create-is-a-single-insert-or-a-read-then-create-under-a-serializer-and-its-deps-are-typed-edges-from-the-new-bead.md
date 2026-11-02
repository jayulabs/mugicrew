# ADR 0413: mugicrew bd core's Create is a single insert or a read-then-create under a Serializer, and its Deps are typed edges from the new bead

Status: Accepted
Date: 2026-10-29

Amends ADR 0380 (mugicrew-the-bd-core-types-its-write-class…).

## Context

ADR 0380 typed Create as a single insert with no class argument, and gave
`CreateSpec.Deps` as a raw `--deps` string. A crewmate's `Spawn…IfAbsent` is a
read (is there already one?) then a create, so it is an ADR 0070
(mugicrew-one-bd-write-rule) read-then-write, and it built create argv for the
CLI, out of reach of the bdtest fake. A raw dep string also hid bd's reversed
`blocks:X`: in `bd create --deps`, `blocks:X` makes X block the new bead.

## Decision

1. **Create's write class is "a single insert, or a read-then-create under a
   given Serializer".** `CreateSpec.Serializer` is optional. nil is a plain
   single insert, as before. The real cli ignores it, as it does for
   `SetField`. The fake enforces a non-nil one through the check `SetField`
   uses.
2. **`CreateSpec.Deps` is `[]Edge`, edges from the new bead.** `From` is empty,
   `To` and `Type` name the target; Create refuses a Dep with `From` set.
3. **Rendering is forward-only.** The cli renders a `blocks` Dep as
   `blocked-by:<to>` and every other type as `<type>:<to>`. A reversed
   Latch-style dep (the new bead blocks an existing one) cannot be a Dep and
   stays on its CLI builder.
4. **The fake lays each Dep as an edge** that `Edges` reads back under its own
   type, through the edge rules `AddEdge` runs minus the dotted-ID pre-check
   (`create --deps` skips it). A refused Dep leaves no bead.
5. **`Successor` gains a `Serializer`** that the spawn helpers copy onto their
   CreateSpec. `SpawnSurveyIfAbsent` creates under `UnderClaim(drafter)`.

## Consequences

- No Metadata field: a create that sets metadata (`CreateDraft`) stays on
  `successorCreateArgs`, as do `CreateRaiseDraft`, the offshoot and Errand
  filings.
- A seeded test sees a spawn in the graph and the fake refuses a spawn whose
  claim is not held.
