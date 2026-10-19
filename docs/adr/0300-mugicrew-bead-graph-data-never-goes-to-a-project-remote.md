# ADR 0300: Bead Graph data never goes to a project remote

Status: Accepted
Date: 2026-10-18

Spec mc-g5jo8. Supersedes ADR 0216, ADR 0217, ADR 0218, ADR 0256 and ADR 0257. Amends ADR 0235 §1.

## Decision

1. **No mugicrew path pushes Bead Graph data to a project remote.** A graph leaves
   the machine only as a `bd backup` or `bd sync` to a destination the user names.
   mugicrew deletes no remote ref. The Graph ref, `graph push`, `graph fetch`, the
   Scratch graph and the code-repo `sync.remote` are gone.
2. **`DOLT_REMOTE_INFO_BRANCH=` is no longer written** by `mugicrew init`; an
   existing key is left alone. The Workspace flake no longer exports it.
3. **Still live, as the Land push's credential** (the Forge calls mugicrew makes
   for a gitlab Landing), restated here so live code cites this ADR, not the
   superseded ones (provenance: ADR 0256 §1, §3, §4; ADR 0257):
   - glab's helper is injected into every git call to the Forge, with no global
     credential entry: only `credential.https://<host>.helper=!glab auth git-credential`,
     never `credential.<url>.username` (that trips glab's username-mismatch refusal
     under `GITLAB_TOKEN`; glab's user must be set). mugicrew ships no helper of its own.
   - A second account needs only `GITLAB_TOKEN` exported in the clone's shell.
   - No token goes in the remote URL.
   - `mugicrew doctor` keeps its single FAIL credential line: `git credential fill`
     under that injected helper, run from outside the repo.
4. **Dropped:** ADR 0256 §2, the sql-server restart.

## Consequences

- ADR 0245 §5's reason is stale and ADR 0200 §2's sync-ref clause is vacuous; both
  are left unedited as history.
- Handoff of a graph between users is not supported by design.
