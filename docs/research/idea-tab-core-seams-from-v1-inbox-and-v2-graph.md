# Core seams the Idea tab reuses from v1 Inbox and v2 Graph

Status: Research (input to the Idea tab Spec, mc-lu2x.5)
Date: 2026-10-12
Bead: mc-lu2x.5.3

## Purpose

The Idea tab has to be a thin client over the core that the CLI and loop share.
This doc lists, with file:line, what already exists for each Idea-tab behaviour,
what is missing, and which logic lives only inside a tab (v1 `tui/` or v2 `v2/`)
and would have to move to core if the CLI is to share it. Paths are relative to
`apps/mugicrew/`.

## 1. Listing open and in-triage Signals, grouped by Initiative

**There is no core list for this.** The only reads are generic:

- `bd.ListAll` (`bd/bd.go:114`) returns every bead, closed ones included, and the
  v2 shell loads it through the `listAll` seam (`v2/live.go:39`). `bd.Bead`
  (`bd/succession.go:34`) carries `Kind`, `Status`, `Parent`, `Assignee`,
  `Priority` and `Created`, which is everything the Idea filter needs.
- Sessions are keyed by bead id: `session.Sessions` (`session/tmux.go:878`), read
  as `readSessions` (`v2/live.go:40`). `Info.Has()` is true for **Alive or
  Exited** (`session/tmux.go:857`, states at `:826–828`). `Info` does not name
  the crewmate (`session/tmux.go:834–843`). "Luffy's session" can only be
  derived: a Signal's registry owner is `luffy` (`registry/registry.go:36`).
- **Drafts** have status `deferred` (`bd/succession.go:70–72`, the `bd list
  --status deferred` in `bd/draft.go:39`). Note that `Bead.Open()` counts a Draft
  as open (`bd/succession.go:72`), so the Idea filter has to test `Status ==
  "open"` / `"in_progress"` explicitly. `Open()` alone lets Drafts through.
- **Parent shape.** `NewSignal` files a Signal with no parent (`bd/bd.go:64–68`).
  The only re-parenting is `AdoptChain`/`ReleaseChain` (`bd/initiative.go:104`,
  `:128`, `:138`, `:153`), which refuse a head inside any non-Initiative
  container. So a Signal's `Parent` is either `""` or an Initiative. Luffy's cut
  session files the next phase's member Signal as a *Draft* child of the
  Initiative (`luffy/luffy.go:83`). That Draft is `deferred` and drops out
  through the status test.

Tab-local logic the Idea list would otherwise copy:

| Logic | Where | Notes |
|---|---|---|
| `inboxList`: open HITL beads, plus any bead with a session, minus blocked ones | `tui/tui.go:372` | v1 only. Closest precedent: "session keeps it in view whatever its status" (`tui/tui.go:366–371`) |
| `isHITL` | `tui/tui.go:54`, `v2/graph.go:21` | Duplicated. Both read `registry.Lookup` |
| `beadCmp` (open first, then priority, then newest) | `v2/graph.go:197` (and v1 `tui/tui.go:233`) | Duplicated |
| `buildGraph` grouping (Initiative heads, member chains, Unparented) | `v2/graph.go:59` | Graph-shaped (Charts, nesting, phaseOrder). Far more than "group Signals by Initiative" |
| `adoptTargets` (open Initiatives, beadCmp order) | `v2/graphforms.go:60` | Pure. Reusable as is inside package `v2` |

**Missing:** a pure `ideaList(beads, sessions)` that keeps a Signal when
`Status=="open"`, or when `Status=="in_progress"` and it has a session. It groups
by `Parent` (an Initiative id or `""`) and sorts with `beadCmp`. The predicate
fits beside `inboxList` and `buildGraph` as a package-level pure function in
`v2`. Nothing in `bd` needs to change, because the snapshot already has every
field.

## 2. Create and edit: `bd.NewSignal` and `bd.Update`

- `bd.NewSignal(title, description)` (`bd/bd.go:66`) takes **no priority and no
  parent**. Its argv is `unparentedArgs` (`bd/bd.go:53–63`). Priority therefore
  needs a second write, `bd.Update(id, {"--priority", N})`. Filing straight into
  an Initiative needs a second `AdoptChain` call (`bd/initiative.go:104`). Both
  are non-atomic two-step writes.
- `bd.Update(id, args)` (`bd/bd.go:348`) is the generic escape hatch. It strips
  the kind prefix from `--title` (`bd/bd.go:349–356`) and passes everything else
  through. Title, priority and description are all `bd update` flags.
- v2 already wraps both as test seams: `newSignal`/`updateBead`
  (`v2/graphforms.go:32`, `:34`). Graph's uses: `fileSignal`
  (`v2/graphforms.go:210`, title plus description only), `bumpPriority` ±1
  (`v2/actions.go:516`), the title form and the `$EDITOR` description round-trip
  (`editPick`, `editDescription` `v2/graphforms.go:327`, then
  `finishDescEdit` → `updateBead --description`).
- All writes run off the event loop under the per-key pending guard
  `startWrite` (`v2/live.go:362`). The Idea tab can call these directly.

**Missing:** nothing in core for a plain title/priority/description edit.
Setting priority at create time is either two calls or a new
`bd.NewSignal`-with-priority variant. That is the only core gap here.

## 3. The z reject gate

- `rejectable` = Signal, `Status=="open"`, `Assignee==""`
  (`v2/graphforms.go:45`). The same function is copied in v1
  (`tui/tui.go:1534`). `reject` (`v2/graphforms.go:242`) re-checks the gate on the
  current snapshot and then calls `closeBead` = `bd.Close`
  (`bd/succession.go:519`). The fallback reason is `rejectDefault`.
- Both the gate and `reject` are package-level functions, not graphTab methods,
  so the Idea tab can reuse them as they are. The **opener is graph-bound**:
  `z` is only reachable through `graphKey` (`v2/graphforms.go:119–120`, which
  does `m.tabAt(tabGraph).(graphTab)`), and the shell routes form/action keys
  only `if m.tab == tabGraph` (`v2/shell.go:309`).
- An in-triage Signal is `in_progress` and claimed, so `rejectable` is false for
  it by design. A live triage session closes it (REJECT is a triage outcome,
  `luffy/triage.go:14–22`).

## 4. enter: launching Luffy's triage session

- The chain is `launchAs` (`v2/actions.go:81`) → `launchNow`
  (`v2/actions.go:124`) → `launchCtx` (`v2/actions_io.go:392`, profile) →
  `launchFor` (`v2/actions_io.go:34`) → `launchOwner` (`v2/graphview.go:686`,
  registry owner) → `Luffy.Launch` (`luffy/luffy.go:58`, `KindSignal` →
  `triagePrompt` at `:61`).
- `launchNow` **claims** the Signal before it starts the session
  (`v2/actions.go:143–152`; only Surgery, Initiative and in-progress Task are
  exempt, `:148`). A failed start unclaims it. This claim is what makes a
  triaging Signal `in_progress`, which in turn makes it fail `rejectable`.
- `launchAs` reattaches an alive session or resumes an exited one before it
  considers a fresh launch (`v2/actions.go:86–104`).
- **Graph coupling:** `launchAs` takes a `graphTab` for `launchWhy`
  (`v2/graphtab.go:57`), `blocked` and `openBlockers`. `actionKey`, the only
  caller, is hard-wired to the Graph tab (`v2/actions.go:43–44`).
  `launchNow` itself is a `Model` method with no tab dependency, so the Idea
  tab can call it directly.
- **Duplication:** `launchFor` and `launchNow` are copied in v1
  (`tui/tui.go:452`, `:1970`). The CLI's `mugicrew signal triage` has its own
  third path (`main.go:455`, `triage` `main.go:472`). It calls `crewFor` and
  `c.Launch` directly and **does not claim**, so a CLI-launched triage leaves
  the Signal `open`.

## 5. Graph's adopt and release (a / A)

- Core: `bd.AdoptChain(head, initiative)` (`bd/initiative.go:104`) and
  `bd.ReleaseChain(head)` (`bd/initiative.go:138`). Both do their own re-checks
  against a fresh `ListAll`. They are shared with the CLI (`mugicrew initiative
  adopt|release`).
- TUI layer: the gates `canAdopt` and `canRelease` are **graphTab methods**
  (`v2/graphforms.go:50`, `:55`) because they read `g.beads`/`g.beadByID`. The
  picker dialog opens inside `graphKey` (`v2/graphforms.go:119`). `adopt`
  (`:259`) is package-level. Release is inline in `graphKey`
  (`v2/graphforms.go:146–154`). v1 equivalents: `openAdopt`/`openRelease`
  (`tui/tui.go:2517`, `:2591`).

## 6. The modal system and a multi-field form

- `v2/modal.go` has three kinds: `confirm`, `input` and `picker`
  (`v2/modal.go:18–22`). An `input` dialog holds N `line` fields with labels
  (`inputDialog` `v2/modal.go:44`; the comment at `:20` says "one or two").
  `tab` cycles fields (`:57`) and **`enter` submits** (`keySubmit` `:55`,
  handled at `:117`).
- `line` (`v2/line.go`) is a hand-rolled **single-line** input. Its header
  comment rejects bubbles `textinput` as too heavy (`v2/line.go:9–14`).
  Nothing in the module imports `bubbles/textarea` (a repo-wide grep finds only
  that comment). The package is in the pinned dependency, though:
  `github.com/charmbracelet/bubbles v0.21.0` (`go.mod:7`) ships `textarea/` in
  the module cache. Using it adds an import, not a dependency.
- **Gaps for a title + priority + description form:**
  1. `enter` is hard-bound to submit. A textarea needs `enter` for a newline, so
     submit would need another key (e.g. ctrl+s) when the textarea has focus.
  2. The `dialog` struct stores `inputs []line` (`v2/modal.go:33`), and
     `modalKey` calls `line.apply` (`v2/modal.go:125`). A textarea field needs a
     new field type, or a new `dialogKind`, plus the textarea's `Update`/`View`
     wiring and focus handling.
  3. There is no priority control. Priority would be a `line` parsed 0–4, or a
     picker step.
  4. `modalView` lays out input rows one line each and wraps them
     (`v2/modal.go:155–163`). A multi-line field needs a height budget, as the
     picker branch already computes one (`:164–181`).
- Today's description path is `$EDITOR` through `tea.ExecProcess`
  (`editDescription` `v2/graphforms.go:327`), which works now with no modal
  change.

## 7. Logic living only in a tab that would have to move to share it

| Logic | Lives in | Why it matters |
|---|---|---|
| Launch path (`launchFor`/`launchNow`, claim-then-start) | `v2/actions*.go`, copied in `tui/tui.go` | CLI `signal triage` diverges: no claim, own path (`main.go:472`). "In triage = in_progress with a session" holds only for TUI launches |
| `rejectable` gate | `v2/graphforms.go:45`, `tui/tui.go:1534` | Copied twice; no CLI reject verb |
| `isHITL`, `beadCmp`, `pivotLeads`/`pivotGate` | `v2/graph.go`, `tui/tui.go` | Copied by design (ADR 0131 "ported by copy", `v2/graph.go:1–5`) |
| Inbox / Idea selection predicate | `tui/tui.go:372` only | No core list of "open + in-triage Signals" |
| `canAdopt`/`canRelease` gates | `v2/graphforms.go:50–58` (graphTab methods) | Core `AdoptChain`/`ReleaseChain` re-check anyway, so these are display gates only |

For the Idea tab inside package `v2`, the concrete refactor is small. Lift the
key routing at `v2/shell.go:309` beyond `tabGraph`, and make `launchAs`, `graphKey`
and `actionKey` take the selection (and a `launchWhy`/`blocked` source) rather
than `m.tabAt(tabGraph).(graphTab)`. No `bd`/`luffy` change is needed for the
list, edit, reject, launch or adopt behaviours. Priority at create time and the
CLI's no-claim triage are the only core-level asymmetries.

## Open questions / gaps

- Does "in triage" mean `in_progress` with `Has()` (alive **or exited**,
  `session/tmux.go:857`), or Alive only? v1 `inboxList` keeps any session
  (`tui/tui.go:394`).
- A CLI-launched triage (`main.go:472`) never claims, so its Signal stays `open`
  and `rejectable` while a session runs (`session.Sessions` sees it only if it is
  tmux-hosted. `runInteractive` runs in the human's TTY). Should the Idea tab
  treat an `open` Signal that has a session as in-triage too?
- Should a Signal under a **closed** Initiative group under it or under
  Unparented? Neither core nor `buildGraph` makes the call. `buildGraph` keys
  on any container head (`v2/graph.go:63–72`).
- Should priority at create time go through two writes or a
  `bd.NewSignal(title, desc, priority)` variant? The same question applies to
  filing straight into an Initiative (`NewSignal` + `AdoptChain`, non-atomic).
- textarea in a modal: which key submits while the description field has focus?
  This is not covered by ADR 0142 §7 as read through `v2/modal.go`.
