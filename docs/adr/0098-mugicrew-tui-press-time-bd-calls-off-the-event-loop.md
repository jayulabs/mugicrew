# ADR 0098: mugicrew TUI press-time bd calls run off the event loop under a per-bead in-flight guard

Status: Accepted; §5 superseded and abort added to §1/§3 by ADR 0107
Date: 2026-10-08

> **Extended by [ADR 0101](0101-mugicrew-tui-render-runs-no-subprocess.md):**
> the rule reaches `View` and every render helper (no bd, git or tmux
> subprocess on render), the HITL launch preview becomes static, and `l`'s
> `launchFor` moves into the launch Cmd.

## Context

Bubbletea runs Update serially, so a key handler that shells out to bd blocks
every key queued behind it. The measurement found
each bd subprocess costs 0.45–2s alone and 1.3–8s while the tick's `bd list
--all` or the loop holds Dolt. That is the felt scroll and tab-switch lag. View
and Update cost 2–25ms at 1000 beads. tmux and git reads cost about 5ms.
Decided in grilling.

## Decision

1. **No bd call on the event loop.** Every key-driven bd call runs inside a
   tea.Cmd: `+`/`-`, `p`, `t`, the `e`/`E`/title/description commits, `z`,
   the `n`/`u`/`R` creates, the orphan drop, `x` drop (whole, tmux included,
   so its event record and unclaim keep their order), and `onBusy`'s
   `afkHolder` (computed inside the launch Cmd and carried on
   `launchDoneMsg`). tmux and git reads that are not part of a moved handler
   stay inline.
2. **One shape.** One `m.pending` map keyed by bead id (or by the create form)
   and one done message whose closure applies the result: status through
   `setStatus`, an echo, and a reload. `m.launching` stays separate.
3. **Per-bead guard, second press refused.** A write in flight on bead A refuses
   another write on A with a status line. It never blocks bead B. Presses are
   neither queued nor coalesced, so mashing `+` twice moves one step until the
   first write lands.
4. **Echo on success only.** `+`/`-` sets the priority locally in the done
   handler, looked up by id, only after bd confirms. Nothing is shown before
   the write returns (optimistic UI is out of scope).
5. **Pending UX is the status line.** "updating A…" and similar, with no row
   badge. Create forms and inline commits stay open and ignore input while
   filing. Success closes them. Failure keeps the text.
6. **`t` keeps claim-before-dispatch.** The whole press (reads, eligibility,
   pool check, claim) is one Cmd. Its done message appends to `m.runs` and
   starts `runNowCmd`. The quit confirm also counts in-flight writes, so quitting
   cannot silently strand a claim.

## Considered Options

- A global busy flag: rejected, because a 2s `t` on one bead would lock out
  edits on every other bead.
- Queuing or coalescing repeat presses: rejected. Queuing brings back ordering
  bugs, and coalescing is optimistic UI.
- A wall-clock acceptance bench: rejected in favour of a table-driven unit test.
  It stubs each bd seam var with a recorder, asserts the handler returns zero
  inline calls and a non-nil Cmd, then runs the Cmd.
