# ADR 0201: A landed container's child closes obsolete:, and its kept branch does not hold the drain

Status: Accepted
Date: 2026-10-15

Chart mc-t72m.12 (a child whose integrate is refused as landed files an
offshoot Signal). Settles grilling mc-t72m.12.4. The landed path closes the
child and keeps `effort/<child>` for the Signal to point at, but
`DrainBlockersOver` counts a closed child holding its effort ref as held (Spec
D7), so the landed Chart would never drain: loop candidacy, nami's
`reconcileChart` and TUI run-now all refuse it, its Landing never closes
`landed:` and its Spec is never cut
(docs/research/closed-child-with-kept-effort-branch-vs-dispatch-and-drain-gates.md
§2a, §3).

## Decision

1. **The close reason is the marker.** bd carries
   `ContainerLandedReason = "obsolete: container landed"`; the child closes
   `ContainerLandedReason + "; carried by <signal>"`.
2. **`DrainBlockersOver` skips it.** A closed child holding `effort/<child>`
   whose close reason starts with `ContainerLandedReason` is not held. The one
   change serves all three readers, the TUI's I/O-free one included, since the
   close reason is already on the child read. `effort/<child>` is kept.
3. **`MembersDrainedOver` is unchanged.** Every Checkpoint path reads the
   landed trailer before any drain check (research §2b), so a landed
   Checkpoint's held member never stalls it.
4. **The child reads as swept, deliberately.** The `obsolete:` prefix makes
   `session.Swept` true: a later backstop run takes the swept arm (idempotent),
   and dispatch's tail runs `retire`, dropping the handed edges and close,
   closing the effort's non-Signal Drafts and publishing its Signal Drafts.
   Drafts aimed at a landed container would otherwise go live as open children
   and stall the drain again.
5. **`retire`'s wording is container-neutral:**
   `obsolete: <child> of <container> was retired`, for sweeps and landed
   containers alike.

## Considered Options

- **A "container landed" short-circuit in `OpenChildren`/`reconcileChart`.**
  Rejected: it needs a landed read in the TUI's I/O-free gate and ahead of the
  loop's `ChartLandingHeld`, and it would drain a Chart past genuinely open
  defog children, cutting a Spec over live fog.
- **Delete `effort/<child>` after the Signal records its range.** Rejected:
  with no ref, the stranded commits survive only in the reflog and `git gc`
  can prune what the Signal names.
- **A non-`obsolete:` reason** (e.g. `stranded:`). Rejected: `Swept` stays
  false, so the tail publishes the effort's Drafts as open children of the
  landed container, holding the drain we just exempted.
