# ADR 0142: v2 shell keys replace v1's letter jumps, and dialogs guard destructive or out-of-order actions

Status: Accepted — §3's contents and close keys superseded by ADR 0229 (…hints-bar-and-key-reference…) (one binding set kept)

Amended by ADR 0171 (§1: # $ * come back for Stats, Logs and Settings; digits 6–8 are unbound).
Amended by ADR 0228 (§2: G and L become g r and g l in the goto menu).
Date: 2026-10-12

Chart mc-lu2x.1 (V2 TUI shell beside v1). Settles grilling mc-lu2x.1.4 and
completes the shell keymap begun in ADR 0141. Amends ADR 0141 §6: the dialog
rule widens from "destructive" to "destructive or out of order".

## Decision

1. **No plain letter jumps.** v1's `I` `S` `H` `#` `*` (and `G` as a plain
   jump) are dropped; `1`–`8`, `tab`/`⇧tab` and `←`/`→` switch tabs.
2. **`G` and `L` stay as shell-wide context moves.** `G` reveals the selected
   bead in Graph, and `L` opens Logs scoped to the selected bead. Each means the
   same thing on any tab with a selected bead and does nothing elsewhere. Which
   tabs offer them is per-tab mapping (mc-lu2x.1.6).
3. **`?` toggles a modal key reference** for the current tab and mode. It and
   the hints bar are generated from one `key.Binding` set (bubbles `help`
   short and full views), so nothing is hand-kept as v1's help.go was. `?` or
   `esc` closes it.
4. **`o` toggles the loop on every tab.** Its state shows in the header. A
   first `o` starts or drains with no dialog. An `o` while draining
   force-stops the loop, behind a dialog.
5. **The detail pane has no focus.** `j`/`k`/`↑`/`↓` always move the
   selection and the detail follows. `pgup`/`pgdn` and `ctrl+u`/`ctrl+d` scroll
   the detail. v1's `tab` focus toggle is gone, because `tab` now switches
   tabs.
6. **Shell-wide conventions.**
   - `/` filters or searches the current tab's list, and tabs without a list
     ignore it.
   - `f5` refreshes. (`r` was free here; it is now `r retrospective` on the bead tabs and the focus view, see ADR 0144's amendment.)
   - `q`/`ctrl+c` quits at once unless something is in flight (see §7).
   - While a text input, dialog or the `?` reference has focus, no shell key
     fires, the digits included.
7. **The dialog rule.** An action gets a dialog when it does either of these:
   - kills, discards or abandons something, or closes a bead
   - runs work whose blockers are still open

   Every other action fires at once, including a plain `enter`
   launch or attach. Applied to v1's confirmations, these keep a dialog:
   - dropping a bead's session
   - aborting a live AFK effort
   - recovering a dead effort or gone session, with a second dialog before
     discarding a dirty tree
   - dropping an orphaned worktree before a launch
   - force-stopping a draining loop
   - quitting while run-now efforts or pending writes are in flight
   - rejecting a Signal
   - a History rollback
   - launching (or running now) a bead whose blockers are still open

   Releasing a head from its Initiative gets no dialog. Adopting it again
   undoes the release.

## Considered Options

- **Keep v1's letters as aliases beside the digits.** Rejected. Inbox and
  Search are gone, and two ways to jump is noise in the hints and `?`.
- **Drop `G`/`L` too.** Rejected. A digit can't carry the selection across tabs.
- **Strictly "destructive only" (ADR 0141 §6), with the blocked launch as a
  named exception or with no dialog.** Rejected. The human wants the blocked-launch
  guard (ADR 0026), and a stated rule gives the per-tab mapping a test to apply
  rather than a list of exceptions.
