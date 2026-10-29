# ADR 0380: mugicrew bd core types each write's ADR 0070 class into its signature, names three serializer kinds, and bdtest seals the bd CLI

Status: Accepted
Date: 2026-10-28

Chart: Bead-level bd interface and bdtest fake, proven on dispatch. Settles the
prototype "Does a typed write-class bd core with a Serializer argument read well
at real call sites and drive a seeded dispatch test?". The throwaway is on
`refs/mugicrew/prototypes/mc-5bg93.2` (commit 20ace1d6).

## Context

The Survey settled on a narrow primitive core in bd. bd's free functions
delegate to a process default, and `bdtest.New(t)` swaps that default. It also
settled that ADR 0070 (mugicrew-one-bd-write-rule)'s write classes are typed
into the core's signatures. A prototype rewrote Publish, Pause, dispatch's
guarded release and EditChartBody over the core. It also ported
TestFailedEffortReleasesClaimGuarded onto a seeded in-memory graph, with no bd
var stubbed.

## Decision

1. **A signature is its write class.**
   - Commutative or idempotent calls take no class argument: `AddLabels`,
     `RemoveLabel`, `Close`.
   - CAS calls take their guard: `Claim(id, IfStatus)` and
     `Unclaim(id, IfAssignee)`, where Unclaim becomes
     `bd unclaim --if-assignee`.
   - Read-then-writes take a `Serializer`: `SetField(id, Field, value, s)` and
     `Delete(ids, s)`.
   - `Batch(ops []Op)` is atomic, with typed ops instead of a script string.
2. **CAS guards stay inside the free functions.** For example,
   `bd.Unclaim(id)` is `core.Unclaim(id, bd.Mine())`. Callers don't change. The
   Serializer is the only class argument a call site writes.
3. **There are three serializer kinds:**
   - `bd.UnderClaim(id)`, the claim on the predecessor, container or creator;
   - a lock token that only its `With…` func mints, such as
     `bd.WithLandLock(func(land Serializer) error)`;
   - the effort's dispatcher hold, ADR 0154
     (mugicrew-the-dispatcher-holds…), minted from its `liveness.Hold`.

   The prototype's fake found the need for the third kind. dispatch's `fail`
   path deletes a handler's Drafts after that handler closed its own bead, so
   no claim is held by then. The dispatcher hold is what still serializes it.
4. **The fake enforces the rule.** It fails the test on a read-then-write in
   any of these cases:
   - it has a nil Serializer;
   - it names a claim this process doesn't hold;
   - it uses a lock token after its lock was released.
5. **`bdtest.New` seals the bd CLI.** While a fake is the default, every
   `run`/`runIn` call errors. A helper that isn't on the core yet fails its
   test and can't reach the live graph. The prototype showed this risk: an
   unmigrated `bd.Close` ran against the real bd.
6. **The seeding API uses plain beads.** It takes literal `bd.Bead` values plus
   small shaping helpers, `bdtest.Claimed(b)` and `bdtest.Draft(creator, b)`.
   There is no builder; helpers are added as migrated tests need them.
   - Faults are injected with `FailOn(verb, id|"*", err)` and cleared with
     `Heal`.
   - Another writer's change mid-test is `Mutate(id, fn)` or `Seed`.
   - Tests assert the graph's end state through `f.Bead(id)`. `f.Calls()` is a
     last resort.

## Considered options

- **CAS guards at every call site** (`bd.Unclaim(id, bd.Mine())`): rejected.
  It touches every caller, and the guard is always this actor's own claim.
- **Two serializer kinds, treating the fail-path delete as an ordering
  defect:** rejected. The delete runs under the dispatcher hold by design.
- **A fluent graph builder:** rejected for now. Literal beads and two helpers
  covered the ported test.

## Consequences

- `session.Swept` and every other package var that still holds a bd free
  function (`showBead = bd.Show`) reach the fake without being touched.
- dispatch's release keeps its status re-read. `--if-assignee` cannot guard on
  status, and the re-read also computes `Aborted`.
- The contract suite's real-bd leg must pin whether
  `bd unclaim --if-assignee` reopens a closed bead. The fake refuses that
  case.
- A fault is placed at a semantic moment (the worktree cut, the reconcile, the
  crewmate's Run) instead of a call counter. Each exit therefore needs its own
  hook.
- The Publish batch remains atomic but not a CAS. The research doc's status
  guard op is still open.
