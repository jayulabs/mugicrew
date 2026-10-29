# GitLab land strategy

`mugicrew init workspace --land-strategy gitlab` writes `land_strategy`; the TOML
below is the reference for hand edits.

Under `land_strategy=gitlab`, everyone who lands on the shared mainline needs
their own `issue_prefix` (`mugicrew init --prefix <p>`, or `bd rename-prefix <p>`).
Two Bead Graphs with one prefix mint the same bead ids, so `loop start` refuses
when another graph already lands with yours, and a colliding Landing holds its
container on an Errand (ADR 0187).

Set it up once:

1. `land_strategy = "gitlab"` in the layered config.
2. `glab auth login --hostname <host>`. mugicrew stores no token; it runs
   `glab api` with your login and fails loudly if glab is missing or logged out.
3. In the GitLab project, set **Squash commits** to Require (every other
   setting is refused) and the **squash commit template** to `%{title}`, a blank
   line, then `%{description}`. The template carries the `<Kind>: <id>` trailer
   from the MR description onto mainline. `loop start` and the first push or MR
   create refuse a project that would drop it.

The Forge is the remote `branch.main.remote` names, else `origin`; the host and
project come from that remote's URL (ssh or https).

Nested repos (`[context.<name>] repo = <dir>`) land through their own GitLab
project, resolved the same way from the child's remote, which must be on the
Workspace's host. `land_strategy` is read from the Workspace only, so every
declared nested repo is on gitlab when the Workspace is. `loop start` runs the
auth, fetch, squash-settings and foreign-Graph checks once per distinct project
(the Workspace's, then each child's) and refuses with the context's name when a
child fails, has no parseable remote, or is on another host.

mugicrew opens the MR and never merges or arms auto-merge. You merge it on the
Forge, or arm auto-merge yourself. The MR's pipeline, not the local nix gate, is
the land gate (ADR 0150, ADR 0189).

## Bead Graph data and project remotes

A Bead Graph never goes to a project remote: no mugicrew path pushes it, and
mugicrew deletes no remote ref (ADR 0300). Back up or sync a graph only to a
destination you name, with `bd backup` or `bd sync`.

- On a user-named git remote bd creates a `__dolt_remote_info__` marker branch
  (`refs/heads/__dolt_remote_info__`). Set `DOLT_REMOTE_INFO_BRANCH=` (empty) in a
  shell export or `.env` line to suppress it. An existing
  `DOLT_REMOTE_INFO_BRANCH=` line in `.beads/.env` is harmless and may be deleted.
- Refs an older mugicrew pushed are yours to delete: find them with
  `git ls-remote <forge> 'refs/mugicrew/graphs/*'`, then
  `git push <forge> --delete refs/mugicrew/graphs/<you>`. For the old GitHub
  sync, delete `refs/dolt/data` and, if present, `refs/heads/__dolt_remote_info__`.
