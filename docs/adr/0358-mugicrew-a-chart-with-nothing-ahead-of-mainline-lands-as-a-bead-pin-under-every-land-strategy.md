# ADR 0358: mugicrew a Chart with nothing ahead of mainline lands as a bead pin under every land strategy

Status: Accepted
Date: 2026-10-28
Amended by ADR 0388 (a-chart-lands-through-an-approval-carrying-a-chart-packet) (§1: Nami checks for nothing ahead before spawning an Approval; a pin-only Chart gets none).
Amends: ADR 0014 (mugicrew-direct-spec-cut-base) §2's empty-anchor rationale, for Charts; extends ADR 0287 (mugicrew-a-direct-spec-s-anchor-is-a-bead-pin…) to Charts.

## Context

Under gitlab, a Chart whose `chart/<id>` has no commits ahead of the Forge main
opens a Land MR that GitLab can never merge (`commits_status`: "The source branch
must exist and contain commits."). Jinbe treats it as pending forever and no Spec
spawns (docs/research/empty-or-adr-only-chart-land-to-spec.md §4). The stall
predates finish-in-place, but ADR 0357 (mugicrew-a-survey-with-no-fog-finishes…)
makes it the usual shape: every no-fog Chart that needs no ADR has nothing to
land. Under local the same Chart lands an empty `--allow-empty` squash carrying
its `Chart:` trailer. Mugicrew never writes to the Forge main itself (ADR 0189,
ADR 0212), and ADR 0287 already replaced the direct Spec's empty anchor commit
with a bead pin. This ADR does the same for a zero-commit Chart.

## Decision

1. **A Chart with nothing ahead of mainline lands as a pin, not a commit, under
   every land strategy.** "Nothing ahead" means the `chart/<id>` head is an
   ancestor of mainline: the fetched Forge main under gitlab, local main under
   local. A Chart with any commit, such as an ADR-only Chart, lands as before.
2. **The pin is an `Anchor: <sha>` line on the Chart bead, set if unset.** The
   sha is the mainline tip at land time, read the same way as a direct Spec's
   (ADR 0287 §1). It is a recorded base, not a land commit.
3. **The land leg writes it.** Under gitlab, Jinbe's Landing leg opens no MR. It
   writes the pin, then closes the Landing `landed: <sha>`. Under local, the
   land leg writes the pin instead of the empty squash and closes the Landing
   the same way.
4. **An already-open zero-commit MR is closed by the same leg.** It writes the
   pin first, then closes the MR with a note that the Chart landed as a pin,
   then closes the Landing. A failure partway through retries cleanly, and
   closing the MR ourselves keeps `mirrorMR`'s closed arm from filing a
   "Closed MR" hold Task.
5. **Readers take a pin or a trailer.** `reconcileChart` counts a Chart as
   landed when it has a pin or its `Chart:` trailer is on mainline. Zoro's
   `checkpointBase` reads the Chart's pin first, then the legacy `Chart:`
   trailer; neither is a hard error, never a `rev-parse main` fallback. This
   includes the Workspace-Chart shortcut and nested repos (the pin is the
   child's mainline tip).
6. **No migration.** Charts that already landed as empty commits keep resolving
   through their trailer (as ADR 0287 §3–4).
7. **A pin-landed Chart skips the post-land renumber.** Nothing was added, so
   there is nothing to clash.

## Considered Options

- **Skip the MR and stamp an empty trailer commit on the Forge main.** Rejected:
  mugicrew would push to a protected main, which goes against ADR 0189 (a human
  merges) and ADR 0212 (no Forge-main writes for anchors).
- **Map `commits_status` to a hold Task carrying `stampDo`.** Smallest change,
  but every no-fog gitlab Chart would then need a human Maintainer push.
- **A finishing Survey always commits something.** Covers only finish-in-place,
  not older zero-commit Charts, and it is unverified that GitLab squash-merges an
  MR whose only commit is empty.
- **Pin under gitlab only.** Rejected in favour of one anchor shape for a
  zero-commit Chart across strategies, as ADR 0287 chose for direct Specs.
- **Fix it in an offshoot Signal.** Rejected: the Destination's "every no-fog
  Chart takes this path" must hold under gitlab when this Spec lands.

## Consequences

- `TestLandEmptyChartStampsAnchor` changes: a zero-commit Chart's land adds no
  commit to mainline and writes the Chart's pin.
- "The landed base is never stored on a bead" (ADR 0010) no longer holds for a
  zero-commit Chart: its base is the pin.
