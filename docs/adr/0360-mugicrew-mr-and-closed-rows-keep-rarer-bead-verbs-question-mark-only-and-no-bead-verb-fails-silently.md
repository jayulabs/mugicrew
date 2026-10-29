# ADR 0360: MR and closed rows keep rarer bead verbs `?`-only, and no bead verb fails silently

Status: Accepted
Date: 2026-10-28
Amends: ADR 0359 (bead-verbs-sit-in-the-hints-bars-row-section…), which left open which bead verbs stay `?`-only on which row kinds.

Chart Bead verbs offered wherever a bead row appears (mc-ak2l0) has each bead row
hint what Graph would hint for that bead's state. ADR 0359 kept the sections and
the drop rank (`c`, `B/U` and `w` in the row section, with `w` at 4.5, `X` at 5.5,
`c` at 12 and `B/U` at 14). The human also accepted that rarer verbs may stay
`?`-only on some row kinds. A `?`-only verb is still routed through the shared
bead-verb router, gated by state and listed under "this row" in `?`. It just isn't
drawn on the bar. The human settled these exceptions:

1. **MR rows** (Defog's and Execute's Merge Requests sections, where the row's bead
   is the MR Watch) hint only `w`, plus `enter`, and `x` while a session runs. Every
   other bead verb is `?`-only and acts on the Watch. `w` is the point of the row,
   and pausing, re-prioritising or blocking a Watch is rarely what is meant there.
2. **Closed rows** (Defog's Finished in last 24h, Execute's Landed in last 24h and
   closed members under `▸ N closed`) hint `c`, plus `x` while a session runs. `e`
   and `U` are `?`-only. A checkup on a closed bead is a real use (ADR 0274
   (a-checkup-launches-from-mugicrew-checkup-or-the-c-key-on-any-bead-row)), while
   editing or unblocking a closed bead is rare.
3. **No other exceptions.** These rows hint the full state-gated set, as Graph would:
   - the "status only" rows (Defog's Charts in work, Execute's Checkpoints and Specs
     lists), which match their tree twins. The same bead must not hint differently
     on two rows of one tab, and "status only" describes the section, not the keys.
   - every row that names an Initiative: Ideate's and Defog's Initiative headers,
     Execute's Initiative section, the focus-view header and the Initiatives list
     rows.
   - Ideate's drained cue row, which is its Initiative's bead row, as it already is
     for `enter`, `+/-` and `X`.
4. **`canUnblock` keeps no status test.** Removing a stale blocker edge from a
   closed bead is legitimate graph hygiene, so `U` stays offered there, `?`-only
   per §2.
5. **No bead verb fails silently.** A bead verb that is routed but not offered for
   the row's state says why on the status line when pressed. That covers `c` on a
   Checkup row (silent on Graph today) and Ideate's and the focus view's unhinted
   `x`, which today drops without a word where Graph's `dropKey` explains.

## Considered Options

- **Hint everything Graph would on MR rows.** Rejected: it buries `w` under verbs
  that act on the Watch, a bead people rarely mean to change.
- **Make every verb `?`-only on status-only rows.** Rejected: one bead would hint
  differently on two rows of the same tab, which is the inconsistency the Signal
  complained about.
- **Tighten `canUnblock` to unclosed beads.** Rejected per §4.
