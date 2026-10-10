# ADR 0088: crewmate content is split across a ~2 KB preamble, four procedure crew docs, Go splices and thin per-kind prompts — no shipped glossary, no ADR citations in prompts

Status: Accepted
Date: 2026-10-07

## Context

The crew-docs Chart makes every crewmate invocation self-contained. ADR 0086 settled
delivery (an inlined CREWMATE.md preamble plus crew docs read by path), and
ADR 0087 turned each skill call into a crew doc. That leaves the shared text the
13 prompts repeat. The inventory found 23
fragments (S1–S23), copied up to 7 times. HITL prompts are limited to about
16 KiB by tmux, and charting alone is 9.8 KB. A foreign Workspace has
neither `apps/mugicrew/GLOSSARY.md` nor mugicrew's ADRs.

## Decision

1. **Preamble (CREWMATE.md, hard cap ~2 KB), true for every crewmate:**
   - identity, coordinating only through the bead graph, and the bd/git policy;
   - refer to beads by title, never by bare id (S14);
   - no kind prefix in titles (S17);
   - roll forward, don't duplicate (S18);
   - "close your bead only if your prompt says to" (S19, phrased so it holds
     for both AFK and HITL);
   - ground in the Workspace's domain vocabulary, *conditionally*: read the
     GLOSSARY-MAP.md/GLOSSARY.md/docs/adr the Workspace has; if it has none,
     infer from the code and don't invent glossary files (S23);
   - a short spine: Signal → Chart (defog) → Spec → Checkpoint → Build, and
     HITL is authorization, not authorship (S20);
   - the crew-docs directory and a one-line index of the docs.
2. **The path gets in through a named token.** CREWMATE.md stays free of fmt
   verbs and carries one `{{CREWDOCS}}` token, which
   `contract.NewInvocation` fills with `strings.ReplaceAll`. Per-kind prompts
   name a crew doc by bare filename only.
3. **Four procedure crew docs, plus one more:**
   - `chart-body.md`: the meanings of the Chart-body sections, the
     defog-create command and rules, and offshoot Signals (S2, S7, S22);
   - `defog-session.md`: the shared skeleton of a HITL defog session: claim,
     orient, capture as docs, tend the Chart, record and close, guardrail
     (S8–S13);
   - `spec-template.md` (S15);

     > **Note (Spec amendment):** the one template gains an optional `## Tasks`
     > section after Further Notes (ADR 0113 §6 named it, not its place). The
     > seven sections keep their order, so a Spec without Tasks is unchanged.

   - `checkpoint-rules.md`: the effective Spec and AMENDMENT, pivot
     raise-or-merge, and critique-create (S4–S6);
   - `adr-renumber.md` (S21), read by both conflict prompts. It is a separate
     doc rather than an addendum, because ADR-FORMAT is vendored whole and
     pinned (ADR 0087), and a re-sync would erase an addendum.

   Single-consumer text stays inline in its prompt: the vertical-slice rules
   (S16) and the research discipline.
4. **Go fragments stay Go splices:** `DraftInstruction` (it carries the creator
   id), `WayfinderMapFields` (section *names*; the meanings are in
   `chart-body.md`), `ClosingLine` and `researchDocPointer`. promote-or-stamp
   splices `chart:defogged` from `contract.LabelDefogged` instead of writing it
   as a literal.
5. **No mugicrew glossary ships.** The prompts and crew docs define the
   process terms they use where they use them. The only ontology pointer is
   the conditional domain pointer to the Workspace's own glossary.
6. **ADR citations leave the prompt text.** "ADR NNNN" rationale moves into a
   comment in the prompt file header or in the Go code. A one-line *why* stays
   inline only where the session needs it to make a judgment call. The same
   text pass fixes the stale "CONTEXT" wording in prototype.md.
7. **Non-skill crew docs are kept by hand.** They are now the single source of
   the procedure text. Each opens with a comment naming the ADRs it
   summarises, so editing an ADR means grepping for the docs that depend on
   it. There is no generator and no test.

## Considered and rejected

- **Two coarse docs (chart side and checkpoint side), or about eight one-rule
  docs.** The coarse docs make grilling and prototype Read text they don't
  need. Eight docs scatter one procedure across several Reads.
- **Ship the mugicrew GLOSSARY.md, or a curated crew glossary.** At 32 KB the
  full file is mostly terms sessions never use, and either way it becomes a
  second copy that drifts.
- **Ship the cited ADRs.** They are rationale for the humans who maintain the
  prompts, not instructions. On a foreign Workspace, an ADR number would also
  collide with that Workspace's own ADRs.
- **A Sprintf preamble, or a generated line appended after it.** The first
  breaks the no-fmt-verbs rule. The second puts the path away from the
  prose that uses it.
- **Generate or test the non-skill docs against their ADRs.** A summary can't
  be generated, and a test that a cited ADR exists catches almost no drift.

## References

ADR 0086 (crew docs delivery), ADR 0087 (skill calls become crew docs). GLOSSARY **Crewmate preamble**,
**Crew docs**.
