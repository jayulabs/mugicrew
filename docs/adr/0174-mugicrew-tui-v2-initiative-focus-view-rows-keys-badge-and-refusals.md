# ADR 0174: v2 Initiative focus view rows, keys and badge; `i` enters from any row in an Initiative, and each section keeps its source tab's keys

Status: Accepted; §1 amended and the no-tab reasoning set aside by ADR 0181; §5 hint order superseded by ADR 0229 (…hints-bar-and-key-reference…) (membership kept)

Amended by ADR 0228 (§1: the i entry key becomes g f in the goto menu).
Amended by ADR 0241 (§5: n opens the New menu in the focus view; a and A stay unbound).
Amended by ADR 0258 (§5: E raw is removed from the focus view; E is a shell key opening $EXPLORER).
Date: 2026-10-14

Amends ADR 0173 §1 and §2 (the back bar's ●N and crew counts).

Chart mc-jmr8 (Initiative focus view). This ADR settles grilling mc-jmr8.4,
which followed the human's try of prototype mc-jmr8.3 and research mc-jmr8.2
(docs/research/initiative-focus-view-rows-and-seams.md). It is modelled on
ADR 0156 (Defog) and ADR 0170 (Execute). ADR 0173 records the chrome: the
back bar, the header row's look and the empty-section placeholders.

The view narrows ADR 0131's rejected "Initiatives tab with drill-in". It is a
drilldown entered from rows that already exist, with no tab and no Initiative
picker, so ADR 0131's reason still holds: there is no second grouped view of
every Initiative to keep in step. Graph keeps the structure edits (adopt,
release, cancel, withdraw, pause) and the landed N/M progress.

## Decision

1. **Entry with `i`.**
   - `i` works on Graph, Ideate, Defog and Execute, on any row that resolves
     to an Initiative:
     - an Initiative header row is that Initiative
     - any other row walks up `Parent` to the Initiative
     - a Task or Pivot uses the rule in §4.5
   - On a row in no Initiative, `i` refuses with "i: this row is not in an
     Initiative".
   - `i focus` shows in the hints bar on Initiative header rows only, Ideate's
     included (ADR 0172 §1 made them selectable). It is listed in `?` on all
     four tabs.
   - Binding `i` on these tabs, and listing it, is the only change this view
     makes to them.
   - Inside the focus view `i` is unbound. There is no nested focus, because
     Initiatives never nest.
2. **Leaving.**
   - `esc` closes the first of these that is open: a dialog, then `?`, then
     the detail drawer, then the focus.
   - Leaving the focus restores the origin tab exactly as it was: cursor,
     folds and detail. If the origin row has gone, the tab's usual clamp
     applies.
   - A tab-switch key also leaves the focus (ADR 0173 §1).
   - Every entry starts fresh: the cursor is on the header row and every fold
     is at its default. Focus folds are not kept, unlike Ideate's
     (ADR 0172 §4).
3. **The Initiative header row** (its look is ADR 0173 §3).
   - `enter` and `b` launch or attach Luffy's cut session, as on Graph
     (ADR 0146). A drained Initiative's focus is where the human decides on
     its next phase. This differs on purpose from ADR 0172 §1, where an
     Ideate header's `enter` does nothing; work in flight (mc-lu2x.31.1) is
     bringing the cut session to Ideate's header as well.
   - `d` shows Graph's Initiative detail.
   - `p`, `+/-` and `x` are not bound on the header. Graph keeps them. On
     Ideate Signal rows and on Defog and Execute bead rows `p` is bound (Graph's
     pause toggle, hinted `p pause`/`p unpause` before `x` on open rows), as on
     those tabs.
   - Hints: `enter <verb>` · `b bg` · `d detail` · `esc back` · `? keys`.
4. **Rows, by kind.** Membership is the Initiative's subtree by `Parent`,
   built on the full snapshot and filtered to rows (research §3), plus the
   rules below.
   1. **Ideate** lists every open member Signal in `beadCmp` order, P3/P4
      included. They are not routed to a Backlog here.
   2. An in-triage Signal with no session (a stranded claim) is listed, so
      `x` can recover it.
   3. **Defog** lists member Charts with their leaves, as the Defog tab does.
      A defogged Chart that has not landed is also listed, with a "defogged,
      awaiting land" suffix: no queue tab shows one, so this view is its only
      place outside Graph.
   4. **Execute** lists member Checkpoints, then Specs, then the Tasks and
      Pivots of §4.5, with their context hints (ADR 0170 §4). There is no
      Actionable section.
   5. **A Task or Pivot** is placed under its own Initiative first. Only when
      it has none does it fall under the Initiative of the Checkpoint it
      waits on or gates (Execute's rule, ADR 0170 §2). A Task adopted into
      Initiative X that holds a Checkpoint in Initiative Y is listed in X's
      focus, with its hint naming Y's Checkpoint. In Y's focus, the
      Checkpoint only shows as blocked, and its `d` detail names the blocker.
   6. Drafts are not listed, nor are unparented Rollbacks and Surgery
      Checkpoints unless a human adopted them.
   7. Closed beads fold under '▸ N closed' at the end of each section and
      container, as the Chart decided.
5. **Per-section keys, marks and hints.** Each row keeps its source tab's
   row marks, keys and hints bar. `enter` and `b` follow the rule settled at
   charting: Ideate rows take the working tab's y/n, and Defog and Execute
   rows take the queue-tab refusals (ADR 0164 §4, ADR 0165).
   - Defog and Execute rows keep `space`, `d`, `x`, `+/-` and `E`.
   - Ideate rows keep `d`, `x` (ADR 0171 §3), `z`, `e`, `E` and `+/-`, and bind `p`
     (ADR 0171 §3), hinted just before `x`.
   - `n`, `a` and `A` are not bound in the focus view: new Signals and
     structure edits stay on their tabs.
   - Section headers and Charts start unfolded. Checkpoints start folded, as
     on Execute.
   - Every hints bar ends `esc back` · `? keys`.
6. **Badge** (amends ADR 0173 §1 and §2).
   - The back bar's ●N counts only live HITL sessions in the Initiative's
     scope that wait on the human (your turn or needs approval), the same
     rule ADR 0171 §5 gives every tab badge. Ready, unlaunched leaves don't
     count.
   - Because ●N now equals the scoped waiting count, `N waiting` leaves the
     back bar's crew counts. They read `N HITL · N AFK · N‖`.
   - No tab's badge changes while the view is open.
7. **Changes under the view.** Only `esc` or a tab-switch key leaves the
   focus.
   - When the Initiative closes (done or cancelled), the view stays open. The
     header shows it closed, and its members fold into their '▸ N closed'
     rows.
   - When the Initiative leaves the snapshot, the body shows one dim line:
     "the Initiative left the graph — esc returns".
   - A released member's row goes on the next refresh. A withdrawn member's
     head moves into its section's closed fold. When a successor is born (a
     Signal triaged into a Chart, the next phase cut), each bead moves to the
     section its kind says.
   - The cursor follows its bead by key while the bead has a row. Otherwise
     it goes to the nearest row above.

## Considered Options

- **`i` on Initiative header rows only.** Rejected. It would leave the member
  rows a human is looking at without a way in.
- **Keep `i focus` in Ideate's Signal-row hints.** Rejected. That row's hints
  are already long (ADR 0171 §3), and `?` lists the key.
- **The header's `enter` does nothing, as on Ideate (ADR 0172 §1) and the
  Defog and Execute headers.** Rejected. Cutting the next phase is the
  decision a drained Initiative waits on.
- **Ideate's Backlog routing for P3/P4 member Signals.** Rejected for the
  focus view. Ideate's own Backlog keeping parented Signals is split off as
  offshoot Signal 'Ideate Backlog holds only unparented P3/P4 Signals'.
- **Place a Task or Pivot by its Checkpoint's Initiative**, or in both
  Initiatives. Rejected. A bead belongs to at most one Initiative, and its own
  membership wins. A blocker highlight (Signal 'Bead list view tabs should
  highlight blockers on select') is the planned way to see an outside
  blocker.
- **Leave out defogged Charts that have not landed**, as the Defog tab does.
  Rejected. No queue tab would show them.
- **●N counts waiting and ready** (ADR 0173 §1 as written). Rejected, to
  match ADR 0171 §5.
- **Keep both ●N and `N waiting`, or drop ●N.** Rejected. ●N beside the title
  matches every tab's badge, and showing the same number twice is noise.
- **Leave the focus automatically when the Initiative closes.** Rejected. The
  human may want to read the closed result, and esc is one key.
- **Remember the cursor and folds per Initiative between entries.** Rejected.
  A fresh view is predictable.
