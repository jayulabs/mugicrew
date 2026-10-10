<!-- hand-kept · summarises ADR 0009 (a Chart's artifacts land on its branch), ADR 0017 (the prototype session), ADR 0102 (effort branches integrate onto it), ADR 0088 (this doc's split) -->

# A defog session

The shared skeleton of a HITL defog session (grilling, prototype): you resolve
one defog bead, a single patch of fog on a Chart, with a human. Answering it
moves the Chart one step closer to its Spec. Your prompt names your bead, the
work in the middle, and the close reason. Crew doc chart-body.md says what a
Chart, its body sections and its defog beads are.

## 1. Claim

Claim your bead first, before any work, so concurrent sessions skip it:

    bd update <your bead> --claim

## 2. Orient on the Chart

Your bead is a child of a Chart (kind:chart). Load the Chart (bd show on your
bead names its parent) and read its body at low resolution: the
**Destination** (the one Spec being charted toward), the **Notes** (domain,
skills, standing preferences), and the **Decisions-so-far** already
recorded. Zoom as needed: fetch the full body of any related or closed defog
bead on demand.

## 3. Do the work

The work your prompt names: the interview, the prototype.

## 4. Capture durable decisions as docs

A session produces decisions, and durable ones are captured as **docs, not just
bead notes**: an ADR for a decision worth a record (format: crew doc
ADR-FORMAT.md), a GLOSSARY.md edit for terminology or ontology the decision
settles (format: crew doc GLOSSARY-FORMAT.md). Commit these in your tree, on
your own effort branch (effort/<your bead>, cut from the chart branch
chart/<chart-id>); they reach the chart branch when you integrate (step 6). A
re-launch reuses the branch: `git log --oneline chart/<chart-id>..HEAD` shows
what a prior run already committed. A Chart's artifacts merge to mainline when
the Chart is defogged, with no review gate (charting is gated by defog and Spec
acceptance). Use judgment: not every answer earns an ADR, but a decision the
Spec or later beads will lean on does.

## 5. Tend the Chart before closing

The answer reshapes the map. Tend it BEFORE you close your bead: the close may
drain the Chart, so the fog it graduates must already exist.

Edit the Chart body one line at a time, never by rewriting its description
(bd update --description): a sibling session may be tending the same Chart, and
a whole-description write loses its edits. The verb re-reads the body and
edits one line of one section under a lock:

    mugicrew chart append <chartID> <section> "<line>"
    mugicrew chart drop <chartID> <section> "<line>"

<section> is one of Destination, Notes, Decisions-so-far, Not-yet-specified,
Out-of-scope; drop takes the line exactly as the body has it.

- **Graduate fog.** If the answer makes previously-dim fog specifiable, create
  fresh defog beads for it (each a Draft child of the Chart, as crew doc
  chart-body.md shows) and clear those patches from the Chart's
  Not-yet-specified (mugicrew chart drop).
- **Rule out-of-scope.** If the answer reveals work beyond the Spec, add a
  one-line Out-of-scope note on the Chart (mugicrew chart append). Separable
  new work splits as an offshoot Signal discovered-from your bead, never a
  second Spec on this Chart.
- **Obsolete the stale.** If the decision invalidates other defog beads, close
  or supersede them rather than leaving them on the frontier.

## 6. Record, integrate and close

Your bead resolves when its question is answered, not merely when work exists.

1. Record the answer on your bead: bd update <your bead> --notes "..."
2. Append a one-line gist and link to the Chart's **Decisions-so-far** (the
   Chart is the index; the detail lives on your bead):
   mugicrew chart append <chartID> Decisions-so-far "- <your bead>: <gist>"
3. Run `mugicrew integrate` in your tree. It squashes your effort branch onto
   the chart branch. Exit 0: integrated, or nothing ahead. Exit 2 with a rebase
   stopped in your tree: resolve the conflicts, `git add` them,
   `git rebase --continue`, and re-run it. If you judge them unresolvable, ask
   the human before `git rebase --abort`; then note why on your bead, leave it
   open and exit. Exit 2 with no rebase stopped (the Chart is held or landed),
   or exit 1: note its output on your bead, leave it open and exit.
4. Only after integrate exits 0, close your bead with the reason your prompt
   gives.

## Guardrail

Resolve only YOUR bead. You may **spawn** new defog beads above, but never
**resolve** them here: leave them on the frontier for another session or
crewmate.
