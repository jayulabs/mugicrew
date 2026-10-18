# ADR 0252: mugicrew a cross-repo Signal splits at triage into context-labelled member Signals

Status: Accepted
Date: 2026-10-17
Amends: ADR 0132 (initiative-born-at-triage-phased-by-rolling-member-signals) §3/§4/§6, for intra-phase members and edges only.

## Context

A cross-repo Signal in the first client dry run became one unlabelled root
Checkpoint, and its Builds wrote nested-repo code into the root. Charting
'Cross-repo work splits into one context-labelled Checkpoint per repo, and
nested-repo code can't land in the wrong repo' settled that the split happens
at triage, as an INITIATIVE whose phases hold one member per context. Grilling
settled how Luffy files and triages those members.

## Decision

1. **Repo contexts only.** The split and the never-none rule apply to declared
   `repo =` contexts. A subtree context lives in the root repo, so an
   unlabelled Checkpoint can't land its code in the wrong repo.
2. **Never none inside a repo context.** Triage never recommends no context
   for work that changes anything under a repo context's dir. This is a prompt
   rule.
3. **Work across repo contexts is an INITIATIVE.** This covers work in more
   than one repo context, or in a repo context and the root. A phase holds one
   member Signal per touched repo context, plus an unlabelled member for the
   root's share. There is at most one member per context in a phase, enforced
   by the prompt only. `blocks` edges join members where one needs the other's
   code landed. The phase's Phases line lists each member as
   `<title> [<context>]`. The cut session splits later phases the same way.
4. **Pre-bound by label.** A member is filed with `context:<name>`. A Signal
   may now carry `context:`. A missing label means *not yet decided*, never
   *Workspace-wide*: an adopted Signal, or the root member, is asked for its
   context at its own triage. No sentinel for "none" is added.
5. **Member triage keeps the label.** Luffy states the pre-binding and asks no
   context question. Only the human's explicit word changes the label, by
   relabelling the Signal before any reconcile. A member is never re-split,
   because Initiatives never nest. Other-repo work found while grilling goes to
   an offshoot member Signal or to the Initiative's Notes.
6. **No new reconcile flag.** `reconcileSignal` and the fast lane read the
   Signal's label. If `--context` is absent, the label is used. A `--context`
   that differs from the label is refused before any write. An unlabelled
   Signal takes `--context` as before.
7. **Ordering stays serial.** A member blocked by another is not triaged until
   its blocker's Checkpoint lands, which is today's carry (ADR 0085). ADR 0085
   is not amended.
8. **No-context cost is zero (ADR 0157).** All rule text is appended in Go,
   gated on declared contexts: triage's `contextSection`, and a new
   conditional slot in the cut prompt. The pre-bound paragraph renders only
   for a labelled Signal. `triage.md`, `cut.md` and `signal-filing.md` stay
   byte-identical.

## Considered Options

- **A description line instead of a label.** Rejected: only prose could read
  it. The label is read by `bd.ContextOf`, and the loop already holds a
  misspelled name under `paused:context`.
- **Carrying member edges down to Checkpoints** (ADR 0085 incoming carry, or
  re-pointing at slicing). Deferred: cross-repo order usually means the code
  must land first, so the serial wait costs little. Revisit if dry run 2 shows
  otherwise.
- **Go enforcement of one member per context.** Rejected: nothing parses
  Phases, so Go can't tell a same-phase sibling from a later-phase one.

## Consequences

Placing a member in a later phase would leave an open child that holds the
current phase's drain, and the next phase would never be cut. This Spec
places nothing in a later phase. The offshoot 'A member Signal's triage places
it in its Initiative's Phases, weighs merging with open siblings, and can
release it' carries that constraint.
