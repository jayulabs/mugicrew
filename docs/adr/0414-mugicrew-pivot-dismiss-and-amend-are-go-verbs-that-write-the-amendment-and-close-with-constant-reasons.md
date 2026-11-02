# ADR 0414: mugicrew Pivot dismiss and amend are Go verbs that write the Amendment and close with constant reasons

Status: Accepted
Date: 2026-10-29
Amends: ADR 0044 (mugicrew-pivot-amend-outcome) §3–5 and its rejected "new Go plumbing" alternative; ADR 0046 (mugicrew-pivot-triage-session) §5, which now names four sweep verbs.

## Context

Confirm and escalate run through `mugicrew pivot confirm` and `pivot escalate`.
Dismiss and amend were left to the prompt: raw `bd create`, `bd comments add` and
`bd close` calls. So the `dismissed:` and `amended:` close reasons and the
`AMENDMENT (<pivot>):` comment format existed only in the Zoro prompt (bead
lifecycle Gaps entries 11 and 17). ADR 0044 kept the Amendment prompt-only and
rejected Go plumbing for it.

## Decision

1. **Two more sweep verbs.**
   `mugicrew pivot dismiss <pivot> --gist <g> [--critique <file>] [--rewrite <ask>=<file>]... --drafted-by <lead>`
   and
   `mugicrew pivot amend <pivot> --gist <g> --amendment "<FR> — <decision> — rejected: <options>" [--critique <file>] --drafted-by <lead>`.
   The triage session still decides with the human. The verbs do the writes, and
   each leg is guarded so that a re-run converges.
2. **Go writes the Amendment.** `pivot amend` appends `AMENDMENT (<pivot>): …` to
   the Spec, and to every Ask Critique of a Signal or Errand raiser. It skips
   any target that already has a comment with that prefix. The format lives in
   Go. The Spec body is still never edited, and the readers named in ADR 0044 §5
   are unchanged.
3. **The refiled Critique.** It is a Draft by the lead pivot (`--drafted-by`),
   because only the lead's Drafts publish at the session's clean exit. It is
   discovered-from the pivot, which is the re-run key, and its title is the
   first line of its body. `--critique` is required when the pivot has no Ask
   Critique and is refused when it has one: the Ask is the refile. On dismiss,
   each Ask named in `--rewrite` is rewritten and every other Ask closes
   `dismissed: <pivot>`.
4. **Raising Errands get a Go-written comment** naming the outcome, the gist and
   the Critique to wait for. If the Ask was closed instead, the comment says the
   step may be wrong. Errands are never closed.
5. **Every pivot close reason is a `bd` constant**, beside `SupersededReason`:
   `confirmed`, `escalated`, `dismissed: <gist>`, `amended: <gist>`, the sibling
   `superseded: <pivot>`, and the Ask Critique's `dismissed: <pivot>`. They
   follow ADR 0396 (mugicrew-the-hold-clearing-step-reads…) §5. `luffy`'s
   reapproval check reads the shared constant. The reason stays `dismissed`, not
   `rejected`, which ADR 0399 (mugicrew-every-spine-close-names-its-outcome…)
   uses for spine rejects. Pivots are not spine kinds, so the detail is the
   gist, not a successor id.
6. **Guard.** Both verbs refuse when the pivot's Checkpoint is already closed,
   because a confirm or escalate sweep has run. The rule against mixing outcomes
   in one triage stays in the prompt, since Go cannot see the decision before
   the sweep runs. Amend's optional architectural ADR stays prompt-only.

## Considered Options

- **Document the reasons, keep the prompt doing the writes**: the format and the
  reasons would stay unchecked text, and the Initiative asks for code doors.
- **Infer the lead pivot** (the claimed one) instead of `--drafted-by`: fragile
  if the claim is lost, and `errand new` and `latch` already take `--drafted-by`.
- **Publish the refiled Critique at once**: it would skip the session's atomic
  publish at clean exit.
- **Successor-shaped detail** (`dismissed: <critique id>`): the Critique is
  already linked by its discovered-from edge, and the gist is what a reader
  needs.
