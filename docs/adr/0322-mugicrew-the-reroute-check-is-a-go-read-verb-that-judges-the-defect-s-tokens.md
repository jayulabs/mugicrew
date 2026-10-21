# ADR 0322: The reroute check is a Go read verb that judges the defect's tokens

Status: Accepted
Date: 2026-10-21

Chart mc-1ysqn (Triage reroutes a Signal into a pivot or Critique on an
in-flight Checkpoint), Prototype mc-1ysqn.4. It builds on the reads in
`docs/research/only-on-one-open-checkpoint-git-and-bd-reads.md`.

Before Luffy proposes FAST LANE, triage asks whether the code a Signal reports
lives on exactly one open, unlanded Checkpoint's branch. That check is a Go
read verb that returns a verdict. It is not a list of git and bd reads in the
triage prompt. Luffy's only judgement is picking the terms. The verb drops
landed Checkpoints, reads the diffs, and applies the decision rule.

## Decision

1. **Shape.**
   `mugicrew triage locate [--signal <id>] --token <t>... --path <p>... [--json]`
   - It prints `verdict: only <id> | main | several | none`.
   - Per term it prints `on_main`, the Checkpoints whose own diff carries it,
     and `unknown` when the term matches nowhere.
   - It lists `skipped` Checkpoints with a reason (landed by trailer, no branch).
   - It is read-only. Only `only <id>` opens the reroute outcome to Luffy.
   - `--signal` picks the repo from the Signal's `context:` label
     (ADR 0166, ADR 0167).
2. **Reads.** These are the research's working combination:
   - non-closed `kind:checkpoint` beads per repo, minus ids found in a
     `Checkpoint:` trailer on mainline;
   - per remaining `checkpoint/<id>`: the three-dot `git diff --name-only` and
     `git log -F -G<token> main..checkpoint/<id>`;
   - `git grep -F` on mainline for each token, and `cat-file -e` for each path.

   The verb never uses ancestry reads (`branch --contains`, `is-ancestor`,
   `cherry`). Only `refs/heads/checkpoint/*` names a Checkpoint.
3. **Rule.**
   - A token pulls the verdict to `main` only when it is on mainline and no
     live Checkpoint's diff touches it. A token that is both on main and in a
     live Checkpoint's diff, such as a feature name or a column name, counts
     for that Checkpoint and is reported as also on main.
   - A path counts toward `main` only when it exists on main and no live
     Checkpoint changed it. When tokens are given, a changed path defers to
     them.
   - `only <id>` means every counted term falls on that one Checkpoint.
4. **"Partly on main" means a defect term on main.** It does not mean a file
   on main. The motivating case edits an existing file, with the defect only
   in the diff, and it reroutes. A Signal with a second defect in code that no
   Checkpoint touches gets `main` and keeps the existing outcomes.

## Considered options

- **Prompt-run reads.** Five Sonnet agents on the synthetic Workspace were all
  right, but needed 3–5 commands (4–7 tool calls) per triage, and one shell
  error. The motivating case was right only because the agent judged
  `most_cited`, the feature's name, not to be a defect token. The agent
  flagged that judgement as fragile. The landed-Checkpoint filter also relied
  on the agent following every step.
- **Verb, first rule ("any token on main gives main").** It got the
  motivating case wrong when the agent also passed `most_cited`. Rule 3 above
  is the fix, now made once in code. A re-run that passed three tokens, two of
  them also on main, answered `only mc-a`.

## Consequences

- The triage prompt section shrinks to two parts: how to pick terms
  (identifiers from the defect, repo-relative paths) and what each verdict
  allows.
- A mistyped term is visible (`unknown`), not a silent miss.
- These stay with the Spec and the race and state fog on the Chart:
  - local `main` vs `origin/<mainline>` after a fetch;
  - GitLab pending Landings, which only bd shows;
  - the race between this check and the publish.
