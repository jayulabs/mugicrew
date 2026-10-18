# Abort and recover honour the dispatcher hold

Status: accepted

ADR 0154 lets the dispatcher (the loop, or a TUI's run-now) hold a per-effort
flock from before the claim until after the release. Abort and Chopper's recover
do not read that lock. Both still use the argv and cwd scans, which see nothing
in the dispatcher's own windows: pre-cut, post-child (publish, retire,
deleteLanded, release) and the repo-root in-process bd work. As a result,
recover can reap a bead whose dispatcher is still alive, and abort reports "no
live process" for a live effort. A held flock counts as a live host for both.
This amends ADR 0083 §9 and ADR 0127 §2.

- **Recover refuses under a held flock, even with `--force`.** `Judge` gains a
  third alive check, after `Probe` and `Cwd` and before the Force case. A held
  flock refuses every verdict, `Forced` and `Dead` included, says the bead is
  held by its dispatcher (the loop or a TUI), and points to `mugicrew abort <id>`.
  `--force` exists to settle ambiguity about a claim with no host. A held flock
  is not ambiguous, because the OS reports the host as alive. Today `--force`
  with the loop off can reap a TUI run-now effort mid-publish, since `TUIAlive`
  is never consulted on that path. `Dead` can also fire in a stopping
  dispatcher's exit window, and `AmbiguousResolved` can fire under a TUI not
  named `tui-*`. This check closes all three. To get out of a dispatcher that is
  wedged but still alive, stop it or kill it. The kernel then frees the lock and
  recover proceeds, so nothing strands. `LockFree` and `TUIAlive` stay as they
  are for the ambiguity over a claim with no host.
- **Abort under a dispatcher-only hold pauses, never signals and never
  refuses.** It writes `paused:manual` as before, because the pause is what
  stops a re-dispatch (ADR 0083 §5). It reports that the dispatcher holds the
  bead with no child and that the effort stops or finishes at its next step. In
  ADR 0083 §9, "no live process" now means no child process and no held flock.
- **The dispatcher re-reads the pause before it spawns.** In `worktreeRun` this
  happens between `CutTree` and the reconcile child. On the repo-root route it
  happens before `crew.Run`. If the bead carries `paused:manual`, the
  dispatcher takes the existing failure path (delete the Drafts, tear down the
  tree, release), as it would for a SIGTERMed child. Without this check, an
  abort in the pre-cut window only sets a label, and the effort runs to
  completion anyway. After the child exits there is no re-read. Work that has
  already landed finishes and publishes under the pause, which is harmless.
- **Messages name no pid.** A flock does not record its holder. Writing the pid
  into the lock file would reopen ADR 0154's rule that lock files are never
  deleted or rewritten. The message still tells the human what to do without
  one.

## Considered options

- **Let `--force` override the flock.** Rejected. It would keep the TUI run-now
  hole open, only to serve a wedged dispatcher that stopping or killing it
  already handles.
- **Make abort refuse ("held by a dispatcher, retry").** Rejected. The pause
  would never be written, and the human would have to time a retry to land
  after the child spawns.
- **Make abort signal the dispatcher.** Rejected. Each spawn leads its own
  process group, so a signal to the dispatcher's group hits the loop or the TUI,
  and that kills every other effort the dispatcher hosts.
- **Re-read the pause before publish as well.** Rejected. Finishing work that
  has already landed is harmless, and the post-child window is short.

## Consequences

Each dispatched effort costs one more `bd show`, for the pre-spawn re-read. That
cost has not been measured against the loop's tick. Lineage:
`docs/research/abort-and-recover-under-a-dispatcher-only-hold.md`.
