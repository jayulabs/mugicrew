# ADR 0121: mugicrew's Stock cutover is one code Checkpoint, then three chained Tasks

Status: Accepted
Date: 2026-10-10

Amends: ADR 0120 (the module path literal is the vanity `jayulabs.org/mugicrew`)

## Context

Converting mugicrew to a Stock under Lineage changes main several times while
the pinned dispatch loop (ADR 0060) runs from it. The loop restarts itself onto
any build whose outPath changes (ADR 0062), but its frozen env has no `lineage`
until it runs the A1-wrapped build. Hook shims that call `lineage` would block
every git write by a caller without it. Decided in grilling
(Chart "mugicrew as a Stock under Lineage, synced and published").

## Decision

1. **One Checkpoint carries only code:** `land-lineage` (ADR 0119),
   `lineage sync --base`, the Stock-owned flake (ADR 0120), and the A1 wrap of
   `#mugicrew` plus the devShell with `lineage`. No Task holds its land.
2. **Everything else is three Tasks (ADR 0116 bodies), chained by blocks
   edges.** They are Shims → Seed and first Sync → First Publish, and the
   first is blocked on the Checkpoint's land.
   - **Shims:** verify the loop restarted onto the wrapped build, then restart
     the TUI from a fresh devShell. Then file a Surgery (ADR 0091) that adds
     strict shims to `.beads/hooks`, and re-verify once it lands. The shims
     never share a land with the wrap.
   - **Seed and first Sync:** run `lineage stock`. In a `stock/mugicrew`
     worktree, the human drives an ad-hoc agent session that does the leak
     cleanup and renames the module path to `jayulabs.org/mugicrew` (checked
     by the denylist grep and the standalone build). Then run
     `lineage sync --base S0` (ADR 0090). Drain the loop (`loop stop`), run
     `land-lineage`, then `loop start`, then verify.
   - **First Publish:** create the repo [irreversible], set the remote and the
     `jayulabs` identity, and deploy a Cloudflare Worker on
     `jayulabs.org/mugicrew*` that serves the `go-import` tag (checked with
     `curl`). Then publish [irreversible]. The checks run on what went public:
     `ls-remote` equals the Projection tip, a fresh clone builds under nix and
     `CGO_ENABLED=0 go build ./...`, and the denylist grep is clean.
3. **The loop check after every move of main** has three parts: `mugicrew loop
   status` shows on, rev = main and not stale; `lineage` resolves on the PATH
   in the live loop's `/proc/<pid>/environ`; and a no-op `mugicrew integrate`
   exits 0, which runs the hooks.

## Considered Options

- **Tolerant shims (exit 0 when `lineage` is missing) riding the Checkpoint.**
  Rejected: any caller without lineage would silently skip Lineage's guards,
  forever.
- **A second Spec'd Checkpoint for the shims.** Rejected: the Chart has one
  Checkpoint, and a Surgery is the cheap door for a near-trivial diff.
- **A script for the rename, or routing Stock work now.** Rejected:
  this is one-off work.
- **Trusting the auto-restart across the first Sync.** Rejected: in-flight
  efforts on pre-rename main would conflict on about 102 renamed imports, so
  drain instead.
- **One long Task.** Rejected: the Surgery's review and the public, irreversible
  Publish are natural pauses.
- **`github.com/jayulabs/mugicrew` as the module path.** Rejected for the vanity
  path, which survives a move off GitHub.

## Consequences

The Worker must stay alive for as long as anyone resolves the module: the proxy
caches only versions it has already fetched.
