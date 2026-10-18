# Event log vs Landing body line as the store for an MR sub-state

Status: Research (input to the grilling "Store for an MR sub-state: event, Landing body line, or both", mc-t72m.19)
Date: 2026-10-16
Bead: mc-t72m.19.3

## Purpose

Chart mc-t72m.19 has Jinbe record an open Merge Request's sub-state, derived
from `detailed_merge_status` (word map in
`docs/research/gitlab-detailed-merge-status-to-mr-qualifier-words.md`). The
Execute Rollup then shows it as a qualifier on `landing`. The Rollup is I/O-free
over the bead snapshot and the in-memory event log (ADR 0198
(tui-v2-execute-checkpoints-section…) Context). So the sub-state has to be
recorded somewhere it can read. This doc gathers the facts for choosing between
an event (e.g. `land.mr_status`), a Landing body line (like `Checked:`), or
both. It covers four things: how the TUI loads the log, how the Rollup reads
Landings today, what each write costs and how it dedupes, and what must survive
across Landings.

**M** = `apps/mugicrew/…` at this branch's HEAD (`2df29282`).

## 1. How the TUI's in-memory event log is loaded

- **One shared file, two generations.** Every process appends to
  `$(git-common-dir)/mugicrew/events.jsonl` (M `eventlog/eventlog.go:1-4,73-79`).
  At ≥ 10 MiB the live file is renamed to the single `.1` backup before the next
  write (M `eventlog/eventlog.go:35-37,108-110`; ADR 0028
  (mugicrew-event-log-rotation) §1–2).
- **It replays from disk on TUI start.** The model starts with a zero
  `logSig`/`logOff`. `loadLogs` calls `tailLog` = `eventlog.ReadFrom` (M
  `tui/live.go:56,280-301`). `ReadFrom` treats a zero `prev.Sig` as a full read:
  it parses all of `.1` and then the live file (M `eventlog/read.go:103-129`).
  Later ticks (every 3 s, M `tui/live.go:35`) read only the bytes appended since
  the last offset. A rotation or truncation forces a full re-read (M
  `eventlog/read.go:109-112`).
- **How far back it goes.** Everything in `.1` + live, so ≤ ~20 MiB. ADR 0028
  Consequences put that at "roughly 3–6 months at current volume". Older
  history is gone. A known race can also clobber `.1` (ADR 0028 Consequences;
  M `eventlog/eventlog.go:105-107`).
- **No trimming in memory.** An extend appends to `m.logs`, and a full read
  replaces it (M `tui/live.go:408-412`). The only limit is rotation.
- **Rollup index.** After each change the TUI rebuilds `m.landEv =
  landEvents(m.logs)` (M `tui/live.go:414`). For each bead id it keeps **only
  the latest msg** of any `land.*`, `gate.*`, `effort.failed` or
  `effort.paused` event (M `tui/live.go:174-182`). The type is
  `map[string]string`, bead → msg, with no attrs (M `tui/shell.go:245`).
- **Unavailable log.** If the log path cannot be resolved, writes are discarded
  with a one-time warning (M `eventlog/eventlog.go:100-103,124-134`), and the
  TUI shows no events.

## 2. How the Rollup reads Landings and events today

- **Landing state comes from the snapshot.** It reads status, `CloseReason`
  (`landed: `, `obsolete:`) and `paused:*` labels (M `tui/rollup.go:53-70,151-155`).
  The first open Landing drives `landing` (M `tui/rollup.go:60-62,114`).
- **The snapshot already includes bead bodies, and the Rollup parses body
  lines.** An Approval's `Commit:` pin is read I/O-free with
  `git.ParseCommit(a.Description)` (M `tui/rollup.go:34-37,166`). The snapshot
  is `bd list … --json` (M `bd/bd.go:187-199`). A Landing body line such as
  `git.ParseLine(l.Description, "MR status:")` (M `git/git.go:629-637` is the
  `MR:`/`Checked:` reader) would be read the same way. The Rollup reads no
  Landing body line today.
- **The `retrying` qualifier is the only event read.** It applies only when the
  word is `landing`, and reads `g.landEv[openL.ID]` through `landRetrying`
  (`land.failed`, `gate.infra`, `gate.pending`, `effort.failed`) (M
  `tui/rollup.go:171-187`). The Chart status word reads it the same way (M
  `tui/chartword.go:193`). ADR 0198 §2 defines `retrying` as "its latest
  land/gate event is a failure or infra retry".
- **Consequence for an event store.** `landEv` keeps one msg per bead, the
  latest. A `land.mr_status` event emitted after a `land.failed` would mask it,
  and `retrying` would drop. Also, a qualifier word needs the status value (an
  attr), which `landEv` does not keep. So the event option needs either a
  `landEvents` filter that skips `land.mr_status` plus a second bead →
  status index, or a richer index value.
- **The snapshot wins.** The Rollup reads events only for the open Landing in
  the snapshot (comment at M `tui/rollup.go:171`). Events from a closed Landing
  are never read.
- **`d` shows the Landing body** in the bead detail (M
  `tui/exectabdetail.go:205-206`). A body line would appear there with no extra
  code.

## 3. Write cost per change, and dedupe

**Tick and read context.** The loop ticks every 15 s (M `loop/loop.go:50`; ADR
0185 (an-open-merge-request-outlives…) §8). A Landing is not research or
charting, so `Dispatch` runs `crew.Run` in-process in the loop's process (M
`dispatch/dispatch.go:270-275,634`). `Jinbe.Run` starts with a fresh
`showBead` (M `jinbe/jinbe.go:56-68`), so `reconcileLanding` holds the
Landing's current body with no extra I/O. `mirrorMR` already makes the one
MR GET per tick (M `jinbe/jinbe.go:429-437`), and that response carries
`detailed_merge_status` (gitlab-detailed-merge-status-to-mr-qualifier-words.md §5). The pending sub-states are read and
then discarded today (M `jinbe/jinbe.go:474-476`).

**Landing body line.**

- A write is one `bd update <id> --description <body>` subprocess (M
  `bd/bd.go:446-457`). Jinbe's seam is `updateBody` (M `jinbe/jinbe.go:86`).
  The body is rebuilt with `setBodyLine`, which deletes every line with the
  prefix and appends the new one at the end (M `jinbe/jinbe.go:570-576`). The
  line therefore moves to the bottom of the body on each write. `recordChecked` is the
  model to copy (M `jinbe/jinbe.go:531-539`).
- **Dedupe is free and durable.** Before writing, compare against
  `l.Description`, which is already in hand. `checkedBase(l.Description) !=
  base` gates `Checked:` (M `jinbe/jinbe.go:501,526-529`), and
  `hasBodyLine`/`heldPipeline` gate `Held:` (M `jinbe/jinbe.go:550-564`). This
  costs zero bd writes on an unchanged tick and one bd write per change. A
  loop restart has no effect because the key is in the graph.
- Write failures surface: `updateBody` errors are returned, which fails the
  tick's reconcile, as `recordChecked` does at M `jinbe/jinbe.go:534-536`.
- Body lines are graph state (ADR 0033 (mugicrew-event-log-is-history…) §1
  calls the log the non-graph kind). Every other Jinbe Landing line
  (`MR:`, `Checked:`, `Held:`, `Retried:`) is read back by Jinbe for a
  decision (M `jinbe/jinbe.go:430,501,463,545-547`). A display-only line would
  be the first of its kind. Chart Notes already settle that nothing decides on
  the sub-state.

**Event.**

- An emit is one stat, an `O_APPEND` open, one write and a close (M
  `eventlog/eventlog.go:100-121`). It never fails the caller (M
  `eventlog/eventlog.go:13-14`). It is roughly free, and a write is never
  observed to fail.
- **Dedupe cannot read the log.** ADR 0033 §1 and its Consequences: "No
  crewmate, the loop, or a future recovery path may branch on event-log
  contents". So "emit only on change" needs a key held elsewhere:
  - **In-memory map keyed by Landing id.** The precedent is `rateLimited`
    (`sync.Map`, logged once per Landing, pruned of closed Landings on each new
    429, M `jinbe/jinbe.go:301-312,405-421`). It lives only as long as the loop
    process, so each loop restart re-emits the current status once per open
    MR. That is harmless. The loop is also not the only host. A Landing is an
    AFK leaf (M `registry/registry.go:57`; `loop.IsAFKLeaf`, M
    `loop/loop.go:1007-1010`), so the TUI's run-now offers `run` on it (M
    `tui/graphview.go:860-868`). `runNowCmd` then calls `dispatch.Dispatch`
    inside the TUI process (M `tui/actions_io.go:366-370`, `tui/live.go:74`),
    and the dispatch falls through to in-process `crew.Run` (M
    `dispatch/dispatch.go:270-275`). A TUI run-now therefore has its own empty
    map and re-emits once. That is also harmless, but it means the map is never
    a single source of truth.
  - **The body line as the key**, which is the "both" option below.
- **Without dedupe** it is one event per open MR per tick: 86400 / 15 = 5760
  events/day. At ADR 0028's "~300 B per event" that is ≈ 1.7 MB/day per open
  MR, so ≈ 12 days to fill the 20 MiB window per MR. ADR 0028 estimates 3–6
  months at current volume. There is a precedent for per-tick emits:
  `land.stalled` is emitted on every tick that sees the stalled pipeline, with
  no dedupe (M `jinbe/jinbe.go:471`).
- **Change-only plus rotation is a correctness hole for display.** A
  change-only event for an MR that sits in `awaiting merge` for longer than the
  rotation window rotates out of `.1`. The TUI then shows no qualifier while
  the state is unchanged (§1). This also happens when the TUI's full read hits
  a clobbered `.1` (ADR 0028 known race). A per-tick emit avoids it but costs
  the volume above.

**Both.** Jinbe writes the body line on change (the store and dedupe key), and
in the same branch emits `land.mr_status` with the old and new words as
history for the Logs tab. That is one bd write plus one append per change,
dedupe survives restarts, and nothing new is needed in memory. The Rollup reads
the body line. `landEvents` must still skip `land.mr_status`, or the `retrying`
masking in §2 applies.

## 4. Must the record survive Landing close, or a new Landing on the same MR?

- **Survival past close is not needed.** The Rollup shows qualifiers only when
  the word is `landing`, which needs an open Landing (M
  `tui/rollup.go:60-62,114,171`). A closed Landing's word is `landed` or one of
  the others. Its body is kept anyway, since closing does not erase it.
- **A new Landing on the same MR starts blank.** ADR 0185 §1, §3 say one MR per
  container branch, reused across Landings, and each Landing is one attempt at
  one pin (ADR 0185 Context, citing ADR 0145). A new Landing's body is just
  `Commit: <pin>` (Checkpoint: M `jinbe/jinbe.go:245`; Chart: M
  `nami/nami.go:641`). Its first tick has no `MR:` line, so `mirrorMR` returns
  `false` (M `jinbe/jinbe.go:430-433`). The land leg pushes and re-arms,
  returns `LandPending`, and `recordMR` appends `MR: !N` (M
  `jinbe/jinbe.go:381-386,727-732`). The mirror, and with it the first
  sub-state record, comes on the next tick, so expect about one or two 15 s
  ticks with no qualifier.
- **Should the old status carry over?** No. A new Landing exists because the
  pin moved (`obsolete: branch advanced` or `obsolete: conflict`, ADR 0185 §2),
  and the push restarts the pipeline and recomputes mergeability, so the old
  status is stale. A body line naturally starts fresh. An event keyed by bead
  id also starts fresh, because the Rollup looks up `landEv[openL.ID]` (M
  `tui/rollup.go:172`). Keying the event lookup by MR iid would carry the stale
  word over, which is not wanted.
- **Forge-side close / trailerless merge** close the Landing or hold the
  container on a Task (ADR 0185 §4–5; M `jinbe/jinbe.go:438-452,478-488`).
  The word leaves `landing` or a hold word takes over, so the sub-state is not
  read.

## 5. Trade-off summary

| | Event only | Body line only | Both |
|---|---|---|---|
| Rollup read | needs new attr index + `landEvents` filter (§2) | `ParseLine` on snapshot body, like the Approval pin (M `tui/rollup.go:35`) | body line |
| Write per change | 1 append | 1 `bd update` | 1 `bd update` + 1 append |
| Dedupe key | in-memory map (lost on loop restart → 1 re-emit) or none (5760/day/MR) | the body itself, durable | the body itself |
| Survives rotation / lost log | no (change-only goes blank after ~months) | yes (graph) | yes |
| History / timestamp of change | yes, in the Logs tab | no (unless the line carries a time) | yes |
| Shown in `d` | needs code | free (body in detail, M `tui/exectabdetail.go:205`) | free |
| New Landing on same MR | fresh (keyed by Landing id) | fresh (new body) | fresh |
| Precedent | `retrying` (ADR 0198 §2), `rateLimited` once-per-Landing | `Checked:`/`Held:` set-on-change | — |

The facts favour the body line as the store, with an event on change as
optional history. That gives display that survives rotation, restart and an
unavailable log, at one bd write per change. Event-only is cheaper per write
but needs a new index, a `retrying` masking fix, and an in-memory dedupe that
still loses the qualifier once its event rotates out.

## Open questions / gaps

- **Staleness** (Chart Not-yet-specified) is untouched by either store. A body
  line or change-only event records when the status changed, not when it was
  last seen. Showing freshness needs a per-tick signal: `land.rate_limited`
  (M `jinbe/jinbe.go:309`), loop status, or a per-tick timestamp write (which
  costs a bd write per tick).
- **Line format not chosen.** Is it the raw `detailed_merge_status` (e.g.
  `MR status: need_rebase`, so the word map lives in the TUI) or the
  qualifier word? Storing the raw value keeps the 19.2 map changeable without
  rewriting bodies.
- **Unmapped/transient statuses.** Should Jinbe write the line for every
  status change, including ones that show no qualifier (`checking`,
  `unchecked`)? Writing them costs bd writes while GitLab rechecks. Skipping
  them leaves a stale mapped word in place. Not measured how often
  `checking`↔`mergeable` flaps on gitlab.com.
- **`main` has moved past the Chart base.** `main` (`39574ad7`) carries five
  landed Checkpoints (`git log HEAD..main -- apps/mugicrew/jinbe`) that shift
  every `jinbe/jinbe.go` cite here by about +21 lines (`mirrorMR` 429→450,
  `setBodyLine` 572→613). The findings hold: `mirrorMR` still reads
  `detailed_merge_status` and drops it, and `updateBody = bd.Update`
  (`main:jinbe/jinbe.go:86`). Two helpers changed. `appendBodyLine` moved to
  `bd.AppendBodyLine` (`main:bd/bd.go:674`). A new `retryPipeline`
  (`main:jinbe/jinbe.go:577`) records the event, then writes the body line
  and refreshes the in-hand `Description`. It is the nearest template for an
  event-plus-body-line write. The Spec should cite `main` once the Chart
  rebases.
- **bd write side effects** of one more `--description` update per change (Dolt
  commit size, `updated_at` churn visible in other views) were not measured.
