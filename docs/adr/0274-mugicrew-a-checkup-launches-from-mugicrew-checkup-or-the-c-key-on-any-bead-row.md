# A Checkup launches from `mugicrew checkup <bead>` or `c` on any bead row

Status: Accepted
Date: 2026-10-17

Chart mc-qcor ("A HITL Chopper session repairs a broken bead on a human's
command"), grilling bead mc-qcor.7. ADR 0272
(mugicrew-a-chopper-checkup-is-a-hitl-kind…) settled what a Checkup is. This ADR
settles how a human starts one, and how starting one sits beside `x`, which
already chooses between drop, abort and recover (ADR 0127
(mugicrew-recover-repairs-every-leftover…) §11).

## Decision

1. **Verb.** `mugicrew checkup <bead> [--problem "<one line>"]` does the whole
   launch. It finds the target's open Checkup or creates one, adds
   `paused:checkup` to the target, starts the HITL session on the repo's tmux
   server and attaches the human, the way bare `mugicrew` attaches to the TUI.
   `--problem` is optional. It seeds the Checkup body with the human's symptom
   for Chopper's first read, as with Surgery's `--problem`.
2. **Key.** `c` on a bead row in the Graph tab and the focus view, with the hint
   `c checkup`. It opens a one-line problem prompt, which may be left empty, and
   then does the same launch. The exec and defog tabs don't offer it.
3. **Rows.** Every bead offers it, open or closed, except a Checkup. A wrong
   edge, a wrong label or a container that drained wrongly doesn't look broken
   on screen, and a container that drained wrongly is closed. The hint always
   shows, and there is no new row colour.
4. **Refusals.** The verb and the key refuse in the same cases. The verb exits 1.
   There is no `--force`: the human clears the blocker first.
   - **A live AFK effort on the target** (`x` reads abort): "an effort is running
     on <title>: abort it (x) first". The fence can't stop it (ADR 0055).
   - **A dead claim with a clean recover verdict** (`x` reads recover): "stranded
     by a dead effort: recover it (x)". The check runs recover's inventory. When
     recover would refuse or calls the row ambiguous, the launch goes ahead,
     because those state faults are the Checkup's.
   - **A HITL session already open on the target** (`x` reads drop): "a session
     is open on <title>".
   - **The target is itself a Checkup.**

   An open Checkup on the target is not a refusal: the launch attaches to it or
   resumes it (ADR 0272 §2).
5. **Beside `x`.** `c` never changes what `x` does, and both hints can show on
   the same row. A `c` refusal names `x` whenever `x` is the way forward.

## Considered Options

- **The verb files and prints the id, as `surgery new` does; `enter` launches.**
  Rejected. The target would sit fenced with no session until someone pressed
  `enter`.
- **Offer `c` only on rows that look faulty (paused, stuck, dead claim).**
  Rejected. Most graph-shape and content faults don't look faulty, and a
  container that drained wrongly is closed.
- **Refuse every dead claim and send the human to recover.** Rejected. It would
  lock out the stuck states that recover refuses or can't classify, which are
  in the Checkup's scope.
