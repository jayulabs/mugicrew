# ADR 0104: mugicrew integrate-conflict pause — only a successful `mugicrew integrate` lifts it; `t`/`l` recut on the effort branch, and the TUI shows live effort refs

Status: Accepted
Date: 2026-10-08
Amends: ADR 0078 §5/§6 (events, precedence), ADR 0057 §1/§5 (colour, `t`), ADR 0082 §7 (precedence), ADR 0102 §3/§6.

## Context

ADR 0102 §6 makes the Go backstop the only writer of
`paused:integrate-conflict`. When it hits a conflict it runs `rebase --abort`,
reopens the bead if it was closed, and keeps `effort/<id>` and its tree for the
human. ADR 0102 left the rest open: how the pause is lifted, how it ranks
against other causes, and how the TUI and event log show it. §9 also lets a
closed child with a live effort ref hold its container open, and the TUI can't
see that today. Grilling settled this.

## Decision

1. **Only a successful integrate lifts the pause.** When `mugicrew integrate`
   exits 0 for a bead carrying `paused:integrate-conflict`, it removes the
   label and logs `effort.resumed {cause: integrate-conflict}`. This happens
   whichever path ran the verb: an AFK retry, a relaunched HITL session, or
   the human by hand in the kept tree. One verb writes the pause (through the
   backstop) and the same verb lifts it. `bd label remove` stays the human's
   escape hatch. No `runNowCmd` or session-exit hook lifts it, because a
   "clean" run that never integrated must not lift it.
2. **The verb never closes the bead.** ADR 0102 rejected Go closing leaf
   beads. Retry and HITL agents close as their usual last step. When the
   human integrates by hand, the verb prints
   `integrated; bead still open: bd close <id>`.
3. **Retry keys.**
   - **AFK leaf:** `t` is allowed and labelled `retry`. Before it cuts, it
     drops the kept tree if the tree is clean. If the tree is dirty, it
     refuses with a hint naming the tree. The retry reuses `effort/<id>`
     (roll forward), and the agent's normal integrate step hits the conflict
     and resolves it (ADR 0102 §5). The prompt gets no special conflict text,
     because the verb's exit-2 output tells the agent what to do.
   - **HITL leaf:** `t` stays hidden. `l` launches, and the kept tree goes
     through the existing orphan confirm (ADR 0041 §3, `y` drop it and
     launch).
4. **Precedence: manual > abandoned > integrate-conflict > failing >
   foreign-hold.** Abandoned flags a possible live writer. Integrate-conflict
   has finished work stranded, which outranks a retry decision.
5. **The failure counter ignores it.** An effort that ends in a backstop
   conflict neither counts as failed nor resets the count (ADR 0078 §2). The
   work succeeded and only the integrate did not. The pause already stops
   re-dispatch.
6. **`‖` is cyan (44)** for integrate-conflict. The row's colour is the only
   place a cause shows (ADR 0057 §1), and the warm colours are all taken. The
   detail pane's pause line names the cause, as for every cause (ADR 0057
   amendment).
7. **Effort refs in the TUI.** The refresh adds one
   `git for-each-ref refs/heads/effort/` read, not render (ADR 0101).
   - The detail pane shows `effort  effort/<id>` when the ref exists. It
     shows no ahead count, because that would cost one call per ref.
   - There is no row marker. A closed bead with a live ref is usually only
     seconds from the backstop, and a conflict reopens it, so it shows `‖`
     anyway.
   - `runNowLabel`'s drained-Chart check and the "Chart not drained" hint
     count a live effort ref as an open child (ADR 0102 §9). That way the
     footer doesn't offer `spec` when the press-time gate would refuse.
8. **Events.** `integrate.*` events use the `Record(done, failed)` shape of
   `land.done`/`land.failed`:
   - `integrate.done {bead, container, branch, sha, retries, by}`, where `by`
     is `agent`, `backstop` or `human`. Nothing ahead logs `sha: ""`.
   - `integrate.conflict` at Warn, with `reason: rebase|held|landed`.
   - `integrate.failed` for transient errors. Spent CAS retries log
     `reason: contended, retries: 3`.

   The backstop's pause logs `effort.paused {cause: integrate-conflict,
   branch, tree}` at Warn (the ADR 0078 §5 shape). The lift logs
   `effort.resumed {cause: integrate-conflict}`.
9. **ADR 0102 §3's refusals pause too.** A container checked out (`held`) or
   already landed (`landed`) both need a human. The backstop writes the same
   `paused:integrate-conflict`, with the refusal in the event's `reason`. A
   second cause would add no behaviour.

## Considered and rejected

- **Each path lifts it on its own** (`runNowCmd` on a clean return, HITL clean
  exit, the human after a manual integrate). It would lift the pause after a
  run that never integrated.
- **The lifting integrate also closes the bead.** It goes against ADR 0102's
  rejection of Go closing leaf beads, and it is only needed on the manual
  path.
- **Re-entering the kept tree on retry.** Dispatch would have to learn to
  adopt a tree, to save recutting a clean checkout of the same branch.
- **Sharing failing's red.** The row could no longer tell a stranded
  integrate from a failing handler.
- **A row marker for closed beads with a live effort ref.** The window is
  seconds long, and a conflict already reopens the bead and shows `‖`.
- **Separate `retry-exhausted` and `held`/`landed` event names or causes.** A
  `reason` field reads the same in the Logs tab with fewer names.

## Consequences

- `bd` gains `PausedIntegrateConflict`. `PauseRank` puts it after
  `abandoned`, and `pauseSty` gains cyan 44.
- `runNowLabel` returns `retry` on an AFK leaf carrying it, and `t`'s press
  path drops a clean kept tree before it cuts. `pauseRefusesRunNow` does not
  refuse it.
- The integrate verb reads its bead's labels, so that it can lift the pause.
- The refresh adds one git read, and the drained-Chart affordance and its
  hint read the effort refs it returns.
