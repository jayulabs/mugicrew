# ADR 0180: v2 Ideate's Actionable section lists drained and cut-session Initiatives as one combined row

Status: Accepted

Date: 2026-10-14

Chart mc-c3es (Queue-tab parity). Settles grilling mc-c3es.5. It fits the
Initiatives that ADR 0177 lists and ADR 0178 launches into ADR 0179's
Actionable section, badge and folds. It amends ADR 0178 §5 (a drained header
starts unfolded and shows `(0)`) and ADR 0179 §4 (a drained header's trailer).

## Decision

1. **Bands.** A listed Initiative goes into Ideate's Actionable section by its
   cut session's state first, then by whether it is drained:
   - cut session alive and waiting (your turn or needs approval), drained or
     not: **waiting band**;
   - cut session alive and the agent is working, drained or not: not in
     Actionable;
   - drained, no cut session: **frontier band**;
   - drained, cut session exited (resumable): **frontier band**, as an idle
     resumable Surgery or Task is (ADR 0170 §9), because `enter` resumes it;
   - not drained, cut session exited: not in Actionable (tree only).
2. **One combined row.** Each such Initiative has one Actionable row, drawn as
   a leaf: `stateGlyph Initiative P# <title> <id>`. A drained Initiative's row
   adds a soft `· all members closed` trailer, and the cut session's rowSuffix
   sits at the right edge. `enter` does what it does on the tree header
   (ADR 0178 §1: cut, attach or resume through Graph's launch path, with the
   y/n when the Initiative is paused). The row's hints are the drained hint
   row's (`enter cut|attach · d detail · n new`). Its detail pane shows the ON
   LAUNCH preview (ADR 0178 §6).
3. **Folds** (amends ADR 0178 §5). A drained header starts folded, like every
   container (ADR 0179 §3). When it is folded, its trailer shows `· drained`
   instead of the zero `open` part, e.g. `<title> · drained · 1 live`, so the
   tree header still shows why `enter` launches from it.
4. **Twins.** The Actionable row and its Initiative's tree header are twins:
   each shades the other, and the header's Parent chain is shaded too
   (ADR 0179 §5).
5. **Badge** (ADR 0177 §2 is unchanged under ADR 0179 §2). A waiting cut
   session adds 1 to amber `●N`. A drained Initiative with no waiting session
   counts only toward the light-blue Actionable count, which equals the
   Actionable header's count and also includes waiting rows.

## Considered Options

- **Copy the tree header, or the drained hint row, into Actionable.** Rejected
  for one combined row. A header row alone loses "all members closed", and the
  hint row alone loses the title and the rowSuffix.
- **Keep drained headers unfolded with `(0)`** (ADR 0178 §5). Rejected. ADR
  0179 §3 folds every container, and the Actionable row now carries the
  drained cue.
