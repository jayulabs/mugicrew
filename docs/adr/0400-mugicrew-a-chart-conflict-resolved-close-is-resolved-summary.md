# ADR 0400: mugicrew A Chart-conflict's resolved close is `resolved: <summary>`, and its other closes keep their reasons

Status: Accepted
Date: 2026-10-29
Amends: ADR 0080 (mugicrew-afk-chart-conflict-go-closes…) §1, whose `Resolved:` arm closes the bead with the agent's bare summary as its reason. Follows ADR 0396 (mugicrew-the-hold-clearing-step-reads…) §5.

## Context

ADR 0396 §5 makes a close reason `<outcome>[: <detail>]`. A Chart-conflict
closes five ways. Four already fit: `superseded: <title>` (Jinbe yields an
unclaimed requested rebase to a Landing's conflict, ADR 0261
(mugicrew-a-requested-rebase-defers…)), `moot: container already on base
<base>` (the backstop's off-pin arm, shared by every replace bead, ADR 0264
(mugicrew-a-squash-integrate-replays…)), `withdrawn` (the Chart's Withdraw) and
`cancelled: <initiative>` (the owning Initiative's Cancel). The resolved close
does not: Go writes the agent's free-form summary alone, so it can't be told
from the others by prefix.

## Decision

1. **Resolved closes `resolved: <summary>`.** The agent's outcome line stays
   `Resolved: <one-line summary>`: it is the session's output protocol, not the
   reason. Go prefixes the summary with `resolved: ` when it closes after the
   post-resolve check. A re-run that finds the work done closes
   `resolved: already resolved`.
2. **The other four reasons are unchanged, byte for byte.** `superseded:
   <title>` keeps the successor's title, since the crash-repair scan in
   `spawnConflict` matches that exact reason; `moot: …`, `withdrawn` and
   `cancelled: …` are shared with other kinds.
3. **The docs name all five.** GLOSSARY.md's Chart-conflict entry names the
   outcome words; the bead lifecycle reference's Closure outcomes table quotes
   each exact reason.

## Considered Options

- **A bare `resolved`**: drops the summary, the only record of what the
  resolve did.
- **No ADR, the Spec citing ADR 0396 §5 alone**: leaves ADR 0080 §1 misstating
  the reason Go writes.
