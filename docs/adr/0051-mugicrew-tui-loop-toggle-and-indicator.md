# ADR 0051: mugicrew TUI loop toggle (`o`) and tab-bar loop indicator

Status: Accepted
Date: 2026-10-04

Amends: ADR 0024 §3 (tab bar kept verbatim) and ADR 0026 (view keys gain `o`).

Chart: Loop lifecycle control. Decided in grilling, on
top of ADR 0050 (stop drains; states off / on / draining read from the
lockfile).

## Decision

1. **`o` toggles the loop.** It is a **view key** (ADR 0026), present on every
   tab. Its label tracks state: `o loop on` (off), `o loop off` (on),
   `o force-stop` (draining). A stray press is safe: start is harmless and
   stop only drains.
2. **The TUI shells out, never owns.** `o` runs `mugicrew loop start`,
   `mugicrew loop stop`, or `mugicrew loop stop --force`, exactly as the CLI
   does. The status line shows `starting loop…` / `stopping loop…` until the
   next read confirms. A CLI error (e.g. "loop is draining") goes to the
   status line verbatim.
3. **Force confirms inline.** Force while draining reuses ADR 0026's
   blocked-launch confirm edit mode: status line `N efforts in flight will be
   abandoned: <title>…, <title>… +N — force stop?`, footer
   `y force · n/esc cancel`. Only `y` forces. A draining loop always has an
   effort in flight, so every TUI force confirms (ADR 0050 §3).
4. **State lives in the tab bar**, visible on every tab:
   `Graph Inbox(N) Logs  · N live · loop on|off|draining N`. Colour: dim off,
   green on, warn draining. Draining titles appear only in the force prompt.
5. **`N open HITL` moves into the Inbox label** as `Inbox(N)` (same count:
   open HITL beads), freeing width for the loop segment at the 40-col floor.
   `N live` stays. Too wide for the terminal (a draining bar at 40 cols) first
   drops the tab padding, then `N live`; the loop segment always stays.
6. **Refresh reuses the existing 3s tick** plus an immediate read after `o`.
   Each read checks that the lock is actually held, so a lockfile left by a
   killed loop reads `off`. No file watch.
7. **Footer position: just before `r`.** e.g. Graph
   `[tab detail] · space fold · h hide-closed · o loop on · r · q`. Narrow
   overflow may drop it; the tab-bar state remains.

## Considered options

- `p` (pause): misleading, since stop drains rather than pauses. `s`/`S` and
  a shifted `O` were also considered. `o` won because it reads as on/off and a
  stray press is safe.
- Indicator in the footer or status line: the footer is commands, and the
  status line is overwritten by every message.
- In-process loop forked by the TUI: breaks "TUI toggles, never owns".
- inotify watch on the lockfile: the 3s tick already exists.

> **Amended (2026-10-04): quit confirms when run-now efforts are
> in flight.** Run-now (`t`) efforts are goroutines inside the TUI, so quitting
> strands their claims `in_progress`, skips worktree teardown and drops the
> post-session steps, which is the same loss as a loop force stop. Once the
> loop drains (ADR 0050) and the TUI never owns it (§2), an unconfirmed `q`
> would be the only lossy exit. So:
>
> - The Model counts in-flight run-now efforts. The count goes up when a claim
>   succeeds and down on `runDoneMsg`.
> - **`q` with N > 0** reuses the §3 / ADR 0026 confirm edit mode. Status line:
>   `N run-now efforts will be abandoned: <title>…, <title>… +N — quit?`.
>   Footer: `y quit · n/esc cancel`. With N = 0, `q` quits at once as before.
> - **`ctrl+c` behaves like `q`.** A second `ctrl+c` while the confirm is
>   showing counts as `y`: the same "second signal forces" rule as the loop
>   (ADR 0050).
> - A confirmed quit writes one `effort.abandoned` per effort (ADR 0029, as
>   amended). Writing is best-effort and never blocks the quit.
> - If efforts finish while the prompt is up, the prompt stays and `y` just
>   quits. The 3s tick does not rewrite it.
> - Closing the terminal, SIGHUP or SIGKILL still strand claims with nothing
>   logged. Recovering them is the Chopper Signal's job (ADR 0050 §6).
>
> Rejected: wait-then-quit (a drain, as in ADR 0050). It adds a quitting state
> and can hang for as long as an effort runs. Also rejected: `ctrl+c` as an
> immediate escape hatch, because it would make the most reflexive key the
> lossy one.
