# A requested rebase defers to any open Conflict, and an unclaimed one yields to Landing's Conflict

A requested rebase is a third variant (`requested`) of `conflict` (Checkpoint, Franky) and `chart-conflict` (Chart, Nami), so the open-child guard of ADR 0011 (…chart-conflict-policy) §2 (`SpawnChildIfAbsent`, keyed on kind, blind to variant) holds at most one of them open per container. Settled by grilling (bead "What a rebase request does when the container already has one open, or an open Conflict"):

- **Request over an open requested rebase:** no second bead. `mugicrew rebase <container>` prints `already requested: <title> (<id>)` and exits 0 (idempotent); the New menu's `r` row shows the same as a flash. The open bead is **not** re-pinned to current mainline: a human who wants the newer base closes it and files again.
- **Request over an open rebase-conflict or number-clash Conflict / Chart-conflict:** no second bead. The verb names the open Conflict and its `Base:`, and exits 0. That Conflict already rebases the container onto a base no older than the last land attempt.
- **Landing hits a real Conflict while a requested rebase is open** (only the race the Survey allowed: a Landing already in flight when the request was filed; an open request undrains the container, so no new Landing spawns):
  - **unclaimed** request: `fileConflict` / `fileChartConflict` close it with reason `superseded: <conflict title>`, file the real Conflict (newer `Base:`, its variant and clash list), and link the two (supersede edge) so the human who filed the request can follow it.
  - **claimed** request: the guard swallows the Conflict as today (the call returns the open request); the in-flight rebase finishes, the container re-drains, and the next Landing files whatever collision remains.

## Considered options

- **Re-pin an unclaimed bead's `Base:`/`Tip:`** (on a repeat request, on an open Conflict, or on Landing's newer base). Rejected: new pin-rewriting code for a narrow case; on a Conflict it hides the base the collision was seen at; on Landing's conflict the requested variant would drop the number-clash detail.
- **Queue a second bead / variant-aware guard.** Rejected: two members rebasing one container branch race each other.
- **Always swallow Landing's Conflict** (no code). Rejected for the unclaimed case: Franky or Nami would rebase onto the older requested base, costing an extra land round, and a number-clash would surface only on that next round.
- **Supersede a claimed request too** (abort the in-flight effort). Rejected: throws away work in progress for a collision the re-land catches anyway.
