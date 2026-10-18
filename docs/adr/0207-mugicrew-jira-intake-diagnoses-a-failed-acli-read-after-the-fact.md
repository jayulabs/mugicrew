# ADR 0207: Jira intake diagnoses a failed acli read after the fact

Status: Accepted
Date: 2026-10-15

Chart mc-8ymw.2 (Pull a Jira ticket in as a Signal by its key). Settles grilling
mc-8ymw.2.6. Intake reads one work item with
`acli jira workitem view <KEY> --fields summary --json` against acli's active
profile, with the site from Workspace config (ADR 0206
(mugicrew-jira-intake-takes-its-site…)). The human running it needs a failure to
say what broke and how to fix it, but `view`'s stderr alone cannot tell
"not logged in" from "bad key" (docs/research/acli-jira-work-item-read-and-failure-modes.md).

Observed on a logged-in acli 1.3.39-stable:

- `acli jira auth status` exits 0 and prints `✓ Authenticated` then indented
  `Site: <host>`, `Email: …`, `Authentication Type: …` lines.
- `view --json` exits 0 with the summary at `.fields.summary` and the key at
  `.key`; `self` names an internal Atlassian host, not the site.
- An unknown key and an unreadable one both exit 1 with
  `✗ Error: Issue does not exist or you do not have permission to see it.`

## Decision

1. **One read on the happy path.** Intake runs `view` only. `auth status` runs
   only after `view` exits non-zero, to diagnose it.
2. **Diagnosis order after a failed `view`:**
   - `auth status` exits non-zero: not logged in.
   - its `Site:` differs from `[jira] site`: wrong site. If no `Site:` line
     parses, skip this check.
   - otherwise: the key is unknown or not visible.
3. **Every failure exits 1, files nothing, and prints one stderr line**
   `intake jira <KEY>: <what failed>; <fix>`, quoting acli's `✗ Error:` text
   where there is one:
   - exec fails (acli not on PATH): `acli not found on PATH; install the
     Atlassian CLI, then 'acli jira auth login'`
   - not logged in: `acli is not logged in to Jira; run 'acli jira auth login'`
   - wrong site: `acli is on <their-site>, Workspace config wants <site>; run
     'acli jira auth switch --site <site>'`
   - unknown or not visible: `acli could not read <KEY> ("<acli text>"); check
     the key and your access on <site>`
   - stdout is not JSON, or `.fields.summary` is missing or empty:
     `acli gave unexpected output for <KEY>; it may be too old (check
     'acli --version' and update)`. This also catches a too-old acli that
     exits 0 with help text.
4. **The returned key wins.** If `.key` differs from the typed key (Jira
   resolves a moved item's old key), intake files under `.key` (title, ref
   `jira-<key>`, link) and prints `<typed> is now <key>`, so one item never
   carries two refs.

## Considered Options

- Probing `auth status` before every `view`: catches a wrong-site success, at
  the cost of a second acli call on every intake.
- Never comparing sites, only naming the site in the message: leaves the human
  to spot a profile mismatch that acli already reports.
- A distinct exit code per failure: no script branches on it, since intake is
  human-run. Add codes if a caller ever needs them.
- Printing acli's line as-is plus a hint line, or dropping it: two lines, or the
  diagnosis is lost.

## Consequences

A successful `view` against the wrong profile goes unchecked: it files the
other site's summary under this site's link. That needs two sites and a
colliding key, and is accepted. Tests fake `acli` on PATH as forge fakes glab,
with a separate stderr file per call and a canned `auth status` response.
