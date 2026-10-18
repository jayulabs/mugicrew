# ADR 0058: mugicrew TUI keymap — G/I/L tab jumps, a `?` key reference, and the audit fixes

Status: Accepted — §3 Logs `esc` amended by a Spec (FR7: `esc` clears search → kind → scope); `S` (Search) jump, 4-tab bar and its 40-col fallback added by ADR 0072; §1 `G` reveals the selection on Search (ADR 0074) and Inbox (a Spec); for v2, §6's `h` key superseded by ADR 0175 (v1 keeps it); §4 Content and Layout and §5 separator tightening superseded by ADR 0229 (…hints-bar-and-key-reference…)
Date: 2026-10-04

Amends: ADR 0026 (the footer is the single command surface, with no modal). Chart
(TUI polish). Settles a grilling that worked from a keymap inventory.

## Context

The tabs can only be reached with `←/→`. The tab bar does not show a key for
them. Several keys are handled but never hinted (`E`, `enter`, `ctrl+c`), and the
Logs tab's `d`/`e`/`esc` are bound by a Build, not by any ADR. ADR 0026
made the footer the only place commands are shown and ruled out modals. So a
full key reference needs an explicit amendment. Everything must still fit the
40-col floor.

## Decision

1. **Tab jumps are `G` / `I` / `L`.** `L` means "go to Logs". On Graph with a
   bead selected, it scopes Logs to that bead (plus its children for a
   container), as it does today. From Inbox or Logs it just jumps there and
   leaves any scope as it is. Pressing the key of the tab you are on does
   nothing. Uppercase is used for all three because `l` is launch and the
   lowercase letters sit next to `j`/`k`.
2. **The tab bar shows the key by color, not by parentheses**: **G**raph
   **I**nbox(N) **L**ogs, with the key letter in an accent color. This costs no
   width, so the 40-col fallbacks (ADR 0051 §4–5, ADR 0057 §4) are unchanged.
3. **The Logs keys `d` (debug), `e` (errors-only) and `esc` (unscope) are
   ratified as they are.** Logs never has a selected bead, so they shadow no
   live action. `?` makes them discoverable.
4. **`?` opens a read-only key reference** in normal mode only. In edit, text
   and create modes `?` stays a text rune.
   - **Content:** every key handled on the *current tab*, one line each as
     `key  what it does`. That includes the keys the footer never hints:
     `j/k`, `←/→`, `tab`, `enter`, `E`, `ctrl+c`, `G/I/L`, `?`. It lists the
     whole key set for the tab, not a filter of what applies to the current
     selection.
   - **Layout:** it replaces the body area. The tab bar and the status line
     stay. It is one column with no border, the keys are padded to a fixed
     width, and descriptions are truncated to the width. When it is taller
     than the body, `j/k` scroll it.
   - **Dismiss:** `?`, `esc` or `q` close it (`q` does not quit). Every other
     key is swallowed, so a stray key can't fire an action you can't see.
5. **ADR 0026 amendment.** The footer stays the single *command* surface. `?` is
   a reference view that takes over the body, like the single-pane detail does,
   not a modal dialog. No action can be taken from it. The footer gains a `?`
   hint that, like `q`, is never dropped. It drops no other hint.
   The footer decides its drops without `?`, then adds it before `q`. If that
   overflows, every separator tightens to a single space, as the tab bar does.
6. **Audit fixes** (inventory §4.2):
   - The `editTitle` and `editRaw` help lines get a legend: `enter commit|run ·
     esc cancel`.
   - `ctrl+c` cancels the pick, text and create modes. Today they swallow it.
   - `h` acts on the Graph tab only.
   - `l` refuses closed HITL beads and HITL containers with a status message,
     so the handler matches the hidden hint.

## Considered options

- *A different letter for Logs, so that `L` stays scoped-only.* Rejected: the
  label would read `lOgs`, and one letter would carry two Logs meanings.
- *Digits 1/2/3.* Rejected: the tab label can't show its key.
- *A bordered box centered over the body.* Rejected: the border costs about 4
  columns at the 40-col floor.
- *Rebinding the Logs `d`/`e`/`esc`.* Rejected: it costs muscle memory and fixes
  no real conflict.
