# ADR 0310: mugicrew withdraw drops any unclosed chain, from the TUI's `X`, and cleans up its trees and branches

Status: Proposed
Amended by: ADR 0342 (mugicrew-withdraw-takes-a-task…) §1–§2.
Date: 2026-10-19

Chart mc-1k3gq (Withdraw or cancel any unclosed chain from the TUI, with
cleanup). Extends ADR 0133 (initiative-lifecycle-drain-cancel-withdraw) §8–§9
rather than superseding it: cancel's and withdraw's refusals while an effort is
in flight, and "landed work stays", are unchanged.

ADR 0133 scoped withdraw to Initiative members, left both verbs CLI-only, and
left every worktree and branch of the closed beads behind. A human who wants to
drop an unfinished chain outside an Initiative had only a bare `bd close`, which
spawns nothing but also cleans nothing.

## Decision

1. **Withdraw takes any unclosed chain head.** Inside an Initiative or not, it
   closes the head and every unclosed bead under it, reason `withdrawn`, no
   successor. Cancel stays the name for dropping a whole Initiative.
2. **The ordering refusal widens with it.** Withdraw refuses while any unclosed
   bead outside the chain, anywhere on the graph, is ordered after a bead in
   it, for ADR 0133 §9's reason: bd would read the closed blocker as satisfied.
3. **An open Landing refuses too.** A pending Landing and its Merge Request are
   not an in-flight effort, but deleting the container branch under an open MR
   leaves the Forge able to merge work the graph has dropped. Withdraw and
   cancel refuse, naming the Landing, and never write to the Forge.
4. **One TUI key, `X`.** On a chain head row it withdraws; on an Initiative row
   it cancels. It is offered on head rows only, on every tab `x` reaches. `x`
   keeps finish, drop, abort and recover, so no press of `x` can close a chain.
5. **Cleanup discards, after naming what it discards.** Every closed bead's
   effort worktrees and `effort/*` branches are removed, and the container's
   branch when the container closes, including uncommitted changes and
   unintegrated commits. The confirm dialog lists those first. A foreign hold
   (ADR 0054) is skipped and reported, never removed, and does not stop the
   close. Any other cleanup failure is reported, not retried.
6. **Cancel lets the human keep chains.** Its dialog lists the member chains,
   each defaulting to close; any may be toggled to release, except where
   Require Initiatives refuses release, shown locked with the reason.
7. **The verbs own the cleanup.** `mugicrew withdraw <head>` is the general
   verb (`mugicrew initiative withdraw` stays as its alias); both it and
   `mugicrew initiative cancel` clean up, and the TUI calls the same verbs.

## Considered options

- **Folding withdraw and cancel into `x`.** Rejected: `x` on a head would be
  one press from dropping a session to closing the chain.
- **Refusing while any leftover tree or branch holds work.** Rejected: every
  withdraw of a chain with a failed effort would wait on manual recovery; the
  dialog naming the loss is the safeguard.
- **Closing the Merge Request as part of cleanup.** Rejected: an outward act
  behind the human's back, as ADR 0133 rejected cancel aborting efforts.
