# ADR 0229: v2 hints bar and `?` reference: sections, one drop rank, and an empty bar under overlays

Status: Accepted
Amended by ADR 0241 (§1, §3, §5: n new in nav and pinned, u and U gone, the n new › group).
Amended by ADR 0321 (§5: one global `<k>  tmux prefix (nested)` line while nested with a prefix2).
Amended by ADR 0258 (§1, §3, §5: E raw is removed; E explorer joins the shell keys in the ? reference after o loop on/off, never on the bar).
Date: 2026-10-16

Supersedes ADR 0058 §4 (Content and Layout) and §5 (separator tightening),
ADR 0066 §2, ADR 0141 §4 ("truncates with `…`") and §6 ("the hints bar shows
only those two"), and ADR 0142 §3 (the reference's contents and close keys).
Amends ADR 0144 §7 (drop order, "always ends with `? keys`", and the picker,
form/reason and y/n rows). Supersedes the hint *order* only, not row
membership, in ADR 0149 §1, ADR 0156 §7, ADR 0164 §6, ADR 0165 §5, ADR 0170 §7,
ADR 0174 §5 and ADR 0181 §5.

Chart mc-uwnd. The human found that useful keys dropped first at narrow widths,
and that order and grouping varied from tab to tab. Both came from each surface
being one flat binding list whose order was also its drop order, with the rules
spread over a dozen ADRs. This ADR moves the shared rules into one place.
Per-tab ADRs still decide *which* verbs a row offers (ADR 0144 §7's "only verbs
that apply"). This ADR decides where those verbs sit, how they shorten and drop,
and when the bar is empty. Research:
docs/research/tui-key-hint-surfaces-and-bindings-inventory.md.

## Decision

1. **The bar has sections in a fixed order:** `row | tab | nav | ? keys`. The
   sections are split by ` | ` and the items by ` • `. A section with no keys
   left takes its `|` with it, so a `|` only ever sits between two non-empty
   sections. Display order comes from the section, and it is separate from the
   drop rank (§3).
   - **row:** `enter <verb>` (including History's reveal and Initiatives'
     focus), `b bg`, `p`, `x`, `z`, `e edit`, `E raw`, `+/- prio`, `a/A`,
     `u`/`U`, `u undo` (History; Stats' `u 5h/wk` is a tab hint).
   - **tab:** `n`, `/`, `s`, `f`, `v debug`, `e errors`, `esc clear`, `w`,
     `b group by`, Settings' `+/- stage`, `enter save` and `esc discard`.
   - **nav:** `g goto`, `space fold`, `d detail`/`d close`, `esc back` (the
     focus view).
   - `j/k`, `J/K`, `pgup/pgdn` and Settings' `j/k row` are in `?` only.
     Shell keys (tab jumps, `tab`/`shift-tab`, `o`, `f5`, `R`, `D`, `S`, `q`,
     `?`) stay off the bar, as ADR 0144 §7 had them.
   - `a/A` is one item, labelled with whichever halves apply.
2. **Narrow widths shorten first, progressively.** One label at a time becomes
   key-only, highest drop rank first. The pinned keys shorten last. `enter`
   keeps its verb until it is the only verb left. Only when every label is
   key-only does the bar start to drop items. Shortening everything at once was
   rejected because it is a cliff: Graph with a bead open overflows 120 columns
   by a few, loses every label, and leaves about 40 columns blank.
3. **Items drop by one shared rank** across tabs and sections, not from the
   right. From last-to-drop to first-to-drop: `enter` › `esc clear`/`esc discard` › `d` ›
   `space` › `b bg` › `x` › `p` › `n` › `/` › `z` › `s` › `f` › tab extras (`v`,
   `e errors`, `w`, `b group by`, `+/- stage`) › `e edit` › `a/A` › `r` › `u` (incl. History's `u undo`) ›
   `U` › `E` › `+/- prio`. Pinned items never drop: `g goto`, `esc back` and
   `? keys`. The bar never cuts with `…`.
4. **Under an overlay the bar is empty.** While a dialog (confirm, input or
   form, picker) or `?` is open, the box holds every key it accepts, and the bar
   draws nothing, not even `? keys`. A dialog's keys are never drawn twice.
   Dialog hint lines inside the box stay flat: no sections and no rank, only
   key-only shortening. The `/` input on Graph and Logs has no box, so it keeps
   `enter keep • esc revert` on the bar, flat and without `? keys` (`?` would be
   typed).
5. **`?` is a centred, bordered, scrolling box.** Its title is
   `keys — <tab>` (`<tab> › focus` in the focus view). It still comes from the
   same binding set as the bar (ADR 0142 §3).
   - **Sections, in order:** this row, this tab, navigation, `g goto ›` (ADR
     0228's options), global. "this row" lists only the verbs the selected row
     offers, as the bar does. With none (a header), the heading stays, followed
     by one dim line: `nothing for this row`. There is no tmux section: the TUI
     session has no tmux bar (ADR 0066 §1).
   - **Columns:** one column below 100 columns (the box is at most 72 wide).
     From 100 columns there are two: this row, this tab and navigation on the
     left; `g goto ›` and global on the right.
   - **Scroll:** `j/k` and `↑/↓` move a line; `pgup/pgdn`, `space`, `ctrl+u` and
     `ctrl+d` move a page; `g`/`G` go to the top and bottom. A dim position
     counter (e.g. `1–5/46 ▼`) sits on the footer line beside
     `j/k scroll • ?/esc close`.
   - **Close:** `?`, `esc` or `q` (`q` does not quit). Every other key is
     swallowed, as in ADR 0058 §4.

## Considered Options

- **Amend each ADR in place** (ADR 0144 §7 for the bar, ADR 0058 §4 and
  ADR 0142 §3 for `?`). Rejected: about six ADRs would be rewritten and the
  rules would stay scattered, and ADR 0058 is a v1 ADR.
- **Restate every tab's hint rows here.** Rejected: which verbs a row offers is
  a per-tab decision with its own reasons. Only the order and drop rule changed.
  The membership disagreements in the research's §11.6–10 stay with their
  tab ADRs.

Amended 2026-10-17: the Stats tab's `d dist time/cost` is a tab extra (§1 tab section, §3 tab-extra rank), and `hintOf` places `d` by its label: `d detail` and `d close` stay in the nav section at rank 2.

Amended 2026-10-17: the `r` drop-rank slot now ranks `r retrospective` (it was rebase, which moved into the New menu).
