# ADR 0186: GitLab auto-merge is re-armed with sha and squash message, and Jinbe maps MR state to a Landing outcome

Status: Accepted (amended by ADR 0189)
Date: 2026-10-14

Chart mc-t72m.2 (GitLab Merge Request land strategy for a single-repo
Workspace). Settles prototype mc-t72m.2.4: a live round trip on gitlab.com (Free
plan, glab 1.107, `squash_option=default_on`, pipeline must succeed), under both
the `merge` and `ff` merge methods. The throwaway is on
`refs/mugicrew/prototypes/mc-t72m.2`. It confirms option (b), GitLab squashes,
and corrects facts ADR 0185 assumed.

## Decision

1. **Option (b) holds.** Every arm (`PUT …/merge`, `auto_merge=true`,
   `squash=true`, `sha=<pin>`) sends `squash_commit_message` = the
   `land <branch>` / `Tip:` / trailer body. Every path we tried landed the
   trailers as whole lines, and `git log main -E --grep='^<trailer>$'` hit after
   a fetch: the happy path, a re-push, a resolved conflict, a pipeline retry, and
   a server-side rebase. A template of `%{title}\n\n%{description}` alone also
   kept them, but the description can be edited on the Forge, so the explicit
   message is the guarantee and the template is only a backstop. Post-merge
   LandedBase stays the check.
2. **GitLab disarms auto-merge on any change to the source branch.** A
   force-push writes the system note "aborted the automatic merge because the
   source branch was updated". So the force-push in ADR 0185 §1 must always be
   followed by a re-arm at the new sha. Arming with a stale sha returns `409`.
   Arming before any pipeline exists is accepted.
3. **Under `ff`, any push to main disarms every waiting MR**, even one with no
   conflict ("target branch was updated", `detailed_merge_status=need_rebase`).
   Under `merge`, a conflicting push leaves the MR armed at `conflict` forever,
   and a non-conflicting one doesn't touch it. Merge trains (not tested, Premium)
   should not disarm on a main push unless they detect a rebase conflict. How
   Jinbe treats `need_rebase` is still open.
4. **A failed pipeline leaves the MR armed.** The MR reads
   `ci_must_pass` with `head_pipeline.status=failed`. Retrying the pipeline merges
   it with the stored squash message and no re-arm, so Brook's retry of a flake
   needs nothing from Jinbe.
5. **Jinbe classifies the MR in this order:** `merged` → landed (fetch, then
   LandedBase); `closed` → ADR 0185 §4; `has_conflicts` → conflict path;
   `head_pipeline` failed → pipeline-failed (Brook); not armed → disarmed (re-arm
   at the current sha if the pin is unchanged, else ADR 0185 obsolete); otherwise
   pending. `detailed_merge_status` is computed asynchronously and lags: just
   after a push it read `unchecked`, or a stale `conflict` with
   `has_conflicts=false`. So conflict keys on `has_conflicts`, and unknown or
   checking statuses read as pending.
6. **Cancel is idempotent by body, not by status.** A
   `cancel_merge_when_pipeline_succeeds` on an MR with nothing armed returned
   status line 201 with the body `{"http_status":406,"message":"Can't cancel the
   automatic merge"}`. ADR 0185 §2's disarm treats that as already disarmed.
   Cancel followed by a re-arm with a sha works.

## Consequences

- ADR 0185 §1: a re-arm after every force-push is required, not optional.
  ADR 0185 §2: the disarm is often a no-op, because GitLab has already disarmed.
- The research doc's open question "lands the trailer byte-exact" is closed for
  non-train merges. The train facts there stay unverified.
