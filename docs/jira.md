# Jira

Jira is optional. Turn it on to file a Jira work item as a Signal, or to hold
beads until a Jira ticket is Done. mugicrew reads Jira only through `acli`, the
Atlassian CLI, and stores no credential.

## Enable it

Set the site once for the Workspace, as a bare host:

```bash
mugicrew init workspace --jira-site acme.atlassian.net
```

`--jira-site` writes `[jira] site` in `.mugicrew/config.toml` (an empty value
clears it). Hand edits go in the same table:

```toml
[jira]
site = "acme.atlassian.net"   # a bare host: no scheme, no slash
poll_interval = "15m"         # how often an open Latch re-reads its ticket
```

A site with a slash in it is refused, and every Jira verb names the config file
to fix. `poll_interval` is described in [Config options](configuration.md).

## acli and its login

Install `acli` as [the install guide](install.md) says (its Jira section), then
log in:

```bash
acli jira auth login
```

mugicrew runs `acli` by name from your `PATH` with your login. When a read
fails it tells you why and the fix: `acli` missing, not logged in, logged in to a
different site than `[jira] site` (switch with
`acli jira auth switch --site <host>`), or a key it cannot read (check the key
and your access).

## File a Jira item as a Signal

```bash
mugicrew intake jira PROJ-123
```

This files a Signal titled `PROJ-123: <summary>` and prints its id. Its body only
points at the work item; triage reads the item from Jira. The Signal carries the
external ref `jira-PROJ-123`. If Jira has renamed the item, the key Jira returns
is the one used.

In the TUI, press `n` and then `j` (New Signal from a Jira issue), and type the
key. The entry shows only when `[jira] site` is set.

Triage never charts or fast-lanes a Jira Signal. If the item is worth doing,
triage turns it into an Initiative that takes the Signal's title and its
`jira-PROJ-123` ref; REJECT and DUPLICATE still apply. What follows is in
[Organizing with Initiatives](../site/learn/organizing-with-initiatives.mdx).

Filing the same item again keeps lineage instead of a second effort: the new
Signal is superseded onto the live bead that already carries `jira-PROJ-123`,
and the verb prints which one.

## Hold beads until a ticket is Done

```bash
mugicrew latch jira PROJ-123 --blocks <bead> [--blocks <bead>...]
```

This files a Latch that blocks each `--blocks` bead until the ticket's status
category is Done. It carries the external ref `jira-latch-PROJ-123`, and its body
starts `Watches: jira PROJ-123`. An already-Done ticket files nothing; a live
Latch for the key is reused and gains only the missing blocks. An Initiative
cannot be a target. Crewmates file Latches with `--drafted-by <bead>`, which
leaves the Latch a Draft until its session publishes.

The loop re-reads each open Latch on `[jira] poll_interval` (default `15m`,
minimum `1m`; a smaller value is rejected with a `config.unusable` warning and
`15m` applies), with no session.

### Latch outcomes

| Outcome | Close reason | When |
| --- | --- | --- |
| completed | `completed: jira <KEY> reached <status> (Done)` | the ticket is Done; the beads it blocked are unblocked |
| orphaned | `orphaned: no open bead waits on jira <KEY>` | no open bead is blocked by it any more; it never reopens |
| withdrawn | `withdrawn` | you withdrew it: `mugicrew withdraw <latch>`, or `X` in the TUI |

### A read that fails

`acli` prints one line for a ticket that is deleted, forbidden or unreachable, so
mugicrew cannot tell them apart. A failed read counts as a failure; after three in a row
the Latch pauses (`paused:failing`) while the beads it blocks stay blocked. The message
names the three causes. If the ticket is gone, withdraw the Latch:

```bash
mugicrew withdraw <latch>
```

Withdrawing closes the Latch alone; the beads it blocked are released and stay
open. If the cause was access or the network, fix it and unpause the Latch with `p` in the TUI (there is no CLI verb
for it).

To drop a Latch for only some of the beads it blocks, run `bd dep remove` on
those edges; removing the last one leaves the orphan close.

## Enforce external ID

[Enforce external ID](configuration.md) (`[initiatives] external_ref = true`,
with Require Initiatives) makes each Initiative carry a unique external ref. A
Signal filed with `intake jira` already has one, `jira-<KEY>`, and reconciling it
into an Initiative gives the Initiative that ref. An Initiative made without a
Jira item takes a ref you type; set a missing one later with
`bd update <id> --external-ref R`. Latches do not need one: their
`jira-latch-<KEY>` ref is their own.
