# ADR 0025: AFK effort liveness is a process-table probe (no captured handle)

Status: Accepted
Date: 2026-09-25

Builds on: ADR 0031 (effort liveness is an observational view) and ADR 0032 (the
observability surface contract). 0031 narrowed the admissible mechanism family to
**live-probe** and/or a **volatile in-process cache**; 0032 ruled the
process-local cache out for `mugicrew ps` (a cold, one-shot reader with no live
loop process to hold a registry) and named live-probe canonical. This ADR picks
the concrete live-probe, resolving the research's open questions ("which pid is
authoritative", "cross-process handle transport", "lease semantics") — the
mechanism half of the Chart.

## Context

A live-probe needs *something* to probe, but ADR 0031 forbids a graph-durable
handle and ADR 0032 forbids a process-local cache. That looks like a paradox
until the harness-boundary research is
read carefully: the bead id is **already in the process argv**. The reconcile
subprocess runs as `mugicrew reconcile <id>` (§1) and the `claude` grandchild is
launched with `--name <BeadID>` (§2). The OS process table is itself shared OS
truth that a cold standalone reader can enumerate — no handle need be captured or
transported.

The research also left the lease unresolved. A fact-find (2026-09-25) settled it:
with the dispatch loop OFF (plan of record), **nothing in mugicrew refreshes
`heartbeat_at`** — the claim is one-shot (`succession.go:312-317`,
`loop.go:146-160`, `tui.go:872`), the harness writes nothing to bd, and the
`claude -p` subprocess is bd-blind (`claudecode.go:38-58`). So the lease goes
stale on a timer whether the effort is alive or dead; lease-staleness tracks
"time since claim," not liveness.

## Decision

1. **Mechanism: a process-table scan matched by bead id in argv.** Liveness is
   answered by enumerating the local process table and matching the bead id
   against process argv. **No handle is captured at spawn; the harness contract is
   unchanged** — `claudecode.Run` keeps using `.Output()`; the `Start()/Wait()`
   capture path sketched in the research §1 is *not* taken. This dissolves the
   the research's "which pid to capture" and "handle transport" questions outright:
   there is nothing to capture and nothing to transport. The process table is the
   cold-standalone-reader source ADR 0032 §3 requires.

   Matching is on **exact argv-token equality**, not substring, so `x-1` does
   not match `x-10`. (`claude --name <id>` puts the id in its own argv token;
   `mugicrew reconcile <id>` likewise.)

2. **Liveness verdict: either-present = alive.** A bead is *alive* iff at least
   one live process carries its id in argv — the reconcile subprocess **or** the
   `claude` grandchild. This is robust to the exec model (non-worktree kinds skip
   the reconcile layer, research §1) and parent-agnostic, so it survives the loop
   turning on later (the Chart's standing preference). The reconcile-vs-claude
   distinction is a recovery concern, not an observation one, so the surface does
   not draw it. **Accepted edge case:** a human manually running
   `claude --name <bead-id>` would false-positive; negligible and out-of-scope.

3. **The bd lease is OUT as a liveness signal.** Because nothing refreshes the
   heartbeat while the loop is OFF, lease-staleness cannot distinguish live from
   dead. The OS process-probe is the **sole** liveness authority. The lease fields
   (`started_at` / `heartbeat_at` / `lease_expires_at`) are still parsed to
   populate ADR 0032's field contract, but the "staleness age" field is honestly
   **age-since-claim**, never a death predictor. The surface must never read
   lease-expiry as "dead."

4. **The lease TTL is not pinned.** It is bd's compiled-in default (beads 1.3.0;
   no `lease.ttl`/`claim.ttl` config key is set, and the value is not surfaced in
   help/config/strings). Since the mechanism does not lean on it (decision 3),
   pinning it would be config for a value nothing depends on. Recorded here as
   deliberate non-reliance.

## Consequences

- **Zero change to the harness seam.** `contract.go` / `claudecode.go` are
  untouched; the Result contract gains no pid field. This is the laziest mechanism
  that satisfies "answerable by a cold standalone process from shared/OS truth."
- **`mugicrew ps` implementation shape:** enumerate `/proc/*/cmdline`, split on
  NUL, match the bead id token. No new dependency — the stdlib reads `/proc`.
  <!-- ponytail: Linux-only /proc scan; reach for a cross-platform lib (gopsutil)
  only if mugicrew ever targets non-Linux, which OE ADR 0007's static-binary intent
  does not require today. -->
- **The TUI** consumes the single derived alive/dead bit from the same probe
  (ADR 0032 §1); the "dead vs slow" distinction comes entirely from the OS-probe
  bit, not the lease.
- **Single-host only.** A remote-host effort would defeat a local `/proc` scan,
  but no such effort exists today; it is out-of-scope for this Chart.
- **Now unblocked:** the glance-legibility prototype (how a dead effort is drawn
  distinctly) — both the mechanism (this ADR) and the surface contract (ADR 0032)
  have landed.
