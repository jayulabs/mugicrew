<!-- hand-kept · summarises ADR 0005 (the pivot), ADR 0043 (pivot raise contract and Checkpoint gate), ADR 0044 (the amend outcome and its AMENDMENT comment), ADR 0045 (concurrent pivots: one FR, one pivot), ADR 0018 and ADR 0019 (critiques as Checkpoint members), ADR 0059 (Drafts), ADR 0088 (this doc's split) -->

# Checkpoint rules

Shared by every crewmate working a Checkpoint (review, build, conflict, pivot).
A Checkpoint is the container of a Spec's Builds on its branch
checkpoint/<checkpointID>; find its id from your bead's parent membership edge
unless your prompt names it. In the commands below, <your bead id> is the bead
your prompt names as yours. Every bead you create is a Draft, as your prompt's
Drafts block says.

## The effective Spec

The Spec a Checkpoint implements is its **effective** Spec: the Spec body plus
its comments (bd show <specID> prints both). A comment

    AMENDMENT (<pivot>): <FR> — <decision> — rejected: <options>

overrides the FR it names; with several for one FR, the latest wins. An amended
FR is settled: build, review and resolve it as amended. Code matching an amended
FR is conformant; never file or re-raise the stale FR's wording against it. A
Critique that copies an amended FR already carries the decision: build that.

## Filing a Critique

A Critique is a defect on the Checkpoint's code, fixed test-first by Franky. It
is a `kind:critique` member of the Checkpoint:

    bd create --type task --labels kind:critique,drafted-by:<your bead id> -s deferred --no-inherit-labels --parent <checkpointID> \
      --title "<what's wrong>" --description "<the defect, where it is, and how to fix it>"

`--no-inherit-labels` is MANDATORY: without it the Critique inherits
kind:checkpoint and routes to the wrong owner. A Critique carrying a pivot's
refiled FR also takes `--deps discovered-from:<pivotID>`.

## Raising a pivot

A pivot says the **Spec itself is wrong** for one FR: following it as written
cannot yield a correct slice. Code that doesn't do what the effective Spec says
is a Critique, not a pivot. One FR maps to one pivot, so check the open pivots
first:

    bd dep list <your bead id> --direction=up -t discovered-from --json  # yours, if a prior run already raised
    bd dep list <checkpointID> --direction=down -t blocks --json  # open kind:pivot rows are the Checkpoint's pivots

- **Merge.** An open (or `deferred` Draft) pivot in either list already names
  the SAME FR → comment your evidence on it and mark yourself a raiser of it
  (Go keys on this edge):

      bd comments add <pivotID> "<your evidence>"
      bd dep add <pivotID> <your bead id> -t discovered-from

- **Raise.** None does → raise your own pivot, an offshoot (NOT a Checkpoint
  member):

      bd create --type task --labels kind:pivot,drafted-by:<your bead id> -s deferred --no-inherit-labels \
        --deps discovered-from:<your bead id> \
        --title "<FR>: <what's wrong>" \
        --description "FR: <the one FR it concerns>. Evidence: <why the Spec is wrong as written>"

Raise one pivot per distinct wrong FR. Go sees a pivot discovered-from your
bead, freezes the Checkpoint (the pivot blocks it until it closes) and closes
your bead `pivoted: <pivot>` in the same publish as your Drafts. Never
improvise a redesign for that FR; your prompt says what you do next.
