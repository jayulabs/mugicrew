# An AFK claim no live host reads is an orphan the pool skips and recover repairs with the loop on

Status: accepted

A loop that crashes or is SIGKILLed leaves its claims `in_progress` and writes
no `paused:abandoned`. `CountInflight` still counts each of them, so the AFK pool
shrinks for good, and Judge reads each one as ambiguous while the loop lock is
held. The only way out was to stop the loop, `recover --force` each bead and
restart, which drops live efforts. ADR 0154 (the-dispatcher-holds-a-per-effort-flock…)
already gives every dispatcher a flock from before the claim until after the
release, and the kernel frees it when the holder dies. That flock settles the
question the loop lock stood in for. This ADR amends ADR 0127
(recover-repairs-every-leftover…) §2 and ADR 0155 (abort-and-recover-honour…).

- **One orphan reading.** An `in_progress` AFK claim is an **orphan** when three
  checks all come back empty: its per-effort flock is free, the argv probe finds
  no live process, and the cwd scan finds no process inside its tree. A
  repo-root claim skips the cwd scan, because the repo root is everyone's cwd.
  The reading is computed once per tick, in the same `/proc` pass the argv scan
  already makes. `CountInflight`, `mugicrew ps`, the TUI and Judge all use it.
  The reading takes no claiming pid from `.git/mugicrew/events.jsonl`. A verdict
  never reads history (ADR 0033), and a pre-flock claim could only come from a
  dead pre-flock binary, so reading it as an orphan is correct.
- **The pool skips orphans.** An orphan takes no AFK pool slot. That covers
  repo-root orphans too, because the pool is a budget, not a safety rule.
- **A worktree-route orphan is dead.** Judge returns Dead for an AFK orphan on a
  worktree or detached route, whether or not it is paused abandoned and
  whatever the loop lock and the TUI-alive read say. For AFK beads, `LockFree`
  and `TUIAlive` drop out of the verdict.
- **A repo-root orphan still needs `--force`, but not the loop off.** A hostless
  claude or an orphaned tool may still be writing (ADR 0127 §2), so the
  `--force` requirement stays. `--force` no longer needs the loop off when the
  bead's flock is free. The Held check runs before Force, so a bead its
  dispatcher hosts still refuses.
- **The loop doesn't repair orphans.** It only stops counting them. Repair stays
  with the human's `recover` (ADR 0126), because it can discard a dirty tree and
  Drafts.
- **The human sees why the pool is short.** The TUI pool readout counts orphans
  separately (e.g. `3/5 · 2 orphaned`). Each orphan row carries a marker that
  points to `x` (recover). `mugicrew recover` with no arguments lists the
  orphans, and the `liveness` column of `mugicrew ps` reads from the shared
  reading.

## Considered options

- **Also require the claiming pid in events.jsonl to be gone.** Rejected: that
  reads history for a verdict, and the claims it would protect can only come
  from a dead binary.
- **The pool reads probe and flock only, and Judge adds the cwd scan.**
  Rejected: an orphaned tool grandchild in the tree would free a pool slot while
  recover still refused the bead.
- **The loop marks orphans `paused:abandoned` or recovers them itself.**
  Rejected: marking adds a write and no safety, since an `in_progress` bead is
  never re-dispatched. Recovering on its own would discard work without the
  human's `--discard`.

Decided on Survey mc-hwril.1 (Chart mc-hwril).
