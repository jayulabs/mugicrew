# Robin explains from cited Workspace sources only

Robin answers questions about the Workspace by reading its sources on demand,
and she names a source for every claim. The sources rank by authority:
glossaries (through GLOSSARY-MAP.md), then ADRs, then crew docs and
README/CONTRIBUTING, then the bead graph through read-only `bd` (in-flight
intent), then code (what actually runs), then git history (when and why it
changed). Docs answer "what was meant"; code and git answer "what it does now".
Every factual claim about the Workspace cites a file path, an ADR number or a
bead title. Anything she cannot source is labelled as her inference. When a doc
and the code disagree, she shows both with their sources and offers the drift
as a candidate Signal. She never settles it by picking one. We chose this because
Robin exists to explain the Workspace, and a confident but unsourced
explanation is worse than none (mc-4ryp.2.5).

Her prompt preloads no Workspace content. It names the entry points
(GLOSSARY-MAP.md, docs/adr/, the crew doc directory, the `bd` read verbs)
because a HITL prompt has to fit about 16 KiB and inlined content goes stale.
Her prompt is embedded in her own package, the way each crewmate's prompt is,
and is not a crew doc. Crew docs are procedures shared across crewmates (ADR
0086), and nobody else explains the Workspace yet. She gets the CREWMATE.md
preamble unchanged. She is invoked with her discussion bead (ADR 0194), so the preamble's bead
framing holds as written.

Her boundaries, set by the prompt: she explains and discusses, and she proposes
Signals. She may run read-only status commands (`bd` and `git` reads,
`mugicrew loop status`, `mugicrew ps`). She does not run builds or tests,
because they write caches and artifacts, and she does not use the web, so every
claim stays sourceable in the Workspace. She may challenge fuzzy terms the way
crew doc domain-modeling.md does, but a term settled in discussion becomes a
proposed Signal, never a glossary edit. ADR 0191 sets the edit-tool guardrail
underneath this.

## Considered Options

- Shipping her prompt as a crew doc, or splitting out a shared `explaining.md`,
  was deferred until a second crewmate needs the same rules.
- Rewording CREWMATE.md to make the bead optional was rejected because the
  precedence clause already covers Robin.
