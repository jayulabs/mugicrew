# ADR 0354: mugicrew the Task kind is renamed Errand

Status: Accepted
Date: 2026-10-22
Supersedes: the naming (not the behavior) in ADR 0113 (task-production-and-ordering-against-checkpoint-lands) and kin: ADR 0114–0118, 0335, 0336, 0337, 0342, 0350, 0352.

## Context

Brook's HITL walk-through of steps that change the outside world was named
Task. Agents confuse it with every other "task": bd's issue type (every leaf
is a bd `task`), Claude Code's Task/TaskCreate/TodoWrite tools, and "your task
is…" prompt wording. Only the mugicrew kind is ours to rename. The collision
check (`docs/research/task-kind-rename-candidate-collisions.md`) swept both
glossaries, the prompts, crew docs, Go, ADRs, bd and Claude Code.

## Decision

The kind is named **Errand**: a one-shot trip into the outside world to get
something done. Landed ADRs keep saying Task; this ADR maps them. Every
surface takes the same name:

| surface | was | becomes |
|---|---|---|
| kind label | `kind:task` | `kind:errand` |
| Go | `contract.KindTask`, `bd/tasknew.go`, `bd/taskbody.go`, `TaskChildren`, … | `contract.KindErrand`, `bd/errandnew.go`, `bd/errandbody.go`, `ErrandChildren`, … |
| CLI verb | `mugicrew task new` | `mugicrew errand new` |
| Spec section | `## Tasks (optional)`, `### <Task title>` | `## Errands (optional)`, `### <Errand title>` |
| adopt line | `task: <id>` | `errand: <id>` |
| ordering line | `after: <Task title>` | `after: <Errand title>` (`after: land` / `before: land` unchanged) |
| compound | Hold Task | Hold Errand |
| crew doc | `task-body.md` | `errand-body.md` |
| Brook's prompt | `brook/prompts/task.md` | `brook/prompts/errand.md` |
| TUI | "Task" in dialogs and header text | "Errand"; no key moves and the New menu gains no entry |

The glossary term becomes **Errand**, with a _History_ line (named Task until
this ADR) and _Avoid:_ "Task" (bd's issue type, Claude Code's Task tools),
"chore", "runbook"/"playbook", "TODO", "manual".

Unchanged: the body's `## Steps` / `## Diagnostics` sections, the run-record
comments (`passed`, `amended`, `stuck`), and Brook's verb ("walk a human
through an Errand"). The old verb and adopt line are not silent aliases: they
fail with an error naming the new form. How existing `kind:task` beads and
landed Specs' `## Tasks` sections migrate is decided separately.

## Considered Options

- **TODO, MANUAL, task-list** (the Signal's): TODO hits bd `todo`, TodoWrite and
  `kindPrefix` stripping `TODO:` titles; MANUAL hits `paused:manual` and
  **manual fix**; task-list keeps the word "task".
- **Chore, Routine**: a bd type and commit type; Claude Code's scheduled agents.
- **Runbook, Playbook**: read well but name a reusable document, not a one-shot
  bead with a run record; Runbook also clashes with
  `docs/client-dry-run-runbook.md`.
- **Walk**: already Brook's verb and a graph-walk idiom; "Hold Walk" is awkward.
