# ADR 0404: The landing page is a split hero, then a Learn band, then the lens doors

Status: Accepted; §3 to §5 (doors, two-column sections, prerequisites and status) superseded by ADR 0422 (the-landing-page-is-the-human-s-own-copy…)
Date: 2026-10-29

Chart mc-k6k1r.7 (landing page redesign of the mugicrew site). Settles
prototype mc-k6k1r.7.2. The human compared three layouts of `/` in the
sea-chart style of ADR 0364 (explainer-wears-the-sea-chart-style…) and picked
variant B as it stands. The throwaway is on side ref
`refs/mugicrew/prototypes/mc-k6k1r.7`. This ADR supersedes §2 of ADR 0366
(explainer-is-five-pages-for-the-deciding-stranger…) for `/` only. §2's
lens pages and `/try` stand.

## Decision

`/` is still written for the stranger deciding whether to try mugicrew. Top to
bottom it holds:

1. **Split hero.** On the left: the name, the one-breath tagline, the lede, the
   install line, and a link to `/try`. On the right: the hero-loop recording in
   a double-ruled frame, offset by a sea-teal block shadow.
2. **Learn band.** A full-width navy band directly under the hero, framed by
   double rules. The whole band is one link into Learn, so a returning user
   can't miss it.
3. **Lens doors as rows.** The three doors are full-width rows rather than
   cards. Each row leads with its Course number and its question, and ends with
   the lens name as the link. The heading is "Pick the question that brought you
   here".
4. **Two-column sections, in this order.** First, the problem it solves beside
   the spine diagram. Second, a worked example (one Signal to a landed commit)
   beside a numbered list of what the human controls, one item per approval.
5. **Prerequisites and a status line.** What it needs and runs on, then a
   single light line on the project's maturity.

Below about 900px, every two-column block stacks into one column. The Learn
band and the door rows wrap without changing their order.

What each block says is decided by grilling mc-k6k1r.7.3, in ADR 0422 (the-landing-page-is-the-human-s-own-copy…).

## Considered Options

- **A, a full-bleed hand-drawn chart hero.** A dashed route through fog to a
  Destination flag, with the title in a panel over it. It is the most
  sea-chart-like option, but the recording slides below the fold and the
  panel fights the route for space.
- **C, a centred cartouche with atlas "plates".** Learn would be the hero's main
  button, with the install line demoted under it. It reads as a book, but it is
  long and it puts the doors fifth.
