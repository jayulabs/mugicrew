# ADR 0421: Gallery art is a text and mask pair, colored through eight art roles in every preset

Status: Accepted
Date: 2026-11-01

Chart Gallery tab in the mugicrew TUI cycles original nautical ASCII art (mc-2quy2)
settled the art format at Survey. Before the Spec, the human tried a throwaway Gallery
tab in the real TUI (kept on `refs/mugicrew/prototypes/mc-2quy2`). It had three original
pieces (a lighthouse, a ship under sail and a parrot), the `&` jump key, `j`/`k` paging
that wraps, and the caption. They tried it in the dark, light and high-contrast presets
and accepted it as built:

1. **A piece is an art `.txt` plus a `.mask` of the same shape.** Each mask character
   is the role letter for the art character at the same position. A space means the
   default ink. A mask character over a space in the art is ignored. Both files are
   embedded with `go:embed`. A test checks that each pair has the same shape and that
   each piece fits 60×16, so a piece plus its one-line caption `n/N · Title` fits the
   80×18 body of an 80×24 terminal. A smaller body shows the line "widen the terminal
   to see the art" instead.
2. **The role letters are not all initials.** Sea, sky and sail all start with `s`, so
   the letters are `s` sea, `k` sky, `h` hull, `a` sail, `l` land, `g` light (lamp,
   sun, flag, beak), `p` plumage and `w` wing. Wooden things that are not a hull, such
   as a perch, use `h`.
3. **There are eight art roles, not six.** The human asked for a parrot during the
   prototype, and the six nautical roles had no color for feathers. Plumage and wing
   join sea, sky, hull, sail, land and light. Rejected: generic accent roles (accent 1,
   2, 3). They would let a new piece reuse colors without adding roles, but a role name
   would no longer say what it colors. Also rejected: forcing the parrot into the six
   nautical roles.
4. **The launch set gains the parrot**, so the five pieces settled at Survey become
   six.

The role values, as dark / light / high-contrast xterm-256 indices, are the ones the
human accepted in the throwaway. They extend ADR 0386
(mugicrew-the-light-and-high-contrast-presets…) with an `art` group:

| Group | Values |
|---|---|
| art | sea 33/25/39; sky 117/31/159; hull 137/88/209; sail 230/137/231; land 107/28/118; light 220/166/226; plumage 203/160/196; wing 35/29/46 |

Light avoids indices 0–15, and high-contrast's values are bright, as ADR 0386 requires.
The Gallery tab has no ●N badge, as ADR 0171 (v2-polish-pass-tab-keys…) §5 has for the
rare tabs.

## Consequences

- A new piece that needs a color outside the eight roles adds a role to all three
  presets and amends this ADR. Mixing fixed colors into the art stays out of scope.
