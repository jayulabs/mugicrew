# ADR 0130: mugicrew Nami pushes back on Luffy's Destination by narrowing, splitting or a hand-back

Status: Accepted
Amended by: ADR 0140 (§6: branch deletion keys on a Survey session whose parent Chart closed with no Spec successor)
Date: 2026-10-11
Amends: ADR 0129 §8

## Context

ADR 0129 gives the Destination to Luffy at triage and the map to Nami's
human-started charting session, but leaves open what Nami does when the
Destination is wrong, and how a Chart with nothing to defog goes back
(§8). Scoping is adjudication (Luffy's); the human is present in the
charting session.

## Decision

1. **Narrow in place.** With the human's approval in the session, Nami may
   sharpen or narrow the Destination (`mugicrew chart drop`/`append` on its
   line). She never changes what is built or widens it.
2. **Split as an offshoot Signal.** Work narrowed or split off becomes an
   offshoot Signal, a Draft by the Chart, `discovered-from` the Chart, plus an
   Out-of-scope line. It joins Luffy's intake backlog.
3. **Hand-back for a wrong Destination.** Nami closes the Chart with reason
   `handed back: <why>` and files a Signal `discovered-from` the Chart that
   carries her case; the human launches Luffy on it as ordinary intake. No new
   edge, state or verb.
4. **Reject or duplicate in session.** When the human explicitly agrees, Nami
   closes the Chart (not worth doing) or supersedes it with the original
   (duplicate) herself, with no trip to Luffy.
5. **No fog is a hand-back.** A Chart Nami could spec outright is handed back
   (3) so Luffy fast-lanes the new Signal; charting still always seeds fog when
   it keeps the Chart.
6. **The branch goes with the Chart.** When the charting session exits with
   its Chart closed and no successor Spec, mugicrew deletes `chart/<id>`: an
   Unmapped Chart's branch holds no artifacts.

## Considered options

- **Reopen the Signal for re-triage.** Rejected: `reconcile` finds the old
  Chart through `caused-by`, and it undoes ADR 0129 §4 (no Signal open across
  two crewmates).
- **A dedicated hand-back verb or Chart state** that makes the Chart a Luffy
  triage target. Rejected: a fresh Signal already routes to Luffy.
- **Nami writes the Spec when there is no fog.** Rejected: a new path in the
  charting session for a rare case; the hand-back already covers it.
- **Rewrite the Destination freely because the human is present.** Rejected:
  changing what is built is a scope call, which the triage/charting split
  keeps with Luffy.
