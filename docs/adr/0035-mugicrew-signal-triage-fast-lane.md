# ADR 0035: mugicrew Signal-triage fast lane — a sharp Signal emits a direct Spec; review is kept

Status: Accepted
Date: 2026-09-30

Extends: ADR 0006 (Signal intake — a fourth triage outcome), ADR 0014 (the fast
lane becomes the producer of the `Spec:` anchor). Supersedes: ADR 0012
(mugi-grill). Decided in grilling.

## Context

Every accepted Signal spawns a Chart, even one that arrives already sharp with no
fog to clear. `mugi-grill` covers small changes (ADR 0012) but runs outside the
core flow. The Signal proposed a `kind:patch`: Franky-owned,
unparented, landed without review. Research found that
work landed without a Checkpoint needs a new cut site, route arm, land candidacy,
approval gate, lander, conflict home and an ADR 0009 amendment. A Checkpoint with
one slice, fed by a direct Spec, already works end to end (`364e809`).

## Decision

1. **The fast lane skips the Chart, not review.** A fast-laned Signal emits a
   **direct Spec** caused by the Signal. From there the path is unchanged: Zoro
   builds the Checkpoint and slices it as usual (usually one Build), then Franky,
   then Usopp's Review, Luffy's Approval, and Jinbe's land. No new kind is added.
2. **Nami writes the full Spec body in the triage session itself.** If she
   cannot, the Signal is not sharp, so it is accepted with a Chart instead.
3. **One human gate.** ADR 0006 already requires the human to approve triage.
   Nami shows the drafted Spec in the same exchange, so approving the fast-lane
   outcome also accepts the Spec.
4. **Nami's Go code writes the anchor.** After the Spec exists, the triage
   handler writes the empty `Spec: <spec-id>` commit on mainline, skipping it if
   the commit already exists (ADR 0014 §2's shape). The prompt never writes it.
5. **Triage writes no files to the repo.** Signal triage runs at the repo root
   with no branch (ADR 0009), so the anchor is always the empty commit. Any doc or
   glossary edit the Spec calls for is part of the Build and is reviewed with it.
   A Signal that needs an ADR is not sharp and gets a Chart.
6. **The predicate is one test** (decided in grilling). A Signal qualifies
   for the fast lane when Nami can write the complete Spec during triage and it
   needs no ADR. If in doubt, Nami charts it. "Single unit", "no new interface"
   and "reversible" are hints in the prompt, not gates. Review is kept, so the
   predicate only guards against skipping fog. It does not guard mainline.
7. **Clarifications, not grilling.** Nami may ask the human clarifying questions
   in triage. No count limit applies, but each answer must (i) be carried entirely
   by the Spec body, with no ADR and no decision later beads lean on, and (ii) not
   open a follow-up question. An answer that branches is a design tree, so the
   Signal gets a Chart. Answers live only in the Spec body, where Review checks
   them.
8. **Nami proposes, the human disposes.** The human can always change a fast-lane
   outcome to a Chart. The human can change a Chart outcome to the fast lane only
   if Nami can then write the complete Spec, because insisting doesn't make a
   Signal sharp. Minor edits to the drafted Spec at approval are fine. An edit that
   settles an open design question counts as a clarification (§7).
9. **A misjudgment escalates through the existing pivot (ADR 0005).** When Franky
   or Usopp finds fog, they raise a pivot. Zoro's escalate arm supersedes the
   Spec and its Checkpoint as usual. The Spec has no Chart to obsolete, so the
   fresh Chart is **caused by the original Signal**, which stays closed. This
   keeps ADR 0006's rule that a Signal spawns at most one Chart. No new
   cut-time path is added: if Zoro spots fog while slicing, it still surfaces
   as a pivot from a Build.
10. **The graph decides the outcome; a flag only seeds the Spec** (decided in
    grilling). The fast-lane verb is `mugicrew reconcile <signal> --spec
    <body-file> [--title <t>]`. The flag spawns the Signal's caused-by Spec
    successor with Nami's body. `reconcileSignal` then branches on the graph: a
    Signal with a Spec successor takes the fast-lane leg (ensure the Spec, write
    the `Spec:` anchor if absent, close the Signal). Otherwise it takes the Chart
    leg. It refuses to mix legs: no Chart leg once a Spec successor exists, no
    fast-lane leg once a Chart exists. So a plain re-run after a crash finishes
    the fast lane and never spawns a second successor (ADR 0006). A missing
    `--title` defaults to exactly the signal title, with no kind prefix (amended). If Zoro reaches the Spec
    before its anchor exists, his hard-fail retries harmlessly.
11. **The prompt shows FAST LANE as a fourth sibling section** next to ACCEPT
    (with a Chart), REJECT and DUPLICATE. It replaces the old "no fog → mugi-grill,
    stop" paragraph with the §6–8 predicate. Nami shows the full Spec body inline
    and verbatim, then waits for an explicit yes or "Chart it". She re-shows any
    edits before the yes. The verb runs only after the yes, since approval *is*
    Spec acceptance (§3).
12. **Observability is one eventlog record** (`triage.fastlane`). There is no TUI
    change: a fast-laned Signal leaves the Untriaged list like any triaged
    Signal, and its caused-by link already tells a fast-laned Spec apart.
13. **mugi-grill is retired; this ADR supersedes ADR 0012** (decided in
    grilling). Signal is again the only door into the graph. Sharp work takes
    the fast lane. Work that needs an interview or an ADR gets a Chart (often one
    grilling bead), whose branch carries the ADR through a land. mugi-grill
    committed ADR and glossary edits straight to mainline without review, which
    is the hole §5 refuses. Existing headless Specs stay valid as history, and
    Zoro cuts them from their `Spec:` anchor as before. The Build for this lane
    deletes the `mugi-grill` skill.

## Considered options

- **`kind:patch` with no review, or an unparented `kind:build`.** Rejected. Each
  needs the seven mechanisms listed above. It also leaves the triage predicate as
  the only safeguard between a Signal and mainline, which makes the lane a way to
  bypass review. The lane's saving is the Chart, not review.
- **A strict five-part checklist, or a question count limit.** Rejected. The
  extra conditions reduce to "no open question" plus "no ADR", or can be gamed.
  Three trivial questions are fine, and one question that branches is not.
- **Reopen the Signal on escalation, or add a fast-lane escalation kind.**
  Rejected. The pivot already re-enters at the shallowest stage that can fix
  the error. The only new rule is which bead causes the fresh Chart.
- **Nami emits a Checkpoint with one slice directly.** Rejected. It duplicates
  Zoro's cut and slicing in Nami, and it leaves the Review with no Spec to check
  against.
- **Commit triage docs straight to mainline (as `mugi-grill` does), or cut a
  `spec/<id>` branch for them.** Rejected. The first lets docs land without
  review. The second needs a new branch kind, a worktree placement and a lander.

- **Keep mugi-grill as a sibling lane** (fast lane for sharp work, mugi-grill
  for work that an interview settles and that needs an ADR), or narrow it and
  route its docs through review. Rejected. The first keeps two doors and the
  unreviewed-docs hole. The second needs new machinery for what a one-bead
  Chart already does.
- **A separate `mugicrew fastlane` verb, or a flag that alone decides the leg.**
  Rejected. After a crash, a plain `reconcile` re-run would take the Chart leg
  and give the Signal a second successor. **Nami running `bd create` for the
  Spec herself** was also rejected, because it hand-wires caused-by.

## Consequences

- Zoro's `checkpointBase` follows the caused-by link only to a Chart, so a Spec
  caused by a Signal cuts from its own `Spec:` anchor with no code change.
- Depends on fixing `LandedBase`'s prefix match (a short id can match a longer
  one). Otherwise the check that skips an existing anchor can match the wrong
  trailer. That fix is filed as its own Signal.
