# ADR 0188: Jinbe watches the Merge Request pipeline, retries it once, and files a real failure

Status: Accepted
Date: 2026-10-15

Chart mc-t72m.2 (GitLab Merge Request land strategy for a single-repo
Workspace). Settles grilling mc-t72m.2.6. The Survey planned for Brook to watch
MR pipelines as new unattended automation, amending ADR 0115. This ADR reverses
that plan. ADR 0186 §5 already has Jinbe classify the MR, `head_pipeline` included,
on every pending-Landing tick, and GitLab gives no flaky-job flag
(docs/research/gitlab-merge-request-mechanics-through-glab.md §7).

## Decision

1. **Jinbe owns the pipeline watch, not Brook.** Forge state that is part of a
   land (MR state, merge, conflict, pipeline) is mirrored by Jinbe inside his
   Landing. Brook owns external state only as a standalone gate (a future kind of
   his), plus `task`. ADR 0115 is unchanged. In ADR 0186 §5, the
   "pipeline-failed (Brook)" outcome becomes Jinbe's.
2. **One retry per pipeline, no knob.** On `head_pipeline` failed, Jinbe retries
   it once (`POST …/pipelines/:id/retry`, which re-runs only the failed jobs) and
   records `Retried: pipeline <id>` on the Landing body. The Landing stays
   pending, and the MR stays armed, so a passing retry merges it with no re-arm
   (ADR 0186 §4). Jinbe never compares with main's pipeline. A project-side
   `retry:` stacks under ours, and `allow_failure` jobs never fail the pipeline.
3. **A failure after the retry is real unless it is infra.** If a failed job's
   `failure_reason` is an infra one (`runner_system_failure`, `stuck_*`,
   `stuck_or_timeout_failure`, …), the Landing stays pending. Jinbe files a HITL
   Task (Brook's) blocking the container, keyed by the pipeline id, so the loop
   stops retrying and a human fixes the runner. This mirrors ADR 0152 §1.
4. **A real failure on a Checkpoint** works like ADR 0152. Jinbe files a Critique
   that is a Checkpoint member, discovered-from the Landing, and idempotent on
   (Tip, pipeline id). He closes the Landing `obsolete: pipeline failed` and
   disarms per ADR 0185 §2, and the MR stays open. The Critique carries `Tip:`. It
   carries `Base:` only when the pipeline was a merged-results pipeline (its
   target sha). Franky fixes it, the tip moves, and a fresh Review, Approval and
   Landing follow.
5. **A real failure on a Chart** closes the Landing `obsolete: pipeline failed`,
   and Jinbe files a HITL Task blocking the Chart. A docs-only branch that fails
   CI points at main or the CI config, which is external-world work, not a
   `chart-conflict`.
6. **The log is never stored raw.** The Critique or Task carries the pipeline URL
   and, for each failing job: its name, web URL, `failure_reason`, and the last 80
   lines of its trace (`GET …/jobs/:id/trace`, ANSI stripped). The 80-line tail
   matches ADR 0152 §3.

## Considered Options

- **A Brook step that Jinbe calls, or a Brook-owned failure kind.** Rejected.
  Either adds a hop and a second crewmate with views on one Landing, for an MR
  Jinbe already polls. Brook's indirection pays only where he owns a gate kind.
- **N retries, or comparing with main's pipeline.** Rejected. One retry is
  enough to catch a flake. Main may not run the same jobs, and a broken main
  blocks the land anyway (ADR 0152 §7).
- **A link-only log.** Rejected. Franky starts from the tail without network
  access, and the link gives the rest.

## Consequences

- Train pipelines can't be retried (research §6), so under merge trains a flake
  can be absorbed only by the project's `retry:`. Trains are untested (Premium).
- The glossary's Brook line loses the MR-state example; Jinbe's gains the
  pipeline watch.
