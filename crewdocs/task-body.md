<!-- hand-kept · summarises ADR 0113 (ordering is blocks edges only), ADR 0114 (the in-session guide), ADR 0116 (steps, checks and record), ADR 0335 (mugicrew-a-task-body-may-carry…), ADR 0118 (the irreversible marker) -->

# The Task body

A Task is a free-standing HITL bead that walks a human through changing the
external world: anything outside the bead graph and the code. Its body is plain
markdown that a guide session reads; nothing parses or lints it. Write it so the
guide can run each step and prove it done.

## Format

The body is one numbered `## Steps` list. Each step has a title line, a `Do:`
paragraph and a `Check:` paragraph:

    ## Steps

    1. Create the public repo.   [irreversible]
       Do: GitHub → New repository → owner `$OWNER`, name `mugicrew`, Public, no README.
       Check: `gh repo view $OWNER/mugicrew --json visibility -q .visibility` prints `PUBLIC`.

    2. Give this machine push rights.   [secret]
       Do: create a fine-grained token with Contents: write and hand it to the credential store.
       Check: `gh auth status` succeeds.

    3. Add the Publisher remote.
       Do: `git -C "$CLONE" remote add publisher <repo url>`
       Check: `git -C "$CLONE" remote get-url publisher` equals the repo url.

A body may also carry a one-paragraph lead, whole-line fact lines (`Key: value`)
before `## Steps`, and a trailing `## Diagnostics`. The first fact line is the
`Initiative:` line (the Initiative the Task joined, or why it was born outside
one); it is written by `mugicrew task new`, not the author, so never write it. The
guide walks only
`## Steps`: the lead and facts are context it may quote, and Diagnostics is
reference it reads on demand. It never checks or records a non-step.

## Markers

Markers go after the title, and tell the guide how to handle the step:

- `[secret]`: the step handles a value that must never pass through the session.
  The human runs a hidden-prompt command in their own terminal; the guide checks
  only the outcome (status, mode, names).
- `[elevated]`: the Do needs sudo, root or admin. The human runs it; the guide
  checks it.
- `[irreversible]`: the Do can't be undone. The guide states the consequence
  and gets the human's explicit yes before it, whoever performs it. Write any
  name or visibility pre-check as a step of its own.

A **web-UI step** needs no marker: a Do with no command is walked click by
click.

## Checks

- A Check is a command and its expected result. Write one wherever anything can
  be probed.
- `Ask:` replaces `Check:` only when nothing is probeable. The human confirms
  it, and the label shows it is weaker.
- A Check must be safe to re-run: read-only, or an idempotent no-op (such as
  `mugicrew integrate` exiting 0 with nothing ahead). The guide re-runs every
  Check on each resume. Anything that changes state on a second run belongs in
  a Do.

## No ordering lines

The body never carries `after:` or `before:` lines. A Task is ordered only by
`blocks` edges: on a Checkpoint (after its land, or holding it), or on another
Task.

## The record

The guide never edits a step to mark it done. Progress is one value-free
comment per passed step (`step N (<title>) passed: …`), plus `step N amended:`
for an approved rewrite and one `step N (<title>) stuck: …` on a stuck exit.
No record ever carries a secret value, token, password, private key,
credentialed URL or raw check output.
