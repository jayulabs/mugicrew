# A retrospective is a framing section before the discussion prompt, chosen by a Target line

A retrospective runs on Robin's discussion prompt with a framing section,
`robin/prompts/retrospective.md`, placed in front of it, as Nami places
`grilling-thread.md` in front of `grilling.md`. The Sources, read-only, Signal
and Draft rules stay in `discussion.md` only, and a plain discussion carries no
retrospective text. Settled by the grilling mc-4ryp.6.8.

1. **Inline, in Robin's package.** The framing section inlines the whole
   procedure: measure the set's digests first (`wc -c` over
   `mugicrew transcript --no-follow --no-pager <bead>`) and pick in-turn or one
   subagent per session (ADR 0277 (mugicrew-a-retrospective-reads-digests-in-turn…)),
   match each pattern against open and closed beads
   (docs/research/robin-pattern-match-against-open-and-closed-beads.md), and
   present the flat ranked list with tails and read-back (ADR 0278
   (mugicrew-a-retrospective-presents-one-flat…)). It is not a crew doc, for the
   reason ADR 0193 (robin-explains-from-cited-workspace…) gives: no other
   crewmate shares it. Today's discussion prompt is about 5.6 KB, so the framing
   fits the 16 KiB HITL ceiling (ADR 0086 (crew-docs-delivery)) that
   `robin_test.go` already asserts.
2. **Selected by body key lines.** `Launch` sees only the bead's id and kind.
   At `r`, the TUI writes `Target: <id>` and `Spine: <ids>` into the discussion
   bead's body. A `Target:` line selects the retrospective prompt. A `Target:`
   with a missing or empty `Spine:` is a loud `Launch` error, never a fallback.
3. **Spine lists every member.** `Spine:` is the Go spine walk's output, every
   bead on it, including Landings and Checkpoint containers that never run a
   session. Robin's prompt carries no graph-walking rules.
4. **She starts at once.** The `r` dialog's yes was the request, so Robin
   measures, reads, matches and presents the list without waiting. Once the
   list is answered she carries on as an ordinary discussion: filters,
   `+N more`, filing a tail item.
5. **Done keeps the title.** A retrospective keeps
   `Retrospective <target title>` and writes only its notes. The framing
   overrides that line of the discussion Done step (ADR 0194 §3).

## Considered Options

- **A self-contained retrospective prompt.** Rejected: it would repeat and drift
  from the shared rules.
- **A section always present in `discussion.md`.** Rejected: plain discussions
  would carry text that doesn't apply to them.
- **Choosing by the `related` edge or the title prefix.** Rejected: `related` is
  not specific to retrospectives, and the title changes at Done in a plain
  discussion.
- **Robin re-walks the spine, or a `transcript --spine` verb.** Rejected: the
  first puts graph rules in the prompt, and the second changes the transcript
  verb, which is out of scope for the Chart.
