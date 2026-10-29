# ADR 0395: mugicrew bd.Core and its process default are bd's test seam, a second real implementation pinned by a contract suite

Status: Accepted
Date: 2026-10-28

Chart: Bead-level bd interface and bdtest fake, proven on dispatch. Cites ADR 0064
(mugicrew-loop-restart-test-seams) without amending it, and complements ADR 0380
(mugicrew-the-bd-core-types-its-write-class…).

## Context

The `bd` package's only test hook was the CLI-string `run`/`runOnce` vars, so each
package that calls bd re-exported the helpers it uses as its own package vars and
stubbed them. ADR 0064 sanctioned that style for the loop restart and rejected an
interface with one implementation (`Builder`). ADR 0380 typed ADR 0070
(mugicrew-one-bd-write-rule)'s write class into a narrow bead-level core and has the
fake seal the CLI. This ADR records the core itself as the seam.

## Decision

1. **`bd.Core` plus its process default is bd's test seam.** The free functions
   delegate to the default. The real default is the unexported `cli`, carrying today's
   argv. `bd.Use(c)` installs another core and returns its restore;
   `bdtest.New(t)` calls it and restores at cleanup.
2. **The fake is a second real implementation.** ADR 0064 rejected an interface with
   one implementation because it would only be mocked. `bdtest`'s in-memory graph
   models bd's observable behaviour (default status set, list caps, readiness, edge
   types, not-found text), and one contract suite, parameterized over `func(t) bd.Core`,
   pins it to real bd: the fake leg runs on every `go test`, the real leg only behind
   `MUGICREW_BD_TEST=1`.
3. **While a non-cli core is installed the CLI is sealed.** `run`, `runIn` and
   `runBatchOnce` error naming the argv and the seal, so a helper not yet on the core
   fails its test and cannot reach the live Bead Graph.
4. **ADR 0064 stands.** The loop-restart vars (`mainRev`, `evalOutPath`, `buildRev`,
   `execLoop`) stay; they stub non-bd seams. Only package vars that re-exported a bd
   helper are replaced, and a var like session's `showBead = bd.Show` keeps working
   untouched because `bd.Show` now reads the default.

## Considered options

- **More package vars per consumer:** rejected. Each test scripts a call sequence, a
  forgotten stub reaches the live bd, and nothing checks the write rule.
- **A fake `bd` on `PATH` (as `fakeBd` does for the loop):** rejected for the graph.
  It would re-parse argv in the fake and cannot be typed by write class.
- **An interface with one implementation:** rejected by ADR 0064, and not what this
  is: the contract suite makes the fake a peer of the CLI.

## Consequences

- A test seeds literal beads and asserts the graph's end state; it does not count calls.
- Free functions off the core's reads stay on the CLI until migrated, and fail loudly
  under a fake until then.
- A bd upgrade that changes a pinned behaviour fails the contract suite's real leg.
- The first Build also moved helpers beyond dispatch's path onto the core (ListAll,
  ListByID, ListChildren, OpenSet, OpenList, PausedList, AddLabel, Comments,
  InitiativesByRef, depTargets, OpenBlocked). They stay there: each is a thin wrapper over a
  core method, and leaving them half-migrated would reintroduce CLI-only holes under a fake.
  Phases 2–4 migrate the rest.
