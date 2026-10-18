# ADR 0251: `mugicrew transcript <bead>` follows by default, pages only when not following, and strips SGR when piped

Status: Accepted
Date: 2026-10-17

Chart mc-pxfb (Show any bead's session transcripts, finished or live, in a
readable view). Settles grilling mc-pxfb.5. Builds on ADR 0250
(mugicrew-the-transcript-view-renders-turns-as-a-digest…), whose look it
keeps, and ADR 0249 (mugicrew-tui-v2-t-opens-a-beads-transcript-view…), whose
Watch pane runs it. Amends ADR 0250 §1.

## Decision

1. **One verb.** `mugicrew transcript <bead>` is user-facing and listed in
   `--help`. The Watch pane runs it too, and the hidden `watch-run` verb is
   deleted. While the verb follows with stdin on a tty, stdin goes raw:
   `q` or Ctrl-C quit, and at the session's end it prints `run ended` and
   waits for `q`, as watch-run did. Otherwise it exits when its output is
   done.
2. **Flags.** There are exactly four: `--no-follow`, `--session <id>` (a
   unique prefix of a claude session id), `--last` (only the newest session)
   and `--no-pager`. `--session` together with `--last` is a usage error.
   With neither flag, every session shows, oldest first.
3. **Follow.** The view follows by default when the last session it shows is
   live. It stops when the event log records that session's end
   (`session.ended`/`session.failed`, or `hitl.exited`/`hitl.failed`).
   `--no-follow` prints what exists and exits, and any pending call shows as
   `· no result`. A view whose last session is finished never follows.
4. **Session id in the header.** The first 8 characters of the session id end
   the session header (ADR 0250 §1), dimmed, so `--session` has something to
   name.
5. **Colour.** When stdout is not a tty, or `NO_COLOR` is set, all SGR is
   stripped: no colour, no 236 band, no inverse badge. The glyphs `✓ ✗ ▍ ·`
   and the layout stay. There is no `--color` flag.
6. **Width when not on a tty.** Nothing wraps. Each row is one logical line,
   and a call's status goes inline after its input (` ✓ 1.2s`) instead of
   right-aligned. mdrender gets a width large enough that it never reflows.
7. **Paging.** On a tty, when the view does not follow, output goes through
   `$PAGER` or else `less -FRX` (git's behaviour). The colour decision is made
   on the real stdout before the pipe opens, so the pager gets colour.
   `--no-pager` writes straight to stdout. A following view never pages. In
   the Watch pane, a finished transcript opens in less: it scrolls there and
   `q` closes it.
8. **Exit status.**
   - An unknown bead is an error: `mugicrew: no bead <id>`, exit 1.
   - `no sessions recorded for <id>` goes to stdout with exit 0. It is an
     answer, not a failure, and the pane shows it as-is.
   - A pruned transcript shows ADR 0250 §8's fallback, exit 0.

## Considered Options

- **Never page, or `less +F` while following.** Rejected. Without a pager,
  finished transcripts in the Watch pane can only scroll through tmux
  copy-mode. `less` buffers its input and cannot redraw a pending
  `· no result` row.
- **Keep the ANSI when piped, or ASCII markers instead of glyphs.** Rejected.
  Escape codes break grep, and the glyphs are plain UTF-8 that grep matches.
- **A fixed width of 80, `$COLUMNS`, or `--width N` when piped.** Rejected.
  Wrapping splits lines that grep would match.
- **`--json`, `--since`/tail-N, `--color`.** Not needed. The raw jsonl is
  already at claude's path, and the pager gets its colour through item 7.
