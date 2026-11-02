# ADR 0409: A Remnant row waits in its Initiative, launches on enter, and drops only through the TUI's X dialog

Status: Accepted
Date: 2026-10-29

Chart "A Forge merge that strands in-flight Checkpoint work asks the human how to recover it". The Remnant is a HITL leaf that Zoro runs. It has two takeability holds: a child of the landed Checkpoint is still running, or an open pivot is on the landed Checkpoint (ADR 0402 (mugicrew-a-remnant-s-respin-carries-builds-and-critiques…) point 4). Its respin exit has a hold of its own: an open Renumber Checkpoint caused-by the landed Checkpoint (ADR 0408 (mugicrew-a-respin-waits-for-the-same-land-s-renumber…) point 1). The landed Checkpoint is closed, so the Execute tab has no tree row to put the Remnant under. Its "Landed in last 24h" row is status-only, and a Remnant can outlive that window.

## Decision

1. **The row sits like a Pivot's.**
   - The Remnant always has a side row in the Initiatives tree, in the landed Checkpoint's Initiative (or No Initiative). Its hint is `stranded from → <landed title>`.
   - While it is takeable, it is also an Actionable row, banded like any HITL leaf.
   - Jinbe titles it `Remnant of <landed title>`.
2. **The row summarizes and the detail pane lists.**
   - The row's right edge carries a count summary, e.g. `3 commits · 2 kept · 5 children` (the `Tip..head` commits, the kept `effort/*` refs, and the stranded children).
   - The detail pane lists the `Tip..head` range, each kept ref, and each child with its close.
3. **Enter launches Zoro's session** like any HITL leaf, through the shared queue-tab gates (launch, attach, `b`). There is no new key or verb.
4. **Both takeability holds are bd blocks edges.**
   - When Jinbe files the Remnant, it adds a blocks edge from each still-running child of the landed Checkpoint. The open pivot already blocks it (ADR 0402 point 4).
   - The row therefore takes the generic `blocked` state, the blocker gutter, and the enter refusal that names the blocker. It leaves Actionable through ordinary banding.
   - The tree row's hint adds which holds apply, e.g. `held by 2 running · pivot <title>`.
5. **The respin hold is not blocked.**
   - It has no edge. The Remnant stays in Actionable's ready band, and enter launches as usual.
   - Both of its rows (Actionable and the tree) carry a dim amber suffix `respin waits on Renumber <title>`, and the detail pane repeats it.
   - The suffix shows only while that Renumber Checkpoint is open.
6. **Drop goes only through the TUI.**
   - Zoro never runs `mugicrew remnant <id> drop`, and its prompt forbids it. When the human chooses drop, Zoro tells them to press X on the Remnant row.
   - X opens the withdraw dialog's modal with the Remnant's own drop wording: the container branch and each `effort/*` ref it deletes, and that nothing is re-filed. `y` runs the drop verb in-process. `remnant drop` stays a CLI verb.
   - X is refused while the Remnant is blocked, with the same "blocked by <title>" reason as enter. It is also refused while its session is live, so a drop can't race an offshoot or respin the session is running.
   - Offshoot and respin still run from Zoro's session on the human's yes.

## Considered Options

- **Actionable only.** Rejected: a held Remnant would be shown nowhere.
- **A new top-level Remnants section.** Rejected: the Pivot's waiter shape already places a HITL bead about a Checkpoint in its Initiative.
- **The stranded lists on the row.** Rejected: they are unbounded, and the detail pane already holds a bead's long facts.
- **The running-child hold as a computed check with no edges.** Rejected: it would need its own row state, refusal and banding. An edge reuses all of them, and a running child's close lifts it.
- **A glyph or new state for the respin hold.** Rejected: the Remnant is takeable, and a state would read as a takeability hold.
- **Drop confirmed in-session, by Zoro asking a drop-specific yes.** Rejected: only an agent would relay the yes to an irreversible ref delete. The human's own keypress in the TUI authorizes it.
- **Zoro runs drop and the verb waits for a TUI confirmation.** Rejected: a cross-process handshake for what one dialog does.

## Consequences

- The Execute tab's waiter scan needs a Remnant arm keyed on its `related` landed Checkpoint, because that Checkpoint is closed and not among the listed Checkpoints.
- X gains a Remnant arm next to the withdraw and cancel arms.
- Jinbe's Remnant filing adds blocks edges from running children. A child that finishes and is refused at integrate appends to the Remnant before it closes, which lifts its edge.
