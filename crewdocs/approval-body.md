<!-- hand-kept · adapted from mattpocock/skills skills/engineering/pr (the pr skill's visual-choosing guidance and brevity rules); summarises ADR 0020 (Luffy's approval session) -->

# The Decision Packet

The Decision Packet is the written case for approving a Checkpoint: kept on its
Approval, carried into its land commit (and, for a remote-truth Workspace, its
Merge Request body). The human certifies it; the crewmate writes it. Write it in
the Workspace's GLOSSARY language so a human can follow it, skip preambles, and
keep prose brief. Refer to beads and Specs by title, never by bare id.

## Sections

Six sections, in this order, each a `##` heading.

### Background

The Spec's intent and the approach taken, naming the Spec by title. Enough that
the reader understands the change without reading the Spec first.

### Summary

A table of the logical changes: change, where, why. Add at most one pr-style
visual (call tree, file tree, diff sketch or Mermaid) only when it makes the
point clearer than the table. Choose the smallest visual that carries it:
behavior flow → call tree; layout change → file tree; one tricky edit → diff
sketch; interacting parts or states → Mermaid. No visual beats a decorative one.

### Evidence

The clean Review and the pinned SHA it certified. Before/after test or run
evidence where it exists; omit what does not.

### Merge Danger

The Door (one-way or two-way) and the Blast Radius, with brief notes on why.

### Execution Notes

The problems met during execution: Critiques raised and how each cleared,
Pivots, Amendments, gate-build failures and Landing conflicts. Say "none" in one
line when there were none.
When the Checkpoint has an open blocker, add one line per blocker: "Land waits on
<blocker title> until it lands".

### Approval History

Every earlier Approval on this Checkpoint: its pinned SHA, how it ended
(obsoleted as stale, sent back for rework (Critiques …), read from its
`rework:` close reason, or approved and then undone by a Landing conflict or
failed gate build), and what changed before the next one. When this is the first
Approval, one line says so.

## The advisory note

Whenever the Approval's pin is behind the Checkpoint branch's tip, the very first
line of the Decision Packet is the note **HEAD advanced since this was reviewed**.
Omit it when the pin is the tip.
