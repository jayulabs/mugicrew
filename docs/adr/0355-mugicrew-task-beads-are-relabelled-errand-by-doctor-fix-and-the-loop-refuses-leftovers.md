# ADR 0355: mugicrew Task beads are relabelled Errand by doctor fix, and the loop refuses leftovers

Status: Accepted
Date: 2026-10-23
Completes: ADR 0354 (mugicrew-the-task-kind-is-renamed-errand), which left the migration of existing beads and Specs open.

## Context

ADR 0354 renames the kind to Errand on every surface. Existing beads still
carry `kind:task`, and mugicrew ships on npm, so every upgraded Workspace's
graph may hold some. No reader validates a kind label: under the new binary a
`kind:task` bead is an unknown kind and fails silently (the TUI does not treat
it as a HITL leaf, an open before-land Task freezes its Checkpoint, withdraw
refuses it, and five readers that look at **closed** Tasks stop finding them:
merge-sha lookup, the hold re-file `related` edge, the slicing gate's
`ErrandChildren`, the triage follow-up container and slicing's title dedupe).
See `apps/mugicrew/docs/research/task-kind-readers-writers-and-migration-exposure.md`.

## Decision

1. **Relabel in place.** Every `kind:task` bead, closed ones included, becomes
   `kind:errand`. bd's `label_added`/`label_removed` events are the audit
   trail; no comment is added. The relabel must not move a closed bead's
   `closed_at` (the hold re-file orders by most recently closed); the Build
   proves this in a test.
2. **The relabel ships in the binary, through doctor.** `mugicrew doctor` shows
   a warn row counting beads that still carry `kind:task`; `mugicrew doctor fix`
   relabels them as a safe fix. It is idempotent and works on any Workspace
   that upgrades.
3. **No reader accepts `kind:task`.** No Go reader, registry row or prompt
   treats it as Errand. Instead the loop, and the TUI's dispatch, refuse to start
   while any `kind:task` bead remains, with an error naming
   `mugicrew doctor fix`. This extends ADR 0354's no-silent-alias rule from
   the verb and adopt line to the label.
4. **Landed Spec bodies stay as history.** A closed Spec's `## Tasks` section
   is not rewritten. No code reads it again.
5. **An open Spec still written the old way** (`## Tasks`, `task: <id>`) gets a
   doctor warn row naming it, with a manual fix (edit it to `## Errands` /
   `errand: <id>`). Slicing fails loudly on the old heading or adopt line,
   naming the new form. Doctor never rewrites Spec prose.
6. **A `kind_profiles` key `task`** gets a manual fix on doctor's
   agent-profiles row ("rename key `task` → `errand`"). Config TOML is never
   rewritten automatically.
7. **Cutover order** (release note): stop the loop, upgrade, `mugicrew doctor
   fix`, restart. An in-progress Task (here, "Walk the Signal-to-land
   walkthrough on a scratch repo…") is relabelled with the rest and keeps its
   claim and `## Steps` body. Its next launch walks it with Brook's Errand
   prompt. A bead an old binary files as `kind:task` after the cutover is caught
   by the refusal in (3) and relabelled by the next `doctor fix`. A live
   session calling `mugicrew task new` hits the error that names `errand new`.
8. **Lifetime.** The doctor rows and the loop refusal stay indefinitely,
   because a Workspace may upgrade from an old release at any time. Retiring
   them takes a later Signal.

## Considered Options

- **Read both labels.** Normalising `task` → `errand` at the kind-parse points
  covers Go. But the slicing and pivot prompts text-match raw labels and would
  have to name `kind:task`, putting the old name back in front of agents, and
  `bd list --label kind:errand` would miss the old beads. The alias would never
  go away.
- **Relabel open beads only.** That would leave the five closed-bead readers
  blind to every closed Task.
- **A one-off relabel of this graph only**, with a `bd` one-liner in release
  notes. This was rejected because other Workspaces that missed the note would
  degrade silently.
- **Relabel automatically at loop start.** This was rejected because it writes
  to the graph without anyone asking. The refusal plus doctor keeps the write
  explicit.
- **Warn without refusing.** This was rejected because it leaves the silent
  failures running until someone happens to run doctor.
