<!-- hand-kept · summarises ADR 0030 (ADR-number collision guard), ADR 0079 §1 (escalate only on an unreadable referent), ADR 0088 (this doc's split) -->

# Renumbering clashing ADRs

A branch landing on mainline (a Chart or a Checkpoint) clashes when it adds an
ADR whose number some other ADR already holds on mainline. **Mainline numbers
are fixed; the landing branch moves.** A clean rebase alone does not resolve a
clash.

1. Rebase onto the base first, so mainline's ADRs are in view.
2. Each `docs/adr/` directory (at the root or nested under any path, e.g.
   `apps/<app>/docs/adr/`) numbers its own ADRs.
   For each colliding ADR, find the highest number in ITS directory on current
   mainline (`git ls-tree --name-only {{MAINLINE}} <its-dir>`, e.g.
   `apps/mugicrew/docs/adr/`), not just the carried base: mainline may have
   moved. Renumber it to the next free number above that max, in the same
   directory and with the same `NNNN-<slug>.md` slug, preserving the branch's
   ADRs' relative order (the lowest colliding ADR gets the lowest new number;
   if the branch also added non-colliding ADRs that would now sort out of
   order, move those up too). Rename the file (`git mv`) and fix its title line.
3. Sweep stale references to each moved number (`ADR NNNN` text and links to
   the old `NNNN-<slug>.md` path) ONLY in:
   - files the branch changed: `git diff --name-only $(git merge-base <base> HEAD) HEAD`
   - the branch's own beads, where your prompt names them (for a Chart: the
     Chart body and its defog beads, via `bd update`; not other beads that
     merely link to it).

   Never rewrite a file that came only from mainline, nor any other bead:
   there `ADR NNNN` means mainline's ADR. Decide a line's referent by reading
   it (a line may cite mainline's ADR of the same number).
4. Commit on the branch.

The clash is resolved when no ADR the branch adds shares a number with another
ADR in its own `docs/adr/` on mainline.

A line whose referent (the branch's ADR or mainline's) can't be told apart by
reading it is the one renumbering call not to guess: a wrong guess silently
changes meaning. Escalate it if your prompt gives you a way to; otherwise leave
the line as it is and say so in your output.
