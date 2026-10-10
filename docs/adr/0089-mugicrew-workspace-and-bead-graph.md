# mugicrew on a foreign Workspace: Workspace vs Bead Graph, and where its record lives

mugicrew separates the **Workspace** (the git repo whose code it changes) from
the **Bead Graph** (the bd database it drives). The Bead Graph is whatever bd
resolves from the Workspace (`.beads/`, a `.beads/redirect`, `BEADS_DIR`, or
contributor routing). mugicrew reimplements none of that. It resolves the Bead
Graph once at startup (`bd where`) and exports the absolute `BEADS_DIR` to the
loop and every session it spawns, so detached loops and `$TMPDIR` worktrees all
see the same graph. A repo is refused only when bd resolves no graph from it.
Nothing bead-related has to be tracked in the Workspace. One loop drives
exactly one Bead Graph and one Workspace, and the loop lock is keyed on the
resolved graph, not on `<git-common-dir>`.

On any Workspace, owned or foreign, mugicrew's record goes into the Workspace's
history:

- land, anchor and revert commits and their trailers (ADR 0010 §2). These stay
  because `LandedBase` and History read them;
- its domain model, in mugicrew's own standard: `GLOSSARY.md`
  (+ `GLOSSARY-MAP.md`) and `docs/adr/NNNN-*.md`, created lazily, written on
  chart branches and landed with the Chart.

A Workspace's own decision or glossary convention is ignored, not adapted to,
and is never grounds to refuse to start. The ADR-clash guard is a no-op until
the first ADR creates `docs/adr/`. Having a Bead Graph resolve from the
Workspace is the owner's consent to all of this.

## A Workspace's own conventions

mugicrew ignores a Workspace's *doc* conventions (above), but crewmates follow
its *code* conventions. mugicrew relies on the harness auto-loading the
Workspace's `CLAUDE.md` and `AGENTS.md`, and re-delivers neither. The
`CREWMATE.md` preamble also names `CONTRIBUTING.md`, then `README.md`, for a
crewmate to read before it changes code. mugicrew only names these files. It
never injects their contents into a prompt: the harness stays content-blind,
and HITL launches have a ~16 KiB ceiling. If a crewmate runs the build or test
commands those docs give, that is its own choice. mugicrew itself still
discovers none. When the two disagree, the crewmate prompt and preamble win on
coordination (bd, branches, no push, closing beads), and the Workspace wins on
how its code is written, styled, built and tested.

## Considered Options

- **Keep the record out of history** (git notes, a side ref or bead fields for
  the land record, and a sidecar for docs). This is a clean foreign repo, but
  it rewrites `LandedBase` and loses chart-branch isolation and the clash guard
  for docs. Rejected for now. Moving docs alongside an out-of-Workspace graph
  (with chart branches in that repo too) is its own Signal.
- **Adapt to the Workspace's doc convention** (configurable ADR paths and a
  configurable guard). This is a configuration surface nobody needs yet.
- **A mugicrew `--graph` flag.** bd already resolves graphs. Adding a second
  way to name one invites the two to disagree.

## Consequences

- Contributor mode on a friend's repo still commits `docs/adr/` and
  `GLOSSARY.md` there, but only on mugicrew's branches and land commits.
  Pushing upstream is out of scope.
- Which repos use contributor mode (for example, an OE's vendored repos) is the
  hosting OE's policy, not mugicrew's.
