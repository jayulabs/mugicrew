# ADR 0258: E is a shell key that opens $EXPLORER, and the raw bd update edit is removed

Status: Accepted

Date: 2026-10-17

Spec mc-22bo. Amends ADR 0144 (mugicrew-tui-v2-per-tab-keys…), ADR 0149
(mugicrew-tui-v2-idea-tab-keys…), ADR 0156 (mugicrew-tui-v2-defog-tab-rows…),
ADR 0174 (mugicrew-tui-v2-initiative-focus-view…), ADR 0181
(mugicrew-tui-v2-initiatives-tab-lists…) and ADR 0229
(mugicrew-tui-v2-hints-bar-and-key-reference…): `E` raw, the free-form
`bd update` args dialog, is removed from every surface that bound it (Ideate,
Defog, Execute, Graph, Recents, the Initiative focus view), and `E` is now a
shell key.

## Decision

1. **`E` opens `$EXPLORER` on every tab and in the focus view.** The TUI
   suspends (`tea.ExecProcess`), `sh -c '$EXPLORER "$1"' sh <root>` runs with
   its working directory also `<root>`, and the TUI resumes on exit. `<root>`
   is the git repo root; unresolved, the argument is `.` and the directory is
   the process cwd. `$EXPLORER` only: no fallback list, no default, no
   Settings knob. Unset or empty, the status line says so and nothing runs.
2. **Exit handling.** A start failure or non-zero exit sets the status line to
   `explorer: <err>`. A clean exit sets nothing and reloads nothing.
3. **Placement.** `E` sits in the shell keys right after `o` loop on/off, so
   `?` lists it there and the hints bar never shows it. It inherits the dialog
   rule of ADR 0142 (mugicrew-tui-v2-shell-keys-and-the-dialog-rule): it does
   not fire under a dialog, a text input or `?`.
4. **Raw edits go to the bd CLI.** ADR 0148
   (mugicrew-tui-v2-signal-form-is-a-centred-box…) routed labels and deps to
   the raw edit; they now go to the `bd` CLI.

## Considered Options

- **Keep `E` raw beside a new explorer key.** Rejected. The raw edit is a
  thin, error-prone wrapper over a CLI the human can run directly.
- **A fallback explorer list.** Rejected. The human sets `$EXPLORER`.
