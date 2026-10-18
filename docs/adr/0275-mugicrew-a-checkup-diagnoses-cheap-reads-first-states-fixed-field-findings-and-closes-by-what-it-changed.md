# A Checkup diagnoses cheap reads first, states fixed-field findings, and closes by what it changed

Status: Accepted
Date: 2026-10-17

Chart mc-qcor ("A HITL Chopper session repairs a broken bead on a human's
command"), grilling bead mc-qcor.8. This ADR sets the diagnosis method that the
Checkup session prompt (ADR 0272 (mugicrew-a-chopper-checkup-is-a-hitl-kind…))
carries. The write surface and one yes per finding come from ADR 0273
(mugicrew-a-checkup-writes-only-through-bd…). The read paths come from
docs/research/per-bead-transcript-and-event-log-read-paths.md.

## Decision

1. **Read order, cheap to costly.** Chopper reads in this order:
   1. the `--problem` symptom (if there is none, ask the human for it);
   2. `bd show <target>`;
   3. the target's `paused:*` causes and their lifters;
   4. its parent, children and edges;
   5. `recover`'s inventory row for the target;
   6. the target's event-log lines;
   7. its effort transcripts (`mugicrew transcript --no-follow --no-pager`,
      `--last` first).

   Chopper stops reading as soon as the evidence supports a cause. It reads
   transcripts only when the graph and the events don't explain the symptom,
   because they are uncapped and cost the most to read.
2. **Event-log read is a grep recipe.** The prompt carries
   `grep '"bead":"<id>"'` over the event log and its `.1`, capped at about the
   last 200 lines. No new print verb is added until something outside Checkup
   needs one.
3. **A finding has fixed fields.**
   - **Symptom**: what the human sees.
   - **Evidence**: quoted lines, each with its source (a bd field, an event
     line or a transcript turn).
   - **Cause.**
   - **Fault class**: graph-shape, content, state, or not a bead fault.
   - **Plan**: the ordered command lines, each with a one-line reason
     (ADR 0273).
   - **After**: what the human should see once the plan has run, for example
     "re-dispatches on the next tick" or "stays `paused:manual`".

   A finding carries no confidence score. When the evidence doesn't single out
   one cause, Chopper says so and names at most two candidate causes, each with
   the read that would settle it. It offers no plan until one cause is settled.
4. **Several findings: list first, then walk.** Chopper first gives one numbered
   list of findings. It then takes each finding in turn, with its own plan and
   its own yes. Findings that depend on another come last, for example an edge
   fix before lifting the pause that edge caused.
5. **Verify every plan.** After a plan runs, Chopper re-reads the target
   (`bd show`) and checks it against the After field. It confirms dispatch from
   the event log only if the human chooses to wait a tick. If the result
   doesn't match, that becomes a new finding.
6. **The close names what the session changed.**
   - **`repaired: …`**: any repair plan ran. The reason names any offshoot
     Signal filed, any command printed for the human, and any finding the human
     declined.
   - **`not a bead fault: …`**: no plan ran, because no bd or crew-verb change
     inside the ADR 0273 surface would make the symptom go away and stay gone.
     That covers three cases. A code defect would re-break the bead (for
     example the stale effort branch in
     docs/research/not-built-retry-reuses-stale-effort-branch.md); it gets an
     offshoot Signal. The fault needs only git or the environment; Chopper
     prints the command. Nothing is wrong at all.
   - **Every plan declined**: the Checkup stays open, and the human drops it
     with `x` (closed `dropped`, fence lifted).

## Considered Options

- **Read everything up front.** Rejected. Transcripts are uncapped, and most
  faults show in the graph or the event log.
- **Read the bead, then let the human steer.** Rejected. It hands fact-finding
  to the human.
- **Close `not a bead fault` whenever a code defect sits behind the symptom.**
  Rejected. The close would hide a bd repair that did run.
- **A third close reason, `declined`.** Rejected. Drop already covers a Checkup
  that changed nothing at the human's word.
