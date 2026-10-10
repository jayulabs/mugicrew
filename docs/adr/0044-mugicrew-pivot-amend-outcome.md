# ADR 0044: mugicrew pivot amend outcome

Status: Accepted
Date: 2026-10-04

Amends: ADR 0005 §3–4 (adds a fourth triage outcome; restates "closed record"),
ADR 0043 §4 and §7 (amend's refile and cleanup).

## Context

ADR 0005 gives Zoro three pivot outcomes: confirm, escalate, dismiss. Confirm
supersedes the Spec and discards the Checkpoint's in-flight work, which is wrong
when one FR's *mechanism* is unbuildable but its intent still holds. The
motivating case is a Spec's FR5: inline `-e KEY=VAL` hit tmux's message-size cap,
and the human recorded a new mechanism as a Spec comment on 2026-10-03. Dismiss
cannot carry that either. It says "the Spec was fine", and a decision recorded
only on a Critique is invisible to Usopp's Spec axis, which reads the Spec and so
re-files the stale FR (a loop of 8 identical Critiques). Decided in
grilling.

## Decision

1. **AMEND is a fourth triage outcome.** Zoro recommends an outcome with a
   reason, and the human picks it in the HITL Launch session.
2. **The line.** Amend only when the FR's **intent holds** *and* **no
   already-built work becomes wrong**. Otherwise use confirm, or escalate. The
   second clause is what makes resuming on the same branch safe.
3. **An amendment is a marked comment on the Spec bead.** Format:
   `AMENDMENT (<pivot-id>): <FR> — <decision> — rejected: <options>`. Zoro checks
   for an existing comment for that pivot before writing one (ADR 0003). The
   Spec *body* is never edited. Its effective content is the body plus its
   amendments, read in order. Zoro also writes an ADR only when the decision is
   architectural beyond this Spec.
4. **Amend reuses dismiss's mechanics plus the amendment.** Zoro files a Critique
   on the **same Checkpoint** that **copies** the unfinished FR and the decision,
   rather than pointing at the comment. The pivot then closes with reason
   `amended: <gist>`, which lifts the gate (ADR 0043 §7). Nothing is superseded.
5. **Readers.** Usopp's review prompt and Franky's Build/Critique prompts read the
   Spec's comments: an `AMENDMENT` overrides the FR it names, and the latest
   wins. `bd show` already prints comments, so this is a prompt change only.
6. **No cap.** Any number of amendments may accrue on one Spec. Each pivot is
   judged on its own against §2, and confirm is always available.

## Consequences

- ADR 0005's "the chain stays append-only" still holds: amendments are appended,
  never edited in. Its "closed record" now means the body is closed, not the
  whole bead.
- A one-FR mechanism change costs a comment and a Critique, not a Checkpoint.
- Usopp's re-file loop closes: the Spec he reads already carries the decision.
- Judging the §2 line is human work, not something the code can check.

## Alternatives considered

- **Dismiss absorbs it** (decision only in the Critique). Rejected: Usopp's
  Spec axis never sees it and files the stale FR again.
- **Confirm covers it.** Rejected: discards a whole Checkpoint for a mechanism
  swap.
- **Amendment as an ADR.** Rejected as the default: too heavy, and it lives
  apart from the Spec. It is kept as an optional extra.
- **Edit the Spec body.** Rejected: breaks append-only and loses what changed.
- **Find amendments via the Spec's `related` pivots.** Rejected: new Go plumbing
  for what a comment already gives.
- **Critique points at the comment.** Rejected: Franky would have to go find the
  decision somewhere else.
- **Cap amendments per Spec.** Rejected: an arbitrary count. §2 is the real test.
