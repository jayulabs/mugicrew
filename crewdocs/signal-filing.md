<!-- hand-kept · Spec mc-0eot: one place for how a crewmate files a raised Signal -->

# Signal filing

How to file a Signal you raise mid-session. Your prompt says *when* to file and
what the description should hold; this doc says *how*. Every Signal you file is
a Draft, as your prompt's Drafts block says. In the commands below,
<your bead id> is the bead your prompt names as yours.

## When

- HITL session: propose the Signal in chat (title, what and why) and file it
  only on the human's explicit yes.
- Autonomous session: file without asking.

## Offshoot form

A Signal for work you found, not a member of any container:

    mugicrew signal offshoot --from <the bead that found it> --drafted-by <your bead id> \
      --order follow-up|prerequisite|none \
      --title "<the work, no kind prefix>" --description "<what and why; Sources: …>"

Pick the order and say why in the description:
- follow-up: the work waits until its container lands;
- prerequisite: what you were working on can't land until this work lands;
- none: separable work with no order either way.

In a HITL session, include the order in your chat proposal. The verb finds the
container, records the order, and prints the new id. If the order can't apply
(no container, a closed one, or a pair bd refuses), it files the Signal
unordered and says why on stdout and in the body. Don't add edges yourself.

Some filings are human-only. A crewmate never files a Rollback, never runs
mugicrew rebase or rebases a container by hand, never files a Surgery, and
never files a Task mid-work. Each one is suggested by an offshoot Signal
instead, and the human decides.

## Initiative member form

For Luffy filing a phase Signal into an Initiative: a raw create with `--parent <initiative id>`:

    bd create --type task -s deferred --no-inherit-labels -l kind:signal,drafted-by:<your bead id> \
      --parent <initiative id> --title "<the work, no kind prefix>" --description "<what and why>"

## Retry

If it fails with *already exists* or *1213*, re-run the same create: nothing was written, and the retry gets a fresh ID. This applies to the verb's create and the member form.

## Rules

- `--no-inherit-labels` is MANDATORY on the member form (the verb sets it itself): without it the Signal inherits its
  parent's or finder's kind label and is misrouted or mislisted.
- Exactly one kind label, kind:signal.
- No `--priority`: put urgency in the body.
- No kind prefix in the title.
- Cite the beads the Signal is about by title in its body; don't link them.
- When your prompt asks, name the filed Signal in your session record.
