# ADR 0288: mugicrew's tab bar shows a loop state word, and `o` opens a loop menu

Status: Accepted
Date: 2026-10-17
Amends: ADR 0051 §1, §3–§4, ADR 0063 §2–§5, ADR 0090 §7

Chart: A human restarts the loop onto main's latest build without stopping it.
Settles the grilling "How status and the TUI show an on-demand restart and a
failed build on an unwatched loop". The prototype "What the loop menu looks
like in each loop state" approved its screens and settled the gaps in §1, §4
and §5 (side ref `refs/mugicrew/prototypes/mc-9j2q`).

## Context

An on-demand `loop restart` works on any from-source loop, watched or not.
ADR 0090 §7 shows the restarting and stale displays only on a watched loop,
and an unwatched loop never sets `failed_rev`. A restart from the TUI is
shelled out, so a human may never see its exit code. A restart action would
also be one more key on the loop surface. The tab bar has grown a rev, an
in-flight count, a reset time and a narrow variant.

## Decision

1. **Restarting and stale apply to every from-source loop (amends ADR 0090
   §7, ADR 0063 §2–§3).** `restart_to` alone tells a restart drain apart from
   a stop drain. `failed_rev` is set when an on-demand build fails, on an
   unwatched loop too. It means "a build of main that was asked for failed,
   so the loop is still on the old build". It never means "behind main". An
   unwatched loop is never stale just because main moved. `loop status`
   keeps its detailed lines (ADR 0063 §3) and drops the watch gate. A
   limited loop adds the stale line too, as an on loop does, to match the
   `loop limited stale` word.
   > **Amended (2026-10-18, ADR 0295 (mugicrew-loop-status-prints-labelled-fields…)):** `loop status` no longer keeps ADR 0063 §3's detailed one-line shape. It
   > prints labelled fields in sections, and the stale line sits in its own
   > stale section (limited loops too).
2. **What clears an unwatched loop's stale flag.** The next `loop restart`
   clears it. That restart either builds and drains (the exec drops the
   flag), or finds the running build current (it rewrites `rev` and clears
   the flag), or fails again (the flag moves to the new rev). A stop and a
   start also clear it. There is no timeout and no acknowledge.
3. **The tab bar shows only a state word (amends ADR 0051 §4, ADR 0063 §4).**
   The words are `loop off` (dim), `loop on` (green), `loop limited`,
   `loop draining`, `loop restarting`, `loop on stale` and
   `loop limited stale` (all warn), plus `loop starting…`. There is no rev,
   no `@latest`, no `@old→new`, no in-flight count and no reset time, so
   there is no narrow (`withRev=false`) variant either.
4. **`o` opens a loop menu (amends ADR 0051 §1, §3, ADR 0063 §5).** The menu
   replaces the `o` toggle, the inline force confirmation and the three-way
   drain dialog. Its body is the `loop status` text, rendered by the same
   formatter, so the CLI and the TUI can't drift apart. Its options depend
   on the state:

   | State | Options |
   |---|---|
   | off | `s` start · esc |
   | on / limited (stale or not) | `r` restart · `s` stop · esc |
   | restarting | `s` stop (converts the drain to a stop drain) · esc |
   | stop drain | `k` keep on (cancels the drain) · `f` force stop · esc |
   | installed (not from source) | `s` stop · esc; no `r`, and a dim line says it is not from source |
   | starting… | details only · esc; under the formatter's bare `off` (the lock is not yet written), a dim line reads `waiting for the loop to write its lock…` |

   Picking `f` in the menu is the confirmation, because opening the menu was
   already a deliberate step. `r` has no confirmation (a restart drains and
   never loses work). Every option shells out, as `o` does today.
   > **Amended (2026-10-18, ADR 0295 (mugicrew-loop-status-prints-labelled-fields…)):** the formatter has no bare `off` text beyond `state: off`; the starting
   > row's dim line sits under that. The shared-formatter principle stays: the
   > menu body is still the same formatter's output, now styled from its
   > structured lines.
5. **`loop restart` output:**

   | Outcome | Message | Exit |
   |---|---|---|
   | already current | `loop is already on main abc1234` | 0 |
   | build failed | `build of main def5678 failed (see build.failed in events)` | 1 |
   | restart drain started | `loop restarting onto main def5678 (pid N, 2 in flight)` | 0 |
   | restart already draining | `loop is already restarting onto main def5678 (pid N, 2 in flight)` | 0 |
   | stop drain | `loop (pid N) is stopping; loop start cancels the stop drain` | 1 |
   | installed | `loop (pid N) is not from source; stop and start it to change builds` | 1 |
   | off | `loop is off; loop start runs main's latest build` | 1 |
   | old build (no ack) | `loop (pid N) did not answer the restart request (an old build); stop and start it` | 1 |

   Off is an error, unlike `loop stop`. For a stop, off is the goal state.
   For a restart, nothing restarted.

## Considered options

- **Keep the watch gate:** rejected. A plain `draining` reads as if someone
  pressed stop (ADR 0063), and a failed build from the TUI would vanish.
- **Report a failed on-demand build only through the CLI exit:** rejected for
  the same reason. The TUI doesn't show the exit.
- **A dedicated restart key next to `o`:** rejected. The menu holds every
  loop action in one place, with the details that a human needs in order to
  pick one.

## Consequences

- An older loop never sets `failed_rev` on an unwatched loop, so a new TUI
  shows it as plain `loop on`. That's degraded but never wrong.
- The rev a loop runs moves out of the bar and into the menu and
  `loop status`.
