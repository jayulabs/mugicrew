# ADR 0132: mugicrew An Initiative is born at triage and phased by rolling member Signals

Status: Accepted
Date: 2026-10-11

## Context

ADR 0128 settled what an Initiative is and who owns it; ADR 0129 moved Signal
triage to Luffy. This grilling settled how intake scopes an Initiative and
cuts it into phases (member chains), and how those phases are ordered.

## Decision

1. **Birth: a fifth triage outcome, INITIATIVE.** Luffy proposes it when a
   Signal's coarse scope spans more than one Checkpoint; the human disposes.
   Only Luffy creates an Initiative. Multi-phase scope that Nami finds while
   charting goes back by the ADR 0130 hand-back, and Luffy triages that
   Signal as INITIATIVE. Adoption (ADR 0128 §5) is the only other way in.
2. **The Initiative succeeds the triaged Signal** (`caused-by`, a new `reconcile`
   leg Signal → Initiative). The Signal still has exactly one successor and
   closes at Luffy's exit (ADR 0129 §4).
3. **A phase enters as a member Signal** parented by the Initiative and is
   triaged like any Signal (ACCEPT or FAST LANE). A cut session only files it,
   as a Draft published at exit; its triage is a later, ordinary launch.
4. **Phasing is rolling.** INITIATIVE triage files only phase 1's member Signal
   (or, with the human's yes in session, adopts existing open chains as the
   first phases). Later phases stay coarse lines in the Initiative body until
   cut.
5. **Body:** `Done when` (an observable finish line), `Phases` (one coarse line
   per expected phase, marked with its member Signal once cut), `Notes`. Edited
   one line at a time. Luffy sets the Initiative's priority; each member gets
   its own at its triage.
6. **Ordering is by drain, not edges.** An open Initiative with no open
   children is **drained** (derived, never stored). The next phase is cut only
   then, unless the human calls a cut session earlier. No blocking edges
   between phases.
7. **Cut session.** The TUI Inbox lists Drained Initiatives as a derived row
   (like Unmapped Chart, ADR 0129 §5); the human launches Luffy on one. He
   checks `Done when`: if it holds he proposes done, else he cuts the next
   member Signal. A phase not traceable to `Done when` needs the human's
   explicit yes. How the Initiative then closes is left to its lifecycle.

## Considered options

- **The triaged Signal becomes phase 1's head.** Rejected: its successor
  Chart would cover only phase 1 while the Signal describes the whole effort,
  phase 1 would be born unlike every later phase, and triage would stop being
  coarse.
- **The Initiative spawns a phase's Chart or Spec directly**, skipping the
  member Signal. Rejected: a headless Chart and a second spawn path, to save
  one triage that is the right moment to judge the phase anyway.
- **Cut every phase up front**, ordered by blocking edges. Rejected: what an
  earlier phase teaches reshapes later ones.
- **The phase list, or the human alone, decides done.** Rejected: the list is
  coarse and meant to change; with no written criterion rolling phases never
  end.
- **Triage the member Signal in the session that cuts it.** Rejected: its
  successor would be a Draft born inside a Draft's session.
