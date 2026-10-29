# ADR 0357: mugicrew a Survey with no fog finishes its Chart in place through chart finish

Status: Accepted
Date: 2026-10-28
Amends: ADR 0129 (mugicrew-luffy-triages-signals-and-nami-maps-unmapped-charts) §8; ADR 0130 (mugicrew-nami-pushes-back-on-a-destination-by-hand-back) §5 and its Considered option "Nami writes the Spec when there is no fog"; ADR 0140 (mugicrew-charting-is-a-survey-a-hitl-leaf-under-every-chart) §1, §2, its ADR 0129 §7 amendment and its Considered option on closing both beads.

## Context

ADR 0130 §5 handed back a Chart with no fog, so Luffy would fast-lane the new
Signal, and rejected a no-fog path in charting as "a new path for a rare case".
ADR 0035 (mugicrew-signal-triage-fast-lane) §5 refuses the fast lane to a
Signal that needs an ADR, so such a Chart came straight back and was charted a
second time. That happened to "Layered auto-approve setting with a machine-local
override". The case is not rare enough to route around.

## Decision

1. **No fog finishes the Chart in place; it is never a hand-back.** Every
   no-fog Chart takes this path, whether or not it needs an ADR, and any ADR it
   needs is written on `chart/<id>` through the Survey's effort branch. The
   hand-back keeps only its other meanings: a wrong Destination, or a Chart
   spanning more than one Checkpoint.
2. **Stamp and drain, no Spec in session.** The Survey records the decisions
   and stamps `chart:defogged`; the existing Landing → Spec spawn → AFK
   speccing flow writes the Spec. The drained, stamped Chart skips
   promote-or-stamp (`reconcileChart` dispatches the judge only on an unstamped
   Chart).
3. **The human's explicit assent to "no fog" gates it**, enforced by the
   charting prompt as it is for reject and duplicate; nothing records it. That
   assent stands in for the promote-or-stamp judge.
4. **The Survey's `notes` carry its decisions**, as a defog bead's notes carry
   its one. Its description stays the `SurveyBody` pointer at the Chart. Nami
   writes the notes (`bd update --notes`) and appends Decisions-so-far lines
   that link the Survey (`mugicrew chart append`), as a defog session does.
5. **`mugicrew chart finish <chart> --survey <survey>` is the stamp**, a
   guarded verb beside `chart append|drop`, under the same lock. It refuses
   unless `--survey` is the Chart's own open, claimed `kind:survey` child, the
   Chart is open, the Chart has no unclosed child but that Survey (Draft or
   published, any kind), its Not-yet-specified section is empty, the Survey's
   notes are non-empty, and some Decisions-so-far line contains the Survey's
   ID. It does not check that the lines match the notes. Run on a Chart
   already stamped, it succeeds and changes nothing. Nami never writes the
   label by hand.
6. **The stamp is live.** `chart finish` writes `chart:defogged` in the
   session. The settle's Go read (`surveyDone`) sees only a stamp already on
   the Chart, since the publish batch cannot read. A crash after the stamp
   leaves the claimed Survey open, so the Chart stays undrained (ADR 0140 §3).
7. **A stamped Chart's Survey closes in the clean-exit batch.** `surveyDone`
   gains a third arm: parent open and stamped. The invariant becomes "never a
   drained, fog-less, *unstamped* Chart".
8. **Fog beats the stamp.** If, at settle, the Survey has Drafts parented by
   the stamped Chart (stamped, then fog drafted), settle removes the stamp,
   appends `stamp removed at settle: fog seeded` to the Survey's notes, and
   exits as an ordinary fog-seeded Survey: the fog publishes, the Survey
   closes, and promote-or-stamp judges the Chart later.
9. **Speccing reads the closed Survey; it stays a non-defog leaf.** Speccing
   takes the Survey's notes as a decision source beside the closed defog beads
   (a finished-in-place Chart may have none). Promote-or-stamp still skips the
   Survey, and it never counts as fog.
10. **A Survey released on a stamped Chart is relaunched to confirm.** A
    crash after the stamp, then a drop, unclaims the Survey back to the Inbox
    and keeps the stamp, the notes, the Decisions-so-far lines and
    `effort/<survey>`. The drop does not unstamp: the assent was given and the
    decisions are written. A relaunch (or resume) is an ordinary charting
    launch. When the parent Chart is stamped and a Decisions-so-far line names
    the Survey, the charting prompt has Nami read the notes back and ask "still
    no fog?". On assent she re-runs `chart finish` (a no-op that re-checks its
    guards) and the clean exit closes the Survey (7). No unstamp verb exists:
    fog drafted on relaunch unstamps at settle (8), and a wrong Destination is
    still a hand-back. The TUI keeps the word `survey due` for this state
    (ADR 0204 (mugicrew-tui-v2-defog-charts-section-and-chart-status-word) is
    unchanged); its why text adds that the Chart is stamped, awaiting
    confirmation.

### Amendments

- **ADR 0129 §8** ("charting always seeds fog"): charting seeds fog or, with
  no fog, finishes the Chart in place.
- **ADR 0130 §5:** removed by (1). Its Considered option "Nami writes the Spec
  when there is no fog" stands as worded (stamp and drain is not that), but
  its premise of a rare case is reversed.
- **ADR 0140 §1:** the Survey is not a defog bead and promote-or-stamp skips
  it, but speccing reads a closed Survey (9).
- **ADR 0140 §2:** the outcomes gain "finished in place".
- **ADR 0140's ADR 0129 §7 amendment:** the clean-exit batch also closes the
  Survey when its Chart is stamped `chart:defogged` (7); the release to the
  Inbox applies only when neither fog nor a stamp exists.
- **ADR 0140's Considered option** on closing both beads: "never leaves a
  drained Chart with no fog" reads "… with no fog and no stamp".

## Considered options

- **Nami writes the Spec in the session and the human approves it there.**
  Rejected in the Survey: a second Spec-writing path beside AFK speccing.
- **`chart finish` also writes the decisions and the Decisions-so-far lines**
  from flags. Rejected: multi-line decision text through CLI flags, and Nami
  already has both edit paths.
- **No verb: Nami adds the label as promote-or-stamp does, with a Go guard at
  settle.** Rejected: a bad stamp is caught only after the session exits, when
  the human can no longer fix it.
- **Stamp in the publish batch.** Rejected: `surveyDone` would need a recorded
  intent to see it.
- **Stamp beats fog**, so fog publishes under a stamped Chart. Rejected: the
  Landing would wait for that fog and then skip promote-or-stamp over anything
  it graduated. **Refusing the exit** was rejected too: it discards the
  session's work.
- **A drop unstamps the Chart**, restoring it as before `chart finish`.
  Rejected: it discards a given assent and forces a full re-finish.
- **Refuse to relaunch a Survey on a stamped Chart, or close it from the TUI
  or loop with no session.** Rejected: the first needs a new TUI act, and
  both skip re-confirming "no fog" with the human after a crash.
- **A new status word (`finish due`) for a released Survey on a stamped
  Chart.** Rejected: the action is still running the Survey, so `survey due`
  is right and ADR 0204's waterfall stays as it is.
- **Record the human's assent in the Survey's notes for the verb to check.**
  Rejected: the verb cannot tell a real assent from a written one; reject and
  duplicate need none.
