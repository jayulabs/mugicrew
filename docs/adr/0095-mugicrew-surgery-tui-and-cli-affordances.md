# ADR 0095: mugicrew Surgery affordances — `u` files and launches, an idle row resumes with `l`, ending stays in-session

Status: Accepted
Date: 2026-10-07

Amends: ADR 0091 §5 (CLI filing does not launch), ADR 0093 §1 (the "paused"
wording becomes "idle"), the earlier rule that `n` is Inbox-only. Builds on
ADR 0058, ADR 0067.

## Context

ADR 0091–0094 settled what a Surgery is, but not how a human files, resumes
and ends one from the TUI and CLI. Today `n` opens the two-field create form
on Inbox only, and `R` reuses that form on History for a Rollback. A Surgery
that is in progress with no session shows as `◐ hitl`, the same as a stranded
HITL claim. ADR 0093 calls that state "paused", but the glossary's **Paused**
already means a bead the loop won't dispatch (`‖`, its causes, `p`). Decided
in grilling.

## Decision

1. **`u` files a Surgery, on Graph and Inbox.** It opens the shared create
   form, headed "new Surgery", with **title** (required) and **problem**
   (optional, one line). Enter files the Surgery and its Checkpoint, launches
   the first session, and switches the client into it (ADR 0091 §5). If the
   launch fails after filing, the surgery is idle and `l` resumes it.
2. **`n` files a Signal on Graph and on Inbox.** It always files a plain,
   unparented intake Signal and ignores the selection. This lifts the
   Inbox-only gate.
3. **`u` and `n` do nothing on Search, History and Logs.** `?` lists them on
   Graph and Inbox only. Those footers hint `u surgery` beside `n signal`,
   and `u` drops first when width runs out.
4. **Idle, not paused.** A Surgery that is in progress with no session is
   **idle**, and its row gets a dim `idle — l to resume` suffix. `l` on it
   launches a fresh session. A session **stops** by committing and then
   running `/exit`. Franky's stop line becomes
   `⏸ Surgery idle — /exit; l on its row resumes it.`
5. **No TUI end action.** Ending stays in-session: the human tells Franky,
   who checks that the tree is clean and the gates are green, then runs
   `mugicrew surgery end <surgery-id>` (ADR 0093 §4–5). The human may also
   run the verb by hand. To end an idle surgery, press `l` and say "end".
6. **CLI filing: `mugicrew surgery new --title T [--problem P]`.** It mirrors
   `signal new`: it files the Surgery and its Checkpoint, cuts the branch and
   prints the Surgery id. It does **not** launch, because a CLI process cannot
   switch the TUI's tmux client. The surgery starts idle, and `l` launches it.

## Considered Options

- **`n` with a tab-dependent meaning (Surgery on Graph), or a
  Signal/Surgery toggle in the Inbox form.** Rejected: `u` gives the Surgery
  its own key and leaves `n` meaning only "new Signal".
- **Keep "paused" for the in-between state.** Rejected: it collides with the
  loop's Paused, which has its own glyph, key and label causes, none of which
  apply here.
- **A TUI end key.** Rejected: it would skip Franky's clean-tree and green-gate
  check.
- **CLI `surgery new` also starts the session detached.** Rejected: it is more
  plumbing for the scripting door, when filing and then `l` costs one keypress.
