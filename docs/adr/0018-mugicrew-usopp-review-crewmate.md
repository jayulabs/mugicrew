# ADR 0018: mugicrew Usopp — a hybrid AFK reviewer that files findings as beads and lets Go drive the clean→Approval transition

Status: Accepted
Date: 2026-09-24

Builds on: ADR 0002 (owner-from-kind routing), ADR 0003 (stateless dispatch +
lease re-dispatch), ADR 0016 (Checkpoint read-model + the tail's
leaf-close-spawns-next / spawn-before-close orchestration, which already pinned
Usopp's pin mechanics and the clean→Approval succession), and ADR 0017 (the Sanji
precedent: thinnest interface, an embedded prompt forked from a skill, the live
`code-review` skill interim-referenced until an offshoot Signal folds it in). Records the input to
the Spec for the Usopp (review) crewmate.

## Context

Usopp owns `review` (CONTEXT.md; registry AFK). A Review is **one pass over the
Checkpoint, pinned to the commit hash it reviewed**; a pass that closes with zero
open Critiques is *clean* and spawns the Approval in succession to that Review
(ADR 0016 §2, §5). Franky (not Usopp) spawns the Review on last-Build-close and
spawns the *next* Review on critique/conflict-close — re-review is event-driven
(ADR 0016 §8). So Usopp's own job is bounded: review the pinned commit, raise
findings, and hand a clean pass to Luffy.

Two facts framed this defog. First, the tail already ships tested,
idempotent-by-construction helpers — `SpawnChildIfAbsent` (parent-edge, no-ops on
an existing open child of the kind), `BuildChildren`/`childBeads` (count children
by kind), `Close`, and `git.Head` as the pin source. Second, Sanji's
"agent authors every mutation" is safe only because Sanji is **HITL** — a human
authorizes each `bd`/`git` command. Usopp is **AFK with lease re-dispatch** and
has no human backstop, so a naive port of the Sanji pattern would re-implement
spawn-if-absent idempotency in prose and could double-approve on a mid-run crash.

The originating Spec (the `code-review` skill's Spec axis) is reachable the same
way Zoro derives the Checkpoint base: the Checkpoint is spawned
`SpawnSuccessorIfAbsent(spec, …)`, so `CausedBy(checkpoint, KindSpec)` walks
Review→(parent) Checkpoint→(caused-by) Spec.

## Decision

**1. Interface & dispatch — AFK, `Launch` errors loudly.** Usopp owns `review`
only and is AFK. `Usopp.Run` reviews the pinned commit and drives the transition
(below); `Usopp.Launch` returns a loud "review is AFK, not a launchable HITL
kind" error, mirroring Nami's `Launch` default and Sanji's inverse. Wiring: a
registry row `{Owner: "usopp", Mode: AFK, Container: false}` for `KindReview` and
a `crew()["usopp"]` entry in `main.go`.

**2. Review is READ-ONLY — no route worktree.** `review` gets no route case, so it
falls through to `RepoRoot` (non-checkout git only) — zero new `route` code. The
pass runs a three-dot diff `main...checkpoint/<id>` (against the merge-base;
`checkpoint/<id>` was cut from the landed mainline base, so no stored base is
needed). Usopp mutates nothing on any tree.

**3. Prompt — an embedded `usopp/prompts/review.md`, mirroring the Nami/Sanji
scaffold; the live `code-review` skill carries over WHOLE, only its report step
replaced.** `//go:embed`-ed, seeded per bead. **What carries over:** the entire
`code-review` skill invoked live ("Call the Skill tool for `code-review`") — both
axes (Standards, incl. the Fowler smell baseline; and Spec), the parallel
sub-agent structure, the fixed point `main...checkpoint/<id>`, spec source via
`CausedBy(checkpoint, KindSpec)`, standards source = repo `CONTEXT.md`/CLAUDE.md.
**What is replaced:** the skill's human-facing *aggregate-and-report* step becomes
the mugicrew resolve contract below (findings → critique beads). The live-skill
dependency is **interim**, folded into `.agents/docs/mugicrew/` by offshoot Signal
— named here so Usopp and Sanji can't drift silently.

**4. Findings → Critiques — per-blocking-finding, plus one conditional nit bead.**
The agent files **one `kind:critique` child of the Checkpoint per confirmed
blocking finding** (a real bug, a spec miss, a documented-standard breach).
Judgement-call smells and nits do **not** block; they are collected into a
**single aggregate "nits & smells" `kind:critique`** filed **only when ≥1 blocking
critique is also filed in that same pass** — never on an otherwise-clean pass.
Because the nit bead is a `kind:critique` (Franky-owned), it gets a cleanup pass
while the branch is already being reworked, and its close participates in the
re-review gate — but it can never stall an otherwise-clean branch into re-review
churn. Without this severity gate an unattended run would stall forever on style
nits, defeating the Chart's unattended-bias preference.

**5. Transition — HYBRID: the agent files findings, Go makes the decision.** The
agentic session ends at *filing critique beads* (step 4), or filing nothing if
clean. Then `Usopp.Run`, on the agent's return, **reads the graph** — the count of
open `kind:critique` children of the Checkpoint (the `BuildChildren` pattern) —
and drives the deterministic transition: **0 open →
`SpawnChildIfAbsent(checkpoint, {Kind: approval})` stamped `Commit: <pinned sha>`**;
then **`Close(review)` in every case**. Go reads *beads the agent wrote*, never
its freeform output, so there is no brittle text-parse seam; and the dangerous
edge (approve / close) runs through the tested idempotent helpers, so ADR 0003
lease re-dispatch never double-approves. A re-dispatched agent could at worst
double-*file* a critique (low cost; Franky dedupes), softened by a
"check existing children first" line in the prompt.

**6. The Approval — a membership child of the Checkpoint carrying the pin; no
caused-by edge.** `approvedAtHead`/`BuildChildren` count the Approval as a
Checkpoint child (ADR 0016 §7), so the child edge is required; the "certifies this
commit" succession semantics are carried entirely by the copied `Commit: <sha>`
body line. No mechanic (count, land gate, obsoletion) reads a caused-by edge, so
none is added.

**7. Pin — captured at review start, no re-check.** `git.Head(checkpoint/<id>)` is
read once at the start; the pass reviews *that* commit and stamps it on the Review
(the Approval inherits it, decision 6). If a late Build lands mid-review and HEAD
advances, Usopp does nothing special: the pin records what was actually reviewed,
Jinbe's `approvedAtHead` gate simply won't fire (pin ≠ HEAD), and the late Build's
succession spawns a fresh Review (ADR 0016 §8). Self-healing; no HEAD-poll, no
re-check branch.

## Consequences

- Usopp ships as `usopp/usopp.go` + `usopp/prompts/review.md` + one registry row +
  one `crew()` entry. The Go transition reuses `SpawnChildIfAbsent`,
  `BuildChildren`/`childBeads`, `Close`, and `git.Head` verbatim — no new helper.
- One review bead == one pass == one pinned SHA. Not-clean passes close their
  Review after filing critiques; the next pass is a fresh Review bead spawned by
  Franky's critique-close succession (ADR 0016 §8), not by Usopp.
- Usopp carries a live dependency on the `code-review` skill until that Signal lands;
  named here so the two can't drift.
- Approve/close idempotency is Go-guaranteed; a mid-run crash rolls forward and
  never double-approves. Residual risk is a re-filed critique on re-dispatch —
  cheap and Franky-dedupable.

## Rejected / deferred

- **Full agent-drives (the Sanji port)** — rejected for Usopp: safe under HITL
  authorization, unsafe AFK, and it throws away the tested idempotent helpers.
- **A caused-by-Review edge on the Approval** — rejected: dead lineage no mechanic
  reads (decision 6).
- **A HEAD re-check before spawning the Approval** — rejected: reimplements the
  obsoletion machinery ADR 0016 §8 already provides (decision 7).
- **A separate adversarial verify pass over findings** — deferred (not in scope);
  the skill's own judgement gates "confirmed" findings for now.
