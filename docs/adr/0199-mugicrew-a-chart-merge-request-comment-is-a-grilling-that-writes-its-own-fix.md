# ADR 0199: A Chart Merge Request comment is a grilling that writes its own fix

Status: Accepted
Date: 2026-10-15

Chart mc-t72m.9 (Jinbe files actionable Merge Request comments on the
container). Settles grilling mc-t72m.9.5. Charting made a Chart-MR comment a
grilling defog bead under the Chart. Research
(docs/research/chart-mr-comment-defog-child-vs-chart-land.md) found that the MR
is open only while the Chart is drained and stamped, before any Spec. An open
child stops a new Chart Landing from being spawned, but it does not hold the
pending Landing or a human merge on the Forge.

## Decision

1. **Body.** *(Key lines superseded by the Forge Review Spec: the `Thread:` /
   `Thread note:` keys are replaced by `Source: !<iid> <discussion-id>
   <note-id>` lines, and thread filing by a Forge Review; the body shape is
   kept.)* The grilling's title is the comment's first line, trimmed. Below
   the key lines (`Thread: !<iid> <discussion-id>`, `Thread note: <id>`) it
   carries the MR link to the note (`<web_url>#note_<id>`). A diff note also
   gets `Position: <path>:<line> @ <sha>` at the commented sha. Then comes
   every note in the thread, each with its author, quoted with `> `.
2. **Whoever picks it up in the TUI is interviewed.** The quoted thread is the
   brief. The commenter hears back only through Jinbe's reply and resolve when
   the bead closes, because sync is one-way and crewmates have no side channel.
3. **The grilling writes the fix.** A `Thread:` line selects a variant of
   Nami's grilling prompt: a reviewer asked for this change. The session
   confirms with the human what to change, commits the edit on its effort
   branch, and integrates onto `chart/<id>` as usual. It appends a Chart
   Decisions-so-far line only if a decision changed. This departs from ADR
   0079 §3, which splits deciding from writing only because a rebase is in
   progress there.
4. **Filing closes the pending Chart Landing** `obsolete: review comment` and
   leaves the MR open, as on a Checkpoint.
5. **The Chart re-lands through the existing loop.** Once the grilling closes
   and its work is integrated, the Chart drains and `reconcileChart` spawns a
   fresh Landing at the head. That Landing force-pushes into the same MR (ADR
   0185 §1). The new head may be unchanged when the grilling made no edit. No
   re-stamp of `chart:defogged` and no nudge are needed.
6. **A human merge while the grilling is open is accepted.** The merge lands
   the Chart. The grilling's integrate is then refused as landed, and the
   backstop files its commits as a stranded-commits Signal and closes the
   grilling, instead of pausing it. Don't merge a Chart MR while one of its
   threads has an open bead.

## Considered Options

- **Interview the commenter only.** Rejected. It needs routing to a person,
  which the bead graph doesn't do, and under ADR 0189 the reviewer is normally
  the TUI human.
- **Decide-only grilling plus an AFK writer (ADR 0079 style).** Rejected. It
  adds a bead and a hop for an edit to a doc the human is already reading.
- **Mark the MR Draft on filing to block the merge.** Rejected for now. It is
  unverified against `forge`, and it covers only comment-filed children.
  Instead, an offshoot Signal covers any Chart child whose integrate is refused
  as landed.
