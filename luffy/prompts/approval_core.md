## Orient

The Approval is a membership child of a Checkpoint (kind:checkpoint). Read the
Approval body: it carries the `Commit: <sha>` pin captured when the clean review
passed — this is the exact SHA the Decision Packet is about, and the SHA Jinbe's land gate
reads. Note the parent Checkpoint id (bd show %[1]s names its parent); the
Checkpoint branch is `checkpoint/<checkpoint-id>`.%[2]s

## Assemble the Decision Packet

Read crew doc approval-body.md: it defines the Decision Packet's sections, in
order, and the advisory note. Gather its inputs with bd and git:

- the Spec, via the Checkpoint's lineage (bd show);
- the change against mainline:

      git diff --stat {{MAINLINE}}...checkpoint/<checkpoint-id>
      git diff {{MAINLINE}}...checkpoint/<checkpoint-id>

  Summarize from the stat; %[5]s;
- the closed `kind:review` child of the Checkpoint (confirm it closed clean) and
  the `Commit: <sha>` pin it was run against;
- the closed `kind:critique` children, and the Checkpoint's Pivots and
  Amendments (the Spec's AMENDMENT comments);
- the Checkpoint's obsoleted and earlier Approvals and Landings, for the Approval
  History and the gate-build failures and Landing conflicts.

Compare the Approval's `Commit: <sha>` pin to the live tip:

    git rev-parse checkpoint/<checkpoint-id>

If HEAD has moved past the pin, open the Decision Packet with the note
**"HEAD advanced since this was reviewed"**. Do **not** block on it and do **not**
re-pin: the self-healing land gate is the real safety net, not you.

Write the doc to its body file: the Commit: <sha> line verbatim and first, a
blank line, then the Decision Packet.