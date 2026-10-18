# ADR 0239: A cut session's claim on its Initiative starts at launch and ends at settle, drop or recover

Status: Accepted
Date: 2026-10-16

Chart mc-wec4 (a cut session claims its Initiative). Settles grilling mc-wec4.3.
Amends ADR 0146 (enter-on-an-initiative-head…) §1 and ADR 0178
(ideate-enter-on-a-drained…) §1, which both said the cut session leaves the
Initiative unclaimed. The Chart's Survey settled that the cut session claims it
for real (in_progress, assignee), as Signal triage does. This ADR settles the
edges of that claim.

## Decision

1. **Launch claims.** `launchNow` claims the Initiative like any other HITL bead.
   A paused Initiative is claimed only after the "Launch paused bead?" y/n
   (ADR 0164 §4), because the claim is inside `launchNow`. A refusal or `n`
   claims nothing.
2. **Resume never claims.** Resuming an exited cut session in its kept worktree
   (ADR 0178 §4) takes the resume path, which claims for no kind. An exited
   session means an unclean exit, so the launch's claim is still held. If a human
   released it by hand meanwhile, the resumed session runs unclaimed, and its
   clean exit's settle leaves the open bead alone.
3. **A clean exit releases in every case.** `settle` judges by status: an
   Initiative still in_progress goes back to open and unassigned, whether the
   session cut a phase, only settled the body (ADR 0182) or changed nothing. An
   Initiative Luffy closed is left alone. The Initiative exemption in `settle` is
   removed.
4. **`x` drop releases.** Dropping a cut session unclaims the Initiative back to
   open, as for every kind except Surgery. This already works.
5. **No self-heal.** A session that ends without settle (crash, kill) leaves the
   claim. `enter` resumes the exited session while it is listed. Once it is gone,
   `enter` refuses with "x recovers it", and only `mugicrew recover` on a
   human's command repairs it (ADR 0126).
6. **No claim on attach at upgrade.** A cut session already running when the new
   binary ships stays unclaimed until its next launch. Its pane runs the old
   `hitl-run`, whose `settle` skips Initiatives, so claiming on attach would
   strand the claim at its clean exit.

## Considered Options

- **Claim on resume when the Initiative is open.** Rejected. It only matters after
  a by-hand release, and it would be the one kind whose resume claims.
- **Never claim a paused Initiative.** Rejected. A pause stops the loop
  dispatching a bead. It does not forbid working on it, and the y/n is the
  human's authorization.
- **Claim on attach for sessions that predate the binary.** Rejected (6).
