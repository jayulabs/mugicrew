# ADR 0335: A Task body may carry a lead, fact lines and ## Diagnostics; the guide walks only ## Steps

Status: Accepted
Date: 2026-10-21

Chart mc-6848i (Hold Tasks carry a Steps body Brook can check), grilling
mc-6848i.3. Amends ADR 0116 (task-steps-checks-and-record) §1.

ADR 0116 §1 says a Task's steps are a numbered `## Steps` list, and crew doc
task-body.md reads that as "the body is one `## Steps` list". The Go-filed hold
Tasks also need text that is not a step: their idempotency and infra-skip
matches run on whole key lines (`Closed MR: !N`, `Merged MR: !N`,
`Pipeline: N`, `Foreign: <sha>`, and the `infraTaskLead` sentence), and their
pipeline bodies carry job trace tails.

## Decision

1. **A Task body is, in order:** an optional one-paragraph lead, optional fact
   lines (`Key: value`, one per line), the numbered `## Steps` list, and an
   optional trailing `## Diagnostics` section.
2. **The guide walks only `## Steps`.** The lead and fact lines are context
   it may quote to the human; `## Diagnostics` is reference it reads on
   demand. Neither is a step, is checked, or is recorded.
3. **Fact lines are whole lines** placed before `## Steps`, so a body-text
   match on one keeps matching after a reformat.
4. ADR 0116 §1 otherwise stands: every step is a title, `Do:` and `Check:`
   (or `Ask:`). Crew doc task-body.md gains a short "may also carry"
   paragraph saying the above.

## Considered Options

- **Ride the Spec, no ADR:** rejected. task-body.md is a hand-kept summary
  of ADRs, and a rule sourced only in a Spec would leave it summarising
  something that is not an ADR.
- **Fold the key lines and traces into steps:** rejected. Keys must stay
  matchable as whole lines, and a trace tail is not a step.
