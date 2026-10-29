# ADR 0361: A command gate names its cmd on gate.started and files an exit-coded Critique

Status: Accepted
Date: 2026-10-20

Chart mc-ij6us (A land gate can be a build command run in a checkout of the
composed squash). Settles grilling mc-ij6us.4. ADR 0151
(the-land-gate-is-observed-through-gate-events-only) and ADR 0152
(a-failed-gate-build-files-a-base-pinned-critique…) are shaped around nix: a
store path, a `.drv`, a `nix build` repro. This ADR fixes what a `gate_command`
gate writes in their place. The nix gate's events and Critique are unchanged.

## Decision

1. **`gate.started` carries `cmd`, not `ref`.** A command gate runs in the
   Landing's own tree, so no `refs/mugicrew/gate/<id>` scratch ref exists. `cmd`
   is the command string. Which of `ref` or `cmd` is present is how a reader
   tells the gate kind; later events join on `composed` and `attempt`.
2. **Event fields** (beyond ADR 0151 §1's common attrs):

   | event | extra fields |
   |---|---|
   | `gate.started` | `cmd` |
   | `gate.passed` | `dur_ms` (no `out`: nothing was built into the store) |
   | `gate.failed` | `dur_ms`, `exit` (the non-zero exit code), `critique` |
   | `gate.failed`, deduped | `dur_ms` 0, `critique` `deduped`, no `exit` |
   | `gate.infra` | `dur_ms`, `err`, `timeout`, as for nix |

   `gate.infra`'s `err` is the exec error, or the 4 KiB tail of combined output
   with the exit status (126/127) or the killing signal appended. No `exit` or
   `signal` field: a paused Landing is how infra gets noticed (ADR 0151 §4), and
   nothing acts on the code.
3. **The Critique** is titled `Checkpoint <id> fails the pinned gate command`.
   Its body:
   - an intro: "The gate command `<cmd>` exited <n> on the composed squash of
     this Checkpoint. Franky rebases onto the pinned base, then fixes test-first.";
   - the `Base:` and `Tip:` pins, unchanged;
   - an `Exit: <n>` line where the nix Critique has `Drv:`; no `Cmd:` line,
     since the intro and repro name it;
   - the last 80 lines of combined, interleaved stdout and stderr, fenced;
   - `Repro: rebase the tip onto the base in a scratch tree, then run
     sh -c '<cmd>' at <the repo root | the root of the nested repo <dir>>, in
     the loop's environment.`

   Franky's rebase-then-fix path (ADR 0152 §6) is unchanged.
4. **Unchanged:** `gate.skipped` when no gate key is in scope; the dedupe on
   (pin, base) of ADR 0152 §5, whose reason here is that the same tree at the
   same base fails the same way and a re-run costs up to the 15m timeout; and
   the close reason `obsolete: build failed`.

## Considered Options

- **A `kind` field on every gate event.** Rejected: it changes the nix gate's
  events, out of the Chart's scope, and the `ref`/`cmd` field already tells.
- **Blank `out` and `drv` instead of dropping them.** Rejected: a blank field
  implies a value that never existed. Nothing parses either (checked at
  grilling), so the shape is free to differ.
