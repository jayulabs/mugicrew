# ADR 0126: mugicrew Chopper — a kindless, deterministic crewmate that recovers a bead stranded by a dead effort, only on a human's command

Status: Accepted
Date: 2026-10-11

Amends: ADR 0006 (Chopper as an in-graph crewmate was out of scope), ADR 0092
(the Chopper name was earmarked for the external monitor), the glossary's
Signal note "Chopper the crewmate is not [in scope]".

## Context

A dead effort holder (crash, SIGKILL, loop force stop, unconfirmed TUI death,
reboot) leaves a stranded claim, a stale tree, stranded Drafts and a
`paused:abandoned` label, and nothing repairs them (ADR 0050 §6, ADR 0082 §4,
ADR 0083 §9). The repair order is fixed: confirm dead, abort the rebase, drop
the tree, delete the Drafts, unclaim, lift `paused:*` last. Every primitive in
it is blind, with no per-effort token
(docs/research/dead-effort-repair-primitives-and-race-windows.md). Liveness
decides some cases and is ambiguous for others: the cut-to-spawn window, an
in-process host between claudes, and a mid-tick kill
(docs/research/dead-effort-holder-liveness-for-claims-trees-and-drafts.md §10).
Decided on grilling bead mc-yr7o.3 (Chart mc-yr7o).

## Decision

1. **Chopper is a crewmate**, the crew's doctor: he keeps the bead graph
   healthy and valid. A crewmate is defined by its role. Owning a kind is a
   characteristic, not a requirement (glossary, Crewmates). Chopper owns no
   kind. The loop stays activation substrate, not a crewmate.
2. **Deterministic code, no claude session.** The repair order is fixed, and
   its two judgment calls belong to the human: whether the holder is really
   dead, and whether uncommitted work is worth keeping.
3. **Surface.** `mugicrew recover <bead>` repairs one bead.
   `mugicrew recover` with no argument lists the stranded beads and files
   nothing. A TUI key on a stranded row runs the same code. Nothing enters the
   graph to describe the repair; the stranded bead is the record.
4. **Always human-triggered.** Nothing repairs automatically: no loop-tick step,
   no sweep on TUI or `loop start`. Typing the CLI verb is the confirm, as for
   `abort` (ADR 0083). The TUI confirms with `y`. This is human-issued action
   on the liveness view, which ADR 0083 §6 already allows, so ADR 0031 and ADR
   0050 §6 stand unamended.
5. **Refuse unless dead.** Chopper refuses when the holder reads alive. A
   live-holder refusal names `mugicrew abort <bead>`.
6. **Ambiguous resolves to dead** when the loop lock is free and no TUI process
   is alive, because then no host can be mid-step. Otherwise `--force`
   overrides an ambiguous reading. `--force` is refused while the loop is on.
7. **Any loop state, behind a pause fence.** Chopper pauses the bead before its
   first repair step and lifts the pause last, after the unclaim, so a running
   loop skips the bead throughout.
8. **`abort` and `recover` stay separate.** Abort stops a live holder, and
   recover repairs a dead one. Each verb has one irreversible job.

Out of this ADR: what repair does for each leftover, the TUI rows, keys and
hints, and events (mc-yr7o.4).

## Considered Options

- **Chopper owns `kind:recovery`.** Detection would file a recovery bead for
  the loop or a human to dispatch. Rejected: the recovery effort's own claim
  can strand, and filing needs automation.
- **A claude session.** Rejected: no step needs judgment the human doesn't
  already give.
- **Automatic repair for decidable-dead cases** (argv-dead AFK, reboot, HITL
  session gone). Rejected: with no per-effort token, the repair still races a
  draining loop, and the plan of record is no self-heal. It would also amend
  ADR 0031 and ADR 0050 §6.
- **Refuse unless the loop is off.** Rejected: the pause fence suffices when the
  liveness reading is trustworthy. Only `--force` needs the loop off.
- **Run in-process kinds under a `reconcile <id>` child** so argv names them.
  Rejected here as a dispatch change. The lock-free, TUI-free rule closes
  the same ambiguity for recovery.
- **`recover` aborts a live holder first.** Rejected: it fuses two irreversible
  jobs into one verb.
- **Keep the Chopper name for the external monitor.** Rejected: an external
  monitor may still file Signals (ADR 0006), but the name goes to the crewmate
  whose role is the graph's health. Chopper detects only by listing stranded
  beads, and files nothing.
