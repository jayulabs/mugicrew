# ADR 0216: A failed Graph push warns and never holds the gitlab Landing

Status: Superseded by ADR 0300
Date: 2026-10-15

Chart mc-t72m.16 (A personal Bead Graph publishes to its per-username ref on the
Forge). Settles grilling mc-t72m.16.5. Under `land_strategy=gitlab` the mugicrew
binary pushes the Bead Graph to its Graph ref (`refs/mugicrew/graphs/<username>`)
automatically at each Landing's write leg (`landGitlabAt`: the branch force-push,
then find-or-create the MR, returning `*LandPending`). This ADR fixes what a
failed Graph push does to that Landing.

## Decision

1. **Warn, never hold.** A failed Graph push is recorded as an event and never
   returned as an error. The Landing's branch push and MR go ahead; the Landing
   is not held and no container is blocked.
2. **After the write leg.** The Graph push runs once the write leg has returned
   `LandPending` and `recordMR` has written `MR: !<iid>`, so the ref shows the
   Landing with its MR, and a failed branch push publishes nothing.
3. **On every write leg that returns `LandPending`**, whether or not that tick
   force-pushed the branch (`Pushed`). A Graph push that changes nothing is a
   no-op git push.
4. **No retry.** The write leg runs only until `MR:` is recorded, and Jinbe's
   pending ticks do not revisit the Graph push. Each Graph push carries the
   whole graph, so the next Landing's push, or the manual verb, catches up.
5. **A root-hash mismatch files one non-blocking Task.** When the Graph ref
   holds a different Graph id, the refusal (naming both ids) also files a HITL
   Task, owned by Brook, that names both graph ids and the fix. It is filed once
   (skipped while one is open) and blocks no container, so lands keep flowing.
6. **No glab credential cannot reach this path.** The Landing's own MR calls go
   through glab and fail first; the refusal pointing to `glab auth login`
   belongs to the manual verb.

## Considered Options

- **Hold the Landing on a failed Graph push** (retry both pushes next tick).
  Rejected: the land is the valuable, irreversible step and the ref is a
  read-back mirror; coupling them lets a side ref stop code from landing.
- **Hold only on a root-hash mismatch, on a blocking Task (ADR 0187 §3 shape).**
  Rejected: a mismatch is the human's to resolve, but nothing about the land
  depends on it, so the Task informs without blocking.
- **Graph push before the branch push.** Rejected: the ref would miss the
  Landing's `MR:` line, and a failed branch push would still have published.
- **Retry while the Landing is pending** (an in-memory owed flag). Rejected:
  extra state for a mirror nobody is blocked on; a full-graph push makes any
  later success a complete catch-up.

## Consequences

- A Graph ref can lag the graph by up to one Landing (or until a manual push)
  after a transient failure.
- Brook gains a non-blocking Task for a foreign Graph ref, beside the blocking
  ones of ADR 0185 §4–5 and ADR 0187 §3.
