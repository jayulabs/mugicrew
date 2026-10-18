# ADR 0240: A claim held elsewhere refuses launch and x by holder, and never attaches across hosts

Status: Accepted
Date: 2026-10-16

Chart mc-wec4 (a cut session claims its Initiative). Settles grilling mc-wec4.4.
Extends ADR 0239 (a-cut-sessions-claim-on-its-initiative-starts-at-launch…) §5,
which fixed "x recovers it" for the no-session case only. The claim path
(`hitlClaim` in `launchNow`) and the Graph's `launchWhy`/`launchPreview` are
shared by every HITL kind, so this applies to all of them, not only Initiatives.

## Decision

1. **A stale-snapshot claim refusal is a refusal, one line, then a reload.**
   `launchNow` returns it with `refused: true`, not as a launch failure, and the
   TUI reloads. It reads bd's two refusals:
   - held by another actor: `<id> is claimed by <A> — not launched`
   - held by this actor (CAS status mismatch):
     `<id> was claimed since the last refresh — not launched`
2. **Another actor's claim on the Graph** (assignee ≠ this TUI's bd actor):
   enter and the preview say
   `<id> is claimed by <A> — not yours to launch; bd unclaim <id> --force once <A> is gone`.
   `xLabel` offers nothing for it, and an `x` press refuses with the same line.
3. **This actor's claim with no local session** (or an empty assignee):
   `<id>'s claim has no session here — x recovers it if it's stranded`. A
   same-actor session live on another machine can't be told from a stranded one;
   that stays the known limit of ADR 0126 (chopper-recovers-stranded-beads…).
4. **The cross-actor guard stays.** `x` drop and `mugicrew recover` never pass
   `--force` to `bd unclaim`. Releasing another actor's claim is the human's call,
   by hand, with the command the refusal names.
5. **No attach across hosts.** enter never attaches to a session held by another
   TUI, tmux server or machine; it refuses as above.

## Considered Options

- **Keep bd's raw multi-line stderr behind `launch: claim:`.** Rejected: it reads
  as a crash, not a held claim.
- **One generic "already claimed" line.** Rejected: the holder's name tells the
  human whether the claim is theirs to recover.
- **Recover / x pass `--force` after a confirmation.** Rejected: another actor's
  claim is someone else's work; mugicrew doesn't override it.
- **Attach to a reachable remote session.** Rejected: there is no cross-host
  session channel; building one is separate work.
