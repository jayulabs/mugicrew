# ADR 0026: mugicrew TUI footer is the single context-sensitive command surface

Status: Accepted — view keys amended by ADR 0051 (`o` loop toggle); `?` key reference and G/I/L tab jumps by ADR 0058; `+/-` bead keys, `f` / `/` / `esc` view keys and the `+/-`-yields-first drop rule by a later Spec (FR11); `b bg` bead key and its first-dropped rule by ADR 0100 §5
Date: 2026-09-29

Chart: Legible TUI: blocked-bead cues + single command surface. Settles
grilling on top of the prototype verdict. **Supersedes ADR 0024
§1** (per-focus navigation-only help line + detail-pane `actionBar`); ADR 0024
§2–3 stand.

The detail pane's `actionBar` is removed and the footer carries every command,
showing only the keys that apply to the current tab × layout/focus × selection.
Split help (nav in the footer, actions in `actionBar`) meant two places to look
and an action list the master-focus single pane could not see. Inapplicable keys
are **hidden, not greyed**, to save width at the 40-col floor.

## Shape

`<bead keys> │ <view keys>`, joined by ` · `. No hints for `j/k`, `E`, or `←/→`
(the tab bar already shows the tabs). With no bead selected (Unparented
header, empty list) the bead side and the `│` are omitted.

## Bead keys (act on the selection, regardless of focus)

| key | shown when | label |
|---|---|---|
| `l` | HITL bead, status open or in_progress, not blocked | `l launch` |
| `l` | HITL bead that is blocked (dep-blocked ∪ status=blocked) | `l launch⚠` (press → confirm) |
| `b` | right after `l` where it reads `launch`, `launch⚠` or `resume`; first hint dropped when narrow (ADR 0100 §5) | `b bg` |
| `t` | Graph tab, run-now affordable (open, not blocked): AFK leaf | `t run` |
| `t` | … drained Chart | `t spec` |
| `t` | … land-ready Checkpoint | `t land` |
| `e` | any selected bead, closed included | `e edit` |

Hidden otherwise: `l` on AFK/Container/closed beads; `t` on the Inbox, HITL,
closed, in_progress, blocked, undrained Chart, not-landable Checkpoint.

## View keys (in priority order; narrow overflow drops from the right, `q` always kept)

- Graph: `[tab detail] · space fold · h hide-closed|show-closed · r · q`
  — `space fold` only on header rows (Chart, Checkpoint, Unparented).
- Inbox: `[tab detail] · n new · r · q`
- The `tab` hint appears **only in single-pane mode**: master focus `tab detail`;
  detail focus `tab list`, and `space`/`h` are hidden there (they act on the
  unseen list). Two-pane footer is focus-independent — the border colour shows
  focus.

Edit/create modes keep their own help line (unchanged); the status message still
renders on the line below the footer.

## Blocked-launch confirm (grilling)

`l` on a blocked HITL bead enters a confirm **edit mode** (inline, like `E` raw
edit — no modal): the status line carries the prompt and the footer shows the
mode's own help `y launch · n/esc cancel`.

- Prompt: `launch "<title>" anyway? blocked by: <blocker title>…, <blocker title>… +N`
  — open blockers by truncated title, folded ones included (the gutter `!` and
  folded-header count already show *where*); `+N` keeps it to one line.
- status=blocked with no open dep edges: `"<title>" is marked blocked (no open
  deps) — launch anyway?`
- Only `y` launches; `n`, `esc`, and any other key cancel (safe default; no
  `enter`, too easy to hit reflexively after `l`). Cancel leaves `launch
  cancelled` on the status line.
