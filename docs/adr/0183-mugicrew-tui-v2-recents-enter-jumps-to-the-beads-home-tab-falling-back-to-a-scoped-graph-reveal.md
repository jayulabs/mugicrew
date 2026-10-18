# ADR 0183: v2 Recents enter jumps to the bead's home tab, falling back to a scoped Graph reveal

Status: Accepted
Date: 2026-10-15

Chart mc-kmbi (v2 TUI Recents). This ADR settles prototype mc-kmbi.2 (side
ref `refs/mugicrew/prototypes/mc-kmbi`). The human tried both jump targets on
the live graph and picked the home tab. ADR 0142 §2 and ADR 0175 still govern
G from Logs and History. This ADR changes only what enter does on a Recents row.

A Recents row stands for a bead the human recently acted on. Landing on the tab
where that bead is worked keeps its verbs and its Actionable context. Graph is
for structure. When a bead is not listed on its home tab, an unscoped Graph
reveal clears the filter and widens the status, which leaves the whole graph
(over 2,000 rows here) on screen and makes the TUI lag. So the fallback scopes
Graph to the one bead.

## Decision

1. **Home tab.** Enter closes Recents and goes to the bead's home tab:
   - Signal: Ideate.
   - Chart and its defog leaves (grilling, research, prototype, survey,
     chart-conflict): Defog.
   - Initiative: Initiatives.
   - Every other spine kind (Spec, Checkpoint, Build, Task, Pivot, Rollback,
     Surgery…): Execute.

   Enter selects the bead's row there. If a row for the bead is already visible
   (including its Actionable twin), enter selects that row and unfolds nothing.
   Otherwise enter unfolds the fewest containers that bring a row for it into
   view (ADR 0179 §3 starts them folded).
2. **Unfolds are session-only.** A jump's unfolds are not written to
   folds.json (ADR 0179 §6). The prototype found that a later `space` saves the
   tab's whole fold set, jump unfolds included. So "session-only" means a jump's
   unfolds stay out of every save until the human flips that container
   themselves. The Spec picks the mechanism.
3. **Fallback: a scoped Graph reveal.** When the home tab does not list the
   bead (closed, filtered, outside the frontier), enter goes to Graph with the
   query scoped to that bead (words = its id, status any). Graph shows the bead
   under its ancestors as context rows, and the status line says why it fell
   back. esc returns Graph to its baseline (ADR 0175). The scope is not saved
   as the Graph query (ADR 0176), because a jump is a visit and not a filter
   the human chose.
4. **Focus view.** A jump from inside the Initiative focus view (ADR 0173)
   closes the focus first, like a tab key does. The existing `revealMsg`
   handler does not close it, so the Recents path must.

## Considered Options

- **Graph reveal, exactly as G.** Rejected. Graph has no queue verbs, and an
  unscoped reveal of a closed bead leaves the full graph listed, which lags.
- **Persist jump unfolds to folds.json.** Rejected. Peeking at a bead from
  Recents would permanently rearrange the tab the human had folded by hand.
