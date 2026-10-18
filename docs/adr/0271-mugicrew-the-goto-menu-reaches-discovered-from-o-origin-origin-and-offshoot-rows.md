# ADR 0271: mugicrew the goto menu reaches discovered-from: o origin, origin and offshoot rows

Status: Accepted
Date: 2026-10-17

Amends ADR 0228 (mugicrew-tui-v2-g-opens-a-goto-menu…) §3, §4, §5 and §6.

ADR 0228 (mugicrew-tui-v2-g-opens-a-goto-menu…) §6 ruled discovered-from out, yet the
crew writes it most: a pivot is discovered-from each raiser, offshoot Signals and gate
Critiques from where they turned up. The menu now follows it one hop, both ways.

- **Ordering.** `bd.Bead.DiscoveredFrom` holds each discovered-from target, oldest edge
  first (`created_at` of the edge in `bd list --json`), ties broken by the smaller target
  id. bd's own array order is a hash of (issue, target) and means nothing. Edge time is
  whole-second, so in a one-second tie a later raiser can beat the true original; this
  only happens in a scripted burst and is accepted.
- **§3.** Bead options read `p` parent, `c` caused-by predecessor, `o` discovered-from
  origin. `o` is the first source in that order that is not the parent or cause and
  resolves in the snapshot.
- **§4.** `o` applies when its target resolves (after the goto fetch, which loads
  discovered-from sources as it loads parent and cause).
- **§5.** `o`, origin rows and offshoot rows use the Recents jump; none writes a Recents
  interaction.
- **§6.** The related groups are origin, blocked-by, blocks, child, successor, offshoot,
  related. origin holds the remaining sources in edge order (not open before closed);
  offshoot holds beads discovered-from the selected bead, open before closed, snapshot
  order. Each bead shows once, in its first group. "Discovered-from … out" narrows to
  "supersedes and multi-hop relations are out".
- **Shadowing.** Inside the menu `o` shadows the shell's loop on/off key, as `l`, `p`,
  `b`, `f`, `r` and `1`–`9` already do. `s` stays unbound for a possible supersedes option.
