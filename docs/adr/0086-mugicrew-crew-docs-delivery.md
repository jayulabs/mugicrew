# ADR 0086: mugicrew crew docs ship embedded in the binary and reach a session by path — a small CREWMATE.md preamble is inlined, the bulky docs are written to a content-hashed user cache dir

Status: Accepted
Date: 2026-10-07

## Context

The crew docs Chart makes every crewmate invocation self-contained: no embedded prompt
calls an installed skill, and the shared crewmate context (flow, bead-graph
conventions, procedures, templates, distilled skills) is authored in mugicrew's
source and ships with the binary (triage Q3). An earlier design settled a two-layer prompt:
a static CREWMATE.md preamble concatenated in front of each per-kind prompt
through `contract.NewInvocation`. That leaves open how the bulky shared docs
reach the session.

The facts that decide it:

- A HITL prompt is nested in one `tmux new-session` command, which tops out at
  about 16 KiB. The charting prompt alone is 9.8 KB. AFK `-p` allows 131071
  bytes.
- The six candidate skills total 33.8 KB, or about 53 KB with the implement
  chain. That is past the HITL ceiling twice over.
- AFK sessions Read files outside their worktree without a prompt. HITL was not
  probed.
- An embedded file must live in a package directory under `apps/mugicrew/`, and
  not under `apps/mugicrew/docs/`, which the nix build excludes.

## Decision

1. **Hybrid delivery.** Only the CREWMATE.md preamble is inlined into
   `Invocation.Prompt`, as that design intended. It holds the stance every crewmate
   needs and the path of the crew docs. Everything bulky (procedures,
   templates, distilled or vendored skills) is a crew doc on disk, and the
   prompt names the file to Read at the step that needs it, loaded on demand
   the way a skill is.
2. **Authored in `apps/mugicrew/crewdocs/`.** This new package owns the `.md`
   files, an `embed.FS`, and the function that writes them out and returns
   their directory. `contract` imports it, so the seam stays thin and the
   harness stays content-blind: the prompt carries only a path.
3. **Written to `os.UserCacheDir()/mugicrew/crewdocs/<content-hash>/`.** The
   docs belong to the binary, not to a repo, so a foreign Workspace
   is left untouched. The content hash keeps two builds, such as the pinned
   loop and a newer TUI, from overwriting each other's docs.
4. **Written lazily by the binary.** The first `contract.NewInvocation` in a
   process materializes the directory if its hash directory is missing. It
   writes to a temp dir and renames it into place, so racing processes
   converge. There is no install step or lifecycle hook, and a wiped cache
   heals on the next invocation.
5. **Updates ride the binary.** Editing a crew doc changes the binary and its
   outPath, and the loop restarts onto the new build (ADR 0062), exactly as a
   prompt edit does today. The new build gets a new hash and a new directory.
   A session always reads the docs of the build that wrote its prompt.
6. **Claude CLI only.** Delivery uses nothing but the `claude` CLI's own file
   reads. It adds no API, SDK or API-key path, so crewmates stay on the
   human's subscription login.

## Consequences

- Old hash dirs accumulate at about 50 KB per build. Pruning is deferred until
  it matters.
- A HITL session may ask the human for permission on its first Read outside
  the worktree. A research probe settles this. If it does prompt, the harness
  passes `--add-dir <crewdocs dir>`, which is still content-blind because it
  names only a directory. (Settled later: the `crewdocs/` parent is
  passed, not the hash leaf. A probe showed the parent covers reads in every
  hash dir, so a session resumed onto a newer build still reads its own.)

## Considered and rejected

- **Inline everything into the prompt.** This breaks HITL launches at the tmux
  ceiling.
- **`--append-system-prompt-file`.** It is Claude-only, it makes the harness
  know about the docs, and its HITL and resume behaviour is unprobed.
- **`<git-common-dir>/mugicrew/` or a worktree dot-dir.** The docs aren't
  per-repo. A worktree dir risks being committed and clutters a foreign
  Workspace.
- **`contract/docs/` or per-crewmate doc dirs.** The first overloads the thin
  seam, and the second scatters docs that are shared by definition.
- **Eager write at loop or TUI start, or a nix-time install.** Each adds a
  lifecycle step the lazy write doesn't need.

## References

OE ADR 0007 (contract), ADR 0062 (loop restart onto main's build). GLOSSARY **Crewmate preamble**,
**Crew docs**.
