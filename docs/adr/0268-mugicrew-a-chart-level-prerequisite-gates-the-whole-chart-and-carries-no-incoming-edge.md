# ADR 0268: mugicrew a Chart-level prerequisite gates the whole Chart and carries no incoming edge

Status: Accepted
Date: 2026-10-17

When a defog bead discovers a prerequisite, the offshoot Signal blocks the cause's
Chart, and nothing more. A blocked Chart leaves `bd ready` with all its children and
is never re-admitted, so all of charting (remaining defog, drain, Landing, Spec draft,
close) waits until the Signal's carried edge reaches the prerequisite's Checkpoint and
that Checkpoint lands (docs/research/container-blocked-by-outside-signal-course-effects.md §4–5).
We accept that freeze: it needs no new machinery, keeps the gate correct, and a
defog-time prerequisite is rare. The one leak is closed: Nami's Chart land guard
(`nami/nami.go`, `openBlockers(chart.ID, contract.KindTask)`) widens to any open
blocker, as Jinbe's Checkpoint guard already does, so a forced run-now on a held,
already-landed Chart cannot publish its Spec ungated and close.

## Considered Options

- **Gate the Spec instead** (Chart Notes line, speccing session adds the edge):
  charting proceeds, but the order lives as prose until the Spec exists and depends
  on a session acting on it.
- **Carry incoming edges Chart→Spec→Checkpoint** with a land-only Chart hold:
  best behaviour, but widens `Readmit` to Charts and carves an exception into
  ADR 0085 (blocking-edges-carry-forward-on-succession) §1. Rejected; §1 stands.

## References

ADR 0085 (blocking-edges-carry-forward-on-succession). Whether a Checkpoint-level
prerequisite should hold only the land rather than the whole Checkpoint is decided
separately, aligned with this one.
