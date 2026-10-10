# ADR 0107: TUI in-flight bead actions show on the row, and launches run in parallel

Status: Accepted
Date: 2026-10-09

Supersedes ADR 0098 §5 ("Pending UX is the status line … with no row badge")
and ADR 0100 §4 ("One start in flight"). Amends ADR 0098 §1/§3 to add abort to
the per-bead guard. Decided in grilling, with input from research.

## Context

Only launch shows on the bead row while it runs (`launching…`). Every other
bead action shows only on the status line, which the next status write erases,
so a several-second write under Dolt contention reads as a TUI that ignored the
key. Abort skips the pending guard entirely. Launch is global: one start or
resume at a time, so starting several HITL sessions means waiting on each one.

## Decision

1. **Every pending bead write shows on its row** with its own verb, the one
   `startWrite` already gets: `pausing…`, `dispatching…`, `aborting…`,
   `dropping…`, `rejecting…`, `updating…`. Run-now's `dispatching…` covers only
   the claim phase. Once the claim lands, the ◐, the age and the ● liveness dot
   say the effort is running, and the minutes-long dispatch gets no word.
   Create has no row, so its feedback stays in the form head.
2. **Placement.** The word is a trailing suffix in `rowSuffix`, styled like
   `launching…`. It replaces the done, waiting and idle text, and keeps the
   age. The status glyph and the liveness dot keep their meanings and change
   only after the reload.
3. **Failure.** The word clears and the error goes to the status line, as
   today. The row keeps no failure mark, because a mark would need its own rule
   for clearing it.
4. **Second press refused, abort included.** ADR 0098 §3's per-bead refusal
   stands. Abort now goes through `startWrite(id, "aborting")`, so a second
   `x`/`y`, or `p`/`+`/`e`, on an aborting bead is refused. The footer is
   unchanged: the refusal message is the feedback.
5. **Launches run in parallel.** The in-flight launch becomes a per-bead set,
   kept apart from `m.pending`, because a launch and a write on one bead are
   different guards. A second `l`/`b` on the *same* launching bead is refused,
   and any other bead is free. `onBusy` never reads a branch held by an
   in-flight launch as an orphaned worktree, so the orphan-drop confirm cannot
   offer to delete a tree that is still being built.
6. **At most one pending attach.** `l` means "start, then attach when it's
   ready". Starting is the same as for `b`, so the thing that is one at a time
   is the *pending attach*, not the launch. The latest `l` wins the slot, and an
   instant attach to an alive session also clears it. A displaced launch lands
   as a `b` would (`started <title> — l attaches`), and the status line names
   the downgrade. As today, a landing attach never interrupts an open edit or
   confirm.
7. **The row reads `launching…`** for every launch, whether it will attach or
   not. The pending attach belongs to this terminal, not to the bead, so only
   the status line names it (`launching <title>… (attaches when ready)`).

## Considered Options

- **One generic marker** (`working…` or a spinner) instead of per-action verbs:
  rejected. The verb is already at hand, and the row then says what is
  happening without the status line.
- **Replacing the glyph or the dot** while an action is in flight: rejected. Both
  carry state that the in-flight word would hide.
- **Keeping one launch at a time:** rejected. Its stated risk (a stray `l` on a
  stale session table) is the same-bead case, which the per-bead refusal closes.
  The real cross-bead hazard is the orphan misread, and §5 fixes it.
- **Refusing a second `l`, or letting the first one win:** rejected in favour
  of the latest press winning. It never blocks, and a stale attach can't pull
  the human away later.
