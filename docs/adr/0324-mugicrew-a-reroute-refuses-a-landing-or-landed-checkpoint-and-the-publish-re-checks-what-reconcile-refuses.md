# ADR 0324: mugicrew a reroute refuses a landing or landed Checkpoint, and the publish re-checks what reconcile refuses

Status: Accepted
Date: 2026-10-21

Amends: ADR 0322 (a-signal-reroute-is-a-flag-seeded-reconcile-leg…) §2 and §5.
Builds on: ADR 0145 (a-landing-is-one-pinned-land-attempt…), ADR 0160
(an-open-surgery-holds-its-checkpoints-critiques), ADR 0045 (concurrent-pivots),
ADR 0185 (an-open-merge-request-outlives-its-landings…), ADR 0263
(…pending-gitlab-landing-s-merge-request-open…). Decided in grilling.

## Context

A Signal reroute's reconcile and its publish are separate steps, and the target
Checkpoint can change state between them. ADR 0322 §2 and §5 check only that the
Checkpoint is open and that `SpecOf` resolves. Three facts shape the rest:

- A Landing exists only after an Approval, so Franky and Usopp never raise a
  pivot while one is open. A reroute is the first raise that can arrive then.
- Jinbe does not read pivots. The gate's `blocks` edge keeps an undispatched
  Landing off the ready frontier but cannot stop a land in progress. Under
  `land_strategy=gitlab` a pending Landing's MR stays open at its approved pin,
  and a human can merge it on the Forge whatever the graph says (mugicrew never
  arms auto-merge).
- A landed Checkpoint stays open in bd until Jinbe's next pass closes it; the
  `Checkpoint: <id>` trailer on mainline is the landed fact (ADR 0145 §3).

## Decision

1. **An open Landing refuses every reroute.** Reconcile refuses, before any
   write, when the target Checkpoint has an open `kind:landing` child, local or
   GitLab-pending, whether the reroute files a pivot, a Critique, or both. Once a
   Landing exists the approved code is effectively main's: Luffy is told the
   Checkpoint is landing and triages the Signal again after the land, against
   main.
2. **A landed Checkpoint refuses every reroute.** Reconcile also refuses when
   `LandedBase` finds the Checkpoint's trailer on its repo's mainline, even
   though the bead is still open. "Unlanded" means no trailer, not open in bd.
3. **An open MR with no open Landing does not refuse.** Under GitLab, between
   Landings the container's MR stays open at an old approved pin (ADR 0185 §2),
   watched by a `kind:mr-watch`. Reconcile allows the reroute and warns with the
   MR's URL that it stays mergeable at that pin until the next Landing, as
   ADR 0263 does for a rebase.
4. **An open Surgery does not refuse.** ADR 0160's hold keeps a rerouted
   Critique from Franky until the Surgery ends; a rerouted pivot freezes the
   Checkpoint and Zoro decides it as usual. Reconcile prints a note naming the
   open Surgery, so the human can choose to fix the defect by hand.
5. **Open pivots on other FRs do not refuse.** A different FR gets its own
   pivot (ADR 0045 §1), a Critique waits behind the gate (ADR 0045 §2), and a
   same-FR case uses `--merge-into` (ADR 0322 §7).
6. **The publish re-checks every reconcile refusal.** Besides ADR 0322 §5's
   "still open" and `SpecOf`, the publish re-reads the open Landing (§1) and
   the trailer (§2). A hit fails the publish the §5 way: the Signal stays open,
   the Drafts stay deferred, and the pane is kept for a drop and re-triage. The
   window between reconcile and publish therefore has no rules of its own.
7. **No re-check of the pivot set.** A same-FR pivot raised between reconcile
   and publish is not detected; Go cannot judge "same FR". Zoro's re-read
   before execute (ADR 0045 §4) puts both pivots to the human together.

## Considered options

- **Allow a pivot over an open Landing and warn.** The gate freezes only an
  undispatched local Landing; a running land or a human GitLab merge still
  lands the code, leaving a pivot over landed code, the case the Destination
  excludes.
- **Allow a Critique-only reroute over an open Landing.** Franky's fix would
  obsolete the Landing (ADR 0145 §1) and send approved work back through Review
  and Approval by a side door, and it races a dispatched local land.
- **Refuse while any MR is open.** That blocks reroutes for most of a GitLab
  Checkpoint's life.
- **Refuse during a Surgery, or only a pivot during a Surgery.** The hold
  already prevents the race, and refusing loses a visible Critique the
  Surgery's human could use.
- **Re-check only ADR 0322 §5 at publish.** A Landing can spawn in the window,
  because the Draft pivot does not gate until publish.
