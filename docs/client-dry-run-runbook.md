# Client-shaped GitLab multirepo dry-run runbook

How to stand up a throwaway client-shaped Workspace, drive one cross-repo item
from Signal to Land under `land_strategy = "gitlab"` with a Jira Latch, hand off
to a second human, file what blocks, and tear it all down. The blockers filed are
the deliverable; fixing them is out of scope. Each section matches a step of the
Task "Run the client-shaped dry run and file its blockers" (step numbers in
brackets).

Placeholders: `<group>` (GitLab group path), `<site>` (Jira site host, e.g.
`<name>.atlassian.net`), `<KEY-1>` (Jira ticket key), `<root>` (root repo name).
Account names are literal. Never write a token or password in this doc, a bead or
a commit; token steps happen in your own terminal.

## Shape

- A throwaway **private gitlab.com group** `<group>` owned by `shawnlee_toptal`.
- A root workspace repo `<root>` plus at least two nested project repos
  (`proj-a`, `proj-b`), each gitignored in the root and declared as a
  `repo = "<dir>"` context.
- One free Jira Cloud site with one project and a ticket.
- Two humans, one machine.

## Accounts

- `shawnlee_toptal` owns the group.
- `shawnlee_minerva` is the second member (Developer or higher): a second GitLab
  account on the same machine, with its own clone and its own `BEADS_DIR`.
- No other real person is involved.

## Provision

[Task steps 1-4]

1. **Group** [irreversible]: logged in to gitlab.com as `shawnlee_toptal`, create
   the private group `<group>`. Check: `glab api groups/<group>` returns it,
   owned by `shawnlee_toptal`.
2. **Repos**: create `<root>`, `proj-a` and `proj-b` in the group. Add this
   `.gitlab-ci.yml` to each (it exists only so the MR has a pipeline for the land
   gate):

   ```yaml
   test:
     script:
       - "true"
   ```

   Check: `glab api groups/<group>/projects` lists at least three projects.
3. **Land settings, on every project**: Settings > Merge requests, **Squash
   commits** = Require, **Squash commit message template** = `%{title}`, a blank
   line, then `%{description}`. Other squash settings are refused; see
   [GitLab land strategy](../README.md#gitlab-land-strategy). Check:
   `glab api projects/<id>` shows `squash_option` = `always` and
   `squash_commit_template` = `%{title}\n\n%{description}`, for each project.
4. **Second member**: invite `shawnlee_minerva` to `<group>` as Developer or
   higher. Check: `glab api groups/<group>/members` lists `shawnlee_minerva`.

## Jira

[Task step 5, secret]

Create a free Jira Cloud site `<site>`, one project, and one ticket `<KEY-1>`.
Install `acli` and log in with `acli jira auth login` **in your own terminal**
(the token is entered there, never recorded; mugicrew stores no Jira credential,
[ADR 0207](adr/0207-mugicrew-jira-intake-diagnoses-a-failed-acli-read-after-the-fact.md)). The
`[jira] site` key goes in the checked-in `.mugicrew/config.toml` (the shared
file, not the machine-local `<git-common-dir>/mugicrew/config.toml`); step 6 (Workspace config)
sets it once the Workspace is cloned.

`mugicrew latch jira` and intake read the ticket through `acli`, and a mismatch
between `acli`'s active site and `[jira] site` is diagnosed with a fix. Latches
are re-polled on the interval in
[Latch poll intervals](../README.md#latch-poll-intervals). Check: the ticket is
visible in Jira and `acli jira workitem view <KEY-1>` succeeds.

## Workspace config

[Task step 6]

For each account, in its own clone and `BEADS_DIR` (install and prerequisites:
[install.md](install.md)):

1. Clone `<root>`, then clone `proj-a` and `proj-b` inside it and add both dirs to
   the root's `.gitignore`.
2. Declare each as a context and set the strategy, as in
   [Declaring contexts](../README.md#declaring-contexts); top-level keys go before
   any table. Edit the checked-in `.mugicrew/config.toml` (the shared file, not
   the machine-local `<git-common-dir>/mugicrew/config.toml`); commit and push it
   from `shawnlee_toptal`'s clone so `shawnlee_minerva` gets it on clone. The
   whole file:

   ```toml
   land_strategy = "gitlab"

   [jira]
   site = "<site>"

   [context.proj-a]
   repo = "proj-a"

   [context.proj-b]
   repo = "proj-b"
   ```

3. `mugicrew init --prefix <p>` with a **distinct** prefix per human (e.g.
   `tpl` and `mnv`); see [ADR 0187](adr/0187-mugicrew-a-land-trailer-carries-its-graph-and-landedbase-refuses-a-foreign-one.md) and
   [GitLab land strategy](../README.md#gitlab-land-strategy).
4. GitLab auth: follow [install.md's GitLab section](install.md#gitlab) as that
   account. For the second account, which cannot share the machine's glab
   login, put its token in a mode-600 file and export `GITLAB_TOKEN` from it in
   that clone's shell only (ADR 0247,
   gitlab-https-credentials-are-glab-s-helper-with-its-user-set…). mugicrew
   needs no global credential entry: it injects glab's helper into every Forge
   git call itself (ADR 0256). The entry is only for your own git over https.

Check: `mugicrew doctor` passes in each clone, and `bd config get issue_prefix`
differs between the two.

## The cycle

[Task step 7]

As `shawnlee_toptal`, file one Signal whose change touches both nested repos:
"Add a shared greeting constant in `proj-a` and use it from `proj-b`". Take it
through triage, Chart, Spec, Checkpoint and Build to Land. Latch at least one
bead on the Jira ticket: `mugicrew latch jira <KEY-1> --blocks <bead>`, then move
`<KEY-1>` to Done in Jira. Land opens one MR per touched repo; mugicrew never
merges, so merge each MR on the Forge once its pipeline passes. Check: the
Initiative's members each show a closed Checkpoint in their repo, and `git ls-remote origin main` in each
touched repo shows the land.

## Handoff

[Task step 8]

As `shawnlee_minerva`, run `mugicrew graph fetch shawnlee_toptal` and read the
printed `BEADS_DIR=<dir>/.beads bd list`. Try to continue an open bead of the
first human's work. `mugicrew graph push` (and the automatic push after a Land)
needs no global credential entry: it injects glab's helper and restarts bd's
sql-server once on a failed fetch or push.

**Expected result:** a "can't continue someone else's work" blocker. The fetched
graph is a read-only Scratch graph ([ADR 0218](adr/0218-mugicrew-graph-push-and-graph-fetch-are-a-graph-noun-and-fetch-clones-into-a-cache-scratch-graph.md)), so this is a result
of the run, not a failure of it. Record the outcome (continued, or what stopped
it) as a filed blocker or a note.

## Filing blockers

[Task step 9]

File each blocker as an offshoot Signal (crew doc `signal-filing.md`):

```
bd create --type task -s deferred --no-inherit-labels -l kind:signal,drafted-by:<Task id> \
  --deps discovered-from:<dry-run Signal> \
  --title "<the blocker>" --description "<what happened>
Blocks client: yes|no
Runbook step: <section name from this doc>"
```

`<dry-run Signal>` is 'Dry run of mugicrew on a client-shaped GitLab multirepo
Workspace'. `<Task id>` is the Task walking this step ('Run the
client-shaped dry run and file its blockers'); its publish makes the Signals
visible. Filed by hand outside a session, omit `-s deferred` and the
`drafted-by` label so the Signal is open at once.

ADR-renumbering cases ('An ADR-number clash can reach mainline between Jinbe's
ticks under gitlab', 'Bead text keeps citing an ADR's number from before its
renumber') go to the Initiative's renumbering phase; do not file them again.

If a step can't proceed without a mugicrew fix, file the blocker, then say
whether to continue past it or stop the run. Check: `bd dep list` on the dry-run
Signal lists one discovered-from offshoot per blocker.

## Teardown

[Task step 10, irreversible]

Delete the GitLab group `<group>` (Settings > General > Advanced > Delete group)
and the Jira site `<site>` (Atlassian admin > Billing/Manage subscriptions >
delete site). Check: `glab api groups/<group>` returns 404.
