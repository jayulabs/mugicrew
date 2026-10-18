# ADR 0175: v2 Graph status defaults to unclosed, `h` is removed, and folded headers count live efforts

Status: Accepted
Amended by the Spec "G reveal scopes Graph to the revealed bead" (§3: a hidden-bead reveal now scopes Graph to the bead instead of widening or clearing).
Amended by the Spec "Graph search at the default status matches closed beads" (§1: words at the unclosed baseline match every status).
Date: 2026-10-14

Chart mc-lu2x.30 (v2 Graph defaults to unclosed, drops h, counts live efforts
on folded rows). Settles grilling mc-lu2x.30.2 and records the decisions
carried in the Chart's Notes. For v2 only, this supersedes ADR 0143 §3's
rejection of the `unclosed` default and its `any` default, ADR 0143 §8's `h`
clause, ADR 0074 §3's `h` flip, ADR 0058 §6's `h` key and ADR 0131 §4's `h`
clause. It keeps ADR 0038 §2. v1 (`tui/`) is frozen and keeps `h` and its
headers.

## Context

ADR 0143 turned down a default status of `unclosed` for Graph because it hides
the closed members of open Charts. That was a v1 habit: in v1, Graph was the
main working view, so the `h` (hide closed roots) toggle earned its place.
v2 has other tabs to work from, so that toggle is no longer needed, and
Graph's standard filters (status, kind, words) cover what is left. Folding
also hides sessions, so a folded group needs to show that something is
running inside it.

## Decision

1. **`unclosed` is the default status facet, and `h` is removed.** The kind,
   status and word filters are the only way to hide beads. `unclosed` is the
   baseline, not a query: the header line (ADR 0143 §5) shows only when the
   query differs from that default, and at the default the folds come from
   the seeded collapse state, not from the query's expand-all (ADR 0143 §4).
   `esc` (ADR 0143 §6) sets status back to `unclosed`, never to `any`. `any`
   is reached only through the `s` picker, and it then counts as a query: the
   header line shows and matching groups expand.
2. **A bead with a session always passes the status facet** (ADR 0038 §2).
   This holds for every session state, `exited` included. Words and kind
   still apply to it.
3. **`G` reveal onto a bead that the status facet hides takes two steps**,
   like the old `h` path. First status is set to `any`, words and kind are
   kept, and the status line reads `showing closed beads`. If the bead is
   still hidden, the whole query is cleared (ADR 0143 §8). Only the target's
   group unfolds (ADR 0074 §3).
4. **`N live` counts live efforts under a header.** It counts every
   descendant, at any depth, whose row would show a live `●`: an alive AFK
   effort, or an alive session (waiting and `done — /exit` included). Exited
   `✕` and dead `○` rows don't count, and neither does the header bead itself.
   The count is taken over the whole non-Draft graph and ignores the word and
   kind filters, as `closed/total` and `landed N/M` do.
5. **The trailer appears on every foldable header while it is folded and
   N > 0.** That covers Chart, Checkpoint and Initiative group headers, the
   chain heads nested under an Initiative, and the Unparented header
   (`▸ Unparented  2 live`). It is drawn in the live-effort colour (`aliveSty`
   green) and placed before the muted `closed/total`. On an Initiative it
   goes before `landed N/M`.
6. **Unparented opens folded.** The startup seed that folds closed-root
   groups stays, and the Unparented fold is added to it. The seed is set
   when the Graph is constructed, so it holds even when the first snapshot
   has no Unparented group. `space` unfolds it, and a refresh never folds it
   again.

## Considered Options

- **Keep `h` beside the `unclosed` default.** Rejected: that is two
  mechanisms for hiding the same closed beads.
- **Show `N live` on unfolded headers too.** Rejected: the rows already show
  their `●`.
- **Draw the count in the waiting amber.** Rejected: the count is about
  liveness, not about whose turn it is.
- **Have `esc` clear status to `any`.** Rejected: then `esc` could not get
  back to the bare Graph.
