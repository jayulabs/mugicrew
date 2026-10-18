# ADR 0020: mugicrew Luffy — the one rich-presentation HITL crewmate whose thin Go launches an approval session

Status: Accepted (§5 amended by ADR 0203)
Date: 2026-09-24

Builds on: ADR 0002 (owner-from-kind routing), ADR 0016 (Checkpoint read-model +
`approvedAtHead` land gate; the Approval carries a `Commit: <sha>` pin and its
close is the sole land trigger), ADR 0017 (the Sanji precedent: thinnest Go
surface, an embedded prompt, HITL = authorization-not-authorship — and the
explicit "Sanji is not Luffy" carve-out), and ADR 0018 (the Usopp precedent:
the Approval is spawned as a bare membership child of the Checkpoint carrying the
inherited pin). Records the input to the Spec for the Luffy (approval)
crewmate.

## Context

Luffy owns `approval` and is the one deliberate human stop in the primary-flow
tail (CONTEXT.md; registry HITL). Two facts were already settled by siblings.
First, the Approval bead arrives **fully formed**: Usopp spawns it as a child of
the Checkpoint carrying `Commit: <sha>` (ADR 0018 §6), and closing it is the
*sole* trigger for the Checkpoint's derived `approved` state (ADR 0016 §4–5).
Second — decisively — once the human closes the Approval, the **loop's Checkpoint
drain-candidacy arm** hands off to Jinbe (ADR 0016 §2, §7); Luffy owns **nothing
after the close**. So Luffy's Go surface has no orchestration to carry and no
post-close step to run.

The gap this ADR closes: the Chart frames Luffy as the crewmate *with* a
"rich domain interface" that "consolidates the decision packet (diff,
clean-review evidence, critique history) and offers the approve action" — the
exact thing ADR 0017 says Sanji is **not**. The question is where that richness
lives, given Luffy's Go is structurally forced to be as thin as Sanji's.

## Decision

**1. Thin Go, rich prompt — the richness is the launched session's presentation,
not a new Go surface.** Luffy's Go is exactly Sanji's shape: `Luffy.Launch`
builds one `contract.AgentCommand` wrapping `approvalPrompt(beadID, ...)`;
`Luffy.Run` returns a loud "approval is HITL, not a loop kind" error (mirroring
Sanji's inverse and Nami's `Launch` default). There is **no Go consolidation
layer, no packet struct, and no post-close step** — after the human closes the
Approval, the loop + Jinbe take over (ADR 0016). The Chart's "rich domain
interface" resolves to the **prompt's packet-presentation contract** (decision
3), not the Go interface. This settles the apparent Sanji/Luffy tension: identical
thin Go, a richer prompt. A Go consolidation layer was rejected — it buys nothing
the in-session agent can't do with its own `bd`/`git` tools and would duplicate
that logic.

**2. `LaunchCommand` is an `AgentCommand`, not a `ChecklistCommand`.** "Luffy's
launched *session* closes the approval bead" — an agent session that assembles the
packet live via tools and runs `bd close` on the human's OK, matching Sanji.
A static `ChecklistCommand ([]string)` cannot assemble a live diff + critique
trail. TUI rendering of the packet stays deferred (Chart Not-yet-specified: "TUI
display/launch affordances for the new HITL kinds").

**3. The decision packet — three ingredients, assembled in-session via `bd`/`git`,
presented to the human.** The prompt instructs the launched agent to gather and
present: (a) the **diff** — three-dot `main...checkpoint/<id>`, the same fixed
point Usopp reviewed; (b) **clean-review evidence** — the closed Review child and
its pinned SHA, proof a clean pass certified *this* commit; (c) **critique
history** — the closed `kind:critique` children (what was raised and fixed, the
rework trail). No new Go helper — the agent uses `bd` + `git` exactly as Usopp and
Sanji do.

**4. Approve = agent-run close on human authorization; the pin is read-only.** On
the human's explicit OK the agent runs `bd close <approval> --reason="approved"`.
It must **not** alter the inherited `Commit: <sha>` body line — Jinbe's
`approvedAtHead` gate compares that pin to `rev-parse checkpoint/<id>` (ADR 0016
§4; the gate counts only an approved close, ADR 0203 §5). HITL here is **authorization, not authorship** (the Sanji division): the
agent crafts the `bd close`, the human approves it. Approval is **read-only** on
every tree — like Usopp's review it gets **no route case** and falls through to
`RepoRoot` (decision 6).

**5. Approve-only is in scope; declining fails safe; the rejection-respawn loop is
a fresh defog bead.** "Closed = approved" is the sole land trigger, so the human
cannot close-to-reject. Both naive rework paths break against the shipped
machinery: *leaving the Approval open and filing a critique* deadlocks
(`SpawnChildIfAbsent(approval)` no-ops on the existing open child, which stays
pinned to a now-stale SHA and can never satisfy Jinbe); *closing the Approval and
filing a critique* **races** Jinbe (between the close and Franky's rework,
`approvedAtHead` is briefly true and the loop could land the rejected code). A
correct rejection loop needs its own machinery — which the Chart's
Not-yet-specified already defers as "Approval obsolete-respawn churn." So Luffy's
Spec covers the **approve action only**. Declining = the human simply does not
close the Approval; it stays open and visible, nothing lands (fails safe, per ADR
0016 §4's fail-safe intent). The active rework-via-rejection loop is beyond this Spec's
Destination (approve-only human stop) and stays in the Chart's Not-yet-specified as
deferred unhappy-path work — an offshoot-Signal candidate (ADR 0006), not a child
defog bead on this Chart.

**6. Prompt — a bespoke embedded `luffy/prompts/approval.md`; NO backing skill.**
`//go:embed`-ed, seeded per bead, same section scaffold as the Nami/Sanji/Usopp
prompts (Orient on the Checkpoint/Chart → Present the decision packet → Offer
approve → Close on authorization → Guardrail). Unlike Sanji (`prototype`) and
Usopp (`code-review`), **there is no "approval" skill to carry** — the
packet-presentation logic (decision 3) lives directly in this prompt, and there is
no "Call the Skill tool" line and no interim skill dependency. Called out
explicitly so no future reader hunts for a missing Luffy skill.

**7. The packet surfaces staleness advisorily; it does not gate.** The Approval's
`Commit: <sha>` pin can drift from live `checkpoint/<id>` HEAD if a late Build
lands after Usopp's review; ADR 0016 §8 makes this self-heal (stale pin ≠ HEAD →
Jinbe won't land, a fresh Review re-spawns). The prompt has the agent compare the
parsed pin against `git.Head(checkpoint/<id>)` and **flag a mismatch advisorily**
in the packet ("HEAD advanced since this was reviewed — the diff shown isn't what
a clean pass certified"), so a human stop sees it. It is advisory only: no re-check
branch, no new Go, no gate — the self-healing machinery stays the real safety net.

## Consequences

- Luffy ships as `luffy/luffy.go` + `luffy/prompts/approval.md` + one registry row
  `{Owner: "luffy", Mode: HITL, Container: false}` for `KindApproval` + one
  `crew()["luffy"]` entry in `main.go`. No new Go orchestration, no new git/bd
  helper, **no route case** (read-only, `RepoRoot` fallthrough) — the same
  footprint as Usopp minus the AFK transition.
- The Chart's "rich domain interface" is delivered as a rich prompt over a thin Go
  surface; the Sanji/Luffy asymmetry is a prompt asymmetry, not a Go one.
- The Approval's pin is never mutated by Luffy, so Jinbe's `approvedAtHead` gate is
  unaffected.
- Luffy is the only tail crewmate with **no** backing skill and **no** interim
  skill dependency.
- Rejection-via-Luffy is out of scope for this Spec; the approval-respawn loop is an
  offshoot-Signal candidate kept in the Chart's Not-yet-specified.

## Rejected / deferred

- **A Go consolidation layer / packet struct** — rejected (decision 1): duplicates
  what the in-session agent does with its own tools; Luffy has no post-close Go to
  justify a richer surface.
- **`ChecklistCommand` + TUI-rendered packet** — deferred (decision 2): TUI
  affordances for the new HITL kinds are Chart Not-yet-specified.
- **Close-to-reject / an in-scope rejection loop** — deferred (decision 5):
  deadlocks or races the shipped land machinery; the "Approval obsolete-respawn
  churn" stays Chart Not-yet-specified as an offshoot-Signal candidate.
- **A HEAD-vs-pin gate before offering approve** — rejected (decision 7):
  reimplements the obsoletion self-heal ADR 0016 §8 already provides; the mismatch
  is surfaced advisorily instead.
