# ADR 0237: The HITL doc pane defaults to a hidden in-house doc-pane viewer, not glow

Status: Accepted
Date: 2026-10-16

Chart mc-r8kv (HITL doc pane re-wraps to its current width). This ADR settles
grilling mc-r8kv.5 (Pick the HITL doc pane's default renderer). The evidence
comes from research mc-r8kv.2
(docs/research/glow-tui-reflow-in-resized-tmux-panes.md) and prototypes
mc-r8kv.3 and mc-r8kv.4 (`refs/mugicrew/prototypes/mc-r8kv`).

The old default, `glow -p -w $(tput cols)`, measured the width once at spawn.
A session started detached therefore kept its doc pane wrapped at about 32
columns after an attach or a resize. The human compared the remaining
candidates side by side in a detached-then-attached tmux session, each in a
40% split, and picked the in-house viewer.

## Decision

1. **Default renderer.** `MUGICREW_MD_RENDERER`'s fallback in the pane wording
   (`luffy/prompts/pane.md`, split and respawn) becomes `mugicrew doc-pane`.
   The wording keeps its shape: it still appends `'<body-file>'` and still
   runs `command -v` on the first word. A renderer the human sets through
   `MUGICREW_MD_RENDERER` is still used as is.
2. **`doc-pane` is hidden.** Like `hitl-run`, `watch-run` and `hitl-hook`, it
   is a pane helper that `usage` does not list. It is not a user-facing
   Markdown viewer.
3. **Behaviour.**
   - It renders the file with `mdrender` at the pane's current width and
     re-renders on every resize or attach.
   - It keeps the scroll position by proportion: the new offset is the old
     offset times the new total over the old total.
   - Keys are j/k, space/b, g/G and the mouse wheel. q quits, which closes
     the pane.
   - It uses a fixed palette with no syntax highlighting, as in the TUI
     drawer (ADR 0196).
   - If the body file is missing or empty, it shows a one-line message
     instead of exiting.
   - It has no `/` search. Search is not required, because the doc is also
     shown inline in the chat.
4. **Doctor's md-renderer check.** With `MUGICREW_MD_RENDERER` unset, it
   reports OK, `built-in (mugicrew doc-pane)`, and does no lookup. With the
   variable set, it keeps today's first-word PATH lookup and its warning.

## Considered options

- **`glow -t -w 1000 <` (body on stdin).** It reflows and needs no code.
  However, on a resize it keeps the line offset, so the scroll jumps (18%
  became 98%), and an empty body file opens glow's file browser. `glow -t FILE`
  blanks its body on every resize in glow 3.0.0.
- **A glow+less wrapper that re-renders on SIGWINCH.** It keeps less's `/`
  search, but only until the next resize, which restarts less. It is a script
  to ship, it needs glow, less and tmux, it flashes on each resize, and it
  misses a resize that arrives while glow is rendering.
- **`glow -p` without `-w`.** It wraps at the 80 columns in glow's
  self-written config, or at the spawn width, and never reflows.
