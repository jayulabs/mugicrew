# ADR 0206: Jira intake takes its site from Workspace config, not from acli

Status: Accepted
Date: 2026-10-15

Chart mc-8ymw.2 (Pull a Jira ticket in as a Signal by its key). Settles grilling
mc-8ymw.2.4. Intake reads through acli's own login (no token in mugicrew), but the
Signal body needs a browse link `https://<site>/browse/<KEY>`, and acli does not
reliably give us `<site>`: `acli jira workitem view` has no `--site` flag and reads
the active profile, its `--json` carries no browse URL, and the REST `self` URL it
may carry names `api.atlassian.com` rather than the site under OAuth
(docs/research/acli-jira-work-item-read-and-failure-modes.md). So the site is
team configuration.

## Decision

1. **`[jira] site` is the whole section.** One required key, a bare host
   (`site = "acme.atlassian.net"`, acli's own word, no scheme, no path), read from
   the Workspace config by the usual cfglayer rule: shared
   `.mugicrew/config.toml`, the machine-local layer wins.
2. **Checked before acli runs.** A missing `site`, or one containing `/`, refuses
   intake with exit 1 and one line naming the key and the file; nothing is filed
   and acli is not called. The value is not repaired.
3. **No version pin, no project rules.** No minimum-acli setting (a too-old acli
   exits 0 with help text, which the JSON decode already rejects; a floor, if ever
   needed, is a code constant). No project allow-list or default project.

## Considered Options

- Deriving the site from the JSON `self` host: unpinned, and wrong under OAuth.
- `site` optional with a `self` fallback: two paths for one link.
- `base_url` instead of `site`: invites schemes and paths for no gain.

## Consequences

The configured site only builds the link: acli still reads whichever profile is
active, so a human switched to another site can read a same-keyed item there.
Detecting that mismatch (comparing `site` with acli's active profile) belongs to
the Chart's acli-failure-modes question, not to this config.
