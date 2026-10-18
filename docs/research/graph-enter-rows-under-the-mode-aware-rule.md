# What Graph's enter does on each row under the mode-aware rule

Status: Research (input to the Mode-aware enter Spec and its prototype, mc-lu2x.12)
Date: 2026-10-13
Bead: mc-lu2x.12.2

## Purpose

The Chart settled one rule for v2: `enter` launches or attaches a HITL bead
and runs an AFK bead now, including spec, land and retry. Graph asks y/n on a
blocked or paused HITL bead, and `t` is dropped. This doc lists, for every kind
of Graph row:

- what `enter` and `t` do today
- what `enter` does under the rule
- the hints label, the status-line refusal and the detail pane's line

It also lists where today's code diverges, the `?` key changes, and the test
changes the rule implies. All paths are relative to `apps/mugicrew/`.

## 1. Today's two paths

**enter (and `b`)** run `actionKey` → `launchAs` (`v2/actions.go:47-50`, `:81-119`).

- `launchWhy` gates it (`v2/graphtab.go:57-69`). In order:
  1. `busyWhy`: a write in flight, a launch in flight, or a session closing (`v2/graphtab.go:42-52`).
  2. Any session (alive or exited) passes, whatever the bead.
  3. A closed bead is refused.
  4. A non-HITL bead or container is refused, except an Initiative. The message is "is not a launchable HITL bead (AFK/container — use edit to intervene)" (`v2/graphtab.go:65-66`).
- With a session, `enter` attaches an alive one or resumes an exited one, with no blocked confirm (`v2/actions.go:86-105`).
- Otherwise a blocked bead gets the "Launch blocked bead?" y/n (`v2/actions.go:106-117`). Everything else launches at once (`v2/actions.go:118`).
- **There is no pause arm anywhere in `launchWhy` or `launchAs`.** A paused HITL bead, by its own pause or an inherited one, launches with no dialog.

**t** runs `actionKey` → `runNow` (`v2/actions.go:51-52`, `:465-482`).

- An Initiative is refused (`:466-468`).
- `g.blocked(&b) && b.Status == "open"` opens "Run blocked bead now?" (`:470-480`). This happens **before** any busy, pause or kind check.
- Otherwise `runNowGo` runs. It goes through `startWrite`, which refuses only a pending write (`v2/live.go:361-372`, `:386-392`). Then it marks the row "dispatching…" and calls `claimRunNow` (`v2/actions.go:486-500`).
- The press-time gate is `runNowEligible` (`v2/actions_io.go:266-282`): no refusing pause, in the `bd ready` frontier, and one of three cases:
  - an AFK leaf
  - a drained Chart
  - a land-ready Checkpoint
- A refusal is worded by `runNowHint` (`v2/actions_io.go:301-328`).
- `bd ready` "Excludes in_progress, blocked, deferred, and hooked issues" (`bd ready --help`). So any in_progress bead reaches the "blocked / not ready" arm (`v2/actions_io.go:311-315`).

**t's hint** is `runNowLabel` (`v2/graphview.go:768-792`), I/O-free. It is "" unless the bead is open and not refused by a pause. Then:

- `run` for an AFK leaf (`loop.IsAFKLeaf`, `loop/loop.go:952-955`)
- `spec` for a Chart whose children are all closed and none held (`:776-779`)
- `land` for a Checkpoint that looks landable and has no land hold (`:780-783`)
- A retry-class pause overrides the label with `retry` or `retry hold` (`:788-790`, table `v2/graphview.go:730-750`).

**Which kinds are HITL** comes from the registry (`registry/registry.go:36-59`):

- HITL: Signal, Grilling, Survey, Pivot, Prototype, Approval, Surgery, Task and Initiative. Initiative is the only HITL container.
- AFK leaves: Research, ChartConflict, Spec, Build, Critique, Conflict, Review, Rollback and Landing.
- AFK containers, driven by the loop: Chart and Checkpoint.

## 2. The row table

"Rule" is the behaviour this doc recommends to the Spec. A refusal is I/O-free
unless it says "press-time".

| # | row | today's enter | today's t | enter under the rule | hints label | status-line refusal | detail pane's enter line |
|---|---|---|---|---|---|---|---|
| 1 | AFK leaf, open, ready (Research, Spec, Build, Rollback…) | refuses: "not a launchable HITL bead (AFK/container…)" (`graphtab.go:65-66`) | runs now; hinted `t run` (`graphview.go:774-775`) | runs now (`runNow`) | `enter run` | none up front; press-time refusals stay (`runNowHint`, the "AFK pool full (n/p)…" pool refusal, `actions_io.go:179-181`) | "AFK — <kind> runs via the loop. Press **enter** to run it now, or edit to intervene." (today says **t**, `graphview.go:677-679`) |
| 1b | AFK leaf, open, blocked by an open Ordering dep | refuses (as 1) | y/n "Run blocked bead now?", then forced into the frontier (`actions.go:470-480`, `actions_io.go:128-130`) | y/n "Run blocked bead now?" naming the blockers (ADR 0142 §7), then runs | `enter run` (`runNowLabel` ignores blockers) | none | as 1, plus the blockers |
| 2 | AFK leaf, in_progress, live effort | refuses (as 1) | not hinted; a press shows "dispatching…", then "is blocked / not ready — run-now only drives a bead already in the bd ready frontier" (`actions_io.go:315`): **misleading** | refuses with no I/O | none (`x abort` stays) | "<id> is already running — x aborts it" | "AFK — <kind> is running. x aborts it." |
| 3 | AFK leaf, in_progress, dead effort | refuses (as 1) | as 2 | refuses with no I/O | none (`x recover`) | "<id>'s effort is not running — x recovers it" | "AFK — <kind>'s effort is dead. x recovers it." |
| 4 | AFK leaf holding a tmux session | attaches or resumes: the session check comes before the kind check (`graphtab.go:61-62`, `actions.go:86-105`) | per `runNowLabel` on its status | attaches or resumes. **A session wins enter**, as it wins x (`graphtab.go:77-78`) | `enter attach` (alive) / `enter resume` (exited) | none | "SESSION — enter attaches (resumes if exited)." Today's preview says "runs via the loop", which disagrees with enter (ADR 0146 §3) |
| 5 | AFK, retry-class pause (`failing`, `integrate-conflict`, `profile` → retry; `foreign-hold` → retry hold) | refuses (as 1) | hinted `t retry` / `t retry hold` (`graphview.go:788-790`) | runs now (retry) | `enter retry` / `enter retry hold` | none | "… Press **enter** to retry it now …" |
| 6 | AFK, refusing pause (own `manual`/`abandoned`/`recovering`/`context`, or an inherited manual pause) | refuses (as 1) | not hinted. A press shows "dispatching…", then the `pauseHint` or "paused via <anc>" message (`actions_io.go:305-308`). **If the bead is also blocked, the y/n opens first and the refusal only follows `y`** (`actions.go:470`) | refuses with no I/O, before any dialog | none (`p unpause` where its own) | `pauseHint(b)` (`graphview.go:721-728`), or "<id> is paused via <anc> — select it and press p…" | today's "AFK — … Use edit to intervene." plus the existing `paused` field (`graphview.go:599-601`) |
| 7 | Chart, drained (all children closed, none holding `effort/<id>`) | refuses (as 1) | hinted `t spec` (`graphview.go:776-779`) | runs its speccing reconcile now | `enter spec` | none | "CONTAINER — drained & defogged. Press **enter** to run its speccing reconcile now …" (today **t**, `graphview.go:670-672`) |
| 8 | Chart, not drained | refuses (as 1) | not hinted. A press refuses press-time with "Chart not drained — …" or `bd.HeldHint` (`actions_io.go:316-319`), or with "blocked / not ready" if `bd ready` omits it | refuses with no I/O | none (`space fold`) | "<id>: Chart not drained — <closed>/<total> children closed" (`gateChildCounts`), or `bd.HeldHint(heldChildren)` | unchanged: "CONTAINER — drain-watched …" (`graphview.go:674`) |
| 9 | Checkpoint, landable (closed Approval, no open conflict/critique, no land hold) | refuses (as 1) | hinted `t land` (`graphview.go:780-783`) | lands now (Jinbe) | `enter land` | none; press-time `CheckpointLandHold` stays authoritative (`loop/loop.go:1065-1108`) | "CHECKPOINT — approved & at HEAD. Press **enter** to land it now (Jinbe) …" (today **t**, `graphview.go:661-663`) |
| 10 | Checkpoint, land held by open blockers | refuses (as 1) | not hinted. **But `g.blocked` is true, so a press opens "Run blocked bead now?". `y` forces it into the frontier and `CheckpointLandHold` then refuses with the land-hold note** (`actions.go:470-480`, `loop/loop.go:1102-1104`, `actions_io.go:322-323`) | refuses with no dialog: the blockers are the land hold (Spec FR14) | none | "<id>: approved; land held by open blocker: …" (`landHeldNote`, `actions_io.go:333-335`, fed by `landHeldBy`) | unchanged: "CHECKPOINT — approved; land held by open blocker: …" (`graphview.go:665-667`) |
| 10b | Checkpoint, not approved (land-watched) | refuses (as 1) | not hinted; a press refuses press-time with "Checkpoint not ready to land — needs a closed Approval …" (`actions_io.go:324-325`) | refuses with no I/O | none | the same words, from `checkpointLooksLandable` | unchanged (`graphview.go:668`) |
| 11 | Initiative head, open (ADR 0146 cut session) | launches or attaches Luffy's cut session, unclaimed (`actions.go:146-148`) | refuses: "is an Initiative — a container with no effort to run" (`actions.go:466-468`) | unchanged: launches or attaches (HITL container) | `enter launch` / `enter attach` | none | unchanged: "ON LAUNCH — Luffy opens a cut session for initiative" (`graphview.go:657-658`, `:687-688`) |
| 12 | Signal (Unparented, triage) | launches Luffy's triage | not hinted. A press refuses press-time with "is an open HITL bead — press enter to launch it (b in the background), not t" (`actions_io.go:310`) | unchanged: launches or attaches | `enter launch` / `enter attach` | none | unchanged: "ON LAUNCH — Luffy opens an agent session for signal" |
| 13 | Rollback (Unparented) | refuses (as 1) | `t run` | as row 1 | `enter run` | as row 1 | as row 1 |
| 14 | Unparented header (no bead) | "Unparented is a group header — select a Signal to launch Luffy to triage it, or a Rollback to run it" (`actions.go:66-72`) | the same message (every action key shares the nil-selection arm) | the same refusal | none (`space fold`; `bindings` adds enter only when `b != nil`, `graphtab.go:119`) | unchanged | "…Select a Signal and press enter to launch Luffy to triage it, or a Rollback to run it." (today "**with t**", `graphview.go:553`) |
| 15 | closed bead, no session | refuses: "<id> is closed — not launching" (`graphtab.go:63-64`) | not hinted. A press shows "dispatching…", then press-time "is closed — kept for lineage, nothing to run" (`actions_io.go:303-304`) | refuses with no I/O | none | "<id> is closed — nothing to launch or run" | unchanged: "closed — kept for lineage." (`graphview.go:651-652`) |
| 16 | closed bead with a session | attaches or resumes (ADR 0038 §3, ADR 0146 §2) | as 15 | unchanged: attaches or resumes | `enter attach` / `enter resume` | none | "closed — kept for lineage. enter attaches its session." Today's preview omits the session, against ADR 0146 §3 |
| 17 | HITL, blocked (no session) | y/n "Launch blocked bead?" naming the blockers (`actions.go:106-117`) | **not hinted, but a press opens "Run blocked bead now?". `y` then refuses with "open HITL bead — press enter …, not t"** | unchanged y/n, then launches | `enter launch` | none | unchanged ON LAUNCH line, plus the blockers |
| 18 | HITL, paused (its own pause or an inherited manual one) | **launches at once, with no dialog and no mention of the pause** | not hinted. A press refuses with `pauseHint` or "paused via <anc>" | **new** y/n "Launch paused bead?" naming the pause (the label, or "manual via <anc>"). Blocked and paused together get one dialog naming both. A session still attaches with no dialog | `enter launch` (enter acts, through a dialog) | none | the ON LAUNCH line; the `paused` field already names the cause (`graphview.go:626-639`) |
| 19 | any bead with a write in flight (also launching… or closing…) | refuses with `busyWhy`: "<id>: write in flight, wait" / "still launching …" / "is closing — wait" (`graphtab.go:42-52`, `:59-60`) | a pending write is refused by `startWrite` ("<key>: write in flight, wait", `live.go:386-392`). **On an open blocked bead the y/n opens first** (`actions.go:470`) | refuses with `busyWhy` before any dialog, on every row | none | `busyWhy(b)` | unchanged; the row suffix already shows the verb (`graphview.go:242-251`) |

Note on row 4: an AFK bead holding a session needs a session recorded under an
AFK bead's id. Nothing in this survey shows a path that creates one (gap 1). The
row is kept because `launchWhy` and `xLabel` already give a session
precedence over the kind.

## 3. Where today's code diverges from the rule

1. **enter refuses every AFK row and loop-driven container**
   (`v2/graphtab.go:65-66`). Under the rule, rows 1, 1b, 5, 7, 9 and 13 run
   now, as `t` does today.
2. **A paused HITL bead launches with no dialog** (row 18). Neither `launchWhy`
   nor `launchAs` reads `bd.Paused` or `inheritedPause`. Defog refuses the same
   bead (`v2/defogtab.go:274-278`, ADR 0156 §5), and the rule wants y/n on Graph.
3. **`runNow` opens the blocked dialog before any other gate** (`v2/actions.go:470`):
   - Rows 6 and 10 get a dialog whose `y` only produces a refusal.
   - Row 17 gets a dialog that refuses with "press enter".
   - Row 19 gets a dialog over a pending write.
4. **In_progress AFK rows (2, 3) get the wrong reason.** `bd ready` drops
   in_progress beads, so `runNowHint`'s "blocked / not ready" arm answers a
   live or dead effort (`v2/actions_io.go:311-315`). They also pay a
   "dispatching…" round trip.
5. **The detail pane names t** in four places (`v2/graphview.go:553`, `:662`,
   `:671`, `:678`). It also disagrees with enter on rows 4 and 16 (ADR 0146 §3).
6. **User-facing strings name t**:
   - "…then t" (`v2/actions_io.go:241`)
   - "…, not t" (`:310`)
   - "…runs via t" (`:327`)
   - Two Go comments, which are not user-facing: "TUI's t refusal" (`loop/loop.go:1063-1064`) and "a second t" (`v2/actions.go:485`).
7. **`b` on an AFK row** refuses with the stale "use edit to intervene" text
   (`v2/graphtab.go:66`). `b` stays HITL-only, so its refusal should say
   "b is HITL-only — enter runs it".

**Recommended shape, the smallest diff.** Graph's enter routes as Defog's
already does (`v2/defogtab.go:355-364`): HITL and Initiative go to `launchAs`,
and everything else goes to `runNow`. Then move the I/O-free gates into the
**top of `runNow`**, so Defog's AFK rows get them too:

1. `busyWhy`
2. closed
3. in_progress, live or dead
4. a refusing pause (`pauseRefusesRunNow`, `v2/graphview.go:706-708`)
5. a held Checkpoint (`landHeldBy`)

Only after those comes the blocked y/n. Add the pause y/n to `launchAs` next to
the blocked one. `runNowLabel` stays the hint, and `claimRunNow` stays the
authoritative press-time gate.

## 4. Do Idea's and History's enter sit inside the rule?

- **Idea: inside the rule.** Its `enter triage` routes through `actionKey` →
  `launchAs` (`v2/ideatab.go:216-217`). It is hinted when `launchWhy == ""`
  (`v2/ideatab.go:153-155`), and ADR 0149 §2 says it "does what Graph's launch
  does". Every Idea row is a Signal, which is HITL, so the rule is just "launch
  or attach". "triage" is the verb name the hints bar gives it.
  - Consequence: Idea **inherits** whatever `launchAs` does on a blocked or
    paused Signal. Today that is y/n on blocked and nothing on paused. After
    change 2 it is also y/n on paused.
  - The Chart names only Defog and Execute as queue tabs that refuse. Whether
    Idea is a queue tab is open (gap 3).
  - The '+ New Signal' row's `enter new` (`v2/ideatab.go:141`, `:212`) has no
    bead and sits outside the rule, like a group header.
- **History: exempt.** Its rows are commits, not beads. `enter`/`G` reveal the
  commit's bead in Graph (`v2/history.go:36`, ADR 0144 §3). The rule speaks
  only to bead rows.

## 5. `?` keys changes

The `?` reference is `bindings()` plus `extra()` (`v2/shell.go:619`). The changes:

- **Drop `t run`.** Remove `keyRunKey` (`v2/graphtab.go:32`), the `t` hint
  (`:135-138`), its entry in `extra()`'s list (`:180`) and its `actionKey` arm
  (`v2/actions.go:51-52`). `t` becomes unbound on Graph, as on Defog
  (ADR 0156 §6).
- **enter's label is per row.** The hints entry becomes
  `verbKey("enter", <verb>)`, with the verb one of:
  - `launch`, `attach` or `resume` for HITL and session rows
  - `run`, `spec`, `land`, `retry` or `retry hold` from `runNowLabel` for AFK and container rows

  This mirrors the old `t` hint (`v2/graphtab.go:136-137`). The labelless
  `keyLaunch`, shown in `?` where enter only refuses, becomes
  `enter  launch/attach (HITL) · run (AFK)`.
- `b bg` is unchanged and stays HITL-only (`v2/graphtab.go:119-124`).
- ADR 0144 §7's Graph list becomes
  `enter <verb> · b bg · space fold · d detail · … · p pause · x … ` with no
  `t run`. §2's "v1's `b t p …` keep their v1 meanings" drops `t`.
  ADR 0146 §2's refusal list adds "an AFK row the gate refuses".

## 6. Test changes implied

In `v2/actions_test.go`:

- `:162` "run a blocked bead now" `resblk`/`t` → `enter`, same title.
- `:261` "t runs" `res` → "enter runs" `res`.
- `:294-312` `TestEnterOnNonLaunchableRows`: drop the `res` "not a launchable"
  case, since it now runs. Add:
  - `live` → "already running"
  - `dead` → "x recovers"
  - `done` → "closed" (message reworded)
- `:338-345` `TestGraphHintsListApplicableVerbs`:
  - `res` → `enter run · d detail · …` with no `t run`
  - `sess` → `enter attach`
  - `live` and `dead` unchanged (no enter)
  - `pz` keeps `enter launch`
- `:388` and `v2/shell_test.go:362` `{"resblk","t"}` → enter.
- `:515` `TestRunNowHintHITLNamesEnterNotL`: the hint loses ", not t". The
  assertion still holds; rename or extend it to assert no "t".

New cases:

- `pz` + enter opens "Launch paused bead?". `y` → `startSession pz`; `n` calls nothing.
- An inherited pause names "manual via <anc>".
- A refusing-paused + blocked AFK bead refuses with **no** dialog.
- A held Checkpoint refuses with no dialog.
- A pending write on a blocked AFK bead refuses with no dialog.
- Retry, spec and land labels on enter (fixtures for a `paused:failing`
  Research, a drained Chart and a landable Checkpoint).
- The detail and Unparented text say "enter", never "t".

Automatic coverage:

- `TestGraphBindingCoverage` (`v2/graph_test.go:312`) and
  `TestGraphActionBindingCoverage` (`v2/actions_test.go:361`) will flag `t` if
  any arm still handles it.
- `v2/ideatab_test.go:231` and `v2/defogtab_test.go:318` already expect `t`
  unbound and stand.
- If `runNow` gains the I/O-free gates, `TestDefogEnter`
  (`v2/defogtab_test.go:270`) needs AFK live/closed refusal cases.

## Open questions / gaps

1. **Can an AFK bead hold a tmux session (row 4)?** No launch path for one was
   found in the files read. The row rests on `launchWhy`'s and `xLabel`'s
   session-first ordering, not on an observed case.
2. **Is a not-drained Chart in `bd ready`?** This decides whether today's
   press-time refusal reads "Chart not drained" or "blocked / not ready"
   (row 8). It was not run against a live graph. The rule's I/O-free refusal
   makes this moot.
3. **Is Idea a queue tab?** Should a blocked or paused Signal on Idea refuse,
   as on Defog, or ask y/n, as on Graph? Idea shares `launchAs`, so it asks
   y/n on blocked today. That is for mc-lu2x.12.4.
4. **A paused Initiative head (row 11 × 18).** It is HITL, so the rule gives
   y/n. But its pause stops its member chains, not the cut session (`v2/actions.go:516-517`, `:525-526`).
   Should the cut session be exempt from the paused dialog?
5. **The HITL hint verb.** Precise `launch`/`attach`/`resume`, or Defog's
   combined `launch/attach` (ADR 0156 §7)? The tabs should agree.
6. **An in_progress HITL bead with no session** (fixture `gone`). Today enter
   is hinted and launches anew, while x offers recover. This is outside the
   bead's row list and not settled here.
7. **Graph's detail has no explicit `enter` field**, unlike Defog
   (`v2/defogdetail.go:131-138`). This doc proposes rewording `launchPreview`.
   Whether to add Defog's field instead is a Spec call.
