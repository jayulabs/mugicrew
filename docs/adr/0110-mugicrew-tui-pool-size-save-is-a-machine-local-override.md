# ADR 0110: mugicrew TUI pool-size save is a machine-local override, never a commit

Status: Accepted
Date: 2026-10-09

Amends the Spec's FR2 and FR12. Completes ADR 0109 §6 (save feedback). Part of
the TUI Settings page Chart. Decided in grilling, using research.

## Decision

1. **A TUI save writes `<git-common-dir>/mugicrew/config.toml` and commits nothing.**
   The file sits beside `land.lock` and `loop.lock`, outside every worktree. So it
   never dirties main (`*MainDirty`, ADR 0069 §6), and it needs no ignore rule. It is
   shared by every linked worktree of the repo, and it is machine-local. The write
   is atomic (temp file + rename), rewrites the whole file as `concurrency = N`, and
   creates `<git-common-dir>/mugicrew/` if needed.
2. **`Pool` layers the files: local first, then the checked-in
   `.mugicrew/config.toml`, then 3.** The checked-in value becomes the Workspace's
   *shared default* (amends FR2). Each layer is re-read on every call (FR4 stays).
   An unusable layer (malformed TOML, a non-integer, or `< 1`) logs `config.unusable`
   and falls through to the next layer, not straight to 3 (amends FR12).
3. **The change applies on the loop's next tick and run-now's next press.** There is
   no pending state to report.
4. **The Settings row names the layer.** Its source is `local`, `config.toml`,
   `default`, or `unusable → <fallback>`. When a local value overrides a checked-in
   one, the row also shows the shared value: `AFK pool  5  (local; shared 3)`.
   This replaces ADR 0109 §2's source list.
5. **Save feedback.** On success the status line reads
   `AFK pool 5 saved (this machine)`, the staged marker clears, and the refresh shows
   the new row. On failure it reads `AFK pool save failed: <err>`, and the staged
   value is kept so `enter` can retry.
6. **There is no reset key.** To go back to the shared value, delete the local file
   by hand; the `shared N` gloss shows what you will get.

## Why

The pool size budgets *this machine's* agents and model spend (FR1). That makes it a
live dial for one human, not a fact the repo shares. The checked-in value is still
the right home for a shared default. The override is about 5 lines in `Pool`. It
can't block a land, and it takes effect immediately.

## Considered Options

- *Commit to main with plumbing, shaped like Anchor (`commit-tree` +
  `ffMainline` under the land lock).* Shared and immediate, but it is about 20
  new lines in `git/`. It bypasses the spine (no Spec, no review), it would need
  a History trailer, and it fails whenever main is dirty or the lock is held.
- *Write onto a branch that a Checkpoint carries to main.* Correct by the spine,
  but the value would apply only after a land, so "change it now" no longer holds.
- *A git-ignored `.mugicrew/settings.local.toml` in the worktree.* On a foreign
  Workspace mugicrew would have to add the ignore rule. A `.gitignore` edit is a
  commit, and an untracked `.mugicrew/.gitignore` is itself dirt. A file in the
  common dir avoids both.
