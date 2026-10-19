# ADR 0218: `graph push` and `graph fetch` are a `graph` noun, and fetch clones into a cache scratch graph

Status: Superseded by ADR 0300
Date: 2026-10-15

Chart mc-t72m.16 (A personal Bead Graph publishes to its per-username ref on the
Forge). Settles grilling mc-t72m.16.4: the CLI surface for the manual Graph push
and for reading another person's Graph ref back, on the mechanism of ADR 0217.

## Decision

1. **A `graph` noun with two verbs.** `mugicrew graph push` is the manual Graph
   push; `mugicrew graph fetch <username> [<dir>]` is read-back. Not "publish"
   (the glossary reserves Publish for Drafts), not a `loop` subcommand (a Graph
   push is not a loop operation and the loop may be off). Read-back is a verb,
   not README-only commands: ADR 0217 §4's clone, `metadata.json` repoint and
   `bd migrate schema` are too easy to get wrong by hand.
2. **`graph push`** pushes only this Workspace's own graph, to
   `refs/mugicrew/graphs/<glab api user>`. It refuses unless
   `land_strategy=gitlab`, refuses with no glab login (pointing to
   `glab auth login`), and always runs ADR 0217 §3's fetched-root check before
   `--force`, refusing a foreign root with both Graph ids. It has no override
   flag: an override would recreate the prototype's foreign-graph overwrite.
3. **`graph fetch` scratch location.** The default is
   `$XDG_CACHE_HOME/mugicrew/graphs/<username>/`; `<dir>` overrides it. Every bd
   call it makes sets `BEADS_DIR=<dir>/.beads` explicitly, never inherited, and
   it refuses a `<dir>` that is, or sits inside, the caller's Workspace or
   current `BEADS_DIR`.
4. **Re-fetch re-clones.** An existing dir carrying a marker from an earlier
   `graph fetch` is deleted and re-cloned; any other non-empty dir is refused.
   A pull would break on a ref force-pushed after `bd compact`.
5. **The scratch server lives only for the fetch.** `graph fetch` starts it with
   `DOLT_REMOTE_INFO_BRANCH=` set, clones, repoints, migrates, stops it, and
   prints `BEADS_DIR=<dir>/.beads bd list` as the next step. Reading is plain bd
   (bd autostarts the server again); there is no read or browse verb.
6. **`__dolt_remote_info__` is documented, not engineered away.** mugicrew still
   neither starts nor stops the Workspace's bd server. The README's gitlab setup
   and this repo's devShell export `DOLT_REMOTE_INFO_BRANCH=`; `graph push` and
   `mugicrew doctor` warn when `refs/heads/__dolt_remote_info__` exists on the
   Forge.
7. **README home.** A `### Graph ref` subsection under **GitLab land strategy**
   covers the automatic push at each Landing, `graph push`, `graph fetch`, the
   export, and discovery by `git ls-remote <forge> 'refs/mugicrew/graphs/*'`.
   No `graph list` verb.

## Considered Options

- **Restart the bd server with `DOLT_REMOTE_INFO_BRANCH=` before a Graph push.**
  Rejected: breaks the hands-off server rule and disturbs running efforts.
- **Default the scratch graph inside the Workspace** (`.beads/graphs/…`).
  Rejected: a second Dolt server's state inside the repo bd autostarts its own
  server for.
