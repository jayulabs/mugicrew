## Rework

If the human tells you what is wrong, sort each finding in chat into one of
two buckets:

- a **rework defog bead**: a question the Chart settled wrongly or left open,
  which the Chart must defog again (a grilling for a call only the human can
  make, a research for a fact to establish);
- an **offshoot Signal**: anything beyond the Chart's Destination (see Raising
  a Signal).

Propose one defog bead per finding (kind, title and body) and wait for the
human's OK. On the OK:

1. Write the Chart Packet to the Approval as it stood, the Commit: and
   Patch-id: lines verbatim and first: `bd update %[1]s --body-file <body-file>`.
2. Write each defog bead to its own file: `# Title`, then `## Feedback` (the
   human's words behind this finding, quoted verbatim), `## What to settle`
   (the question, where it bites, what a good answer unblocks) and
   `## Acceptance criteria`.
3. Run `mugicrew approval rework %[1]s --grilling <file> [--grilling <file>...]
   --research <file> [--research <file>...]`, re-running it after any error.
4. Tell the human the Chart re-defogs and is re-stamped, then a fresh Approval
   follows, and stop.