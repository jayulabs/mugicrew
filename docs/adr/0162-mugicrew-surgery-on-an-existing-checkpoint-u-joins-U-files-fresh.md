# ADR 0162: mugicrew Surgery on an existing Checkpoint — `u` joins the selected Checkpoint, `U` files a fresh one

Status: Accepted
Amended by ADR 0241 (§1–5: u and U and the title-problem form become the New menu's Surgery form).
Date: 2026-10-13

Amends: ADR 0095 §1 (`u` filed a fresh-Checkpoint Surgery), §3 (where `u` is
hinted), §6 (the CLI files only fresh Checkpoints), and the single
`Surgery:` line in the Approval packet. Refusals follow ADR 0144 §2–3
(status line, no dialog). Decided in grilling (Chart "Surgery on an existing
Checkpoint").

## Context

A Surgery can now join an open Checkpoint as a new member, not only create its
own. Eligibility was settled at Survey and in the integrate-concurrency
research: at most one open Surgery per Checkpoint, and none while a Landing is
live, a pivot gate is open, or a Conflict or Base+Tip Critique is open. The
remaining question was the TUI and CLI surface for that eligibility.

## Decision

1. **Target resolution.** `u` targets the selected Checkpoint row, or the
   Checkpoint parent of a selected direct member (Build, Review, Critique,
   Approval, Landing, Surgery). It never walks up from a Spec or Chart: a Spec
   can have several Checkpoints (Rollback successors), so that walk is
   ambiguous.
2. **`u` with no Checkpoint refuses; it never falls back.** It reports on the
   status line:
   `u: select a Checkpoint or one of its members — U files a surgery on a fresh Checkpoint`.
3. **Ineligible Checkpoint refusals.** These go on the status line. If
   several apply, only the first in this order is shown:
   - `u: <cp> is landing — no surgery while a Landing is live`
   - `u: <cp> is frozen at a pivot gate — resolve the pivot first`
   - `u: <cp> has an open <Conflict|Base+Tip> Critique rewriting its branch — wait for it to publish`
   - `u: <cp> already has open Surgery <id> — l to resume` (v2: `enter to resume`)
   - `u: <cp> is closed — U for a fresh Checkpoint`
4. **Form.** `u` opens the same create form with **title** (required) and
   **problem** (optional, one line). It is headed
   `New Surgery on <cp> — <cp title>`, truncated to fit, because Enter files
   and launches with no y/n step. Enter files, launches and switches the
   client, as in ADR 0095 §1. `U` is ADR 0095 §1's old `u`, unchanged and
   headed `New Surgery`.
5. **Bindings, hints and `?`.**
   - **v1 Graph:** binds `u` and `U`. The footer reads
     `n signal · u surgery · U fresh surgery`, and `u`/`U` drop first when
     width runs out.
   - **v1 Inbox:** binds `U` only and hints `U surgery`, because its rows are
     intake Signals with no Checkpoint.
   - **v2:** Graph only, binding both keys. Ideate binds neither.
   - **`?` entries:**
     `u  Surgery on the selected Checkpoint: file it and start dictating` and
     `U  Surgery on a fresh Checkpoint: file it and start dictating`.
6. **Idle suffix unchanged.** It sits on the Surgery row. At most one Surgery
   is open, and closed Surgeries get no suffix, so a Checkpoint with several
   Surgeries needs no Checkpoint-row marker.
7. **CLI: `mugicrew surgery new --checkpoint <id> --title T [--problem P]`.**
   - It files and claims a Surgery as a child of `<id>`, cuts no branch,
     prints the Surgery id, and does not launch (ADR 0095 §6).
   - It refuses with the texts in §3, prefixed `surgery new:`, plus
     `<id> is not a Checkpoint` and `no such bead <id>`.
   - Without `--checkpoint` it behaves as before.
8. **Approval packet: one `Surgery: <id> — <title>` line per Surgery.** Closed
   Surgeries are included, in filing order.
   - **Surgery Checkpoint:** the lines open the packet, as the single line
     did.
   - **Spec'd Checkpoint:** the Spec stays the intent, and the lines follow
     under `Surgeries on this Checkpoint:`. This tells the approver that some
     commits were dictated by hand.

## Considered Options

- **`u` on a no-Checkpoint selection falls back to a fresh Checkpoint.**
  Rejected: `u` would mean two things.
- **`u` resolves from a Spec or Chart.** Rejected: one Spec, several
  Checkpoints.
- **A Checkpoint-row `· N surgeries` marker.** Rejected: the Surgery rows
  already show under it.
- **Keep one Approval line naming the first Surgery.** Rejected: it hides
  later hand-dictated commits from the approver.
