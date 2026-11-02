# Upgrading

`mugicrew upgrade` moves an npm global install onto the latest `@jayulabs/mugicrew`
and then runs the new binary's `mugicrew doctor`, so any human step the release
needs shows in its To-fix list. It refuses while this Workspace's loop is running
(stop it with `mugicrew loop stop`). A loop in another Workspace that runs the same
shared global binary is not detected: stop every such loop first. When the install
is not an npm global one, it changes nothing and prints what to run.

In the TUI, while the tab bar shows the Release notice (`↑ v<latest>`), the Loop menu
(`o`) has a `u` upgrade option, and clicking the notice opens the same action. It asks
y/n, stops a running loop first and waits for it to finish draining, then upgrades and
runs doctor. The status line then says to quit and restart mugicrew, with doctor's
summary. For any install that is not an npm global one, it shows the command to run
and stops nothing.

## Unattended Upgrade

A loop can Upgrade an npm global install itself. It is off until you opt in, once
per machine: `[upgrade] unattended = true` in the personal layer
(`~/.config/mugicrew/config.toml` on Linux), or `MUGICREW_UNATTENDED_UPGRADE=1`,
which wins over the key (`0` turns it off). A Workspace's `config.toml` has no
effect on it. See [configuration](configuration.md#the-personal-layer).

The loop upgrades only when all of these hold:

- It runs an npm global install (not `npm i -D`, pnpm/yarn global, npx or a
  from-source build), and the Release notice is not turned off.
- A newer strict release exists in the 24h release check. A release that declares
  a human step, and every release past it, is skipped; a safe release before it
  still installs.
- The loop is naturally idle: no effort in flight and no live session in its
  Workspace. After 24h of waiting it stops dispatching and drains until idle. It
  never kills a session, HITL included. `mugicrew loop stop` during the drain wins.

It then installs that exact version, runs the new binary's `mugicrew doctor`, and
restarts onto it, keeping the loop lock. The loop's event log shows
`upgrade.skipped`, `upgrade.waiting`, `upgrade.draining`, `upgrade.installing`,
`upgrade.failed` and `upgrade.doctor`. The TUI shows the Draining state while it
waits, and a one-time line, `loop upgraded vX→vY; restart mugicrew to run it here`,
once the binary was swapped by any loop on the machine. Quit and restart mugicrew
to run it. Every loop on the machine, opted in or not, detects a swapped binary,
stops dispatching, drains and restarts onto it.

### Recovering

- **npm failed.** The loop keeps running on its current version and logs
  `upgrade.failed`; `loop status` and the TUI loop menu show the failed version,
  and the loop doesn't retry it until a newer release appears. Run
  `mugicrew upgrade` yourself to retry, or leave it.
- **Doctor tripped.** The loop stops without restarting. The loop row and status
  line show why. Run `mugicrew doctor fix`, then start the loop again.
- **A loop never upgrades.** The release cache file is shared across installs. An
  older mugicrew (say a ProjectLocal install in another repo) that refreshes it
  writes it without the human-step markers, so an opted-in loop reads the fresh
  cache as current and waits for the next daily refresh before it sees a release.
- **A swap restart waits forever.** If the binary at the startup path never
  answers a strict release (a pre-release, or a non-npm build), the loop drains
  and logs `swap.waiting` with the path and its answer. Install a release there,
  or stop and start the loop.

## Upgrading onto the first swap-surviving release

Loops on older releases can't survive a binary swap, and nothing can see them, so
the first release that survives it declares a human step and never installs
unattended. Upgrade by hand and restart every loop:

1. Run `mugicrew upgrade` (stop this Workspace's loop first).
2. In every other Workspace on the machine, stop its loop and start it again, so
   it runs the new binary. Quit and restart any open TUI.

Unattended Upgrades are safe from the next release on.

## Upgrading across the Errand rename

The Task kind is now the Errand kind (ADR 0354, ADR 0355). Upgrade in this order:

1. Stop the loop (and quit the TUI's dispatch).
2. Upgrade: `npm i -g @jayulabs/mugicrew`.
3. Run `mugicrew doctor fix`. It relabels every `kind:task` bead `kind:errand` in
   place, closed beads included.
4. Restart the loop.

Until step 3 has run, `mugicrew loop start` and the TUI's dispatch refuse, and their
error names `mugicrew doctor fix`. Three more things now fail loudly and name their
new form:

- `mugicrew task new` fails; use `mugicrew errand new`.
- A `task: <id>` adopt line in a Spec fails the slice; write `errand: <id>`.
- A `## Tasks` heading in an open Spec fails the slice; write `## Errands`.

Self-hosting: when this change lands in the Workspace that develops mugicrew, the
loop refuses on restart until a human runs `mugicrew doctor fix` over that
Workspace's 29 `kind:task` beads. That refusal is the designed cutover.
