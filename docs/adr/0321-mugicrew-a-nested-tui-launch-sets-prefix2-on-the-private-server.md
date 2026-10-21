# ADR 0321: a nested TUI launch sets prefix2 on the private server, and `?` shows it as one global line

Status: Accepted
Date: 2026-10-20

Chart mc-aov0i (Nested-tmux second prefix (prefix2) on mugicrew's private
server), prototype mc-aov0i.3. Amends ADR 0042
(…concurrent-sessions-and-nested-attach) §4, whose cost was "a double prefix",
and ADR 0229 (…hints-bar-and-key-reference…) §5, whose `?` had no tmux line. The
tmux facts are in `docs/research/tmux-prefix2-semantics-on-the-private-server.md`.

When mugicrew's TUI runs inside the human's own tmux, both servers default to
the C-b prefix, so the outer tmux takes every C-b and the inner one needs C-b C-b.
Rather than remap the inner prefix, mugicrew adds a second prefix that the outer
tmux leaves alone.

## Decision

1. **Detect at TUI launch only.** `launchTUI` treats the launch as nested when
   the launcher's `$TMUX` is non-empty and does not name the private socket
   (`insideOf`). It reads the outer server's `prefix` and `prefix2` with a plain
   `tmux show -gv`, keeping `$TMUX` and passing no `-S`. A failed read (non-zero
   exit or empty output) means the outer value is unknown.
2. **Respect an existing prefix2.** If the private server's `prefix2` is not
   `None`, mugicrew changes nothing. The value may come from the user's
   `~/.tmux.conf` or from an earlier launch, and mugicrew keeps no record of
   which. This is also what makes a second launch idempotent.
3. **Pick C-a, else C-Space, else nothing.** A chord is taken if it equals the
   outer `prefix` or `prefix2`, or is the key of a binding in the private
   server's full `list-keys -T prefix` (never `list-keys -T prefix <key>`, which
   is silent on 3.7). The first free chord gets
   `set -g prefix2 <k> ; bind -T prefix <k> send-prefix -2`, run after the
   server starts and before `attach-session`. If both are taken, mugicrew sets
   nothing and says nothing, and C-b C-b still reaches the inner. Only the
   spellings `C-a` and `C-Space` are set and compared, never `C-@`.
4. **Server-wide, until the private server exits.** `set -g` applies to every
   session, HITL (`mchitl`) and TUI (root) alike. A later non-nested client
   inherits it.
5. **ADR 0042 §4 now reads:** *Inside the user's tmux, nest.* Every attach,
   launch and resume command runs with `TMUX` removed from its environment. The
   cost is a double status bar. A nested TUI launch adds a second prefix
   (ADR 0321), so the inner tmux is one chord away. C-b C-b still works through
   the outer's own `send-prefix`. `F12` still detaches the inner client.
6. **ADR 0229 §5 gains one global line.** While the TUI is nested and the
   private server's `prefix2` is not `None`, the `?` global section ends with
   `<k>  tmux prefix (nested)`, where `<k>` is the live `prefix2`, the user's own
   included. The line does not claim that pressing it twice sends the key,
   because a user's prefix2 may have no `send-prefix -2` binding. The launcher
   passes the nested flag to the TUI pane through the env handoff, because the
   pane's own `$TMUX` names the private server.

## Measured (prototype, side ref `refs/mugicrew/prototypes/mc-aov0i`)

A harness tmux typed raw bytes into an outer tmux client. The outer pane ran a
launcher that applied 1–3 and then attached to an inner server with a root
session and a `mchitl` session. The inner ran tmux 3.7c, and also the 3.4 floor:

- Outer C-b gave prefix2 C-a. `C-a c` made a window in both sessions, and the
  client returned to root or `mchitl`. `C-a C-a` put `^A` in a `cat -v` pane.
  `C-b C-b c` reached the inner.
- Outer C-a gave prefix2 C-Space. `C-Space c` worked, and `C-Space C-Space`
  put `^@` in the pane, also with the outer's `extended-keys on`. A single C-b
  reached the inner directly.
- With a user `prefix2 C-x`, mugicrew left it alone and bound nothing. A second
  launch on the same server left the existing C-a alone, with one binding.
- With prefix2 already C-a and a second outer whose prefix is C-a, `C-a C-a c`
  still reached the inner: the outer's `send-prefix` delivers C-a, which is the
  inner's prefix2.
- By hand, in the human's terminal: C-a C-a worked as start-of-line, and
  Ctrl-Space reached the inner under an outer C-a.

## Considered Options

- **Remapping the inner prefix:** rejected. It changes every user's muscle
  memory, including users who are not nested.
- **Per-session or per-client prefix:** rejected. tmux has no per-client
  prefix, and per-session tracking buys nothing that `set -g` lacks.
- **`<k>  tmux prefix here; twice sends <k>` as the `?` line:** rejected. It is
  false for a user's prefix2 that has no `send-prefix -2` binding, and making it
  true would mean another probe.
