# Collision check for candidate names of the Task kind

Status: Research (input to "Choose the new name for the Task kind", mc-8xo5i)
Date: 2026-10-22
Bead: mc-8xo5i.2

## Purpose

The Chart "Rename the Task kind" (mc-8xo5i) renames mugicrew's Task kind,
Brook's HITL walk-through of steps that change the external world
(`apps/mugicrew/GLOSSARY.md:740-766`), because agents confuse it with the other
uses of "task". This note checks each candidate name against the vocabularies
it could collide with. It also checks whether the name reads well in the
compounds it must form: `Hold <X>`, a `## <X>s` Spec section, a `<x>: <id>`
adopt line, `mugicrew <x> new` and `kind:<x>`. It decides nothing. It feeds the
naming grilling "Choose the new name for the Task kind".

The candidates are the Signal's three: task-list, TODO and MANUAL (Signal
"Consider renaming the TASK kind", mc-7scko). This note adds names that evoke a
human walking steps in the outside world: Errand, Chore, Walk, Runbook, Drill,
Checklist, Procedure, Playbook, Routine, Legwork, Fieldwork, Outing and Sortie.
Two control names, Run and Steps, show how a plain English word fares.

## 1. The surfaces a name must fit (current spellings)

- **Kind label and Go const:** `KindTask Kind = "task"`
  (`apps/mugicrew/contract/contract.go:42`). Hyphenated kind values already
  exist: `chart-conflict` (`contract.go:38`), `forge-review` (`contract.go:65`)
  and `mr-watch` (`contract.go:75`). So a hyphenated label is mechanically
  fine.
- **Title prefix stripping:** every kind value feeds `kindPrefix`. That regexp
  strips any leading `<kind>:` from a bead title, case-insensitively
  (`contract.go:88-121`). A kind name that people write as an ordinary title
  prefix would get silently stripped from those titles.
- **CLI verb:** `mugicrew task new` (`apps/mugicrew/main.go:145`, usage
  `main.go:224`). The top-level verbs it must not shadow are `signal`, `task`,
  `intake`, `latch`, `triage`, `probe`, `surgery`, `pivot`, `initiative`,
  `withdraw`, `reconcile`, `loop`, `abort`, `run` (hidden), `recover`,
  `checkup`, `ps`, `stats`, `integrate`, `rebase`, `chart`, `approval`,
  `land-lineage`, `hitl-run`, `transcript`, `doc-pane`, `doctor`,
  `hitl-follow` and `hitl-hook` (`main.go:83-202`).
- **Spec section and adopt line:** `## Tasks (optional)` and `task: <id>`
  (`apps/mugicrew/crewdocs/spec-template.md:59`, `:81`, `:84`).
- **Body:** a numbered `## Steps` list of Do/Check steps
  (`GLOSSARY.md:745-747`; crew doc `task-body.md`).
- **Compound:** Hold Task (`GLOSSARY.md:767-773`).
- **TUI keys:** no key is Task-specific today. A Task launches or resumes with
  `enter`/`b` and is withdrawn with `X` (`GLOSSARY.md:755-761`). The New menu
  has no Task entry: its keys are `s`, `j`, `i`, `u`, `r` and `d`
  (`apps/mugicrew/tui/newmenu.go:39-60`). The TUI branches on `KindTask` but
  binds no mnemonic to it (`tui/actions.go:204`, `tui/exectab.go:248-250`,
  `tui/graphtab.go:91`). A rename moves no key unless the grilling adds one.

## 2. Method and raw counts

Each candidate was matched as a whole word, case-insensitively (`grep -ciwE`),
against these sources:

- `apps/mugicrew/GLOSSARY.md`
- the root `GLOSSARY.md`
- `apps/mugicrew/*/prompts/*.md`
- `apps/mugicrew/crewdocs/*.md`
- non-test `apps/mugicrew/**/*.go`
- `apps/mugicrew/docs/adr/*.md`
- the help text of bd 1.3.0 (dev): `bd --help` plus `create`, `update`,
  `list`, `close`, `show`, `gate`, `formula`, `mol` and `todo --help`

Claude Code 2.1.308 was checked with `claude --help` and this session's tool
and skill list. The counts are line counts:

| candidate | mugicrew glossary | root glossary | prompts | crew docs | Go | ADRs | bd help |
|---|---|---|---|---|---|---|---|
| task-list (`task.?list`) | 0 | 0 | 0 | 0 | 1 | 1 | 0 |
| TODO | 1 | 0 | 0 | 0 | 6 | 0 | 13 |
| MANUAL | 5 | 0 | 1 | 1 | 86 | 135 | 1 |
| Errand | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| Chore | 0 | 0 | 4 | 0 | 3 | 1 | 3 |
| Walk | 2 | 0 | 4 | 1 | 30 | 36 | 0 |
| Runbook | 0 | 0 | 0 | 0 | 0 | 8 | 0 |
| Drill | 1 | 0 | 0 | 0 | 0 | 8 | 0 |
| Checklist | 0 | 0 | 1 | 0 | 1 | 11 | 1 |
| Procedure | 0 | 0 | 0 | 0 | 5 | 9 | 0 |
| Playbook, Routine, Legwork, Fieldwork, Outing, Sortie | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| Steps (control) | 7 | 0 | 68 | 26 | 122 | 255 | 6 |
| Run (control) | 16 | 4 | 106 | 16 | 878 | 452 | 16 |

A zero count only means the word does not appear. The specific collisions
behind the nonzero counts are in §3.

## 3. Candidates × collisions

| candidate | glossary / *Avoid:* | bd CLI | Claude Code | prompts, crew docs, code | mugicrew CLI / TUI | compounds | verdict |
|---|---|---|---|---|---|---|---|
| **task-list** | — | bd's type is still `task` (§3.1) | the Task tools' task list (§3.1) | goldmark `extension.TaskList`, i.e. Markdown checkbox lists (`apps/mugicrew/mdrender/render.go:36`) | — | "Hold Task-list", "## Task-lists" | **Reject**: keeps the word "task" |
| **TODO** | *Avoid:* "todo" for **Actionable** (`GLOSSARY.md:270`) | `bd todo` is a shortcut for bd `task` issues (§3.1) | `TodoWrite` tool (`CLAUDE.md:20`) | the rebase `todo` file (`apps/mugicrew/git/git.go:2587-2606`); `TODO:` code comments | — | `kindPrefix` would strip `TODO:` titles (`contract.go:88-121`); "## TODOs", "todo: <id>" | **Reject**: worst collision |
| **MANUAL** | **manual fix** is a doctor term (`GLOSSARY.md:361-368`); "A manual pause" (`GLOSSARY.md:858`) | — | `--permission-mode` has a `"manual"` choice (§3.1) | `PausedManual = "manual"`, the `paused:manual` label (`apps/mugicrew/bd/bd.go:627`; `chopper/prompts/checkup.md:58`); the TUI `p` toggles it (`tui/graphtab.go:146-148`) | — | "## Manuals" reads as documentation | **Reject** |
| **Errand** | — | — | — | — | no verb clash | Hold Errand · `## Errands` · `errand: <id>` · `mugicrew errand new` · `kind:errand` | **Clean** |
| **Chore** | — | `chore` is a built-in issue type (§3.1) | — | Conventional Commit type: `typ := "chore"` (`git/git.go:652-658`); `conventionalTypes` (`promptguard/promptguard.go:206`); commit-type lists in `franky/prompts/build.md:89`, `conflict.md:57`, `surgery.md:44`; `chore(...)` commits in `sanji/prompts/prototype.md:27` | — | fine | **Reject**: same failure as "task" (a bd type and a commit type) |
| **Walk** | Brook's role is to "walk a human through a Task" (`GLOSSARY.md:347-348`); "a HITL walk-through" (`GLOSSARY.md:740`) | — | — | Brook's prompt uses "walk them through it" as a verb (`brook/prompts/task.md:47`, `:70`); graph walks (`bd/spine.go:71-72`), 30 Go lines | — | "Hold Walk" is awkward and "hold" is already loaded (foreign hold, `GLOSSARY.md:1110`); "## Walks" | **Weak**: an overloaded verb inside Brook's own wording |
| **Runbook** | — | near `bd formula`/`bd mol` ("Manage workflow formulas", "work templates") in meaning (§3.1) | — | `apps/mugicrew/docs/client-dry-run-runbook.md` exists, and its sections are themselves `[Task steps 1-4]`, `[Task step 5, secret]` (`:33`, `:60`); ADR 0245 (mugicrew-install-is-doctor-led…) §5 keeps it | — | reads well, but a runbook is a reusable document, while a Task is a one-shot bead with a run record (`GLOSSARY.md:748-750`) | **Possible**, with one real collision |
| **Drill** | the TUI "Initiative drill-in" (`GLOSSARY.md:972`); ADR 0131 (mugicrew-tui-initiatives-nest…), 0136, 0174 and 0181 use "drill-in" | — | — | — | — | "drill" means rehearsal, but a Task changes the real world | **Reject**: wrong meaning, plus the drill-in clash |
| **Checklist** | — | — | — | slicing's "this checklist" (`zoro/prompts/slicing.md:174`); Markdown task lists (`mdrender/render.go:36`) | — | "Hold Checklist" is awkward | **Weak** |
| **Procedure** | — | — | — | 5 Go lines and 9 ADR lines of generic use | — | "Hold Procedure" | **Weak**: too generic for agents to tell apart |
| **Playbook** | — | near `bd formula` in meaning (§3.1) | — | — | — | fine | **Possible**: same "reusable document" misfit as Runbook |
| **Routine** | — | — | the `schedule` skill manages "scheduled cloud agents (routines)" (§3.1) | — | — | fine | **Reject**: Claude Code clash, and it implies repetition |
| **Legwork / Fieldwork** | — | — | — | — | — | mass nouns: "## Legworks" and "a fieldwork" don't read | **Weak** on grammar |
| **Outing / Sortie** | — | — | — | — | — | Outing has an unwanted sense; Sortie is military | **Weak** on meaning |
| **Run** (control) | **Run-now host**, `mugicrew run <id>` (`GLOSSARY.md:1015`) | — | the `run` skill | 878 Go lines | hidden `run` verb (`main.go:177`) | — | **Reject** |
| **Steps** (control) | the Task body's own `## Steps` section (`GLOSSARY.md:746`) | — | — | 68 prompt lines | — | "## Stepss" | **Reject** |

### 3.1 External tool output these rows rely on

- **bd 1.3.0 (dev) `bd todo --help`:** "Manage TODO items as lightweight task
  issues… `bd todo add "Title"` -> `bd create "Title" -t task -p 2`".
- **`bd types`:** the built-in types are `task, bug, feature, chore, epic,
  decision, spike, story, milestone`.
- **`bd statuses`:** `open, in_progress, blocked, deferred, closed, pinned,
  hooked`. No candidate collides with these.
- **`bd --help`:** includes `gate`, `formula` ("Manage workflow formulas"),
  `mol` ("Molecule commands (work templates)"), `swarm`, `human` and `todo`.
- **Claude Code 2.1.308 `claude --help`:** `--permission-mode` choices
  `"acceptEdits", "auto", "bypassPermissions", "manual", "dontAsk", "plan"`.
- **Claude Code's own tools and skills:** the repo's agent instructions name
  `TodoWrite` and `TaskCreate` (`CLAUDE.md:20`, `AGENTS.md:20`). This
  session's tool list includes `TaskStop`. Its skill list includes `run`,
  `loop` and `schedule`, whose description is "scheduled cloud agents
  (routines)". No candidate except TODO, task-list and Routine hits a Claude
  Code tool or skill name.

## 4. Ranked shortlist

1. **Errand.** It has zero hits in every vocabulary checked (§2): both
   glossaries, the prompts, the crew docs, the Go code, the ADRs, bd and
   Claude Code. It forms every compound naturally: Hold Errand, `## Errands`,
   `errand: <id>`, `mugicrew errand new`, `kind:errand`. It also carries the
   brief's meaning, a human going out into the world to get something done.
   Brook can keep "walk a human through an Errand" without the kind name
   clashing with the verb.
2. **Runbook.** It reads fluently and is the operations word for "steps a
   human executes". Its costs: the existing `docs/client-dry-run-runbook.md`
   would have to be renamed or explained, and the word suggests a reusable
   document (close to `bd formula`) rather than a one-shot bead.
3. **Playbook.** No hits in the repo, but it has the same reusable-document
   misfit as Runbook, which makes it weaker on meaning.
4. **Walk.** It is already Brook's verb (`GLOSSARY.md:347`) and a graph-walk
   idiom in the code. Both would become ambiguous, and Hold Walk is awkward.

Not shortlisted: TODO, MANUAL, task-list, Chore and Routine each collide
directly with bd, Claude Code or an existing mugicrew term. Drill has the
wrong meaning.

## Open questions / gaps

- The bead asks for the note under `apps/mugicrew/docs/research/`. The
  dispatch prompt fixes the path as `docs/research/` at the repo root, and
  this note follows the dispatch prompt. Moving it is a one-file `git mv`, if
  the grilling wants it there.
- The Claude Code tool names come from this session's live tool list and the
  repo's `CLAUDE.md:20`. They are not from a pinned Claude Code doc, and other
  harnesses (pi, see `apps/mugicrew/agentprofile/`) were not checked.
- The word counts are whole-word line matches. They catch neither substrings
  (for example "walkthrough" written as one word) nor plurals beyond the
  patterns shown. §3's rows were each read by hand, but only for the
  candidates with nonzero counts.
- The other Workspace contexts under `apps/` and `libs/` were not swept. Only
  the root and mugicrew glossaries were checked.
- Whether to also rename Brook's prompt file `brook/prompts/task.md`, crew doc
  `task-body.md` and the Go identifiers (`tasknew.go`, `KindTask`) is a
  spelling consequence of the name. That belongs to the grilling and to
  "Map every reader and writer of the Task kind and its Spec section".
