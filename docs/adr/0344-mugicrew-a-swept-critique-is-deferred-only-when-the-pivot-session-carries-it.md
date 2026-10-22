# ADR 0344: A swept Critique is Deferred only when the pivot session carries it

Status: Accepted
Date: 2026-10-21

Chart mc-uigy1 (Every close of a Merge-Request-filed bead gets a close-out
reply, including closes after the last close-out pass). Settles grilling
mc-uigy1.4. Amends ADR 0339 (a-close-out-reply-reads-its-verdict…) §5.
When a pivot is confirmed or escalated, the Zoro sweep's `closeOut` closes an
open filed Critique with `obsolete: Spec … (pivot … confirmed|escalated)`.
Charting settled the rule: the reply is Deferred only if the ask really reaches
the corrected Spec or the escalated Chart, else the neutral ☑️ Closed. The
research (docs/research/pivot-sweep-fate-of-checkpoint-mr-landing-watch-and-filed-critique.md)
found that nothing reads or records that today. The ask reaches the successor
only if someone happens to type it into `--spec` or `--evidence`.

## Decision

1. **The pivot session decides, and Go records it.** `zoro/prompts/pivot.md`
   reads the swept Checkpoint's open filed Critiques (children carrying
   `Source:` lines). For each one, the session recommends fold or leave out,
   and the human decides. A folded FR is written into `--spec` (confirm) or
   `--evidence` (escalate), and its id goes into a new `--carries <id,…>` flag
   on `mugicrew pivot confirm|escalate`.
2. **The mark is an edge, written before `closeOut`.** Go refuses a
   `--carries` id that is not an open filed child of the swept Checkpoint. For
   each accepted id it writes successor `discovered-from` Critique
   idempotently, so a re-run adds no duplicate. The successor is the corrected
   Spec on confirm and the fresh Chart on escalate. The order is: successor →
   `--carries` edges → `carryCheckpoint` → `closeOut`. A Watch pass inside
   `closeOut`'s race therefore reads the same graph as a late pass, and posts
   the same reply. The marker (ADR 0200 §4) makes it post once.
3. **Recognition parses the Go-written shape.** A swept close keeps its
   `obsolete:` prefix, because `retireStragglers`, the backstop and
   `DrainBlockersOver` key off it. One exported parser in `bd`, next to the
   writers `obsoleteReason` and `escalateReason`, matches
   `obsolete: Spec <old> superseded by <new> (pivot <p> confirmed)` and
   `obsolete: Spec <old> escalated to Chart <c> (pivot <p> escalated)`, and
   tells confirm from escalate. A round-trip test pins writer to reader. Any
   other `obsolete:` reason (`container landed`, a retired Draft) is outside
   this ADR.
4. **The edge picks the verdict; the shape picks the sentence.**
   - Edge present → `⏭️ Deferred · **<title>** (…) · tracked by **<successor title>**`.
     The carrier is the successor, not the pivot, and it is named even while
     it is still a Draft. Rationale:
     - confirm: "The Spec was revised and this Merge Request was replaced; this ask moves to the revised Spec."
     - escalate: "The Spec was sent back to charting and this Merge Request was replaced; this ask moves to the new Chart."
   - No edge → ADR 0339's neutral `☑️ Closed · **<title>** (…)`. It names no
     successor, because one that does not carry the ask would send the
     reviewer looking for it there. Rationale:
     - confirm: "The Spec was revised and this Merge Request was replaced; this ask was not carried into the revised Spec."
     - escalate: "The Spec was sent back to charting and this Merge Request was replaced; this ask was not carried forward."
   - Neither reply shows `in <sha8>`. The raw reason is never shown to the
     reviewer.
5. **ADR 0339 §5's carrier read widens.** Jinbe reads the Signals, Specs and
   Charts `discovered-from` the bead, at any status, in place of Signals
   only. So a `deferred:` close and a carried swept Critique name their
   carrier the same way.

## Considered Options

- **Always ☑️ Closed for a swept Critique.** Rejected. It is cheap, but it
  makes the charted "Deferred if it really reaches" rule never apply, and it
  tells a reviewer their ask was dropped when the human folded it.
- **The session writes the mark itself.** Rejected. The mark could land after
  `closeOut`, so a race pass would post Closed, and the marker makes that first
  reply final.
- **A `related` edge, or matching the Critique id in the successor's body.**
  Rejected. `related` would need a second carrier reader with a new meaning.
  Matching the body is fragile. `discovered-from` reuses ADR 0339 §5's reader,
  and the retrospective spine follows `discovered-from` only from a Pivot.
- **A dedicated swept reason.** Rejected. It must still start with `obsolete:`
  for the stragglers cascade, so the reader would parse it anyway, and the
  writer would change for nothing.
- **Naming the pivot as the carrier** (as `pivoted:` does). Rejected. The pivot
  is closed by the time a late reply posts, and the ask now lives in the
  successor.
