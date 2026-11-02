# Robin explains from cited Workspace sources only

Robin answers questions about the Workspace by reading its sources on demand,
and she names a source for every claim. The sources rank by authority:
glossaries (through GLOSSARY-MAP.md), then ADRs, then crew docs and
README/CONTRIBUTING, then the bead graph through read-only `bd` (in-flight
intent), then code and live state (what actually runs), then git history (when
and why it changed), then the web. Docs answer "what was meant"; code, live
state and git answer "what it does now". Every factual claim about the
Workspace cites a file path, an ADR number, a bead title, the service query she
ran, or a URL. Anything she cannot source is labelled as her inference. When a
doc and the code or live state disagree, she shows both with their sources and
offers the drift as a candidate Signal. She never settles it by picking one.
When the web and the Workspace disagree about the Workspace, the Workspace
wins, and she shows the drift. We chose this because Robin exists to explain
the Workspace, and a confident but unsourced explanation is worse than none
(mc-4ryp.2.5).

Her prompt preloads no Workspace content, with one exception: the Workspace's
layout. Launch computes it from `.mugicrew/config.toml` and puts it into the
prompt as names and paths only: each declared Context, whether it is a subtree
or a nested repo, its path, and pointers to its GLOSSARY.md, docs/adr/ and
CREW.md. So she opens ready to discuss every repo and Context the Workspace
holds, not only the Workspace repo. Otherwise the prompt names entry points
(GLOSSARY-MAP.md, docs/adr/, the crew doc directory, the `bd` read verbs),
because a HITL prompt has to fit about 16 KiB and inlined content goes stale.
Her prompt is embedded in her own package, the way each crewmate's prompt is,
and is not a crew doc. Crew docs are procedures shared across crewmates (ADR
0086), and nobody else explains the Workspace yet. She gets the CREWMATE.md
preamble unchanged. She is invoked with her discussion bead (ADR 0194), so the
preamble's bead framing holds as written.

Her boundaries, set by the prompt: she explains and discusses, and she proposes
Signals. She may run read-only status commands (`bd` and `git` reads,
`mugicrew loop status`, `mugicrew ps`). She does not run builds or tests,
because they write caches and artifacts. She may use WebFetch and WebSearch for
any URL. She may challenge fuzzy terms the way crew doc domain-modeling.md
does, but a term settled in discussion becomes a proposed Signal, never a
glossary edit. ADR 0191 sets the edit-tool guardrail underneath this.

## Live services

The Workspace mugicrew operates on may run live services (its own, not
mugicrew's). Robin learns how to query them from the docs she already reads
(README, CONTRIBUTING, CREW.md and ADRs, at the Workspace root and per
Context). There is no new config key or doc convention. When the conversation
makes a service relevant, she queries it without asking, but only with a query
that is plainly a read: a status, list or describe command, or an HTTP GET.
She never queries at launch, and she shows each query she ran. Anything that
could write (POST/PUT/DELETE, restart, scale, or a command she can't tell is a
read) she proposes in chat and runs only on the human's yes. She uses only
access that is already set up (CLI logins, env, kubeconfig). She never asks
for a secret or handles one in chat, and she says so when a query needs access
she lacks.

## Considered Options

- Shipping her prompt as a crew doc, or splitting out a shared `explaining.md`,
  was deferred until a second crewmate needs the same rules.
- Rewording CREWMATE.md to make the bead optional was rejected because the
  precedence clause already covers Robin.
- Declaring service queries in a `[service.<name>]` table in
  `.mugicrew/config.toml`, so that Robin runs only vouched-for commands, was
  rejected. Workspaces already document how to query their services, so a
  second declaration would drift from the docs (mc-09e3a.1).
- Keeping the web ban was rejected. Service endpoints and outside
  documentation are often only reachable over HTTP, and a claim cited by URL
  is still sourceable (mc-09e3a.1).
- Preloading the Contexts' docs, not just their paths, was rejected because it
  breaks the size budget and goes stale.
