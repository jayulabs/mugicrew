# ADR 0295: `loop status` prints labelled fields in sections, and the loop menu colors them

Status: Accepted
Date: 2026-10-18
Amends: ADR 0050 (mugicrew-loop-stop-drains) §4, ADR 0060 (mugicrew-every-loop-runs-the-pinned-mainline-build) §5, ADR 0063 (mugicrew-loop-shows-restart-drain…) §3, ADR 0090 (mugicrew-watched-mode) §7, ADR 0288 (mugicrew-the-tab-bar-shows-a-loop-state-word…) §1, §4

Chart: Sectioned, colored loop status layout. The prototype "What the
sectioned, colored loop menu looks like in each loop state" approved the
screens (side ref `refs/mugicrew/prototypes/mc-j2na`).

## Context

Today `loop status` packs pid, rev, store path and in-flight titles into one
parenthesised line. In the narrow modal that line wraps into a block that is
hard to scan. One formatter still serves both the CLI and the menu (ADR 0288
§4).

## Decision

1. **The formatter returns structured lines (section, label, value).**
   `Text()` prints each one as `label: value`, unaligned, and puts a blank
   line between sections. There are no section headers. The TUI styles the
   parts from the structure and never parses the text.
2. **Sections, in order, each left out when it is empty:**

   | Section | Fields |
   |---|---|
   | state | `state: <word>`; on a restart drain `restarting onto: main <short>`; `pid: N` |
   | build | `main: <short rev>` (from-source only); `store: <path>` (the executable, also for an installed loop) |
   | in flight | `in flight: N`, the count alone, on every running loop |
   | limit | `held by usage limit until: <ResetLabel>`; `reset time from: message\|usage\|fallback (1h, no reset time found)` |
   | stale | `stale: build of main <short> failed (see build.failed in events)` |
   | watch | `watching: <source repo> <mainline>` |

3. **The state word is the lock's own:** `off`, `on`, `limited` or
   `draining`. A restart drain stays `draining` and adds the
   `restarting onto` field. A loop with a failed build reads `on (stale)` or
   `limited (stale)`, and the stale line below it names the build that
   failed. Off, or a lock held with pid 0, prints only `state: off`.
4. **The menu adds color only.** The state value takes the tab bar's mapping
   from ADR 0288 (mugicrew-the-tab-bar-shows-a-loop-state-word…) §3: `off` is
   dim (240), a plain `on` is green, and everything else uses warn (203). All
   labels are dim (244). A value that wraps in the modal continues under the
   value column, not at the box edge. This applies to the store path and the
   stale line. The menu's own dim notes and the options line don't change.
5. **In flight is the count, not the bead titles.** The session view already
   shows which beads are in flight. This reverses the charting note that
   listed them one per line under `in flight: N`.

Example, a watched loop that is limited and stale:

```
state: limited (stale)
pid: 48213

main: 9f3c2e1
store: /nix/store/7q0k…-mugicrew-9f3c2e1/bin/mugicrew

in flight: 2

held by usage limit until: 14:00
reset time from: usage

stale: build of main def5678 failed (see build.failed in events)

watching: /home/alice/src/myrepo main
```

## Considered options

- **Aligned label column with tab-bar words (`restarting`):** rejected. It
  looked tidy, but the plain `label: value` lines read just as well at 60
  columns and stay the lock's own words.
- **The state as an unlabelled headline, with process details last:**
  rejected in favour of the Destination's fixed section order.
- **`until` / `reset from`, or `rev`/`build`/`exe`/`binary`:** rejected.
  "until" didn't say what was being held. `main`/`store` match the rest of
  the loop surfaces.

## Consequences

- The narrowest modal (60 wide) was approved as readable, so it needs no
  narrow variant.
- `TestOLoopMenuOptions` still finds the raw state word in the body.
- `loop status --json` is unchanged.
