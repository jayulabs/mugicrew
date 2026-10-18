# ADR 0230: A bare MR iid means the Workspace's project, and a nested Landing names its MR by project path

Status: Accepted
Date: 2026-10-16

Chart mc-t72m.25 (Each nested repo lands through its own GitLab project under gitlab).
Settles grilling mc-t72m.25.5 after the seam map
(`docs/research/gitlab-seams-scoped-to-a-nested-repo-handle.md`).

MR iids are per project. Once a nested repo lands through its own GitLab project,
a bare `!42` could name an MR in either project. We qualify only where the
ambiguity lives, so a Workspace with no contexts pays nothing (ADR 0157
(…no-context-cost…)).

## Decision

1. **A bare `!<iid>` always means the Workspace's project.** Only the surfaces of a
   container whose context resolves to a nested repo qualify the MR. A Workspace
   container's wording is unchanged even when contexts are declared.
2. **A nested Landing records its MR as `MR: <group/proj>!<iid>`** (GitLab's
   cross-project reference syntax). `recordMR` writes it, and `RecordedMR` parses
   both forms. The Landing body stays the single I/O-free source of the project
   path for the TUI. A no-context Landing's line stays `MR: !<iid>`, byte for byte.
3. **Tasks and Critiques filed by the MR lifecycle** (trailerless and foreign holds,
   Forge close, red pipeline, revert-MR holds) name a nested MR `<group/proj>!<iid>`
   in title and body. Their idempotency keys stay `Merged MR: !<iid>`. A key is
   per container and a container lands through exactly one project, so it is
   unambiguous, and existing Tasks still dedupe.
4. **Events.** `land.<outcome>` and every `revert.*` event of a nested Landing or
   Rollback carry the same nested attrs that `land.pending` and `land.done` already
   carry (`context`, `repo`, `into`), with `into` set to the child's mainline. There
   is no `project` attr, because the context resolves to it.
5. **TUI.** On a nested Landing, the Execute Rollup's `!<iid>` qualifier (ADR 0226
   (a-gitlab-landing-s-mr-iid-and-sub-state…)) reads `<context>!<iid>`. The context
   comes from the container's `context:` label, which keeps the row short. `d`'s
   why: line shows the full `<group/proj>!<iid>` from the body line.

## Considered Options

- **Qualify every MR once any child project is declared.** Rejected. It changes
  the Workspace's own wording for no gain, since bare already has one meaning.
- **Rely on the `context:` label alone, keeping `!N` bare everywhere.** Rejected.
  Tasks and log lines are read away from the container, where `!42` is ambiguous.
- **MR web URL in Tasks.** Rejected. No URL is recorded, and a project path plus an
  iid pastes straight into GitLab.
- **`d` reads the child's remote to get the path.** Rejected. It would break the
  TUI's I/O-free read (ADR 0198 (tui-v2-execute-checkpoints-section…)).
- **A `project` event attr.** Rejected. It duplicates `context`.
