# ADR 0017: mugicrew Sanji — a thin HITL prototype crewmate that lands throwaway code to a local side ref

Status: Accepted
Date: 2026-09-24

Builds on: ADR 0002 (owner-from-kind routing), ADR 0009 (prototype output goes
to a side ref, never merged), ADR 0015 (the Zoro precedent: an embedded prompt
forked from a skill), and the crewmate Chart's settled frame (Sanji = HITL, and
"not every crewmate needs a rich interface"). Records the input to the
Spec for the Sanji (prototype) crewmate.

## Context

Sanji owns `prototype` — a defog bead resolved by a high-fidelity throwaway that
validates an interface-design question during charting (CONTEXT.md; the
`prototype` skill). Two seams are already settled: the registry mode is **HITL**
(the Chart's Modes note), and `route` sends a prototype to
`Destination{Placement: SideRef, Ref: refs/mugicrew/prototypes/<chart-id>}` with
**no checkout** (`route/route.go:73`, `prototypeRef`). The gap this ADR closes is
everything between those two facts: what the launched session *does*, how
throwaway output reaches the side ref when route hands it no worktree, how thin
Sanji's Go surface is, and how its prompt is built from the existing skill.

The `prototype` skill assumes a working tree and ends (its step 6) by telling the
agent to "commit to a throwaway branch." mugicrew keeps that isolation but names
the destination itself (ADR 0009), and this environment has **no git remote** —
the whole `git` package is local (`Land` = local `update-ref`).

## Decision

**1. Interface & dispatch — as thin as the bare activation contract.** Sanji owns
`prototype` only and is HITL, so the loop never dispatches it. `Sanji.Launch`
builds exactly one `contract.AgentCommand` wrapping `prototypePrompt(beadID,
sideRef, worktreePath)`; `Sanji.Run` returns a loud "prototype is HITL, not a
loop kind" error — mirroring Nami's `Launch` default case and `Reconcile`
no-handler. There is **no decision packet and no consolidation** (Sanji is not
Luffy): its domain interface *is* the Launch command shape. Wiring: a registry
row `{Owner: "sanji", Mode: HITL, Container: false}` for `KindPrototype` and a
`crew()["sanji"]` entry in `main.go`. The side-ref name stays `route.prototypeRef`
— not duplicated as a Sanji method until a TUI ticket needs it (the Chart's
Not-yet-specified already defers prototype TUI affordances).

**2. Prompt — an embedded `sanji/prompts/prototype.md`, mirroring Nami's
grilling/charting scaffold.** `//go:embed`-ed, seeded by `prototypePrompt(...)`,
same section shape as `nami/prompts/grilling.md`: Orient on the Chart → Prototype
→ Land the throwaway on the side ref → Record & close → Capture durable decisions
as docs → Tend the Chart → Guardrail. **What carries over:** the entire
`prototype` skill (the LOGIC-vs-UI branch pick, the throwaway rules,
surface-the-state), invoked live via "Call the Skill tool for `prototype`" — the
Nami precedent, not the Zoro fork. **What is adapted:** the skill's step 6
("capture it when done → commit to a throwaway branch") is **replaced** by the
mugicrew side-ref landing + bead-close contract below. The live-skill dependency
is **interim**: an offshoot Signal will fold the prototype workflow into a
shared `.agents/docs/mugicrew/` asset and remove the install dependency — done
uniformly across crewmates, so Sanji is not forked-early as a special case.

**3. Working tree & side-ref landing — the launched agent cuts its own scratch
worktree and lands to a LOCAL ref.** `SideRef` (no checkout) means the loop and
Sanji check nothing out; the prompt instructs the agent to
`git worktree add <path> chart/<chart-id>`, build the prototype there, then
`git update-ref refs/mugicrew/prototypes/<chart-id> HEAD` and remove the
worktree. The ref is **local** — a ref preserves lineage whether or not a remote
exists, and this matches the local-only `git` package (ADR 0009 §2's "your git
remote" is satisfied by a local ref here). This keeps Sanji pure-prompt: no cwd
plumbing is needed. Syncing `refs/mugicrew/*` to a remote, if one is
ever configured, is out of scope.

**4. Resolve contract — the agent authors every mutation; the human only
authorizes.** A prototype bead resolves when the *design question is answered*,
not merely when code exists. The agent records the verdict (the answer + what it
settled) on the bead (`bd update --notes`), appends a one-line gist to the
Chart's Decisions-so-far, and — when the decision is durable enough that the Spec
or later Builds will lean on it — writes an ADR / CONTEXT edit on `chart/<id>`
(which merges to mainline on defog, no review gate; ADR 0009). The throwaway
*code* stays on the side ref, never merged (ADR 0009). HITL here means
**authorization, not authorship**: the launched agent crafts every `bd`/`git`
command and the human approves it — the same division as Nami's grilling.

## Consequences

- Sanji ships as `sanji/sanji.go` + `sanji/prompts/prototype.md` + one registry
  row + one `crew()` entry — no new Go orchestration, no new git/bd helper.
- Prototype lineage is reachable locally at `refs/mugicrew/prototypes/<chart-id>`
  and never pollutes the mainline tree (ADR 0009 upheld).
- Sanji carries a live dependency on the `prototype` skill until that Signal lands;
  named here so the two can't drift silently.
- Prototype TUI launch/side-ref-view affordances remain deferred (Chart
  Not-yet-specified); Launch returning a bare `AgentCommand` is enough for
  triage/manual open today.
