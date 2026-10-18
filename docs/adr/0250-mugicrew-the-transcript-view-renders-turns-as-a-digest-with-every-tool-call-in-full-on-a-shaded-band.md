# ADR 0250: The transcript view renders turns as a digest, with every tool call in full on a shaded band

Status: Accepted
Date: 2026-10-17

Chart mc-pxfb (Show any bead's session transcripts, finished or live, in a
readable view). Settles prototype mc-pxfb.3. The human compared four
renderings of real AFK, HITL, failed and pruned sessions and picked variant D.
Its code and sample output are on the local side ref
`refs/mugicrew/prototypes/mc-pxfb`
(`apps/mugicrew/claudecode/prototype_transcript/samples/D-*.txt`). Variant D
replaces `claudecode.Render`'s look (`apps/mugicrew/claudecode/follow.go`).

## Decision

1. **Session header.** Each session in a bead's stream opens with one line: an
   inverse `AFK` or `HITL` badge, then in bold its start (`Oct 15 01:44`), its
   kind and crewmate, its outcome (`ended (exit 0)`, `failed (exit 1)` or
   `live`) and its duration. Two blank lines separate sessions.
2. **Human turns** are headings. They start with `▍` in the accent colour
   (212), in bold, with the turn's `HH:MM:SS` (local time) right-aligned.
   Wrapped lines keep the `▍`. The launch prompt is collapsed to one dim line,
   `▍launch prompt · N lines`. Timestamps appear only on these headings.
3. **Assistant text** goes through mdrender (ADR 0196
   (mugicrew-tui-v2-renders-bead-bodies…)) at width − 2, indented one cell.
4. **Tool calls are shown in full, one row per call, never folded.** Each row
   starts with the tool name in bold blue (111), followed by its whole input.
   The input is the first of `command`, `file_path`, `notebook_path`,
   `pattern`, `url`, `query`, `path` or `description`, else the input as JSON.
   It is wrapped, and every line of a multi-line command shows.
   - **Status.** The call's first line ends, right-aligned, with green (114)
     `✓ <took>`, red (203) `✗ <took>`, or dim `· no result` while it has none.
   - **Took** is the result's timestamp minus the call's timestamp:
     `1.2s` under 10 s, then `13s` or `2m05s`.
   - **Errors.** A failed call shows the first 4 lines of its error, in red,
     under the call. A successful call shows no output.
   - **Band.** Every tool-call line sits on a 256-colour 236 background,
     filled to the full width.
5. **Padding.** A blank line goes between blocks wherever the kind changes:
   tool calls, assistant text, a human turn (or the launch prompt). Calls in
   a row stay together in one band.
6. **Hidden.** Thinking is not shown: it is almost always redacted
   (docs/research/claude-session-transcript-lines-resume-and-pruning.md §2.3).
   `isMeta` lines and `<local-command…>` wrappers are not shown either.
7. **API errors** show as a red `✗ API error: <text>` line in the assistant's
   flow.
8. **Pruned fallback.** When the transcript is gone, the header is followed
   by the dim line `transcript pruned (claude keeps ~30 days) · from the
   event log:`.
   - A failed session then shows its `result` and its stderr tail. The tail
     is the last 12 lines in red, under an `… N earlier lines` marker.
   - An ended session says that nothing was kept.

## Considered Options

- **A, Log.** One line per block, with a `HH:MM:SS │` gutter and verbatim
  text. This is close to today's Render. Rejected: dense, and markdown shows
  raw.
- **B, Chat.** Speaker headings, with tool cards showing 5 result lines.
  Rejected: the result bodies drown out the calls.
- **C, Digest.** Like D, but each run of calls folds to `⋯ 6 tool calls: Bash
  ×5, Read`. The human asked to see each call in full, with whether it failed
  and how long it took. D is C with that change, plus the band and padding.

## Consequences

- **Durations can be negative.** Transcript timestamps are not monotonic
  (research doc §2.1), and the prototype showed `✓ -1.0s`. The Spec must
  clamp a call's duration at 0.
- **Calls are paired with their results by `tool_use_id`**, so a run of calls
  renders only once their results arrive, or at the next non-tool block. A
  live follow must show a pending call as `· no result` and redraw that row
  when its result arrives.
