# ADR 0019: mugicrew Franky — the AFK build/critique/conflict workhorse: agent authors + commits, Go drives one spawn-before-close re-review transition

Status: Accepted
Date: 2026-09-24

Builds on: ADR 0002 (owner-from-kind routing), ADR 0003 (stateless dispatch +
lease re-dispatch), ADR 0005 (pivot triage is Zoro's, an offshoot Signal), ADR 0016 (the
Checkpoint read-model + the tail's leaf-close-spawns-next / spawn-before-close
orchestration, `approvedAtHead` land gate, and event-driven re-review), ADR 0017
(the Sanji precedent: thinnest interface, an embedded prompt that carries a skill
whole and replaces only its terminal step, the live-skill dependency folded by
an offshoot Signal), and ADR 0018 (the Usopp precedent: **hybrid** AFK — the agent authors,
Go drives the dangerous mutation through tested idempotent helpers, because AFK has
no human backstop). Records the input to the Spec for the Franky crewmate.

## Context

Franky owns `build`, `critique`, and `conflict` (CONTEXT.md; registry AFK). He is
the workhorse of the tail and the **first crewmate that mutates the Checkpoint
branch** — Usopp is read-only, Sanji lands to a throwaway side ref, Jinbe is
pure-Go. That makes the AFK-idempotency question ADR 0018 raised (and dodged, by
being read-only) land squarely here: Franky commits real code to
`checkpoint/<id>` with **no human backstop** and lease re-dispatch on crash.

All three kinds share one job shape — *rework the branch, then hand a fresh
Checkpoint state to the next stage*:

- **Build** — implement one sliced unit in a worktree of `checkpoint/<parent-id>`,
  commit to that branch; on the last Build, the Checkpoint is ready for its first
  Review (ADR 0016 §2).
- **Critique** — resolve a filed defect (Usopp-raised, or Build-raised offshoot),
  commit the fix; a clean set of critiques hands back to re-review (ADR 0016 §8).
- **Conflict** — Jinbe aborted a rebase and filed a `kind:conflict` carrying the
  target base SHA; Franky re-drives the rebase, resolves, commits; the re-landed
  commit must be re-reviewed (ADR 0016 §8), and Jinbe re-lands later via the loop's
  Checkpoint drain-candidacy arm (ADR 0016 §7).

Two facts framed the defog. First, the tail already ships tested, idempotent
helpers — `SpawnChildIfAbsent` (parent-edge, no-ops on an existing open child of
the kind), `BuildChildren`/`childBeads`/`OpenChildren` (count children), and
`Close` — and Zoro's `reconcileSpec` is the roll-forward template (skip the
expensive session when the artifact already exists; each leg individually
idempotent). Second, `route.Route` already anticipates Franky
(the `ponytail:` note: "a Build leaf extends the same pattern — worktree of
`checkpoint/<parent-id>`") but has **no case** for `critique`/`conflict`, which the
tail-seams inventory explicitly flagged for the Spec to add or defer.

## Decision

**1. Interface & dispatch — thin, three AFK handlers.** Franky is a stateless
`struct{}`. `Franky.Run` reads the bead then `Reconcile` switches on `bead.Kind`
into `reconcileBuild` / `reconcileCritique` / `reconcileConflict` (the
Zoro/Nami shape). `Franky.Launch` returns a loud "build/critique/conflict is AFK,
not a launchable HITL kind" error (Zoro/Usopp default arm). No decision packet, no
rich domain interface — his interface *is* the three handlers; any TUI/query
affordance is deferred (Chart Not-yet-specified). Wiring: registry rows
`{Owner: "franky", Mode: AFK, Container: false}` for `KindBuild`, `KindCritique`,
`KindConflict`, and a `crew()["franky"]` entry in `main.go`. (Depends on the
registry-driven-dispatch verb fix — a hard prerequisite for any AFK
worktree crewmate.)

**2. Routing — one shared worktree arm for all three kinds.** `route.Route` gets a
single arm `case KindBuild, KindCritique, KindConflict:` →
`Destination{Placement: Worktree, Branch: checkpoint/<parent-id>}`, reusing the
existing `errNoParent` guard (all three are Checkpoint members). Git's own
"one branch checked out in one worktree" rule then serializes concurrent Franky
sessions on the same Checkpoint as `IsBusy` (the existing contention seam) — no new lock. The loop manages the checkout (Worktree placement =
loop-managed working tree, unlike Sanji's SideRef self-cut); Franky stays
pure-Go + prompt.

**3. Execution model — HYBRID: the agent authors + commits, Go drives the graph
mutation (the Usopp lesson, applied to a writer).** The agentic session does the
authorship the harness can't mechanize — writes code + tests and **commits to the
checked-out `checkpoint/<parent-id>`** (there is no `git.Commit` helper, and
committing arbitrary authored work is not a mechanizable Go op the way spawn/close
are). On the agent's return, the Go handler drives the deterministic transition
(decision 4) through the tested idempotent helpers, so ADR 0003 lease re-dispatch
never corrupts the graph. **AFK idempotency rides on roll-forward, not a new
structural marker:** the prompt tells the agent to inspect the worktree/diff first
and **commit only if there is uncommitted work**, so a re-dispatch after a
"committed-but-not-closed" crash converges (mirrors `reconcileSpec`'s
"skip the fork if Builds already exist" and Usopp's "check existing children
first"). No `git.Commit` helper and no per-build marker trailer — added only if
roll-forward proves insufficient (YAGNI).

**4. Transition — ONE unified spawn-before-close re-review, shared by all three
handlers.** Every handler, after the agent returns, drives the same succession:

- compute `readyForReview` = **zero open Builds AND zero open Critiques AND zero
  open Conflicts** on the Checkpoint, **excluding this bead**;
- if `readyForReview` (this leaf is the last rework outstanding) →
  `SpawnChildIfAbsent(checkpoint, {Kind: review})` **FIRST**;
- then `Close(this bead)`.

Spawn-**before**-close is load-bearing, not decorative: the loop only re-dispatches
*open* beads, so a close-then-crash-before-spawn would strand the Checkpoint (the
now-closed leaf is never re-dispatched, the Review never spawns). Computing "am I
the last?" before the close, and spawning the (idempotent) Review first, makes
"predecessor closed, successor missing" unreachable — a re-dispatch of the
still-open leaf re-runs, sees the Review already present, and closes. The unified
predicate also correctly **holds off the Review while sibling Critiques from the
same Usopp pass are still open** (Usopp files several critiques per not-clean pass,
ADR 0018 §4 — you must not re-review after the first fix). The Review child is
spawned **bare** — no `Commit:` pin — because Usopp captures `git.Head` at review
*start* and pins it himself (ADR 0018 §7). New Go seam: a "count open children of
kind K" helper extending the `BuildChildren` pattern (ADR 0016 §1 already called
for it); `SpawnChildIfAbsent` and `Close` are reused verbatim.

**5. Prompts — two embedded files, each carrying a skill whole (the Usopp/Sanji
pattern).** `//go:embed`-ed, seeded per bead, same scaffold as `nami/prompts/*`:

- `franky/prompts/build.md` serves **both** `build` and `critique` (a critique
  *is* a defect — the same "make the code correct, test-first" job). It drives the
  repo's **`implement` skill** whole (invoked live) — which itself bundles `/tdd`
  at pre-agreed seams, regular typechecking + single-file tests, a full-suite run
  at the end, a `/code-review` self-pass, and "commit your work to the current
  branch." **What is replaced:** only the terminal graph mutation — the *close* and
  *succession* are Go's (decision 4); the commit stays with the agent (decision 3).
  The `implement` skill's own `/code-review` self-check is **kept** (a cheap
  in-session churn-reducer that files nothing and never gates the Checkpoint —
  fewer defects reaching Usopp serves the Chart's unattended-bias preference).
- `franky/prompts/conflict.md` drives the **`resolving-merge-conflicts` skill**
  whole. Because Jinbe *aborted* the rebase (clean tree, no rebase in progress,
  ADR 0016 §6), the prompt first **re-runs `git rebase <base>` to reproduce the
  conflict**, then invokes the skill to resolve + continue + commit. `<base>` is
  parsed from the `conflict` bead body (`Base: <sha>`), a small parser mirroring
  the `Commit:`-pin parser (ADR 0016 §5).

The live-skill dependency is **interim**, folded into `.agents/docs/mugicrew/` by
offshoot Signal — named here so Franky, Usopp, and Sanji can't drift.

**6. Conflict resolution stops at re-review — Franky never lands, never touches the
Approval.** `reconcileConflict` resolves + commits the rebased branch and runs the
decision-4 transition (a fresh Review). It does **not** land (Jinbe re-lands via
the loop's `approvedAtHead`/land-trailer drain candidacy, ADR 0016 §7) and does
**not** obsolete the stale Approval — the pin already makes it inert (ADR 0016 §8,
obsoletion "safety-optional"), and a fresh clean Review spawns a new Approval since
the old one is closed. No `supersede` call, no land call in Franky.

**7. Raising work during a Build — Critique in scope, Pivot deferred.** If a Build
agent finds a defect *beyond its own slice*, `build.md` tells it to file a
`kind:critique` **offshoot of the Build** (agent-authored — a non-dangerous
mutation, and `reconcileCritique` dedupes on resolve, ADR 0018 §5). If the agent
believes the **Spec itself** is wrong, it **stops and flags for a human** —
`pivot` triage is out of scope, owned by an offshoot Signal (ADR 0005), already on the
Chart's Out-of-scope list. Both critique sources (Usopp-raised and Build-raised)
resolve through the same `reconcileCritique`, so this adds no Go.

> **Amended by ADR 0043:** the `human`-label placeholder is retired. The agent
> raises a `kind:pivot` discovered-from its bead; Franky's tail wires the
> Checkpoint gate and closes the bead `pivoted: <pivot>`.

## Consequences

- Franky ships as `franky/franky.go` + `franky/prompts/build.md` +
  `franky/prompts/conflict.md` + three registry rows + one `crew()` entry + one
  `route.Route` arm. New Go seams are minimal: a "count open children of kind K"
  helper (extends `BuildChildren`, ADR 0016 §1) and a `Base:` body parser (mirrors
  the pin parser). `SpawnChildIfAbsent`, `Close`, `OpenChildren` are reused
  verbatim; no `git.Commit` helper, no new git op (the agent rebases/commits in the
  loop-managed worktree via the shell).
- One unified re-review transition covers all three kinds — last-Build-close,
  critique-close, and conflict-close all converge on `readyForReview` +
  spawn-before-close. Idempotency is Go-guaranteed on the dangerous edges; the
  residual re-dispatch risk is a re-run authoring session, bounded by the
  roll-forward "commit only if dirty" gate.
- Franky carries a live dependency on the `implement` and
  `resolving-merge-conflicts` skills until that Signal lands; named here so they can't
  drift.
- Concurrency on one Checkpoint is serialized by git-worktree `IsBusy`; no new
  locking primitive.

## Rejected / deferred

- **Go-side commit helper / per-build marker trailer** — rejected for the first
  cut: roll-forward ("commit only if dirty") + idempotent close/spawn already make
  re-dispatch converge; a structural marker is added only if that proves
  insufficient (decision 3).
- **Three separate transitions (one per handler)** — rejected: all three reduce to
  the same `readyForReview` predicate; one path is less to get wrong (decision 4).
- **Franky proactively `supersede`-ing the stale Approval on conflict-resolve** —
  rejected: the pin makes it inert and a fresh Review respawns the Approval anyway;
  obsoletion is "safety-optional" (ADR 0016 §8, decision 6).
- **Splitting `critique` into its own prompt** — rejected: a critique is a defect;
  the `implement`/`tdd` job is identical, so `build.md` serves both (decision 5).
- **Pivot handling** — out of scope; owned by an offshoot Signal (ADR 0005, decision 7).
- **A distinct route placement for conflict (in-place on the branch vs. a fresh
  worktree checkout)** — rejected: all three kinds mutate `checkpoint/<id>` and want
  the same loop-managed worktree; one shared arm (decision 2).
