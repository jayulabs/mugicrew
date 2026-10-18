# ADR 0245: Install is doctor-led from a thin install doc with per-feature sections, and the runbook stays a dry-run script

Status: Accepted
Date: 2026-10-16

Chart mc-uqpj.2 (Standalone install and bootstrap of mugicrew in a client
Workspace). Settles grilling mc-uqpj.2.9.

## Decision

1. **`apps/mugicrew/docs/install.md` is the one consumer-facing install doc,
   and it is thin.** The path it describes is: `npm i -g @jayulabs/mugicrew`
   (`npm i -D` in a JS repo), `mugicrew init`, then `mugicrew doctor`, fixing
   each FAIL from its hint until doctor passes. The doc holds only what doctor
   cannot: the npm line, the credential steps, and a short list of what doctor
   checks.
2. **Doctor's hints carry the fix.** Each prerequisite FAIL names the minimum
   version and the install command for the running OS. A missing bead graph says
   `run mugicrew init`. A hint too long for one line links to its install.md
   section by anchor.
3. **Prerequisites are core plus per feature.** Core (always): bd, dolt, tmux,
   claude, git. `land_strategy = "gitlab"` adds glab and the GitLab https
   credential steps. `[jira]` adds acli and `acli jira auth login`. install.md
   has a Core section and one section per feature, and doctor checks a feature's
   tools only when that feature is configured.
4. **The credential steps in install.md cover one account per machine.** The
   second-account `GITLAB_TOKEN` export (ADR 0247,
   gitlab-https-credentials-are-glab-s-helper-with-its-user-set…) is a dry-run
   device and lives only in the runbook.
5. **`docs/client-dry-run-runbook.md` stays a script for the owner's dry run.**
   Its Workspace config step links to install.md for each account. Step 3
   becomes `mugicrew init --prefix <p>` (a distinct prefix per human). Step 4
   links to install.md's GitLab section plus the runbook's own second-account
   note. Step 5 (`export DOLT_REMOTE_INFO_BRANCH=`) is deleted, because init
   persists it in `.beads/.env`.
6. **The README's `## Install` is the npm one-liner plus a link to install.md.**
   Its Bd store mode section tells a new Workspace to run `mugicrew init`, and
   it keeps the steps for moving an embedded store to server mode.
7. **The npm package ships a minimal README.** It says what mugicrew is, gives
   the install line, says to run `mugicrew doctor` next, and links to install.md.

## Considered Options

- **Restructure the runbook in place.** Rejected: the runbook is a throwaway
  procedure built around the owner's two accounts, group and Jira site, and its
  deliverable is filed blockers.
- **A long step-by-step install doc.** Rejected in favour of doctor-led setup:
  doctor already attaches a hint to every FAIL, and a hint can't go stale
  against the check it belongs to.

## Consequences

The dry run exercises the same install.md a consumer reads. The per-feature
sections and the conditional doctor checks are the seam a future module or
plugin system would lift out; that idea is filed separately and is not part of
this Spec.
