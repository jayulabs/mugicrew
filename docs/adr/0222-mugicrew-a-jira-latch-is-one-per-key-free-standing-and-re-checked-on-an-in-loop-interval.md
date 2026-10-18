# ADR 0222: A Jira latch is one per key, free-standing, and re-checked on an in-loop interval

Status: Accepted
Date: 2026-10-15

Amends: ADR 0115 (mugicrew-brook-owns-task-human-started-guide-closed) §1
(Brook's "later also mirror external state" becomes the `latch` kind).

Chart mc-8ymw.6 (A Brook-owned latch that blocks work until a Jira ticket is
done). Settles grilling mc-8ymw.6.4. The Survey fixed the shape: `kind:latch`,
created by one Go verb, done at Jira statusCategory Done, a failed read
failure-pauses it (ADR 0078 (mugicrew-failure-pause)), a one-shot latch, polled
on a `[jira]` interval. This records how the verb and the re-check work against
the loop seams in docs/research/afk-recheck-of-an-open-gate-bead-loop-seams.md
and the acli reads in
docs/research/acli-jira-status-category-and-gone-or-forbidden-reads.md.
Grilling mc-8ymw.6.5 renamed the kind from `gate` (the Survey's name), which
the glossary already uses loosely (land gate, review gate, candidacy gating),
and settled §7–§8; ADR 0223 settles lost edges.

## Decision

1. **Verb.** `mugicrew latch jira <KEY> --blocks <bead> [--blocks <bead>…]`;
   at least one `--blocks`. It is idempotent on the key: at most one open latch
   per Jira key, found by external ref `jira-latch-<KEY>` (intake's Signal owns
   `jira-<KEY>`). Re-running it adds only the missing blocks edges; it never
   touches a pause. A closed latch does not count, so a fresh one can be filed.
2. **Create-time read.** The verb reads the item through `jira.View` and
   refuses on failure with intake's diagnostics (ADR 0207
   (mugicrew-jira-intake-diagnoses-a-failed-acli-read…)), worded
   `latch jira <KEY>: <what failed>; <fix>`, exit 1, nothing filed. The returned
   key wins for title and ref, printed as `<typed> is now <key>`. A ticket
   already Done prints `<KEY> is already done; no latch filed` and exits 0 with
   no edges.
3. **Title, body, close.** Title `Jira <KEY> is done`. Body: a machine-read
   `Watches: jira <KEY>` line (so a later check seam can read old latches), the
   browse link `https://<site>/browse/<KEY>` (site per ADR 0206
   (mugicrew-jira-intake-takes-its-site…)), and the summary as read at
   creation. Close reason `jira <KEY> reached <status name> (Done)`.
4. **Interval.** `[jira] poll_interval`, a Go duration string, default `15m`,
   minimum `1m`, optional, layered by the cfglayer rule (machine-local wins).
   Unparseable or under the minimum logs `config.unusable` and uses the
   default, as `auto_exit` does. No per-latch override.
5. **Where not-due is decided.** At candidacy: a latch whose last check is
   younger than the interval is not a loop candidate, so it is never claimed.
   Last-checked lives in an in-process map in the loop, stamped by every real
   check, pass or fail. A TUI run-now ignores the interval.
6. **Pause.** A failed check returns an error and the loop's existing count
   pauses it `paused:failing` after 3 consecutive failures; no new cause.
   Resume is the existing path: TUI unpause, or a successful run-now.
7. **Placement.** A latch is a Brook-owned leaf, free-standing like a Task:
   never a Chart child, Checkpoint member, Spec child or Initiative member. It
   is placed only by the `blocks` edges it holds. A session that files one as
   a Draft gives it no parent. This amends ADR 0115 §1: Brook owns `task` and
   `latch`.
8. **What it may block.** A latch may block any bead, a Signal, Chart or Spec
   included; that bead waits until the ticket is Done. How its edges follow a
   blocked bead's succession or pivot, and what happens when they are all
   closed, is ADR 0223
   (mugicrew-an-orphaned-latch-closes-and-a-latch-carries-onto…).

## Considered Options

- One latch per blocked bead: duplicate acli reads and duplicate closes for one
  ticket.
- Not-due decided inside `Run` (returning nil): resets the failure count
  between real checks, so a broken latch never pauses.
- Last-checked as a `Checked at:` body line: survives restart but rewrites the
  body and `updated_at` every interval. The event log as the clock: contradicts
  ADR 0033 (mugicrew-event-log-is-history-not-a-view).
- A latch as a Chart child: the Chart could not drain until the ticket was
  Done, conflating "charted" with "the world is ready".
- Keeping the name `gate`, or `fence` (Chopper's pause fence) or `barrier`
  (reads as a sync primitive): `latch` was unused and names the one-shot
  behaviour.
- Pause on the first bad read: acli gives one identical exit-1 line for a
  network blip and a deleted item, so it would pause on blips.

## Consequences

A loop restart makes every latch due once (one extra acli read each) and loses
at most 2 counted failures. A broken latch pauses after about three intervals
(about 45 minutes at the default).
