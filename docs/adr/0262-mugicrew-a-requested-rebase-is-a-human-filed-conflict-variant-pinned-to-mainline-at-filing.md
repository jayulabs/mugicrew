# A requested rebase is a human-filed `requested` variant of Conflict / Chart-conflict, pinned to mainline at filing

Amends: ADR 0102 (leaf-efforts-integrate-onto-ref-only-containers) §4 (adds a replace-mode user).

A human sometimes needs an open container branch (`chart/*`, `checkpoint/*`) on current mainline before it lands: mainline gained a fix in-flight Builds and Reviews need, or drift should surface early. Containers are already rewritten today through the replace mode of ADR 0102 §4 (Conflict, Chart-conflict, and the Base-pinned Critique of ADR 0152 (a-failed-gate-build-files-a-base-pinned-critique…)), so a requested rebase reuses that path rather than adding a rewrite rule. Settled by grillings on the "Manually requested rebase of a Chart or Checkpoint branch" Chart:

- **A variant, not a kind.** It is a third variant, `requested`, of `conflict` (Checkpoint, Franky) and `chart-conflict` (Chart, Nami), beside `rebase-conflict` and `number-clash`. Owner, replace-mode integrate and close follow the kind.
- **Human-filed only, no triage.** A human files it directly with `mugicrew rebase <container>` or the New menu's `r` row (ADR 0241) on a container row. Agents never file one; they may suggest one by offshoot Signal.
- **Pinned at filing.** It carries `Base:` (mainline at filing) and `Tip:` (the container tip), like a Conflict; it rebases onto that pin, not onto mainline at claim time.
- **Filing any time; the land lock orders it against Landing.** No check against an in-flight Landing. The land lock and the Approval's `Commit:` pin serialize the two: rebase first, the Landing finds the branch moved and closes `obsolete: branch advanced`; land first, the rebase finds the container landed and closes moot. Open-Conflict handling: ADR 0261 (a-requested-rebase-defers-to-an-open-conflict…).
- **Close follows Conflict.** On a Checkpoint, closing spawns a fresh Review only if a Review already ran, and a stale Approval is obsoleted; on a Chart, it re-drains as a Chart-conflict does.
- **Blocks no sibling.** It adds no edges; a human who wants a Build to wait for it adds the edge.

Sibling efforts cut before the rewrite: ADR 0264 (mugicrew-a-squash-integrate-replays-from-the-effort-s-recorded-cut-point…) covers every replace rewrite, this variant included.

## Considered options

- **A new kind or a new rewrite rule.** Rejected: the replace path, owners and open-child guard already exist; a kind would duplicate them.
- **Triage the request, or let agents file it.** Rejected: the need is the human's judgment; an agent-filed rewrite of a container under live siblings is too costly to automate. Agents suggest by Signal.
- **Rebase onto mainline at claim time.** Rejected: an unpinned base cannot be compared with Landing's or a Conflict's base (ADR 0261) and moves under the effort.
- **Refuse filing while a Landing is in flight.** Rejected: the land lock and `Commit:` pin already make either order safe.
- **Record only an amendment to ADR 0102.** Rejected: 0102 is the integrate contract; who may trigger a rewrite and when is trigger policy.
