# ADR 0045: mugicrew concurrent pivots on one Checkpoint

Status: Accepted
Date: 2026-10-04

Amends: ADR 0043 §3 (raise when a pivot is already open), §6 (dedupe scope),
§8 (the confirm/escalate sweep); ADR 0044 (amendments under a later confirm).

## Context

ADR 0043 already holds the gate with more than one pivot: each pivot adds its
own `blocks` edge, so the Checkpoint stays frozen until all of them close. But
the freeze means a second pivot can only come from an effort already running at
the first raise (a Build or Review still finishing). That leaves pivot
identity, triage unit and cleanup open. Decided in grilling.

## Decision

1. **Merge by FR at raise.** A raiser that finds an open pivot on its Checkpoint
   for the **same FR** comments its evidence on that pivot and closes
   `pivoted: <that pivot>`; it files no new bead. A **different FR** gets its
   own pivot (ADR 0043 §3 unchanged). One pivot = one FR, so amend (ADR 0044,
   judged per FR) stays clean.
2. **Usopp dedupe is per FR.** An open pivot covers a finding on the same FR
   only. A different Spec flaw raises or merges per §1; an implementation defect
   is a Critique that waits behind the gate.
3. **Triage unit = the Checkpoint.** One Inbox item and one HITL Launch session
   per Checkpoint with open pivots. Zoro is given every open pivot, recommends
   an outcome for each, and the human decides them together.
4. **Re-read before execute.** Before carrying out decisions, Zoro re-reads the
   Checkpoint's open pivots. Any that appeared during the session are put to the
   human first. Every pivot is decided by a human, including at the sweep edge.
5. **Confirm/escalate sweeps sibling pivots.** The sweep (ADR 0043 §8) also
   closes every other open pivot on the Checkpoint, reason
   `superseded: <pivot>`. Their evidence and FRs are carried into the corrected
   Spec, like the raising bead's unfinished FR.
6. **Amendments fold into a superseding Spec.** The corrected Spec's body is
   re-derived from the old body plus its Amendments, and starts with none of
   its own.
7. Dismiss and amend of one pivot while others stay open need no new rule: the
   pivot closes, the others' edges keep the Checkpoint frozen (ADR 0043 §7).

## Consequences

- No pivot outlives its Checkpoint, and none is closed unseen by a human.
- Per-FR scoping in raise, dedupe and amend keeps one pivot to one decision.
- Zoro's triage input is a Checkpoint's set of pivots, not a single pivot.

## Alternatives considered

- **Always a separate pivot.** Rejected: duplicate pivots for one FR, two
  decisions to make once.
- **Always merge into the open pivot.** Rejected: mixes FRs, breaks per-FR amend.
- **Any open pivot covers any finding.** Rejected: silently drops real flaws.
- **One session per pivot.** Rejected: invites contradictions (amend A in a Spec
  that confirming B then discards).
- **Leave siblings open after confirm, re-triage against the new Spec.**
  Rejected: a pivot blocking a closed Checkpoint is the same failure again.
- **Execute only what the session saw.** Rejected: a sweep would close a late
  pivot no human saw.
