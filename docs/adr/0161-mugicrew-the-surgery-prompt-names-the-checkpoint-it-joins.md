# ADR 0161: mugicrew the surgery prompt names the Checkpoint it joins, with its Spec, Surgeries, Builds and Critiques as context

Status: Accepted
Date: 2026-10-13

Amends: ADR 0160 ("an open Surgery holds its Checkpoint's Critiques") §3, by
widening what the surgery prompt lists. Builds on the Chart "Surgery on an
existing Checkpoint" and its assumption inventory (row P1,
`docs/research/one-surgery-owns-its-checkpoint-assumption-inventory.md`).
Decided in grilling.

## Context

`franky/prompts/surgery.md` tells the session it works "for its own
Checkpoint". A Surgery can now join an open Checkpoint that may have a Spec,
earlier Surgeries, open Builds and held Critiques. The git log filter (P2–P4)
and the absence of a pivot section were already settled. The open question was
what else the session is told, and how.

## Decision

1. **One template, neutral opener.** The opener names the Checkpoint
   (`for Checkpoint <cp> — <title>`) instead of "its own Checkpoint". Each
   context block below renders only when it is non-empty. A fresh-Checkpoint
   (`U`) Surgery therefore reads almost as it does today, and only the opener
   changes. At render time no one decides whether the Surgery created the
   Checkpoint.
2. **The Spec is background, not binding.** On a Spec'd Checkpoint the prompt
   names the Spec (ID and title, with a `bd show` pointer) and says that the
   human's dictation is the intent and Usopp reviews this Surgery's commits
   against the Surgery bead, not the Spec. If a dictated change visibly
   contradicts the Spec, the session says so once, then follows the human, in
   the same way as the "too big for a surgery" warning. It never blocks, and
   it never raises a pivot.
3. **Earlier Surgeries are listed.** The prompt lists every closed Surgery on
   the Checkpoint by ID and title, in filing order, including those abandoned
   with zero commits (matching the Approval packet, ADR 0162 §8). Each entry
   has a pointer to its session records (`bd comments`). Their commits are not
   listed.
4. **Open Builds are listed with a caution.** The prompt lists open Builds by
   ID and title and warns that they integrate onto the same branch, so the
   session's integrate may need a rebase. Other members (Review, Approval) are
   not listed. Held Critiques are listed as ADR 0160 §3 already says.
5. **Rendered at launch, re-read at Start.** The lists are rendered into the
   prompt from bd when the session launches, so the prompt stays a
   deterministic template. When the session starts, it re-reads
   `bd children <cp>` before telling the human where the surgery stands, so a
   resume after a long idle gap sees current state.

## Considered Options

- **Spec as binding intent.** Rejected: the session would enforce a rule
  that review does not, because Usopp checks Surgery commits against the
  Surgery bead.
- **Inline the Spec's body.** Rejected: `bd show` returns the same text, and
  inlining makes the prompt longer.
- **Two openers, chosen by whether the Surgery created the Checkpoint.**
  Rejected: it needs the "created it" check at render time, and that check is
  only cheap at End.
- **List every open member.** Rejected: a Review or Approval waiting for drain
  tells the session nothing it can act on.
- **Live read only, no rendered lists.** Rejected: the prompt could no longer
  be tested as a pure template.
