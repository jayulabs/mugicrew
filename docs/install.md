# Install mugicrew

Three commands, then fix what doctor reports:

```bash
npm i -g @jayulabs/mugicrew     # or, in a JS repo: npm i -D @jayulabs/mugicrew
mugicrew init                   # bootstrap your bd store in this clone
mugicrew doctor                 # repeat: run mugicrew doctor fix, then each Manual step, until it passes
```

mugicrew bundles none of its prerequisites. `mugicrew init` never commits. It
prompts for an issue prefix (`--prefix <p>` skips the prompt), so use a distinct
prefix per human.

In a terminal, `mugicrew init` then asks `Enable mouse mode? [y/N]` once, and says
that Shift-drag (Option/Alt on some terminals) selects text while mouse mode is on.
It writes the answer to your machine-local `[tui] mouse` key and never asks again
once the key is set. `--mouse on|off` answers for a script; with no terminal and no
flag the key stays unset, which reads as off. `mugicrew init workspace` never
touches it.

## Release notice

An npm-installed mugicrew (not a from-source build) asks the npm registry at most
once every 24 hours whether a newer `@jayulabs/mugicrew` release exists, in the
background and silently on any failure. When one does, a human-facing command run
on a terminal ends with one stderr line naming it and `mugicrew upgrade`. It never
shows for `--version`, `--help`, piped output or crewmate sessions. The answer is
cached in `mugicrew/release.json` under your user cache dir.

Two opt-outs each turn off both the check and the notice: `[release] notice =
false` in `.mugicrew/config.toml` (the machine-local file wins over the checked-in
one), and `MUGICREW_RELEASE_NOTICE=0` (or `off`/`false`), which wins over config.

### Unattended Upgrade

The notice only tells, unless you opt a loop into upgrading itself: set
`[upgrade] unattended = true` in `~/.config/mugicrew/config.toml` or
`MUGICREW_UNATTENDED_UPGRADE=1`. It applies only to an `npm i -g` install, and
`MUGICREW_RELEASE_NOTICE=0` turns it off with the check. The gates, what the loop
log and TUI show, and recovery from a failed version or a tripped doctor are in
[upgrading](upgrading.md#unattended-upgrade).

## First human in a Workspace

`mugicrew init workspace` writes the settings the whole Workspace shares into
`.mugicrew/config.toml`. It runs once per Workspace, not once per human, and needs
no bd store. If `mugicrew init` printed the hint or `mugicrew doctor` warned that
the Workspace is not set up:

1. Pull `main` first: the first human's config may not have merged yet, or this
   clone is behind. If the hint is gone, you are done.
2. Otherwise one human cuts a linked worktree, since init refuses on `main`:
   `git worktree add ../<dir> -b <branch> main`.
3. In that worktree, run `mugicrew init workspace`. It asks for `land_strategy`,
   Require Initiatives, the Jira site and Contexts; each question has a flag
   (`--land-strategy`, `--require-initiatives`, `--external-ref`, `--jira-site`,
   `--context`). It never commits.
4. Land `.mugicrew/config.toml` by the steps init prints.
5. Re-run `mugicrew doctor`.

With no shared config the Workspace is valid and runs on defaults, so the hint
never blocks.

## Bead actor

Each Device claims beads as `<user>@<device>`. Set it once per Device in bd's user
config, `~/.config/bd/config.yaml`:

```yaml
actor: <user>@<device>
```

`<user>` is the same on every Device. `<device>` is a Device name you choose and
keep, not the raw hostname, so a hostname change or reinstall doesn't make your own
claims look foreign. `$BEADS_ACTOR` still overrides it for one shell. Confirm with
`mugicrew doctor`: the `bd actor` row shows the actor, and warns with the exact line
to add while the Device name is missing. mugicrew keeps working under the bare name
meanwhile.

A running loop and the processes it spawns (reconcile, integrate, crewmates) keep
the actor resolved when it started: each user verb exports it as `$BEADS_ACTOR`
for its children. Changing `actor:` takes effect on the next loop or TUI start, so
restart the loop after editing it.

Then run `mugicrew doctor fix`: it reassigns your in-progress claims made under the
bare name to the full actor, so your own live work doesn't read as claimed elsewhere.

## Core

Always checked. Each FAIL's To-fix item names the minimum version and this OS's install command.

- `bd` 1.3.0 or later
- `dolt` (pinned 2.5.0; doctor FAILs below 2.3.0 and WARNs on 2.6.0 or later)
- `tmux` 3.4 or later (3.5 or later for mouse-picking in the F1 menu)
- `claude` 2.1.78 or later
- `git` 2.40 or newer (`merge-tree --write-tree --merge-base`, used by Revert and the renumber-only check)

### dolt

Install the pinned 2.5.0 release (Linux amd64 shown; pick your platform's
tarball from the [release page](https://github.com/dolthub/dolt/releases/tag/v2.5.0)):

```bash
curl -fsSLO https://github.com/dolthub/dolt/releases/download/v2.5.0/dolt-linux-amd64.tar.gz
tar -xzf dolt-linux-amd64.tar.gz
sudo install dolt-linux-amd64/bin/dolt /usr/local/bin/dolt
```

## GitLab

Only when the Workspace sets `land_strategy = "gitlab"`. Needs `glab` and https
credentials for git.

Run `mugicrew doctor` first. Only if its glab-user or credential-fill check
FAILs, run one interactive login and choose gitlab.com, **Web**, **HTTPS**, and
**Yes** to "Authenticate Git with your GitLab credentials?":

```bash
glab auth login
```

That writes git's credential helper, sets glab's user and stores a token. Doctor's
credential fill catches a missing glab user or login, and re-running the login
fixes it.

### Scripted setups

For a non-interactive machine, use three lines instead:

```bash
glab auth login --hostname gitlab.com --git-protocol https --stdin < <token-file>
glab config set user <gitlab-username> --host gitlab.com
```

The token's scope is `api` alone.

mugicrew needs nothing beyond glab's login and user: it injects glab's helper into
every git call to the Forge (ADR 0300,
bead-graph-data-never-goes-to-a-project-remote…). A global
`credential.https://<host>.helper` entry is only for your own git over https
(clone, manual push):

```bash
git config --global credential.https://gitlab.com.helper '!glab auth git-credential'
```

Credentials cover one account per machine.

## Jira

Only when the Workspace sets `[jira] site`. Needs `acli`, then:

```bash
acli jira auth login
```

### acli

On Debian/Ubuntu, from Atlassian's apt repo (tested 1.3.39); macOS uses
`brew tap atlassian/homebrew-acli && brew install acli`:

```bash
sudo mkdir -p -m 755 /etc/apt/keyrings
curl -fsSL https://acli.atlassian.com/gpg/public-key.asc | sudo gpg --dearmor -o /etc/apt/keyrings/acli-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/acli-archive-keyring.gpg] https://acli.atlassian.com/linux/deb stable main" | sudo tee /etc/apt/sources.list.d/acli.list
sudo apt update && sudo apt install acli
```

## Backup

Optional, but doctor warns until it is set up. Your Bead Graph lives in one
machine's Dolt sql-server; this keeps an off-machine copy.

### What it is

A push of the graph's `main` to a Dolt remote named `backup`, run hourly by
`mugicrew backup push` (ADR 0367, a-bead-graph-backs-up-off-machine-by-dolt-push…).
It is not `bd backup`, not `bd sync`, and not a backup of git branches or refs.
The local `bd backup` is left alone and doctor ignores it.

### Destination kinds

- `file://` on an off-machine mount (verified). Doctor warns if the path is on the
  same filesystem as `.beads`.
- A credentialed Dolt remotesapi (verified).
- DoltHub and cloud storage should work but are untested.

### Credentials

A remotesapi needs `DOLT_REMOTE_PASSWORD` and `DOLT_REMOTE_USER`. `file://` needs
neither. Put them in `~/.config/mugicrew/backup.env`, mode 0600, outside every repo:

```bash
install -m 0600 /dev/null ~/.config/mugicrew/backup.env
cat > ~/.config/mugicrew/backup.env <<'ENV'
DOLT_REMOTE_USER=mugicrew_backup
DOLT_REMOTE_PASSWORD=<password>
ENV
```

`DOLT_REMOTE_USER` is mugicrew's convention: Dolt never reads it, and the push
passes it as `--user` only when it is set. The bd-spawned sql-server inherits the
credential from its environment at start, so start or restart it from a shell
that has the file sourced (this Workspace's devShell sources it when present).
Any export that reaches the server's env at start works. `mugicrew backup push`
never starts the server: a server without the credential fails the push.

### Seeding a remotesapi destination

A remotesapi push never creates its database. As the Dolt host's admin, run:

```sql
CREATE DATABASE <graph-db>;
CREATE USER 'mugicrew_backup'@'%' IDENTIFIED BY '<password>';
GRANT SUPER ON *.* TO 'mugicrew_backup'@'%';
```

Use one destination database per machine's graph, named after the graph's
database (here `<graph-db>`), adding `_<hostname>` only for a second machine
backing up the same graph. A `file://` destination needs no seed: the push creates the
directory (ADR 0368, a-backup-destination-is-seeded-by-hand…).

### Adding the remote

In the graph's database, once per clone:

```bash
dolt --host 127.0.0.1 --port "$(cat .beads/dolt-server.port)" --no-tls -u root -p '' \
  --use-db <db> sql -q "CALL DOLT_REMOTE('add','backup','<url>')"
```

`<url>` is `file:///mnt/offsite/<db>` or `http(s)://<host>:<port>/<db>` for a
remotesapi. Use the `dolt` client, not `bd sql`, which times out at about 10 s.

### First push

Every destination needs one push by hand before `mugicrew backup push` can work: the
verb reads `remotes/backup/main`, which does not exist until something has been pushed.
A freshly created remotesapi database also shares no ancestor with your graph, so that
first push needs `--force`. Only a human forces; the recurring push never does.

```bash
dolt --host 127.0.0.1 --port "$(cat .beads/dolt-server.port)" --no-tls -u root -p '' \
  --use-db <db> sql -q "CALL DOLT_PUSH('--force','--user','$DOLT_REMOTE_USER','backup','main')"
```

For `file://`, make the same first push but omit both `'--force',` and
`'--user','$DOLT_REMOTE_USER',`: an empty `file://` directory takes a plain push. Every
later push is `mugicrew backup push`.

### Recurrence

The loop pushes hourly while it runs in any state but off, and does nothing when
there is no `backup` remote. With no loop running, call the same verb from the OS
scheduler. The lock makes the triggers safe together.

```ini
# ~/.config/systemd/user/mugicrew-backup.service
[Service]
Type=oneshot
WorkingDirectory=<your clone>
EnvironmentFile=%h/.config/mugicrew/backup.env
ExecStart=<absolute path to mugicrew> backup push
Environment=PATH=<dirs holding mugicrew, bd and dolt>:/usr/bin:/bin

# ~/.config/systemd/user/mugicrew-backup.timer
[Timer]
OnCalendar=hourly
Persistent=true
[Install]
WantedBy=timers.target
```

A systemd user unit or cron job runs with a minimal PATH, so use an absolute
`ExecStart` path and set `PATH` so `mugicrew`, `bd` (which resolves the graph) and
`dolt` (which does the push) all resolve.

Enable with `systemctl --user enable --now mugicrew-backup.timer`. Or a cron line (put a `PATH=…` line above it in the crontab):
`0 * * * * cd <your clone> && . ~/.config/mugicrew/backup.env && mugicrew backup push`
(export the variables, or `set -a` first). Either way, the sql-server must have
the credential in its own env (ADR 0370, an-off-machine-backup-recurs-hourly…).

### Where failures show

- The event log: every push attempt writes `backup.pushed` or `backup.failed`
  with Dolt's error.
- The TUI status bar marks a failed latest attempt until the next success.
- `mugicrew backup push` exits non-zero, so a journal or cron mail sees it.
- Doctor's backup freshness row warns when the oldest unpushed commit is over 24 h
  old, which also catches broken credentials.

### Troubleshooting

Resolve by hand; doctor does not probe the destination.

- `database not found`: the destination database does not exist. Seed it (above)
  and check the URL's database name.
- `no common ancestor`: the destination database is empty or unrelated. Do the
  one-time first push with `--force`.
- Non-fast-forward rejection: the backup has diverged (for example, a second
  machine pushed to it). Inspect it first (`dolt clone` it, or read
  `dolt_log`). Then either switch to a fresh database, or run one deliberate
  `--force` push if you know the graph is the truth.

### Restore drill

Proves the copy comes back and opens beads (ADR 0369, the-restore-drill-clones-the-pushed-backup…;
ADR 0384, a-restored-graph-runs-bd-migrate-schema…).
Run it once right after the first push, then periodically. It is untracked.

1. In your real shell, with `BEADS_DIR` still pointing at your real graph,
   push so the copy is current and record the source count:

   ```bash
   mugicrew backup push
   bd count   # the source count; note it
   ```

2. Make a scratch dir and point bd at it before any scratch write. The devShell
   exports the real `BEADS_DIR`, so check the guard. Do this in a second shell
   (or after step 1 in the same one), never before step 1:

   ```bash
   scratch=$(mktemp -d) && cd "$scratch" && export BEADS_DIR=$scratch/.beads
   bd config get issue_prefix   # must not print your real graph's prefix; stop if it does
   ```

   Steps 3–5's scratch-shell commands run from `$scratch`; don't `cd` back into
   the repo before step 3's `bd init`. bd ignores a `BEADS_DIR` whose directory
   doesn't exist yet (step 3's `bd init` creates it) and resolves `.beads` upward
   from the cwd. So a guard that prints the real prefix means the shell is still
   inside the repo, or `BEADS_DIR` still points at the real graph. `cd` into
   `$scratch`, re-run the guard, and do not continue to step 3 until it passes.

3. Clone the copy into the scratch graph and copy its project id into
   `metadata.json` (else bd refuses with a project identity mismatch). Use
   `--user "$DOLT_REMOTE_USER"` for a remotesapi:

   ```bash
   bd init --server -p drill --non-interactive   # creates $BEADS_DIR and its server
   bd dolt stop
   dolt clone [--user "$DOLT_REMOTE_USER"] <backup url> $scratch/clone
   rm -rf $BEADS_DIR/dolt/<db> && mv $scratch/clone $BEADS_DIR/dolt/<db>   # <db> = dolt_database in $BEADS_DIR/metadata.json
   # (if the clone must keep another name, set dolt_database in metadata.json to match)
   # copy metadata._project_id (select value from metadata where `key`='_project_id',
   # via `dolt sql` in the clone) into $BEADS_DIR/metadata.json's project_id field
   bd dolt start
   bd migrate schema   # recreates the dolt_ignore'd tables a push never carries; prints "Schema already at v66"
   ```

   Run no other bd command between `bd dolt start` and `bd migrate schema`. A push
   never carries the `dolt_ignore`'d tables, and bd's read-only opens (`count`,
   `show`, `ready`, `list`) skip migrations, so a read would fail with `table not
   found`. A writable open replays the ignored migration lane because its cursor
   table `ignored_schema_migrations` is itself ignored, so absent on a clone;
   `bd migrate schema` is the explicit writable open. Don't drop it as redundant
   when it prints "Schema already at v66". Same for a file:// and a remotesapi
   backup.

4. Compare the source count from step 1 with `bd count` in the scratch shell
   (the restored count). Then pick one bead id from `bd list` and check that
   `bd show <id>`, `bd ready` and `bd list` all exit 0 with no `table not found`.
   No write is checked.
5. Tear down: `bd dolt stop` in the scratch shell, then `cd` out of `$scratch`
   and `rm -rf $scratch`.

The `events` table and the other tables bd lists in `dolt_ignore` come back
empty from step 3's `bd migrate schema`; their contents (the events audit trail,
wisps, leases) do not. The optional local `bd backup` is still the only thing
that keeps them: `bd backup restore --force <dir>`.
