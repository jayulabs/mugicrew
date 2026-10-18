# A retrospective proposes a pattern matching a rejected Signal, with the rejection quoted

The Chart's match rule was: a pattern matching an open bead is named and not
proposed, and one matching a closed spine is proposed as a regression. A third
case was left open: the only match is a Signal closed with no successor, by
rejection or as a duplicate
(docs/research/robin-pattern-match-against-open-and-closed-beads.md §4 and Open
questions). Robin proposes it, because the rejection was made without the new
sessions she now has as evidence, and the human still says yes item by item.
This amends ADR 0278 (mugicrew-a-retrospective-presents-one-flat…) §2 and §6.
Settled by the grilling mc-4ryp.6.11.

1. **Rejected match: proposed, not tailed.** It goes in the numbered list, not
   the `Already on the graph:` tail, which stays for open beads. The title line
   is tagged `previously rejected: <title>`. The close reason follows on its own
   line, quoted, and cut short with … when it is long. It is not called a
   regression. A drafted Signal cites the rejected Signal on its Sources line and
   quotes the full reason in its body, so Luffy re-triages knowing the earlier
   answer. A reason that invites a re-raise ("file a new Signal if it shows up
   again") is the strongest case for proposing.
2. **Duplicate match: find the original.** Robin follows the duplicate's
   `supersedes` edge, else an id named in its close reason ("duplicate of
   <id>"), else the original among the same search's hits. She then classifies
   the original by the usual rule: open, regression, or rejected. Only when no
   original turns up is the item tagged `previously closed as duplicate:
   <title>`, handled like a rejected match.
3. **Other successor-less closes are judged from the reason.** Close reasons are
   free text, so Robin reads the reason. If it names a bead that carried the work
   ("Fixed directly by Build …", "resolved as Spec …"), she walks from that bead as
   the successor, and a match through it is a regression. A bare "obsolete" or
   "Already built" with no named bead is handled as a rejected match.
4. **`bd search` is a named Source.** Both discussion.md's Sources line and the
   retrospective framing name `bd search` beside bd show, bd list and bd dep list.
   The framing adds the match rule: at least two phrasings per pattern, with
   `-n 0`.

## Considered Options

- **List it in the `Already on the graph:` tail.** Rejected: the tail is for
  live work, and a rejected idea with new evidence deserves a numbered yes or no.
- **Drop it.** Rejected: it hides recurrence evidence the human never weighed.
- **Tag it `regression of`.** Rejected: nothing was built that regressed.
- **A keyword list to classify close reasons.** Rejected: the reasons are free
  text, and a named successor in the text would be missed.
