<!-- hand-kept · summarises ADR 0035 (a fast-laned Spec takes the standard template), ADR 0088 (one Spec template), ADR 0113 (a Spec's Tasks section) -->

# The Spec template

A Spec is a decision record naming WHAT to build and what's in and out. Its
body carries these seven sections, in exactly this order, so it slices into
Builds like any other Spec, then an optional Tasks section last:

## Problem Statement

The problem being solved, from the perspective of whoever the Spec is for.

## Solution

The solution to that problem, at low resolution: what gets built, in and out.

## Functional Requirements

A LONG, exhaustive, numbered list of functional requirements in user-story form:

1. As an <actor>, I want <feature>, so that <benefit>

Actors are drawn from the *target Spec's own domain*: an actor may be an end
user, an external system, a developer consuming a library, a CLI caller, or a
crewmate. Cover every aspect of the feature; this section preserves the *why*
behind each unit a later slicing turns into a Build.

## Build-sliceable Implementation Decisions

The decisions that were settled, framed so they slice cleanly into independent
Builds. This can include:

- The modules built/modified and the interfaces of those modules
- Architectural decisions and technical clarifications
- Schema changes, API contracts, specific interactions

Do NOT include specific file paths: they go stale fast. Exception: where a
prototype produced a snippet that encodes a decision more precisely than prose
can (state machine, reducer, schema, type shape), inline it within the relevant
decision and note briefly that it came from a prototype. Trim to the
decision-rich parts, not a working demo: just the important bits.

## Testing Decisions

The testing decisions that were settled. Include:

- What makes a good test (test external behavior, not implementation details)
- Which modules will be tested
- Prior art for the tests (similar types of tests already in the codebase)

## Out of Scope

The things ruled beyond this Spec, each with the gist plus why it is out.

## Further Notes

Any further notes about the feature.

## Tasks (optional)

Omit it when the Spec needs no step in the external world (anything outside the
bead graph and the code). Otherwise list each Task the slicing files beside the
Builds:

### <Task title>

after: land

<its ## Steps body, in crew doc task-body.md's format>

Each entry carries one or more ordering lines: `after: land` (the Task waits
for this Spec's Checkpoint to land), `before: land` (the land waits for the
Task) or `after: <Task title>` (another Task in this section). The ordering
lines become blocks edges; the Task's own body never carries them.
