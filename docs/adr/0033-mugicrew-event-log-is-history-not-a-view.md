# ADR 0033: the event log is history, a third kind of state beside the liveness view

Status: Accepted
Date: 2026-09-28

Chart: Mugicrew session logs tab. Settles the grilling "Logs vs the
effort observability contract (ADR 0032)".

Amends: ADR 0031 — extends its two-kinds-of-state taxonomy with a third kind.
Leaves ADR 0032 untouched (adds a pointer only).

## Context

The Chart's Spec needs a persisted, structured **event log** (effort
started/ended/failed plus land/reconcile/claim ops, with error traces) so a
human can do a post-mortem even when the TUI wasn't running. Its most valuable
content is exactly what ADR 0032 §2 trimmed from the observability surface:
exit status and stop time. And ADR 0031 admits state outside the bead graph only
as a view that is "re-derivable or safely discardable … never a durable source
of record". A log fits neither of ADR 0031's two kinds:

- It is not **coordination state** (nothing should decide dispatch from it).
- It is not an **observational view** (it is durable by design and cannot be
  re-derived. A crashed effort's result JSON exists only if it was recorded).

## Decision

1. **A third kind of state: history.** Beside coordination state (graph-only,
   ADR 0003) and the observational view (ADR 0031), **history** is a durable,
   append-only record of what happened. It is admissible outside the graph iff
   it is **non-authoritative**: no dispatch or recovery path reads it as truth,
   and losing or truncating it costs diagnostics only, never correctness. The
   event log is history.

2. **A separate surface, not an amendment to ADR 0032.** ADR 0032 answers *what
   is running now* (`mugicrew ps`, the liveness dot), cold from OS truth. The
   event log answers *what happened* (the Logs tab), from its own records.
   Different question, different source. ADR 0032 §2 stays as written: exit
   status / stop time remain out of the liveness surface. Recording them in the
   event log as post-mortem evidence does not make them observability fields,
   and does not take recovery away from the recovery Signal.

3. **The event log never infers liveness.** A `started` event with no matching
   `ended`/`failed` is shown as exactly that. A crash, which leaves no final
   event, is the case the log exists for, so "no end event" must never be read
   as "running". Liveness is the probe's alone (ADR 0025). Joining the probe
   verdict into the Logs tab is a possible later nicety, out of this Spec.

## Rationale

Amending ADR 0032 would put persisted history into a contract that ADR 0031
defines as non-persisted, and blur `ps`'s single source of truth. Promoting the
log to coordination state (so recovery could read `failed`) would make a
lock-free, skip-unparseable-lines file (per the research) authoritative, which
ADR 0003 forbids. Naming history as its own kind keeps every existing rule
intact and states the one rule the log needs: non-authoritative.

## Consequences

- The event schema may freely carry exit status, stop time, session
  id, and stderr tail. They are history fields, not ADR 0032 fields.
- No crewmate, the loop, or a future recovery path may branch on event-log
  contents. Recovery keeps reading the graph and the probe.
- The Logs tab renders raw event sequences. It draws no alive/dead verdict.
