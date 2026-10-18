# ADR 0232: TUI on-demand closed beads stay loaded while a claim names them

Status: Accepted; amends ADR 0197 §1–§2
Date: 2026-10-16

Chart mc-1nvm (TUI Graph and Logs tabs bounded by what is shown). Settles
prototype mc-1nvm.6 (How on-demand closed beads live beside the 3 s refresh).
The throwaway is on `refs/mugicrew/prototypes/mc-1nvm`
(`apps/mugicrew/tui/zz_proto_ondemand_closed.html`).

The TUI's default load is now the open set plus the default closed set: two
exact calls, `bd sql` for the ids and then `bd list --all --id`
(docs/research/bd-read-paths-for-tui-default-and-on-demand-closed-sets.md).
Older closed beads load on demand. This ADR decides how those beads join the
snapshot that each load replaces, and when they leave it.

## Decision

1. **Claims.** An on-demand closed bead stays loaded while at least one claim
   names it. A bead no claim names is evicted at the next fold. There are four
   claims, and each has its own bound:
   - **The Graph query, while Graph is the tab.** It claims its closed hits and
     each hit's group: the hit's ancestors and the children of those
     ancestors. Without the whole group, a closed Chart that appears only as
     context would count its header `closed/total` and drawer `children` over
     just the loaded rows. While a query is active, an open match that closes
     joins the query's hits, so it does not disappear from the result.
   - **Recents**, up to 20 ids. Recents rows then load, and `recordRecent`'s
     prune (it drops ids not in the snapshot) never removes a bead that is
     only unloaded. Without this claim, Recents would change, which breaks the
     Chart's hard constraint.
   - **Every bead with a tmux session.** Its close still reaches the HITL
     done-flip, which keys on `Status == "closed"` in the snapshot
     (ADR 0197 §2).
   - **An open Goto menu**, which claims the selected bead's one-hop
     neighbours. Rows that are not loaded stream into the menu.

   There is no count cap and no LRU. A cap would evict rows that are still on
   screen. Each claim above is already bounded, except `s any`/`s closed` with
   no words, and that query is exactly what Graph is showing.
2. **Merge.** Every load builds the snapshot as open ∪ default ∪ claimed. When
   ids overlap, the fresh open copy wins over the default copy, and the
   default copy wins over the on-demand copy. That way a reopen is never
   treated as a drop-out.
3. **Two tiers, amended.** The 3 s tier still reads only the open set. The
   slow tier, the Graph-entry load and the drop-out read (ADR 0197 §1–§2) no
   longer run `bd list --all`. They run the default `bd sql`, then one
   `bd list --all --id` over the default ids together with every claimed id
   that is neither open nor default. That call also refreshes the claimed
   beads, so a deletion drops out within 30 s (the read cache ADR 0197 already
   accepts). `bd list --all` runs only while the Graph query claims every
   closed bead (`s any`/`s closed` with no words).
4. **On-demand fetches run off the event loop** (ADR 0098), and each carries
   the query generation that started it. A fetch that lands after a newer
   keystroke is dropped. When the query narrows, earlier hits that still match
   stay until the new fetch replaces them, so rows do not flicker.
5. **Leaving Graph releases the query's claim.** The query text, its
   remembered hit ids and the Graph cursor stay. Graph is not rebuilt while
   inactive, so leaving does not move the cursor. Other tabs then fold only
   the open set, the default set and the remaining claims, so their cost stays
   flat even after `s any`. On Graph entry, the slow load claims the hits again
   and re-reads them in the same call.
6. **Selection follows its bead** (ADR 0076 §1, ADR 0143 §7), with one
   addition. While a query fetch for the current generation is in flight
   (`searching…`), a lost cursor bead is remembered. The cursor parks on the
   nearest neighbour and moves back when the fetch brings the bead in. This
   covers `G`/Recents onto an unloaded bead, which scopes Graph to it, and
   Graph entry. Any cursor move cancels the memory. Once nothing is in
   flight, ADR 0076 §2 applies unchanged: a bead that leaves the rows,
   including by eviction, hands the cursor to its nearest neighbour.

ADR 0175 is unchanged. The default status stays `unclosed`, and words at that
default still match every status. Closed matches now arrive under
`searching…` rather than at once, which this ADR covers.

## Considered options

- **Keep the query's beads loaded while the query lives, across tabs.** This
  avoids a re-read on entry. But after `s any`, every tab would fold about 10k
  closed beads on each load until the query is cleared.
- **A count cap or LRU on on-demand beads.** It gives a hard bound, but it
  evicts rows still on screen and Recents entries.
- **Move the cursor to the nearest neighbour at once, even while searching.**
  `G` onto an unloaded bead would then need a pending-target rule of its own,
  and Graph entry after `s any` would lose the cursor.
- **No session or Recents claim.** The prototype showed that both break
  behaviour the Chart holds fixed: the done-flip is missed, and Recents
  prunes beads that still exist.
