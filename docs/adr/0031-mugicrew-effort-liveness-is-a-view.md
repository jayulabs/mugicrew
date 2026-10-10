# ADR 0031: effort liveness is an observational view, not coordination state

Status: Accepted
Date: 2026-09-25

Amends: ADR 0003 — clarifies that "the bead graph holds all coordination state;
the loop holds none" governs *coordination* state only. A pure runtime **view**
of a process the graph does not own may live outside the graph, under the
conditions below.

## Context

ADR 0003 makes activation a deliberately **stateless** dispatch loop: the bead
graph holds ALL coordination state, the loop holds none, and everything is
re-derived each tick — so a crash re-derives everything and loses zero
coordination state.

The Chart *Effort liveness & the AFK process-observability surface* has
to expose, per active AFK effort, whether "in_progress + process alive" or
"in_progress + process dead". "Is the OS process alive?" is exactly the runtime
fact the graph does not hold. The research established the facts:
mugicrew owns no process handle (the `exec.Cmd` is discarded at every seam), the
`session_id` is an after-the-fact resume/debug UUID (not probeable for a running
process), and the only durable trace is bd's `in_progress` + assignee + the
unparsed lease fields (`heartbeat_at` / `lease_expires_at`) — a coarse proxy,
not OS truth.

This raises the governing question this ADR settles: **may observational
liveness state live outside the bead graph — because it is a *view* of a running
process, not coordination state — or must every trace be graph-recorded?**

## Decision

**Effort liveness is an observational view, not coordination state.** We draw a
boundary that ADR 0003 did not name:

1. **Two kinds of state.**
   - **Coordination state** — what work exists, who is busy, what is dispatched
     next: anything a crewmate reads to decide what to do. This is graph-only,
     exactly as ADR 0003 requires.
   - **Observational view** (the *effort liveness view*) — a read-model over a
     running process the graph does not own (alive-vs-exited, and if exited,
     exit status / stop time). It is *not* coordination state.

2. **A view may live outside the graph iff both hold:**
   - **(a) Non-authoritative for dispatch** — no crewmate's dispatch decision
     treats it as authority; the graph remains the sole coordinator.
   - **(b) Re-derivable or safely discardable** — it is re-derived from OS /
     harness truth on demand, never a durable source of record.

   Effort liveness satisfies both, so it is admissible outside the graph.

3. **Reconciliation with ADR 0003 (the amendment).** ADR 0003's "the loop holds
   no state / nothing is stored in the loop / re-derives every tick" means no
   *coordination* state. A volatile observational view (e.g. an in-memory
   registry in the running process) does **not** violate it. **Corollary:**
   because the view is re-derivable and non-authoritative, **losing it on a
   crash is a non-event** — ADR 0003's "a crash loses zero coordination state"
   property stands unchanged.

4. **Derived constraint on the mechanism (not the mechanism decision itself).**
   Condition (b) disallows a **durable liveness handle in the graph as a source
   of record** — writing a pid/handle to bd would be coordination-shaped runtime
   state, and is unsound anyway (a pid is stale the instant the process dies, and
   subject to reuse). This narrows the admissible mechanism family to
   **live probe** (canonical) and/or a **volatile in-process cache**; the actual
   pick is a separate defog bead, not this one.

5. **"Dead" is a derived view.** The dead state is computed on demand from
   (`in_progress` + stale lease + no live process); nothing is persisted. In
   particular the **unclean-exit** case (crash/kill drops exit code + stop time,
   leaving the bead `in_progress` with a stale lease) is *rendered*, not written
   back. Note a **clean** exit is already graph-recorded — a successful effort
   calls `bd close` — so this view exists to cover the failure path the graph
   does not capture.

## Consequences

- **ADR 0003 is amended, not overturned.** Coordination state stays graph-only;
  the stateless-loop invariant and its self-healing property are intact. Only the
  boundary is newly named: a pure runtime view is permitted outside the graph.
- **The mechanism frontier narrows** to live-probe and/or volatile cache; the
  graph-recorded-handle option is ruled out as a source of record. The concrete
  mechanism graduates as its own defog bead now that this principle and the
  research facts have both landed.
- **The observability surface reads a view, not the graph, for
  liveness.** It may render a derived "dead" state without any graph mutation.
- **Persisting terminal facts is out-of-scope here.** If exit status / stop time
  is ever written back to a bead, that write reacts to a dead process — it is
  recovery/coordination state and belongs **in-graph, to the recovery Signal**
  (recovery is Out-of-scope for this Chart per its map). This pre-answers the
  "why not just save the exit code?" question: because doing so is recovery, not
  observation.

> Extended by ADR 0033: a third kind of state, **history** (the event log),
> durable but non-authoritative, sits beside coordination state and the
> observational view.
