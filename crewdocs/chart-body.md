<!-- hand-kept · summarises ADR 0006 (offshoot Signals), ADR 0008 and ADR 0013 (a Chart charts toward exactly one Spec), ADR 0059 (Drafts), ADR 0088 (this doc's split) -->

# The Chart

A Chart is one bead on the beads graph, labelled kind:chart: the shared map
charting a Signal toward exactly one Spec, and the canonical artifact of its
charting branch (chart/<chart-id>). Its defog beads are its child beads
(membership). The Chart is an **index**, not a store: a decision lives in
exactly one place, its defog bead, so the Chart body only gists it and links.

## The Chart body

The whole Chart at low resolution, in five sections:

- **Destination**: the one Spec this Chart is charting toward: WHAT to build,
  in and out. One or two lines; every session orients to it before picking a
  defog bead.
- **Notes**: domain; skills every session should consult; standing preferences
  for this effort.
- **Decisions-so-far**: the index: one line per closed defog bead, enough to
  judge relevance, then follow the link for the detail. Empty at charting;
  filled as defog beads close.
- **Not-yet-specified**: in-scope fog you can't bead yet (see Fog of war).
- **Out-of-scope**: work ruled beyond the Spec (see Out of scope).

**At triage** (the Chart's first body, written by Luffy): the Destination names
the one Spec (what is built, what is in and out); Notes carry what the human
settled at triage; Decisions-so-far and Not-yet-specified are left to Nami,
each present but empty; Out-of-scope carries only calls the human made at
triage.

## Defog beads

Each defog bead is a child of the Chart; its bead id is its identity. Its body
is the question it resolves, sized to one agent session. Three defog kinds, and
nothing else:

- **grilling**: interview a human to answer an open question.
- **research**: read primary sources to establish a fact (AFK; resolved
  autonomously).
- **prototype**: build a throwaway to measure or decide a mechanism.

A session **claims** a defog bead by assigning it, before any work, so
concurrent sessions skip it. Ordering (blocking) edges gate the frontier (bd
ready): a defog bead is takeable when every bead blocking it is closed.

### Creating a defog bead

Each new defog bead is a Draft child of the Chart, drafted by your own bead:

    bd create --type task -s deferred -l kind:<grilling|research|prototype>,drafted-by:<your bead id> \
      --parent <chartID> --no-inherit-labels --title "..." --description "..."

If it fails with *already exists* or *1213*, re-run the same create: nothing was written, and the retry gets a fresh ID.

Always pass --no-inherit-labels and set kind explicitly: a child that inherits
the Chart's kind:chart breaks one-kind routing. Wire ordering only where one
defog bead genuinely depends on another's answer (e.g. a prototype that needs a
research fact first), so bd ready shows the true frontier:

    bd dep add <blocked-bead> <blocking-bead>

Most fog is independent: spawn flat and skip this when there's no real
dependency.

## Fog of war

The Chart is deliberately incomplete: don't chart what you can't yet see.
Beyond the unclosed defog beads lies fog: decisions you can tell are coming but
can't yet pin down. The test for **bead vs. fog** is whether you can state the
question precisely now, not whether you can answer it:

- **Bead it** when the question is already sharp, even if it's blocked.
- **Not-yet-specified** when you can't phrase it that sharply yet. Don't
  pre-slice the fog; one patch may graduate into several beads, or none, once
  the frontier reaches it.

Resolving a defog bead clears the fog ahead of it, graduating whatever's now
specifiable into fresh defog beads, and clearing those patches from
Not-yet-specified; that happens in later sessions. A charting session only
seeds Not-yet-specified.

## Out of scope

Fog only gathers toward the Spec. Work beyond it is out of scope: it isn't fog,
and it doesn't belong in Not-yet-specified. It gets its own Out-of-scope line:
the gist plus why it's out.

**Offshoot Signals.** Separable new work found along the way splits as an
offshoot Signal (filed through signal-filing.md's Offshoot form), one
Chart each, never a second Spec on this Chart. Title it for the work it names,
with no kind prefix: the kind:signal label carries the kind.
