# ADR 0366: The explainer is five pages for the deciding stranger, and renders only install.md

Status: Accepted; §1 and §3 superseded by ADR 0403 (mugicrew-the-site-gains-a-learn-section…); §2's composition of `/` superseded by ADR 0404 (the-landing-page-is-a-split-hero…)
Date: 2026-10-28

Chart mc-tmuh9 (public mugicrew explainer site with three framings). Settles
grilling mc-tmuh9.6 (site shape and the human/agent content boundary), building
on ADR 0363 (explainer-is-pick-your-lens…) and ADR 0364
(explainer-wears-the-sea-chart-style…).

## Decision

1. **Audience: only the stranger deciding whether to try mugicrew.** People who
   already use it are served by the repo, the README and `mugicrew --help`, so
   the site carries no operator reference (Agent Profiles, land strategies,
   Contexts and the like).
2. **Pages.** `/` holds the hero (with its install line) and the three lens
   doors. `/workspace`, `/framework` and `/game` each run one full lens path in
   ADR 0363's order and end with a call to action to `/try`, plus cross-links
   to the other two lenses. `/try` holds the three commands, the first Signal
   and "Where next", and appears once rather than as a copy on each lens.
   `/try/install` is `apps/mugicrew/docs/install.md`.
3. **Content boundary.**

   | Document | On the site |
   |---|---|
   | docs/install.md | Rendered unedited at `/try/install` |
   | README.md, GLOSSARY.md | GitHub only, linked from "Where next" |
   | Crew docs, ADRs | Not linked: written for agents |
   | release.md, spec-mvp.md, runbooks, tui-* docs, docs/research | Not linked: internal |

4. **Links leaving the rendered set.** At build time, relative links in
   install.md that point outside the site are rewritten to their GitHub URLs on
   `jayulabs/mugicrew` main. Any link that still can't be resolved fails the
   build. install.md itself stays unedited.
5. **Generator: Docusaurus 3.10.** It uses `markdown.format: 'detect'`, a docs
   `include` allow-list holding only install.md, and React pages for the five
   hand-designed pages. Search, sidebar and the theme toggle are off, and the
   site is light-only, since ADR 0364 defines one palette. No Mermaid theme
   (ADR 0364).
6. **Navigation.** The header shows the compass-rose mark plus Workspace ·
   Framework · Game · Try it, with GitHub and npm on the right. The footer
   repeats GitHub and npm.

## Considered Options

- **Landing only, linking out for everything**: rejected because it sends a
  stranger to GitHub's file view at the moment of installing.
- **Rendering README and GLOSSARY too**: rejected because they are reference
  material for people already running mugicrew, not explanation.
- **One long page with a lens switcher** (as in the prototype): rejected in
  favour of a shareable URL per lens.
- **VitePress or no generator**: Docusaurus's case narrowed once ADR 0364
  dropped Mermaid. It still wins as the only researched option that is healthy
  and meets every criterion, and its allow-list and Markdown detection cover
  the content boundary. The heavier footprint costs only build time on a
  path-filtered deploy.
