# ADR 0150: The land gate builds the composed squash outside the lock and lands it at the same base

Status: Accepted (amended by ADR 0189)
Date: 2026-10-12

Chart mc-fv9r ("Lands refuse a commit whose pinned nix build fails"), prototype
mc-fv9r.5 (throwaway on `refs/mugicrew/prototypes/mc-fv9r`). The gate must build
the tree that will land. The build can take minutes, but the land lock times out
waiters after 60s (ADR 0069), so the build cannot run inside `AnchorIfAbsent`'s
write leg (docs/research/landing-build-gate-loop-stall-and-land-lock.md).

## Decision

1. **`LandLocal` splits into compose and land.** *Compose* runs with no lock. It
   runs LandLocal's own squash sequence (detach at pin, rebase onto base, soft
   reset, commit) in the Landing's detached reconcile tree, which yields a squash
   commit that no branch reaches. *Land* runs under the land lock. It fast-forwards
   main to that **same commit**, but only if main is still at base and the
   container is still at pin. The commit that was built is the commit that lands,
   so its tree is the same by construction.
2. **The build lives in Jinbe, not in the strategy.** For `land_strategy: local`,
   `reconcileLanding` composes, builds the composed commit through a scratch ref
   `refs/mugicrew/gate/<landing-id>` (form from
   docs/research/nix-gate-build-of-an-unlanded-commit.md, ref deleted afterwards),
   then calls the locked land. nix stays out of package `git`. The locked leg
   stays `AnchorIfAbsent`'s write leg (ADR 0147 §1).
3. **A moved base re-runs, at most 3 times per reconcile.** If main has moved
   off base, the land is a no-op and the gate goes back to compose. Most moves of
   main leave the `apps/mugicrew` fileset unchanged, so the rebuild is a cache
   hit. After 3 misses the gate returns `ErrLandPending`: the Landing stays open,
   its claim is released and the next tick retries. This does not count toward
   `paused:failing`.
4. **Nothing is remembered across ticks.** The nix store is the memo. A
   re-gated success is a cache hit, a builder failure closes the Landing, and an
   infra failure needs a fresh run anyway, because nix does not cache failures.
   No Landing body line or event-log read-back is involved.

> **Amended by ADR 0189:** the gate is local-only. Under `land_strategy=gitlab`
> no local nix build gates the land; the MR's pipeline is the land gate.

## Consequences

Measured on this host against the real flake: compose took ~1s, an uncached build
took 244–265s, and a rebuild after a docs-only base move, or a re-gate, took 3s.
The uncached figure is above triage's 1m50s, but still well inside the ~15m hard
timeout. `merge-tree --write-tree` and `git replay` were considered for compose
and rejected: they can drift from `rebase`'s per-commit result, while reusing the
sequence `LandLocal` already runs cannot.
