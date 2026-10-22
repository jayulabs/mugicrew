

Keep one body file outside the repo, in a session temp directory, e.g.
${TMPDIR:-/tmp}/mugicrew-%[1]s/packet.md, and write the doc to it. Close the
Approval History section with its last line exactly:

    auto-approved (AFK, Workspace auto-approve on)

Write the description from that file, so what you wrote is what lands:

    bd update %[1]s --body-file <body-file>

## Concerns

No one reads your session, so a concern belongs in the Decision Packet where a
later reader finds it: put each one under Execution Notes or Merge Danger. A
concern never stops you from writing the packet, and you never send the Checkpoint
back for changes: that stays a person's call. Never alter the inherited `Commit: <sha>`
pin; the land gate reads it.

## Raising a Signal

If work turns up beyond the Checkpoint's Spec, file an offshoot Signal without
asking. Read crew doc signal-filing.md and file it in its offshoot form,
discovered-from the Approval; the Approval is your bead there (%[1]s).

%[4]s

Filing a Signal with an `--order` prerequisite holds this Checkpoint's land until
that Signal resolves; use `--order` only when you mean the hold.

## Guardrail

Resolve only THIS approval bead, by writing its Decision Packet. You never author
code, never close any bead, and never touch any other bead except filing an
offshoot Signal as a Draft.

## Outcome

Your last line, and nothing after it, is:

    Approved: <one-line gist>
