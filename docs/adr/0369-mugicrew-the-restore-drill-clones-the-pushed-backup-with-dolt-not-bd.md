# ADR 0369: The restore drill clones the pushed backup with `dolt clone`, not a bd verb

Status: Accepted
Amended by ADR 0384 (a-restored-graph-runs-bd-migrate-schema…) (the drill recreates the dolt_ignore'd tables empty and checks show, ready and list).
Date: 2026-10-28

Chart mc-wmn0w (A user's Bead Graph backs up off-machine to a Dolt destination
they name). Settles grilling mc-wmn0w.7. Builds on ADR 0367
(mugicrew-a-bead-graph-backs-up-off-machine-by-dolt-push…).

## Decision

The documented restore drill (install.md's Backup section; a procedure, not a
mugicrew verb) brings the off-machine copy back with the `dolt` CLI, the same way
for file:// and a remotesapi:

1. Make a scratch dir (`mktemp -d`), `export BEADS_DIR=<scratch>/.beads`, and
   check `bd config get issue_prefix` before any write (the devShell exports the
   real `BEADS_DIR`).
2. Run the backup push, so the copy is current.
3. `dolt clone [--user $DOLT_REMOTE_USER] <backup url>`, move the clone into the
   scratch `.beads/dolt/<db>`, and copy the DB's `metadata._project_id` into
   `metadata.json` (else bd refuses with a project identity mismatch). Then
   `bd dolt start` followed by `bd migrate schema`.
4. Compare live `bd count` on the source with `bd count` on the restore, and
   check `bd show` of one bead, `bd ready` and `bd list`.
5. `bd dolt stop` in the scratch dir and remove it.

The drill states that the tables bd lists in `dolt_ignore` (`events`, the audit
trail, among them) come back empty and only their data is lost; one line notes
that the optional local file:// `bd backup` restores that data (`bd backup restore --force
<dir>`). It runs once right after the first push, then periodically, untracked:
doctor checks staleness, not drills.

## Considered Options

- **`bd init --remote <url>` (or `bd bootstrap`).** bd's own clone, but bd 1.3.0
  has no way to name a remote user, so a credentialed remotesapi is tried as
  root; it also persists `sync.remote`, and Dolt remote sync is out of scope.
  Unmeasured even for file://. A raw `dolt` step is acceptable here: the backup
  itself is already a raw `CALL DOLT_PUSH`.
- **`bd backup restore`.** Takes only a local backup directory, never a pushed
  remote.
- **Comparing against the source as of `remotes/backup/main`.** Exact without a
  fresh push, but needs a Dolt `AS OF` query; pushing first is simpler and also
  exercises the push.

## Consequences

- No upstream bd request is filed for URL restore or a remote user on
  `bd init --remote`; the drill does not need one.
