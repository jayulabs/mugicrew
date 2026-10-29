# ADR 0371: mugicrew an offshoot filer opts out of its cause's Initiative with --outside and a reason

Status: Accepted
Date: 2026-10-28
Amends: ADR 0267 (an-offshoot-signal-is-filed-through-signal-offshoot…), the verb's flags and stdout; ADR 0348 (a-filing-with-no-plain-member-to-climb…) §6, the `Initiative:` line.

## Context

The Chart "An offshoot whose cause is in an Initiative is born as its member"
settled two things. An offshoot, ordered or not, is born in its cause's
Initiative by default. A filer that judges the work completely unrelated opts
out at filing with an explicit reason, and member triage's release (ADR 0255
(member-triage-places-the-signal…) §4) stays the safety net. The grilling
"Surface of the filer's opt-out from its cause's Initiative" settled the surface.

## Decision

1. **The flag is `--outside "<why>"`** on `mugicrew signal offshoot`. The reason
   is required: an empty value is a usage error. It is independent of
   `--order`. A follow-up or prerequisite edge is still set against the
   container, and only the Initiative parent is skipped.
2. **The verb has a `--dry-run`.** It runs the full climb and prints the
   `Order:` and `Initiative:` lines, plus the climbed Initiative's
   `## Done when`. It writes nothing. The Offshoot form asks for it only when the
   filer suspects the work has nothing to do with the cause's Initiative. A dry
   run before every filing is not required, because born-in-place is the
   default.
3. **The `Initiative:` line goes on stdout and into the body, under `Order:`**,
   in every case:
   - born in place: `Initiative: <title>`;
   - opted out: `Initiative: outside (filer: <reason>)`;
   - closed Initiative: `Initiative: outside (<title> is closed)`;
   - no Initiative: `Initiative: outside (<container> has no Initiative)`.

   `Initiative: outside (unordered)` goes away. A parent climb can't reach two
   Initiatives, so that case has no line. When `--outside` is given but the climb
   finds no open Initiative anyway, the climb's reason wins. Triage can then
   quote the body line to say why a loose offshoot is outside. This replaces the
   stale "so it starts outside it" wording.
4. **`--outside` is allowed under Require Initiatives.** Nothing refuses at
   birth. Triage limits a loose Signal to adopt, INITIATIVE, REJECT, DUPLICATE or
   REROUTE (ADR 0221 (require-initiatives-gates-surgery…) §5).
5. **The charting hand-back joins** the Initiative by default. It carries the
   Chart's own work, and triage still sees it as a member.
6. **Only signal-filing.md's Offshoot form changes among the crew docs.** Its
   lead ("not a member of any container") becomes "born in its cause's Initiative
   unless you opt out". It gains `--outside`, the rule (opt out only when the
   work does nothing toward the Initiative's Done when), and the dry-run step.
   chart-body.md and defog-session.md only point at the form and stay as they
   are.

## Considered Options

- **A Go-resolved template argument at launch** (as in ADR 0348 §4). Rejected:
  it means a new argument in about 11 prompts across 6 crewmates, and a session
  may have several candidate causes.
- **Printing the Initiative only after filing.** Rejected: opting out would then
  mean deleting a Draft and filing again.
- **Showing nothing.** Rejected: the filer couldn't judge "unrelated" against the
  Initiative's Done when.
- **Refusing `--outside` under Require Initiatives.** Rejected: enforcement
  stays at triage, and the human decides there.
