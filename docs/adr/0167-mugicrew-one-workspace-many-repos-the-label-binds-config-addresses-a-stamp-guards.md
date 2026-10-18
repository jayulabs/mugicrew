# One Workspace, many repos: the label binds, config addresses, a stamp guards

Status: accepted

This amends ADR 0089 (one Workspace = one repo). A Workspace still has exactly
one Bead Graph, one loop, one land lock and one `events.jsonl`, all in the
Workspace repo. A container whose `context:` names a `[context.<name>] repo =`
context branches and lands in that child repo (ADR 0166). The repo is resolved
live at every use: the label is the binding, config is the address. Live
resolution alone lets an edited `repo =` silently fork work in flight (a cut
site finds no `checkpoint/<id>` and cuts a fresh one, and `LandedBase` finds no
trailer). So the first branch cut also stamps the dir, which guards but never
addresses.

- **The stamp is a `repo:<dir>` label** on the Chart and each Checkpoint. It is
  written at the first cut and copied by successors wherever `context:` is
  copied. Only labelled nested containers carry it, so a no-context Workspace
  pays 0 (ADR 0157).
- **Drift holds.** The ADR 0158 tick scan, which already resolves config when a
  bead carries `context:`, also compares stamp to live dir. A mismatch writes
  `paused:context`, which cascades, and the pause line names both dirs. Jinbe
  re-checks before a land, the one irreversible step.
- **The human clears drift with no new verb**: revert `repo =`, or relabel
  `repo:<new dir>` on the container by hand. The next tick lifts the hold.
- **A Rollback under drift refuses.** Jinbe compares the target Checkpoint's
  stamp with the live dir before reverting. On a mismatch the Rollback stays
  open and paused. The stamp is never used as a fallback address.
- **An undeclared context fails safe in flight.** `integrate` refuses, and the
  backstop leaves `effort/<id>` intact in the child. Work resumes when the ADR
  0158 hold lifts.
- **Per-repo lands surface Workspace-wide, as unions over the declared repos**
  (each 0 extra reads with none declared):
  - History (TUI and v2) is one merged list, newest first, with a context
    column, so `R` reaches nested lands.
  - Efforts and `chopper ps/list` take the same union.
  - Nested land, revert and integrate events gain `context=<name>` and
    `repo=<dir>`, and `into` names the child's trunk. Unlabelled events are
    byte-identical.
- **`mugicrew lineage` stays Workspace-only.**

## Considered Options

- **Pin the dir and use it as the address.** Rejected. A legitimate on-disk move
  would need a hand fix anyway, and it splits the address between config and
  bead.
- **Resolve live with no guard.** Rejected because of the silent fork.

## Consequences

- A Checkpoint landed in a repo that is later removed from config disappears
  from History. That is accepted.
- Grouping or dashboards per repo, beyond the merged list, are a later Signal
  if anyone asks.
