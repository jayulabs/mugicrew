# How far the existing seams carry one bead-verb router and its coverage test

Status: Research (input to the Spec of Chart "Bead verbs offered wherever a bead row appears", mc-ak2l0)
Date: 2026-10-28
Bead: mc-ak2l0.3

## Purpose

The Chart settled that the Spec requires one shared bead-verb router that every
bead-row tab calls, plus a coverage test that fails when a tab's bead row does not
route a bead verb. This doc reads `apps/mugicrew/tui` to answer four questions:

1. Can `actionKey` and `graphKey` (with `formBindings` and `formRefusal`) merge into
   one router, or must one wrap the other?
2. Where would each tab's pre-routing call the router instead, and what must the
   exempt keys (enter, b, d, space, i) keep?
3. Can `modeBindings` and the binding-coverage test be extended to assert, per tab,
   that every bead-carrying row routes and hints the same verb set as Graph?
4. What does such a test need?

All line numbers are at `758024d9` (this branch's base).

## 1. What the two shared paths are today

**`actionKey`** (`tui/actions.go:45-86`) owns enter, b, p, x, X and +/-. It picks an
act by key, then applies it to `m.selection()` (`actions.go:70`). It handles its keys
unconditionally: with nothing selected it sets "nothing selected" and returns
`ok=true` (`actions.go:77-83`). X has one special fallback. When the selection is nil
and the cursor rests on an Initiative header, X acts on `m.initRow()` (`actions.go:71-76`).
`initRow` resolves Ideate's `cursor().init`, Defog's `dHead` and Execute's `eSec` to
their Initiative (`actions.go:743-760`). The per-verb refusals live inside each act
(`togglePause` `actions.go:622-658`, `dropKey` `actions.go:354-397`, `bumpPriority`
`actions.go:666-688`, `withdrawKey` in `xdialog.go`).

**`graphKey`** (`tui/graphforms.go:192-270`) owns z, e, c, a, A, B, U. It handles a
key only if `formBindings(m.beads, b)` binds it on the selection (`graphforms.go:195-199`),
or if `formRefusal` has a reason for it (`graphforms.go:200-205`). Otherwise it returns
`ok=false`. The comment states the design rule: "Gating through formBindings keeps
handled == bound" (`graphforms.go:190-191`). `formBindings` (`graphforms.go:131-155`) is
the single hint/gate list for those seven keys. `formRefusal` (`graphforms.go:159-188`)
covers a, A, z, B, U (and c only when nothing is selected).

**Graph calls both, in order**: `graphKey` first, then `actionKey`
(`tui/shell.go:509-515`).

**Both read the bead from `m.selection()`**, not from a parameter (`actions.go:70`,
`graphforms.go:193`). `selection()` is already tab-aware and focus-aware: with the focus
open it returns the focus cursor's bead, otherwise the active tab's `selected()`
(`shell.go:158-166`). So the two paths already work from any tab. What gates them
today is only the pre-routing in each tab (§3).

## 2. Merge or wrap

**They can sit behind one router function without changing either body.**

- Both have the same signature `(tea.Model, tea.Cmd, bool)` (`actions.go:45`,
  `graphforms.go:192`).
- Their key sets do not overlap: actionKey matches keyLaunch, keyBg, keyPauseKey,
  keyDropKey, keyWithdrawKey, keyPrio (`actions.go:48-66`), and graphKey matches
  formBindings' z e c a A B U (`graphforms.go:131-155`).
- Graph's own dispatch (`shell.go:510-515`) already is that router: graphKey, then
  actionKey.

**A full merge into one function is not free.** The two "handled" conventions differ:

- actionKey claims its keys whatever the state (`actions.go:77-83`), and its acts
  refuse on the status line.
- graphKey claims a key only when bound or refusable (`graphforms.go:200-205`). For
  example, `e` with nothing selected returns `ok=false`, because formRefusal has no `e`
  case (`graphforms.go:159-165`).

Folding them together would mean picking one convention per key. Keeping each key's
own gate table is less risky.

**actionKey must be split, or the router must wrap only part of it.** enter and b are
exempt (they keep tab-defined meanings), yet they live in actionKey (`actions.go:49-57`).
Every tab's pre-routing calls actionKey for enter and b after its own refusals:

- Ideate: `ideatab.go:980-981`
- Defog and Execute: `queueRowKey` (`defogtab.go:767-782`)
- Focus: `focusAction` (`focus.go:523-529`)

The recommended shape:

- `beadVerbKey(k)` covers p, x, X, +/- (actionKey's acts), z e c a A B U (graphKey),
  and w (new, §5).
- actionKey keeps enter and b as the launch path that tabs call after their exempt-key
  handling.

In other words the router wraps graphKey whole and wraps actionKey's verb cases. It does
not absorb the launch cases.

**The row bead must include header rows.** The Chart settled that a header naming a
bead is that bead's row. Today `selected()` is nil on such headers:

- Ideate: `ideatab.go:805-806`
- Defog: `defogtab.go:644-648`
- Execute: `exectab.go:855-859`

Only X sees through them, via the `initRow` fallback (`actions.go:71-76`). Generalizing
that fallback to every verb is a one-line change: a `rowBead() = selection() ?? initRow()`
that the router reads. The other bead-row surfaces already resolve to a bead:

- The focus header's `r.b` is the Initiative (`focus.go:107`).
- Initiatives-list rows' `selected()` is the Initiative (`initstab.go:290-297`).

Changing `selected()` itself is the riskier option. Its nil on headers drives
enter/b's header handling, which is exempt: Ideate `ideatab.go:958-972`, queueRowKey
`defogtab.go:768`.

**Precedent for a shell-level router.** t transcript and r retrospective are already
routed once in the shell, ahead of every tab's pre-routing:

- t: `shell.go:441-446`, gated `m.focus.id != "" || m.tab <= tabInits`
- r: `shell.go:447-449`

r's hints are split into hinted and extra by `retroKeys` (`retro.go:38-50`). They are
spliced into every tab's bindings by `tabBindings` (`shell.go:718-725`) and `tabExtra`
(`shell.go:94-103`).

A bead-verb router could take the same slot: match a bead-verb key on a bead-row
surface, then route on `rowBead()`. That would make the per-tab pre-routing irrelevant
for verbs, and a new tab would get the verbs for free. The gate `m.tab <= tabInits`
covers exactly Ideate, Defog, Execute, Graph and Initiatives (`shell.go:39-50`). It also
keeps the keys off History, Stats and Logs, where `w` means window (`stats.go:24`) and
`e` means errors (`logs.go:31`).

The one ordering constraint is Graph's typing filter. It already captures every key
earlier (`shell.go:423-425`), so the slot after `keyYank` (`shell.go:450`) is safe.

## 3. Per-tab pre-routing: what each tab routes today and where the router goes

| Surface | Entry | Routes today | Bead verbs not routed | Where the router call goes |
|---|---|---|---|---|
| Graph | `shell.go:509-515` | graphKey (z e c a A B U) then actionKey (enter b p x X +/-) | w | Replace both calls with router, then actionKey for enter/b |
| Ideate | `ideaKey` `ideatab.go:942-990` | enter b +/- p X → actionKey; x only when `xHint != ""` (`ideatab.go:982`); e z a A → graphKey (`ideatab.go:986-987`); on an Initiative header only +/- (`ideatab.go:973-975`) and X (`976-977`) | c, B, U, w on Signal rows; p, x, e, c, B, U, w on Initiative headers | After the exempt enter/b cases (`ideatab.go:958-972`), replace `ideatab.go:973-988` with one router call on `rowBead()` |
| Defog | `defogKey` `defogtab.go:736-758` → `queueRowKey` `defogtab.go:764-791` | p x X +/- → actionKey (`defogtab.go:787-788`); X on header (`783-784`); w only on `dMR` rows (`defogtab.go:749-752`) | z e c a A B U; w off MR rows; everything but X on the Initiative header | Shrink queueRowKey to enter/b (`767-782`) and call the router after it; `dMR`'s w becomes the router's w |
| Execute | `execKey` `exectab.go:947-969` → `queueRowKey` | as Defog; w only on `eMR` (`exectab.go:960-963`) | as Defog | as Defog |
| Initiatives list | `initsKey` `initstab.go:107-120` | space only; enter → `focusOpen` in the shell (`shell.go:459-463`) | every bead verb | Call the router where `selected() != nil` |
| Focus view | `focusKey` `focus.go:440-517` | c on fBead/fHead (`499-501`); enter/b via `focusAction` (`502-507`); X on fHead (`508-509`); Ideate members → `focusIdeateKey` (`533-543`: p, x-when-hinted, +/-, X, e, z); other members → `queueRowKey` (`512-514`: p x X +/-) | fHead: p, +/-, e, z, B, U, w; Ideate members: c (routed at 499), a, A, B, U, w; Defog/Execute members: z, e, a, A, B, U, w | One router call for `r.kind == fHead \|\| r.kind == fBead`, replacing `499-501`, `508-514`; delete `focusIdeateKey` |

Notes on the table:

- **focusKey swallows every unhandled key.** It ends `return m, nil, true`
  (`focus.go:516`), and the shell only reaches the tab path when focusKey returns
  false (`shell.go:453-457`). So with the focus open, no tab pre-routing ever runs.
  The focus branch inside ideaKey (`ideatab.go:946-948`) is therefore unreachable.
  Every `shellKeys` entry (`shell.go:214`) is handled earlier in `Update`
  (`shell.go:426-452`, `464-490`), so none falls through to ideaKey. Swallowing is
  fine for the router: focusKey can call it and keep its trailing `true`.
- **x gating differs today.** Ideate and the focus's Ideate members route x only when
  hinted (`ideatab.go:982`, `focus.go:535`). Graph and queueRowKey always route it, and
  `dropKey` answers with a status line (`actions.go:387-396`). Graph also lists x in
  `extra` when unhinted (`graphtab.go:229`). Under "gated only by bead state", Graph's
  behaviour is the one to keep.
- **What the exempt keys keep:**
  - **enter and b** keep each tab's own refusals and verbs:
    - Ideate: create row, cut session, header refusals (`ideatab.go:958-972`)
    - Defog: `dFin` (`defogtab.go:753-756`)
    - Execute: `eLanded` (`exectab.go:964-967`)
    - queueRowKey: `rowEnterWhy`/`rowBgWhy` (`defogtab.go:767-782`)
    - Focus: `focusAction` (`focus.go:502-507`, `523-529`)
    - Initiatives: enter means focus (`shell.go:459-463`)

    None of these needs to move. They stay ahead of, or beside, the router.
  - **space** is handled before any verb by `defogKey`/`execKey`/`ideaKey`/`initsKey`
    (`defogtab.go:741-748`, `exectab.go:952-959`, `ideatab.go:950-957`,
    `initstab.go:108-120`) and in focusKey (`focus.go:479-481`).
  - **d** and the scroll keys stay in each tab's `update` and focusKey
    (`focus.go:482-498`).
  - **i** never reaches a tab: it is in the goto menu.

  None of the exempt keys collides with a bead-verb key.

## 4. The hint side: one list Graph already builds inline

Graph's per-bead verb hints are built inline in `graphTab.bindings`
(`graphtab.go:187-209`), in this order:

- p (if unclosed)
- x (if `xHint`)
- X (if `xOffers`)
- t
- formBindings' z and e
- +/- (if unclosed)
- formBindings' c a A B U

Every other surface re-derives its own subset:

- **Ideate** (`ideatab.go:884-907`): z, p, x, X, t, e and +/- (unconditionally), and a
  merged `keyAdoptA` (`ideatab.go:813`). No c, B or U.
- **Defog** (`defogtab.go:694-706`) and **Execute** (`exectab.go:906-918`): p, x, X, t,
  +/-. No form verbs. On headers only X cancel (`defogtab.go:688-692`,
  `exectab.go:900-904`).
- **Initiatives** (`initstab.go:315-326`): enter, d, t only.
- **Focus** (`focus.go:622-660`): Ideate members get z, p, x, e, +/-. Defog/Execute
  members get p, x, +/-. Any bead row gets X and c. t is inserted after x or p.

Extracting `graphTab.bindings`' lines `187-209` into one function would give the router
its hint list. It takes the bead and the shell's `gateData()` (`shell.go:150-155`), for
example `(g graphData) beadVerbBindings(b) []key.Binding`. Graph and every other
surface would splice that list in.

Order on the bar is safe to share. `sortHints` sorts only by section, stably
(`hints.go:174-184`). `hintOf` already places every bead verb's key in the row section,
except `w`: it goes to the tab section with the default rank (`hints.go:71-72`,
`hints.go:29`). The Spec has to decide w's placement and rank if it should sit with the
row verbs.

Graph's separate `a`/`A` are merged at render by `mergeAdopt` (`hints.go:96-107`), so
switching Ideate from `keyAdoptA` to Graph's pair renders the same bar. But tests that
read the raw `tabBindings` text change, because `hintsOf` (`actions_test.go:131-138`)
does not merge.

The `extra` lists need the same treatment. Graph's `extra` already lists every
actionKey and form key it handles when unhinted (`graphtab.go:229`). Ideate, Defog,
Execute and focus each list only their own subset:

- Ideate: `ideatab.go:931`
- Defog: `defogtab.go:722-725`
- Execute: `exectab.go:933-936`
- Focus: `focus.go:676-682`

Shared extras would follow the `retroKeys` hinted/extra split (`retro.go:38-50`).

## 5. w: what moving it into the router needs

`w` today is a row-kind key. It is bound on `dMR`/`eMR` only (`defogtab.go:680-682`,
`exectab.go:892-894`) and routed through `openMRRow(w, cp)` (`browser.go:69-79`). That
call reads the Watch's `MR link:` line via `git.RecordedMRLink` (`browser.go:83`,
`git/git.go:821-824`), and uses the container `cp` only to name a nested repo's MR
(`bd.ContextOf(*cp)`, `browser.go:73-75`).

The Chart settled that w is gated by the bead's own `MR link:` line. So the router's
gate is `git.RecordedMRLink(b.Description) != ""`. That holds on Watch and Landing
rows on every tab, and the `dMR`/`eMR` rows' `r.b` is already the Watch
(`defogtab.go:38`).

What the router lacks is `cp` for the nested-repo ref. It would have to resolve the
container from the bead's Parent (or pass "" and accept the plain `!iid` form). That is
an implementation choice for the Spec.

`openMR` refuses "no link yet" when the link is empty (`browser.go:83-87`). Under a
link-gated router that refusal is only reachable from an `extra` binding.

## 6. Can modeBindings and the coverage test carry the parity assertion?

**What the existing test asserts.** `checkCoverage` (`shell_test.go:386-411`) proves,
for one Model state, that over every candidate key (`shell_test.go:342-356`) "handled"
equals "bound". Handled means a Cmd came back or the frame changed (`shell_test.go:398`).
Bound means the key is in `modeBindings()` (`shell.go:654-665`): tab bindings, tab
extra, goto, new and shell keys. The test is a per-state consistency check between a
surface's handler and its own binding list. It says nothing about Graph.

A tab that neither hints nor handles `B` passes today. That is exactly the drift the
Chart wants caught.

**modeBindings cannot be extended to carry parity by itself.** It is the union a mode
handles. Parity needs a second, independent oracle: Graph's verb set for the same bead.
Keep `checkCoverage` as is (it still guards handled == bound after the change) and add a
separate parity test.

**The Graph oracle is constructible today without switching tabs.**
`graphTab{graphData: g, sel: id}` yields Graph's bindings for any bead. Its
`selected()` is index-based (`graph.go:643-651`), so it works even when Graph's filter
would hide the bead. The pattern is already used at `initstab.go:310` and
`focus.go:566`. If the router's hint list is extracted (§4), the oracle must stay
Graph's bindings filtered to bead-verb keys, not the extracted function itself. That
way the test still catches a surface that drops or re-orders the shared list.

**Existing per-row coverage callers and fixtures** exist on every bead-row surface:

| Surface | Coverage test | Fixture | Cursor helper |
|---|---|---|---|
| Ideate | `ideatab_test.go:384-395`, `ideadrained_test.go:159` | `keysModel` | `ideaAt` (`ideatab_test.go:229`) |
| Defog | `defogtab_test.go:392-399` | `defogActModel` | `defogAt` (`defogtab_test.go:272`) |
| Execute | `exectab_test.go:365-371` | `execModel(execFixture())` | `execAt` (`exectab_test.go:197`) |
| Initiatives | `initstab_test.go:381-384` | — | — |
| Focus | `focus_test.go:446-460` | `defaultFocus`/`ideaFocus` | `focusAt` (`focus_test.go:95`) |
| Graph | `actions_test.go:403-420` | `actModel` (`actions_test.go:113-125`, with `stubActions` seams) | `selectBead` (`graph_test.go:59-69`) |

The `TestBindingCoverage` walk over every tab (`shell_test.go:362-384`) loads
`stubBeads()` (`graph_test.go:26-38`). That fixture has no Signal or Initiative-headed
queue rows, so it exercises no bead row on Ideate.

**There is a latent bug the parity test would surface: c on a Checkup row on Graph.**
`graphTab.extra` always lists `keyCheckup` when it is not hinted (`graphtab.go:229`).
But on a Checkup bead `canCheckup` is false (`checkup.go:24`), so formBindings leaves c
out, and `formRefusal` has no c case for a non-nil bead (`graphforms.go:166-187`). The
result is that graphKey returns `ok=false` and nothing else handles c.

A throwaway probe (not committed) confirmed it. It loaded `stubBeads()` plus one open
Checkup under `ini`, selected it on Graph, and pressed c. Output:
`c on Checkup row: bound=true handled=false` (run with `CGO_ENABLED=0 go test -run
TestZZProbeCheckupC -v ./tui`). No existing Graph coverage fixture includes a Checkup
(`actionBeads`, `actions_test.go:100-111`). The fix is a c case in formRefusal, or
dropping c from extra on a Checkup. It belongs in the router's gate table either way.

## 7. What the parity test needs

1. **One enumeration of bead verbs.** No such list exists. The nearest is Graph's
   `extra` candidates (`graphtab.go:229`), which include the exempt enter and b and
   omit e and w.

   The Spec's list is z, e, a, A, c, B, U, +/-, p, x, X, w (Chart Destination). A
   package var such as `beadVerbs []key.Binding` would serve the router's key match
   (§2), the hint extraction (§4) and the test, so a verb added to one is added to all.
   `keyPrio` binds four keys (`graphtab.go:28`), so the test must iterate `Keys()`, not
   bindings.
2. **Row enumeration per surface, not fixed ids.** Each surface exposes its rows:
   - `ideaTab.rows()`, cursor via `cursorIn` (`ideatab.go:801`)
   - `defogTab.selRow`/rows (`defogtab.go:640-648`)
   - `execTab.selRow` (`exectab.go:850-859`)
   - `initsTab.rows()` (`initstab.go:291-297`)
   - `m.focusRows()` (`focus.go:101`)

   Walking j over every row and testing each one where `rowBead() != nil` makes a newly
   added row kind covered automatically. A fixed id list, as the current coverage tests
   use, would not.
3. **A fixture that yields every bead-carrying row kind.** That is:
   - Ideate: Signal rows, act/live twins, and combined or drained Initiative headers
     (`ideatab.go:172-187`)
   - Defog: `dHead`…`dMR` (`defogtab.go:24-39`)
   - Execute: `eSec`…`eMR` (`exectab.go:26-41`)
   - Initiatives rows
   - Focus: `fHead`/`fBead` (`focus.go:43-50`)
   - at least one bead whose Description carries an `MR link:` line, for w
   - an open Checkup, for c

   The existing per-tab fixtures each cover one tab. A parity test can reuse them per
   surface, because the Graph oracle reads the same Model's `gateData()`.
4. **Two assertions per (row, verb).**
   - (a) **Hint parity.** The bead-verb keys in `m.tabBindings()` (`shell.go:718-725`)
     equal those in the oracle's bindings, compared as sets or in order, as the Spec
     decides.
   - (b) **Route parity.** Pressing the key on the surface and on Graph-with-`sel`-set
     gives the same handled bit and the same observable outcome (status line, top
     modal title, and calls recorded by the `stubActions`/`seam` write stubs). Comparing
     outcomes, not just handled, is what catches pre-routing that refuses differently,
     such as Ideate's silent unhinted x (§3).
5. **An explicit allowlist for special-role rows,** if the Spec gives them per-row-kind
   calls: Defog's status-only `dCW`/`dFin`, Execute's `eLanded`/`eCPS`/`eSPS`, and the
   MR Watch rows. Those are the Chart's open Not-yet-specified item. The allowlist keeps
   their exceptions visible in one place instead of as silent gaps.

## Open questions / gaps

- **Is t a bead verb?** Graph hints t on every bead row (`graphtab.go:197`), and the
  shell already routes it for every bead-row surface (`shell.go:441-446`). But the
  Chart's verb list omits it. The parity test should either include it or exempt it
  explicitly.
- **Rows with no bead:** what do verb keys do there? Graph answers "nothing selected"
  for actionKey keys and most form keys (`actions.go:77-83`, `graphforms.go:160-165`).
  Defog and Execute leave them unhandled (`defogtab.go:785-786`). The Chart says such
  rows "offer no bead verbs". The Spec must say whether that means unbound and silent,
  or bound with a status refusal, because `checkCoverage` requires one or the other.
- **Nested-repo MR ref for w** off `dMR`/`eMR` rows needs the container
  (`browser.go:73-75`). Should the router resolve it from Parent, or show the plain
  `!iid`?
- **w's hint placement and rank:** `hintOf` sends w to the tab section at the default
  rank (`hints.go:71-72`). Should it join the row section?
- **Shell slot vs per-tab calls (§2):** this doc recommends the shell slot. It was not
  prototyped. Behaviour under an open drawer (Initiatives `drawerOpen`,
  `initstab.go:322-323`) and under focus should be checked against the existing
  drawer-open coverage runs (`focus_test.go:453-454`, `initstab_test.go:384`).
- **Stale entries noticed in passing:**
  - The "r" entry in Graph's in-section split (`graphtab.go:201`) never matches:
    formBindings yields no r since rebase moved to the New menu (ADR 0144
    (tui-v2-per-tab-keys-detail-on-d-and-hints-bar), 2026-10-17 amendment).
  - `focus.go:513` and `ideatab.go:5` still mention E, which ADR 0258 removed as a row
    key.
- **Not run:** the full `go test ./tui` suite. Only the one throwaway probe above was
  run, and it was deleted afterwards.
