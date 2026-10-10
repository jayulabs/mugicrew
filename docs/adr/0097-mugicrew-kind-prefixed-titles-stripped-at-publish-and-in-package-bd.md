# ADR 0097: mugicrew strips kind-prefixed bead titles at publish and in package bd, never rejects

Status: Accepted
Date: 2026-10-08
Backstops: ADR 0088 (CREWMATE.md rule S17)

## Context

A bead's kind lives on its `kind:*` label, never in its title. CREWMATE.md, the
crew docs and promptguard's `kindTitle` lint already say so, but those are all
text checks: nothing stops a title like `Critique: …` when mugicrew writes a
bead, and Go copies titles down the succession chain, so one prefix spreads to
every successor. No single place catches every title. The publish batch sees
every prompt-written title (each one is a Draft), and package `bd`'s create
builders see every Go and TUI title. Decided in grilling.

## Decision

1. **One matcher, derived from contract.** `contract.StripKindPrefix(title)`
   sits next to the Kind consts and is built from all of them (17 today):
   - it matches a leading kind name and a colon, ignoring case, allowing
     whitespace before the name and before the colon
   - the colon is required, so `Checkpoint mc-x conflicts with main` is left
     alone
   - longer names are tried first, so `Chart-conflict:` is stripped whole
   - repeated prefixes are stripped (`Spec: Chart: foo` → `foo`)
   - if stripping leaves the title empty, the original title is kept
2. **Two call points, both in package `bd`.**
   - (a) `bd.Publish`: each Draft's `update <id> status=open` batch line also
     carries `title="<stripped>"` when the title changes, quoted the way the
     batch already quotes the close reason. This covers every prompt-written
     title and every Go-created Draft.
   - (c) the four create argv builders (`unparentedArgs`, the Surgery's
     Checkpoint, `successorCreateArgs`, `childCreateArgs`) and `bd.Update`.
     `bd.Update` strips both the guided `--title` and a `--title X` /
     `--title=X` in raw TUI args. This covers every Go-composed or inherited
     title, `mugicrew reconcile|pivot confirm|signal new|surgery new`, and
     every TUI create and edit.
3. **Strip, never reject.** A rejected publish fails the effort: AFK deletes
   its Drafts and starts over, and HITL is left stuck waiting for a resume or
   a drop. A rejected Go create fails a step over a title the step often
   copied and can't fix. A wrong prefix doesn't justify either cost.
4. **Every strip is recorded.** `slog.Warn("title.stripped", "from", …,
   "to", …)` is emitted at the strip site, with the bead id where one exists
   (publish, update) and the parent or creator on a create. It goes to the
   event log, never stderr, so the TUI isn't corrupted.
5. **promptguard keeps its own list.** `kindTitle` is a test-time lint on
   prompt text. contract's tests import promptguard, so sharing contract's
   list would be an import cycle. It may gain `Surgery` and `Task`.

## Considered and rejected

- **A `bd` shim on PATH.** It would have to parse about eight bd forms that set
  a title (positional, `--title`, `-f`, `--graph`, `q`, `edit`, `import`,
  batch lines). It would also have to stay off the human's own shell, and
  every launcher would need its own PATH change. That's more machinery for no
  coverage (a)+(c) don't already give.
- **Either point alone.** (a) misses the ten-plus non-Draft Go creates and
  every TUI write. (c) misses every bd call a prompt makes itself.
- **A Claude Code PreToolUse hook.** Not needed for anything in scope today.
  It is the fix for the gap below if that gap ever matters.

## Consequences

- **Known gap.** A prompt that ignores `contract.DraftInstruction` and creates
  a bead that isn't a Draft, or retitles a bead that's already published, gets
  through. No prompt does either today.
- A human's raw `bd create` / `bd update` is never touched (by scope).
- The ~220 existing prefixed titles stay as they are. A Go successor of one of
  them gets a clean title the next time it's created.
