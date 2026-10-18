# ADR 0192: v2 Robin sessions start and live in a shell-wide `C` modal, and a closing line is her done cue

Status: Accepted
Superseded in part by: ADR 0194 (§1–4: the key is `D`, a discussion is a bead, rows come from discussion beads)
Date: 2026-10-15

Chart mc-4ryp.2 (Robin: a beadless discussion session). Settles grilling
mc-4ryp.2.6 after the seam inventory
`docs/research/hitl-session-machinery-bead-assumptions-for-beadless-sessions.md`.
Builds on ADR 0184 (the Recents modal and its picker chrome) and the
ADR 0142 §7 dialog rule.

## Decision

1. **Key.** Shell-wide `C` opens the Robin modal from any tab and from the
   Initiative focus view. `esc` or `C` closes it, and no shell key fires
   while it is open. Lowercase `c` stays free.
2. **Modal.** It is a centred list in picker chrome (`j`/`k` move). The
   first row is `+ New discussion`. Robin's sessions follow it, newest
   first. Each row shows the synthetic id, age and state (live, your turn,
   needs approval, exited). The rows come from the tmux session table, not
   from beads.
   - `enter` on `+ New discussion` launches a session at once, with no
     dialog.
   - `enter` on a session attaches to it, or resumes it if it has exited.
   - `x` drops a session (kills it and deletes its Drafts) behind the y/n
     dialog, because dropping discards.
3. **Text.** The status bar reads `robin-<ts> · Robin discussion`. No
   `@title` tag is added, so rows show only id, age and state.
4. **Done.** Robin's prompt defines done as the human saying they are
   finished. She then prints the standard closing line, so Auto-exit
   (ADR 0111) arms unchanged. A clean exit publishes her Drafts and flashes
   `✓ robin-… finished`, and the session leaves the modal. Her status bar
   never flips (ADR 0066 §4 has no bead to read) and stays
   `F12 back to TUI · /exit to finish`.
5. **Visibility.** A Robin session waiting on the human shows only in the
   header's waiting count, never on a tab badge.

## Considered Options

- **A "Discussions (N)" section at the top of Ideate.** Rejected. It would
  put rows with no bead behind them on a bead tab, and amend ADR 0179's
  sections and badge.
- **A ninth "Robin" tab.** Rejected. It is the heaviest option and needs a
  jump key.
- **A `@title` tag Robin sets from the topic.** Rejected for now. It needs
  a write path back from inside the session.
- **A Robin-specific done flash with the count of Signals drafted.**
  Deferred until it is missed.
- **No closing line, so only `/exit` ends her.** Rejected. Auto-exit
  already works for a session with no bead.
