# ADR 0032: the effort-observability surface & per-effort field contract

Status: Accepted
Date: 2026-09-25

Builds on: ADR 0031 (effort liveness is an observational view). Where 0031
settled *whether* a liveness view may exist, this ADR settles *what it shows* and
*what it therefore requires of the mechanism* — the surface half of the Chart
"Effort liveness & the AFK process-observability surface".

## Context

Per-active-AFK-effort, the surface must make "in_progress + alive" vs "in_progress
+ dead" obvious. Two surfaces are candidates: the `mugicrew ps`-style verb the
Signal proposes, and the existing TUI (a 3s bd poller with zero process
introspection — a crashed effort renders `◐` indefinitely, `view.go`). bd already
carries `started_at` / `heartbeat_at` / `lease_expires_at` but mugicrew does not
parse them (per the research). This ADR fixes the surface contract so the
mechanism bead has a target to satisfy.

## Decision

### 1. Two surfaces, distinct jobs

- **`mugicrew ps` (new verb)** — the authoritative, one-shot, scriptable snapshot
  of active AFK efforts and their liveness. It is what a human runs over SSH or a
  script greps. It offers a human table and a `--json` form (mirroring bd's own
  dual output); exact columns are an implementation detail.
- **The TUI** — consumes the *same liveness bit* so a dead effort stops rendering
  as a live `◐`. The TUI carries only the derived alive/dead state, **not** the
  full field table; the table is `ps`'s job. (How "dead" is drawn distinctly — a
  new glyph / derived state, and validating it reads as dead at a glance — is
  **not** decided here; it graduates as the glance-legibility prototype — see
  §5.)

### 2. Per-effort field contract

Per active AFK effort the surface presents:

| Field | Source |
| --- | --- |
| bead identity (id · title · kind) | bd (parsed today) |
| claimant (assignee — loop vs human) | bd (parsed today) |
| spawned command (`mugicrew reconcile <id>` / `claude -p …`) | constructible from the bead |
| start time | bd `started_at` (needs parsing) |
| liveness (alive \| dead) | the mechanism — derived, ADR 0031 |
| staleness age | bd `heartbeat_at` / `lease_expires_at` (needs parsing) |

**Trimmed from the contract: exit status and stop time.** For any effort still
visible in an `in_progress` listing, these are the unclean-terminal facts that are
dropped today (a clean exit leaves via `bd close` and drops out of the listing; a
crash leaves `in_progress` with a stale lease and no captured code/time). Per
ADR 0031 they are recovery/terminal state — in-graph, owned by the recovery
Signal — and out-of-scope for this observation surface.

### 3. Requirement this surface imposes on the mechanism

**Liveness must be answerable by a cold, standalone process reading only shared /
OS truth — with no dependence on a live loop process's in-memory state.**
`mugicrew ps` is a one-shot invocation, and per the plan-of-record the dispatch
loop is OFF, so no long-lived loop process exists to hold a registry when `ps`
runs. A process-local volatile cache is therefore unreachable as `ps`'s liveness
source. This rules that option out for the standalone verb and is consistent with
ADR 0031 naming **live-probe** the canonical mechanism. This ADR sets the
requirement; the concrete pick remains the mechanism's.

### 4. Scope

AFK efforts only. A HITL / human claim (different process boundary, Signal)
is omitted or shown without a liveness column, never given a fabricated
alive/dead verdict.

### 5. Drawing the bit in the TUI (settled by the glance-legibility prototype)

Liveness renders as a **second, separate glyph column** beside the unchanged
status glyph, not as a new state glyph: `●` alive (green), `○` dead (red), `·`
no verdict (not in_progress, or HITL per §4). The status glyph keeps meaning
*graph status*, and `◐○` reads "claimed, but nothing is running". Age-since-claim
is a dim trailing suffix and **never colours the liveness verdict**. A fresh
crash (4m) and a slow live effort (3h) must look different by the dot alone.
The detail pane adds a `liveness` field line (`alive · claimed 3h ago` /
`dead — no reconcile/claude process · claimed 4m ago`).

Rejected: swapping `◐` for `✗` (one glyph carrying two facts), a `DEAD` pill
(too wide for Mobile-SSH widths), and a tab-bar dead-count banner (deferred;
`mugicrew ps` is the listing). Throwaway variants:
`refs/mugicrew/prototypes/<id>` (local, never merged).

## Consequences

- The mechanism must choose a mechanism whose liveness answer survives a cold
  standalone reader (live-probe, or a cache in shared storage — never
  process-local memory).
- The cheap fields (start time, staleness age) require only that `parseBeads`
  (`succession.go`) start reading the lease fields bd already returns.
- The TUI change is minimal: consume one derived bit and draw it as the §5
  liveness-dot column.
- Exit status / stop time stay out of this surface; surfacing them later is a
  recovery concern, not an observability one.
- See also ADR 0033: the event log records exit status / stop time as
  *history* on a separate surface (the Logs tab). This contract is unchanged.
