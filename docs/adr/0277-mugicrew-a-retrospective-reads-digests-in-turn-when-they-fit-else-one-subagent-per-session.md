# A retrospective reads its digests in turn when they fit, else through one subagent per session

Robin reads a retrospective's sessions one of two ways, chosen by size. If the
set's rendered digests come to at most about 40 % of her context window
(estimated as digest bytes ÷ 4), she reads every digest in turn. Otherwise she
spawns one subagent per session, and each returns a cited summary with the
session id8 and quoted lines. Either way, she quotes lines verbatim and cites
each as `<bead title> session <id8>`.

This was settled by prototyping (mc-4ryp.6.4, side ref
refs/mugicrew/prototypes/mc-4ryp.6). The three mechanisms were measured on the
spine "Launching a cut session claims its Initiative, and a claim held elsewhere
refuses in one line": 26 sessions and 836,555 B of digests. Each ran on sonnet
with its 1 M window, inside Robin's read-only argv. All three ran at the same
time.

| Mechanism | Patterns | Verbatim citations | Peak context | Wall | Cost |
|---|---|---|---|---|---|
| Read in turn | 8 | 28/28 | 192 k | 95 s | $0.92 |
| Grep first | 3 | 9/9 | 49 k | 42 s | $0.21 |
| Subagent per session | 6 | 22/22 | 102 k leader, 26–46 k each | 136 s | $2.74 |

Reading in turn found the most patterns, at a third of the subagent cost. It
stops fitting as sets grow: measured spines and the Robin Initiative so far
reach about 0.5 M tokens, and a 200 k window holds none of them. Subagents keep
the leader's context bounded and found two patterns that reading in turn
missed.

A subagent is allowed under ADR 0191 (robin-read-only-is-a-disallowed-tools…)
because it inherits the guard. A subagent of a session launched with
`--disallowedTools Edit,Write,NotebookEdit` has no Write or Edit tool, and its
Write call fails. The same request without the flag succeeds.

## Considered Options

- Grep-first over the digests for errors, refusals and retries was rejected as
  the default. It was cheapest, but it found only 3 patterns, all of which
  reading in turn also found. It suits a narrowing filter the human asks for
  inside the conversation, such as failed-only, not a full retrospective.
- Always using subagents was rejected. It costs about 3× as much and found
  fewer patterns on a set that fits.

## Consequences

- The 40 % share is a heuristic from one measured run. It is a single constant,
  easy to retune.
- Robin needs the set's digest size before she picks a mechanism, so her prompt
  must tell her to measure it first, for example with `wc -c` over the verb's
  output.
