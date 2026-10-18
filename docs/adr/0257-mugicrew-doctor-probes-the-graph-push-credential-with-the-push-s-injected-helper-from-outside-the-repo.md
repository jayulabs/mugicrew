# ADR 0257: Doctor probes the Graph push credential with the push's injected helper, from outside the repo

Status: Accepted

Amended 2026-10-17: its two credential lines are collapsed into one FAIL line once the Land push is injected, as its Considered Options foresaw; see Spec 'Every Forge git call authenticates through the injected glab helper, and doctor checks one credential line'.
Date: 2026-10-17

Chart mc-uqpj.14 (Forge-ready Land MRs and an https-safe Graph ref push under
land_strategy=gitlab). Settles grilling mc-uqpj.14.3. Today doctor's one
`git credential` line runs `git credential fill` in the Workspace repo, in the
shell's env (`doctor.go` `credentialResult`, `git/credential.go`). In the
client dry run a repo-local helper made that fill pass while every Graph push
failed: the sql-server pushes from outside the repo. ADR 0256
(mugicrew-the-graph-push-runs-under-an-injected-glab-helper…) makes the push
carry an injected glab helper. The Land branch push (`git push` in
`git/git.go`) still runs in the repo on ambient git config.

## Decision

Under land_strategy=gitlab, doctor prints two credential lines, each naming the
push it covers:

1. **`git credential`** (Land branch push): the plain fill as today, in the
   repo and the shell's env. **FAIL**, because a broken Land push blocks
   the Landing. Hint unchanged.
2. **`graph push credential`** (Graph push): a `git credential fill` run with
   exactly ADR 0256's recipe. That means `GIT_CONFIG_COUNT=1`,
   `credential.https://<host>.helper='!glab auth git-credential'` (no
   username), and `GITLAB_TOKEN` inherited. It runs from a cwd **outside**
   the Workspace repo, so a repo-local helper cannot answer for a push that
   never sees it. **WARN**, since a failed Graph push warns and never holds
   the Landing (ADR 0216 (mugicrew-a-failed-graph-push-warns…)). Hint:
   `glab cannot answer git for <host>: set glab's user (glab config set user
   <name> --host <host>) or re-run glab auth login (docs/install.md, GitLab
   section)`. The detail carries git's stderr. Stdout (the password) is
   dropped.
3. Both lines skip with `ssh remote` for a non-http(s) Forge URL.
4. Doctor does **not** probe the bd sql-server's env. It cannot read it
   without side effects, and it never restarts the server. A server started
   without the injection is healed at push time by ADR 0256 §2. If that fails,
   ADR 0216's warning surfaces it.

## Considered Options

- **Keep one plain fill and only add the injection env.** Rejected: run
  inside the repo, a repo-local helper answers first and hides the gap the
  server hits, which is the dry run's false OK.
- **Replace the plain fill with the injected one.** Rejected while the Land push
  still relies on ambient config: it would stop checking a push that blocks
  Landing. A Signal filed from this grilling proposes injecting the helper
  into the Land push too, after which the two lines can collapse into one.
- **FAIL on the Graph push line.** Rejected: doctor would be stricter than
  the runtime, which only warns.
