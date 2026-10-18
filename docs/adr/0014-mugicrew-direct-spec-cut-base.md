# ADR 0014: mugicrew direct-Spec checkpoint cut base — the Spec-anchor trailer

Status: Accepted
Date: 2026-09-23

Superseded in part by: ADR 0287 (a-direct-spec-s-anchor-is-a-bead-pin-under-every-land-strategy) — §1 (the mechanism only; the
exact-base-at-birth principle survives), §2 and §4. §3 is restated: a Spec with
neither an `Anchor:` pin nor a `Spec:` trailer is a hard failure.

> **Amended by ADR 0212:** under `land_strategy=gitlab` the anchor is a bead pin,
> not a commit; ADR 0287 makes that the rule under every strategy.

Amends: ADR 0010 (the landed-base seam now spans a headless direct Spec, not
only a container-branch land) and ADR 0012 (a mugi-grill session gains a
mandatory terminal anchor commit).

> **Producer changed by ADR 0035:** mugi-grill is retired (ADR 0035 §13). The
> `Spec:` anchor is now written by Nami's fast-lane triage handler (ADR 0035
> §4, always empty). Decisions 1–4 are unchanged.

## Context

ADR 0010 fixed the cut base for the Chart-backed path: Zoro walks
`Spec —caused-by→ Chart`, re-derives the Chart's landed base via
`LandedBase("Chart: "+chartID)`, and cuts `checkpoint/<id>` from it. It
explicitly rejected `rev-parse main` at cut time — the loop serializes lands
(ADR 0003), so mainline drifts between the Chart land and Zoro's later cut, and
`rev-parse main` would return the latest trunk, not *this* Chart's landed base.

ADR 0012 then introduced the **direct headless Spec**: a `mugi-grill` session
files one `kind:spec` with no Signal and no Chart. There is therefore no chart
branch, no rebase+squash land, and no `Chart: <id>` trailer — **no landed base
to cut `checkpoint/<id>` from.** Surfaced live on a direct Spec and
its Checkpoint, which was cut from `main` as a stopgap this session and
flagged the gap on the Zoro Chart. This ADR settles the direct-Spec cut base and
reconciles it with ADR 0010's rejection.

## Decision

**1. The cut base is an exact recorded anchor, re-derived by trailer — never
`rev-parse main`.** A `mugi-grill` session's terminal act, before filing the
Spec, is to ensure **exactly one commit on mainline carries a `Spec: <spec-id>`
trailer** (`TrailerFor("spec", id)` already produces that string). Zoro cuts the
Checkpoint from `LandedBase("Spec: "+specID)` — the *same* seam the Chart path
uses, keyed on the Spec's own id instead of a `caused-by` Chart, so there is one
`CutBranch(name, base)` call site with two ways to compute `base`. `rev-parse
main` is rejected for the direct Spec for the *same* reason ADR 0010 rejected it
for the Chart: trunk drifts between Spec-filing and the later cut, and cutting
from latest `main` silently bundles unrelated concurrent lands into the
Checkpoint's base, breaking reproducibility. The drift is smaller here (no
guaranteed intervening land) but real, and the anti-pattern is identical.

**2. The anchor commit always exists — empty when there are no artifacts.** When
the session writes ADR / glossary / `CONTEXT.md` commits, the trailer rides the
final one (or a separate terminal commit — the session's latitude, since Zoro
only greps the trailer). When the design needed **no** artifact commit, the
session emits `git commit --allow-empty` carrying only the `Spec: <spec-id>`
trailer. The empty commit is not waste: it **freezes the Spec's birth point** on
mainline, which is exactly what the exact-anchor cut needs. This mirrors the
Chart path, where the land is always exactly one `Chart: <id>`-trailered commit.
Invariant: **exactly one `Spec: <spec-id>` commit on mainline per direct Spec.**

**3. A missing anchor is a hard failure, not a fallback.** (Restated by ADR 0287: a
Spec with neither an `Anchor:` pin nor a `Spec:` trailer.) If Zoro dispatches on
a headless `kind:spec` and `LandedBase("Spec: "+id)` finds no trailer, it errors
and does **not** cut. A silent `rev-parse main` fallback is the drift-prone path
decision 1 rejects, wearing a disguise; a missing anchor means the producer
(`mugi-grill`) is broken, and that must be surfaced, not papered over.

**4. No bead-stored SHA.** Consistent with ADR 0010: the anchor is a derived,
immutable git fact recovered by trailer grep. Duplicating it onto the Spec bead
buys nothing.

**Precondition.** A `mugi-grill` session runs on and commits to **mainline
directly** — it has no branch of its own (only container kinds are branches,
ADR 0009; a direct Spec is a headless leaf). So the anchor commit is already on
the branch Zoro reads; no land step is reintroduced.

## Consequences

- The landed-base seam is **uniform across both Spec origins**: a Chart-backed
  Spec cuts `LandedBase("Chart: "+chartID)` after the `caused-by` walk; a direct
  Spec cuts `LandedBase("Spec: "+specID)` with no predecessor walk. The `git/`
  package needs no new function — `LandedBase` and `CutBranch` already suffice.
- `mugi-grill` gains one mandatory terminal step (emit the `Spec:` anchor
  commit); its SKILL flow and ADR 0012 carry that addition.
- `CONTEXT.md`'s **landed base** glossary gains the direct-Spec **anchor** as a
  sibling case — the `Spec:` trailer joins `Chart:` / `Checkpoint:`.
- Auditability: a headless Spec's cut base is a real, greppable commit on
  mainline, replacing the prose "cut from main" stopgap.

## Alternatives considered

- **`rev-parse main` at cut time** (the shipped stopgap). Rejected:
  reintroduces ADR 0010's drift for the direct-Spec case; the same anti-pattern,
  merely quieter.
- **Trailer only when artifacts exist, else `rev-parse main`.** Rejected: two
  Zoro code paths and drift on exactly the no-artifact case — the empty anchor
  commit removes the need for a fallback at all.
- **Store the anchor SHA on the Spec bead.** Rejected: ADR 0010 already refused
  bead-stored SHAs — an immutable git fact duplicated into bd for no gain.
- **Silent `rev-parse main` fallback when the anchor is missing.** Rejected
  (decision 3): papers a non-reproducible base over a broken producer instead of
  surfacing it.
