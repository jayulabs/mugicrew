# ADR 0224: Require Initiatives and Enforce external ID are shared opt-in settings gated at triage

Status: Accepted
Date: 2026-10-16

From Spec mc-8ymw.7. A Workspace running beside an external tracker wants all
work inside an Initiative, each naming its tracker issue. It amends ADR 0128
(mugicrew-initiative-container-membership…): an Initiative stays opt-in unless
the Workspace sets Require Initiatives. Grandfathering and the beside-path
rules are in ADR 0220 (mugicrew-switching-require-initiatives-on…) and ADR 0221
(mugicrew-require-initiatives-gates-surgery…), not restated here.

## Decision

1. **One table, shared only.** `[initiatives]` with `required` and
   `external_ref` in the checked-in `.mugicrew/config.toml`. The reader never
   reads the machine-local layer and re-reads on every call.
2. **Unusable reads as off.** A decode error, a mistyped table, or
   `external_ref = true` without `required = true` warns `config.unusable` and
   resolves both settings off. `ExternalRef` is never true unless `Required` is.
3. **Gated where a human is present.** Luffy's triage prompt gains a section
   limiting an unparented Signal's outcomes, backed in Go by a refusal in
   `reconcile` (ACCEPT and FAST LANE) before any write, only while the Signal
   has no Chart or Spec successor.
4. **Ref resolution.** A ref is trimmed, any ref is accepted (not only `jira-`),
   and it is unique across all Initiatives, open or closed. It is checked only
   at birth.
5. **Accepted birth race.** Two Initiatives born concurrently with one ref can
   both pass the uniqueness check; no lock is taken.
