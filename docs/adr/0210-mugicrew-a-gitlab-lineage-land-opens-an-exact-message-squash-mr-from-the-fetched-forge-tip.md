# ADR 0210: A gitlab lineage land opens an exact-message squash MR from the fetched Forge tip

Status: Accepted
Date: 2026-10-15

Chart mc-t72m.15 (Every direct move of mainline has a Forge path under gitlab).
Settles grilling mc-t72m.15.8. Charting settled that under `land_strategy=gitlab`
`mugicrew land-lineage` pushes the lineage branch, opens an MR, prints its URL and
exits; a human merges and nothing watches it. This ADR says how the MR is opened
so the `Lineage:` trailer lands intact, and amends ADR 0119
(lineage-branches-land-by-a-locked-fast-forward) on what "byte-for-byte" means
under gitlab. Evidence: docs/research/lineage-land-under-gitlab-trailer-survival-and-sha-consumers.md.

## Decision

1. **Check the Forge project first.** land-lineage under gitlab runs
   `checkForgeProject` (`ValidateSettings`), as a container land does, so the
   README's `%{title}\n\n%{description}` template is in force.
2. **Check against the Forge tip, not local main.** Under the land lock it runs
   `FetchMainline`, then the existing refusals (`MainDirty`, `LineageShape`,
   `MainMoved`, `NoLineageTrailer`) against the fetched main, then pushes. A
   branch cut from a stale main is refused before it can force a rebase under
   ff or semi-linear.
3. **The MR is squash-on with the exact commit message.** `forge.CreateMR` is
   reused unchanged (`squash=true`), with title = the commit's subject and
   description = its body (`lineage merge <src>` / `Lineage: <src> <tip>`). The
   template then renders the commit's own message and GitLab skips the
   one-commit squash, so the original object merges. No `Tip:`, `Graph:` or
   `TrailerBlock` line goes in the description (ADR 0187 §1).
4. **A re-run reuses the open MR.** When `FindOpenMR` finds one for the branch,
   land-lineage prints its URL and exits 0, with no push or second MR.
5. **The guarantee under gitlab is weaker (amends ADR 0119).** The commit lands
   byte-for-byte when Forge main has not moved between open and merge and no
   human edits the squash message. Otherwise a rebase or squash rewrites it and
   only the `Lineage: <src> <tip>` value survives, which is all lineage reads.
   Under local nothing changes.
6. **Nobody in mugicrew deletes the spent branch under gitlab.** The Forge copy
   goes by the project's "delete source branch" default; the local branch is
   kept (lineage treats it as landed once the trailer reaches local main, and it
   is the human's handle on a closed MR) and the human prunes it.
7. **Land through mugicrew, not git, under gitlab.** lineage's TUI hint
   `git merge --ff-only` would diverge local main from the Forge's. lineage stays
   mugicrew-agnostic, so the warning is a README note, not lineage code.

## Considered Options

- **`squash=false` on the lineage MR.** Rejected: GitLab refuses it at merge under
  `squash_option=always`, stranding the MR.
- **Enforce byte-for-byte** (refuse ff/semi-linear, or watch the MR and rebuild).
  Rejected: watching contradicts fire-and-forget, and lineage needs only the
  trailer value.
- **`remove_source_branch=true` on the MR, or delete the local branch at push.**
  Rejected: the first changes `CreateMR` for one caller; the second loses the
  human's handle if the MR is closed.

## Consequences

lineage sees a merged lineage MR only after a `FetchMainline`. A second
`lineage sync` before then builds on the pre-merge base; the next land-lineage
fetches first and refuses it `MainMoved`, and re-syncing builds on the fetched
main.
