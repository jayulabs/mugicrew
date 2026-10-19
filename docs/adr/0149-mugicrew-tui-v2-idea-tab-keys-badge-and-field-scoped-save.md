# ADR 0149: v2 Idea tab keys, a badge that counts triage sessions waiting on you, and a field-scoped form save

Status: Accepted; §1 hint order superseded by ADR 0229 (…hints-bar-and-key-reference…) (membership kept)

Amended by ADR 0171 (§1: x drop/abort/recover on the Signal row; §4 stands) and ADR 0177 (§4: also counts a listed Initiative whose cut session waits on you).
Amended by ADR 0258 (§1: E raw is removed from the Idea tab; E is a shell key opening $EXPLORER).
Amended by ADR 0315 (§1: Backlog also holds each listed P3/P4 Initiative's whole group; §3: its groups come before its flat Signals).
Date: 2026-10-13

Chart mc-lu2x.5 (Idea tab for the v2 TUI). Settles grilling mc-lu2x.5.5, which
followed the human's try of prototype mc-lu2x.5.4. Adds the Idea tab's row to
ADR 0144 §7 and fills in ADR 0141 §8's badge for this tab. Builds on ADR 0148,
the form's layout, fields and keys.

## Decision

1. **Keys and hints bar** (ADR 0144 §7), in drop order:

   | row / mode | hints, in order |
   |---|---|
   | '+ New Signal' row | enter new · n new |
   | Backlog header (Spec mc-csvx) | enter expand\|fold · n new |
   | Signal row | enter triage · d detail · z reject · n new · e edit · E raw · +/- prio · a/A adopt/release |
   | Signal row, detail open | the same, `d close`, plus `pgup/pgdn scroll` |
   | form (ADR 0148) | ctrl+s save · tab field · esc cancel |

   The Backlog header is the folded group of P3/P4 Signals that are not in
   triage; `enter` toggles it and shows `expand` while folded, `fold` while open.
   `z` shows only on an open, unclaimed Signal, and `A` only when the Signal
   has an Initiative. `E` is on the tab because ADR 0148 sends labels and deps
   to the raw edit. `+/-` and `E` mean the same here as on Graph.
   The form's bar omits FR40's usual `? keys` on purpose: `?` is typed into
   the form's fields.
2. **An in-triage Signal.** `enter` does what Graph's launch does: it attaches
   to a live Luffy session, or resumes an exited one. `e` is allowed, and §5
   handles any clash.
3. **Row order.** Within a group, rows use Graph's `beadCmp` (priority, then
   newest first). In-triage rows are mixed in by priority and stand out only
   by their `◐ triage` mark. Initiative groups are sorted by `beadCmp`, and
   'No Initiative' comes last. A closed Initiative that still has open Signals
   is headed "Title (closed)".
4. **The Idea tab's ●N** counts in-triage Signals whose Luffy session is
   waiting on the human (your turn or needs approval). It does not count open,
   untriaged Signals.
5. **Field-scoped save.** The form takes a snapshot of the Signal when it
   opens. `ctrl+s` writes only the fields the human changed from that
   snapshot. If another writer (Luffy, the CLI, another pane) has also changed
   one of those fields, the save is refused, the form stays open, and the
   status line names the field. If the Signal is closed while the form is
   open, the form shows a warning, `ctrl+s` is refused, and `esc` is the only
   way out.
6. **esc on a changed form** opens the y/n dialog "Discard changes?" (ADR 0141
   §6): `y`/`enter` discards and `n`/`esc` goes back to the form. A form with
   no changes closes at once.

## Considered Options

- **Badge counts open, untriaged Signals.** Rejected. That is a backlog, so
  the badge would almost never be off. A badge should mean "blocked on you
  now".
- **Last write wins on save.** Rejected. `e` is allowed during triage, so
  writing all three fields would silently overwrite Luffy's edits.
- **Refuse `e` on an in-triage Signal, as `z` is refused.** Rejected. It would
  block fixing a typo while triage runs, and §5 already prevents clobbering.
- **esc keeps the draft in memory for the next `n`/`e`.** Rejected. It is
  extra state that nobody asked for.
