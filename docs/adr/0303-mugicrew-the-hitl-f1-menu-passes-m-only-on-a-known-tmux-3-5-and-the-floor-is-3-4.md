# ADR 0303: the HITL F1 menu passes -M only on a known tmux ≥ 3.5, and the floor is 3.4

Status: Accepted
Date: 2026-10-19

Chart: tmux 3.4 floor with the F1 menu's -M gated on tmux ≥ 3.5. This
supersedes point 5 of ADR 0296 (the-hitl-f1-menu-is-if-shell…); the rest of
that ADR stands. The prototype "HITL F1 menu without -M on real tmux 3.4"
settled it on Ubuntu 24.04's apt tmux 3.4 (side ref
`refs/mugicrew/prototypes/mc-u4yoe`).

## Decision

1. **The tmux floor is 3.4.** Stock Ubuntu 24.04 ships it. Below 3.5 the F1
   menu is keys-only, and doctor warns about that.
2. **menuArgv passes `-M` only when the tmux version is known to be ≥ 3.5.**
   If `tmux -V` fails or can't be parsed, it leaves `-M` out. A wrong guess
   costs very different amounts in each direction. On 3.4:
   - **`-M` passed by mistake:** preamble's bind is accepted, so Start still
     succeeds. Pressing F1 then shows
     `Command display-menu: unknown flag -M` and opens no menu, so F1 does
     nothing.
   - **`-M` left out by mistake:** the menu still works by keys, and only
     mouse-picking is lost.
3. **The version comes from the `tmux` on PATH, not the running server.**
   mugicrew's private server may be older than the PATH binary, for example
   when tmux was upgraded while the server kept running. That case is not
   handled. F1 shows the `-M` error until that server restarts. Start never
   fails.

## Below 3.5, without -M (measured on 3.4)

- F6, F10 and c pick their entries. Cancel auto-exit (c) unsets @autofinish.
- **`mouse` on:** a click on an entry, or outside the menu, closes the menu
  without picking anything and with no error.
- **`mouse` off:** tmux doesn't turn on mouse reporting while the menu is open,
  so clicks and mouse moves never reach it. The menu stays open.
- The copyMouse run-shell still copies root's mouse bindings into mchitl.

## Considered options

- **Pass `-M` when the version is unknown:** rejected. On 3.4 that leaves F1
  doing nothing at all.
- **Read the running server's `#{version}` at each Start:** rejected. It would
  only help an old server outliving a tmux upgrade, which is rare. Even then
  the result is a visible error that goes away on restart.
