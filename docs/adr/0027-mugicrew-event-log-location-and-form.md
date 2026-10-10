# ADR 0027: the event log is one slog JSONL file under git-common-dir

Status: Accepted
Date: 2026-09-29

Chart: Mugicrew session logs tab. Settles the grilling "Where the
event log lives and in what form". Builds on ADR 0033 and
research on event-log emitters and concurrent append safety.

## Context

Events come from several processes at once: the TUI, `mugicrew loop`, and
`mugicrew reconcile` subprocesses. The reconcile subprocesses run with their
cwd in a `/tmp/mugicrew-wt-*` worktree, so any path relative to `RepoRoot`
would split the log across worktrees. The research found that concurrent
O_APPEND writes of single-`write(2)` JSONL records are safe on this ext4 without
a lock, as long as each record stays under about 64 KiB and readers skip lines
they cannot parse.

## Decision

1. **Location:** `$(git rev-parse --git-common-dir)/mugicrew/events.jsonl`,
   resolved to an absolute path. Every worktree resolves to the same file. It
   is per-repo, git never commits it, it needs no `.gitignore` entry, and it
   survives worktree removal.
2. **One file**, which every emitter opens with `O_WRONLY|O_APPEND|O_CREATE`.
   It is not split per process. Splitting over time is left to rotation.
3. **Form:** stdlib `slog.JSONHandler` JSONL with the default keys (`time`,
   `level`, `msg`). `msg` holds the event name (e.g. `effort.started`). The
   attribute set is decided by ADR 0029.
4. **Levels on disk:** Info and above by default. Debug is written only when
   `MUGICREW_LOG_LEVEL=debug`. The Logs tab hides Debug by default even when
   the file contains it.
5. **Unopenable path** (outside a git repo, read-only `.git`, …): the command
   does not fail. Events are discarded, with a one-time warning (stderr for CLI
   commands, a status-line note in the TUI). The log is non-authoritative
   (ADR 0033), so it must never block real work.

## Considered and rejected

- **XDG state dir** (`~/.local/state/mugicrew/<repo-key>/`): needs an invented
  repo key, and the history is orphaned if the repo moves.
- **Gitignored `.mugicrew/` in the main checkout**: finding the main checkout
  still requires resolving git-common-dir first. It also needs a `.gitignore`
  entry and clutters the shared `main` tree.
- **Per-process files**: the tab would have to merge-sort several files, and
  the research shows the shared file is safe without them.

## Consequences

- The log lives inside `.git`, so it is out of sight and a fresh clone starts
  with an empty history. Both are accepted, because the log is per-machine
  post-mortem history.
- The drvfs/9p (`/mnt/c`) filesystems are untested. A repo on such a
  filesystem would put the log there.
