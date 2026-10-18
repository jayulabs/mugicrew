# ADR 0061: `mugicrew stats` sums session-level telemetry per bead, and marks unknown cost as partial instead of guessing it

Status: Accepted
Date: 2026-10-05

Chart: Bead telemetry. Settles the grilling "Telemetry query verb
shape and rollups". Builds on the ADR 0029 cost-attrs amendment
and the retention research. §2 and Consequences amended by ADR 0286
(mugicrew-stats-filter-takes-a-resolved-scope-map…).

## Context

The event log carries `dur_ms` on `session.*`, `hitl.*` and `effort.*` end
events, and session cost (`cost_usd`, `api_ms`, `models{…}`) on the session and
HITL end events only. A bead may run many efforts and sessions (retries,
failures, HITL resumes recorded as deltas). Some sessions have no cost at all
(killed, `/clear`, events written before the enrichment). Rotation (ADR 0028)
keeps roughly 41 days of history at the loop-era rate.

## Decision

1. **Verb:** `mugicrew stats [<bead>] [--since <dur|date>] [--by crewmate|model|day] [--json]`.
2. **Scope:** an optional positional bead. If it is a Chart or Checkpoint, the
   Logs-tab rule applies (`bead==X || parent==X`, direct children only). With
   no bead, every bead in the window is included. There is one row per bead,
   then a TOTAL row.
3. **Window:** `--since` filters by event time, so a bead that straddles the
   cutoff counts only its sessions inside the window. The default is the whole
   retained log.
4. **WALL is agent time:** the sum of `dur_ms` over `session.*` and `hitl.*`
   end events. HITL wall time includes time the human was idle in the pane.
   API (`api_ms`) sits next to it as the busy measure. Effort `dur_ms`
   (reconcile overhead, headless only) is not reported.
5. **Retries:** everything is summed, with SESSIONS and FAILED counts so retry
   cost shows. There are no per-attempt rows.
6. **Unknown cost:** COST is the known sum, marked partial (`$1.23+`) with a
   footnote counting the sessions that have no cost. Events written before the
   enrichment are counted the same way, so no backfill is needed.
7. **`--by`:** only one at a time. It replaces the bead rows with group-key
   rows and combines with scope and `--since`. `day` is the local date of the
   end event. `model` splits only COST and tokens (the `models` keys).
   SESSIONS, FAILED, WALL and API are shown as `–` for model rows.
8. **Table:** `BEAD|<key> SESSIONS FAILED WALL API COST IN OUT CACHE_R CACHE_W`.
   Values are human-formatted. There is no title column (the log carries no
   titles). Rows are sorted by COST descending, with TOTAL last.
9. **`--json`:** `{"rows":[…],"total":{…}}`. Each object is `{key, sessions,
   failed, wall_ms, api_ms, cost_usd, cost_partial_sessions, in, out,
   cache_read, cache_write}` with raw numbers. Fields that cannot be split are
   omitted, never 0.
10. **Retention is accepted as is.** ADR 0028 is not changed. `stats` reports
    only what the log still holds.
11. **CLI only.** A TUI surface would be a separate Signal.

## Considered and rejected

- **Raising the rotation cap, or trimming `session.ended` result text**
  (roughly half of all log bytes). History is not authoritative (ADR 0033),
  and about six weeks answers "what did this Chart cost". Revisit only if this
  becomes a real problem.
- **`?` for a whole row when any session lacks cost.** Every row from before
  the enrichment would become useless. Summing silently would understate cost
  with no sign that it does.
- **Effort-level wall time.** It exists only for headless sessions and adds
  reconcile overhead.
- **Attributing each session to its costliest model** under `--by model`.
  This would invent precision that the data does not have.

## Consequences

- `stats` reads history, and bd for scope only (amended by ADR 0286
  (mugicrew-stats-filter-takes-a-resolved-scope-map…)), so ADR 0033 needs no
  amendment. Nothing decides dispatch from these numbers.
- Grandchildren of a Chart are not counted, the same blind spot the Logs tab
  has (ADR 0029).
- Wall time for HITL sessions overstates how long the agent was actually
  working. API time is the measure to compare instead.
