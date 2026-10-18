# ADR 0184: v2 Recents is an R modal of the last 20 beads acted on, kept per Workspace

Status: Accepted
Date: 2026-10-15

Chart mc-kmbi (v2 TUI Recents). This ADR records the calls settled in the
Chart's survey and in grilling mc-kmbi.3. ADR 0183 covers where enter on a row
goes. Recents gives the human quick access to the beads they were just working
on, without hunting for them across tabs.

## Decision

1. **Interaction.** An interaction is an *act* on a bead in the v2 TUI, not a
   keypress. It is either a bead verb that fires (enter/b, x, p, z, e/E, +/-,
   a/A, the n/u creates, closing an Initiative from the focus view), or opening
   the bead's detail drawer (ADR 0171). A key bound to something else, such as
   a modal's `d` for drop, does not count. These do not count either:
   - a refused verb, because nothing fired;
   - a cursor move;
   - the Recents jump itself, because a jump is a visit (ADR 0183);
   - activity outside the TUI: bd CLI, crew sessions, the loop.
2. **Key and form.** Shell-wide `R` opens Recents as a centred modal list, from
   any tab and from the Initiative focus view (ADR 0173). It uses picker chrome:
   j/k move, enter picks, esc or `R` closes. No shell key fires while it is
   open. It is not the per-tab detail drawer of ADR 0171.
3. **List.** Recents holds at most 20 beads, most recent first, one row per
   bead. A repeat interaction moves the bead to the top. Closed beads stay and
   are shown dimmed.
4. **Persistence.** Recents is kept per Workspace in
   `<git-common-dir>/mugicrew/recents.json`, following the ADR 0176/0179
   precedent. ADR 0077's process-only search query binds v1 only. Every
   interaction saves as a read-modify-write: re-read the file, move the bead to
   the top, prune, cap at 20, then write atomically. Two TUIs on one Workspace
   therefore merge rather than overwrite each other. A missing or unparsable
   file reads as empty.
5. **Pruning.** A save drops beads that are gone, meaning ids that no longer
   resolve in the TUI's loaded graph snapshot. A bead that still exists but is
   unlisted stays, and ADR 0183's scoped Graph fallback reaches it. Without a
   successfully loaded snapshot (none yet, or the last bd read failed), a save
   prunes nothing.

## Considered Options

- **Last writer wins, like folds.json.** Rejected. A second TUI would silently
  erase the first one's recent beads. Folds are per-container state and can
  tolerate that; a list of "what I touched" cannot.
- **Count the d key, or the Recents jump.** Rejected. `d` means close or drop
  in some views, and counting the jump would reorder the list while the human
  hops through it.
- **Process-only list (ADR 0077).** Rejected. Recents is most useful across
  TUI restarts.
