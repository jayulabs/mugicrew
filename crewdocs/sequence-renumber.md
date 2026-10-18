<!-- hand-kept · summarises ADR 0030 (adr-number-collision-guard) §5, ADR 0079 §1 (escalate only on an unreadable referent), ADR 0088 (this doc's split), ADR 0219 (the-adr-number-guard-is-a-numbered-file-clash-check…), ADR 0225 (a-forge-merged-number-clash-is-renumbered…) -->

# Renumbering clashing numbered files

A branch landing on mainline (a Chart or a Checkpoint) clashes when it adds a
numbered file whose number some other file in the same sequence already holds
on mainline: an ADR (`docs/adr/NNNN-<slug>.md`, the built-in example) or any
file a configured sequence matches (e.g. `db/migration/V{n}__*.sql`). **Mainline
numbers are fixed; the landing branch moves.** A clean rebase alone does not
resolve a clash.

Under gitlab a clash can reach mainline through a human's Forge merge; the
later-landed container then moves after its land, through a renumber
Checkpoint cut from mainline. There the clashing files are already on your
branch's base, so step 1 is a no-op and the sweep's "files the branch changed"
are the files the landed container's squash commit changed
(`git diff --name-only <landed>^1 <landed>`), and its own beads are the landed
container's subtree, as your prompt names them.

1. Rebase onto the base first, so mainline's files are in view.
2. Each matched directory numbers its own files (for ADRs, each `docs/adr/`,
   at the root or nested under any path, e.g. `apps/<app>/docs/adr/`).
   For each colliding file, find the highest number in ITS directory on current
   mainline (`git ls-tree --name-only {{MAINLINE}} <its-dir>`, e.g.
   `apps/mugicrew/docs/adr/`), not just the carried base: mainline may have
   moved. The next free number is above that max. Move the branch's clashing
   files to the next free numbers, preserving their original order (the lowest
   colliding file gets the lowest new number; if the branch also added
   non-colliding files that would now sort out of order, move those up too).
   Mainline's numbers stay fixed.
3. Rename each file (`git mv`) keeping its directory, prefix and the rest of
   its name; change only the captured digits, at the renamed file's own width
   (`0007` → `0012`, never `12`). If the new path would leave the sequence's
   rule (the number outgrows its width), escalate instead of guessing.
4. Fix any number the file states about itself (an ADR's title line).
5. Sweep stale references to each moved number, for every sequence. Recognise
   them by reading, in these forms: the name stem, the path, a `V{n}`-style
   token, "migration {n}" prose, and `ADR NNNN`. Sweep ONLY in:
   - files the branch changed: `git diff --name-only $(git merge-base <base> HEAD) HEAD`
   - the branch's own beads, where your prompt names them (for a Chart: the
     Chart body and its defog beads, via `bd update`; not other beads that
     merely link to it).

   A line citing a file by slug or path has its referent settled by that slug
   or path; in sweep scope, a citation matching a moved file takes the new
   number and keeps its slug.

   Never rewrite a file that came only from mainline, nor any other bead:
   there a number means mainline's file. Decide a line's referent by reading
   it (a line may cite mainline's file of the same number).
6. Commit on the branch.

The Workspace's CONTRIBUTING.md and README.md renumber guidance wins on how;
mugicrew's invariants above win on what (mainline numbers fixed, the branch
moves, sweep scope, escalate on ambiguity). A conflict between them escalates.

The clash is resolved when no file the branch adds shares a number with another
file in its own number space on mainline.

A line whose referent (the branch's file or mainline's) can't be told apart by
reading it is the one renumbering call not to guess: a wrong guess silently
changes meaning. Escalate it if your prompt gives you a way to; otherwise leave
the line as it is and say so in your output.
