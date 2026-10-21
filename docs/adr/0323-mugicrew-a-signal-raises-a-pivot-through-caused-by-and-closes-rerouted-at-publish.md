# ADR 0323: mugicrew a Signal raises a pivot through caused-by, carries its ask in a Critique, and closes `rerouted:` at publish

Status: Accepted
Date: 2026-10-21

Amends: ADR 0096 (signal-closes-at-publish) §2, ADR 0043 (pivot-raise-and-checkpoint-gate)
§4, ADR 0046 (pivot-triage-session) §2.
Builds on: ADR 0322 (a-rerouted-signal-s-pivot-or-critique-is-caused-by-it…),
ADR 0322 (a-signal-reroute-is-a-flag-seeded-reconcile-leg…). Decided in grilling.

## Context

A reroute turns a Signal into a pivot and/or a Critique on an open, unlanded
Checkpoint (the host). Every pivot until now was raised by an effort (Franky,
Usopp): the raiser closes `pivoted:` in its tail, and its unfinished FR lives in
the pivot body, which dismiss and amend refile as a Critique (ADR 0043 §4). A
Signal raiser has no tail and no unfinished FR. It is closed by triage, and what
it carries is a report and an ask. The inventory
(`docs/research/signal-reroute-reader-writer-inventory.md` §1, §2, §5) found the
close, Zoro's raiser read, and the refile all assume an effort raiser.

## Decision

1. **A Signal may raise a pivot.** It is the pivot's raiser through the
   `caused-by` edge ADR 0322 (a-rerouted-signal-s-pivot-or-critique-is-caused-by-it…)
   gives it, not `discovered-from`. A merge into an open same-FR pivot makes
   that pivot `caused-by` the Signal too.
2. **The Signal closes in the publish batch.** ADR 0096 §2 gains a reroute
   branch: the batch closes a Signal that has a `caused-by` pivot or Critique
   successor **among its own Drafts**. A merge edge alone never closes it. The
   agent never closes it, and no verb hands `CloseOnPublish`: outside a
   dispatched effort that publishes at once, mid-session, so a later drop could
   not reopen it (ADR 0096 §4).
3. **The reason is `rerouted: <ids>`**, listing every `caused-by` pivot and
   Critique, a merged-into pivot included. It is not `pivoted:`, which means an
   effort stopped mid-FR, and it covers a Critique-only reroute. The Checkpoint
   is not named: it is one hop from each id.
4. **The pivot body carries the triage's settled decision** in a fixed
   section, after the usual evidence and FR:

       ## Triage (<signal id>)
       FR judged wrong: <FR id and verbatim text>
       Report: <the Signal's claim, quoted>
       Why this Checkpoint: <the only-on-one-Checkpoint check's result>
       Settled with the human: <what triage decided, or "none">
       Ask: <the change the Signal wants>

   A merge posts the same section as its evidence comment on the existing pivot.
5. **Every pivot reroute also files a Critique carrying the Ask**,
   `discovered-from` the pivot (and `caused-by` the Signal, ADR 0322). It stands
   in for an effort raiser's unfinished-FR refile, and Zoro's dismiss dedupe
   already finds it as that refile. A `blocks` edge from the pivot would be
   redundant: the gated Checkpoint already freezes every member.
6. **What each pivot outcome does to that Critique.** It carries the Signal's
   ask, which assumes the Spec is wrong, so it is not refiled as-is:
   - **dismiss** (the report was wrong): Zoro rewrites it to the as-specced work
     left, if any, else closes it `dismissed: <pivot>`. The Signal stays closed;
     it is not reopened for a re-triage of what the human just decided.
   - **amend**: Zoro adds `AMENDMENT (<pivot>): …` as a comment on it, as on the
     Spec, keeping the original ask visible.
   - **confirm / escalate**: the sweep closes it obsolete, as any member. The
     Ask goes into the corrected Spec, or into the escalate evidence, in the
     place a raiser's unfinished FR goes.

   Several Critiques `discovered-from` one pivot (after a merge) are each
   treated so.
7. **Zoro reads a Signal raiser from the prompt, with no Go change.** The raiser
   read adds `bd dep list <pivot> --direction=down -t caused-by` for
   `kind:signal`. Its card's **Raised by** quotes the Signal's report and the
   Triage section (or the merge comment). The prose gets a Signal-raiser
   variant: "the report was wrong" for dismiss, "the Ask" for the unfinished FR.

## Considered options

- **The pivot `discovered-from` the Signal**, so Zoro's raiser read needs no
  change. Rejected with ADR 0322: it calls the rerouted work an offshoot. Both
  edges together give the Signal the double meaning ADR 0322 rejects.
- **The agent closes the Signal (`bd close`), as REJECT does.** It closes the
  Signal before its pivot Draft is visible, the stranded close ADR 0096 removed.
- **Close on a widened `SuccessorOf` with any status.** A merge edge onto a
  published pivot would then close the Signal at a publish that drafted nothing.
- **`pivoted: <pivot>`, plus `rerouted:` for a Critique-only reroute.** Two
  prefixes for one outcome, and "pivoted" misreads to Robin, the only reader of
  the reason.
- **Settled decision as free prose or a bd comment on the pivot.** Free prose
  gives Zoro's card no fields to quote; a comment outlives a dropped Draft.
- **A Critique only when the Signal also reports a separate code defect.** The
  Signal's ask then lives only in the pivot body, and dismiss or amend would
  have to refile it.
- **Dismiss closes the Critique outright, or reopens the Signal.** The first
  loses as-specced work the Critique may still hold; the second re-asks a
  decision the human just made.
