<!-- verbatim subset of mugicrew GLOSSARY.md (ADR citations and History lines stripped) · checked by vocabulary_test.go -->

# Vocabulary

- **Draft** — a bead still being set up by the effort or session that created
  it. The loop never sees it and it can't be claimed, but every "is it still
  open?" check counts it as open, so a Chart or Checkpoint with drafts is not
  drained. It becomes a normal bead when that effort or session **publishes**
  it, in one step together with any of its drafts' edges and the closing of its
  own bead, and only when it exits cleanly.
  _Avoid:_ "deferred" (bd's name for the state a Draft is stored in),
  "hidden", "pending", "paused" (a paused bead is fully set up).
- **Publish** — turning all of an effort's or session's Drafts into normal beads
  in one atomic step.
- **Integrate** — moving an effort branch's work onto its container's branch
  as one squash commit, before the bead closes. Distinct from _land_, which
  moves a container onto mainline through a Landing.
  _Avoid:_ "land" or "merge" for this step.
- **Held** — integrate's refusal when the container branch it would move is
  checked out in another worktree: the container is left untouched and the bead
  stays open until the branch is free.
  _Avoid:_ confusing it with a foreign hold (the effort branch) or a pause (a
  bead the loop won't dispatch).
- **Land** — moving a container's branch (a Chart's or a Checkpoint's) onto
  mainline as one commit, its landed base, through a Landing. Only containers
  land; an effort branch integrates.
  _Avoid:_ "land" for pushing to a side ref or committing a doc; "merge".
- **Mainline** — the Workspace's trunk branch (`main`). Lands advance it and
  container branches are cut from it; no crewmate commits to it directly.
  _Avoid:_ "trunk", "master".
- **Drained** — a container (Chart, Checkpoint, Initiative) that is still
  open but has no unclosed member; Drafts count as unclosed. A drained
  container is routed to its handler.
  _Avoid:_ "draining" (a loop state), "empty", "done".
