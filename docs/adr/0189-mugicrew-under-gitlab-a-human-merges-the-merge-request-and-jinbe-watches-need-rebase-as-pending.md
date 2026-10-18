# ADR 0189: Under gitlab a human merges the Merge Request, and Jinbe reads need_rebase as pending

Status: Accepted
Date: 2026-10-15

Chart mc-t72m.2 (GitLab Merge Request land strategy for a single-repo
Workspace). Settles grilling mc-t72m.2.8. Under `merge_method=ff` (and
semi-linear), any push to main disarms every waiting MR and leaves it at
`detailed_merge_status=need_rebase` (ADR 0186 §3). Rather than have Jinbe rebase
and re-arm every MR on every main push, mugicrew stops arming auto-merge. This
amends the Survey Note "mugicrew sets auto-merge", ADR 0185 §1–2 and §7, and
ADR 0186 §1–2 and §5.

## Decision

1. **mugicrew never arms auto-merge and never merges.** A Landing force-pushes
   its `Commit:` pin, opens or reuses the container's MR (ADR 0185 §1, minus the
   arm), and carries the trailer block (`Tip:` / `<Kind>: <id>` / `Graph:`,
   ADR 0187) in the MR description. A human merges on the Forge, or arms
   auto-merge there. The Approval authorizes opening the MR, and the merge is the
   human's act. Chart MRs work the same way: a human merges them too.
2. **The trailer rides the description.** No arm call means no stored
   `squash_commit_message`, so the trailer reaches mainline through the project
   template's `%{description}`. On each pending tick, Jinbe rewrites the
   description if the trailer block is missing. The template refusal (Survey) and
   ADR 0185 §5's Task on a trailerless merge are the backstops. LandedBase on the
   fetched mainline is still the landed fact.
3. **`need_rebase` is pending.** Jinbe keys on the MR's state, not on
   `merge_method`, so `ff` and semi-linear are treated alike. He never calls
   `…/rebase`: the human merging the MR clicks Rebase. Because `Tip:` comes from
   the description, a Forge-side rebase still lands `Tip:` = the approved pin
   (ADR 0185 §6 compares against it unchanged).
4. **Conflicts are detected locally, too.** When the fetched mainline has moved
   since the Landing last checked, Jinbe runs a `Compose` trial (ADR 0150:
   ADR-number clash, then rebase onto the base) against it. A `*RebaseConflict`
   or `*ADRClash`, or the Forge's `has_conflicts`, or a `merge_error` left by a
   failed rebase, closes the Landing `obsolete: conflict`. Jinbe files the
   container's conflict at the fetched base (ADR 0145 §2), and the MR stays open
   with nothing to disarm.
5. **Jinbe's order** (replacing ADR 0186 §5): `merged` → landed (fetch, then
   LandedBase); `closed` → ADR 0185 §4; conflict (§4) → conflict path;
   `head_pipeline` failed → pipeline-failed (Jinbe, ADR 0188 §1); otherwise pending, including
   `need_rebase` and any unknown or checking status. The disarmed/re-arm state is
   gone.

## Considered Options

- **Jinbe rebases on the server and re-arms.** Rejected along with auto-merge.
  It sets off a rebase and a pipeline for every waiting MR on every main push.
- **`need_rebase` as a conflict.** Rejected. Every concurrent land would make
  every pending Landing obsolete and force a re-Review with nothing to resolve.
- **Refusing `ff` without merge trains, or refusing `ff` outright.** Rejected.
  It rules out a common GitLab setup, and once a human merges there is nothing to
  guard against.
- **Forge flags only for conflicts.** Rejected. GitLab can't see an ADR-number
  clash (two branches each adding ADR N merge cleanly), and a rebase-only
  conflict on a multi-commit branch shows up only after a human's Rebase click
  fails. A clean `Compose` trial costs no Review, so detecting conflicts locally
  adds no review churn beyond the conflicts that are really there.

## Consequences

- Every Checkpoint and Chart land waits on a human click in GitLab. That is one
  extra human step per land, unless the human arms auto-merge there.
- Jinbe's retry of a flaky pipeline (ADR 0188 §1) no longer merges anything by itself (ADR
  0186 §4). The human, or the auto-merge they set, still merges the MR.
- A stale-pin merge (ADR 0185 §6) is still possible when a human merges before
  the next Landing force-pushes, and it is handled the same way.
- The Compose trial runs only while mirroring a recorded MR,
  so a Landing's first tick pushes and opens the MR with no trial. A conflict is
  still caught on the next tick, before any human merge is expected.
- A pending tick sees a push to the Forge branch only as a Forge rebase; it does
  not compare the MR's head sha with the pin. A human who merges commits pushed
  there lands them under `Tip: <pin>`, and the stale-pin check (against the local
  branch head) says nothing. Decoding `sha` to flag a head that is neither the pin
  nor a rebase of it is future work.
