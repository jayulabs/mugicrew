# ADR 0087: each skill a crewmate prompt calls becomes a crew doc, vendored whole or distilled per skill, and prompts stop naming the Skill tool

Status: Accepted
Date: 2026-10-07

## Context

The crew-docs Chart makes every crewmate invocation self-contained. Crew docs reach a
session by path (ADR 0086). The inventory found eight
skill bodies reachable from the prompts: grilling, domain-modeling (+ ADR and
GLOSSARY formats), prototype (+ LOGIC/UI), code-review, resolving-merge-conflicts,
and franky's implement → tdd (+ tests/mocking) → codebase-design chain. Some are
used whole. Others are already half-overridden by the prompt that calls them, or
conflict with AFK work or mugicrew's landing rules. Triage Q4 said to decide
each skill on its own.

## Decision

| Skill | Becomes | Read by |
|---|---|---|
| grilling | crew doc, **vendored whole**; only the sub-agent line changes (below) | nami charting, grilling |
| domain-modeling + ADR-FORMAT + GLOSSARY-FORMAT | crew docs, **vendored whole** | nami charting, grilling; the ADR format also from zoro pivot and sanji prototype, which write ADRs with no format today |
| prototype + LOGIC + UI | **distilled** `prototype` doc(s): branch pick, rules and LOGIC/UI bodies kept, and every capture/fold-into-real-code step (three places upstream) rewritten to "land on the side ref" | sanji prototype |
| code-review | **distilled** `code-review.md`: the two axes, the Fowler smell baseline with its two binding rules, standards-source discovery. The caller's prompt supplies the fixed point, the Spec source and the report | usopp review; franky's Build/Critique self-pass |
| resolving-merge-conflicts | **inlined** into `franky/conflict.md`, with no crew doc (one AFK consumer, ~900 B) | franky conflict |
| implement → tdd → codebase-design | **distilled** `tdd.md`: the test-first loop and condensed tests/mocking guidance. Seams come from the Spec's build-sliceable decisions and the bead's acceptance criteria, not from a user. implement's four steps move inline into `franky/build.md` (Read `tdd.md`, typecheck and test as you go, full suite at the end, self-pass via `code-review.md`). codebase-design is dropped | franky Build and Critique (both run `build.md`) |

- **Docs are named for the procedure, not the bead kind.** `tdd.md` serves a
  Critique as well as a Build. Each doc stays reusable.
- **Sub-agents are optional.** Where a doc says to dispatch a sub-agent
  (grilling's fact-finding, code-review's parallel axes), it says to do so if
  the harness can, and otherwise to do the work inline, one step after another.
  A harness with no Agent tool still works.
- **Prompts stop naming the Skill tool.** Every "call the Skill tool for X" or
  `/skill` instruction is replaced by "Read <crew doc>". There is no "use the
  skill if installed" fallback: two sources for one procedure drift, and it is
  ambiguous which one wins.
- **`.agents/skills/` is untouched.** It stays for humans' interactive
  sessions.
- **Each crew doc that came from a skill is pinned to its upstream version.**
  It opens with a one-line pin comment, for example
  `<!-- from mattpocock/skills@<commit-sha> skills/engineering/grilling/SKILL.md · lock-hash <computedHash> · vendored -->`.
  The lock hash is copied from the skill's `computedHash` in the repo's
  `skills-lock.json`. That file records no commit, so the commit SHA is looked
  up at sync time (the latest upstream commit touching `skillPath`). The SHA
  says which upstream diff to read when re-syncing.
- **A drift test, with no automated sync.** A Go test in `crewdocs/` fails when
  a pin's lock hash no longer matches `skills-lock.json`. That is the signal
  that a skill update has left a crew doc behind. Re-syncing stays manual: a
  vendored doc is re-copied, a distilled one is re-read and hand-merged, and
  the pin is updated. The test reads `skills-lock.json` only from this repo, so
  the binary and foreign Workspaces never depend on it.

## Considered and rejected

- **Vendor everything whole.** prototype and code-review would carry steps that
  contradict the prompt (folding into real code, their own fixed point and
  report), and tdd would ask an AFK session to confirm seams with a user who
  isn't there.
- **One combined "implement" doc for franky.** It would tie tdd and code-review
  to one bead kind, while code-review is also read by usopp.
- **A crew doc for resolving-merge-conflicts.** One consumer and no size
  pressure, so a separate doc doesn't pay for itself.
- **Fetch upstream or re-sync automatically.** A network-dependent test or
  generator is heavier than a pin plus a failing test, and a distilled doc
  can't be regenerated anyway.
- **Keep the Skill call as an optional extra.** It gives one procedure two
  sources and breaks the "no prompt names the Skill tool" guard.

## References

ADR 0086 (crew docs delivery). GLOSSARY
**Crew docs**.
