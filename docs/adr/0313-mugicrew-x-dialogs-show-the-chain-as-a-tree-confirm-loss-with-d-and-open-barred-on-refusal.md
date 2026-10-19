# ADR 0313: mugicrew's `X` dialogs show the chain as a tree, confirm loss with `D`, and open barred on a refusal

Status: Proposed
Date: 2026-10-19

Chart mc-1k3gq (Withdraw or cancel any unclosed chain from the TUI, with
cleanup). Settles the layout and keys of the two dialogs ADR 0310
(withdraw-drops-any-unclosed-chain-from-tui-x…) §4–§6 calls for, chosen by the
human from three variants of each, built as a throwaway over the real TUI frame
(prototype mc-1k3gq.4, side ref `refs/mugicrew/prototypes/mc-1k3gq`).

## Decision

1. **Withdraw lists the chain as its own tree.** Each row is one bead to close,
   head first, its descendants under `├`/`└`. A row whose bead loses or keeps
   work is tagged on the right: `✎` uncommitted changes in its tree, `↑`
   unintegrated commits on its branch, `⊘` a foreign hold (ADR 0054) that is
   skipped. Wide, a tag carries its words (`✎ 4 files changed`, `↑ 3 commits`,
   `⊘ held: skipped`); narrow (inner width under 80), only the glyph and count
   (`✎4 ↑3`, `⊘`). One summary line under the tree totals each tag. The lead
   line says the close reason, that there is no successor, and which container
   branch goes with it. A list taller than the screen scrolls with `j`/`k` and
   a `k/N` counter, like the picker.
2. **Losing work takes `D`, not `y`.** When nothing is discarded, `y`/`enter`
   withdraws. When any tree or branch loses work, the hint reads `D discard and
   withdraw`: `y` only prints the warning line "This discards work. Press D to
   discard it and withdraw." and `D` confirms. `d` keeps its existing meaning,
   drop, elsewhere in confirm dialogs.
3. **Cancel puts the tally first and the choice on each row.** The top lines
   say how many chains (and beads) close, how many are released, and what the
   closing chains discard, and they update as rows change. Each chain row has a
   `(●) close  ( ) release` choice: `r` releases the highlighted row, `c`
   closes it, every row starting on close. A row Require Initiatives keeps on
   close shows `🔒 locked`, with its reason on the line under it while
   highlighted; `r` on it leaves it closed and says why. Narrow (inner width
   under 70), the choice drops to its own line under the chain's title.
4. **Cancel confirms on a review page.** `enter` opens a second page listing
   what closes (with each chain's `✎`/`↑` cost), what is released, and the
   total discarded; `y` cancels the Initiative there, `esc` goes back to the
   rows with the choices kept.
5. **A refusal opens the dialog, barred.** `X` on a refused head or Initiative
   opens the same dialog with a red "Refused" banner on top: one `✗` line per
   blocker (in-flight effort, bead ordered after, open Landing and its Merge
   Request), each with the fix under it ("abort it first: x on …"), then, dim,
   what the verb would do once clear. `y` and `D` do nothing; `enter` jumps to
   the first blocker; `esc` closes it.

## Considered options

- **Sections or counts with drill-down for withdraw.** Rejected: both split a
  bead's losses across lists, so the human can't see which chain member each
  dirty tree or branch belongs to.
- **A checklist with a space toggle, or Close/Release columns, for cancel.**
  Rejected: the tally sat under the list, and neither had a last look at the
  discards before confirming.
- **A status-line refusal, like other refused verbs.** Rejected: a status line
  holds one blocker in one line, and withdraw can be refused by several at
  once, each with its own fix. A dialog that shows what would happen also
  answers "what did I just try to drop?".
