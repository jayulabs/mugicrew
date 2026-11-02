<!-- hand-kept · adapted from mattpocock/skills skills/engineering/pr (the pr skill's visual-choosing guidance and brevity rules); summarises ADR 0020 (Luffy's approval session), ADR 0407 (a respin's Packet) -->

# The Decision Packet and the Chart Packet

The Decision Packet is the written case for approving a Checkpoint: kept on its
Approval, carried into its land commit (and, for a remote-truth Workspace, its
Merge Request body). The human certifies it; the crewmate writes it. Write it in
the Workspace's GLOSSARY language so a human can follow it, skip preambles, and
keep prose brief. Refer to beads and Specs by title, never by bare id.

A Chart's Approval carries the **Chart Packet** instead: the same six headings
in the same order, each read for a Chart (see "The Chart Packet" below). This
opening, the brevity rules and the advisory note apply to both Packets.

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

## A respin's Packet

A respin is a Checkpoint whose body starts `Respin of`. It carries the work that a
Forge merge stranded when it landed an earlier Checkpoint for the same Spec. Its
branch is cut from mainline, so its diff already leaves out what landed. Add to the
sections above:

- **Background**: one line: "Respin of <landed Checkpoint title>, which a Forge
  merge landed at <sha>. The Spec's work up to `Landed-tip` is already on mainline
  and is not in this diff."
- **Summary**: tag each row **replayed** (from the commit with a `Replayed:`
  trailer, never approved) or **respin** (built by a member).
- **Execution Notes**: the replay's outcome (clean, or the replay Conflict and how
  it cleared) and the re-filed members by title.
- **Approval History**: after this Checkpoint's own Approvals, one line for the
  landed Checkpoint's last approved pin (`Landed-tip`), saying that nothing after it
  was approved.

## The Chart Packet

A Chart has no Spec, Review or Surgeries: defog and the Approval are its review.
Read each section for what lands on mainline from the `chart/<id>` branch. The
body starts with the `Commit:` and `Patch-id:` lines, both verbatim, then the
Packet. Under gitlab the Packet is the Draft MR's description, so write "Draft
MR" or "GitLab Draft", never bare "Draft".

### Background

The Signal the Chart charted, by title, and the Chart's Destination.

### Summary

A table of what lands: ADRs, GLOSSARY entries, research docs and crew docs, each
with where it lands and why. Then the Decisions-so-far index, one line per
decision. The visual rule above applies.

### Evidence

The pinned SHA, and the closed defog beads (grilling, research, prototype) and
the Survey, each by title.

### Merge Danger

The Door (one-way or two-way) and the Blast Radius, naming each ADR the Chart
supersedes or amends.

### Execution Notes

Chart-conflicts and how each was resolved, offshoot Signals, and Destination
narrowings. Say "none" in one line when there were none. When the Chart has an
open blocker, add one line per blocker: "Land waits on <blocker title> until it
lands".

### Approval History

As for the Decision Packet: every earlier Approval on this Chart, its pinned
SHA, how it ended (obsoleted as stale, sent back for rework (defog beads …),
read from its `rework:` close reason, or approved and then undone), and what
changed before the next one; one line saying so when this is the first.

## The advisory note

Whenever the Approval's pin is behind the Checkpoint or Chart branch's tip, the very first
line of the Packet (below the pin lines) is the note **HEAD advanced since this was reviewed**.
Omit it when the pin is the tip.
