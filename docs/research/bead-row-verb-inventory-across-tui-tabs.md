# Bead-row verb inventory across the TUI's bead tabs

Status: Research (input to the Spec for Bead verbs offered wherever a bead row appears, mc-ak2l0)
Date: 2026-10-28
Bead: mc-ak2l0.2

## Purpose

The Chart Bead verbs offered wherever a bead row appears has one Not-yet-specified
item: rows that carry a bead in a special role (Defog's status-only rows,
Execute's landed Checkpoint, the MR rows whose bead is the MR Watch). This doc
answers it. For every row kind on Ideate, Defog, Execute, Graph, the Initiatives
list and the Initiative focus view, it records:

- which bead the row resolves to;
- which of the bead verbs `z e a A c B U +/- p x X w` the shared gates would offer
  on it, by state;
- what the tab routes and hints today;
- every divergence between the two.

All paths are under `apps/mugicrew/tui/` unless they are qualified. Line numbers
are at the effort branch's cut (HEAD `758024d9`).

## 1. The shared gates (the reference)

Graph is the one tab where every bead verb reaches the shared paths. The shell
calls `graphKey` and then `actionKey` only for `graphTab` (`shell.go:509-514`).
The gates, by verb:

| verb | hint gate (what Graph's bar shows) | press path | press-time refusal |
|---|---|---|---|
| `z` reject | `rejectable`: open, unclaimed Signal (`graphforms.go:53-55`, `:133`) | `graphKey` → reject dialog (`graphforms.go:229-231`) | `formRefusal` (`graphforms.go:177-178`) |
| `e` edit | any bead (`graphforms.go:136-138`) | `graphKey` → `editSignalForm` (`:232-233`) | none needed |
| `a` adopt | `canAdopt`: unclosed, not an Initiative, some open Initiative exists (`graphforms.go:58-60`) | `graphKey` → picker (`:234-247`) | `formRefusal` (`:167-174`) |
| `A` release | `canRelease`: unclosed, parent is an Initiative (`graphforms.go:63-65`) | `graphKey` → `releaseChain` (`:208-221`) | `formRefusal` (`:175-176`); `ReleaseRequiredWhy` (`:209-211`) |
| `c` checkup | `canCheckup`: any bead but a Checkup, open or closed (`checkup.go:23-24`, ADR 0274) | `graphKey` → `checkupKey` (`graphforms.go:226-228`, `checkup.go:34-44`) | none from `formRefusal` for a Checkup: the key falls through, silent |
| `B` add blocker | `canBlock`: unclosed, not deferred (`graphforms.go:68`) | `graphKey` → picker (`:248-255`) | `formRefusal` (`:179-183`) |
| `U` unblock | `canUnblock`: at least one open blocker. It does not test the bead's own status (`graphforms.go:100-102`) | `graphKey` → picker (`:256-267`) | `formRefusal` (`:184-185`) |
| `+/-` prio | the bead is unclosed (`graphtab.go:206-208`) | `actionKey` → `bumpPriority` (`actions.go:64-66`) | refuses a closed bead and the P0/P4 bounds (`actions.go:667-675`) |
| `p` pause | the bead is unclosed; the label comes from `pauseLabel` (`graphtab.go:188-190`, `:147-152`) | `actionKey` → `togglePause` (`actions.go:58-59`) | refuses a closed bead (`actions.go:623-626`) |
| `x` drop/abort/recover | `xHint` is not "" (`graphtab.go:191-193`, `:122-145`) | `actionKey` → `dropKey` (`actions.go:60-61`) | `dropKey` says why on every unhinted state (`actions.go:354-397`) |
| `X` withdraw/cancel | `xOffers`: a withdrawable head or an open Initiative (`xdialog.go:119`); `xBinding` picks the label (`xdialog.go:41-46`) | `actionKey` → `withdrawKey` (`actions.go:62-63`) | `withdrawKey` says why (`xdialog.go:122-133`) |
| `w` open MR | not on Graph. Defog and Execute hint it on every MR row with no gate (`defogtab.go:680-682`, `exectab.go:892-894`) | `openMRRow` (`browser.go:69-79`) | `openMR`: "no link yet" when the body has no `MR link:` line (`browser.go:82-87`) |

The hint gates for `z e a A c B U` all come from `formBindings`
(`graphforms.go:131-155`). `graphKey` routes only a key that `formBindings`
binds (`graphforms.go:195-206`), so on Graph a handled form key is always a
bound one. `Graph.extra` lists every routed but unhinted action key for `?`
(`graphtab.go:215-238`).

`w`'s gate under the Chart's rule would be `git.RecordedMRLink(b.Description) != ""`
(`git/git.go:820-821`). Today no hint calls it. `RecordedMRLink` is called only
when `w` is pressed (`browser.go:83`) and when the detail drawers render
(`defogdetail.go:113`, `exectabdetail.go:124`).

`selection()` sets which bead every shared path acts on. Under the focus view it
is the focus cursor's bead; otherwise it is the active tab's `selected()`
(`shell.go:157-166`). A tab's row reaches the shared gates with the right bead
only if `selected()` returns that bead.

### 1.1 The per-key guard, and the `A` order

Every write goes through `startWrite`, which calls `refusePending` first
(`live.go:865-868`, `live.go:910-916`). `startAct` wraps `startWrite`
(`live.go:890-896`). `togglePause` and `bumpPriority` use `startAct`
(`actions.go:652`, `:677`). `dropKey` and `withdrawKey` check `busyWhy` first,
and `busyWhy` includes `pending` (`graphtab.go:42-52`, `actions.go:355`,
`xdialog.go:123`). `graphKey` calls `refusePending` before it opens the
`z e a B U c` dialogs (`graphforms.go:222-224`), and `checkupKey` calls it again
(`checkup.go:35`).

`A` is handled at `graphforms.go:208-221`, which is before that
`refusePending` call. So `A` skips the explicit up-front check. It does not skip
the guard: its `startAct` → `startWrite` still refuses a pending bead. The only
effect is the order of the checks. If a write is pending and
`initiatives.Required` makes `ReleaseRequiredWhy` refuse, the status line shows
the Required refusal and not "write in flight, wait". `A` opens no dialog, so no
dialog can open over a pending write either.

## 2. Graph (`graphTab`)

| row kind | bead | routing and hints |
|---|---|---|
| any bead row: Signal, Chart head, Initiative head, members, closed rows | its own (`graph.go:643-651`) | the reference (§1): every verb is routed and gated. `w` is not routed. |
| Unparented group header | none (`graph.go:644`) | `actionKey` says "nothing selected" plus an Unparented note (`actions.go:77-82`). `formRefusal` says "nothing selected" for `a A z B U c` (`graphforms.go:160-164`). |

Under the Chart's rule, Graph's only gap is `w`. ADR 0333 §2 keeps MR URLs out
of every place but the two Merge Requests sections; the Chart reverses that.

## 3. Ideate (`ideaTab`, `ideaKey` at `ideatab.go:942-990`)

`ideaTab.selected()` is the cursor's `b`, nil on every header and on the create
row (`ideatab.go:806`). An Initiative row has `b` nil and `init` set
(`ideatab.go:172-190`). `initRow()` returns the cursor's `init` on Ideate
(`actions.go:748-749`).

| row kind | bead | `z` | `e` | `a`/`A` | `c` | `B`/`U` | `+/-` | `p` | `x` | `X` | `w` |
|---|---|---|---|---|---|---|---|---|---|---|---|
| Signal row (tree, Backlog, Actionable/Live twins) | its own Signal | ✓ | ✓ | ✓ (one `a/A` hint when both apply) | ∅ | ∅ | routed; hinted **with no gate** | ✓ | routed **only when hinted** | ✓ | ∅ |
| Initiative header, drained header, drained cue row, combined Initiative row (`c.init != nil`) | the Initiative, through `c.init` only | n/a | ∅ | n/a | ∅ | ∅ | routed straight to `bumpPriority`; hinted **with no gate** | ∅ | ∅ | ✓ through `initRow` | ∅ |
| section header, No Initiative header, Backlog header, placeholder, create row | none | · | · | · | · | · | · | · | · | · | · |

Legend for every table: ✓ means routed to the shared path with Graph's hint
gate. ∅ means not routed: the key is dropped without a word, and the shared gate
would offer it in some state. · means no bead, so no bead verb, which matches the
Chart's charting decision. n/a means the shared gate never offers the verb on
that row kind.

Evidence:

- Signal routing. `enter b +/- p` go to `actionKey` (`ideatab.go:980-981`). `x`
  goes there only when `xHint(b) != ""` (`:982-983`); otherwise it falls to
  `return …false` (`:989`) and then `tabKey`, which ignores it. Graph routes an
  unhinted `x` to `dropKey`, which says why (`actions.go:354-397`). `X` goes to
  `actionKey` (`:984-985`). `e z a A` go to `graphKey` (`:986-987`). `c B U` are
  not matched, so they fall through `ideaTab.update` without a word.
- Signal hints (`ideatab.go:884-908`). `z` is gated by `rejectable` (`:884`),
  `p` by `Unclosed` (`:887`), `x` by `xHint` (`:890`) and `X` by `xOffers`
  (`:893`). `e` and `+/-` are appended unconditionally (`:897`). The `e` hint
  matches the shared gate, since `formBindings` offers `e` on any bead
  (`graphforms.go:136-138`). The `+/-` hint does not: Graph hints it only on an
  unclosed bead (`graphtab.go:206-208`). Adopt and release come from `canAdopt`
  and `canRelease`, merged into one `a/A` binding (`ideatab.go:813`, `:898-907`).
  `c`, `B` and `U` are never hinted.
- Initiative rows. `+/-` calls `bumpPriority(*c.init)` directly
  (`ideatab.go:973-975`) and is hinted with no state test (`ideatab.go:876-877`).
  `X` goes to `actionKey`, which finds the Initiative through `initRow`
  (`ideatab.go:976-977`, `actions.go:70-76`), and is hinted when the Initiative
  is open (`ideatab.go:878-879`). `enter` and `b` launch the cut session
  (`ideatab.go:960-962`). Every other key returns false (`ideatab.go:978-979`).
  On an open Initiative the shared gate would also offer `e`, `c`, `B`, `U`
  (with blockers), `p`, and `x` (with a cut session). `xLabel` gives "drop" for
  any session (`graphtab.go:128-129`).

## 4. Defog (`defogTab`, `defogKey` at `defogtab.go:736-758`)

`defogTab.selected()` is the row's `b` (`defogtab.go:645-648`). `defogKey`
handles fold, `w` on `dMR`, and `enter`/`b` on `dFin`, then hands every other
key to `queueRowKey` (`defogtab.go:741-757`). `queueRowKey` routes `enter`, `b`,
`p`, `x`, `+/-` and `X` (`defogtab.go:764-791`). For a header it routes only
`X`, and only when `initRow` finds an open Initiative (`:783-784`). It returns
false for every form key, so `z e a A c B U` are dropped without a word on every
Defog row.

The hints (`defogtab.go:663-708`) gate `p` and `+/-` on `Unclosed`, `x` on
`xHint` and `X` on `xOffers`. They hint `w` on every `dMR` row and never hint a
form key.

| row kind (`defogtab.go:26-39`) | bead | `z e a A c B U` | `+/-` `p` `x` `X` | `w` | special role |
|---|---|---|---|---|---|
| `dHead` with an Initiative | the Initiative (`r.init`; `selected()` is nil) | ∅ | `X` ✓ through `initRow` (`actions.go:750-753`), hinted `keyCancelKey` when open (`defogtab.go:689-691`); `+/- p x` ∅ | ∅ | a header that names a bead |
| `dHead` No Initiative, `dSec`, `dPlace`, `dSum` | none | · | · | · | none |
| `dChart`, `dLeaf`, `dChild`, `dAct`, `dLive`, `dACW`, `dACA` | its own | ∅ | ✓ | ∅ | none |
| `dCW` (Charts section, "status only", `defogtab.go:35`) | its own open Chart | ∅ | ✓, routed and hinted like any bead row | ∅ | "status only" is not enforced by any key: `enter` goes through `rowEnterWhy` like a tree Chart (`defogtab.go:654-659`) |
| `dFin` (Finished in last 24h, "status only", `defogtab.go:36`) | its own **closed** Chart (`defogtab.go:406`) | ∅ (the gate would offer `e` and `c` on it, and `U` if it has an open blocker) | routed; `p` and `+/-` are unhinted and refused at press; `x` only with a session; `X` never | ∅ | `enter`/`b` give `finRefuse` (`defogtab.go:753-756`) |
| `dMR` (Merge Requests) | the **MR Watch** (`r.b`); its Chart is `r.chart` (`defogtab.go:39`, `:360`) | ∅ | ✓ on the **Watch**: `p` pauses the Watch and `+/-` bumps the Watch. `X` is never hinted, because a Watch is neither a chain head nor an Errand (`bd/chainhead.go:23-28`, `bd/sweep.go:13-14`) | hinted with no gate, routed to `openMRRow(Watch, Chart)` (`defogtab.go:749-752`) | the row's bead is the Watch, not the Chart |

## 5. Execute (`execTab`, `execKey` at `exectab.go:947-969`)

Execute has the same shape as Defog. `execKey` handles fold, `w` on `eMR`, and
`enter`/`b` on `eLanded`, then calls `queueRowKey` (`exectab.go:952-968`). The
hints (`exectab.go:875-920`) are Defog's, with `w` on `eMR` (`:892-894`).
`selected()` is the row's `b` (`exectab.go:856-859`). For an `eSec` header,
`initRow` returns its `init` (`actions.go:754-756`).

| row kind (`exectab.go:27-40`) | bead | `z e a A c B U` | `+/-` `p` `x` `X` | `w` | special role |
|---|---|---|---|---|---|
| `eSec` that is an Initiative header | the Initiative (`r.init`) | ∅ | `X` ✓ (hint at `exectab.go:901-903`); `+/- p x` ∅ | ∅ | a header that names a bead |
| `eSec` section header or No Initiative header, `ePlace`, `eSum` | none | · | · | · | none |
| `eAct`, `eCP`, `eSpec`, `eMem`, `eSide`, `eClosed`, `eLive` | its own | ∅ | ✓ | ∅. Some `eSide` Errands carry an `MR link:` line (§6), so they would gain `w` under the rule | none |
| `eCPS` (Checkpoints, "status only", `exectab.go:37`) and `eSPS` (Specs, "status only", `:39`) | its own open Checkpoint or Spec (`exectab.go:411`, `:431`) | ∅ | ✓ | ∅ | "status only" is not enforced by any key |
| `eLanded` (Landed in last 24h) | its own **closed Checkpoint** (`r.b`). The Landing that landed it rides in `members[0]` (`exectab.go:516-529`) | ∅ (the gate would offer `e` and `c` on the Checkpoint, and `U` if it has an open blocker) | routed; `p` and `+/-` are unhinted and refused (closed); `x` only with a session; `X` never | ∅. The row's bead is the Checkpoint, which has no `MR link:` line. The Landing has one (`jinbe/jinbe.go:1083`, `:548`), but the Landing is not the row's bead | `enter`/`b` give `landedRefuse` (`exectab.go:964-967`, `:68`) |
| `eMR` (Merge Requests) | the **MR Watch**; its Checkpoint is `r.cp` (`exectab.go:452`) | ∅ | ✓ on the Watch, as on Defog's `dMR` | hinted with no gate, routed to `openMRRow(Watch, Checkpoint)` (`exectab.go:960-963`) | the row's bead is the Watch |

## 6. Which beads carry `MR link:`

Under the Chart's rule (`w` is gated by the bead's own `MR link:` line, read
through `RecordedMRLink`), these are the beads whose rows would offer `w`:

- **MR Watch.** It is created with the line (`jinbe/mrwatch.go:19-25`) and
  backfilled later (`jinbe/mrwatch.go:93`, `:176-180`).
- **Landing.** `setMRLines` writes it (`jinbe/jinbe.go:1083`) and
  `backfillMRLink` fills it in later (`jinbe/jinbe.go:548`, `:653-661`).
- **Rollback.** `setMRLines` writes it (`jinbe/jinbe.go:1516`).
- **Brook-owned hold Errands.** These are the closed-MR Errand
  (`jinbe/jinbe.go:603`), the trailerless merged-MR hold (`jinbe/jinbe.go:990`)
  and the pipeline-failure Errand (`jinbe/jinbe.go:882`). They show as Execute
  `eSide`/`eAct` rows and as Graph rows.
- **Forge Review bodies.** These write the line as part of the snapshot
  (`jinbe/forgereview.go:248`).
- **Not** Chart or Checkpoint beads. That matches the charting decision that a
  Chart or Checkpoint row does not look its MR up.

## 7. Initiatives list (`initsTab`)

| row kind | bead | every bead verb |
|---|---|---|
| Open / Closed section header | none (`initstab.go:33-39`) | · |
| Initiative row, open or closed | the Initiative (`initstab.go:291-297`) | ∅ for all 12 |

`initsKey` handles only `space` (`initstab.go:107-110`). `enter` opens the focus
view (`shell.go:459-463`, exempt). `t` is a shell key (`shell.go:441`). The
shell never calls `actionKey` or `graphKey` for this tab (`shell.go:505-508`),
and `initsTab.update` ignores the verb keys (`initstab.go:336-379`). The hints
are only `enter focus`, `d`, `t` and `J/K` (`initstab.go:315-326`).

The shared gates would offer these. On an open Initiative: `e`, `c`, `B`, `U`
(with blockers), `+/-`, `p`, `x` (with a cut session) and `X` cancel. On a
closed one: `e`, `c`, `U` (with an open blocker) and `x` (with a session).
`selected()` already returns the Initiative, so the shared gates would see the
right bead without a new seam.

## 8. Initiative focus view (`focusKey` at `focus.go:440-517`)

`selection()` is `focusBead()`, which is the cursor row's `b`. The header's `b`
is the Initiative (`focus.go:292-295`, `:107`). `focusKey` takes every
non-shell key while the focus is open: it returns `ok = true` at `focus.go:516`
for any key it did not match. It also drops `queueRowKey`'s `ok`
(`focus.go:512-514`), and `focusIdeateKey` returns true for anything unmatched
(`focus.go:542`). So an unrouted verb is swallowed without a word. The shell's
per-tab dispatch (`shell.go:491-525`), Graph's `graphKey`/`actionKey` included,
never runs under the focus.

| row kind (`focus.go:44-49`) | bead | `z` | `e` | `a`/`A` | `c` | `B`/`U` | `+/-` | `p` | `x` | `X` |
|---|---|---|---|---|---|---|---|---|---|---|
| `fHead` | the Initiative | n/a | ∅ swallowed | n/a | ✓ (`focus.go:499-501`) | ∅ swallowed | ∅ swallowed | ∅ swallowed | ∅ swallowed | ✓ `actionKey` (`focus.go:508-509`) |
| `fBead`, Ideate section | its own Signal | ✓ `graphKey` | ✓ `graphKey` | ∅ swallowed (ADR 0174 §5) | ✓ | ∅ swallowed | routed; hinted **with no gate** (`focus.go:632`) | ✓ | routed **only when hinted** (`focus.go:535`) | ✓ |
| `fBead`, Defog/Execute section, closed members under `▸ N closed` included (`focus.go:134`) | its own | ∅ swallowed | ∅ swallowed | ∅ swallowed | ✓ | ∅ swallowed | ✓ (`focus.go:641-643`) | ✓ | ✓ | ✓ |
| `fSec`, `fSum`, `fPlace`, `fGap` | none | · | · | · | · | · | · | · | · | · |

`w` is never routed in the focus view. It needs no MR rows to matter: under the
rule it would apply to any member row whose bead has a link, such as a hold
Errand on Execute's side.

Hints: `focusBindings` (`focus.go:574-660`). ADR 0174 §3 leaves `p`, `+/-` and
`x` unbound on the header, and §5 leaves `a`/`A` unbound. The Chart reverses
both. The case at `focus.go:605` is dead code: the earlier case at `:589`
already matches every Execute `fBead`.

## 9. Divergences from the shared gates

1. **The focus view swallows every unhandled key.** See §8. It is the only
   surface where an unrouted bead verb is taken and dropped, not passed on.
   Every other tab passes it to `tabKey`, which also ignores it.
2. **`A` runs ahead of `refusePending`** (`graphforms.go:208-222`). The per-key
   guard still holds through `startWrite`. Only the refusal wording differs
   under `initiatives.Required` (§1.1).
3. **Ideate hints `+/-` with no gate**, on Signal rows (`ideatab.go:897`),
   Initiative rows (`:877`) and focus Ideate rows (`focus.go:632`). Graph gates
   it on `Unclosed`. **Ideate's unconditional `e` is not a divergence**, because
   `formBindings` offers `e` on every bead.
4. **Ideate and focus-Ideate route `x` only when it is hinted**
   (`ideatab.go:982`, `focus.go:535`). Graph routes it always and says why
   (`dropKey`). An unhinted `x` is silent there.
5. **`c B U` are unrouted on Ideate, Defog, Execute and Initiatives.** `c` is
   routed only on Graph and in the focus view, although ADR 0274 says any bead
   row. `B` and `U` are routed only on Graph.
6. **`z e a A` are unrouted on Defog, Execute and Initiatives**, and on the
   focus view's Defog and Execute rows. `a A` are also unrouted on the focus
   view's Ideate rows (ADR 0174 §5).
7. **Rows that name a bead but carry it outside `selected()`.** These are
   Ideate's Initiative rows (`c.init`), Defog's `dHead` (`r.init`) and
   Execute's Initiative `eSec` (`r.init`). On them `selected()` is nil, so every
   shared path except `X` (through `initRow`, `actions.go:70-76`) sees "nothing
   selected", and Ideate's `+/-` bypasses `actionKey` entirely. A shared router
   needs one resolver from row to bead (see §10). Without it, these rows cannot
   reach `formBindings` or `actionKey`.
8. **The Initiatives list routes no bead verb** (§7).
9. **`w`** is routed only on `dMR` and `eMR`, and is hinted there whether or not
   a link exists (`defogtab.go:680-682`, `exectab.go:892-894`). Under the
   Chart's rule the hint gate would be `RecordedMRLink` and the route would
   cover every bead row (§6).
10. **The "status only" rows are not status-only for keys.** `dCW`, `eCPS` and
    `eSPS` route and hint `p x X +/-` and `enter` exactly like their tree twins.
    Only `dFin` and `eLanded` refuse `enter`/`b`, and they refuse with a fixed
    line (`finRefuse`, `landedRefuse`), not through the shared `rowEnterWhy`.
    Their remaining verbs are already gated by the closed state.
11. **`U` on a closed bead.** `canUnblock` does not test the bead's status
    (`graphforms.go:100-102`). A closed bead with an open blocker is offered `U`
    on Graph, and so would be a `dFin` or `eLanded` row under the rule. This is
    a shared-gate fact, not a per-tab one. It is noted for the Spec.

## 10. Rows in a special role: answer to the Not-yet-specified item

For each special-role row, the bead the shared gates should see:

| row | bead the shared gates should see | needs a per-row-kind call? |
|---|---|---|
| Ideate Initiative header, drained header, cue row, combined row; Defog `dHead`; Execute Initiative `eSec` | the Initiative (`init`) | **Yes, but only to resolve the bead.** `selected()` returns nil on these rows today, so the router needs "row → bead" to return `init`. After that, every verb is state-gated like Graph's Initiative row. `enter` and `b` stay tab-defined (exempt). |
| Defog `dCW`, Execute `eCPS` and `eSPS` | its own Chart, Checkpoint or Spec | No. They already pass their own bead, and only their status-only labels say otherwise. |
| Defog `dFin`, Execute `eLanded` | its own closed Chart or Checkpoint | Only for `enter`/`b`, which are exempt and already per-row (`finRefuse`/`landedRefuse`). The bead verbs fall out of the closed state: `e`, `c`, `U` (open blocker), and `x` (with a session). `eLanded`'s `w` stays off, because a Checkpoint has no `MR link:`. The Landing is reachable only as `members[0]`. |
| Defog `dMR`, Execute `eMR` | the MR Watch | No call on `w` beyond the link gate. Every other verb acts on the **Watch**, as `p x X +/-` already do. The Spec must accept that `p`, `+/-` and `B` on an MR row act on the Watch and not on its container. The other choice is a per-row redirect to `r.chart`/`r.cp`, which would contradict the charting decision that the row's bead is the Watch. |

## Open questions / gaps

- **The cue row.** The drained cue row on Ideate (`c.cue`) carries `init` and
  acts as the Initiative's row for `enter`, `+/-` and `X` today
  (`ideatab.go:960-977`). The charting decision names header rows only. Is the
  cue row that Initiative's row too, or a no-bead row?
- **Does Ideate ever list a closed Signal or a closed Initiative?** If it never
  does, divergence 3 is latent, not visible. The row builders were not traced
  here.
- **Landing rows.** It was not checked whether a Landing bead shows as an
  `eMem`/`eClosed` member row on Execute or in the focus view. If it does, that
  row gains `w` under the rule (§6).
- **What `enter` does on a Watch row** is `runNowWhy`'s fallback ("has nothing
  for enter to run", `graphview.go:1052`) unless the Watch has a run-now label.
  It is exempt from the rule, and was not verified against a live Watch.
- **`c` on a Checkup row is silent on Graph.** `formRefusal` has no `c` line
  (`graphforms.go:159-188`), so the key falls through. Whether the shared
  router should give it a refusal line is left to the router/coverage-test
  research (sibling bead How far the existing seams carry one bead-verb router
  and its coverage test).
- **Dead code at `focus.go:605`** should go when `focusBindings` is rewritten.
