# ADR 0164: v2 `enter` is mode-aware on every tab: launch or attach HITL, run AFK

Status: Accepted; §9 amended by ADR 0238 (the ON LAUNCH preview line is removed); §7 amended by ADR 0181 (the Initiatives tab's `enter` opens the focus); §6 hint order superseded by ADR 0229 (…hints-bar-and-key-reference…) (membership kept)
Date: 2026-10-13

Amends ADR 0144 §2 and §7, ADR 0146 §2 and ADR 0156 §5 and §7. Chart
mc-lu2x.12 (from Signal mc-nay8) found that Graph had two keys for one job:
`enter` launched HITL beads and `t` ran AFK ones. Defog already had a
mode-aware `enter` (ADR 0156 §5). This ADR makes one rule hold on every v2 tab
that lists beads, Execute (phase 4) included. The row table it rests on is
docs/research/graph-enter-rows-under-the-mode-aware-rule.md. The rule was
prototyped and accepted (mc-lu2x.12.3) and settled in grilling mc-lu2x.12.4.

## Decision

1. **The rule.** On a bead row, `enter` launches or attaches a HITL bead
   (Initiative included, ADR 0146 §1) and runs an AFK bead now. On AFK rows
   it does every verb `t` used to do: `run` an AFK leaf, `spec` a drained
   Chart, `land` a landable Checkpoint, `retry` or `retry hold` a
   retry-paused bead. Loop-driven containers (Chart, Checkpoint) count as AFK.
   A bead with a session (alive or exited, open or closed) attaches or
   resumes it, whatever its kind. A session wins `enter`, as it wins `x`.
2. **`t` is dropped** from Graph and is not kept as an alias (ADR 0144 §1). In
   ADR 0144 §2, "v1's `b t p …` keep their v1 meanings" no longer includes
   `t`. `b` stays HITL-only. On an AFK row it refuses with "b is HITL-only —
   enter runs it".
3. **Refusals come before dialogs.** Before any y/n, `enter` refuses with no
   I/O, naming the reason on the status line, on these rows:
   - a write, launch or close in flight
   - a closed bead with no session
   - an AFK bead with a live effort ("x aborts it") or a dead one ("x
     recovers it")
   - an in-progress HITL bead with no session ("x recovers it"). It never
     starts a second agent on a stranded claim. ADR 0170 §9 narrows this to
     the HITL kinds other than Surgery and Task, which resume on `enter`.
   - a bead under a refusing pause, its own or inherited (an AFK bead only;
     see 4 for HITL)
   - a Chart that is not drained (n/m children closed)
   - a Checkpoint whose land is held or that is not approved

   These gates live in the shared run-now path, so Defog's AFK rows get them
   too. The press-time eligibility check stays authoritative.
4. **Blocked and paused beads, by tab.**
   - **Working tabs (Graph, Idea)** ask y/n, naming the cause:
     - "Run blocked bead now?" on a blocked AFK bead
     - "Launch blocked bead?" on a blocked HITL bead
     - "Launch paused bead?" on a paused HITL bead, own pause or "manual via
       <anc>". A paused Initiative head asks too.
     - A HITL bead that is both blocked and paused gets one dialog naming
       both.
   - **Queue tabs (Defog, Execute)** refuse a blocked or paused HITL leaf and
     name the blocker or pause (ADR 0156 §5 stands).
5. **Hints name the verb.** The hints bar shows `enter <verb>`. The verb is
   `launch`, `attach` or `resume` for HITL and session rows, and `run`,
   `spec`, `land`, `retry` or `retry hold` for AFK rows. `enter` is left
   out of the bar where it would only refuse. Graph and Defog share the
   labelling. In `?` keys, enter reads
   `enter  launch/attach (HITL) · run/spec/land/retry (AFK)`.
6. **Amendments.**
   - ADR 0144 §7's Graph row becomes `enter <verb> · b bg · space fold ·
     d detail · / search · s status · f kind · p pause · x drop/abort/recover
     · z reject · n new · u surgery · e edit · E raw · +/- prio · a/A
     adopt/release · h closed · L logs`.
   - ADR 0146 §2: the rows on which `enter` does nothing are the refusals in
     3. "A container that is neither HITL nor an Initiative" is removed, since
     a Chart or Checkpoint now runs.
   - ADR 0156 §5: on Defog, a drained Chart's `enter` runs `spec`, and a
     not-drained Chart refuses with its counts. Initiative and summary rows
     still do nothing.
   - ADR 0156 §7: HITL and AFK rows show the per-row verb from 5. A drained
     Chart row leads with `enter spec`.
   - ADR 0149 §2 stands. Idea's `enter triage` goes through Graph's launch, so
     it inherits 4's working-tab y/n.
7. **Exempt.** History's `enter` reveals a commit's bead (ADR 0144 §3). Its
   rows are commits, not beads. Group headers and Idea's '+ New Signal' row
   have no bead and also sit outside the rule.
8. **Execute inherits the rule** as a queue tab. Its own row mapping
   (Checkpoint, Spec, Build and their states) is left for phase 4's Chart.
9. **Graph's detail pane** keeps its ON LAUNCH preview, reworded to name what
   `enter` does on the row (ADR 0146 §3). It does not add Defog's explicit
   `enter` field.

## Considered Options

- **Keep `t` for AFK and `enter` for HITL.** Rejected. You have to know a
  bead's mode before you know which key acts on it, and Defog already uses
  one `enter`.
- **Keep `t` as an alias.** Rejected. One key, one meaning (ADR 0144 §1).
- **Queue tabs ask y/n like Graph, or Graph refuses like the queues.**
  Rejected. A queue offers work that is ready to pick up, so a blocked or
  paused bead there is a mistake. Graph and Idea are where a human
  deliberately works on any bead.
- **An in-progress HITL bead with no session launches afresh.** Rejected. Its
  claim and worktree may still be live, and `x` already recovers it.
- **Defog's Chart rows stay inert.** Rejected. A drained Chart is exactly
  when to spec it, and an inert row would make Defog the one tab where
  `enter` on a loop-driven container means something else.
