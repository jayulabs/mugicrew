# ADR 0029: event schema, with effort and session as two correlated layers

Status: Accepted
Date: 2026-09-29

Chart: Mugicrew session logs tab. Settles the grilling "Event schema
field set". Builds on ADR 0033 (history, not a view), ADR 0027 (one
slog JSONL file; `msg` is the event name) and ADR 0028 (rotation).

## Context

For a worktree effort, `dispatch.Dispatch` runs in the TUI or loop process and
sees only the `mugicrew reconcile <id>` child. The claude session inside it
(session id, result JSON, stderr) is visible only to that child's `Harness.Run`
caller. When the child is killed, it logs nothing. A single-layer schema either
loses the kill case (if it emits only from the session) or has to scrape the
child's output (if it emits only from Dispatch; rejected in
failed-session stderr-tail research §5).

## Decision

1. **Two layers.** `effort.*` events are emitted by `dispatch.Dispatch` (bead,
   crewmate, duration, the reconcile child's exit, the Go error chain).
   `session.*` events are emitted by the `Harness.Run` caller: inside the child
   for worktree efforts, in-process for RepoRoot efforts.
2. **Correlation by a minted effort id.** Dispatch mints a short random id,
   hands it to the child as env `MUGICREW_EFFORT=<id>`, and every event inside
   that effort (session, land, reconcile, claim) carries `effort=<id>`. Ops run
   outside an effort, such as a manual `mugicrew reconcile` or a TUI claim,
   carry no `effort`.
3. **Common attrs**, alongside slog's `time`/`level`/`msg`: `bead`, `parent`
   (the bead's direct Parent, which is what the Logs tab matches for Chart
   scope: `bead==X || parent==X`), `crewmate`, `effort` (when inside one),
   `pid`, and `proc` (the emitting verb: `tui` for the bare TUI, else the CLI
   verb as typed: `loop`/`reconcile`/`signal`, …).
   `err` is present on failures only, as the plain `err.Error()` string.
   Exception: a pause write's outcome is `error`, not `err` (ADR 0082 §2) —
   `effort.abandoned` when its `paused:abandoned` write failed, and the loop's
   `effort.paused` tally carrying the last failure.

   > **Amended (2026-09-29):** `proc` was the closed set
   > `tui`/`loop`/`reconcile`/`signal`; it is now the CLI verb as typed
   > (`main.go` passes `args[0]`), so a new verb such as `ps` needs no ADR edit.
   > An unknown verb errors out before emitting any event, so a typo never
   > becomes a `proc` value.

4. **Session payload:** `session` (a UUIDv4 pre-assigned via `--session-id`,
   already present on `session.started`), `exit` (int), `signal` (when killed),
   `dur_ms` (int), a `result` group (`subtype`, `terminal_reason`, `is_error`,
   `errors[]`, `result` truncated to 1 KiB), and `stderr` (the last 4 KiB,
   cut at a line boundary). Exit 0 with `is_error:true` is `session.failed`.
   Effort events carry `exit` and `dur_ms` of the reconcile child the same way.
5. **Catalogue** (`noun.verb`, with separate `.failed` names):

   | Event | Level | Extra attrs |
   |---|---|---|
   | `effort.started` / `.ended` | Info | `dur_ms`, `exit` on end |
   | `effort.failed` | Error | `exit`, `signal`, `dur_ms`, `err` |
   | `effort.busy` | Debug | `branch` |
   | `effort.held` | Warn | `branch`, `holder`, `prunable` (ADR 0054 foreign hold) |
   | `effort.paused` | Warn | `branch`, `holder`, `skips` (loop wrote `paused:foreign-hold`, ADR 0055) |
   | `effort.resumed` | Info | `branch` (`paused:foreign-hold` removed, ADR 0055) |
   | `effort.stale-base` | Warn | `branch`, `cut`, `container_tip` (a reused effort branch with its own commits sits on a base other than the container tip; `bead` names the id) |
   | `session.started` / `.ended` | Info | `session`; payload on end |
   | `session.failed` | Error | full payload (§4) |
| (cost attrs) | — | `cost_usd`, `api_ms`, `models{…}`, `cost_unpriced` on `session.ended`/`.failed` and `hitl.exited`/`.failed` (amendment) |
   | `hitl.launched` / `.exited` | Info | `session`; `exit` on exited |
   | `hitl.resumed` | Info | `session` |
   | `hitl.failed` | Error | `session`, `exit`, `signal`, `dur_ms`, `err` |
   | `hitl.continued` | Info | `session` (join to its `hitl.exited`), `continued_in`, `tail_ms`; `cost_known=true` + remainder `cost_usd`/`api_ms`/`models`, or `cost_known=false` + `reason` (`timeout`/`not_found`); written late by the detached `hitl-follow` (ADR 0254), never a session end |
   | `hitl.dropped` | Info | `session`, `state` (`alive`/`exited`), `err` |
   | `claim.taken` / `.released` | Info | `by` |
   | `claim.failed` | Error | `by`, `err` |
   | `draft.published` / `.deleted` | Info | `creator`, `ids` (ADR 0059 publish / cleanup); withdraw/cancel cleanup adds `target` |
   | `draft.obsoleted` | Info | `creator` (the retired child), `container`, `ids` (its non-Signal Drafts closed `obsolete:`, ADR 0124) |
   | `effort.stranded` | Warn | (common attrs); `branch`, `signal`, `container`, `landed`, `range` (a landed-refused child's commits carried as an offshoot Signal, ADR 0102) |
   | `draft.failed` | Error | `creator`, `ids`, `err` |
   | `sweep.straggler` | Warn | `checkpoint`, `ids` (loop-tick backstop force-closed a swept Checkpoint's late children, or superseded a live pivot whose every gate target is closed, ADR 0124) |
   | `reconcile.done` | Info | `branch` |
   | `reconcile.failed` | Error | `branch`, `err` |
   | `land.done` | Info | `branch`, `into`, `sha` |
   | `land.failed` | Error | `branch`, `into`, `err` |
   | `branch.deleted` | Info | `branch` (a landed, closed container's own branch, Spec FR30–32; withdraw and cancel cleanup widen it to any closed bead's effort branch and a withdrawn container's branch, adding `bead`, `target`, `repo`, and `unintegrated` (+`replace`) for `effort/*` or `ahead` for `chart/*`/`checkpoint/*`, ADR 0310) |
   | `branch.kept` | Warn | `branch`, `err` (that delete refused, e.g. a worktree holds it; the effort still succeeds); from withdraw/cancel cleanup also `target`, `repo` and either `holder` (a foreign or alive tree still holds it) or `err` (that tree's removal failed) |
   | `tree.removed` | Info | `target` (the withdrawn head or cancelled Initiative), `repo`, `tree`, `child`, `session`, `dirty` (changed files), `rebasing`, `prunable` (withdraw/cancel cleanup; keyed to the bead that owned the tree) |
   | `tree.kept` | Warn | `target`, `repo`, `tree`, `child`, and either `holder` + `foreign=true` (a foreign or alive hold, skipped) or `err` (a failed removal) |
   | `revert.done` | Info | `target`, `squash`, `into`, `sha` (ADR 0047 Rollback) |
   | `revert.conflict` | Warn | `target`, `squash`, `into`, `paths` (planned: ADR 0049 then fixes forward) |
   | `revert.failed` | Error | `target`, `squash`, `into`, `err` |
   | `handoff.done` | Info | `successor` (ADR 0048 Rollback → successor Checkpoint) |
   | `handoff.failed` | Error | `successor`, `err` |
   | `carry.done` | Info; Warn when `skipped` | `successor`, `moved`, `skipped` `[{bead, reason}]` (ADR 0085; none when nothing to carry) |
   | `carry.failed` | Error | `successor`, `moved` (landed before the failure), `skipped`, `err` |
   | `triage.fastlane` | Info | `spec`, `anchor` |
   | `triage.failed` | Error | `spec`, `err` |
   | `initiative.new` | Info | `kind` (`initiative`; a human started an Initiative from the Initiatives tab, ADR 0190) |
   | `initiative.adopt` | Info | `initiative`, `from` (a chain head re-parented into an Initiative, on the head) |
   | `initiative.adopt.refused` | Warn | `reason`, `blocking` (ids, a list) |
   | `initiative.release` | Info | `initiative` (a member chain head's parent cleared, on the head) |
   | `initiative.release.refused` | Warn | `reason`, `blocking` (ids, a list) |
   | `withdraw.done` | Info | on the chain head: `closed` (ids, leaves first, head last), `reason` (`withdrawn`); ADR 0310 |
   | `withdraw.refused` | Warn | `reason` (`in-flight effort` \| `ordered after the chain` \| `open Landing`; several join with `; `), `blocking` (ids, a list), `mr` when a refusal names one; nothing written |
   | `withdraw.failed` | Error | `step` (`read` \| `plan` \| `fence` \| `inventory` \| `close`), `err`, `closed` (on `close`: ids a re-read finds closed) |
   | `cancel.done` | Info | on the Initiative: `closed` (member chains, then the Initiative), `released` (heads), `reason` (`cancelled: <initiative>`) |
   | `cancel.refused` | Warn | as `withdraw.refused`, plus `release refused: Require Initiatives` with the locked heads in `blocking` for a `--release` naming a locked head |
   | `cancel.failed` | Error | as `withdraw.failed`, with `step` `release <head>` for a failed release; `released` (heads released before the failure) |
   | `adoption.skipped` | Warn | `source` (the discovered-from bead whose climb failed; absent when the Signal itself failed to read), `err` (Luffy's best-effort Adoption offer lookup failed; `bead` is the Signal) |
   | `loop.no_owner` | Warn | |
   | `loop.tick_failed` | Error | `err` |
   | `loop.started` | Info | `detached` (re-exec'd by `loop start`); `restarted=true` in a restart's new image (ADR 0062 §5) |
   | `loop.draining` | Info | `inflight`, `beads`; `restart_to` (rev) on a restart drain (ADR 0062 §8) |
   | `loop.restarting` | Info | `rev`, `store_path`, `dur_ms` (old image's uptime); written just before a restart's exec |
   | `loop.stopped` | Info | `dur_ms` (uptime) |
   | `loop.forced` | Warn | `inflight` |
   | `loop.failed` | Error | `err` (lock open / re-exec / fatal) |
   | `effort.abandoned` | Warn | (common attrs; claim not released, `proc` loop or tui); `paused` (did its `paused:abandoned` write land, ADR 0082), `error` when it didn't |
   | `effort.aborted` | Warn | (common attrs); `by` (cli or tui), `pids` (the liveness scan's matches, before the SIGTERM), `paused` (always true: logged only after the `paused:manual` write lands, ADR 0083) |
   | `recover.done` | Info | (common attrs); `verdict` (`dead`, `ambiguous-resolved` or `forced`), `rebase_aborted` (it aborted a rebase stopped in the tree), `tree` (the dropped effort tree, "" none), `discarded` (it held uncommitted files), `drafts` (Draft ids deleted), `unclaimed`, `lifted` (pause causes lifted), `remaining` (pause causes left: manual, integrate-conflict) (ADR 0127 §10) |
   | `recover.refused` | Warn | (common attrs); `reason` (the refusal and its way out) |
   | `recover.failed` | Error | (common attrs); `step` (check, fence, re-check, abort rebase, drop tree, delete Drafts, unclaim, lift <cause>), `error`; `paused:recovering` stays |
   | `build.started` | Info | `rev` (ADR 0062 §8) |
   | `build.done` | Info | `rev`, `store_path`, `dur_ms` |
   | `build.failed` | Error | `rev`, `dur_ms`, `err` (nix stderr, last 4 KiB at a line boundary; a failed eval too) |

6. **No schema version.** The reader skips unparseable lines and ignores
   unknown attrs. Add `v` when a breaking change actually lands.

> **Amended (2026-10-03):** for a HITL session the TUI runs under
> tmux (ADRs 0036–0039), the in-pane `mugicrew hitl-run` wrapper (ADR 0037)
> emits `hitl.launched` (or `hitl.resumed` when started with `--resume`) on
> start, and on claude's exit either `hitl.exited` (rc=0, worktree torn down)
> or `hitl.failed` (non-zero or a signal, worktree kept), with `proc=hitl-run`
> and `dur_ms`. The TUI emits only `hitl.dropped` (on `x`). Detach and attach
> emit nothing: they are navigation, not lifecycle. When `kill-session` drops an
> alive session, the wrapper's own `hitl.failed signal=hangup` is best-effort.
> A killed tmux server or a reboot emits no exit event. The
> un-tmuxed CLI launch keeps emitting `hitl.launched`/`.exited` itself.

> **Amended (2026-10-04):** loop lifecycle events (ADR 0050's
> off → on → draining → off). The loop process writes all of them itself with
> `proc=loop`. The `loop start|stop|status` verbs write nothing, except the
> `loop.failed` of a `loop start` that cannot re-exec.
> `loop.started` is written once the single-loop lock is held. `detached` says
> whether the loop was re-exec'd by `loop start`. On the first stop signal the
> loop writes `loop.draining` with the in-flight count and beads, and after a
> clean drain it writes `loop.stopped` (`dur_ms` = uptime). A force stop is
> delivered as a **second stop signal** (`loop stop --force` sends SIGTERM
> twice; a second Ctrl-C works the same way), never SIGKILL, so the loop can
> still log it. It writes one `effort.abandoned` per in-flight effort, with that
> effort's `effort`/`bead`/`crewmate`, meaning its claim was **not** released,
> then `loop.forced`, then exits. A worktree effort that keeps running may go on
> writing `reconcile.*`/`session.*` under the same effort id after that.
> `loop.failed` covers a start that cannot open the lock or re-exec, and any
> fatal loop error. A start refused because the lock is held (on or draining)
> is **not** logged: nothing changed state, and the CLI or TUI tells the human
> directly. A SIGKILL or crash still writes nothing, so its efforts stay
> dangling (stranded-claim recovery is the Chopper Signal, ADR 0050 §6).

> **Amended (2026-10-04):** the TUI also writes
> `effort.abandoned`, with `proc=tui`. When the human confirms quitting with
> run-now (`t`) efforts in flight (ADR 0051, as amended), it writes one per
> effort with that effort's `effort`/`bead`/`crewmate`, meaning the claim was
> not released, then exits. Writing is best-effort. There is no `tui.quit`
> event.

> **Amended (2026-10-04):** `effort.busy` now means own
> contention only: a refused cut whose holder an in-flight effort or an alive
> session accounts for. Every other refused cut is a foreign hold, logged as
> `effort.held` at Warn (ADR 0054). `effort.suppressed`/`.unsuppressed` are
> reserved for the loop's suppression of a foreign-held bead.

> **Amended (2026-10-04):** the reserved
> `effort.suppressed`/`.unsuppressed` are renamed `effort.paused` (Warn
> `{branch, holder, skips}`) and `effort.resumed` (Info `{branch}`). They fire
> when the loop writes or removes `paused:foreign-hold` (ADR 0055).

> **Amended (2026-10-05):** session cost attrs (bead telemetry Chart; sourced from
> research on headless Claude result cost/usage fields and HITL session cost from transcript state).
> - **Where.** Top-level attrs (not inside `result`) on `session.ended`/`.failed`
>   (written by `sessionHarness.Run`) and on `hitl.exited`/`.failed` (written by
>   hitl-run, and by the un-tmuxed CLI launch). Same names on both, so a query
>   reads one path. No new event (no `hitl.usage`). `effort.*` gets no cost: the
>   query sums sessions by `effort`; a rollup on the effort would double-count
>   and need child→parent plumbing.
> - **Attrs.** `cost_usd` (float, client-side estimate for the whole session
>   tree incl. subagents), `api_ms` (int), and `models`, a group keyed by model
>   id, each `{cost_usd, in, out, cache_read, cache_write}`. "Model" is the
>   `models` keys: there is no single `model` attr; `--by model` splits a session
>   per key. Not recorded: `turns` (unreliable both ways), top-level `usage`
>   (last step only), thinking tokens, `costBasis`/`canonicalModel`.
> - **Sources.** Headless: the `-p --output-format json` result
>   (`total_cost_usd`, `duration_api_ms`, `modelUsage`). HITL: the last
>   `cost-state` line of the session transcript, read after the agent exits by
>   one helper behind the harness that both HITL end-event writers call.
> - **Resumed HITL** records the **delta** against the previous `cost-state`
>   (total and per model), so a query just sums end events; with no previous
>   record it records the whole total.
> - **Unknown is not zero.** With no data (headless killed by SIGTERM/SIGKILL
>   and so no result; HITL with no transcript, no `cost-state`, or SIGKILL) the
>   cost attrs are omitted, never 0. A failed headless session reporting
>   `cost_usd: 0` (crash) is treated as no data. Unpriced models (`costBasis:
>   unknown` / `hasUnknownModelCost`) keep `cost_usd` and add
>   `cost_unpriced=true`. Recovering a SIGTERM-killed headless session's cost
>   from its transcript is deliberately skipped (mugicrew never kills one).
> - **Known undercount.** `/clear` inside a HITL session moves later spend to a
>   new session id mugicrew never recorded; it is not chased.
>
> **Reader (2026-10-05):** `mugicrew stats` (ADR 0061) only reads
> these attrs and reports them. It never feeds dispatch.

`triage.fastlane` (ADR 0035) names the triage *outcome* rather than a verb —
a deliberate exception kept for its ADR; read it as "triage took the fast lane".

This catalogue is the app's logging convention: a new event picks a
`noun.verb` name, sets the common attrs, and is added to this table.

## Considered and rejected

- **Session-only events:** a killed child leaves no trace at all.
- **Dispatch-only events:** would have to plumb the session result through the
  child's stdout.
- **Claude session id as the effort id:** Dispatch doesn't know it, and an
  effort may run more than one session.
- **Resolved `chart` ancestor attr:** requires a tree walk and serves only
  Charts; one-level `parent` covers today's containers.
- **Go error chain as `[]string`:** `%w` chains already read as `a: b: c`.

## Consequences

- A Logs tab row can group an effort's events by `effort`, and the kill case
  still shows `effort.failed` from the parent (Go reports a signalled child as
  `exit=-1 signal=killed`, not the shell's 137).
  > **Amended (2026-09-29):** this bullet read `effort.failed
  > exit=137`; 137 is the shell's encoding, and Go's `ExitCode()` reports a
  > signalled child as `-1` with the signal in `signal`.
- `Harness.Run` must return the session id, exit, result summary and stderr
  tail on failure as well as success (the research §5 changes).
- Grandchild beads (if any ever exist) are not caught by Chart scope.
