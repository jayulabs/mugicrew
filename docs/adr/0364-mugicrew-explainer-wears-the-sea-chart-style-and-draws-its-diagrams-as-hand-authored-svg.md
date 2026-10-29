# ADR 0364: The explainer wears the sea-chart style and draws its diagrams as hand-authored SVG

Status: Accepted
Date: 2026-10-28

Chart mc-tmuh9 (public mugicrew explainer site with three framings). Settles
prototype mc-tmuh9.7, whose throwaway (`apps/mugicrew/site/prototype-style.html`
on side ref `refs/mugicrew/prototypes/mc-tmuh9`) the human tried in three styles
and three diagram renderings.

## Decision

1. **Visual style: the sea chart.** The site looks like a nautical chart:
   a parchment background (`#f3ead4`, with a faint dot grid), navy ink (`#1f2a44`),
   a red accent (`#a23b2a`) and a sea-teal second accent (`#2f6f73`). It uses
   Fraunces for display and diagram labels, Source Serif 4 for body text and IBM
   Plex Mono for code. Frames and section rules are double-ruled, and the lens
   doors read "Course 01/02/03". The brand mark is an original eight-point
   compass rose. Following ADR 0363 (explainer-is-pick-your-lens…), crew names
   may appear, but no One Piece artwork.
2. **Diagrams: hand-authored SVG, not Mermaid.** All five flagged diagrams
   (the spine, the bead graph as a sea chart with fog, the branch topology, the
   Checkpoint lifecycle, the loop) are hand-drawn inline SVG themed by the site's
   CSS variables. Only hand SVG can draw the game lens's map literally: blurred
   fog patches, a route with cleared waypoints, a Destination flag.

## Considered Options

- **Signal lamp** (dark, terminal-first, the TUI's palette) and **Clean product**
  (Inter, gradient, cards, near Docusaurus's Infima defaults). Rejected after the
  try: the sea chart carries the map metaphor the game lens rests on.
- **Mermaid through `@docusaurus/theme-mermaid`**, the first-party route that
  partly justified choosing Docusaurus 3.10. Rejected: it themes acceptably but
  cannot draw fog or a chart, and its layouts sprawl (the lifecycle, the loop).
  **Mermaid's `handDrawn` look** was also rejected: it clipped labels under the
  serif fonts.

## Consequences

Every diagram is an asset to redraw by hand when the story changes, so the
site does not need `@docusaurus/theme-mermaid`. Docusaurus remains the
generator for its Markdown rendering. Customizing Infima to the sea-chart
palette and fonts is build work.
