# ADR 0255: mugicrew member triage places the Signal in the Phases and parks a later-phase member by close

Status: Accepted
Date: 2026-10-17
Amends: ADR 0132 (initiative-born-at-triage-phased-by-rolling-member-signals) §4/§6.

## Context

Luffy triaged a member Signal like a loose one, never reading the Initiative's
Done when, Phases or open members. A member could be charted outside the
phasing or become a second same-context member of a phase (ADR 0252
(a-cross-repo-signal-splits-at-triage…) §3), and an open member in a later
phase held the current phase's drain (ADR 0132 §6), so the next phase was
never cut. An adopted Chart, Spec or Checkpoint skips triage and was never
written into Phases.

## Decision

1. **Triage places the Signal first.** A member triage prompt gets a Go-built
   member section (Done when, Phases, open siblings with title, context and
   kind). Before any outcome Luffy proposes one placement: join an existing
   phase, open a new live line, or open a new later line. A live new phase and
   a phase not traceable to Done when need the human's explicit yes (as in
   ADR 0132 §7). Phases changes are one whole-body `bd update --description`.
2. **Sibling rule.** A same-context sibling in the target phase is never a
   co-member. If it is still a Signal or Chart, merge: `bd dep remove` any
   existing edge, `bd supersede <signal> --with <sibling>`, and carry the gist
   (`bd note`, or `mugicrew chart append … Notes`). If it is at Spec or
   Checkpoint, sequence the new Signal into another phase (park). A sibling in
   another context may co-member the phase, with `blocks` edges where order
   matters.
3. **Park by close.** A Signal placed in a later phase is closed with
   `later: <title> (phase line on <initiative>)`, keeping its parent, and a
   `later: <title> (from <signal>)` line is written under Phases. This amends
   ADR 0132 §4 (a later phase may hold a parked `later:` line before it is cut)
   and §6 (a parked member is closed, so it never holds the drain; if it was
   the last unclosed member the Initiative is drained at once and Luffy says
   so). Parking works under Require Initiatives.
4. **Release or move.** If the Signal doesn't belong, Luffy offers
   `mugicrew initiative release` (Require Initiatives off), continuing triage
   as a loose Signal in the same session. Under Require Initiatives he offers
   `mugicrew initiative adopt <signal> <other>` or REJECT, never release
   (ADR 0224 (require-initiatives-and-enforce-external-id…)).
5. **Cut session.** The cut prompt gains a Go section: write adopted
   Chart, Spec and Checkpoint heads that Phases doesn't name into an
   under-way phase (never a later one), and turn each `later:` line of the
   phase being cut into a fresh member Signal citing the closed one. The
   closed Signal is never reopened.

No Go change to drain, StateOf, reconcile, adopt or release.

## Considered Options

- **Release and park.** Rejected: the Signal leaves the Initiative's focus view
  and cut-session member listing, and Require Initiatives refuses release.
- **A drain-skipping label.** Rejected: StateOf would need to learn it, and
  every member view would need to filter on it, for what a close already does.
- **Supersede to the Initiative.** Rejected: bd keeps one edge per ordered
  pair and the parent-child edge already holds it; removing that edge first
  drops membership. See docs/research/superseding-a-member-signal-with-its-own-initiative.md.
