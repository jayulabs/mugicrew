# ADR 0254: a setsid follower writes hitl.continued after the pane dies

Status: Accepted
Date: 2026-10-16

Chart: Record a HITL session's continued-in destination and count its Session
cost and Agent time. Settles the prototype "Whether a setsid follower re-exec'd
by hitl-run survives the pane kill and writes a late end event" (throwaway on
`refs/mugicrew/prototypes/mc-rvvq`). Builds on ADR 0029 (mugicrew-event-schema),
ADR 0028 (mugicrew-event-log-rotation), ADR 0061 (mugicrew-stats-verb).

## Context

At charting it was decided that hitl.exited records `continued_in` at once,
and that a detached follower writes the destination's remainder later. That
remainder is ready only when the destination's cost-state lands, usually 1–3 min
after hitl.exited. hitl-run runs inside the tmux pane it kills as its last act
(`endSession`), so anything it starts shares that pane's fate unless it is
detached.

## Decision

1. **Spawn.** Just before it records hitl.exited, hitl-run re-execs itself as
   the follower, in the same shape as `spawnDetached`: `Setsid`, stdin
   `/dev/null`, stdout/stderr on a file and never on the pane's pty. hitl-run
   calls `Release` and never `Wait`s. The prototype ran this for real. The
   follower survived kill-session, a tmux kill-server, its cwd being removed,
   and an events.jsonl rotation, and wrote its event every time. A child left
   on the pane's pty died on the hangup and wrote nothing.
2. **What the follower is handed.** On its argv: the bead attrs, `session`
   (the source id), `continued_in`, the source's last cost-state (its
   baseline), the hitl.exited time, and the absolute event-log path that
   hitl-run already resolved. Its environment is hitl-run's after
   `applyEnvFile`, which carries `CLAUDE_CONFIG_DIR` (to find the
   destination's transcript by id) and `MUGICREW_EFFORT`. It reads nothing from
   tmux or from its cwd, and it writes through `eventlog.New(path)`, not
   `eventlog.Open`.
3. **The second end event, `hitl.continued`.** It carries the bead attrs,
   `session` (the join key to its hitl.exited), and `continued_in`. Its cost is
   the remainder: the destination's final cost-state minus the source's last
   cost-state, which hitl.exited already counted once the meter is fixed. It
   also carries `tail_ms`: the destination's last timestamp minus hitl.exited,
   clamped ≥ 0. stats.Sum adds its cost and `tail_ms` to the same HITL session
   and never counts it as a session. If the destination has no cost-state, it
   is `cost_known=false` with a `reason` (timeout, killed, not found), shown as
   Partial cost. A destination that hands off again is followed down the chain
   before the follower writes.
4. **Rotation needs nothing.** The follower holds no log handle (ADR 0028 §2),
   so a rotation in between only puts the pair on either side of `.1`. Readers
   already read `Files()` in order.

How long the follower waits before giving up on a cost-state is left to the
sibling defog bead on how long a destination lives.

## Considered and rejected

- **No setsid, inheriting the pane's stdio:** it dies on the pane's hangup.
- **Follower resolves the log from its cwd (`eventlog.Open`):** its cwd may be
  a worktree the clean exit already removed. The path costs nothing to hand over.
