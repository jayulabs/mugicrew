# Bd store mode

Cap > 1 wants a server-mode bd store. Each human runs `mugicrew init` in their own
clone, which creates one, and `mugicrew doctor` FAILs on an embedded store.

To move an existing embedded store to server mode, from the Workspace root:

1. Stop the loop.
2. `bd backup init <dir>` and `bd backup sync`.
3. Move `.beads/embeddeddolt/` aside.
4. `bd init --server --prefix <p> --stealth` with the same prefix (`mugicrew init`
   would skip it, since the leftover `.beads/metadata.json` still resolves).
5. `bd backup restore --force <dir>`.
6. Verify with `bd count`, `bd dolt status` and a fast `bd show <id>`.

bd autostarts a detached per-repo Dolt server on the first call, and all linked
worktrees share it. mugicrew neither starts nor stops it, and works on embedded
stores too.
