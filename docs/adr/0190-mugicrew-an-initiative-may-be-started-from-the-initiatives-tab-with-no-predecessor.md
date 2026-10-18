# ADR 0190: An Initiative may be started from the Initiatives tab, with no predecessor

Status: Accepted
Amended by ADR 0241 (§2–3: Initiatives' n is the shell menu's n i; the empty line reads n i).
Date: 2026-10-15

Amends ADR 0128 §3, ADR 0132 §1–§2 and ADR 0181 §2, §5–§6. Spec mc-h1jd.

Initiatives were born only at triage, from a Signal. A human can now press `n`
on the v2 Initiatives tab (from any row, either section header, or the empty
tab) and file one directly, with the Signal form's fields and keys.

## Decision

1. **Birth.** `bd.NewInitiative(title, body)` creates an open, unparented
   `kind:initiative` epic: no caused-by predecessor, no Draft stage, no
   throwaway Signal, no `drafted-by`, no members. The seed text is the body
   verbatim; priority is the form's follow-up update, as for a Signal. Luffy
   remains the body's only writer, and settles it in the first cut session
   (ADR 0182). It logs `initiative.new` (ADR 0029).
2. **One form.** The Signal form carries its create func and noun, so `n` on
   the Initiatives tab opens "New Initiative" and every message says
   Initiative; empty-title refusal, keep-form-on-failure and the priority
   two-step are unchanged.
3. **The tab.** `n` works with zero rows. Row hints read `enter focus · d
   detail · n new · ? keys`, header hints `space fold · n new · ? keys`, and the
   empty line `no Initiatives yet — n to start one`. Every other key stays
   unbound (ADR 0181 §5). The new row appears in its `beadCmp` place and the
   cursor does not move (ADR 0181 §9).

## Considered Options

- A member Signal "draft the body for X": a Signal means work that may change
  code, would be triaged like a phase and make the Initiative look under way.
- A triage-routed Signal leaning toward Initiative: saves little over filing a
  Signal on Ideate today.
- A `mugicrew initiative new` CLI verb: the TUI calls bd in-process.
- A **seeded** (or any fourth) lifecycle state: every `InitiativeDrained`
  caller would have to learn it, and it mixes the body axis with the members
  axis (ADR 0182). Bodyless stays a render-time qualifier.
- Keeping the seed verbatim as a Notes line or a `## Seed` section: breaks the
  three-section body (ADR 0182).
- A line-edit verb for the body (`initiative append|drop`): the body has one
  automated writer, so whole-description writes are enough (ADR 0182).
- Editing a TUI-born Initiative's title or seed from the tab: Graph already
  edits beads and Luffy rewrites the body.
- A bodyless cue in the v1 TUI: v1 gets the branch through the shared Launch,
  but its views are not being extended.
- A line-anchored `## Done when` heading match: `bd.HasDoneWhen` stays the
  single substring parse, accepted as the same parse as `doneWhen`.
