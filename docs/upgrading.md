# Upgrading across the Errand rename

The Task kind is now the Errand kind (ADR 0354, ADR 0355). Upgrade in this order:

1. Stop the loop (and quit the TUI's dispatch).
2. Upgrade: `npm i -g @jayulabs/mugicrew`.
3. Run `mugicrew doctor fix`. It relabels every `kind:task` bead `kind:errand` in
   place, closed beads included.
4. Restart the loop.

Until step 3 has run, `mugicrew loop start` and the TUI's dispatch refuse, and their
error names `mugicrew doctor fix`. Three more things now fail loudly and name their
new form:

- `mugicrew task new` fails; use `mugicrew errand new`.
- A `task: <id>` adopt line in a Spec fails the slice; write `errand: <id>`.
- A `## Tasks` heading in an open Spec fails the slice; write `## Errands`.

Self-hosting: when this change lands in the Workspace that develops mugicrew, the
loop refuses on restart until a human runs `mugicrew doctor fix` over that
Workspace's 29 `kind:task` beads. That refusal is the designed cutover.
