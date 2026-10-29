# ADR 0359: Bead verbs sit in the hints bar's row section, with w and X ranked high

Status: Accepted
Date: 2026-10-28
Amends: ADR 0229 (tui-v2-hints-bar-and-key-reference-sections…) §1 and §3.
Amended by ADR 0360 (mr-and-closed-rows-keep-rarer-bead-verbs-question-mark-only…): which bead verbs stay `?`-only on which row kinds.

Chart Bead verbs offered wherever a bead row appears (mc-ak2l0) hints every bead verb
on every bead row, so `c checkup`, `B add blocker`, `U unblock` and `w open MR`
reach Defog, Execute, Initiatives and the focus view. ADR 0229 §1 calls `w` a tab
extra, and its §3 rank has no slot for `c`, `B` or `U`. The code already ranks `c`
at 12 and `B`/`U` at 14 for Graph. `X` has no rank at all, so `hintOf` treats it as a
tab extra and draws it after the first `|`. We place every bead verb in the **row**
section. `w` ranks just below `b bg` (4.5): on an MR row it is the main verb.
`X` ranks just below `x` (5.5). `c` stays at 12, and `B`/`U` stay at 14 with `a/A`.

A throwaway prototype rendered representative rows at 80, 100 and 120 columns. It
was checked against the real `sectionedBar` and kept on
`refs/mugicrew/prototypes/mc-ak2l0`. At these widths nothing drops on any row with
the full verb set. The verbs cost labels, not keys: the busiest rows need 130–195
columns to keep every label (83–149 today), and at 80 they are nearly all key-only.
Items start dropping only below about 70–78 columns. So the rank mostly decides
which labels go first. The human picked this placement after trying it.

The human also accepted that rarer bead verbs may stay `?`-only on some row kinds.
This qualifies the Chart's default that a row hints whatever Graph would. Which
verbs, and on which row kinds, is left to the grilling Where c, B/U and w sit in
the hints bar's sections and drop rank.

## Considered Options

- **Today's code:** `w` and `X` stay tab extras. Rejected because it puts two bead
  verbs in the tab section, after `|`, apart from the row's other verbs.
- **Maintenance verbs drop first:** `c` at 17, `B`/`U` at 18 and `w` at 7.5.
  Rejected by the human. At 80–120 columns it changed only which labels shorten
  first, and it moved `w` down on MR rows, where `w` is the point of the row.
