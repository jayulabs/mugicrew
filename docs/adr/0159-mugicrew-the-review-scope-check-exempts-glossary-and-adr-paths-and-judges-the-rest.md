# The review scope check exempts glossary and ADR paths, and Usopp judges the rest

Status: accepted

When a Checkpoint carries a `context:` label, Go hands Usopp the paths the
Checkpoint changed outside that context's subtree. The check is soft: it
informs the review and never fails it by itself. Three rules shape it.

- **The exempt list is a fixed path pattern, with no registry.** Go drops
  any file named `GLOSSARY.md` or `GLOSSARY-MAP.md`, any path under a
  `docs/adr/` directory (root's or another context's), and the whole of
  `.mugicrew/config.toml` (a new `[context.*]` stanza is the expected edit, and
  a path can't name a stanza). This matches NenOE ADR 0086 §3 and §5: one
  Checkpoint may write domain docs in several contexts, and the number space is
  derived from the path. Lockfiles and flake files are **not** exempt. A root
  `pnpm-lock.yaml` or `flake.lock` change can be a real dependency added outside
  the subtree, and Usopp judging it costs little.
- **An out-of-subtree path is input to the Spec axis's scope-creep check, not
  an automatic Critique.** Usopp files a blocking Critique only for a change
  the Spec doesn't need. For a justified one Usopp adds a single
  `bd comments add <Review>` per pass, listing each path and why it was needed,
  so the human at Approval sees what crossed the boundary even on a clean pass.
  A comment is not a status change, so the review stays read-only.
- **Changed paths are measured from the merge-base with mainline**, the same
  three-dot base as the review diff (`git diff main...<branch>`), not the Chart's
  landed base (ADR 0010). Without a rebase the two are the same commit. After a
  rebase onto a newer mainline, the landed base would count mainline's own
  changes as the Checkpoint's.

## Considered options

- Exempting lockfiles and flake files would hide a dependency added outside the
  subtree. A configurable exempt list is YAGNI.
- Making every out-of-subtree path a Critique would block every legitimate root
  lockfile bump. Never filing one would leave an unattended run unable to stop a
  stray edit to another context.
