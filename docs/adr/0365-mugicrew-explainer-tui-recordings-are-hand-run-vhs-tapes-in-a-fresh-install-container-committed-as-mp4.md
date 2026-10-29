# ADR 0365: The explainer's TUI recordings are hand-run VHS tapes in a fresh-install container, committed as MP4

Status: Accepted
Date: 2026-10-28

Chart mc-tmuh9 (public mugicrew explainer site with three framings). Settles
prototype mc-tmuh9.5, whose throwaway tapes, seed script and recordings sit on
side ref `refs/mugicrew/prototypes/mc-tmuh9` under `apps/mugicrew/site/prototype-vhs/`.

## Decision

1. **Charm VHS records the site's TUI demos.** The prototype recorded the TUI
   (Ideate, Defog, Execute, Graph tabs) and `mugicrew doctor` with vhs 0.12.1
   from nixpkgs: true colour, box drawing and the tab bar render exactly, and a
   tape's fixed Sleeps make timing deterministic.
2. **Output is MP4 (H.264) in a `<video autoplay loop muted playsinline>`.** It
   is crisp and about half a GIF's size (223 KB against 396 KB for the same
   tour). A GIF is made only where video cannot go, such as a README on npm or
   GitHub.
3. **Recordings are re-made by hand and committed.** Tapes and a seed script
   live beside the site, one command re-records them all, and the outputs are
   committed. The Pages deploy never runs VHS, so CI needs no Chromium, ttyd
   or bd/Dolt store.
4. **Tapes run in a sandbox built from the fresh-install containers**
   (`containers/fresh-install/`), not on the recorder's machine. A recording
   on the host leaks it: `doctor` prints the home and nix-shell paths, and the
   Stats tab shows the recorder's real plan usage. A container's neutral
   `tester` user and short paths avoid both. The recording image adds
   mugicrew's prerequisites plus vhs, ttyd, ffmpeg and Chromium on top.
5. **The fixture is small.** A seed script builds a scratch git repo and a
   server-mode bd store (prefix `demo`) holding two Inbox Signals, a Chart
   mid-defog and a Checkpoint with one of three Builds done. The human judged
   that enough to read as a demo; a busier graph is not wanted.

## Considered Options

- **GIF everywhere**, and **WebM plus MP4 sources**: rejected. GIF is twice the
  size at 256 colours, and a second video format buys little once H.264 plays
  everywhere.
- **Re-recording in CI on every deploy**: rejected. It would be slow and flaky,
  and it needs a live bd/Dolt store and a headless Chromium in the workflow.
- **Recording on the host and avoiding the leaky scenes**: rejected in favour
  of the sandbox, which keeps `doctor` and Stats recordable.

## Consequences

- The TUI keeps its tmux socket at `<repo>/.git/mugicrew/tmux.sock`, which must
  fit the 103-byte unix socket limit, so the scratch Workspace needs a short
  path such as `/tmp/demo`.
- Bead ids are random on each seed (`demo-xov` one run, another the next), so a
  re-record changes the ids on screen. Tapes must navigate by keys and
  position, never by id.
- The Stats tab reads zero sessions for a scratch store. Recording it needs a
  fixture event log, or the Stats scene is dropped.
