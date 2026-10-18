Show the doc in a sibling pane as well as inline. Keep one body file per doc
outside the repo, in a session temp directory, e.g.
${TMPDIR:-/tmp}/mugicrew-<bead-id>/<doc>.md, and write the doc to it. The chat
still shows the doc inline; the pane is purely additive.

- First presentation: if MUGICREW_HITL_PANE is set and `command -v` succeeds on the first word of
  `${MUGICREW_MD_RENDERER:-mugicrew doc-pane}`, split your own pane and record the printed pane id:

      tmux -S "${MUGICREW_HITL_PANE%%,*}" split-window -t "${MUGICREW_HITL_PANE#*,}" -h -l 40% -P -F '#{pane_id}' "${MUGICREW_MD_RENDERER:-mugicrew doc-pane} '<body-file>'"

- Revision: rewrite the body file, then re-render the same pane:

      tmux -S "${MUGICREW_HITL_PANE%%,*}" respawn-pane -k -t <doc-pane-id> "${MUGICREW_MD_RENDERER:-mugicrew doc-pane} '<body-file>'"

  If that fails because the human closed the pane, split again as on first
  presentation and record the new pane id.
- If MUGICREW_HITL_PANE is empty or unset, run no tmux at all (no pane, no
  session, no server). If the renderer is not on PATH, skip the pane and say so in one
  line naming the renderer you looked for. Either way, continue inline-only.
- The pane is an ordinary tmux pane; mugicrew does not mark, track or reap it.
  It closes when the human quits the pager or the session ends.
