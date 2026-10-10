# mugicrew MVP — Nami + the outer dispatch loop

**Status:** buildable implementation spec · **Effort:** the MVP wayfinding map

This spec is the executable output of the MVP wayfinding effort. It fixes
the *mechanics* of a minimum mugicrew that can file a Signal, let a human triage
it into a Chart, seed and work defog beads, auto-run AFK work through a stateless
loop, and surface human work through a TUI — all over a swappable harness.

It does **not** re-decide the ontology. Kinds, edges, owner-from-kind, and
derived state are settled in `apps/mugicrew/GLOSSARY.md` and ADRs 0001–0006; this
spec references them and never restates them. Where a decision here amends or
constrains an ADR, it says so inline.

## 1. Scope

**In:** the plumbing that dispatches. Concretely — the crewmate contract + the
kind→owner registry (§4), the harness seam (§5), the outer dispatch loop (§6),
`signal new` / `signal triage` and the triage→Chart accept path (§7), Nami's
`reconcile` primitive and the drained-Chart→Spec handler (§8), and the TUI (§9).
Only **Nami**'s kinds are wired; only the **Claude Code** harness backend is
built.

**Out (this map's destination stops here):**
- Nami charting *well* — real grilling/Spec-synthesis quality. The Chart→Spec
  handler stubs **one** Spec.
- The other crewmates (Zoro, Sanji, Franky, Usopp, Luffy, Jinbe) and their kinds.
- The **Pi** harness backend (the seam is built; a second backend waits until
  one is actually needed).
- Richer Signal handling (dedup/supersede, offshoot-Signals, a mechanical
  `signal reject` fast-path) — ADR 0006's MVP subset is accept + reject only.
- ADR 0003 upgrades: per-crewmate cap config, journal-tail cadence, `bd serve`,
  out-of-process crewmates.
- Ontology revision (Chart→Spec exactly-one vs "one or more") — filed separately (since settled by ADR 0008).

## 2. Runtime stack (OE ADR 0007)

- **Language:** Go, compiled to a single static binary `mugicrew`.
- **TUI:** bubbletea.
- **bd access:** shell-out to the `bd` CLI (§3). In-process bd (import beads as a
  Go library over embedded Dolt) is a **logged upgrade**, gated on a
  single-writer concurrency model + a CGO build — not built here.
- **Repo:** polyglot Nx monorepo; Nx/pnpm retained for future Angular/Wails GUIs.
  The old `apps/factory` TS scaffold is abandoned.

OE ADR 0007 supersedes ADR 0003's Node/TS runtime *assumption*; ADR 0003's
stateless-loop *decision* stands unchanged.

## 3. Module layout & build prerequisite

`apps/mugicrew/` is a self-contained Go module `jayulabs.org/mugicrew`,
`package main`, with internal packages:

| package     | holds |
|-------------|-------|
| `bd/`       | the `bd` shell-out helper (§3.1) + `spawnSuccessorIfAbsent` (§4.3) |
| `contract/` | the `Crewmate` interface + `LaunchCommand` type (§4); the `Harness` seam — `Harness`, `Invocation`, `Result` (§5.1, see note) |
| `registry/` | the static `kind → {owner, mode, container}` map (§4.2) |
| `claudecode/` | the concrete `claudecode` `Harness` backend (§5.3) |
| `loop/`     | the outer dispatch loop (§6) |
| `nami/`     | Nami: `Run` / `Launch`, `reconcile`, the triage front door (§7–§8) |
| `tui/`      | the bubbletea app (§9) |

**Build prerequisite (not a map dependency):** `apps/factory` is deleted and
`apps/mugicrew` initialised as the Go module above. It is a
build-time first step, not a decision this spec depends on.

### 3.1 bd access layer

All bd access is **shell-out to the `bd` CLI in embedded-DB mode**, with `--json`
for reads, behind one `bd()` helper in `bd/`. `bd serve` (measured-overhead
in-process daemon) is deferred per ADR 0003 §8.

Two footguns the helper MUST encapsulate (verified against bd 1.3.0):

1. **`--no-inherit-labels` on every runtime child create.** Without it a defog
   child inherits the Chart parent's `kind:` label and breaks one-kind routing.
2. **Edges via the right verbs.** The spine uses `bd dep add -t caused-by`;
   obsoletion uses `bd supersede` — neither is reachable through `bd link`.
   Directed non-blocking edges fail silently if reversed, so direction is fixed
   **once** in the helper, never at call sites.

Command mapping (kind→type derivation, all five edges, scoped frontier/HITL
queries, claim/close/supersede, JSON shapes) is in the research doc above; build
the helper from it.

## 4. The crewmate contract

A **crewmate** is a Go interface that owns ≥1 kind. It is **harness-blind**:
everything it needs — bd access and the harness — arrives on a `ctx`.

```go
package contract

type Crewmate interface {
    // AFK kinds. Does the work (agentic runs via ctx.Harness()), then mutates
    // the graph itself (close + spawn) per ADR 0001. Invoked BY THE LOOP.
    Run(ctx Ctx, bead Bead) error

    // HITL kinds. Returns the command the human opens. Invoked BY THE TUI
    // (or the `signal triage` CLI verb, §7) on pick-up — never by the loop.
    Launch(ctx Ctx, bead Bead) (LaunchCommand, error)
}
```

A crewmate owning several kinds switches on `bead.Kind` inside each method; the
interface stays with the kind's owner (never a shared central module — GLOSSARY.md
"interface follows kind-ownership"). `ctx` carries bd access + the harness (§5).

**Split rationale:** `Run` and `Launch` have genuinely different shapes (do-work
vs return-a-command); they are two methods, not one union. This mirrors the
harness Run/LaunchCommand split (§5).

### 4.1 LaunchCommand

`LaunchCommand` is the thing a human runs. It is **not** necessarily an agent
command — it may be an agent session (§5.3) or, for `task`, a checklist of manual
steps. MVP defines two varieties:

- **agent** — argv + cwd/env for the selected harness (produced by the harness,
  §5.3).
- **checklist** — hand-run steps for a `task` bead (rendered by the TUI, §9).

Model it as a sum type (`agent | checklist`); the harness fills only the agent arm.

### 4.2 The registry (static map, Nami's kinds only)

A static Go map in `registry/` **is** the total owner-from-kind function and the
AFK/HITL source. Nothing is derived or stored elsewhere.

```
kind      → owner   mode   container
signal    → nami    hitl   false
grilling  → nami    hitl   false
task      → nami    hitl   false
research  → nami    afk    false
chart     → nami    afk    true
```

Other crewmates' kinds are omitted (YAGNI). The loop reads `owner` to find the
crewmate and `mode` to choose dispatch path (afk→`Run`, hitl→surface+`Launch`).
An **unknown kind → loud error** (a defensive assertion; the loop's scope fence
in §6.1 means it never fires in MVP).

### 4.3 Idempotent succession — `spawnSuccessorIfAbsent`

`spawnSuccessorIfAbsent(ctx, pred, spec)` in `bd/` is the **only** sanctioned
successor-spawn path. It wraps: check-successor-exists → create with
`--no-inherit-labels` → correct edge kind/direction (`caused-by` spine,
`supersede` obsoletion). Handlers never raw-`bd create` a successor.
Idempotency-by-construction: re-running a handler that already spawned its
successor is a no-op — this is what makes the stateless loop (§6) and crash
recovery safe.

## 5. Harness indirection layer

The single choke point for *any* "spawn an agent". Both crewmate methods reach it
via `ctx.Harness()`: AFK `Run` uses `Harness.Run`; HITL `Launch` uses
`Harness.LaunchCommand`.

### 5.1 Seam

```go
package contract // see note — not a standalone harness package

type Invocation struct {
    Prompt string // crewmate-authored; the domain lives here — harness is dumb
    BeadID string // names the session (--name); the natural anchor
}
type Result struct {
    Output    string // agent's final text
    SessionID string // for debugging / deferred resume
}
type Harness interface {
    Run(ctx Ctx, inv Invocation) (Result, error)     // AFK: exec headless, block till done
    LaunchCommand(inv Invocation) (LaunchCommand, error) // HITL: return the command the human runs
}
```

**Reconciled:** the seam lives in `contract`, not a standalone
`harness` package. A crewmate stays harness-blind by reaching it via
`ctx.Harness()` (§5), so `contract.Ctx` must expose `Harness() Harness` — and
`Harness.Run` takes `Ctx` and `LaunchCommand` returns `contract.LaunchCommand`.
A separate `harness` package would make `contract ↔ harness` a Go import cycle,
so the seam types fold into `contract` (which is exactly "the type foundation
every crewmate builds on"). Only the concrete backend needs its own package
(`claudecode/`). Same reconcile pattern as ADR-0007's ctx-first arg order.

The shared "invoke Claude Code with (prompt, bead)" guts live once in the
backend; `Run` execs-and-waits, `LaunchCommand` formats-and-returns.

### 5.2 Selection & model

**One** concrete `claudecode` backend, constructed in `main`, placed in `ctx`.
Swap = a new impl of the interface + one line in `main`; no harness registry, no
per-invocation/per-kind harness choice. Model rides Claude Code's **native**
resolution (`--model` flag > `ANTHROPIC_MODEL` > `settings.json` > default);
mugicrew adds at most an optional `--model` pass-through when a value is
configured. No mugicrew config framework.

### 5.3 Claude Code backend (concrete flags)

- **AFK `Run`** (no TTY):
  `claude -p "<prompt>" --output-format json --permission-mode acceptEdits --allowedTools "Read,Grep,Glob,Bash,Write,Edit" --session-id <uuid> [--model X] --name <beadID>`.
  The session id is a pre-assigned UUIDv4 → `SessionID`; JSON `.result` → `Output`.
  Exit 0 = success; non-zero (incl. 143 SIGTERM / 137 killed), or exit 0 with
  `is_error:true`, = error. On success and error alike the `Result` carries the
  exit/signal, a best-effort `Summary` of the result JSON and the stderr tail
  (last 4 KiB), recorded as `session.*` events (ADR 0029). Scoped allow-list is chosen
  over `--dangerously-skip-permissions` (trust boundary: `bd` needs Bash,
  findings need Write).
- **HITL `LaunchCommand`** (agent variety): argv `["claude", "<prompt>", --session-id <uuid>, (--model X), (--name <beadID>)]`,
  run in the human's terminal (which supplies the TTY), default manual
  permissions. No slash-command preselect flag exists, so **the prompt text
  carries the workflow** (the `/wayfinder`-equivalent instructions the crewmate
  composes).
- Auth (`ANTHROPIC_API_KEY` / OAuth) is the environment's concern, not the seam.

### 5.4 Seam boundary — harness is bd-blind AND content-blind (both paths)

The harness invokes an agent and returns a `Result`. It writes **nothing** to bd,
performs **no** state transitions, and dictates **no** content destination — on
AFK *and* HITL.

- **State transitions** (open→closed, successor spawn) happen **outside** the
  harness, via `spawnSuccessorIfAbsent` + close (§4.3). For AFK this is `Run`
  mutating the graph itself after the run returns (ADR 0003). The spawned agent
  never closes/spawns; the harness never does either.
- **Content destination follows each skill's own convention**, decided
  crewmate-side (§8), not by the harness. Verified pattern: decision/gist →
  tracker (resolution comment / Decisions-so-far / `bd` notes); bulky/throwaway
  artifact → file or branch + a **pointer** on the bead. So research findings = a
  Markdown file + pointer; grilling = a resolution gist on the bead. The harness
  is agnostic to all of it.

## 6. The outer dispatch loop

A stateless activation substrate within ADR 0003. It owns no kind (GLOSSARY.md:
substrate, not a crewmate). Shaping fact: `bd ready` returns open containers even
with open children (the map epic itself sits in `bd ready`), so "drained" is
**not** readable off `bd ready`.

### 6.1 Scope — registry-fenced

The loop polls `bd ready` fenced to beads whose `kind:` is IN the MVP registry.
Non-mugicrew planning beads (no `kind:` label) are skipped. A known-but-unowned
kind (e.g. `kind:spec`, which Nami's stub creates but Zoro owns) is **left
sitting open**, not dispatched.

### 6.2 Cadence & cap

- **Cadence:** blind poll, `15s` default, one flag/env override. No journal-tail
  (ADR 0003 out of scope). Fork-per-poll via bd shell-out is negligible at 15s.
- **Cap:** the per-crewmate concurrency gate IS built (count a crewmate's
  `in_progress` beads; gate dispatch). Value is a hardcoded
  `const cap = 1 // ponytail: hardcoded; per-crewmate config is the upgrade path`.
  The gate earns its keep by stopping a 2nd `research` Run firing while the 1st is
  live.

### 6.3 AFK-only

The loop dispatches **only** `mode:afk` ready work via `Run`. It never reads,
claims, marks, or pushes `mode:hitl` beads — the TUI (§9) queries bd independently
for those. Zero shared state between loop and TUI beyond bd itself.

### 6.4 Tick shape — two detections

```
1. inflight  = count in_progress beads per owner          (busy re-derived from bd; ADR 0003)
2. leaves    = bd ready ∩ {registry kinds: mode:afk, container:false}   (MVP: research)
3. drained   = open beads of {registry kinds: container:true}
               with 0 OPEN children AND label chart:defogged             (MVP: chart; see §8.3)
4. candidates = leaves ++ drained, in bd-ready order
5. for each candidate: if inflight[owner] < cap: claim + dispatch; inflight[owner]++
6. sleep(15s); repeat
```

Two scoped bd queries per tick (ready AFK leaves; open containers → children +
label check). A ready leaf → `owner.Run`; a drained Chart → the same `owner.Run`
(the crewmate switches on `bead.Kind` internally). The loop keeps **no** in-memory
coordination state — all re-derived from bd every tick (stateless / self-healing).

### 6.5 Dispatch — claim synchronously, then Run in a goroutine

An AFK `research` Run is agentic (minutes); a synchronous call would freeze
polling. At dispatch the loop sets the bead `in_progress` **synchronously, before
spawning** (this makes the next tick's busy-count truthful and prevents
re-dispatch), then invokes `Run(ctx, bead)` in a **goroutine** and continues
ticking. `Run` does the work and mutates the graph itself (close +
`spawnSuccessorIfAbsent`); the loop never touches the bead again.

**Crash safety:** claims carry no lease (ADR 0052). A crash, SIGKILL or force
stop mid-`Run` leaves the bead `in_progress`, and nothing re-dispatches it: it
needs a manual `bd unclaim` until graph repair (the Chopper Signal) exists.
The loop never double-dispatches; the cost is a stranded claim, named by its
`effort.abandoned` event on a force stop. Retry/error *semantics* are fog (§11).

### 6.6 Running the loop (ADR 0050, ADR 0053, ADR 0060)

One loop per repo, held by a flock on `<git-common-dir>/mugicrew/loop.lock`.

**Pinned build (ADR 0060).** Both loop verbs run the shared repo's `main`, whatever
`mugicrew` binary you typed: they resolve main's SHA, `nix build` that rev's
`packages.mugicrew` (never your checkout or its dirty edits) and exec the
`/nix/store` binary, which leads the loop's PATH with its own bin dir. A held
lock is refused before any build; a failed build is `loop.failed` with nix's
error verbatim, and no loop runs. A warm start (main already built) costs about
1.5s of nix eval; a cold start after a mainline change takes tens of seconds.
The TUI, HITL sessions and the devShell's bare `mugicrew` stay unpinned; only
the loop verbs pin.

**Testing loop changes.** There is no `--dev` flag or env var. Clone the repo,
check your branch out as `main` in the clone, and run the loop there: it builds
the clone's `main` and runs on the clone's own lock, event log and beads.

- `mugicrew loop`: foreground, output on the terminal; execs the pinned build in
  place, so the pid, terminal and signals stay the same.
- `mugicrew loop start`: execs the pinned build's `loop` under `setsid`, so it outlives
  the shell or TUI that started it, with the starter's env and cwd. It returns once
  the child holds the lock. A start against a held lock is refused (`loop is
  already on (pid N)` / `loop is draining (pid N)`). The detached loop's
  stdout/stderr (panic traces, child tools' raw output) go to
  `<git-common-dir>/mugicrew/loop.out`, beside `events.jsonl`, truncated on each
  start and never rotated.
- `mugicrew loop status`: `off`, `on (pid N, main <short-rev>, <store path>)` or
  `draining (pid N, main <short-rev>, <store path>, K in flight: …)`. An `on`
  loop whose build of a newer main failed adds a second line, `stale: build of
  main <short-rev> failed (see build.failed in events)`. `--json`
  prints the lock body as one object (`state`, `pid`, `rev`, `store_path`,
  `inflight`, `restart_to`, `failed_rev`; empty optional fields omitted).
- `mugicrew loop stop [--force]`: SIGTERM drains (waits for in-flight efforts);
  `--force` abandons them, leaving their claims `in_progress`. Force is a second
  SIGTERM, sent once the loop has written `draining` (the runtime coalesces an
  undelivered repeat); if that takes over 10s, `--force` errors and says to run
  `loop stop` again, which forces a draining loop at once.

**Known limit (WSL2):** a detached loop dies with the WSL distro: on `wsl
--shutdown`/terminate/reboot, and on the idle shutdown about 15s after the last
*Windows* client exits (Linux processes never keep the distro alive). Opt-in fix:
`instanceIdleTimeout=-1` under `[general]` in `%UserProfile%\.wslconfig`.

## 7. Signal filing + triage

Signal *ontology* is ADR 0006; this fixes the MVP *mechanics*, on top of the §4
contract.

### 7.1 Filing — `mugicrew signal new`

Thin CLI: `mugicrew signal new --title=… --description=…` wraps
`bd create --type task -l kind:signal` as a **chain head** (no parent,
`--no-inherit-labels`). Fields = title + description only. No `signal-kind:*`
variety label (ADR 0006 defers it until a 2nd variety appears). The CLI is the
one surface both humans **and** external systems hit; TUI filing is deferred.

### 7.2 Triage — `mugicrew signal triage <id>`

Triage is HITL via the `Launch` seam (`signal=hitl` → `Nami.Launch`). The verb
resolves the Signal, calls `Nami.Launch(signal)`, and runs the returned launch
command — an interactive **charting session**, the mugicrew equivalent of
`/wayfinder` on a loose idea. This makes `triage` a **second `Launch` invoker**
alongside the TUI. The backlog a human picks from = `bd list` scoped to open
`kind:signal`; §9 renders it and calls this verb. **§7 owns the verb; §9 owns the
view.**

### 7.3 Triage happens inside the session

Mirroring `/wayfinder` (the Chart is created *after* grilling names the
destination, not up front): the session launches pointed at the **Signal** (Chart
not yet spawned). Inside, Nami grills, then:

- **accept** → `reconcile(signal)` (§8.2) ensures its one Chart successor
  (`spawnSuccessorIfAbsent`, `caused-by`; ADR 0006 1:1 invariant) + close the
  Signal.
- **reject** → close the Signal with a reason, no Chart. Handled **inside** the
  session (`bd close --reason`); no separate `signal reject` CLI verb (YAGNI).

**Boundary:** §7 owns the front door (`signal new` / `signal triage`), the
`Launch` invocation, and the *contract* that a triage session yields at-most-one
Chart in succession OR a reasoned reject. §8 owns everything once the Chart
exists. §7 and §8 are two phases of the *same* charting session.

**MVP subset of ADR 0006:** accept + reject-with-reason only, both in-session.
Dedup/supersede and offshoot-Signals are deferred (§10 / OOS).

**Build order:** `triage`'s launch wiring consumes §5's `LaunchCommand`, so build
the harness (§5) before this.

## 8. Nami's defog seeding + drained-Chart→Spec

Core frame: **Nami *is* the wayfinder skill.** Chart ↔ map, defog beads ↔
wayfinder tickets, "defogged" ↔ frontier clear, and
`kind:grilling/research/prototype/task` ↔ the four `wayfinder:<type>`s.

### 8.1 Defog seeding = the charting session, not Go

On accept, the launched interactive agent session (agent variety, §5.3) runs the
wayfinder charting flow: names the destination, writes the Chart body as a
wayfinder map (Destination / Notes / Decisions-so-far / Not-yet-specified /
Out-of-scope), and spawns the initial defog children via bd — mixed kinds,
one *or more*. The Go "Nami" crewmate holds **no** charting logic; it is plumbing
only: the `Launch` command, the `reconcile` verb, and the drained-Chart `Run`
handler. Charting *quality* is OOS ("Nami charts well").

### 8.2 `reconcile` — the unifying idempotent primitive

`mugicrew reconcile <bead>`: given a bead + its graph neighborhood, drive the
graph to its conformant next state. It subsumes `spawnSuccessorIfAbsent` (that
helper becomes reconcile's "ensure successor if absent" leg) and matches the
stateless/self-healing loop. Two callers:

- **accept path (§7.3):** `reconcile(signal)` → ensure its one Chart successor.
  (This names §7's previously-unnamed accept verb.)
- **`Nami.Run(chart)` IS `reconcile(chart)`** (§8.4).

Chart succession goes through `spawnSuccessorIfAbsent` (the only sanctioned
successor path). Defog **children are membership**, not succession → plain
parented `bd create --parent=<chart> -l kind:<type> --no-inherit-labels` issued by
the charting session; no helper.

### 8.3 Completion signal — explicit `chart:defogged` label

A Chart is ready-to-spec **only** when it carries the **`chart:defogged`** label —
NOT "0 open children". "0 open children" is transient: true mid-charting (Chart
created before its first ticket) and true again whenever the last promoted fog
bead closes while the map body still carries un-promoted "Not yet specified" fog.
`chart:defogged` is the explicit assertion that the frontier reached the
destination (no open children AND no un-promoted fog), set **either** by the HITL
charting flow when a resolution clears the last fog and promotes nothing further,
**or** by an AFK promote-or-stamp run (§8.4) that judges a drained Chart genuinely
complete — wayfinder's "the way is clear" either way. This **subsumes** any "≥1-child-ever" race
guard: an unmarked childless Chart is simply "not done", so the newborn /
mid-charting drain race disappears by construction.

### 8.4 `reconcile(chart)` — the drained-Chart→Spec handler

`reconcileChart` discriminates on the drained Chart's body + label (ADR 0021/0022):

- **un-charted body** (Description still the Chart placeholder) → return nil:
  nothing to judge until the charting session (§8.1) writes the map body.
- **charted but not `chart:defogged`** → dispatch the **promote-or-stamp** AFK
  judge anchored to the Chart, then return **thin** (no re-read, no close — the
  agent's stamp-or-spawn IS the state change). The judge reads the Chart body AND
  gathers the closed children (authoritative, required — not the advisory
  Decisions-so-far), bucket-sorts every "Not yet specified" bullet, then:
  **PROMOTE** remaining specifiable fog into fresh defog children (Chart un-drains,
  charting continues) / **STAMP** `chart:defogged` when nothing promotable is left
  / **ESCALATE** by spawning one grilling child when a load-bearing bullet can't be
  confidently classified — never blind-stamp. This is the real seam the branch used
  to defer: the AFK fog-promotion once punted to the HITL working sessions.
- **`chart:defogged` present AND 0 open children** → **land** the Chart branch (a
  rebase conflict files one chart-conflict member, ADR 0011), `spawnSuccessor(chart
  → spec)` for the **one** `kind:spec` bead, then — if the Spec body is still the
  placeholder — run the AFK **speccing session** over the Chart, which authors the
  Spec body via `bd update`. Close the Chart **only once the body is authored**
  (gated close); on failure the Chart stays open for the next tick. The Spec is
  Zoro's kind, **unowned** by the MVP registry → it sits open as the terminal
  artifact (§6.1).
- **Loop gating (refines §6.4):** drained-Chart detection = open `kind:chart` ∩ 0
  open children ∩ charted body (not the placeholder). It NO LONGER gates on
  `chart:defogged` — both the promote-or-stamp and land flows route
  through one candidate path, and `reconcileChart` discriminates downstream.

### 8.5 Post-seed advancement — no new mechanism

Each seeded defog bead is worked by its registry mode: `grilling`/`prototype`
(HITL) surface in the TUI → `Launch` → session → closed; `research` (AFK) is
picked up by the loop → `Run`. Whoever works a bead closes it; `chart:defogged` is
set when the frontier clears — by the charting flow, or by the promote-or-stamp AFK
run (§8.4) once a drained Chart drains empty. Once labelled + all children closed,
the loop dispatches `reconcile(chart)` → Spec + close.

### 8.6 Spec count

The handler emits exactly **one** Spec. The ontology's "one or more Specs"
(ADR 0002 / GLOSSARY.md) is left **unchanged** — "one" is a valid, forward-
compatible instance. The narrow-to-exactly-one revision candidate is filed off
this route (since settled by ADR 0008).

## 9. The human-action TUI

A tabbed bubbletea app. **Three tabs:** Graph, Inbox, Logs (§9.6).

**Tab bar** (ADR 0051): `Graph Inbox(N) Logs  · N live · loop off|on|draining N`,
`Inbox(N)` counting open HITL beads; the loop state (lock probe, as `loop status`)
is re-read on the §9.5 tick and right after `o`. **`o`** is a view key on every tab,
just before `r` (`o loop on` / `o loop off` / `o force-stop`): it shells out to
`mugicrew loop start` / `loop stop`, and while draining y-confirms `loop stop
--force`. The TUI never owns the loop (§6.6), so a TUI-started loop survives `q`.

**Scope shift (a human call):** the human surface widened from "HITL beads
awaiting action" to "**see + interact + intervene on the whole graph**" as the
MVP centerpiece, with the HITL inbox secondary.

### 9.1 Graph tab (MVP-primary)

Whole-graph master/detail.

- **Left pane (master):** beads **grouped by Chart** (parent container). Every
  kind & state shown, incl. closed lineage (struck through) and AFK work the loop
  is running (e.g. loop-claimed `research`). Chart/parent groups **and** an
  "Unparented" group (open Signals and Rollbacks; was "Untriaged
  Signals") are **collapsible** (caret in a left gutter, aligned
  so a chart badge is not more indented than its children). Chart-head rows are
  **selectable** and render their own detail (state, children N/M closed, a
  `drain-watched → chart:defogged spawns Spec` note per §8).
- **Right pane (detail):** the selected bead's detail + action set (§9.3).
- **Nav within master:** `j`/`k` walk the visible tree (chart heads included;
  children under a collapsed group skipped).

### 9.2 Inbox tab (secondary)

Flat, one-line-per-bead HITL queue: **open HITL kinds only**
(signal/grilling/task/prototype), freshest-first, detail + launch docked below.

Amended by ADR 0038: a HITL bead with a session stays in the Inbox whatever its
status, so a closed or blocked bead's session can still be reattached or dropped.

**Filter invariant:** AFK kinds (research, drained Chart→Spec) **never** gate the
Inbox — the loop runs them. They appear in the Graph tab for observe/intervene
only.

### 9.3 Detail action set (intervention) = **launch · edit** (+ session `l`/`x`)

Amended by ADR 0038 §3–4: on a bead with a detached session, `l` reattaches an
alive session or resumes an exited one instead of launching, and `x` drops the
session (confirm first).

- **launch** → the owning crewmate's `Launch` → emits the launch command. Two
  varieties (§4.1 / §5.3): **agent** (`claude "<charting/grill/proto prompt>"`
  for signals/grilling/prototype — a Signal via Nami's `Launch` directly, the
  `mugicrew signal triage <id>` verb being CLI-only, ADR 0039 §6) vs
  **checklist** (`task` kind → hand-run steps).
- **edit** → general escape hatch; subsumes unclaim/close/reopen (set any field
  via `bd`). NB: `bd edit` blocks agents, but a human-driven TUI is fine.
- **view is dropped** (two-pane already shows detail on select). **No manual
  run-now** in MVP (the loop owns AFK dispatch).

### 9.4 Navigation

Arrows / tab-row switch Graph ↔ Inbox ↔ Logs; `tab` moves focus between master & detail
panes.

### 9.5 Read-consistency (noted, not fully specified)

The Graph tab is a live whole-graph admin view over the writing loop, so refresh
cadence vs concurrent loop writes is a real question. With bd shell-out
(process-per-call) contention is light; a simple periodic refresh is adequate for
MVP. This sharpens if in-process bd is adopted (§10).

Prototype (6 iterations) captured on a `prototype/` TUI branch +
artifact `476BcWU8rZKUXorMCUwbk9`.

### 9.6 Logs tab

The **event log** (ADR 0027–0029, 0033), rendered raw for post-mortem. Master/detail:
one row per event, newest first (time, level, event name, bead); the detail
lists every attr of the selected event, a multi-line `err` or `stderr` intact
and a group (`session.failed`'s `result`) as indented sub-keys. It reads
`events.jsonl.1` then `events.jsonl` under `$(git rev-parse --git-common-dir)/mugicrew/`
as one stream, skips unparseable lines with a visible count, and re-reads on the
§9.5 tick only when the files' stat changed. Debug records are hidden unless
`d` toggles them in (ADR 0027 §4); `e` toggles errors-only. `L` on a Graph
selection opens the tab scoped to that bead — a container to `bead==X ||
parent==X`, its direct children (ADR 0029 §3) — and `esc` clears it; the tab bar
names the active scope and filters. It never infers liveness: a
`started` with no end is shown as exactly that (ADR 0033 §3). Footer: view keys
only (`[tab …] · d · e · [esc unscope] · o · r · q`); `L` is a Graph bead key (ADR 0026 footer).

## 10. Build order

1. **Scaffold** — delete `apps/factory`; init `apps/mugicrew` Go module (§3).
2. **`bd/`** — shell-out helper + `spawnSuccessorIfAbsent` (§3.1, §4.3).
3. **`contract/` + `registry/`** — `Crewmate`, `LaunchCommand`, the static map (§4).
4. **`harness/`** — seam + `claudecode` backend (§5). *Before* §7 and §9, which
   consume `LaunchCommand`.
5. **`loop/`** — the outer dispatch loop (§6).
6. **`nami/`** — `signal new` / `signal triage` front door (§7); `reconcile` +
   drained-Chart→Spec handler (§8).
7. **`tui/`** — the bubbletea app (§9).

## 11. Deferred / fog (from the map)

Recorded here so a builder doesn't mistake a deliberate gap for an omission:

- **Loop error/retry behavior** when a handler throws (§6.5: a crash strands the
  claim, with no lease to expire; retry *semantics* are undecided).
- **Kind-specific `Result` payloads** — `harness.Result.Output` is a plain string;
  a typed per-kind payload is a likely later need.
- **TUI-vs-loop read-consistency** (§9.5) — refresh cadence over concurrent loop
  writes; sharper if in-process bd is adopted.
- **In-process bd upgrade** — import beads as a Go library over embedded Dolt;
  gated on a single-writer concurrency model + CGO build (OE ADR 0007).

## References

- Ontology: `apps/mugicrew/GLOSSARY.md`, system `GLOSSARY.md`.
- ADRs: 0001 (single app, crewmate seams), 0002 (bead taxonomy), 0003 (stateless
  dispatch loop), 0006 (Signal intake human-gated), OE ADR 0007 (Go runtime +
  polyglot repo). ADRs 0004/0005 are tail-crew, out of scope here.
