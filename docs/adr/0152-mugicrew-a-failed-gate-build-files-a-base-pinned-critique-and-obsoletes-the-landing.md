# ADR 0152: A failed gate build files a base-pinned Critique and obsoletes the Landing

Status: Accepted (amended by ADR 0188)
Date: 2026-10-13

Chart mc-fv9r (Lands refuse a commit whose pinned nix build fails). Settles
grilling mc-fv9r.4. On a Checkpoint, the gate builds the rebased tree, at a
(pin, base) pair, before the land. This ADR adds a third obsolete reason to ADR
0145 §1–2 and widens the Critique (ADR 0018, ADR 0019) to be raised by a
Landing.

## Decision

1. **A builder failure obsoletes the Landing.** A builder failure is nix failing
   on a derivation build error. On one, Jinbe files a Critique and closes the
   Landing `obsolete: build failed`. This sits beside `obsolete: branch advanced`
   and `obsolete: conflict`. Infra failures still leave the Landing open.
2. **The Critique is a Checkpoint member, discovered-from the Landing.** Being a
   member is what holds the land and routes the Critique to Franky. The
   `discovered-from` edge is provenance only. The Landing stays a leaf.
3. **Its body carries what reproduces the failure:**
   - the `Base:` and `Tip:` pins (`git.BasePin` and `git.TipPin`);
   - the failing `.drv`;
   - the last 80 lines of the build log;
   - the command that reproduces the failure.
4. **Nothing new gates the re-land.** An open Critique already holds the land
   (`CheckpointLandHold`) and blocks the Review and Approval spawn
   (`MembersDrained`). Its fix moves the tip, so `approvedAtHead` fails and a
   fresh Review, Approval and Landing follow.
5. **Idempotent on (pin, base).** Before it builds, the Landing looks for an open
   Critique whose `Tip:` and `Base:` equal its own pair. If it finds one, it
   closes `obsolete: build failed` without building, because nix does not cache
   failures. `SpawnChildIfAbsent` is the backstop.
6. **Franky fixes the rebased tree.** A `Base:` pin on a Critique switches
   Franky's handler to rebase-then-fix. Usopp's Critiques carry no `Base:` pin.
   Franky rebases the Checkpoint onto the pinned base, not current main,
   resolving any textual conflict in the same session as the conflict handler
   does. He then fixes the failure test-first. The tree he fixes is the exact
   tree that failed.
7. **No attribution to main.** If main itself fails to build at the base, the
   Checkpoint still gets the Critique. Its fix unbreaks main when it lands.

> **Amended by ADR 0188:** under `land_strategy=gitlab` a failed MR pipeline
> joins the build failure: Jinbe files it as a Critique after one retry.

## Considered Options

- **A `conflict` variant for a build break.** Rejected. A build break is a defect
  in the work, which is a Critique, and the Critique's test-first fix path fits.
  Only the rebase leg is borrowed from conflict.
- **A plain Critique fixed at the Checkpoint tip.** Rejected. A break that exists
  only in the rebased tree would pass at the tip and fail the gate again,
  forever.
- **Building the bare base first to attribute the failure.** Rejected. It doubles
  the cost of a rare path.
