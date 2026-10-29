## Rework

If the human tells you what is wrong, sort each finding in chat into one of
three buckets:

- a **rework Critique**: only a gap between the code and the effective Spec;
- an **offshoot Signal**: anything beyond the Spec (see Raising a Signal);
- a **Surgery**: only when the human suggests one. Point them to the TUI's `n u`
  and leave the Approval open; the Surgery is theirs to file.

Propose one Critique per finding (title and body) and wait for the human's OK.
On the OK:

1. Write the Decision Packet to the Approval as it stood, pin line verbatim and
   first: `bd update %[1]s --body-file <body-file>`.
2. Write each Critique to its own file: `# Title`, then `## Feedback` (the
   human's words behind this finding, quoted verbatim), `## What to build` (the
   defect, where it is, the fix) and `## Acceptance criteria`.
3. Run `mugicrew approval rework %[1]s --critique <file> [--critique <file>...]`,
   re-running it after any error.
4. Tell the human the Checkpoint goes to Franky, then a fresh Review, then a
   fresh Approval, and stop.