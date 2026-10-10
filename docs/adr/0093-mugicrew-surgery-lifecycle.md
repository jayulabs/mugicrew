# ADR 0093: mugicrew Surgery lifecycle — pause by commit-and-exit, a comment per session, end only on the human's word

Status: Accepted
Date: 2026-10-07

Amends: ADR 0067 §1 (a surgery's pause line beside the injected closing
line). Builds on ADR 0091, ADR 0092, ADR 0016 §2–3.

## Context

A Surgery spans one or more HITL sessions on the same bead and ends only when
the human says so (surgery Chart). Research found
that a clean `/exit` deletes the worktree and forgets the session id, so only a
failed exit can resume. It also found that the claim persists between
sessions, and that `bd comments add` is a safe append-only record. A HITL
session has no Go step after it to spawn the Review, as Franky's Go code does
when the last Build closes. Decided in grilling.

## Decision

1. **Pause = commit, then `/exit`.** Every session starts fresh. Franky
   commits after each accepted change (ADR 0092), so the clean exit loses
   nothing and frees `checkpoint/<id>` between sessions. On a pause, Franky
   first handles a dirty tree (commit on green, or discard, as the human
   chooses), writes the session record (§2), then prints
   `⏸ Surgery paused — /exit; relaunch with l to continue.` That line lives in
   Franky's surgery prompt only. ADR 0067's injected ✅ line keeps meaning
   "bead closed". A paused surgery is an `in_progress` Surgery with no session.
   No label marks it.
2. **The running record is one `bd comments add` per session**, written at
   pause or end. It holds the changes made (short hash and subject per
   commit), the decisions and context the diff does not show, and what is next
   or still open. The problem statement (title plus the optional one-liner
   from filing) stays in the description, unchanged. `git log` on the branch
   is the authoritative list of changes. If a session crashed without writing
   its record, the next session rebuilds it from `git log`.
3. **Continuity comes from the prompt.** A surgery session first reads its
   bead (description and comments) and `git log` on `checkpoint/<id>`.
4. **The end comes only on the human's explicit word.** Franky never ends a
   surgery on his own judgment, and any other exit is a pause. Franky reads the
   end back in one line ("Ending: N commits → review"), with no y/n step,
   after checking that the tree is clean and the gates are green at HEAD.
5. **A CLI verb `mugicrew surgery end <id>` does the end in Go**, keeping
   spawn-before-close (ADR 0016 §3). It spawns the Review on the surgery
   Checkpoint if none exists, then closes the Surgery. Franky then prints the
   ✅ line. With zero commits on the branch, the verb closes both the Surgery
   (abandoned) and its empty Checkpoint, and spawns no Review.
6. **Commits** follow the Build convention: a conventional-commit message
   naming the Surgery bead, one per accepted change. Jinbe squashes at land.

## Considered Options

- **A "pause" exit that keeps the worktree and session id for `--resume`.**
  Rejected: it needs new wrapper plumbing and holds the branch while paused.
  Commit-per-change already makes a clean exit lossless.
- **Leaving the session alive (F12) as the pause.** Rejected as the mechanism:
  it holds the branch and dies on a reboot. It is still fine for short breaks.
- **`--append-notes`, or a record after every commit.** Rejected: ADR 0070
  does not name `--append-notes`, and per-commit entries would repeat
  `git log`.
- **The agent spawns the Review with `bd create`, or `hitl-run` spawns it
  after exit.** Rejected: the first puts graph logic in a prompt, and the
  Review would only publish as a Draft at exit. The second hides the
  orchestration in the wrapper.
- **A y/n confirm before the end.** Rejected: a one-line read-back is enough
  ceremony for a human who is already present.
