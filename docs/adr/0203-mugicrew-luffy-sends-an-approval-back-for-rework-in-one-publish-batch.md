# ADR 0203: Luffy sends an Approval back for rework in one publish batch

Status: Accepted
Date: 2026-10-15

Chart mc-e5n6 (Approval rework loop). Settles grillings mc-e5n6.4 and mc-e5n6.5 (§8). Amends ADR
0020 §5, which kept rework out of scope because both naive paths break: an open
Approval plus a Critique deadlocks, and closing the Approval before filing races
Jinbe's land gate. Research (docs/research/rework-verb-critique-filing-and-approval-retire.md,
docs/research/approval-close-reason-reads-and-writers.md) found that a Draft
Critique already holds every gate, and that Usopp's Review close publishes
Drafts and a close in one atomic batch.

## Decision

1. **The verb.** `mugicrew approval rework <approval> --critique <file>
   [--critique <file>...]`. Each file is one Critique, one per distinct
   finding: a first `# Title` line, then a non-empty body. Luffy runs it only
   on the human's OK of the Critiques it proposed in chat. The prompt never
   sequences raw bd calls for rework.
2. **Steps.** (a) Validate every file before any read or write, and require at
   least one `--critique`. (b) One read: refuse loudly unless the target is a
   `kind:approval` child of a Checkpoint that is open or in_progress. (c) Create
   each missing Critique as a Draft child of the Checkpoint, `drafted-by:<approval>`.
   (d) `bd.Publish(approval, …, Closing{approval, "rework: <id>, <id>, …"})`:
   the Critiques go live and the Approval closes in one transaction, so no
   state ever has the Approval closed and no Critique holding the gates.
   (e) Print the Critique ids.
3. **Re-run.** A Critique is keyed by its exact title among the Approval's
   `kind:critique` Drafts: a match is adopted as-is. Before the publish, the
   verb deletes any `kind:critique` Draft of the Approval that this run did not
   name, so the batch publishes exactly the OK'd set. Run against a closed
   Approval, the verb refuses loudly and shows its close reason (the
   `initiative` verbs' precedent). Luffy re-runs the verb after any error.
4. **Fail-safe residue.** If the verb dies before the publish and the human
   leaves anyway, a clean `/exit` publishes the Drafts while the Approval stays
   open. Franky fixes them, the tip moves, and Usopp's stale-Approval heal
   retires the Approval. A drop deletes the Drafts, which leaves the plain open
   Approval. Neither lands anything, so no extra machinery guards it.
5. **Close reason.** It is `rework: <critique ids>`. It never starts `obsolete:`
   (that would read as swept) or `approv`. The land gate and ReviewDue count
   only Approvals closed exactly `approved`, so a rework close never lands and
   a fix with no new commit still gets a fresh Review and Approval. The Approval
   History gains the ending "sent back for rework (Critiques …)".
6. **Luffy's session.** On the human's OK, Luffy writes the Decision Packet to
   the Approval (`bd update --body-file`) as it stood, so the closed Approval
   records what was rejected. It then runs the verb, tells the human the
   Checkpoint goes to Franky and then a fresh Review and Approval, and stops.
7. **ADR 0059 §4 note.** The verb publishes the session bead's Drafts (any
   offshoot Signals the session filed included). It is a second publisher for
   that creator beside `hitl-run`'s exit. This is accepted because those Drafts
   would be published at `/exit` anyway, and no Critique Draft outlives the
   verb, so ADR 0124 §1's "hitl-run publishes only Signals" holds.

8. **What a rework Critique carries.** Franky's run gets nothing beyond the
   Critique body and the Spec, the same as for any Critique. It gets no
   pointer to the closed Approval or its Decision Packet, and its prompt does
   not change. So the body must stand alone. In `approval.md`, Luffy is told
   to write each `--critique` file as `# Title`, then `## Feedback` (the
   human's words behind this finding, quoted verbatim), `## What to build`
   (the defect, where it is, the fix) and `## Acceptance criteria` (what done
   looks like, judged against the quoted words). These are the Build's own
   headings, which `build.md` already tells Franky to read. The verb does not
   enforce the shape. A rework Critique covers only a gap between the code
   and the effective Spec. Feedback that goes beyond the Spec becomes an
   offshoot Signal (approval.md "Raising a Signal"). Luffy names a Surgery
   only if the human suggests one. Before the OK, Luffy says in chat which
   bucket each finding falls into.

## Considered Options

- Filing each Critique already published, one by one (the `fileConflict`
  shape). This was rejected because Franky can finish Critique 1 before
  Critique N exists, which spawns a Review while Critiques are still open.
- Leaving Drafts for `hitl-run` to publish at `/exit`. This was rejected because
  a drop deletes them after the Approval has closed.
- A no-op exit 0 on a re-run against a `rework:`-closed Approval. This was
  rejected because the refusal already tells Luffy the work is done.
- Linking each rework Critique to the closed Approval so that Franky reads its
  Decision Packet. This was rejected because the packet covers the whole
  Checkpoint and was written to persuade a human, not to scope a fix. Luffy
  already holds that context when it writes the body.
- A rework Critique that carries intent beyond the Spec, with the quoted
  feedback as its authority. This was rejected because it makes Franky build
  off-Spec and the next Review would flag it. The Spec stays the single
  contract.
