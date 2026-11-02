# A Build behind mainline is held on a human-requested rebase: a computed hold before dispatch, a `behind-mainline` pause after

Amends: ADR 0262 (a-requested-rebase-is-a-human-filed-conflict-variant…) "Blocks no sibling"; ADR 0248 (not-built-pause-ranks-after-integrate-conflict…) §2.

A Checkpoint branch cut before a needed change reached mainline lacks it until someone rebases. The loop dispatched such Builds anyway: the crewmate refused, the Build paused `paused:not-built`, and a human requested a rebase, added an edge and lifted the label by hand. Settled by the Survey of the Chart "A Build whose Checkpoint branch lacks mainline code it needs waits on a human-requested rebase":

- **Prevention is a computed verdict cause, not a label.** A Build has a closed `blocks` blocker outside its own Checkpoint. That blocker's container has a land trailer on mainline, and the landed commit is not an ancestor of `checkpoint/<id>`. Such a Build gets verdict cause `behind-mainline`, held by that blocker. It ranks right after `blocked`. It releases on its own once the tip contains the commit, whichever rewrite put it there. Unforced run-now refuses it, and `Force` waives it as it waives `blocked`.
- **Recovery is a reported, verified pause.** The Build prompt gains a distinct comment, `Behind mainline: <what's missing>`. The backstop maps it to `paused:behind-mainline` only after it checks that mainline has commits the Checkpoint lacks. When that check fails it falls back to `paused:not-built`. Pause precedence: … integrate-conflict > not-built > **behind-mainline** > failing …. Its `‖` is gold, like not-built.
- **A requested rebase holds the Builds that asked for it.** Filing a requested rebase on a Checkpoint adds a `blocks` edge from the rebase bead to each member paused `behind-mainline`, and lifts that pause. Its close releases them through the ordinary edge path. Other siblings stay unblocked, so 0262's reason (don't stall live siblings) holds. A Build that reports behind-mainline while a requested rebase or a rebase-conflict / number-clash Conflict is already open on its Checkpoint is edged to that bead at once and not paused. A moot close releases the Builds. A `superseded:` close (ADR 0261 (a-requested-rebase-defers-to-an-open-conflict…)) moves the edges to the superseding Conflict.
- **Mugicrew suggests and the human files.** A `behind-mainline` row's cause hint and detail pane say that the Checkpoint is behind mainline and that `r` requests a rebase. `r` on that row files it. No Signal, no agent-filed rebase.

## Considered options

- **A pause label for prevention.** Rejected: the hold is a fact of git and the graph. A label can go stale and needs its own lifter.
- **Keep the recovery label until the rebase closes.** Rejected: the row would read "paused" while it is waiting on a named bead. An edge says so and reuses release.
- **Pause anew when a rebase is already open.** Rejected: re-filing prints "already requested" (ADR 0261) and the Build would be stuck.
- **An offshoot Signal per occurrence.** Rejected: too noisy for a one-press remedy.
