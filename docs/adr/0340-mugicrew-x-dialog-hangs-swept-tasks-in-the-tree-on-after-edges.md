# ADR 0340: mugicrew's `X` dialog hangs swept Tasks in the chain's tree on dashed after-edges

Status: Proposed
Date: 2026-10-21

Chart mc-sdkpu (Withdraw takes a Task, and a chain's withdraw sweeps its
post-land Tasks). Extends the withdraw dialog of ADR 0313
(x-dialogs-show-the-chain-as-a-tree…) §1 to Tasks: a Task withdrawn on its own,
the Tasks a withdraw sweeps because they are ordered after a bead it closes, and
the pre-land Tasks it leaves open. Chosen by the human from three variants built
as a throwaway over the real TUI frame (prototype mc-sdkpu.4, side ref
`refs/mugicrew/prototypes/mc-sdkpu`).

## Decision

1. **One tree, with ordered-after on its own edge.** A swept Task hangs under the
   bead it is ordered after on a dashed `┄▸` edge, drawn after that bead's own
   `├`/`└` subtree, so the edges never read as descendants. A Task swept because
   it follows another swept Task hangs under that Task, indented one step. A
   Task withdrawn on its own is the tree's head, and its after-Tasks hang from it
   the same way.
2. **Before-closed and left-open Tasks hang on `┄◂`.** A Task the withdraw
   closes because it is ordered before the set (ADR 0351, mugicrew-a-chains-withdraw-closes-the-tasks-ordered-before…)
   hangs under the bead it comes before on `┄◂`, at normal weight with no tag,
   drawn the way a swept `┄▸` row is. Each bead's edge pass draws its swept `┄▸`
   rows first, then its before-closed `┄◂` rows, then its left-open `┄◂` rows,
   and a before-closed Task recurses through the same pass, so a chained
   predecessor nests one step under it. A Before bead that isn't drawn falls
   back to the head. A Task that blocks an in-set bead but also something
   outside the final set stays open: its row is dim on `┄◂` under its Before
   bead, whatever that bead's kind, tagged `◌ left open: also before <titles>`
   when wide (the titles truncated like `⇠ also after`) or `◌+N` when narrow
   (N = the number of Also beads). The summary line adds `· N left open`,
   counting only left-open Tasks. The UI never calls a before-closed Task
   "swept".
3. **"Also waited on" is a tag.** A swept Task that also waited on a bead outside
   the closing set is tagged `⇠ also after <title>` when wide (the title is
   truncated) or `⇠+N` when narrow, on the right where ADR 0313's `✎`/`↑` tags
   sit.
4. **A Task row carries no tag.** A Task has no tree or branch, so its row has
   no `✎`/`↑`. For a Task head, the lead line adds "A Task has no tree or
   branch, so nothing is cleaned up." For a chain head, the lead names each
   non-empty Task group, putting "Task(s)" on the first: "Closes N beads as
   withdrawn: M in the chain and K Tasks ordered after it", "… M in the chain
   and J Tasks ordered before it", or "… M in the chain, K Tasks ordered after
   it and J ordered before it" (ADR 0351).
5. **Releasing a land hold is flagged twice.** A Task head that gates a land is
   tagged `⚑ releases a land hold` when wide or `⚑` when narrow. Above the key
   hints, a red bold line reads "⚑ <Checkpoint> can then land without this
   Task's steps." `y` still confirms, and `D` stays for discarding work (ADR
   0313 §2).

## Considered options

- **The tree, then a flat "Ordered after it, closed too" section.** Rejected:
  it puts a swept Task away from the bead it follows, and each row needs an
  `after <bead>` tag to restore the link.
- **A close / sweep / keep column on every row, with a ∅ legend.** Rejected:
  the column repeats what the edge glyphs already show, and the legend costs a
  line.
