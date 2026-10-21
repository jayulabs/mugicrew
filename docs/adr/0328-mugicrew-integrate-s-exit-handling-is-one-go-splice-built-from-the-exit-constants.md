# ADR 0328: integrate's exit handling is one Go splice built from the exit constants

Status: Accepted
Date: 2026-10-21
Amends: ADR 0102 (leaf-efforts-integrate-onto-ref-only-containers…) §3, ADR 0264 (a-squash-integrate-replays-from-the-effort-s-recorded-cut-point…)

## Context

Five packaged texts (Franky's build, conflict and surgery prompts, Nami's
research prompt, crew doc defog-session.md) each restated `mugicrew integrate`'s
exits by hand, and all five drifted: they file "landed" under exit 2 though
`session.ExitLanded` is 3 (ADR 0102 §3), and none knows exits 4 (no cut point)
and 5 (off pin) from ADR 0264. "The container was rewritten" invites a
crewmate to repair branches by hand, the wrong action.

## Decision

1. **One Go splice** in `contract/contract.go`, beside `DraftInstruction`, renders
   the exit list from the `session.Exit*` constants, so the text cannot drift from
   the code again (ADR 0088 (mugicrew-crewmate-content-split) §4).
2. **Spliced into every prompt that runs integrate**: build, conflict, surgery,
   research, grilling, prototype, promote-or-stamp.
3. **defog-session.md stops restating the codes**: its integrate step points at
   "your prompt's integrate exits".
4. **The splice's text**: 0 integrated or nothing ahead; 2 with a rebase stopped,
   resolve and re-run; 2 with none stopped, held; 3 landed; 4 or 5, the container
   was rewritten past your cut or is off its pin: never repair or rebase branches
   by hand. On any exit but 0 or a resolved 2: note the output on your bead, leave
   it open and stop.

## Considered Options

- **Fix each inline list in place.** Cheaper now, but it is the same five-way
  restatement that drifted twice.
