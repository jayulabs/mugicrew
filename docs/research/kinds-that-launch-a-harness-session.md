# Routing kinds that launch a harness session, and where to derive that set

Status: Research (input to the Settings tab kind → Agent Profile editor Spec, mc-snpl)
Date: 2026-10-12
Bead: mc-snpl.1

## Purpose

The Settings tab lists one `[kind_profiles]` row for `default` plus one for every
routing kind that launches a harness session (AFK `Run` or HITL `Launch`). A kind
that gains a session later must get its row without anyone editing the TUI. This
doc works out from the code which of the 18 registry kinds start a harness today,
and names the one place the Settings tab should read that set from.

## 1. How a profile reaches a session

- `ctxForKind` builds the harness for a kind's mapped profile
  (`profile.go:65-69`). It is reached only through `appCtx.ForKind`
  (`main.go:342-347`).
- `ForKind` has four callers:
  - AFK admission gate `dispatch.Admit` → `Unrunnable` (`dispatch/dispatch.go:628`, `:637-650`), run by the loop on every bead it dispatches (`loop/loop.go:647`) and by TUI run-now (`tui/tui.go:2377`).
  - AFK ctx assembly `dispatch.assemble` (`dispatch/dispatch.go:661`), wrapped by `Sessions` (`:608`, called at `:552`).
  - HITL launch `launchCtx` (`tui/tui.go:479`), then `launchFor` → `crew[owner].Launch` (`tui/tui.go:455-461`).
  - CLI Signal triage (`main.go:440`).
- So the mapping is **resolved** for every AFK-dispatched kind and every launched
  HITL kind. It is **used** only when the crewmate then calls
  `ctx.Harness().Run` or `ctx.Harness().LaunchCommand`.

## 2. The 18 registry kinds against their harness calls

The registry rows are at `registry/registry.go:32-55`. All 8 owners are wired in
`crew()` (`main.go:370-381`). The kind constants are at `contract/contract.go:32-44, 54-58`.

| Kind | Mode | Owner | Harness call that starts a session | Session? |
|---|---|---|---|---|
| signal | HITL | luffy | `Launch` triage prompt → `LaunchCommand` (`luffy/luffy.go:61-78`); `Run`→`Reconcile` ignores ctx (`luffy/triage.go:36`) | **yes** (HITL) |
| grilling | HITL | nami | `Launch` grilling prompt (`nami/nami.go:220-237`) | **yes** (HITL) |
| research | AFK | nami | `runResearch` → `Harness().Run` (`nami/nami.go:67`) | **yes** (AFK) |
| chart | AFK container | nami | AFK: promote-or-stamp judge (`nami/nami.go:539`) and speccing session (`nami/nami.go:592`); HITL: charting `Launch` on an Unmapped Chart (`nami/nami.go:226-230`, admitted by `tui/tui.go:2699`, `:1947`) | **yes** (both) |
| chart-conflict | AFK | nami | `runChartConflict` → `Harness().Run` (`nami/nami.go:128`) | **yes** (AFK) |
| spec | AFK | zoro | `Reconcile` slicing session (`zoro/zoro.go:213`), via `Run` (`zoro/zoro.go:31-37`) | **yes** (AFK) |
| pivot | HITL | zoro | `Launch` pivot prompt (`zoro/zoro.go:43-55`) | **yes** (HITL) |
| checkpoint | AFK container | jinbe | none: `Run(_ contract.Ctx, …)` (`jinbe/jinbe.go:41`) → `reconcileCheckpoint(cp bd.Bead)` takes no ctx (`jinbe/jinbe.go:119`); `Launch` refuses (`jinbe/jinbe.go:58-60`) | **no** |
| rollback | AFK | jinbe | none: same `Run`, `reconcileRollback(rb bd.Bead)` (`jinbe/jinbe.go:171`) | **no** |
| prototype | HITL | sanji | `Launch` (`sanji/sanji.go:39-47`) | **yes** (HITL) |
| build | AFK | franky | `runImplement` (`franky/franky.go:195`) | **yes** (AFK) |
| critique | AFK | franky | `runImplement` (`franky/franky.go:42-43`, `:195`) | **yes** (AFK) |
| conflict | AFK | franky | `runConflict` (`franky/franky.go:218`) | **yes** (AFK) |
| review | AFK | usopp | `runReview` (`usopp/usopp.go:135`) | **yes** (AFK) |
| approval | HITL | luffy | `Launch` approval prompt (`luffy/luffy.go:66-78`) | **yes** (HITL) |
| surgery | HITL | franky | `Launch` surgery prompt (`franky/franky.go:57-65`) | **yes** (HITL) |
| task | HITL | brook | `Launch` (`brook/brook.go:32-40`) | **yes** (HITL) |
| initiative | HITL container | luffy | `Launch` cut prompt (`luffy/luffy.go:63-64`, `:78`), admitted as launchable (`tui/tui.go:1947`, `tui/view.go:802`) | **yes** (HITL) |

**Current set: 16 kinds.** That is every registry kind except `checkpoint` and
`rollback`, which are both Jinbe's and both mechanical.

Two corrections to what the bead assumed:

- **initiative does launch a session.** It is branchless and has no effort, so
  the loop never dispatches it. But a human `l` on it opens Luffy's cut session
  through `ForKind` (`tui/tui.go:479`, `:1947`, `:1991`). Its profile row matters.
- **The registry's `Mode` does not decide this.** `checkpoint` and `rollback` are
  AFK yet start no session. `chart` is AFK yet also has a HITL session, the
  Unmapped Chart charting launch.

## 3. What does not work as the derivation

- **`Mode` / `Container`** (`registry/registry.go:22-26`). Mechanical AFK kinds
  share `Mode: AFK` with agentic ones, so these fields can't tell them apart
  (§2).
- **"Owner is wired in `crew()`".** All 8 owners are wired, Jinbe included
  (`main.go:370-381`), so this would list all 18 kinds.
- **Owner ≠ `jinbe`.** It matches today's set by coincidence. Jinbe is the one
  crewmate whose `Run`/`Launch` never touch ctx (`jinbe/jinbe.go:41`, `:58`),
  but nothing ties ownership to the session fact. A future agentic Jinbe kind
  would be dropped silently.
- **Probing crewmates at runtime** (calling `Run`/`Launch` with a recording
  harness). `Run` and `Launch` read bd and git before they reach the harness
  (for example `jinbe/jinbe.go:42`, `zoro/zoro.go:47`), so the Settings tab
  can't do this.

## 4. Recommended single source: a new registry `Entry` field, opt-out polarity

The registry is already the one routing table: "Nothing is derived or stored
elsewhere" (`registry/registry.go:1-3`). The `[kind_profiles]` key validator
already reads it (`loop/config.go:211`). The session fact should live there too:

```go
type Entry struct {
	Owner      string
	Mode       Mode
	Container  bool
	Mechanical bool // runs no harness session (Run/Launch never reach ctx.Harness); no [kind_profiles] row
}
```

Set `Mechanical: true` on `checkpoint` and `rollback`, plus a read-only
accessor (for example `registry.SessionKinds() []contract.Kind`, sorted) that
the Settings tab ranges over. Rows are `default` plus that list.

**Rule:** a kind gets a row unless its registry entry is marked `Mechanical`.

The polarity matters for the requirement. Go's zero value is `false`, so a new
registry row gets a Settings row by default. Hiding a kind takes a deliberate
`Mechanical: true`. The field could instead be `Session bool`, but then a newly
added kind would silently get no row until someone remembered to set it. That
fails the "automatically" clause in the bead.

The cost: the field is hand-kept, one line per kind, next to the kind's owner and
mode. Nothing mechanically checks that a `Mechanical` kind's crewmate really
ignores ctx. Jinbe's signatures (`Run(_ contract.Ctx, …)`, `reconcileCheckpoint(cp bd.Bead)`)
show it today, but they don't enforce it.

## Open questions / gaps

- **Mechanical kinds are still profile-gated.** `dispatch.Admit` resolves
  `ForKind` for every dispatched bead (`loop/loop.go:647`,
  `dispatch/dispatch.go:637-650`), and `assemble` does it again
  (`dispatch/dispatch.go:661`). So an unrunnable `default` profile (no
  `checkpoint` key exists to fall through to) pauses Checkpoint lands and
  Rollbacks `paused:profile`, even though Jinbe never uses the harness. Once the
  `Mechanical` field exists, `Admit`/`assemble` could skip resolution for it.
  That is a code change and belongs in its own Signal.
- **`checkpoint` / `rollback` keys are accepted but have no effect.**
  `readKindProfiles` accepts any registry kind (`loop/config.go:211`), so
  `checkpoint = "x"` validates and only matters through the gate above.
  Undecided: should those keys warn `config.unusable` once `Mechanical` exists,
  or stay valid?
- **One kind's profile drives two kinds of session.** `chart` covers both the
  AFK promote-or-stamp/speccing sessions and the HITL charting session, so one
  row drives both. The speccing session authors the *Spec* body, but it runs
  under the `chart` profile, not `spec` (`nami/nami.go:586-592`, ctx assembled
  for the Chart bead). The Settings row's label or help text may need to say so.
- **No test enforces `Mechanical`.** A Jinbe change that starts calling the
  harness would keep its kinds off the Settings tab until someone flips the
  flag.
