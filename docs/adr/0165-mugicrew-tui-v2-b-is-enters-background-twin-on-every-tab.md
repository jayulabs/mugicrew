# ADR 0165: v2 `b` is `enter`'s background twin on every tab that lists beads

Status: Accepted; §5 hint order superseded by ADR 0229 (…hints-bar-and-key-reference…) (membership kept)
Date: 2026-10-14

Builds on ADR 0164 (mode-aware `enter`). Amends ADR 0156 §6 and §7 and ADR
0149 §1. Chart mc-lu2x.14 (from Signal mc-lu2x.13). Before this ADR only Graph
bound `b`. The rule was prototyped and accepted in mc-lu2x.14.2 and settled in
grilling mc-lu2x.14.3.

## Decision

1. **The rule.** On every v2 tab that lists beads, `b` launches or resumes a
   HITL bead without attaching, wherever `enter` would launch or attach it.
   When `enter` would attach a live session, `b` refuses with "already running
   — enter attaches". `b` stays HITL-only: on an AFK row or a container with
   no session it refuses with "b is HITL-only — enter runs it". A bead with a
   session behaves as on Graph, whatever its kind (ADR 0164 §1): `b` resumes an
   exited session in the background. An Initiative head launches Luffy's cut
   session in the background. After a background launch, the status line
   reads "started X — enter attaches".
2. **Blocked and paused beads.** `b` follows its tab's `enter` rule and has no
   rule of its own (ADR 0164 §4):
   - Queue tabs (Defog, Execute) refuse and name the blocker or the pause.
   - Working tabs (Graph, Idea) ask the same y/n as `enter`.
3. **Per tab.**
   - **Defog** binds `b` on HITL leaves, and on AFK rows that hold a session.
   - **Idea**: `b` on any Signal row, Backlog Signals included, starts or
     resumes triage in the background. On '+ New Signal' and on the Backlog
     header, `b` answers "nothing to launch here".
   - **Graph** is unchanged.
   - **Execute** (phase 4) inherits the rule as a queue tab.
   - Tabs that list no beads (History, Logs, Stats, Settings) are outside the
     rule. Stats keeps `b` as group-by (ADR 0144 §5).
4. **Shown only where it acts, answers everywhere.**
   - `b bg` appears in the hints bar right after `enter` and before
     `d detail`, and only on rows where `b` would act: launching, resuming or
     asking y/n.
   - On any other row `b` is hidden from the bar, but it still answers with
     `enter`'s refusal text, or with the texts given in 1 and 3.
   - Every tab that binds `b` lists it in `?` keys as
     `b  bg launch/resume (HITL)`.
   - The detail pane does not name `b`.
5. **Amendments.**
   - ADR 0156 §6: `b` is bound on Defog as described in 3.
   - ADR 0156 §7, with ADR 0164's verbs:

     | row | hints, in order |
     |---|---|
     | HITL leaf | enter \<verb> · b bg · d detail · x drop/abort/recover · +/- prio |
     | AFK row | enter \<verb> · b bg · d detail · x drop/abort/recover · +/- prio |

     On a HITL leaf, `b bg` is left out when the leaf is blocked or paused, or
     when its session is live. On an AFK row it shows only when the row holds
     an exited session. The Chart, Initiative, '▸ N closed' and closed-child
     rows are unchanged.
   - ADR 0149 §1: the Signal row becomes `enter triage · b bg · d detail ·
     z reject · n new · e edit · E raw · +/- prio · a/A adopt/release`. The
     '+ New Signal' and Backlog header bars are unchanged. `b` is listed in
     `?` only.
   - ADR 0144 §7 stays as ADR 0164 left it, because Graph already has `b bg`
     after `enter`.

## Considered Options

- **Leave `b` unbound and unlisted on Idea's '+ New Signal' and Backlog
  header.** Rejected after the prototype. Every key the tab binds should
  answer and be listed in `?`.
- **A blocked/paused rule for `b` that differs from `enter`'s.** Rejected.
  `b` is `enter`'s twin, so the two keys should not disagree on a row.
- **`b` on AFK rows with no session.** Rejected. Run-now already runs in the
  background.
