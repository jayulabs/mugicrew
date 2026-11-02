# ADR 0403: mugicrew The site gains a Learn section for operators, built from the operator docs

Status: Accepted
Date: 2026-10-29
Supersedes: ADR 0366 (explainer-is-five-pages-for-the-deciding-stranger…) §1 (audience) and §3 (content boundary). Its §2, §4, §5 and §6 stand, amended below for `/learn`.

Chart mc-k6k1r.5 (Learn section of the mugicrew site), settled at its Survey
mc-k6k1r.5.1 with the human.

## Decision

1. **Audience: strangers and operators.** The site also serves people already
   running mugicrew, through a Learn section at `/learn`. It holds the guides
   Basic concepts, Working the spine, Organizing with Initiatives, Discussions
   for meta-analysis, Failure modes and troubleshooting, a bead glossary, a CLI
   verb glossary, and Customization (config options, GitLab, Jira).
2. **The three lenses stay where they are.** `/workspace`, `/framework` and
   `/game` remain the stranger's doors from `/`; Basic concepts links to them
   rather than absorbing them.
3. **Content boundary, widened.**

   | Document | On the site |
   |---|---|
   | docs/install.md | Rendered unedited at `/try/install` |
   | docs/configuration.md, docs/gitlab.md, a new Jira doc in docs/ | Rendered as Learn's Customization pages: one source for the repo and the site |
   | Learn's guides | New Markdown written for the site |
   | GLOSSARY.md | Its bead-kind entries generated at build time into the bead glossary, ADR citations and History lines stripped; the full file stays GitHub-only |
   | `mugicrew --help` | The operator verbs, hand-grouped by task on one page; crew-only verbs (those crewmates run, such as `chart append`, `integrate`, `sequence next`) get one line saying agents run them. A site test fails when an operator verb in `--help` is missing from the page |
   | Crew docs, ADRs | Not linked: written for agents |
   | release.md, spec-mvp.md, runbooks, tui-* docs, docs/research | Not linked: internal |

4. **Every operation shows TUI and CLI.** Each is a `Tabs` block with a TUI tab
   (the keys and the screen) and a CLI tab (the command). VHS clips, made under
   ADR 0365 (explainer-tui-recordings-are-hand-run-vhs-tapes…), go only at a few
   anchor moments, such as filing a Signal, defogging and approving.
5. **Navigation.** The header gains a Learn item. `/learn/*` pages carry a
   sidebar; the five explainer pages stay sidebar-less. Search stays off.

## Considered Options

- **Moving the three lenses into Basic concepts**: rejected, since they are the
  stranger's entry from `/`, whose redesign is separate work.
- **Writing every Learn page fresh**, including Customization: rejected, since it
  forks the operator docs the repo already keeps.
- **A hand-written bead glossary**: rejected for the same reason; GLOSSARY.md
  stays the one source, as crew doc vocabulary.md already does.
- **Every CLI verb on the verb page**: rejected, since crew-only verbs are not
  an operator's to run.
