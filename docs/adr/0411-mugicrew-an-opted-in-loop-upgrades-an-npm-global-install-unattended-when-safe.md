# ADR 0411: An opted-in loop upgrades an npm global install unattended when safe

Status: Accepted
Date: 2026-10-30

## Context

The glossary said an Upgrade runs "only when the human asks" and the Release
notice "only tells". A loop that runs AFK for days therefore keeps running old
code until the human notices. This ADR amends that human-asks rule for one case.
The swap hazards it creates are settled by ADR 0406
(mugicrew-loops-sharing-a-global-binary-survive…): every loop re-execs through
its startup path, detects a swapped binary and drain-restarts onto it.

## Decision

1. **Loop-only, NpmGlobal-only.** Only the loop process upgrades unattended,
   never the TUI, a CLI verb or a crewmate session. Only an install that
   `release.Classify` reads as NpmGlobal (from the startup path) qualifies;
   ProjectLocal, pnpm/yarn global, npx, unknown and from-source installs keep
   the Release notice only, and a Watched loop stays the only way a from-source
   build moves.
2. **Opt-in through the personal layer plus env.** `[upgrade] unattended = true`
   in `<os.UserConfigDir()>/mugicrew/config.toml`, or
   `MUGICREW_UNATTENDED_UPGRADE` (`1`/`on`/`true`, `0`/`off`/`false`), which wins.
   Workspace layers are never read for it, so a repo can't opt a machine's
   binary in. Opting out of the Release notice (`MUGICREW_RELEASE_NOTICE=0`,
   `[release] notice = false`) opts out of this too.
3. **The marker and crossing rule.** A release declares that it needs a human
   step with `mugicrew.humanStep` in the launcher's `package.json`, honoured
   only when its `version` equals the enclosing version's. The 24h release check
   reads the full packument, so the cache (`release.json`) also holds `versions`
   and `human_step`. The target is the highest strict release above the running
   version with no marked release in `(running, target]`. A marked release and
   everything past it waits for a human `mugicrew upgrade`; a safe release
   before it still installs. A cache without `human_step` counts as unknown, and
   the loop waits for the next refresh.
4. **Natural idle, with a 24h cap.** The loop installs when no effort is in
   flight and no session is live in its Workspace's tmux server; a failed
   session-table read is not idle. After 24h of waiting it stops dispatching and
   drains until idle, never killing a live session (HITL included). A stop
   during the drain wins.
5. **npm failure versus doctor trip.** If npm exits non-zero, the loop keeps
   running on its current binary and records the version as failed
   (`UpgradeFailed` in the lock body); it is not retried unattended until a newer
   target appears or a human `mugicrew upgrade` succeeds. If npm succeeds, the
   loop runs the new binary's `doctor`. Clean, it restarts through ADR 0406's
   swap restart. A non-zero exit or a To-fix count above zero stops the loop
   without restarting, with `StoppedFor` naming `mugicrew doctor fix`, which the
   TUI shows until the next loop start.
6. **Audit.** The event log records `upgrade.skipped`, `upgrade.waiting`,
   `upgrade.draining`, `upgrade.installing`, `upgrade.failed` and
   `upgrade.doctor`, then the swap restart's `swap.detected`, `swap.ready` and
   `loop.restarting`.

## Considered Options

- **Upgrade from the TUI or a CLI verb too.** Rejected: a short-lived command
  must never swap the binary under the human.
- **Opt in through a Workspace config key.** Rejected: one repo could opt in a
  whole machine's shared binary.
- **Skip a marked release but install later ones.** Rejected: a step such as
  `mugicrew doctor fix` would be skipped by a machine.
- **Kill live sessions after the cap.** Rejected: pausing dispatch and draining
  reaches idle without losing work, and ADR 0090 (mugicrew-watched-mode) already
  sets that restart-drain precedent.
