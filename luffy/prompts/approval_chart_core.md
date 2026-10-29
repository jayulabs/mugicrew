## Orient

The Approval is a membership child of a Chart (kind:chart). Read the Approval
body: it carries the `Commit: <sha>` pin captured when the Chart drained, and
the `Patch-id: <id>` line directly after it. The pin is the exact SHA the Chart
Packet is about, and the SHA Jinbe's land gate reads; the patch-id lets a later
re-approval tell whether the content landing changed. Note the parent Chart id
(bd show %[1]s names its parent); the Chart branch is `chart/<chart-id>`.

## Assemble the Chart Packet

Read crew doc approval-body.md: its Chart section defines the Chart Packet's
sections, in order, and the advisory note. A Chart has no Spec, Surgeries or
Review; defog and this Approval are its review. Gather the inputs with bd and git:

- the Chart body (bd show the parent): the Destination, the Decisions-so-far
  index and what was left out of scope;
- the Chart's closed grilling, research and prototype children, and its closed
  Survey: read their notes and close reasons. Skip Landings and chart-conflicts
  as sources of decisions; read a chart-conflict only for the Execution Notes;
- the prototype side-ref, if any prototype names one: read its pushed result as
  a primary source for the prototype's answer;
- the change against mainline, the Chart's squash diff from merge-base to pin:

      git diff --stat {{MAINLINE}}...chart/<chart-id>
      git diff {{MAINLINE}}...chart/<chart-id>

  Summarize from the stat; %[5]s;
- the Chart's obsoleted and earlier Approvals and chart-conflicts, for the
  Approval History and Execution Notes.

Compare the Approval's `Commit: <sha>` pin to the live tip:

    git rev-parse chart/<chart-id>

If HEAD has moved past the pin, open the Chart Packet with the note
**"HEAD advanced since this was reviewed"**. Do **not** block on it and do **not**
re-pin: the self-healing land gate is the real safety net, not you.

Write the doc to its body file: the `Commit: <sha>` line verbatim and first, the
`Patch-id: <id>` line verbatim directly after it, a blank line, then the Chart
Packet.
