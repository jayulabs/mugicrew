# ADR 0046: mugicrew pivot triage session shape

Status: Accepted
Date: 2026-10-04

Amends: ADR 0005 §3 (how triage outcomes execute), ADR 0045 §3 (how "one Inbox
item per Checkpoint" is realised).

## Context

ADR 0045 makes the Checkpoint the triage unit, with one Inbox item and one HITL
session per Checkpoint with open pivots. But the Inbox lists every open HITL
bead and keys sessions by bead id (`tui/tui.go` `inboxList`), so N pivots would
give N rows and N sessions. Confirm and escalate are also many-write sweeps
(ADR 0043 §8, ADR 0045 §5). The loop may be off, so the session itself has to
make them happen. Decided in grilling.

## Decision

1. **Lead pivot.** `inboxList` shows only the oldest open pivot of each
   Checkpoint. Its detail view lists the other open pivots on that Checkpoint
   ("+N other open pivots: …"). When the lead closes by amend or dismiss, the
   next oldest becomes the lead and gets its own row and session.
2. **Launch inputs.** `Zoro.Launch` (KindPivot) passes the lead pivot, Checkpoint
   and Spec ids into `prompts/pivot.md`. Go resolves the Checkpoint (reverse
   `blocks`) and the Spec (`related`). The agent reads the rest: every open
   pivot, each raising bead, the Spec's Amendments, and
   `git diff <base>..checkpoint/<id>`.
3. **Session flow.** For each pivot, Zoro recommends an outcome and gives a reason.
   The human decides them together. Before executing, Zoro re-reads the open
   pivots (ADR 0045 §4).
4. **Dismiss and amend are agent-run.** The agent runs the `bd` writes from the
   prompt, checking each before writing (ADR 0043 §7, ADR 0044).
5. **Confirm and escalate end in a Go sweep verb.** The session calls one
   idempotent verb (ADR 0003) that does the supersede, closes the members and
   sibling pivots, and closes the pivot (ADR 0043 §8, ADR 0045 §5).
   - **Confirm:** first the agent writes the corrected Spec with the human. Its
     body is the old body with the Amendments folded in, the fixed FR, and the
     unfinished FRs from sibling pivots and raising beads. The new Spec is
     `caused-by` the old Spec's predecessor, so a charted Spec keeps its Chart's
     landed base. A fast-laned Spec gets a fresh `Spec:` anchor on main, so its
     new Checkpoint cuts from current main.
   - **Escalate:** the verb creates the fresh Chart. It is `caused-by` the
     original Signal (which stays closed) and `supersedes` the old Chart when
     there is one. Its body is seeded with the pivot evidence. Nami charts it as
     usual. The seed is a filled wayfinder map (the evidence in Notes, one
     Not-yet-specified fog bullet), not the un-charted placeholder. So the Chart
     skips the charting-session gate and goes straight to Nami's
     promote-or-stamp, which turns that fog into defog beads. This is
     deliberate: the triage session already gathered the evidence with the
     human, and the Signal is closed, so there is nothing for a separate
     charting session to read first.
6. **Fast-laned escalation needs nothing more.** The only difference is that
   there is no old Chart to supersede. The Signal→successor lookups
   (`successorOf`) run only on open Signals, so a closed Signal with a second
   Chart never reaches them.
7. **Placement.** The session runs at RepoRoot. The prompt cuts its own worktree
   only to write an ADR (ADR 0044), as `chart-conflict.md` does.

## Consequences

- No new bead kind and no TUI session model change; one Inbox filter.
- The partial-failure-prone sweep lives in tested Go, not in prompt prose.
- A fast-laned correction may cut from a newer main than the original. Nothing
  on the old Checkpoint branch survives a confirm, so nothing is lost.

## Alternatives considered

- **Every pivot shown, launches attach to one session.** Rejected: it needs a
  session keyed to a Checkpoint instead of a bead.
- **A per-Checkpoint triage bead.** Rejected: a new kind for a grouping the
  graph already has.
- **Prompt-only context.** Rejected: an agent can easily get the reverse
  `blocks` lookup wrong.
- **Agent runs the sweep's `bd` writes.** Rejected: too many writes for prose
  to make idempotent.
- **Session records decisions; AFK reconcile sweeps.** Rejected: the loop may be
  off.
- **`checkpointBase` walks `supersedes` to the original base.** Rejected: new
  code for a base nothing depends on after a confirm.
- **Escalate as a fresh offshoot Signal.** Rejected: CONTEXT keeps the original
  Signal as the Chart's cause.
