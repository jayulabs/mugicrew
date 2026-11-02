# ADR 0415: mugicrew Review, Conflict and Critique completed closes name their outcome

Status: Accepted
Date: 2026-10-29
Amends: ADR 0339 (mugicrew-a-close-out-reply-reads-its-verdict…) §1 and §2. Follows ADR 0396 (mugicrew-the-hold-clearing-step-reads…) §5 and ADR 0399 (mugicrew-every-spine-close-names-its-outcome…).

## Context

Chart mc-3cnhf.54, settled by its Survey mc-3cnhf.54.1. Three completed closes
write an empty reason, which bd records as `Closed` (bead lifecycle Gaps entry
15). These are a Review closed by Usopp's session tail, a Conflict that Franky
resolved, and a Critique that Franky fixed with no `Rationale:` comment. Every
other close of these kinds already names its outcome. ADR 0339 §2 says
`finish` never claims Accepted on its own, because it cannot prove a change
was made. ADR 0339 §1 keeps an unknown prefix verbatim as the rationale, so
any new word on a Critique would show up in Jinbe's reply as its rationale.

## Decision

1. **A Review closes with one of three outcomes**, all written by
   `usopp.approveIfClean`:
   - `clean: <approval id>`: no open Critiques and no open pivots. The id is
     the Approval this pass spawned, or the open Approval at head that it
     reused.
   - `critiqued: <critique ids>`: the pass left open Critiques on the
     Checkpoint. The ids are those Critiques, comma-joined, read from the
     Checkpoint's open `kind:critique` children that Go already counts.
   - `held: pivot <pivot ids>`: the pass left no Critiques but a pivot is
     open on the Checkpoint, so no Approval was spawned. The ids are
     comma-joined.
2. **A resolved Conflict closes with a bare `resolved`**, written by
   `franky.finish`. Franky's conflict session emits no summary line, and
   adding an output protocol for one is outside this decision.
3. **A Critique fixed with no `Rationale:` comment closes with a bare
   `completed`**, written by `franky.finish`. Like the Errand's `completed`,
   it claims no verdict. A Critique with a `Rationale:` comment still closes
   `accepted: <rationale>`.
4. **Jinbe reads a bare `completed` as no reason** (amends ADR 0339 §1–2).
   `parseVerdict` treats `completed`, `Closed` and the empty reason the same
   way: no prefix and an empty rationale. `closeVerdict`'s fallback then
   applies unchanged: an integrate commit makes the verdict Accepted, with
   the commit subject as the rationale, and otherwise the verdict is Closed.
   Any other unknown prefix is still kept verbatim as the rationale.
5. **History is not rewritten** (ADR 0399 §4). Beads closed before this
   decision keep `Closed` or the empty reason, and readers treat those as
   legacy.
6. **The docs name the reasons.** GLOSSARY.md's Review, Conflict and Critique
   entries name the outcome words. The closure tables in
   `docs/bead-lifecycle.md` quote each exact reason, and Gaps entry 15 is
   marked resolved.

## Considered Options

- **`approved: <approval id>` for a clean Review.** Rejected. The Approval
  still awaits its own sign-off, so the Review approved nothing.
- **`resolved: <summary>` or `resolved: onto <base sha8>` for a Conflict,
  like ADR 0400.** Rejected. The summary needs a new prompt protocol, and the
  base pin is already on the bead.
- **`fixed` or a bare `accepted` for a Critique with no rationale.**
  Rejected. Both claim a fix that `finish` cannot prove, which ADR 0339 §2
  forbids.
- **Leaving the pivot-held Review blank.** Rejected. That would keep the very
  gap this decision closes.
