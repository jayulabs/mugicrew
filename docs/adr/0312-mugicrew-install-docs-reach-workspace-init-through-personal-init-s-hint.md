# The install docs reach Workspace init through personal init's hint

Status: accepted

Chart mc-o2i55 (Workspace init writes the shared config), grilling mc-o2i55.4
(human, 2026-10-19). `mugicrew init workspace` runs once per Workspace and plain
`mugicrew init` runs once per human. The docs give every human the same path and
let the hint tell the first human apart, so nobody has to know in advance
whether they are first.

- **install.md** keeps its three commands (`npm i`, `mugicrew init`, `mugicrew
  doctor`) for everyone. A separate **First human in a Workspace** section after
  them is where personal init's one-line hint and doctor's non-blocking warn
  point. It says: `git worktree add ../<dir> -b <branch> main`, then run
  `mugicrew init workspace` in that worktree, land `.mugicrew/config.toml` by
  the steps init prints, and re-run `mugicrew doctor`. Personal init comes first
  because the two are independent (Workspace init needs no bd store) and the
  hint then acts as the signpost.
- **Telling it is set up.** If there is no hint and no warn, the committed config
  holds the keys Workspace init owns, and you are done. On the hint, pull main
  first (the first human's config may not have merged yet, or this clone is
  behind). If the hint stays, one human runs Workspace init. The hint never
  blocks: with no shared config, the Workspace is a valid all-defaults one (ADR
  0157 (mugicrew-subtree-contexts-cost-a-no-context…)).
- **The client dry-run runbook** gets no new Task step. Its `[Task step N]`
  brackets cite a closed Task and stay as they are, and nothing renumbers.
  *Workspace config* step 2 becomes `mugicrew init workspace --land-strategy
  gitlab --jira-site <site> --context proj-a=repo:proj-a --context
  proj-b=repo:proj-b`, run in a linked worktree of the owning account's clone
  only and landed through an MR. The second account gets the file on clone or
  pull. The whole-file TOML stays as the check of what step 2 produces.
- **README** sections for keys Workspace init owns (*GitLab land strategy*,
  *Require Initiatives*, *Declaring contexts*, and the `[jira] site`
  mention) each start with one line naming `mugicrew init workspace` and its
  flag. Their TOML stays as the reference for hand edits. *Bd store mode* says
  each human runs `mugicrew init` in their own clone. *Install* adds that the
  first human also runs `mugicrew init workspace`. Sections for keys init does
  not own (`gate`, `concurrency`, profiles) are unchanged.

## Considered Options

- **A branch inside install.md's three commands** ("if you are first, also
  run…"): it asks the reader to know whether they are first before the hint
  can tell them.
- **Workspace init as step 0, before personal init**: a later human would read
  past a step that is not for them, and the order buys nothing, since neither
  init depends on the other.
