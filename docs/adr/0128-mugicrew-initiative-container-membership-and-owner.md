# ADR 0128: mugicrew Initiative: a Luffy-owned container whose membership spans every bead of its member chains

Status: Accepted
Date: 2026-10-11

## Context

The Multi-phase initiatives Chart puts fan-out in a new opt-in container kind
above the spine, keeping ADR 0008's 1:1 succession. Container membership is
the workstream tag. This grilling settled the container's name, what it holds,
who owns it, and how beads enter it. The edges it adds to ADR 0002's taxonomy
and the amendment to ADR 0008 belong to the Spec.

## Decision

1. **Name:** Initiative, `kind:initiative`, a container (bd `epic`).
2. **Membership spans the whole chain.** The Initiative parents (`parent-child`)
   its member Signals *and* each successor (Chart, Spec, Checkpoint) as it is
   born. The live phase therefore sits directly under the Initiative, and a
   manual pause on it cascades to every member (ADR 0081).
3. **Owner: Luffy, steward and handler.** He creates the Initiative and decides,
   when it drains, whether a next phase opens or it is done. Moving Signal
   intake from Nami to Luffy is separate work (an offshoot Signal), and every
   open defog bead on this Chart waits for it to land.
4. **At most one Initiative per bead**, which `parent-child` enforces.
5. **Adoption is an explicit human act.** It re-parents an existing chain's
   unclosed head bead, and membership carries forward from there. Closed
   predecessors are not rewritten. Moving between Initiatives is a release,
   then an adoption.
6. **Offshoots are born outside.** An offshoot of a member joins only by
   adoption, so an Initiative never grows silently.

## Considered options

- **Member Signals only.** Rejected: Signals close at triage, so finding the
  live work becomes a `caused-by` walk.
- **A non-blocking `tracks` edge instead of `parent-child`.** Rejected: no pause
  cascade, and nothing enforces single membership.
- **Nami as owner (she triages today).** Rejected: deciding scope, priority and
  the next phase is adjudication, which is Luffy's role.
- **An unowned, human-held grouping.** Rejected: it breaks the kind-ownership
  invariant (ADR 0002).

## Consequences

- Charts and Checkpoints can now have a parent, so membership can reach depth 2
  (Initiative → Chart → defog). ADR 0081 already cascades at any depth.
- Every successor-spawning path must copy the predecessor's Initiative parent.
