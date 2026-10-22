# ADR 0336: Hold Task Checks probe through `mugicrew probe`, a one-word read verb

Status: Accepted
Date: 2026-10-21

Chart mc-6848i (Hold Tasks carry a Steps body Brook can check), grilling
mc-6848i.3, from the research note
`docs/research/hold-task-clearing-conditions-steps-and-checks.md`.

A Hold Task's Checks (ADR 0116 (task-steps-checks-and-record) §3) must prove
the hold cleared before Brook closes it. They run through a new read-only
verb family built on the same Go reads that decide the hold, not through raw
`glab api … | jq` commands rendered at filing time. The precedent is ADR 0322
(the-reroute-check-is-a-go-read-verb…): the rule lives once, in code.

## Decision

1. **Shape.** One namespace, `mugicrew probe`:
   - `probe mr <ref>` or `probe mr --branch <b>` prints
     `opened|merged|closed|locked|none`. `<ref>` is `!N`, `<path>!N` or a
     URL, read through `forge.ParseMRRef`, so a nested project resolves
     itself. `--branch` mirrors `FindOpenMR`.
   - `probe pipeline <ref> <id>` or `probe pipeline <ref> --main` prints the
     pipeline's GitLab status.
   - `probe landed <container id>` prints `landed|absent|foreign`, through
     `LandedBase`. It finds the kind and repo from the bead
     (`contexts.RepoOf`).
   - `probe foreign [--context <c>]` prints `none|foreign`, running the
     `loop start` preflight scan over the current `issue_prefix`.
2. **Contract.** It prints one word and exits 0 whenever the read succeeded,
   whatever the word. It exits non-zero only when the read failed (no Forge,
   an unknown ref, the network). The expected word stays in the Task body's
   Check ("prints `merged` or `closed`"). That keeps a *fail* (still
   `running`) apart from an *error*.
3. **Read-only.** `probe landed` and `probe foreign` first fetch the Forge's
   main into its remote-tracking ref, as Jinbe does. That is an idempotent
   no-op in ADR 0116 §4's sense. Under `land_strategy=local` they read local
   main without fetching, and `probe mr` and `probe pipeline` exit non-zero.
4. **Per step.** Each step keeps its own Check. There is no whole-Task
   verdict verb.

## Considered Options

- **Raw `glab api | jq` and `git` commands, rendered by Go** (the leaning at
  charting): rejected. glab 1.124.0 has no `--jq`, and jq is not in the
  devShell. `holdTrailerless` and `bd.HoldForeign` lack the Forge project.
  The trailerless and foreign-land reads are git-native and `Graph:`-aware,
  which raw commands only approximate, so a Check could pass while Jinbe
  still holds.
- **Hybrid** (raw glab for MR and pipeline state, a verb for the git reads):
  rejected. It keeps the jq dependency and the project plumbing for half the
  bodies.
- **One hold-aware verb, `mugicrew hold check <task>`:** rejected. It
  collapses every step into one opaque Check, and on a foreign land
  `bd rename-prefix` renames the very Task id it takes.
- **Verbs under existing nouns** (`mugicrew forge mr-state`,
  `mugicrew landed`): rejected in favour of one namespace whose every
  subverb is read-only, which gives one rule for the guide's permissions.
