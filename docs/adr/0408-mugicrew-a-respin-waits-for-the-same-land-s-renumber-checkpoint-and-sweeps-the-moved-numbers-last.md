# ADR 0408: A respin waits for the same land's Renumber Checkpoint and sweeps the moved numbers last

Status: Accepted
Date: 2026-10-29

Chart "A Forge merge that strands in-flight Checkpoint work asks the human how to recover it". A Forge merge that strands work (a Remnant) can also make the landed Checkpoint the later side of a number clash. In that case `closeLanded` mints a Renumber Checkpoint in the same tick (ADR 0225 (a-forge-merged-number-clash-is-renumbered…)), which moves the landed file's number on mainline, for example `0405-slugA → 0410-slugA`. Its sweep scope is the landed squash's files plus the landed Checkpoint's bead subtree (`RenumberScope`). That scope reaches the stranded children's bodies, but not the respin (ADR 0407 (a-respin-checkpoint-is-caused-by-its-remnant…)), its seed commit, or the kept `effort/*` branches it adopts (ADR 0402 (a-remnant-s-respin-carries-builds-and-critiques…)). This leaves two hazards:

- stranded lines that cite the landed number go stale;
- a `Tip..head` renumber lap on the landed file, replayed before the Renumber lands, would rename mainline's file without `numberClashes` noticing.

The research behind this is `docs/research/respin-checkpoint-cut-replay-and-cross-container-integrate.md` §1d, §2c.

## Decision

1. **The respin goes second.**
   - While a Renumber Checkpoint minted for the landed Checkpoint is open, `mugicrew remnant <id> respin` refuses, naming the Renumber Checkpoint by title. The hold ends when the Renumber Checkpoint closes for any reason, landed or not.
   - The Remnant stays takeable, and its TUI row shows "respin waits on Renumber <title>".
   - Drop and offshoot are never held, because neither replays onto mainline. Offshoot's one Signal also names the Renumber Checkpoint and its moves (`0405 → 0410`), so whoever picks it up knows that the stranded range cites pre-move numbers.
2. **The replay needs nothing new.** Run against renumbered mainline, the three-way `merge-tree --merge-base=<Tip>` (ADR 0407 §4) carries an edit of the moved file onto its new name through rename detection. A stranded rename of that file meets the Renumber's rename as a rename/rename conflict. That goes through the `replay` Conflict, and Franky keeps mainline's number. Re-filed member bodies are copied from beads the Renumber Build already swept.
3. **The dry-run flags it.**
   - While the Renumber Checkpoint is open, Zoro's Remnant session runs no merge-tree, because the result against current mainline would mislead. Instead it shows the wait, the planned moves, and every `Tip..head` change to a numbered file the landed squash added, with a rename called out as "will conflict with the Renumber's move".
   - After the Renumber Checkpoint closes, the dry-run runs as ADR 0407 §4 describes, plus a line naming the moves that landed.
4. **A dedicated sweep Build runs last on the respin.** The respin files it only when the Renumber Checkpoint landed its moves. It is a Franky `kind:build` member:
   - its body uses crew doc sequence-renumber.md step 5 wording and names each landed move;
   - its sweep scope is the files the respin branch changes plus the respin's bead subtree;
   - it is blocked by every other member, including a `replay` Conflict, so it sees the seed and all adopted and new work before Review.
5. **`RenumberScope` and `RenumberPred` are unchanged.** The respin stays outside the Renumber's scope, and it is never caused-by the landed Checkpoint (ADR 0407 §1).

## Considered Options

- **The Renumber waits for the respin.** Rejected: mainline keeps a duplicate number for as long as the stranded work takes, and the rename hazard still needs a new guard.
- **Neither waits, with `RenumberScope` widened to the Remnant and respin, plus a guard against replayed renames of landed numbered files.** Rejected: the Renumber Build still cannot reach the respin's branch content, so the respin would need its own sweep anyway, as well as a new guard.
- **Hold the whole Remnant.** Rejected: drop and offshoot have no reason to wait.
- **Record the respin choice now and let Jinbe cut and replay when the Renumber closes.** Rejected: the human would authorize a dry-run that is stale by the time the replay runs (ADR 0407 §4).
- **Sweep in Go during the replay, or only in the `replay` Conflict leaf, or leave it to Review.** Rejected: a sweep must read each line to settle its referent (crew doc sequence-renumber.md); a clean replay never reaches the Conflict leaf; and Review judges a diff but does not sweep, so a missed citation silently changes what a number means.

## Consequences

- The respin exit has a hold of its own, separate from the Remnant's two holds (ADR 0402 point 4). `remnant respin`, Zoro's session and the TUI row all need to read "an open Renumber Checkpoint caused-by the landed Checkpoint".
- The respin command reads the Renumber Checkpoint's landed squash for the moves that its sweep Build names.
