# ADR 0226: A gitlab Landing's MR iid and sub-state ride the Rollup as qualifiers

Status: Accepted; amends ADR 0198 (tui-v2-execute-checkpoints-section…) §2 and its Considered Options.
Date: 2026-10-16

Chart mc-t72m.19 (Execute rollup shows a gitlab Landing's Merge Request sub-state).
Settles grilling mc-t72m.19.5 after research mc-t72m.19.2
(`docs/research/gitlab-detailed-merge-status-to-mr-qualifier-words.md`).

ADR 0198 refused to show landing sub-phases (queued, attempting, gate running)
because they all mean "moving" and only `retrying` needs attention. That
argument doesn't hold under `land_strategy=gitlab`. There a Landing waits on an
open Merge Request for a human to merge it (ADR 0189
(under-gitlab-a-human-merges…)). So "awaiting merge", "needs approval" and the
like are the Landing's steady state, and each names a click that only a human
can make. That makes them worth a qualifier.

## Decision

1. **Two new qualifiers on `landing`**, after `stale pin` and before `retrying`,
   so the alarm stays last:
   `round N · re-review · !42 · awaiting merge · retrying`.
   - `!<iid>` shows whenever the Landing body has an `MR: !<iid>` line, read
     I/O-free. It shows even when the status is transient or unknown, because it
     is how the human finds the MR.
   - The sub-state word is the mapped `detailed_merge_status` (awaiting merge,
     pipeline running, needs rebase, needs approval, threads open, draft; see the
     research note). An unmapped or transient status shows no word. The word is
     display-only and nothing decides on it.
   - `retrying` still shows beside them. The two can coexist.
2. **`land failed` / `land paused`** show `!<iid>` but not the sub-state word.
   Jinbe may not be mirroring a paused Landing, so the word could be stale, and
   the MR is still open and waiting for a human.
3. **`d`'s why: line** adds the iid and GitLab's raw value, for example
   `Landing mc-x open · MR !42 detailed_merge_status=mergeable`. It adds
   `since <age>` only if the sub-state record already carries a timestamp. No
   record gains one just for this line. No MR URL: the TUI has none, and `!42`
   is enough to find the MR.
4. Only the Execute Checkpoints section shows these qualifiers. Chart Landings'
   sub-states are recorded but not shown (the Chart's Out-of-scope).
5. **An open, unpaused Landing shows its recorded word even if it may be
   stale.** That covers the loop being off or draining (the tab bar already
   says so), a Forge 429 streak, and Jinbe being down while the loop runs.
   Nothing marks or hides the word, and `d` stays as §3 has it. Settled by
   grilling mc-t72m.19.6.

## Considered Options

- **iid inside the word qualifier (`awaiting merge !42`).** Rejected. The iid
  would vanish whenever the status is transient or unknown.
- **iid only in `d`.** Rejected. The iid is what the human acts on.
- **Sub-state on `land failed`/`land paused` too.** Rejected because it might be
  stale. `d` still shows the raw value.
- **MR URL in `d`.** Rejected. It would need a new recorded field for no new
  information.
- **Hide or mark the word while the loop is off.** Rejected. The tab bar
  already shows `loop off`, and with nobody mirroring the MR it moves only on a
  human click, so the word usually stays true.
- **Hide the word while `land.rate_limited` is the Landing's latest event.**
  Rejected. Nothing is logged when a 429 streak ends, so the word would stay
  hidden after recovery.
- **A heartbeat or timestamp to detect a dead Jinbe.** Rejected. It would amend
  ADR 0227 (jinbe-records-a-gitlab-mr-s-sub-state…) §1 for a rare case.
