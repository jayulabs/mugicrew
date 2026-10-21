## Retrospective framing

This discussion is a retrospective: the human's yes on the dialog was the
request. Read `Target:` and `Spine:` from your own bead with bd show; the
Spine: line lists every bead in the finished piece of work. This framing
overrides exactly one rule below: at Done a retrospective keeps its
"Retrospective <title>" title and writes notes only (bd update with --notes, no
--title), then closes as usual. Everything else in the discussion prompt that
follows applies unchanged, read-only included.

Work in this order:

1. **Start at once.** Do not ask what to do; begin gathering.
2. **Gather and measure.** For each Spine: bead run
   `mugicrew transcript --no-follow --no-pager <bead> | wc -c` (never save
   output to a file; you are read-only). Total the bytes and estimate tokens
   as bytes / 4. When that is at most about 40 % of your context window (the
   one constant here), read every digest in turn, rendering each again as you
   read it. Otherwise spawn one subagent per session; each renders its own
   session and returns friction points with the session id8 and verbatim
   quoted lines. Quotes are always verbatim. Cite every occurrence as
   "<bead title> session <id8>".
3. **Coverage.** Read the oldest event's time from the first line of
   `$(git rev-parse --git-common-dir)/mugicrew/events.jsonl.1`, else the live
   `events.jsonl` (the horizon is read from the rotated session log, then the current one). Take each bead's closed_at from bd. A bead is lost when it is
   of a kind that runs sessions (anything but a Checkpoint or Landing), has no
   findable session, and closed before the log's oldest event. A session is
   pruned when its transcript renders header only. Print the header
   "N sessions read, M with a transcript", then a coverage line such as
   "Coverage: 22 of 29 beads have sessions; 5 closed before the event log's
   start (10-16 17:59) and are lost; 3 sessions are pruned (header only)".
   When anything is lost, say the patterns are partial.
4. **Recurring.** A pattern is recurring at 2 or more distinct occurrences. A
   session that only retells another session's event is not an occurrence and
   is not cited. The count is sessions with an occurrence.
5. **Match each pattern against the graph** with bd search, at least two
   phrasings, all kinds, `-n 0`, reading the plain-text output.
   - Unclosed pass first: `bd search "<phrase>" -n 0 -s open,in_progress,blocked,deferred`.
     A same-pattern hit goes under "Already on the graph:" by title, not
     proposed.
   - Then the all-status pass: `bd search "<phrase>" -n 0`; judge each closed
     hit by its title line.
   - For a matching closed hit, walk back through caused-by (else parent-child,
     skipping Initiatives) to its Signal, then forward (`bd dep list <id>
     --direction=up -t caused-by`, `bd list --parent <id> -s
     open,in_progress,blocked,deferred`). With nothing unclosed, the item is a
     "regression of <title>".
   - A Signal rejected with no successor makes the item "previously rejected:
     <title>", the close reason quoted on its own line, cut short with ...
     when long.
   - A duplicate is followed to its original (the supersedes edge, else an id
     in its close reason, else the same search's hits) and classified by these
     rules; fall back to "previously closed as duplicate: <title>".
   - Any other successor-less close is judged from its reason text: a bead the
     reason names as carrying the fix is walked as the successor; a bare
     "obsolete" or "Already built" counts as rejected.
   - Use `bd list --all --desc-contains` only as a fallback, with a
     distinctive phrase.
6. **Present.** Merge clear duplicates first; mark near-duplicates "overlaps
   M". Give one flat numbered list, most sessions first, no cap. Each title
   line carries a [harness], [process] or [code] tag, "(N sessions)", and
   overlaps / regression of / previously rejected where they apply. Each item
   is one sentence, then up to 3 citations
   `<bead title> session <id8>: "<verbatim quote>"`, then "+N more". After the
   list, unnumbered tails: "Seen once:" (one citation each) and "Already on the
   graph:" (the matching bead's title).
7. **Answer and draft.** Take the human's free-form answer ("yes 1, 3", "all
   but 4", "merge 2 5"). Read back the Signals you will draft before drafting
   any. Each yes drafts one Signal (a merge drafts one) through the Signals rule
   below. Its Sources line cites every session with an occurrence, plus the
   regression or rejected bead's title, plus bd search where it was used. Its
   body quotes the 2 strongest lines, and for a rejected match the rejected
   Signal's title and full close reason. Number a tail item only when the human
   asks for it to be filed.
8. **Then carry on as a discussion**: filters (crewmate, time, failed-only,
   grep-first as a narrowing step), "+N more", filing a tail item.

The discussion prompt follows.
