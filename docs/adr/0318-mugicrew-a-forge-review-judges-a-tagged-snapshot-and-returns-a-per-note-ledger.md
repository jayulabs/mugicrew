# ADR 0318: A Forge Review judges a tagged snapshot and returns a per-note ledger

Status: Accepted
Date: 2026-10-20

Chart mc-xw289 (Actionable Merge Request comments are filed deduplicated and
acked). Settles prototype mc-xw289.5. Prototype runs were measured against real
Merge Requests (!3124, !3116, !2859) and a hand-filed set. One pass with this
shape filed everything the human would have filed, with no false files and no
wrong merges. That held on 13 of 14 runs; the 14th was a scoring artifact and
correct on hand-check. Side ref: `refs/mugicrew/prototypes/mc-xw289`.

## Decision

1. **The input snapshot.** Jinbe builds it after his mechanical pre-filter.
   It holds:
   - the MR title and author;
   - every bead already filed from this MR, each with its status (open, or
     closed with its close reason) and its source note ids;
   - the unacked notes, grouped by discussion.

   Each note is wrapped in `<discussion id kind>` / `<note id by at status>`
   tags, never a markdown heading, because bot bodies carry their own `#`
   headings. `kind` is plain comment, thread, or diff thread on path:line.
   `by` tags the author as bot and/or MR author. A thread's earlier, already
   read notes come along as `status="context, read earlier"`, so a reply can be
   read in context but is never judged twice.
2. **The output is a ledger.** Every unacked note gets one verdict: file (with
   the keys of the beads it feeds), fold (with the filed bead's id), or skip
   (with a one-line why). Filed beads and folds follow. The ledger's count is
   the N in `nothing actionable: read N notes through <id>`, and it records why
   each note was skipped.
3. **A bot repeat of a closed bead folds onto it.** It is recorded as a source
   and nothing is filed. A human repeat of a closed bead still files fresh. Bot
   reviewers re-post a declined suggestion on every run: !2859 asked for the
   same ADR status flip six times after the human declined it. The close-out
   reply relays the close reason to the bot's note.
4. **A later "done" note never stops a filing.** The ask is filed as asked, and
   the note saying it was done is quoted in the body, so the fixer can close
   it as already fixed. This is the prompt form of ADR 0317 §6. Without it, the
   judge dropped or reshaped an ask the reviewer had fixed themselves.
5. **Bot summaries are skipped, but a concrete defect inside one is filed
   alone.** A bot's summary note that only points at its own diff thread is
   that thread's duplicate: one bead, both sources.

## Considered Options

- **Beads-only output (no per-note verdicts).** Equally accurate in the
  prototype and a little cheaper. Rejected: it leaves no record of why a note
  was skipped.
- **A bot repeat of a closed bead files fresh** (the Chart's first rule).
  Rejected: it re-files once per bot run.
- **A flat chronological note list.** Not built. Thread context is what lets
  the judge tell a reply ("Fixed in X", "Not applied, on purpose") from an ask.

## Consequences

- A pass costs about $0.03 to $0.10 on Opus for 2 to 8 notes, and a first pass
  over a whole old MR costs about the same.
- The claude-review-bot rewrites its old reviews into "superseded"
  placeholders in place, so a note's body is what it says when it is read.
  That is evidence for the Chart's open question on edited notes.
