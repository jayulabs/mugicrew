# mugicrew

The **mugicrew** context: an Application that systematically creates code
changes to the repo it runs in (aka a commit factory). It runs in any git + bd
repo; a NenOE operating environment is one host it runs on.
Named for its internal **crewmates**, which coordinate through a graph of **beads** (bd
issues) rather than by calling each other directly.

**mugicrew**:
A Checkpoint Producer (root glossary): the single Application that consumes a
Signal and drives the change through its entire lifecycle — charting,
specification, build, review, approval, and landing the branch — operating on
a Workspace: by default the OE it is implemented in, including itself. The crewmates are parts of
mugicrew, not Applications in their own right.
_Avoid_: Factory, Merger, pipeline, generator, agent runner, PR system, CI
_History_: replaces the former Factory and Merger, now one Application — the
review/merge stages are mugicrew's tail, not a separate Application (ADR 0001).

**Signal**:
Anything that could catalyze a change to the OE. Abstract by nature — concrete
forms include a human-written idea, an error log, or a system alert. mugicrew
records each one as a `signal` bead (see the Signal kind below); anything that
may result in a code change enters the bead graph this way.
_Avoid_: event, input, trigger, issue, ticket

This glossary is being built out by the wayfinding effort
"Ontology for the mugicrew bead graph". The kind / edge / owner / state
taxonomy is settled (ADR 0002); items marked _(pending)_ or with an inline
_(… still open)_ note are decided by that effort's remaining open tickets.

## Settled frame

The frame every kind definition builds on (see ADR 0001 and the wayfinding map):

- **Chain, not mutation** — a unit of work is a chain of distinct
  beads, not one bead changing kind. Crossing a stage boundary spawns a new
  bead in **succession** to its predecessor; the predecessor closes but is kept
  for lineage. A bead's kind never changes, so its owning crewmate never changes.
- **The spine** — The typical flow is Signal → Chart → Spec → Checkpoint → Build, with Review,
  Critique, and Approval hanging off the Checkpoint.
- **Two relationships, two jobs** — parent/child = membership; blocking edges =
  ordering and the frontier.

## Where mugicrew runs

**Workspace**:
The git repo whose code mugicrew changes: worktrees are cut from it, lands
move its mainline, and its history carries mugicrew's land record. Any repo
from which bd resolves a Bead Graph (ADR 0089).
_Avoid_: context, project, target repo

**Bead Graph**:
The bd issue database one loop reads and writes, as bd resolves it from the
Workspace. It may live inside the Workspace or elsewhere (a planning repo);
one loop drives exactly one Bead Graph and one Workspace (ADR 0089).
_Avoid_: tracker, beads dir, planning repo

**Source repo**:
The repo that holds mugicrew's own code, from which a from-source binary was
built. It is separate from the Workspace; when self-hosting on NenOE the two
are the same repo (ADR 0090).
_Avoid_: home repo, mugicrew repo (when the Workspace is meant)

**Watched loop**:
A loop started with `--watch` from a from-source binary. It restarts onto each
new build of the source repo's landed mainline. Every other loop runs one
build until stopped (ADR 0090).
_Avoid_: dev loop, auto-update

## Crewmates

> Note: Earlier drafts called these "subapps".

A **crewmate** is a named member of mugicrew's crew. Crewmates are parts of the
one Application, not Applications in their own right (ADR 0001). A crewmate is
invoked with a bead; success is a mutation to the bead graph plus side effects
(file edits, git ops). Ownership below is tentative,
pending the ownership tickets. The concrete runtime shape of a crewmate is
recommended (in-process library calls) but not fixed by the ontology — see
ADR 0003.

**Interface follows kind-ownership.** A crewmate owns its kind(s) _and_ the
**domain interface** for those kinds — the outward operations that expose the
kind (Luffy consolidating the approval decision packet and offering the approve
action is the exemplar). The interface lives _with_ the kind's owner, never in
another crewmate or a shared central module. The ontology fixes only this
ownership boundary; a crewmate's concrete interface — and whether it is rich
(Luffy) or as thin as the bare activation contract — is designed when that
crewmate is built, not here. This is the seam that lets a crewmate be lifted
out into its own Application (ADR 0001): its domain interface becomes that
Application's public API. Not every crewmate needs a rich interface.

Owning ≥1 kind is a **characteristic** every crewmate shares, not a strict
definition — "crewmate" stays open-textured so a future unit isn't foreclosed.
It is, however, the practical test for crew-vs-plumbing: the stateless dispatch
loop (ADR 0003) owns no kind and is **activation substrate, not a crewmate**.

**Loop states:** `off`, `on` and **draining**: stopped polling, waiting for
in-flight efforts to finish before exiting (ADR 0050). A **restart drain** is
a draining loop that will exec instead of exit; a human stop turns it into a
stop drain (ADR 0063). _Avoid:_ confusing the draining loop with a **drained**
container (open, all children closed), or calling a restart drain a fourth state.

**Restart:** a watched loop's drain followed by an exec onto the source
repo's newer mainline build, still holding the lock (ADR 0062). A human stop always beats a restart.
_Avoid:_ "reload", "hand-off" (a hand-off restart, two loops overlapping, is
ruled out).

**Stale build:** the loop's build differs from main's current mugicrew outPath.
A moved rev alone is not stale. The loop shows a stale build only once its
build of main has failed; while that build runs, the loop is about to restart,
not stale (ADR 0063).

**AFK pool:** the one Workspace-wide limit on AFK efforts in flight at once,
across all crewmates in any mix of owners; the loop and TUI run-now both honor
it. HITL sessions take no slot. Its size is `concurrency = <N>`, taken from this
machine's local override (set from the TUI), else the Workspace's checked-in
`.mugicrew/config.toml` (the shared default), else 3, and is re-read every loop
tick and run-now press (ADR 0110).
_Avoid:_ Cap, per-crewmate cap (the retired one-AFK-effort-per-crewmate gate).

**Crewmate preamble:** the short, shared text that opens every crewmate
session's prompt, ahead of its kind's own workflow. It carries the stance every
crewmate shares and tells the session where the crew docs are (ADR 0086).
_Avoid:_ system prompt, header, CREWMATE.md when you mean the concept rather
than the file.

**Crew docs:** the shared crewmate reference (procedures, templates, bead-graph
conventions, skills vendored whole or distilled) that ships with mugicrew itself and is read by a
session on demand, never fetched from the Workspace or an installed skill
(ADR 0086). _Avoid:_ skills, context docs, `.agents/docs`.

- **Nami** — chart the course and eliminate fog, clarify a Signal into a Spec; owns `grilling`,
  `research`, `chart-conflict`; steward of the `chart`
- **Sanji** — high-fidelity prototypes during charting; owns `prototype`
- **Zoro** — slice the Spec, cut the branch, lay the Checkpoint skeleton; triage
  `pivot` beads (correct the Spec, amend one FR in place, or escalate a wrong
  charting assumption to a fresh Chart for Nami); owns `spec`, `pivot`; steward of the `checkpoint`
- **Franky** — execute Builds, address Critiques, resolves Conflicts, resolve merge conflicts,
  write a human's dictated Surgery; owns `build`, `critique`, `conflict`, `surgery`
- **Usopp** — review changes on a branch, raise Critiques; owns `review`
- **Jinbe** — land the Checkpoint branch by **rebase + squash** onto the current base,
  closing the `checkpoint` (which _is_ the `merged` state); on a rebase conflict
  he aborts and raises a `conflict` bead. Handles the `checkpoint` (the land is a
  step, not a bead); owns `rollback`, undoing a landed Checkpoint with a revert
  commit. See ADR 0004, ADR 0047.
- **Luffy** — final adjudicator, approve a checkpoint; owns `approval`
- **Brook** — deal with the external world (outside the bead graph and the code); walk a
  human through a Task; owns `task`. Later also mirrors external state into the graph so
  work can gate on it. Not the out-of-band channel ADR 0003 ruled out. See ADR 0115.

## Bead kinds

A bead's **kind** is its single routing key (carried as a `kind:<value>` label;
mechanics in ADR 0002). A kind is either a **container** (parents member beads;
a bd `epic`) or a **leaf** (a bd `task`). Owner is a function of kind — see
Crewmates. **Kind-ownership invariant:** every kind is owned by exactly one
crewmate (`kind → crewmate` is total and single-valued); a crewmate may own
several kinds; no kind is ever co-owned (co-ownership would make owner-from-kind
routing ambiguous).

Each kind records a short **description** (top line), then as sub-bullets: its
**owner** (with role), whether it is a **leaf** or **container**, the
**operations** crewmates apply to it and what those produce, and free-form
**notes** for anything else that matters.

- **Signal** — an intake into the factory: the head of the spine and, with
  Rollback, one of two kinds that enter the graph from outside mugicrew (ADR
  0035 §13, ADR 0047; the retired `mugi-grill` Spec door of ADR 0012 survives
  only as history).
  - _Owner:_ Nami (routing owner); activation is **human-gated** — open Signals
    form an intake backlog, and a human explicitly invokes Nami on a chosen one.
  - _Structure:_ leaf
  - _Operations:_ Nami **triages** the chosen Signal — triage _is_ its own
    `open → closed` transition (no separate triage bead, no stored state). →
    **accept** spawns **exactly one Chart** in succession; **fast lane** (for a
    Signal already sharp, with no fog) spawns a **direct Spec** in succession
    instead, written by Nami during triage, and keeps full review (ADR 0035);
    **reject** closes with
    a reason and no successor; **duplicate** is rejected via
    `bd supersede <dup> --with <original>` (obsoletion), preserving multi-report
    lineage.
  - _Notes:_ Filed by any external actor — a human _or_ a system
    (`system-alert`); the ontology models only the intake, never the source (so
    an external monitor is in scope even though Chopper the crewmate is not). May
    also be an **offshoot** (`discovered-from`) when mid-work turns up genuinely
    new, separate work — distinct from a `pivot` (Spec wrong) or a `critique`
    (defect in current work). No fan-out: a lumpy report splits into
    offshoot-Signals, one Chart each. Signal _variety_
    (bug-report/idea/system-alert/…) shares owner, edge, and lifecycle, so it is
    **not** a routing kind; if ever recorded, an orthogonal `signal-kind:*`
    label, deferred until needed. See ADR 0006.
- **Chart** — Nami's charting **branch** (`chart/<chart-id>`) that wayfinds a
  Signal into a Spec.
  - _Owner:_ Nami (steward; also handler once drained)
  - _Structure:_ container (parents the defog beads)
  - _Operations:_ closes ("is defogged") when all defog beads resolve and is stamped as defogged → **authors and spawns**
    **exactly one Spec** in succession (a speccing session writes the Spec body
    by synthesizing the Chart + its closed defog beads — charting's terminal act;
    see the Spec entry). Lands by **rebase + squash** onto
    mainline (same mechanic as Jinbe's Checkpoint land), so its **landed base**
    is one squash commit (on the OE, a Checkpoint; never a Stock).
  - _Notes:_ Charting and its knowledge-bearing defog sessions (grilling →
    ADRs/CONTEXT, research → `docs/research/*`), plus prototype and chart-conflict
    sessions, **operate in a worktree of `chart/<id>`** (ADR 0071), so their artifacts (ADRs, CONTEXT edits, research docs) accrue
    on the branch and **merge to mainline on defog** — no review gate; charting is
    gated by defog + Spec acceptance. Prototype output does **not** land here (side
    ref; ADR 0009 §2). Separable work found mid-defog splits as an offshoot-Signal
    (ADR 0006), not a second Spec. See ADR 0008, 0009, 0010.
- **Grilling** — a defog bead resolved by interviewing a human.
  - _Owner:_ Nami
  - _Structure:_ leaf
  - _Operations:_ resolve (interview) → close, contributing to the Chart's defog.
  - _Notes:_ One of the "Fog" defog beads.
- **Research** — a defog bead resolved by autonomous agent investigation.
  - _Owner:_ Nami
  - _Structure:_ leaf
  - _Operations:_ resolve (investigate) → close, contributing to the Chart's
    defog.
  - _Notes:_ One of the "Fog" defog beads.
- **Prototype** — a defog bead resolved by a high-fidelity prototype that
  validates interface design.
  - _Owner:_ Sanji
  - _Structure:_ leaf
  - _Operations:_ resolve (build + validate) → close, contributing to the Chart's
    defog.
  - _Notes:_ Throwaway by definition: code is pushed to a side ref
    (`refs/mugicrew/prototypes/<chart-id>`), preserved for lineage but **never
    merged** — downstream builds on the validated design in the Spec, not the
    throwaway. The session commits its decisions on its own **Effort branch**
    and integrates them onto `chart/<id>`; the throwaway is built in a separate
    detached tree and never lands on either. See ADR 0009, 0071, 0102.
- **Spec** — a closed record produced by a defogged Chart: the plan, sliced into
  Builds. Its body is never edited; it may accrue **Amendments**.
  - _Owner:_ Zoro
  - _Structure:_ leaf
  - _Operations:_ spawns the Checkpoint in succession, and is sliced into many
    Builds. A **direct Spec** has no Chart above it: born by Nami's **fast
    lane** triage as the successor of a sharp Signal (ADR 0035), or,
    historically, filed headless by a retired `mugi-grill` session (ADR 0012).
  - _Notes:_ Exactly one per Chart (ADR 0008). Direct Specs are the one exception
    (ADR 0035). **Authorship vs. ownership:** the Spec _body_ is
    written by **Nami**, not Zoro — a speccing session run over the defogged
    Chart is charting's terminal act. Zoro _owns the kind_ for what comes next
    (slicing the authored Spec into Builds), so an open Spec routes to him
    already written.
- **Checkpoint** — a candidate for its Workspace's next mainline state, which it
  becomes when Jinbe lands it. When the Workspace is the OE, that state is the
  OE's next **Checkpoint** (root glossary); on a foreign Workspace it is just a
  landed change (OE ADR 0088, ADR 0089). Concretely the **build** branch (`checkpoint/<id>`), cut from its Spec's
  base: the Chart's **landed base**, or — for a direct Spec — the Spec's
  **anchor** commit (ADR 0012/0014). A **successor Checkpoint** is the one a
  Rollback produces: same Spec, caused by the Rollback, cut at the revert commit
  (ADR 0048), or at main HEAD when the Rollback fixes forward (ADR 0049). A
  **surgery Checkpoint** is the one a human's Surgery opens: no Spec and no
  predecessor, cut at main HEAD when the Surgery is filed, with the Surgery as
  its member (ADR 0091).
  - _Owner:_ Zoro (steward, creates it); Jinbe (handler, lands it; steward of a
    successor Checkpoint); Franky creates a surgery Checkpoint (ADR 0092)
  - _Structure:_ container (parents the Build/Review/Critique/Approval beads)
  - _Operations:_ lifecycle **is** the branch lifecycle
    (`cut → in-review → approved → merged`), derived from its children. Jinbe
    lands it by **rebase + squash** onto the current base, closing it (which _is_
    the `merged` state); on a rebase conflict he aborts and raises a `conflict`
    bead.
  - _Notes:_ See ADR 0004, 0010.
- **Build** — a unit of implementation work sliced from the Spec (a slice
  inside a Checkpoint, not what a Checkpoint Producer produces).
  - _Owner:_ Franky
  - _Structure:_ leaf
  - _Operations:_ Franky executes → close. May raise a `critique` (offshoot
    defect) or a `pivot` (Spec proven wrong).
  - _Notes:_ —
- **Review** — one review pass over the Checkpoint, **pinned to the commit hash
  it reviewed**.
  - _Owner:_ Usopp
  - _Structure:_ leaf (one bead per iteration)
  - _Operations:_ Usopp reviews → close. A pass that closes with zero open
    Critiques is _clean_ and spawns the Approval.
  - _Notes:_ —
- **Critique** — a finding raised against the work (offshoot of a Build or
  Review).
  - _Owner:_ Franky
  - _Structure:_ leaf
  - _Operations:_ raised as an offshoot → resolve → close.
  - _Notes:_ —
- **Approval** — the stamp that gates merge.
  - _Owner:_ Luffy
  - _Structure:_ leaf (one per Checkpoint)
  - _Operations:_ Luffy exposes the approval interface — consolidates the decision
    packet (diff, clean-review evidence, Critique history) and offers the approve
    action that closes the bead. Open = merge pending; closed = approved, the sole
    trigger for the Checkpoint's derived `approved` state (carries **no ordering
    edge** — merge is a step, not a bead).
  - _Notes:_ Spawned on demand when a Review closes clean, in **succession** to
    that Review, so it certifies that Review's exact commit; if the branch
    advances past it, it is **obsoleted and respawned** off the new clean Review.
- **Conflict** — a rebase conflict, or an ADR-number clash (ADR 0030/0069), hit
  while Jinbe lands the Checkpoint.
  - _Owner:_ Franky
  - _Structure:_ leaf (member of the Checkpoint)
  - _Operations:_ Jinbe aborts the rebase and raises it carrying the **target base
    commit hash**. Franky resolves it (rebase + resolve) → closing spawns a fresh
    Review, so the re-landed commit is re-reviewed and the stale Approval is
    obsoleted.
  - _Notes:_ Named for the problem, not the action (both Franky and Jinbe rebase).
    See ADR 0004.
- **Chart-conflict** — the Chart-land counterpart of Conflict: a collision that
  stops `reconcileChart` landing the Chart branch. One kind, two **variants**: a
  textual **rebase-conflict**, or an **adr-number-clash** (the Chart adds an ADR
  number mainline already has).
  - _Owner:_ Nami
  - _Structure:_ leaf (member of the Chart)
  - _Operations:_ Nami files it carrying the branch, the **target base commit
    hash**, the **Chart tip** at filing, and the variant. Nami resolves it AFK:
    rebases onto that base and, for a clash, renumbers the Chart's ADRs above
    mainline's max in each ADR's own `docs/adr/` and sweeps references in what the Chart owns. The agent
    commits; Nami closes it only after a **post-resolve check** confirms
    mainline's content survived; closing it → the Chart re-drains and
    re-lands, with **no re-review** (a Chart has no review gate). On a real
    judgment call it **escalates**: a grilling bead on the Chart blocks the
    chart-conflict until the human answers, then the AFK writer applies the
    answer.
  - _Notes:_ See ADR 0011, ADR 0030, ADR 0079, ADR 0080.
- **Pivot** — work on a Checkpoint proves the **Spec** wrong (the plan, not the
  code).
  - _Owner:_ Zoro (triages)
  - _Structure:_ leaf (offshoot of the Build/Review, pointing at the Spec)
  - _Operations:_ raised by the discovering crewmate (Franky/Usopp), whose own
    bead then closes normally; a live pivot **blocks the whole Checkpoint** —
    every current and later member, the Review/Approval spawn, and the land.
    Zoro triages → **correct the Spec**
    (slice error); **amend** (the FR's intent holds and no built work becomes
    wrong → an Amendment on the Spec, refiled as a Critique on the same
    Checkpoint); **escalate** (wrong charting assumption → fresh Chart for Nami,
    obsoleting the old Spec/Checkpoint via `supersedes` and spawning a new
    Checkpoint on a fresh branch); or **dismiss** it (the Spec was fine — the
    finding may become a Critique) and let Builds resume. A fast-laned Spec has
    no Chart: escalating it spawns the fresh Chart **caused by the original
    Signal** (which stays closed).
  - _Notes:_ See ADR 0005, ADR 0035, ADR 0043, ADR 0044, ADR 0045, ADR 0046. Zoro
    recommends the outcome; the human picks. One pivot = one FR: a second
    finding on the same FR merges into the open pivot. A Checkpoint's open
    pivots are triaged together; correcting or escalating sweeps the rest.
- **Rollback** — a human's request to undo one landed Checkpoint that broke
  main, and recover its work.
  - _Owner:_ Jinbe
  - _Structure:_ leaf (unparented; `related` to its target Checkpoint)
  - _Operations:_ filed directly by a human, with no triage. Jinbe adds a
    **revert commit** on main for the target's squash, never rewinding main,
    then hands the work on to a **successor Checkpoint** under the same Spec,
    caused by the Rollback and seeded with the reverted work, whose first member
    is a Build carrying the Rollback's breakage evidence. The Rollback closes at
    that handoff. A target that never landed closes the Rollback as invalid; an
    already-reverted target skips the revert. A revert that conflicts with later
    lands is skipped, and the Rollback **fixes forward**: the successor is cut
    at main HEAD with no seed, and its Build fixes the breakage in place.
  - _Notes:_ "Rollback" is the bead and the operation; "revert commit" is only
    the git mechanism. One Rollback = one Checkpoint; a landed Checkpoint is
    never reopened. Agents never file one. See ADR 0047, ADR 0048, ADR 0049.
- **Surgery** — a HITL session in which a human dictates small code changes on
  a surgery Checkpoint and Franky writes them, then hands them to review.
  - _Owner:_ Franky
  - _Structure:_ leaf (the only authoring member of its surgery Checkpoint)
  - _Operations:_ filed directly by a human, with no triage; filing has Franky
    create the surgery Checkpoint with the Surgery as its member and launch the
    first session. The human directs and Franky writes: every edit and commit
    is his, one commit per accepted change, each behind the Workspace's
    gates. Spans one or more sessions: a session **stops** by committing and
    exiting, leaving a comment on the bead that records what it did. The
    surgery **ends** only on the human's explicit word, which closes the
    Surgery; Jinbe then spawns the Review. A surgery that ends with no commits closes
    together with its Checkpoint. Its Review checks the code against the
    Surgery bead in place of a Spec, its Critiques go to Franky as usual, and
    no pivot can be raised on its Checkpoint.
  - _Notes:_ HITL only; agents never file one but may suggest one by offshoot
    Signal. "Dictation", not authorship: the human authorizes each change.
    An **idle** surgery is open and in progress, with no session running;
    `l` on its row resumes it. Never "paused": that word is the loop's
    (Paused). See ADR 0091, ADR 0092, ADR 0093, ADR 0094, ADR 0095.
- **Amendment** — a decision appended to a Spec that replaces one FR's
  mechanism while keeping its intent. It comes from an amended Pivot.
  - _Owner:_ Zoro (writes it at triage)
  - _Structure:_ not a bead; a marked comment on the Spec
  - _Operations:_ read alongside the Spec body; the latest Amendment for an FR
    overrides it.
  - _Notes:_ A Spec's effective content is its body plus its Amendments in
    order, so the Spec stays append-only. A superseding Spec folds them into
    its body. See ADR 0044, ADR 0045.
- **Task** — a HITL walk-through of steps that change the external world
  (outside the bead graph and the code), such as creating a repo or minting a
  token.
  - _Owner:_ Brook
  - _Structure:_ leaf, free-standing (never a Chart child, Checkpoint member or
    Spec child). Its body is a numbered `## Steps` list of Do/Check steps (crew
    doc `task-body.md`); its run record is comments: one value-free `passed`
    per step, `amended` for an approved rewrite, one `stuck` on a stuck exit.
    Ordered only by `blocks` edges: after a land (blocked by the Checkpoint),
    before a land (the Checkpoint blocked by it), or after another Task.
  - _Operations:_ only a human launches or resumes it (`l`/`b`); the loop never
    does. The guide runs at the repo root, re-runs every Check to find where it
    is, and closes the Task itself only after a final all-checks pass and the
    human's in-session yes. An interrupted or stuck Task stays claimed and
    **idle**; `x` unclaims it. Abandoning one is the human's own `bd close`.
  - _Notes:_ Not a defog kind: a Chart never contains one (ADR 0013). Agents
    never file one mid-work; they file an offshoot Signal. See ADR 0113, ADR
    0114, ADR 0115, ADR 0116, ADR 0117, ADR 0118.

"Fog" is the collective term for a Chart's defog beads (Grilling/Research/
Prototype); it is not itself a kind — the grouping is the Chart parent.

The **landed base** is the single commit a container branch produces when it
lands on mainline (a rebase+squash commit). It is the seam between the spine's
branches: the Chart's landed base is where the Checkpoint is cut from. It is a
derived git fact — recovered from the land commit's `Chart:`/`Checkpoint:`
trailer, never stored on a bead (ADR 0010).

A **direct Spec** (ADR 0035; historically headless, ADR 0012) has no
container-branch land, so it carries its own **Spec anchor** instead: one commit
on mainline bearing a `Spec: <spec-id>` trailer, written empty by Nami's
fast-lane triage handler to freeze the Spec's birth point. Zoro cuts `checkpoint/<id>` from whichever base applies —
the `Chart:` landed base or the `Spec:` anchor — via the same `LandedBase`
trailer grep; a missing anchor is a hard error, never a `rev-parse main`
fallback (ADR 0014).

## Bead relationships

Named domain relationships, each on one bd edge (ADR 0002):

- **Succession** — same unit of work advancing a stage (the spine). Predecessor
  closes, kept as lineage, and never before its successor is visible (published):
  a predecessor is never left closed with no successor.
- **Membership** — a container holds a member (Chart→defog beads,
  Checkpoint→Build/Review/Critique/Approval).
- **Offshoot** — new work discovered _mid-work_, distinct from succession.
- **Obsoletion** — a bead proven wrong or stale and replaced; the old bead is
  closed.
- **Ordering** — the only relationship that gates the frontier (`bd ready`).
- **Carry** — when a Signal, Chart or Spec closes in **succession**, the beads it
  _orders_ (blocks) become ordered by its successor, so a dependent waits for
  the real work, not the stage that described it. Outgoing ordering only; the
  old edge stays as lineage. A Spec's dependents therefore wait for its
  Checkpoint to **land**. A Checkpoint obsoleted by a confirmed or escalated
  **pivot** carries the same way, to the corrected Spec or the escalated Chart.
  _Avoid:_ "moving" edges (nothing is removed).

## Ownership roles

- **Routing owner** — whose queue an open **leaf** bead lands in. A function of
  kind; nothing is stored.
- **Steward** — the crewmate that _creates_ a **container**. It is not routed to
  while the container still has open children.
- **Handler** — the crewmate a **drained** container (open, all children closed)
  is routed to: `chart` → Nami, `checkpoint` → Jinbe. Owner-from-kind extended to
  container kinds. Steward (creates) and handler (drains) may differ — Zoro
  stewards the Checkpoint, Jinbe handles it. See ADR 0003.
- **Operator** — anyone who acts on a bead. Equals the owner for every leaf;
  diverges only on the Checkpoint (Zoro creates it, the tail crew operate on it
  via their own beads, Jinbe's merge closes it).

Owner is distinct from two bd-native facts it is easy to conflate:

- **Creator** — bd's `created_by`; who filed the bead. Owner is _derived from
  kind_; the creator is whoever authored it. For every kind but one the creator
  is a crewmate (a closing crewmate spawns its successor); **Signal is the only
  kind whose creator can be external to mugicrew** — a human or system filing
  intake (see ADR 0006).
- **Claimer** — bd's `assignee` + `in_progress`; the crewmate _actively working_
  the bead (the activation model's busy-signal, ADR 0003). Owner is who a bead
  routes _to_; the claimer is who has _picked it up_. An ended effort,
  failed or not, releases its claim unless the bead was closed (ADR 0034).
- **Effort liveness** — whether anything is actually running for an active AFK
  effort (`in_progress` + mode:afk): _alive_ iff a `mugicrew`/`claude` process
  carries the bead id as an exact argv token, else _dead_ (ADR 0025). Derived on
  demand, never written back; the claim age (`started_at`) is shown beside it but
  is never a liveness signal. HITL claims get no verdict.
- **Effort liveness view** — the read-model over those verdicts (`mugicrew ps`,
  and the TUI's liveness dot beside the status glyph, ADR 0032 §5): observational, not coordination state — no
  dispatch path reads it and nothing is persisted (ADR 0031/0032).
- **Event log** — mugicrew's durable, append-only **history** of what happened:
  effort lifecycle (started / ended / failed) and mugicrew ops (land, reconcile,
  claim / unclaim), each entry an **event**. History is a third kind of state,
  distinct from coordination state and the effort liveness view:
  non-authoritative (nothing dispatches or recovers from it) and never a
  liveness signal (a `started` with no end event means nothing on its own). Shown
  in the TUI's Logs tab (ADR 0033); one JSONL file at
  `$(git rev-parse --git-common-dir)/mugicrew/events.jsonl` (ADR 0027–0029).
  _Avoid:_ effort history, run log, crew log.
- **Event** — one entry in the event log.
- **Effort** (in events) — one dispatch of a claimed bead to its owner, from
  route to teardown; the outer layer of the event log, identified by a minted
  effort id that every event inside it carries (ADR 0029).
- **Session** (in events) — one claude run inside an effort, identified by its
  claude session id; an effort may contain zero or more sessions (ADR 0029). A
  HITL launch is a session outside any effort (`hitl.*` events).
  _Avoid:_ using "session" for the effort as a whole.
- **Session cost** — the estimated USD spend (and per-model tokens) of one
  session, as Claude Code itself prices it; an estimate, not billing data. A
  session whose cost could not be read has an _unknown_ cost, never zero
  (ADR 0029).
  _Avoid:_ "bill", "charge".
- **Partial cost** — a rolled-up cost (per bead, Chart or group) that sums only
  the sessions whose cost is known; it is shown marked (`$1.23+`) with a count
  of the sessions without a cost, never as zero for them (ADR 0061).
- **Agent time** — a bead's wall time as the sum of its sessions' durations
  (headless and HITL); excludes reconcile overhead, includes HITL idle time
  (ADR 0061). _Avoid:_ "effort time" for this.
- **Plan usage** — the account-wide share of the Claude subscription's rate
  limit used in its 5-hour and weekly windows, as Claude Code's `/usage` shows
  it; not Session cost, and not mugicrew's own share of it.
  _Avoid:_ bare "usage", "quota", "budget".
- **Abandoned effort** — an in-flight effort the loop gave up on during a
  force stop, or a run-now effort a confirmed TUI quit left behind: its
  bookkeeping ended without releasing its claim (`effort.abandoned`, ADR 0029).
  The work itself may still finish.
  _Avoid:_ calling it a failed or ended effort, which releases its claim (ADR 0034).
- **Aborted effort** — an in-flight AFK effort the human stopped with
  `mugicrew abort` or TUI `x`: its bead is paused _manual_ first, then its
  processes are terminated, and a live host cleans up as for a failure (ADR 0083).
  _Avoid:_ calling it abandoned, which leaves its claim and tree behind.
- **Auto-exit (HITL session)** — the visible countdown that a session's
  closing line starts. When it runs out, the session exits by itself just as a
  human `/exit` would, and ends _finished_. A reply cancels it, and so does its
  cancel key. The next closing line starts it again (ADR 0111). _Avoid:_
  "auto-finish" and "autoclose" (a synonym pair), and "timeout" (nothing
  failed).
- **Closing (HITL session)** — the human has exited the session cleanly and
  is back in the TUI, but the session's close (publishing its Drafts, then
  integrating or removing its tree) is still running. It sits between _done_ and
  _finished_, and every TUI sees it, because the session itself reports it
  (ADR 0108). _Avoid:_ calling it "pending" or "in flight", which name a TUI's
  own writes that are not finished yet (ADR 0107).
- **Detached session** — a HITL session the TUI launched that keeps running as
  a live process while no one is attached, and can outlive the TUI. At most one
  per bead. Its verdict is _alive_, _exited_ (with its exit status) or _gone_
  (no longer running and not listed, for example after a reboot; not resumable). Only a
  failed exit leaves it _exited_, holding its worktree and branch until it is
  resumed or dropped. A clean exit ends it outright and frees both (ADR 0037).
  _Avoid:_ "background session" for one started with `b` (ADR 0100): it is an
  ordinary detached session, and no TUI runs in the background.
- **Done (HITL session)** — the session's bead is closed. The session may still
  be alive. Done is the cue to `/exit`, and the session's status bar says so
  (ADR 0066). _Avoid:_ calling a session done because the agent said so, or
  because it detached.
- **Waiting (HITL session)** — the session's agent has ended its turn, or is
  asking a question or permission, and the human hasn't replied yet. Only a
  reply clears it; looking at the session does not. Done takes precedence over
  it (ADR 0099). _Avoid:_ "idle" (a quiet pane is not necessarily waiting) and
  "pending".
- **Finished (HITL session)** — the session exited cleanly: its Drafts are
  published and it is gone from the session list. The human finishes a session
  with `/exit`, and F12 only goes back to the TUI (ADR 0067). _Avoid:_ "detached"
  for a session that has ended. No human-facing label says "detached".
- **Reattach** — joining an _alive_ detached session's live process again; the
  conversation and any running tool are as they were left. The human's terminal
  moves from their TUI to the session, and F12 moves it back (ADR 0065).
- **TUI session** — one running TUI, hosted in the same multiplexer as the
  detached sessions and belonging to the terminal that launched it. It is never
  a detached session and never in the session list. It ends when the TUI quits
  or its terminal leaves the multiplexer: there is no background TUI (ADR 0065).
  _Avoid:_ "detaching" the TUI; detach applies only to detached sessions.
- **Resume** — restarting an _exited_ detached session's conversation in its kept
  worktree. The process is new; only the recorded conversation carries over.
  _Avoid:_ "resume" for reattaching to a live session.
- **Drop** — ending a detached session for good: the session is killed and its
  worktree is removed, discarding uncommitted changes. The opposite of resume.
- **Orphaned worktree** — a launched session's worktree left behind when the
  session became _gone_ (killed multiplexer server, reboot), or an abandoned
  effort's tree once the human has released its claim and lifted its pause. It
  still holds its branch. It is never resumed. The human drops it when the next
  launch on that bead finds it; the effort branch is kept (ADR 0041, ADR 0105).
- **Effort branch** — the branch a leaf bead's work happens on, `effort/<bead-id>`,
  cut from its container's branch. A container's branch is never checked out;
  work reaches it only by integrate (ADR 0102).
  _Avoid:_ "private branch", "feature branch".
- **Integrate** — moving an effort branch's work onto its container's branch
  as one squash commit, before the bead closes. Distinct from _land_, which
  moves a container onto mainline (ADR 0102).
  _Avoid:_ "land" or "merge" for this step.
- **Foreign hold** — the branch a dispatch cuts for a bead (its effort branch)
  checked out by a worktree that no in-flight effort or _alive_ detached
  session accounts for: the human's own tree, the main worktree, an orphaned
  worktree, or an _exited_ session's worktree. mugicrew surfaces it and never
  removes it (ADR 0054). A refused cut with no holder listed is not a hold: it
  is a failed effort (ADR 0056). A container branch checked out by hand is not
  a foreign hold; integrate refuses to move it (ADR 0106).
  _Avoid:_ "busy" (mugicrew's own contention, a silent retry), "stuck", "lock".
- **Paused** — a bead the loop won't dispatch until its pause is lifted. A
  pause names its cause: _foreign-hold_ (the loop paused it after repeated
  foreign-held skips, and lifts it once the hold is gone), _failing_ (the loop
  paused it after repeated failed efforts; only the human or a successful
  run-now lifts it), _abandoned_ (the loop or TUI abandoned its effort, whose
  orphaned process may still be running; only the human lifts it),
  _integrate-conflict_ (the backstop couldn't integrate its effort branch;
  only a successful integrate or the human lifts it) or _manual_
  (the human paused it, and only the human lifts it). A bead can carry several causes at once; it stays paused until every
  cause is lifted, and each lifter removes only its own (ADR 0070, ADR 0078, ADR 0082, ADR 0104). A pause never stops an effort already running (ADR 0055).
  A _manual_ pause on a Chart or epic also pauses every member beneath it
  (_inherited pause_); loop-written causes pause only their own bead (ADR 0081).
  _Avoid:_ "held" (that is a branch's foreign hold), "suppressed", "deferred".
- **Draft** — a bead still being set up by the effort or session that created
  it. The loop never sees it and it can't be claimed, but every "is it still
  open?" check counts it as open, so a Chart or Checkpoint with drafts is not
  drained. It becomes a normal bead when that effort or session **publishes**
  it, in one step together with any of its drafts' edges and the closing of its
  own bead, and only when it exits cleanly.
  _Avoid:_ "deferred" (bd's name for the state a Draft is stored in),
  "hidden", "pending", "paused" (a paused bead is fully set up).
- **Publish** — turning all of an effort's or session's Drafts into normal beads
  in one atomic step.
- **Event bead** — the closed record bd writes under a bead when one of its
  states changes with `set-state`: the reason, who, and when. It is history,
  not work. The TUI never lists it as a row and never counts it. Pauses no
  longer write one: a pause is a bare label and keeps no reason (ADR 0057
  amendment).
  _Avoid:_ calling it a child, task, or step.
- **Session list** — the TUI's view of this repo's detached sessions, re-read
  each time from the multiplexer's own session table (bead sessions only, not
  TUI sessions). It is observational and
  never written to bd or read from the event log (ADR 0036).
  _Avoid:_ session registry.

## Lifecycle

Every bead is `open → closed`; there is no stored intermediate state. A
Checkpoint's branch-state is **derived** from its children by a waterfall (first
match wins): **merged** (epic closed) → **approved** (approval bead closed) →
**in-review** (a review bead exists) → **cut** (no review bead yet). The
skeleton (Checkpoint epic + Build beads) is laid atomically, so a Checkpoint is
born already **cut**; Review/Critique/Approval beads are spawned on demand.

- **Unclosed** — a bead in any status except `closed` (open, in_progress,
  blocked, deferred, …). It is the Search tab's default status facet.
  _Avoid:_ "live" or "active" (those words mean Effort liveness), and "open"
  (that is one specific status).
