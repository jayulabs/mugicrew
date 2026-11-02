<!-- verbatim subset of mugicrew GLOSSARY.md (ADR citations and History lines stripped) · checked by vocabulary_test.go, whose admission test (a) counts uses in crew docs and CREWMATE.md only -->

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
- **Container** — a bead that parents member beads; a bd `epic`. Contrast a
  **leaf**, a bd `task` that parents none.
  _Avoid:_ a Docker or OCI container.
- **Signal** — anything that could catalyze a change to the Workspace. Abstract
  by nature — concrete forms include a human-written idea, an error log, or a
  system alert. mugicrew records each one as a `signal` bead; anything that may
  result in a code change enters the bead graph this way. Triage can **reroute** a Signal whose code is only on one in-flight
  Checkpoint (its **host**) instead of charting it.
  _Avoid:_ event, input, trigger, issue, ticket
- **Landing** — one bead per attempt to land a container's branch on mainline,
  pinned (`Commit: <sha>`) to the base it will land. Its outcomes: _landed_
  closes it `landed: <sha>`; _conflict_ closes it `obsolete: conflict` and files
  the container's conflict bead; _pending_ leaves it open; _branch advanced_
  closes it `obsolete: branch advanced without landing`;
  _build failed_ closes it `obsolete: build failed` and files a Critique on the
  Checkpoint; _pipeline failed_ closes it `obsolete: pipeline failed` and files
  a Critique on a Checkpoint, or a hold Errand on a Chart; _merge request
  closed_ closes it `obsolete: merge request closed` and holds the container on
  an Errand.
  _Avoid:_ the act of landing, which is **Land**.
  - _Structure:_ leaf (member of a Checkpoint or a Chart; at most one open per
    container); AFK, Mechanical, no session
  - _Notes:_ The `<Kind>: <id>` trailer on mainline stays the durable landed
    fact; the Landing's close mirrors it. Only Checkpoint and Chart lands are
    Landings; other moves of main (Rollback reverts, and legacy Spec anchors)
    stay bead-less. Every follow-up a Landing files
    (Critique, Conflict, Chart-conflict, Hold Errand) is born discovered-from
    it.
- **Build** — a unit of implementation work sliced from the Spec: a slice
  inside a Checkpoint.
  - _Owner:_ Franky
  - _Structure:_ leaf
  - _Operations:_ Franky executes → close. May raise a `critique` (offshoot
    defect) or a `pivot` (Spec proven wrong).
  - _Notes:_ —
- **Carry** — when a Signal, Chart or Spec closes in **succession**, the beads it
  _orders_ (blocks) become ordered by its successor, so a dependent waits for
  the real work, not the stage that described it. Outgoing ordering only,
  except that an open Latch blocking it also comes to block its successor,
  unless that successor is an Initiative; the old edge
  stays as lineage. A Spec's dependents therefore wait for its Checkpoint to
  **land**. A Checkpoint obsoleted by a confirmed or escalated **pivot** carries
  the same way, to the corrected Spec or the escalated Chart.
  A bead closed as a duplicate carries the same way, onto its original's live head
  (the original while unclosed, else the first unclosed bead along its succession);
  when that chain has ended, its dependents are released and the event records them
  skipped `ended`.
  _Avoid:_ "moving" edges (nothing is removed).
