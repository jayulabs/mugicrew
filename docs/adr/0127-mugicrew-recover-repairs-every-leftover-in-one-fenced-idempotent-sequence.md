# ADR 0127: mugicrew recover repairs a stranded bead's leftovers in one fenced, idempotent sequence, and a cwd in the tree reads alive

Status: Accepted
Date: 2026-10-11

Amends: ADR 0126 §5–6 (what reads alive and what reads ambiguous), ADR 0059 §6
(who deletes a crashed creator's Drafts), ADR 0082 §4 (who lifts
`paused:abandoned`).

## Context

ADR 0126 settles Chopper's shape: `mugicrew recover [<bead>]` and a TUI key,
always human-triggered, refusing a live holder, behind a pause fence. It left open
what repair does to each leftover and what the human sees. Each repair primitive
is blind (docs/research/dead-effort-repair-primitives-and-race-windows.md). The
argv probe misses live holders that are not named `mugicrew` or `claude`, such as
an npm `node …/claude` or a tool grandchild
(docs/research/dead-effort-holder-liveness-for-claims-trees-and-drafts.md §7).
Decided on grilling bead mc-yr7o.4 (Chart mc-yr7o).

## Decision

1. **One sequence, every step idempotent.** `recover <bead>` does the following,
   in order:
   1. check liveness;
   2. fence with `paused:recovering`;
   3. **re-check liveness**;
   4. abort a stopped rebase;
   5. drop the tree;
   6. delete the Drafts;
   7. unclaim;
   8. lift the pauses.

   A step with no leftover is a no-op, so a re-run after a partial failure rolls
   forward. There are no per-leftover flags. The probe excludes recover's own
   pid, since its argv carries the id.
2. **Verdicts.**

   | Holder | Verdict |
   |---|---|
   | AFK, probe alive, **or any process whose `/proc/<pid>/cwd` lies in the bead's tree** | alive: refuse, naming `mugicrew abort` |
   | AFK worktree/detached route, probe dead, `paused:abandoned` | dead |
   | AFK in-process route (Spec, Review, Rollback), probe dead, `paused:abandoned` | ambiguous, always `--force`: a hostless claude or its orphaned tools may still be writing |
   | AFK, probe dead, not abandoned | ambiguous |
   | HITL session Alive or Closing | alive: refuse |
   | HITL session Exited (resumable, ADR 0037) | refuse, naming the TUI drop: recover never discards a resumable session |
   | HITL session gone, and the table read succeeded | dead |
   | tmux table read error | refuse: an error is never read as gone |

   The not-abandoned ambiguous row counts as dead only when the loop lock is
   free and no TUI is alive (ADR 0126 §6).
   The abandoned in-process row never resolves that way. After a force stop, a
   hostless npm claude (argv wiped, cwd the repo root) or a tool grandchild
   orphaned by a SIGKILLed claude passes both checks while still writing
   (docs/research/live-holders-that-read-dead-under-recover-verdict-rule.md §7
   rows 2 and 6). So it always needs `--force`. Its refusal says a hostless
   claude or its tools may still be writing and to check `pgrep -a claude` and
   the human's own shells first. A TUI calling recover always excludes its own
   session (a run-now effort is a `mugicrew run <id>` host, found by `Probe` and
   the flock, ADR 0330). Otherwise the human needs `--force`, which is
   refused while the loop is on. Liveness covers **this host only**.
   Multi-host recovery is out of scope.
3. **Mid-rebase tree.** Recover finds it through
   `.git/worktrees/*/rebase-merge/head-name` naming `refs/heads/effort/<id>`, then
   runs `git rebase --abort`. That is what a live backstop would have done.
4. **Dirty tree.** Refused, naming the path, unless `--discard` is given. The TUI
   asks a second confirm that states the file count. Recover never commits WIP
   onto `effort/<id>`, because a later integrate would squash it onto the chart
   branch.
5. **Drafts are always deleted.** The dead run's work stays on `effort/<id>`, and a
   re-dispatch rolls forward and drafts again. A HITL creator's Drafts are never
   reached, because an Exited session is refused.
6. **Pauses.** The fence uses a new cause, `paused:recovering`. If recover dies
   partway, the label it leaves tells the human to re-run. At the end recover
   lifts `recovering`, `abandoned` and `foreign-hold`. It lifts `failing` only
   when it aborted a rebase, because that stranding caused the failures. It
   leaves `manual` and `integrate-conflict`, and names them in its output.
7. **Unowned trees.** Recover only lists a detached land tree that no bead can be
   linked to. It never repairs one.
8. **The listing.** `mugicrew recover` with no argument is a pure view. It shows
   one row per candidate, giving its verdict, its leftovers and the way out for a
   refused row. Candidates are:
   - `in_progress` beads;
   - beads paused `abandoned` or `recovering`;
   - beads whose `effort/<id>` tree has no live holder;
   - mid-rebase trees.

   It also lists unowned trees under their own heading.
9. **Output and exit codes.** `recover <bead>` prints one line per step that
   acted, then any pause left on the bead. It exits with:
   - 0: repaired, or nothing to recover;
   - 1: refused, with the way out in the message;
   - 2: a step failed, `paused:recovering` stays, and the message says to re-run.
10. **Events.** Detection logs nothing, because the listing is a view (ADR 0033).
    A repair logs one of:
    - `recover.done`: bead, verdict (`dead`, `ambiguous-resolved` or `forced`),
      rebase aborted, tree path, discarded, Draft ids, unclaimed, lifted causes,
      remaining pauses;
    - `recover.refused`: bead, reason;
    - `recover.failed`: bead, step, error.

    No per-step `draft.deleted` or `claim.released` events, and no correlation
    with `effort.abandoned`: `L` scoped to the bead shows both.
11. **TUI.** On a row whose AFK claim reads dead (◐○), or whose HITL session is
    gone, `x` opens the recover confirm. The confirm states the verdict and the
    leftovers. It takes the inventory at key press, not at refresh, and recover
    re-checks liveness at `y`. The hint is `x recover`. There is no new row
    colour. The TUI never forces: an ambiguous row's hint names
    `mugicrew recover --force` and the loop. `x` already chooses between drop and
    abort by liveness, so abort and recover stay separate jobs (ADR 0126 §8).

## Considered Options

- **Per-leftover subcommands.** Rejected: the order is fixed, so the human
  would only be able to run it out of order.
- **Rescue a dirty tree as a WIP commit.** Rejected: the junk reaches the
  chart branch at the next integrate. Whether the work is worth keeping is the
  human's call.
- **Refuse a mid-rebase tree.** Rejected: the live backstop already aborts in
  this situation, and refusing leaves the CutFailed loop deleting Drafts every
  tick.
- **Treat an abandoned in-process bead as dead.** Rejected: a hostless claude
  installed with npm is invisible to the probe and has no tree for the cwd check.
- **Correlate with `effort.abandoned`.** Rejected: it would mean reading history
  for a decision (ADR 0033).

- **A `comm` check for the in-process row**: alive while any same-uid process
  with `comm == "claude"` has its cwd at the repo root. Rejected: it misses an
  orphaned tool grandchild (row 6), and the human's own claude in the repo root
  blocks it anyway, so `--force` is the way out either way.
- **Accept the in-process force-stop hole without `--force`.** Rejected: recover
  would delete Drafts and unclaim under a live writer. Decided on grilling bead
  mc-yr7o.6.

## Consequences

The verdict rule rests on unverified readings: npm claude, an in-process force
stop, tool grandchildren, the cwd probe on WSL, and the "no TUI alive" check,
which does not exist yet. A research bead on Chart mc-yr7o verifies these before
the Spec.

That research ran (mc-yr7o.5). The Spec names these as known risks, not
settled before it:

- On the worktree route, after the host dies, a grandchild running with its cwd
  outside the tree, or work handed to another uid (docker, the nix daemon),
  reads dead (rows 7 and 11, reasoned, not observed).
- Whether a newer npm claude that accepts `--name` still wipes its argv. Only
  the AFK-child row depends on it, and the cwd and lock checks cover that row.
- Whether claude kills its background tools on exit. On the worktree route their
  cwd is the tree.
- Windows-side access to a tree through `\\wsl$` has no `/proc` entry.
