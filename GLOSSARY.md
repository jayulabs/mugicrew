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

- **Signal** — anything that could catalyze a change to the Workspace. Abstract
  by nature — concrete forms include a human-written idea, an error log, or a
  system alert. mugicrew records each one as a `signal` bead; anything that may
  result in a code change enters the bead graph this way. Triage can **reroute** a Signal whose code is only on one in-flight
  Checkpoint (its **host**) instead of charting it.
  _Avoid:_ event, input, trigger, issue, ticket

**Reroute**:
The sixth triage outcome. After `mugicrew triage locate` says a Signal's code is only
on one open Checkpoint's branch, Luffy folds the Signal into that Checkpoint:
`mugicrew reconcile <signal> --checkpoint <cp>` files a pivot (the Spec is wrong
for one FR) and/or Critique (the code is) there, or merges evidence into a pivot
already open on the same FR. The Signal's clean-exit publish closes it. Not open to
an Initiative member.
_Avoid_: redirect, fold-in

**Host**:
The one in-flight Checkpoint a **Reroute** targets.
_Avoid_: target Checkpoint, parent Checkpoint

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
  Critique, and Approval hanging off the Checkpoint. A retrospective reads it as an
  edge closure: from a target, follow edges either way — caused-by and parent-child
  except an edge with an Initiative end; related only from a Rollback or Pivot;
  discovered-from only from a Pivot; never supersedes, blocks or conditional-blocks.
  So offshoots and duplicates are out; Rollbacks with their successors and Renumbers
  are in. See ADR 0281 (mugicrew-a-retrospective-s-spine-follows…).
- **Two relationships, two jobs** — parent/child = membership; blocking edges =
  ordering and the frontier.

## Where mugicrew runs

**Workspace**:
The git repo whose code mugicrew changes: worktrees are cut from it, lands
move its mainline, and its history carries mugicrew's land record. Any repo
from which bd resolves a Bead Graph (ADR 0089).
_Avoid_: context (as a synonym for Workspace only; a **Context** is a subtree of one), project, target repo

**Context**:
A declared subtree or nested repo of the Workspace that a container targets. It
is named in `[context.<name>] path = "<subtree>"` or `repo = "<dir>"` of
`.mugicrew/config.toml`, and a container carries it in its `context:` label. The
label is the binding and config is the address, resolved live at each use.
_Avoid_: Workspace, project, module

**Crew brief**:
The optional, human-authored `.mugicrew/CREW.md` of a Workspace or Context, naming
the traps in its architecture. The crew reads it and changes it only through a Signal.
_Avoid_: manifest, memory, notes

**Agent Profile catalog**:
The OE's `agent-profiles.toml` and its local twin, one of the places a profile
name can be declared. mugicrew's `config.toml` layers declare profiles too, and
the merged set of all four files is "the declared names", not the catalog
(ADR 0308 (the-settings-cycle-is-every-declared-profile-name…)).
_Avoid_: catalog (for the merged four-file set)

**Repo stamp**:
The `repo:<dir>` label that a nested-repo container's first branch cut writes. It
guards against a moved `repo =`: a mismatch holds the container as
`paused:context`. It is never used as an address (ADR 0167).
_Avoid_: repo pin

**Bead Graph**:
The bd issue database one loop reads and writes, as bd resolves it from the
Workspace. It may live inside the Workspace or elsewhere (a planning repo);
one loop drives exactly one Bead Graph and one Workspace (ADR 0089).
_Avoid_: tracker, beads dir, planning repo

**Device**:
One machine a user runs mugicrew on, named by a stable **Device name** that the
user chooses, not the raw hostname.
_Avoid_: host, machine, box

**Bead actor**:
The name bd writes as "you" on a claim, `<user>@<device>`: the person and the
Device they hold the claim from (ADR 0376).
_Avoid_: whoami, claimer, bare user name

**Graph id**:
A Bead Graph's identity: its Dolt root commit hash, 32 lowercase base32
characters. Two graphs that share an `issue_prefix` still differ by it
(ADR 0187).
_Avoid_: graph name (for the id)

**Graph name**:
A Bead Graph's human-readable label: its owner's GitLab username. It labels
a graph for people and never identifies one (ADR 0200).
_Avoid_: graph id (for the label)

**Qualified Bead Id**:
A bead id qualified by its Bead Graph, written `<bead-id>@<graph-id>`. Its
display form, `<bead-id>@<graph-name>`, is for people only (ADR 0200).
_Avoid_: bead ref, global bead id

**Review thread**:
Retired. A container's Merge Request comments, threads included, are read and
filed by a **Forge Review**; the `Thread: !<iid> <discussion-id>` key is gone,
replaced by `Source:` lines (ADR 0199 §1, superseded for the key lines).
_Avoid_: using it for new work; say Forge Review

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

**Personal layer**:
The per-user config file `<os.UserConfigDir()>/mugicrew/config.toml`, outside any
repo. It is read only for machine-wide keys such as `[upgrade] unattended`;
Workspace layers never are, so such a key set there has no effect.
_Avoid_: global config, user config

**Release notice**:
An npm-installed mugicrew telling the human that a newer `@jayulabs/mugicrew`
release than the one it runs exists. It only tells unless unattended Upgrade is
opted into.
_Avoid_: auto-update, auto-upgrade, update check

**Upgrade**:
Moving an npm-installed mugicrew onto a newer release, when the human asks from
the CLI or the TUI, or by an opted-in loop unattended when safe (ADR 0411
(an-opted-in-loop-upgrades-an-npm-global-install…)). A from-source build never
upgrades; a Watched loop keeps it current.
_Avoid_: auto-update, auto-upgrade, update, self-update

**Watch pane**:
A read-only tmux split beside the TUI that shows a bead's session
transcripts, finished or live, opened by t on the bead's row. Unrelated to a
watched loop.
_Avoid_: tail pane, live view

**Forge**:
The remote hosting service for a Workspace's repo, such as GitLab. It is the
source of truth for mainline when a Workspace is remote-truth. Only the
`gitlab` land strategy talks to one (ADR 0147).
_Avoid_: host, remote service, PR system

**Merge Request**:
The Forge's reviewable proposal to merge a container's branch into mainline.
There is at most one open per container branch. It outlives its Landings: each
Landing re-pushes its pin into the same open Merge Request and re-arms it
(ADR 0147, ADR 0185). Jinbe records its GitLab web URL as an `MR link:` line
beside every `MR:` line it writes (Landing, MR Watch, Rollback).
_Avoid_: PR, pull request

**Bead verb**:
One of the twelve TUI keys that act on a bead: `z` reject, `e` edit, `a` adopt,
`A` release, `c` checkup, `B` add blocker, `U` unblock, `+`/`-` priority, `p` pause,
`x` drop, abort or recover, `X` withdraw or cancel, and `w` open Merge Request. A
bead verb is offered wherever its bead's row appears, on every tab up to
Initiatives and in the Initiative focus view, and is gated only by the bead's state,
never by the tab. One shared router routes them; a row with no bead behind it
offers none. The exempt keys are `enter`, `b`, `d`, `space` and `i`, and the shell
keys `t` and `y` (ADR 0359, ADR 0360).
_Avoid_: tab action, per-tab key

**Land strategy**:
The Workspace's choice of how a Landing puts a commit on mainline, set by the
layered config key `land_strategy` (`local` | `gitlab`, default `local`). `local`
rebases, squashes and fast-forwards local mainline; `gitlab` opens a Merge Request,
returns pending, and mirrors the MR's state into the Landing (ADR 0185, ADR
0189). An unusable value is an
error, never a silent fallback to `local` (ADR 0147).
_Avoid_: merge strategy, land mode

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
loop (ADR 0003) owns no kind and is **activation substrate, not a crewmate**. Chopper is a
crewmate by his role (ADR 0126); his one kind, the Checkup, came later (ADR 0272).

**Loop states:** `off`, `on`, **limited** and **draining**: stopped polling, waiting for
in-flight efforts to finish before exiting (ADR 0050). A **restart drain** is
a draining loop that will exec instead of exit; a human stop turns it into a
stop drain (ADR 0063). A human can cancel a stop drain back to `on` (or
`limited`) with `loop start`, which sends SIGUSR1; a stop drain converted from a
restart drain reverts to the restart drain (ADR 0236). _Avoid:_ confusing the draining loop with a **drained**
container (open, all children closed), or calling a restart drain a fifth state.

**limited** (loop state): polling, but claude dispatch held until a Plan usage
reset + 1m, then resumed on its own (`loop.resumed {cause: limit}`; a cancelled
stop drain logs `loop.resumed {cause: cancel}`). Housekeeping
keeps running; in-flight efforts are left to finish; the hold lives only in loop
memory. _Avoid:_ "paused", which is a bead label.

**limited effort:** an AFK effort that ended on a Plan usage limit hit. Its
Drafts are deleted, its claim released and its effort branch kept, as a failed
effort's are, but it records `effort.limited {resets_at, source}` instead of
`effort.failed`, never counts toward `paused:failing` and writes no pause label:
a limit hit blames the account's window, not the bead. _Avoid:_ "failed".

**Restart:** a from-source loop's drain followed by an exec onto the source
repo's newer mainline build, still holding the lock (ADR 0062). Automatic on a
watched loop; on demand (`loop restart`, ADR 0289) on any from-source loop. A
human stop always beats a restart.
_Avoid:_ "reload", "hand-off" (a hand-off restart, two loops overlapping, is
ruled out).
_History_: only a watched loop restarted before ADR 0289; `loop restart` made
it on demand for any from-source loop (ADR 0090 §4, ADR 0062 §3, §7 amended).

**Stale build:** the loop's build differs from main's current mugicrew outPath.
A moved rev alone is not stale. The loop shows a stale build only once its
build of main has failed; while that build runs, the loop is about to restart,
not stale (ADR 0063). The failed build may be on-demand or automatic; an
unwatched loop is stale only after an asked-for build failed, never for main
moving.
_History_: stale showed only on a watched loop before ADR 0288.

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

**Unmapped Chart** _(retired)_: the derived state of ADR 0129, an open Chart
that had never had a child, waiting for the human to start charting. Retired by
ADR 0140: every Chart is born with a **Survey**, and its open Survey is what
waits. _Avoid:_ the term, except as history.

**Hand-back:** Nami's Survey session returning a wrong Destination to Luffy:
the Chart closes unspecced and a fresh Signal, discovered from it, carries the
case back to intake. A Chart with nothing to defog is finished in place instead, never
handed back (ADR 0357). _Avoid:_ re-triage, reopening the Signal.

**Crew docs:** the shared crewmate reference (procedures, templates, bead-graph
conventions, skills vendored whole or distilled) that ships with mugicrew itself and is read by a
session on demand, never fetched from the Workspace or an installed skill
(ADR 0086). _Avoid:_ skills, context docs, `.agents/docs`.

**Actionable:** the section that opens the Ideate, Defog and Execute tabs: the
tab's HITL sessions waiting on you, then its takeable frontier (the items you could
start from that tab now; on Execute that includes every open, unblocked Errand, wherever
it sits), then its paused band: leaves carrying their own paused:* label that are
otherwise ready (not blocked, Live, waiting, launching or exited), HITL and AFK alike
(on Execute, every open Checkpoint member; AFK beads join only this band); on every tab it lists every
own-paused, otherwise-ready bead whose home tab is that tab (ADR 0183), whatever container it sits under; each tab's
own container kind joins it when own-paused, open and unblocked: Ideate's P0–P2 Initiatives
(as ADR 0180's combined row; a P3 or P4 one is Actionable only while its cut session waits, ADR 0314), Defog's Charts and Execute's Checkpoints (as their
section rows); beads paused only by an ancestor stay out. On Defog, a listed stamped Chart's
review grillings join the queue like any listed Chart's leaves. A live session the agent is
working is not Actionable (ADR 0179). _Avoid:_ ready, queue, todo.

**Live:** the section directly below Actionable on the Ideate, Defog and Execute
tabs: every Live effort among the tab's tree beads not in Actionable (on the Defog
tab, those are every open Chart, stamped chart:defogged or not, plus the Chart's open
children, its Landing included; an alive AFK effort or an alive non-waiting detached session), plus any bead being launched from
the tab, in bead order; the whole header is bold in the live green (ADR 0179). _Avoid:_ running, active.

**Specs:** the section between Live and Checkpoints on the Execute tab: every open
published Spec, headed `Specs (N)`; status only, unfolded by default.

**Merge Requests:** the section right below Live on the Execute and Defog tabs,
under the gitlab land strategy only (hidden under local; an unusable strategy
shows its resolve error), headed `Merge Requests (N)`: one row per open MR Watch
whose container is the tab's own kind (Checkpoint on Execute, Chart on Defog), so
no MR is on both tabs. `w` opens the MR and `d` shows its link (ADR 0333). Unfolded
by default. _Avoid:_ MRs tab.

**All green:** a Merge Request with nothing left for the crew or the Forge to
clear: its pipeline passed on its current head, every resolvable thread is
resolved, and it has no conflict, so only a human's approve, rebase or merge
remains. _Avoid:_ mergeable, ready to merge, awaiting merge (GitLab's own
first-blocker status).

**Stabilizing:** a Merge Request the crew still owes work that will change it:
a Forge Review, a fix, its Review or Approval, a re-Land, or a close-out of a
comment. It outranks all green. _Avoid:_ working, active, running.

**Initiatives:** the section below Live on the Ideate and Defog tabs, and below
Checkpoints on the Execute tab: the
tab's Initiative groups only, headed `Initiatives (N)` for N groups (on Defog, a stamped
chart:defogged Chart is listed while it is open or in_progress with an open child); on Ideate a P3 or
P4 Initiative's whole group sits in Backlog instead while ADR 0314 puts it there (ADR 0315). _Avoid:_ tree, all.

**No Initiative:** the top-level section below Initiatives on the Ideate, Defog and
Execute tabs: the Checkpoints and Specs with no Initiative parent, and the
outside Errands and Pivots (on Defog, stamped Charts with an open child
included), headed `No Initiative (N)`. Always shown, folded by default.
_Avoid:_ loose group.

**Backlog:** the last top-level section of the Ideate tab: the listed P3 and P4
Initiatives with no cut session alive or launching and no member in triage, each with
its whole group (members at any priority), then the other P3 and P4 Signals not in
triage, loose or under a P0–P2 Initiative, headed `Backlog (N)`, N counting each bead
once (ADR 0314, ADR 0315). Nothing in it is Actionable. Always shown, folded by default.
Defog and Execute have none.

**Recents:** the TUI's list of the last beads the human acted on in the TUI,
most recent first (ADR 0183, ADR 0184). _Avoid:_ drawer (the per-tab detail,
ADR 0171), history (the History tab, ADR 0233), favourites.

**Goto menu:** the TUI's shell-wide `g` modal in Recents' picker chrome: tab
options (top, bottom), the selected bead's moves (Initiative focus, reveal in
Graph, scoped Logs, parent, cause) and its one-hop related beads, each fired by
its key (ADR 0228). It replaces the standalone `i`, `G` and `L` keys. _Avoid:_
command palette, jump menu.

**Graph scope:** the Graph query's group-head facet, set by a Goto menu bead move
(ADR 0334): Graph shows that group's whole subtree, closed beads included, and esc
clears it last. The **group head** is the outermost container on the bead's parent chain
(the Initiative when it nests under one, else its Chart or Checkpoint); a bead with no
container ancestor **self-scopes** to itself alone. A scope is a visit, never saved.
_Avoid:_ reveal (for the Graph scope; reveal is the bead-and-ancestors filter ADR 0228 §5
keeps for other senders).

- **Nami** — chart the course and eliminate fog: map a Chart toward its Spec in its Survey's human-started charting session (ADR 0129,
  ADR 0140); owns `survey`, `grilling`, `research`, `chart-conflict`; steward of the `chart`
- **Sanji** — high-fidelity prototypes during charting; owns `prototype`
- **Zoro** — slice the Spec, cut the branch, lay the Checkpoint skeleton; triage
  `pivot` beads (correct the Spec, amend one FR in place, or escalate a wrong
  charting assumption to a fresh Chart for Nami); owns `spec`, `pivot`; steward of the `checkpoint`
- **Franky** — execute Builds, address Critiques, resolves Conflicts, resolve merge conflicts,
  write a human's dictated Surgery; owns `build`, `critique`, `conflict`, `surgery`
- **Usopp** — review changes on a branch, raise Critiques; owns `review`
- **Jinbe** — land branches on mainline: spawns a drained, approved Checkpoint's
  `landing`, then runs every Landing (Checkpoint and Chart) through the land
  strategy; on a rebase conflict he files the container's `conflict` or
  `chart-conflict`. Closes the `checkpoint` after its Landing (which _is_ the
  `merged` state). Handles the `checkpoint`; owns `landing` (the land is a bead,
  ADR 0147) and `rollback`, undoing a landed Checkpoint with a revert commit.
  Under a Forge land strategy he also mirrors the land's Forge state (Merge
  Request, merge, conflict, pipeline) into the graph, retrying a failed pipeline
  once before filing it. See ADR 0004, ADR 0047, ADR 0145, ADR 0147, ADR 0188.
- **Luffy** — final adjudicator: triage a Signal (name the Destination and priority, or write
  the fast-lane Spec), approve a checkpoint; owns `signal`, `approval` (ADR 0129) and
  `initiative` (creates an Initiative at triage or writes the body of one a human started;
  in its cut session cuts the next phase or, on the human's "done", closes it; ADR 0128)
- **Brook** — deal with the external world (outside the bead graph and the code); walk a
  human through an Errand; owns `errand` and `latch` (a Latch blocks work until something in
  the world is true). A land's own Forge state is Jinbe's, not Brook's (ADR 0188). Not
  the out-of-band channel ADR 0003 ruled out. See ADR 0115, ADR 0222.
- **Robin** — explain the Workspace read-only from cited sources and, with a
  human's yes, draft Signals and Initiatives from what a conversation finds; owns `discussion`.
  Never writes a repo file; a service query that could write runs only on the
  human's yes. See ADR 0191, ADR 0193, ADR 0194.
- **Chopper** — the crew's doctor (not `mugicrew doctor`, see **doctor**): keep the bead graph healthy and valid; recovers a
  bead stranded by a dead effort, only on a human's command (deterministic code, no
  session), and diagnoses and repairs a broken bead with a human in a **Checkup**.
  See ADR 0126, ADR 0127, ADR 0272.
- **doctor** — `mugicrew doctor`: the read-only verb that checks whether the environment
  and Workspace are ready to run mugicrew (tools, git repo, bead graph, profiles, preview
  renderer, and the warn-only **off-machine backup** and backup freshness rows). It
  reports each warn or FAIL row's precondition and fix, classed **safe fix** or **manual
  fix**, and repairs nothing itself. Not **Chopper**, who repairs stranded beads in the
  graph. See ADR 0302.
- **doctor fix** — `mugicrew doctor fix`: applies every safe fix without asking, lists the
  manual ones it left, re-checks, and exits 1 if any FAIL remains. It changes only this
  machine or clone, never the bead graph. See ADR 0302.
- **safe fix** / **manual fix** — a doctor row's fix class. A safe fix changes only this
  machine or clone, needs no prompt, credential or sudo, is idempotent, and takes only
  values doctor can derive. Every other fix is manual.
  _Avoid:_ "hint" for a fix.
- **off-machine backup** — the copy of a user's Bead Graph on a Dolt destination they name
  (a `file://` path on an off-machine mount or a credentialed remotesapi), kept current by a
  push, not by `bd backup` or `bd sync`. Doctor warns when none is configured or the oldest
  unpushed commit is over 24 h old. Setup, recurrence and the restore drill are in
  install.md's Backup section. See ADR 0367, ADR 0368, ADR 0369, ADR 0370, ADR 0384.
  _Avoid:_ bd backup (a different, local copy), sync.
- **backup remote** — the Dolt remote with the fixed name `backup` on the graph's database,
  added once per clone; its absence means no off-machine backup is configured. See ADR 0367.
- **mugicrew backup push** — the verb that performs one push of the graph's `main` to the
  backup remote. Never forces, never starts the sql-server, lock-guarded, a no-op with
  nothing unpushed; records `backup.pushed` or `backup.failed`. The loop runs it hourly.
  See ADR 0370.

## Bead kinds

A bead's **kind** is its single routing key (carried as a `kind:<value>` label;
mechanics in ADR 0002). A kind is either a **container** (parents member beads;
a bd `epic`) or a **leaf** (a bd `task`). Owner is a function of kind — see
Crewmates. **Kind-ownership invariant:** every kind is owned by exactly one
crewmate (`kind → crewmate` is total and single-valued); a crewmate may own
several kinds; no kind is ever co-owned (co-ownership would make owner-from-kind
routing ambiguous).

- **Container** — a bead that parents member beads; a bd `epic`. Contrast a
  **leaf**, a bd `task` that parents none.
  _Avoid:_ a Docker or OCI container.

Each kind records a short **description** (top line), then as sub-bullets: its
**owner** (with role), whether it is a **leaf** or **container**, the
**operations** crewmates apply to it and what those produce, and free-form
**notes** for anything else that matters.

- **Signal** — an intake into the factory: the head of the spine and, with
  Rollback, one of two kinds that enter the graph from outside mugicrew (ADR 0035, ADR 0047); the
  retired `mugi-grill` Spec door survives only as history.
  - _Owner:_ Luffy (routing owner) (ADR 0129); activation is **human-gated** — open
    Signals form an intake backlog, and a human explicitly invokes Luffy on a chosen one.
  - _Structure:_ leaf
  - _Operations:_ Luffy **triages** the chosen Signal — triage _is_ its own
    `open → closed` transition (no separate triage bead, no stored state). →
    **accept** spawns **exactly one Chart** in succession; **fast lane** (for a
    Signal already sharp, with no fog) spawns a **direct Spec** in succession
    instead, written by Luffy during triage, and keeps full review (ADR 0035);
    **initiative** (for an effort that spans more than one Checkpoint) spawns,
    on the human's yes, **exactly one Initiative** in succession and files
    phase 1's member Signal(s) (ADR 0128); **reject** closes with
    a `rejected[: <why>]` reason and no successor; **duplicate** closes
    `duplicate: <original>` through `mugicrew duplicate <dup> <original>`,
    keeping the `supersedes` lineage of multi-report duplicates; **reroute**
    (when `mugicrew triage locate` finds the Signal's code only on one open
    Checkpoint) files a pivot and/or Critique on that **host**, or merges
    evidence into an open pivot on the same FR, and spawns no successor — see
    **Reroute** (ADR 0322).
  - _Notes:_ Filed by any external actor — a human _or_ a system
    (`system-alert`); the ontology models only the intake, never the source (so
    an external monitor is in scope; it is not Chopper) (ADR 0126). May
    also be an **offshoot** (`discovered-from`) when mid-work turns up genuinely
    new, separate work — distinct from a `pivot` (Spec wrong) or a `critique`
    (defect in current work). No fan-out: a lumpy report splits into
    offshoot-Signals, one Chart each. Signal _variety_
    (bug-report/idea/system-alert/…) shares owner, edge, and lifecycle, so it is
    **not** a routing kind; if ever recorded, an orthogonal `signal-kind:*`
    label, deferred until needed. See ADR 0006.
- **Chart** — Nami's charting **branch** (`chart/<chart-id>`) that wayfinds a
  Signal into a Spec. Born at triage (or by a pivot escalate) with its
  Destination written, and with a **Survey** as its first child; the open
  Survey keeps it from draining until Nami's charting session seeds its fog
  or finishes it in place (ADR 0129, ADR 0140, ADR 0357).
  - _Owner:_ Nami (steward; also handler once drained)
  - _Structure:_ container (parents its Survey and its defog beads)
  - _Operations:_ closes ("is defogged") when all defog beads resolve and is stamped as defogged
    (by promote-or-stamp, or by its Survey's `mugicrew chart finish` when it had no fog) → **authors and spawns**
    **exactly one Spec** in succession (a speccing session writes the Spec body
    by synthesizing the Chart + its closed defog beads and closed Survey — charting's terminal act;
    see the Spec entry). Lands via a **Landing** (Jinbe's, spawned by Nami once
    the Chart is drained and stamped) by **rebase + squash** onto
    mainline (the local land strategy, as for a Checkpoint), so its **landed base**
    is one squash commit (on the OE, a Checkpoint; never a Stock).
  - _Notes:_ Charting and its knowledge-bearing defog sessions (grilling →
    ADRs/CONTEXT, research → `docs/research/*`), plus prototype and chart-conflict
    sessions, **operate in a worktree of `chart/<id>`** (ADR 0071), so their artifacts (ADRs, CONTEXT edits, research docs) accrue
    on the branch and **land on mainline once the Chart is defogged and its Approval is approved**
    (ADR 0388): defog is the review, and
    the Approval gate, with no Usopp Review, is the human's yes. Prototype output does **not** land here (side
    ref) (ADR 0009). Separable work found mid-defog splits as an offshoot-Signal
    (ADR 0006), not a second Spec. See ADR 0008, 0009, 0010.
- **Survey** — a Chart's HITL charting leaf: the bead of the human-started
  session in which Nami maps the Chart's fog with a human.
  - _Owner:_ Nami
  - _Structure:_ leaf (exactly one per Chart, seeded with it)
  - _Operations:_ the session claims the Survey and drafts as it, but parents,
    appends to and pushes back on the Chart: it seeds fog, narrows the
    Destination, splits off offshoots, or hands back, rejects or supersedes
    the Chart, or, with no fog and the human's assent, finishes it in place
    (`mugicrew chart finish` stamps it defogged). mugicrew closes the Survey on
    clean exit once fog is published, the Chart is closed or the Chart is
    stamped, with the reason `seeded: <chart id>`, `pushed back: <chart id>` or
    `defogged: <chart id>`; otherwise it returns to the Inbox. A chain withdraw
    closes it `withdrawn`. A Survey retires only with its Chart, through a chain
    withdraw or a session that pushes the Chart back.
  - _Notes:_ Not a defog bead: its body points at the Chart, and its notes
    carry the decisions it settled when it finished its Chart in place. Speccing
    reads a closed Survey; promote-or-stamp skips it, and it never counts as fog
    (ADR 0357). Charting is work, not a
    state, so it is a kind, as Approval is (ADR 0140). _Avoid:_ charting
    Grilling, Unmapped Chart.
- **Grilling** — a defog bead resolved by interviewing a human.
  - _Owner:_ Nami
  - _Structure:_ leaf
  - _Operations:_ resolve (interview) → close, contributing to the Chart's defog.
    Born from Nami's Survey charting, grilling and promote-or-stamp sessions, Sanji's
    prototype session, the chart-conflict escalation, the Forge Review on a Chart and
    Chart Approval rework. Closes: `answered: <summary>` (completed, by the session after
    `mugicrew integrate`, and by the chart-conflict escalation grilling and the
    Forge Review grilling through the same launch); `obsolete: <decider id>`
    (`mugicrew obsolete <bead> --by <decider>`); `superseded: <new id>`
    (`mugicrew supersede <bead> --with <new>`, which also adds the `supersedes`
    edge, and the door for a duplicate defog bead); `withdrawn` (the human's
    Withdraw of the one leaf); `cancelled: <initiative>`. Only `answered:` counts
    as a decision. While the chart-conflict that drafted an escalation grilling
    is open, it closes only `answered:` (ADR 0417).
  - _Notes:_ One of the "Fog" defog beads. Born `discovered-from` the bead whose work raised it (the Survey, the grilling being resolved, the prototype, the chart-conflict, the Forge Review or the Approval), written in the same create; Nami's promote-or-stamp on a drained Chart is the exception, its drafter being the Chart, already its parent (ADR 0401 (mugicrew-a-landing-s-follow-up-is-born-discovered-from-it) §1).
- **Research** — a defog bead resolved by autonomous agent investigation.
  - _Owner:_ Nami
  - _Structure:_ leaf
  - _Operations:_ resolve (investigate) → close, contributing to the Chart's
    defog. Closes: `Wrote docs/research/<slug>.md: <summary>` (completed, by Go
    from the agent's last line); `obsolete: <decider id>`; `superseded: <new id>`;
    `withdrawn`; `cancelled: <initiative>`, by the same doors as a Grilling.
  - _Notes:_ One of the "Fog" defog beads. Born `discovered-from` the bead whose work raised it (the Survey, the grilling being resolved, the prototype, the chart-conflict, the Forge Review or the Approval), written in the same create; Nami's promote-or-stamp on a drained Chart is the exception, its drafter being the Chart, already its parent (ADR 0401 (mugicrew-a-landing-s-follow-up-is-born-discovered-from-it) §1).
- **Prototype** — a defog bead resolved by a high-fidelity prototype that
  validates interface design.
  - _Owner:_ Sanji
  - _Structure:_ leaf
  - _Operations:_ resolve (build + validate) → close, contributing to the Chart's
    defog. Closes: `prototyped: <verdict>` (completed, by the session after
    `mugicrew integrate`); `obsolete: <decider id>`; `superseded: <new id>`;
    `withdrawn` (needs `--discard` when the throwaway tree holds work);
    `cancelled: <initiative>`, by the same doors as a Grilling.
  - _Notes:_ Throwaway by definition: code is pushed to a side ref
    (`refs/mugicrew/prototypes/<chart-id>`), preserved for lineage but **never
    merged** — downstream builds on the validated design in the Spec, not the
    throwaway. The session commits its decisions on its own **Effort branch**
    and integrates them onto `chart/<id>`; the throwaway is built in a separate
    detached tree and never lands on either. Born `discovered-from` the bead whose work
    raised it (the Survey, the grilling being resolved, the prototype, the chart-conflict,
    the Forge Review or the Approval), written in the same create; Nami's promote-or-stamp
    on a drained Chart is the exception, its drafter being the Chart, already its parent
    (ADR 0401 (mugicrew-a-landing-s-follow-up-is-born-discovered-from-it) §1).
    See ADR 0009, 0071, 0102.
- **Spec** — a closed record produced by a defogged Chart: the plan, sliced into
  Builds. Its body is never edited; it may accrue **Amendments**.
  - _Owner:_ Zoro
  - _Structure:_ leaf
  - _Operations:_ spawns the Checkpoint in succession, and is sliced into many
    Builds. A **direct Spec** has no Chart above it: born by Luffy's **fast
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
  landed change (ADR 0089). Concretely the **build** branch (`checkpoint/<id>`), cut from its Spec's
  base: the Chart's **landed base**, or — for a direct Spec — the Spec's
  the Spec's **anchor** (ADR 0287). A **successor Checkpoint** is the one a
  Rollback produces: same Spec, caused by the Rollback, cut at the revert commit
  (ADR 0048), or at main HEAD when the Rollback fixes forward (ADR 0049). A
  **surgery Checkpoint** is the one a fresh-Checkpoint target of the New Surgery form opens for a human's Surgery: no Spec and
  no predecessor, cut at main HEAD when the Surgery is filed, with the Surgery
  as its first member (ADR 0091). It takes further Surgeries like any open
  Checkpoint (ADR 0163).
  - _Owner:_ Zoro (steward, creates it); Jinbe (handler, lands it via a Landing; steward of a
    successor Checkpoint); Franky creates a surgery Checkpoint (ADR 0092)
  - _Structure:_ container (parents the Build/Review/Critique/Approval/Landing beads)
  - _Operations:_ lifecycle **is** the branch lifecycle
    (`cut → in-review → approved → merged`), derived from its children. Once
    approved, drained and unblocked, Jinbe spawns its **Landing**, which lands
    it by **rebase + squash** onto the current base; Jinbe closes the Checkpoint
    after its Landing (which _is_ the `merged` state). On a rebase conflict the
    Landing files a `conflict` bead on the Checkpoint.
  - _Notes:_ See ADR 0004, 0010.
- **Build** — a unit of implementation work sliced from the Spec: a slice
  inside a Checkpoint.
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
    Critiques is _clean_ and spawns the Approval; the clean close names that
    Approval (ADR 0415 (mugicrew-review-conflict-and-critique-completed…)).
  - _Close reasons:_ `clean: <approval id>`, `critiqued: <critique ids>`,
    `held: pivot <pivot ids>` (no Critiques, but an open pivot withholds the
    Approval), `pivoted: <pivot ids>` (the session raised its own pivot) and
    `obsolete: …`. Reviews closed before this naming keep plain `Closed`.
  - _Notes:_ Usopp writes the pin onto the Review as a `Commit:` line at review
    start, and a re-dispatch rewrites it. No outcome obsoletes a Review whose
    branch moved past its pin: a pass against a commit the branch has left
    spawns an Approval behind the tip, the at-head gate rejects it, and the
    drained Checkpoint gets a fresh Review.
- **Critique** — a finding raised against the work (offshoot of a Build,
  Review, Landing or Approval).
  - _Owner:_ Franky
  - _Structure:_ leaf
  - _Operations:_ raised as an offshoot → resolve → close. A Landing whose gate
    build fails raises one carrying the **target base commit hash**; Franky
    rebases onto that base before fixing, so he fixes the tree that failed.
  - _Close reasons:_ `completed` (fixed with no `Rationale:` comment),
    `accepted: <rationale>`, `rejected: …`, `deferred: …`, `pivoted: <pivot ids>`,
    `dismissed: <pivot id>` (an Ask Critique dropped by pivot triage) and
    `obsolete: …`. Critiques closed before this naming keep plain `Closed`.
  - _Notes:_ See ADR 0152. A Critique is born discovered-from the bead that
    raised it, with the pivot (a refiled FR, discovered-from the pivot) and the
    Signal (a reroute, caused-by the Signal) as the named exceptions that keep
    their one provenance edge; see ADR 0401
    (mugicrew-a-landing-s-follow-up-is-born-discovered-from-it) §1.
- **Forge Review** — one pass over a container's unacked Merge Request comments
  (plain comments and thread notes, from any reviewer, bots included) that files
  the actionable ones on the container, merged where they duplicate, and acks
  every comment it read so none is read again.
  - _Owner:_ Usopp
  - _Structure:_ leaf (member of a Checkpoint or a Chart; at most one open per
    container); AFK
  - _Operations:_ Jinbe spawns it when unacked comments appear → Usopp files
    Critiques (Checkpoint) or grillings (Chart) → close. A pass that filed or
    folded notes closes `completed: filed N, folded N, skipped N; read through
    note X`; an empty pass closes `completed: nothing actionable; read N notes
    through X`. A Forge Review swept with its container closes with that
    container's sweep reason. Not a **Review**, which reviews the code at a pin.
  - _Notes:_ Charted on the Chart "Actionable Merge Request comments are filed
    deduplicated and acked".
- **MR Watch** — the leaf that reads a container's open Merge Request every paced
  tick, so a comment left between Landings is picked up and spawns a **Forge
  Review**.
  - _Owner:_ Jinbe
  - _Structure:_ leaf (child of a Checkpoint or a Chart, never a member and never
    undraining it; at most one open per container); AFK, Mechanical
  - _Operations:_ spawned when the first MR is recorded for the container → read
    the MR each `landing_poll_interval` (skipped while a Landing is pending, whose
    own tick reads it) → close once no filed child still owes a close-out and
    either the MR is merged or closed or the container closes; until then it
    outlives its container, answering late closes; a close-out still failing 7
    days after the container closed is abandoned and carried by a filed
    Signal. It closes `completed: container closed` when its container lands or
    closes with no MR recorded, under a non-gitlab strategy, or with nothing
    owed; `completed: merge request merged` or `completed: merge request closed`
    once nothing is owed and the MR reads so; and `abandoned: close-outs still
    failing; carried by <signal id>` when it gives up, the Signal filed first. It
    closes with its container's sweep reason only where a sweep reaches it.
    Its body is the `MR:` line plus the `MR link:` line, copied from
    the Landing, and, once its container is closed, the `Close-outs due:` /
    `Close-outs failing:` lines.
  - _Notes:_ See ADR 0185 §8, ADR 0343 (an-mr-watch-closes-once-nothing-is-owed…)
    and ADR 0345 (an-outliving-mr-watch-carries-its-close-out-lines…).
  - _Under gitlab:_ it reads the Draft MR's draft flag and closes the container's
    waiting Approval `approved` once the MR is un-drafted and no pivot is open. It stays
    open while a waiting Approval names a closed MR, so a reopen is read (ADR 0382).
- **Approval** — the stamp that gates the land.
  - _Owner:_ Luffy
  - _Structure:_ leaf (one per Checkpoint or Chart; for a Chart, spawned by Nami at the
    `chart/<id>` head with no Review) (ADR 0388)
  - _Operations:_ Luffy _writes_ the **Decision Packet** (a **Chart Packet** on a Chart) to the Approval,
    presents it, revises it on request, and offers the approve action that closes
    the bead. Open = merge pending; closed approved is the sole
    trigger for the Checkpoint's derived `approved` state; an Approval closed
    for rework or obsoleted (`obsolete: branch advanced past the pinned commit`) lands nothing (carries **no ordering
    edge**; a **Landing** succeeds it).
  - _Notes:_ Spawned on demand when a Review closes clean, in **succession** to
    that Review, so it certifies that Review's exact commit; if the branch
    advances past it, it is **obsoleted and respawned** off the new clean Review. A Chart's
    Approval is spawned by Nami once the Chart is drained and stamped, with no Review to succeed (parent edge only), and respawned at the new
    head when the branch advances; a Chart with nothing ahead of mainline gets none.
  - _Auto-approve:_ while the `[approvals] auto_approve` setting is on (layered: the
    machine-local override, then the shared config, then off), an unmarked
    Approval is AFK: the loop dispatches it and an AFK Luffy session writes the
    Decision Packet, ending its Approval History with the line for the layer in force
    at run start: `auto-approved (AFK, Workspace auto-approve on)` (checked-in
    config), `auto-approved (AFK, machine-local auto-approve on)` (machine-local),
    or `auto-approved (AFK, auto-approve off at run start)` (flipped off after
    dispatch); Go closes it `approved`. Off, or marked, it is HITL.
    A third consecutive failed AFK session adds the sticky `auto-approve:failed`
    **fallback marker**, which makes it an ordinary HITL Approval until it closes or
    a human removes the label (ADR 0341, ADR 0346, ADR 0353).
  - _Under gitlab:_ the Approval is AFK unless marked `auto-approve:failed`. Its session
    writes the Decision Packet; Go then opens the Checkpoint's MR as a GitLab Draft (or
    reuses the open one) with the Packet and the Landing's trailer block as its
    description, records `MR:` and `MR link:`, and leaves the Approval open, awaiting
    un-draft. The human's un-draft approves: the MR Watch closes it `approved`
    (`approved (un-drafted on GitLab)`). Under auto-approve the MR opens not as a
    GitLab Draft and Go closes the Approval. A re-approval closes at once when the MR is
    still un-drafted and no pivot amended the Spec since the last approved Approval;
    otherwise Go re-drafts the MR and it waits (ADR 0382).
  - _Carried:_ a **carried Approval** is one Go writes, closed approved at birth, when the
    head differs from the last approved one only by a renumber of the branch's own numbered
    files. It succeeds the Approval it carries, and its authority is that of its **origin**,
    the first Approval up that chain that was not carried (ADR 0397, ADR 0398).
    _Avoid:_ auto-approved (that names an AFK session's stamp), skipped Approval.
- **Decision Packet** — the written case for approving a Checkpoint, kept on its
  Approval and carried into its land commit (and, for a remote-truth Workspace,
  its Merge Request body; under gitlab the Draft MR's description from the moment it
  opens, followed by the Landing's trailer block) (ADR 0382). Sections: Background, Summary, Evidence, Merge Danger,
  Execution Notes, Approval History (crew doc `approval-body.md`). A Chart's
  counterpart is the **Chart Packet**.
  _Avoid:_ PR body, approval doc, formal document.
- **Chart Packet** — the written case for approving a Chart, kept on its Approval:
  the Decision Packet's six headings in the same order, each read for a Chart (the
  Signal charted and its Destination, a table of the ADRs, GLOSSARY entries, research
  docs and crew docs that land, the pinned SHA and the defog beads and Survey behind
  each decision, the ADRs superseded or amended, chart-conflicts and narrowings, the
  Approval History). Written by Luffy; the Approval's `Commit:` and `Patch-id:` lines
  stay first and verbatim. It is the land commit's body and, under gitlab, the Draft
  MR's description, each followed by the trailer block (ADR 0388).
  _Avoid:_ Chart summary, Chart review.
- **Conflict** — a rebase conflict, or a numbered-file clash (ADR 0219, ADR 0030, ADR 0069),
  hit either by the pre-review probe, when Jinbe would spawn the Review, or while a
  Landing lands the Checkpoint; or a human's **requested rebase** of the Checkpoint
  onto mainline (the `requested` variant) (ADR 0262).
  - _Owner:_ Franky
  - _Structure:_ leaf (member of the Checkpoint)
  - _Operations:_ Jinbe's Landing aborts the rebase, files it on the Checkpoint
    carrying the **target base commit hash**, and closes itself obsolete (ADR 0145). Franky resolves it (rebase + resolve) → closing spawns a fresh
    Review, so the re-landed commit is re-reviewed and the stale Approval is
    obsoleted. A pre-review Conflict (filed by Jinbe instead of the Review) is
    resolved the same way, but its close leads to a re-probe and then the first
    Review, not a re-review.
  - _Close reasons:_ `resolved`, `pivoted: <pivot ids>`, `superseded: <conflict
    title>` (a requested Conflict replaced by a real one) and `obsolete: …`.
    Conflicts closed before this naming keep plain `Closed`.
  - _Notes:_ Named for the problem, not the action (both Franky and Jinbe rebase).
    See ADR 0004. A **numbered-file clash** is a branch adding a file whose number
    mainline already holds in the same sequence and number-space directory under another
    filename (an ADR, or a configured `{n}` template such as a migration); its body is
    the **number-clash** variant, listing each clashing path with its template.
    _Avoid:_ adr-number-clash, "ADR-number clash" (History: the entry covered ADR numbers only until the numbered-file clash variant was added).
    A **requested rebase** is filed by a human only (never an agent), pinned to
    mainline at filing, and closes like any Conflict (ADR 0262). The CLI door
    (`mugicrew rebase`) refuses a crewmate session (an effort id or HITL pane in the
    env) with exit 1 and no override; an agent suggests one by an offshoot Signal.
    At most one Conflict is open per Checkpoint, whatever its variant (a human's
    requested rebase included); Landing supersedes an unclaimed requested one (ADR 0261).
    A Conflict a Landing raises is born discovered-from that Landing (ADR 0401); a
    pre-review or requested one carries no provenance edge.
- **Chart-conflict** — the Chart-land counterpart of Conflict: a collision that
  stops a Chart's Landing from landing the Chart branch, or a human's request to
  rebase the Chart branch onto mainline. One kind, three **variants**: a
  textual **rebase-conflict**, an **number-clash** (the Chart adds a numbered
  file whose number mainline already has; legacy token `adr-number-clash`), or a
  human-filed **requested** rebase (ADR 0262); its CLI door (`mugicrew rebase`)
  refuses a crewmate session, as for a Conflict.
  - _Owner:_ Nami
  - _Structure:_ leaf (member of the Chart)
  - _Operations:_ Jinbe's Landing files it on the Chart carrying the branch, the **target base commit
    hash**, the **Chart tip** at filing, and the variant. Nami resolves it AFK:
    rebases onto that base and, for a clash, renumbers the Chart's ADRs above
    mainline's max in each ADR's own `docs/adr/` and sweeps references in what the Chart owns. The agent
    commits; Nami closes it only after a **post-resolve check** confirms
    mainline's content survived; closing it → the Chart re-drains and
    a fresh Approval follows, with **no Review** (a Chart has none). On a real
    judgment call it **escalates**: a grilling bead on the Chart blocks the
    chart-conflict until the human answers, then the AFK writer applies the
    answer.
  - _Notes:_ See ADR 0011, ADR 0030, ADR 0219, ADR 0079, ADR 0080, ADR 0261, ADR 0262, ADR 0400. At most
    one is open per Chart, whatever its variant; Landing supersedes an unclaimed
    requested one (ADR 0261). One the Chart's Landing raises is born discovered-from
    that Landing (ADR 0401); a requested one carries no provenance edge. It closes
    `resolved: <summary>`, `superseded: <title>`, `moot: …`, `withdrawn` or
    `cancelled: <initiative>`.
- **Pivot** — work on a Checkpoint proves the **Spec** wrong (the plan, not the
  code). A triaged Signal can raise one on its **Host** by **Reroute**, and a
  **Errand** can raise one on the Checkpoint it holds.
  - _Owner:_ Zoro (triages)
  - _Structure:_ leaf (offshoot of its raiser: discovered-from it, or caused-by
    the Signal for a reroute; pointing at the Spec; its host is the Checkpoint
    the raiser holds)
  - _Operations:_ raised by the discovering crewmate (Franky/Usopp), whose own
    bead then closes normally, or by a stuck Errand (Brook), which stays claimed; a live pivot **blocks the whole Checkpoint** —
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
  - _Notes:_ See ADR 0005, ADR 0035, ADR 0043, ADR 0044, ADR 0045, ADR 0046,
    ADR 0326 (mugicrew-a-task-raises-through-mugicrew-pivot-raise…), ADR 0416
    (mugicrew-a-pivot-s-host-is-its-raiser-s-checkpoint). Zoro
    recommends the outcome; the human picks. One pivot = one FR: a second
    finding on the same FR merges into the open pivot. A Checkpoint's open
    pivots are triaged together; correcting or escalating sweeps the rest.
  - _Close reasons:_ `confirmed`, `escalated`, `dismissed: <gist>`,
    `amended: <gist>`, `superseded: <lead>` for a sibling the lead's sweep
    retires (`sibling superseded`), `superseded: gated Checkpoint <ids> closed`
    for a straggler, and `obsolete: …` for a retired Draft. Each is a bd
    constant or builder (`bd.DismissedCloseReason`, `bd.AmendedCloseReason`,
    `bd.SiblingSupersededReason`); the four outcomes run through the sweep verbs
    `mugicrew pivot confirm`, `escalate`, `dismiss` and `amend`.
- **Rollback** — a human's request to undo one landed Checkpoint that broke
  main, and recover its work.
  - _Owner:_ Jinbe
  - _Structure:_ leaf (unparented; `related` to its target Checkpoint)
  - _Operations:_ filed directly by a human through the History tab's `u` key,
    its only door: no CLI verb, no agent filing (ADR 0047), no triage. Jinbe adds a
    **revert commit** on main for the target's squash, never rewinding main,
    then hands the work on to a **successor Checkpoint** under the same Spec,
    caused by the Rollback and seeded with the reverted work, whose first member
    is a Build carrying the Rollback's breakage evidence. The Rollback closes at
    that handoff. A target that never landed closes the Rollback as invalid; an
    already-reverted target skips the revert. A revert that conflicts with later
    lands is skipped, and the Rollback **fixes forward**: the successor is cut
    at main HEAD with no seed, and its Build fixes the breakage in place.
    Under remote truth the revert goes through a revert MR; a human closing
    that MR **abandons** the Rollback, with no successor.
  - _Close reasons:_ `handed off: <successor id>` (completed), `fix-forward:
    <successor id>`, `abandoned: revert MR closed`, `invalid: …` (no related
    Checkpoint, target not landed, or no land trailer), and `withdrawn`. Each
    outcome with a successor names it; Rollbacks closed before this naming
    keep their old reason (plain `Closed`, `fix-forward: revert conflicted`).
  - _Notes:_ "Rollback" is the bead and the operation; "revert commit" is only
    the git mechanism. One Rollback = one Checkpoint; a landed Checkpoint is
    never reopened. Agents never file one. See ADR 0047, ADR 0048, ADR 0049,
    ADR 0208.
- **Remnant** — the in-flight work that a Forge merge left off mainline when it
  landed a Checkpoint at an earlier approved pin. It is also the HITL bead that asks
  the human which exit to take: drop, offshoot or respin.
  - _Owner:_ Zoro
  - _Structure:_ leaf (one per landed Checkpoint; discovered-from the Landing,
    `related` to the landed Checkpoint)
  - _Notes:_ See ADR 0402, ADR 0407, ADR 0409.
  _Avoid:_ Split, Recovery.
- **Respin** — the Remnant exit that cuts a new Checkpoint for the landed
  Checkpoint's Spec from fresh mainline, seeded with the stranded work. It is also
  that new Checkpoint.
  - _Structure:_ container (kind:checkpoint), caused by its Remnant, with no new
    Spec; in the landed Checkpoint's Initiative while that is unclosed
  - _Notes:_ It goes through the full Review and Approval, because nothing it
    replays was approved onto mainline. See ADR 0407.
  _Avoid:_ rollback successor, re-cut.
- **Renumber Checkpoint** — a Spec-less Checkpoint filed on the landed container's
  reconcile pass (Jinbe for a Checkpoint, Nami for a Chart), owned by Jinbe, when a landed
  gitlab container is the later side of a numbered-file clash (two Forge merges
  gave two files the same ADR or sequence number).
  - _Owner:_ Jinbe
  - _Structure:_ container (kind:checkpoint), caused by the landed container,
    in its Initiative while that is unclosed, with one Build member; cut from
    the fetched Forge main
  - _Operations:_ filed before the landed container closes, once per landed
    container. Its Build renumbers the later file and sweeps references (crew
    doc sequence-renumber.md); it then runs the full Review, Approval, Landing
    and Forge merge path. The earlier side files nothing.
  - _Notes:_ recognised from the graph alone: a Checkpoint caused by a Chart or
    Checkpoint, where an ordinary one is caused by a Spec. See ADR 0225.
- **Landing** — one bead per attempt to land a container's branch on mainline,
  pinned (`Commit: <sha>`) to the base it will land. Its outcomes: _landed_
  closes it `landed: <sha>`; _conflict_ closes it `obsolete: conflict` and files
  the container's conflict bead; _pending_ leaves it open; _branch advanced_
  closes it `obsolete: branch advanced without landing`;
  _build failed_ closes it `obsolete: build failed` and files a Critique on the
  Checkpoint; _pipeline failed_ closes it `obsolete: pipeline failed` and files
  a Critique on a Checkpoint, or a hold Errand on a Chart; _merge request
  closed_ closes it `obsolete: merge request closed` and holds the container on
  an Errand.
  _Avoid:_ the act of landing, which is **Land**.
  - _Structure:_ leaf (member of a Checkpoint or a Chart; at most one open per
    container); AFK, Mechanical, no session
  - _Notes:_ The `<Kind>: <id>` trailer on mainline stays the durable landed
    fact; the Landing's close mirrors it. Only Checkpoint and Chart lands are
    Landings; other moves of main (Rollback reverts, and legacy Spec anchors)
    stay bead-less (ADR 0145, ADR 0147). Every follow-up a Landing files
    (Critique, Conflict, Chart-conflict, Hold Errand) is born discovered-from
    it (ADR 0401).

Landing mechanics (not lifted into vocabulary.md): Jinbe owns it. It is
spawned at the container's land gate, pinned to the commit it will land: on a
Checkpoint, once an Approval is at head, members are drained and no blocker is
open, copying the Approval's pin and succeeding it; on a Chart, once it is
drained and stamped, once an Approval is approved at the branch head, copying its pin
and succeeding it (a Chart with nothing ahead of mainline skips the Approval and lands
its bead pin directly, ADR 0388). Jinbe runs it through the **Land
strategy**; any error other than conflict or pending (including a dirty main)
leaves it open and the effort fails. A branch that advanced past its pin sends a
Checkpoint's gate back to its Approval gate, and a Chart's gate respawns an
Approval, as docs/bead-lifecycle.md says. A container's reconcile closes a
stranded Landing first, and the container closes after its Landing. Under
gitlab a pending Landing records `MR: !<iid>` and, beside it,
`MR link: <web_url>` (GitLab's absolute URL for the MR, never derived; no line
when the URL is unknown).

- **Surgery** — a HITL session in which a human dictates small code changes on
  a Checkpoint and Franky writes them, then hands them to review.
  - _Owner:_ Franky
  - _Structure:_ leaf member of any open Checkpoint; at most one open per Checkpoint
  - _Operations:_ filed directly by a human, with no triage. `n` then `u` opens the New
    Surgery form: a Checkpoint target joins it; a new-Checkpoint target creates a
    surgery Checkpoint with the Surgery as its member. Filing launches the first session. The human directs and Franky writes: every edit and commit
    is his, one commit per accepted change, each behind the Workspace's
    gates. Spans one or more sessions: a session **stops** by committing and
    exiting, leaving a comment on the bead that records what it did. The
    surgery **ends** only on the human's explicit word, which closes the
    Surgery; Jinbe then spawns the Review. The end rule counts only this Surgery's
    trailered squashes: with some on its Checkpoint it closes `ended`; with none it
    closes `abandoned: surgery ended with no commits`, and its Checkpoint closes
    with the same reason too only when nothing is ahead of main and the Surgery is
    its only child. A Surgery also closes `withdrawn` when its Checkpoint is
    withdrawn, and `cancelled: <initiative>` when its Checkpoint's Initiative is
    cancelled. An idle Surgery ends either way: `enter` (resume) on its row, then
    telling Franky "end"; or the human running `mugicrew surgery end <id>`. A
    stop or a drop leaves it open and idle and is not a close.
    Its commits are reviewed against the Surgery bead (the Spec, if any, stays
    the intent), its Critiques go to Franky as usual, and pivots follow the
    Spec: none on a surgery Checkpoint. While open (idle included) it
    holds its Checkpoint's Critiques from Franky; one it fixes by hand is
    closed afterwards by Franky's roll-forward.
  - _Notes:_ HITL only; agents never file one but may suggest one by offshoot
    Signal. "Dictation", not authorship: the human authorizes each change.
    An **idle** surgery is open and in progress, with no session running;
    `enter` on its row resumes it. Never "paused": that word is the loop's
    (Paused). See ADR 0091, ADR 0092, ADR 0093, ADR 0094, ADR 0095, ADR 0160, ADR 0162, ADR 0163.
- **Amendment** — a decision appended to a Spec that replaces one FR's
  mechanism while keeping its intent. It comes from an amended Pivot.
  - _Owner:_ Zoro (writes it at triage)
  - _Structure:_ not a bead; a marked comment on the Spec
  - _Operations:_ written by `mugicrew pivot amend`; read alongside the Spec
    body; the latest Amendment for an FR overrides it.
  - _Notes:_ A Spec's effective content is its body plus its Amendments in
    order, so the Spec stays append-only. A superseding Spec folds them into
    its body. See ADR 0044, ADR 0045.
- **Errand** — a HITL walk-through of steps that change the external world
  (outside the bead graph and the code), such as creating a repo or minting a
  token.
  - _Owner:_ Brook
  - _Structure:_ leaf: a direct Initiative child when ordered against a member,
    else outside (never a Chart child, Checkpoint member or Spec child). Its
    body is a numbered `## Steps` list of Do/Check steps, optionally after a
    lead and fact lines and before a `## Diagnostics` section the guide never
    walks (crew doc `errand-body.md`); its run record is comments: one
    value-free `passed` per step, `amended` for an approved rewrite, one
    `stuck` on a stuck exit.
    Ordered only by `blocks` edges: after a land (blocked by the Checkpoint),
    before a land (the Checkpoint blocked by it), or after another Errand.
    A Spec's `## Errands` entry may adopt an existing open Errand by id (`errand:
    <id>`): slicing files no duplicate and only adds its ordering edges.
  - _Operations:_ only a human launches or resumes it (`enter`/`b`); the loop never
    does. The guide runs at the repo root, re-runs every Check to find where it
    is, and closes the Errand itself, with reason `completed`, only after a final
    all-checks pass and the human's in-session yes. The one exception to "only
    the guide closes it" is Go's hold clearing step, for a Hold Errand only and
    never while its guide session is live (ADR 0393). It may raise a pivot or Critique on the one
    Checkpoint it holds. An interrupted or stuck Errand stays claimed and
    **idle**; `x` unclaims it. Abandoning one is a **withdraw**, with `X` or
    `mugicrew withdraw <errand>`, not a hand `bd close`.
  - _Notes:_ Not a defog kind: a Chart never contains one (ADR 0013). Errands
    are filed by `mugicrew errand new`, and crew producers file them only that
    way: from a Spec's `## Errands` when it is sliced and from Nami's charting,
    grilling and promote-or-stamp sessions; Hold Errands are the only Errands Go
    files. A human may also file one by hand with raw `bd create` and
    `kind:errand`, with no Signal and no triage (ADR 0113 §1, ADR 0117 §3, ADR
    0350 §2); that path skips `errand new`'s Initiative climb, so the Errand gets
    no `Initiative:` line and stays outside any Initiative unless the human
    parents and orders it. A crewmate that finds an
    external-world step partway through a Build, Review or other work still files
    an offshoot Signal. Going forward only:
    an earlier free-standing Errand is not migrated, and a later Chart adopt does
    not carry the Errands its session drafted. See ADR 0352, ADR 0113, ADR
    0114, ADR 0115, ADR 0116, ADR 0117, ADR 0118, ADR 0335, ADR 0342, ADR 0350.
    Existing `kind:task` beads, closed included, are relabelled by `mugicrew
    doctor fix`; the loop refuses to start while any remain (ADR 0355).
  - _History:_ named Task until ADR 0354.
  - _Avoid:_ "Task" (bd's issue type, Claude Code's Task tools), "chore",
    "runbook"/"playbook", "TODO", "manual".
- **Hold Errand** — an Errand Go files to block a container or Rollback until a
  Forge or mainline condition clears (a closed or trailerless merge request, a
  failed pipeline, a foreign land), found again by a whole fact line such as
  `Closed MR: !N`. Its Checks probe through `mugicrew probe` (ADR 0336). It
  joins its container's Initiative like any Errand (ADR 0352).
  Its closure outcomes are `completed` (the guide, after the human's yes),
  `cleared: <key line>` and `obsolete: …` (Go's hold clearing step, for the
  variants it reads) and `withdrawn…` (as Errand). A foreign land (`Foreign:`)
  and every Chart `Pipeline:` hold are guide-only for clearing, but Go still
  closes them `obsolete:` when the bead they block is closed (ADR 0393, ADR 0396).
  It is born open, not as a Draft, because Go files it with no session to
  publish it. Its lineage is its key fact line, a `blocks` edge to the container
  or Rollback it holds, a `discovered-from` edge to the Landing for a
  closed-MR, failed-pipeline or trailerless-merge hold a Landing raised (ADR 0401),
  and, when it re-files after an earlier hold on the same
  key closed, a `related` edge to that closed hold. A revert-pipeline or
  foreign-land hold has no `discovered-from`. The key, not an edge, is its
  identity for finding it again, because the MR number or sha that makes a hold
  unique can't live on an edge.
  _Avoid:_ "foreign hold" for the foreign-land Errand (that term names a
  checked-out effort branch), "blocker".
- **Latch** — a bead that blocks other work until a fact in the outside world
  is true; today, a Jira ticket reaching a done status, or a GitLab MR being merged.
  - _Owner:_ Brook
  - _Structure:_ leaf, free-standing like an Errand (never a Chart child,
    Checkpoint member, Spec child or Initiative member). Placed only by the
    `blocks` edges it holds; it may block any bead but an
    Initiative (ADR 0348).
  - _Operations:_ filed by a human or a session (as a Draft) through one verb,
    at most one open per watched item; re-checked unattended by the loop on an
    interval, no session; closes `completed` once its fact holds, `orphaned`
    when nothing open waits on it, or `withdrawn` by a human, and never
    reopens. A watched MR closed unmerged or missing pauses it _unmet_;
    repeated failed reads, including a gone Jira ticket, pause it _failing_.
  - _Notes:_ Carry makes an open Latch also block a blocked bead's
    successor; a Latch whose blocked beads are all closed closes itself. See ADR 0222 (a-jira-latch-is-one-per-key…), ADR 0223 (an-orphaned-latch-closes…), ADR 0389 (a-latch-whose-mr-is-closed-or-missing…). _Avoid:_ gate (the
    Survey's name; "gate" stays informal for land and review gates), fence,
    external.
- **Discussion** — one human-started session with Robin, in which she
  explains the Workspace and, with the human's yes, drafts Signals and Initiatives, as a bead.
  - _Owner:_ Robin
  - _Structure:_ leaf, free-standing and off the spine, like an Errand: never a
    member of a Chart, Checkpoint or Initiative.
  - _Operations:_ only a human launches, resumes or drops one; the loop never
    does. Robin closes it `completed` when the human says they are finished; a drop
    closes it `dropped`. Each Signal
    or Initiative she drafts is an offshoot (`discovered-from`) of it. A retrospective keeps
    its title: "Retrospective <target title>", not "Discussion <time>".
  - _Notes:_ See ADR 0194. _Avoid:_ chat, conversation, Robin session (for the
    bead).
- **Retrospective** — a Discussion Robin runs on a finished **target** (a Signal,
  Chart, Spec, Checkpoint or Initiative whose **spine** has no open bead): she
  reads that spine's sessions and drafts Signals from recurring patterns.
  - _Structure:_ an unparented Discussion titled "Retrospective <target title>",
    `related` to its target. Its body holds the key lines `Target: <id>` and
    `Spine: <id> <id> …`. The human starts it with `n t`.
  - _Operations:_ closes like any Discussion: `completed` or `dropped`.
  - _Notes:_ A retrospective keeps its title (see **Discussion**).
- **Checkup** — one human-started session in which Chopper diagnoses, with the
  human, a bead they think is broken (its **target**) and repairs it, as a bead.
  - _Owner:_ Chopper
  - _Structure:_ leaf, free-standing and off the spine, like an Errand; bound to
    its target by a `related` edge. At most one open Checkup per target.
  - _Operations:_ only a human launches, resumes or drops one; the loop never
    does. While it is open its target carries `paused:checkup`. It closes as
    repaired (`repaired: <what changed>`), not a bead fault
    (`not a bead fault: <why>`) or dropped (`dropped`). Every close lifts
    `paused:checkup` from a target only when no other open Checkup still holds it.
  - _Notes:_ See ADR 0272; launched by `mugicrew checkup <bead>` or `c` on a
    bead row (ADR 0274). Not **recover**, which repairs a bead stranded by a
    dead effort with no session. _Avoid:_ repair, recovery, doctor (for the
    bead).

- **Initiative** — an opt-in container above the spine that groups several
  Signal → Chart → Spec → Checkpoint chains, so one large effort spans several
  Checkpoints. Its membership is the effort's workstream tag.
  - _Owner:_ Luffy (steward, creates it; handler, decides on drain whether a
    next phase opens or the Initiative is done). Robin may also create one
    (bodyless, a Draft, on the human's yes) besides Luffy and the tab (ADR 0381).
  - _Structure:_ container (parents every bead of each member chain); it has
    no branch and no land, since each member Checkpoint lands on its own
  - _Operations:_ **membership** covers the whole chain: a member Signal's
    successors join the Initiative as each is born. **Adoption**: a human pulls
    an existing chain in by its unclosed head; its closed predecessors stay
    where they are, as lineage. Moving a chain between Initiatives is a release,
    then an adoption. **Phases** enter one at a time, each as one or more
    member Signals (one per touched repo context, at most one per context)
    (ADR 0252); the next is cut only once the Initiative is **drained** (no
    unclosed member), against the **Done when** finish line written at its
    triage. A **pre-bound member** carries `context:<name>` from its filing
    and its triage keeps it; an unlabelled member's context is undecided, not
    Workspace-wide.
    **Cancelling** an Initiative closes every unclosed member chain and the
    Initiative itself, and landed work stays. **Withdrawing** a member chain
    closes its unclosed head and leaves the Initiative going. Neither runs
    while a member effort is in flight, and withdraw also waits until no
    unclosed member is ordered after the chain, except an Errand, which the
    withdraw sweeps instead (ADR 0342).
    An Initiative ends in one of two closures, each with its reason: **done**
    (how the Done when finish line holds), on the human's explicit done at a
    drained Initiative's cut; or **cancelled** (why), a human drop, whether of a
    whole Initiative with members or of a bodyless one. There is no obsolete,
    superseded or rejected outcome: the cancel's why carries those cases. A
    rejected idea is rejected at Signal triage, before any Initiative; an
    Initiative replaced by another releases its chains, the other adopts them,
    and the replaced one is cancelled.
    A **parked member** is a later-phase Signal closed with a `later:` reason
    and kept under the Initiative; it never holds the drain (ADR 0255).
  - _Lifecycle:_ derived, first match wins: **closed** → **drained** (no
    unclosed member, Drafts included) → **under way**. A drained Initiative
    waits for the human: done (closed for good) or a next phase.
  - _Bodyless:_ an open Initiative whose body lacks `## Done when`. A render-time
    qualifier, not a lifecycle state; its cut session settles the body first
    (ADR 0182).
  - _Notes:_ A bead belongs to at most one Initiative. Its members include the
    Errands and the offshoots filed against a member: every Errand joins the
    Initiative of the member it is ordered against, and an offshoot joins its
    cause's Initiative whatever its order unless its filer opts out with `--outside`,
    while repair Signals, a Latch and a Pivot stay outside and join only by
    adoption (ADR 0375, ADR 0352).
    Single-Checkpoint work never needs one, unless the Workspace sets Require
    Initiatives. A manual pause on
    it pauses every member (ADR 0081). It carries no defog of its own: a
    question shared across phases is defogged on the first phase's Chart that
    needs it, and its doc is pointed to from the Initiative's Notes. See ADR 0128, ADR 0132, ADR 0133, ADR 0134, ADR 0135, ADR 0252.
    _Avoid:_ epic (bd's type), program, outcome, workstream; "abandon" (that is
    an abandoned effort) for cancel or withdraw.

- **Require Initiatives** — a shared, opt-in Workspace setting
  (`[initiatives] required = true` in the checked-in `.mugicrew/config.toml`):
  every spine bead below intake belongs to an Initiative, enforced at triage,
  `mugicrew surgery new` and release. Off by default; work already outside an
  Initiative is grandfathered. See ADR 0224, ADR 0220, ADR 0221.
  _Avoid:_ mandatory Initiatives.

- **Enforce external ID** — a shared, opt-in Workspace setting
  (`[initiatives] external_ref = true`, usable only with Require Initiatives):
  each Initiative born while it is on carries a non-empty, unique bd external
  ref. See ADR 0224. _Avoid:_ Jira ID.

"Fog" is the collective term for a Chart's defog beads (Grilling/Research/
Prototype); it is not itself a kind — the grouping is the Chart parent.

The **landed base** is the single commit a container branch produces when it
lands on mainline (a rebase+squash commit). It is the seam between the spine's
branches: the Chart's landed base is where the Checkpoint is cut from. It is a
derived git fact — recovered from the land commit's `Chart:`/`Checkpoint:`
trailer, not stored on a bead (ADR 0010) except for a zero-commit Chart. A
**zero-commit Chart** (its `chart/<id>` head already an ancestor of mainline) writes no land commit under
any land strategy: its land records an `Anchor: <sha>` pin on the Chart bead, set
if unset, and Zoro cuts from that pin before any `Chart:` trailer (ADR 0358).

A **direct Spec** (ADR 0035; historically headless, ADR 0012) has no
container-branch land, so it carries a **Spec anchor** instead: an
`Anchor: <sha>` pin on the Spec bead, set if unset by the fast-lane triage
handler to freeze the Spec's birth point (ADR 0287). Under `land_strategy=local`
the pin is the mainline tip at triage; under gitlab it is the fetched Forge tip
(ADR 0212). It is a recorded birth point, not a landed base. Zoro cuts
`checkpoint/<id>` from whichever base applies: the `Chart:` landed base, or the
pin. A **legacy Spec** anchored by an empty `Spec: <spec-id>` commit still
resolves through its trailer; a Spec with neither pin nor trailer is a hard
error, never a `rev-parse main` fallback (ADR 0014 §3).

## Bead relationships

Named domain relationships, each on one bd edge (ADR 0002):

- **Succession** — same unit of work advancing a stage (the spine). Predecessor
  closes, kept as lineage, and never before its successor is visible (published):
  a predecessor is never left closed with no successor.
- **Membership** — a container holds a member (Chart→defog beads and Landing,
  Checkpoint→Build/Review/Critique/Approval/Landing, Initiative→every bead of its
  member chains).
- **Drained** — a container (Chart, Checkpoint, Initiative) that is still
  open but has no unclosed member; Drafts count as unclosed. A drained
  container is routed to its handler.
  _Avoid:_ "draining" (a loop state), "empty", "done".
- **Effective priority** — the level a bead dispatches at and the TUI badge
  leads with: the most urgent (lowest P-number) of its own priority and that of
  every open container ancestor (Checkpoint, Chart, Initiative). Derived, never
  stored, and distinct from the bead's **own** priority, the bd field `+`/`-`
  edits. Own wins a tie; the nearest ancestor at the effective level is its
  source (ADR 0410, `bd.EffectivePriority`).
  _Avoid:_ "inherited priority", "container priority" (a container's own field).
- **Offshoot** — new work discovered _mid-work_, distinct from succession.
  Its **order** with the work it was found from is one of: a **follow-up**
  (it waits for the cause's container to land), a **prerequisite** (the cause's
  container waits for it), or none (ADR 0267).
  _Avoid:_ "dependency" or "blocker" for the order.
- **Obsoletion** — a bead proven wrong or stale and replaced; the old bead is
  closed.
- **Ordering** — the only relationship that gates the frontier (`bd ready`).
- **Carry** — when a Signal, Chart or Spec closes in **succession**, the beads it
  _orders_ (blocks) become ordered by its successor, so a dependent waits for
  the real work, not the stage that described it. Outgoing ordering only,
  except that an open Latch blocking it also comes to block its successor,
  unless that successor is an Initiative (ADR 0223, ADR 0348); the old edge
  stays as lineage. A Spec's dependents therefore wait for its Checkpoint to
  **land**. A Checkpoint obsoleted by a confirmed or escalated **pivot** carries
  the same way, to the corrected Spec or the escalated Chart.
  A bead closed as a duplicate carries the same way, onto its original's live head
  (the original while unclosed, else the first unclosed bead along its succession);
  when that chain has ended, its dependents are released and the event records them
  skipped `ended`.
  _Avoid:_ "moving" edges (nothing is removed).

## Ownership roles

- **Routing owner** — whose queue an open **leaf** bead lands in. A function of
  kind; nothing is stored.
- **Steward** — the crewmate that _creates_ a **container**. It is not routed to
  while the container still has open children.
- **Handler** — the crewmate a **drained** container (open, all children closed)
  is routed to: `chart` → Nami, `checkpoint` → Jinbe. Owner-from-kind extended to
  container kinds. Steward (creates) and handler (drains) may differ — Zoro
  stewards the Checkpoint, Jinbe handles it by spawning its Landing and closing
  it once the Landing has landed. See ADR 0003.
- **Operator** — anyone who acts on a bead. Equals the owner for every leaf;
  diverges only on the Checkpoint (Zoro creates it, the tail crew operate on it
  via their own beads, Jinbe's close after the Landing closes it).

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
  carries the bead id as an exact argv token or the dispatcher's per-effort lock
  is held, else _dead_ (ADR 0025, 0154). Derived on
  demand, never written back; the claim age (`started_at`) is shown beside it but
  is never a liveness signal. HITL claims get no verdict.
- **Live effort** — a bead whose row shows the live `●`: an AFK effort whose
  Effort liveness is _alive_, or a detached session that is _alive_ (waiting
  and done included). Exited and dead ones are not live. The tab bar's and a
  folded header's `N live` count these (ADR 0038 §2, ADR 0175).
  _Avoid:_ "active" for this.
- **Effort liveness view** — the read-model over those verdicts (`mugicrew ps`,
  and the TUI's liveness dot beside the status glyph, ADR 0032 §5; the dot slot of the **Standard bead row notation (SBRN)**): observational, not coordination state —
  `loop.LiveAFK` feeds contention (ADR 0054) but nothing is persisted (ADR 0031/0032).
- **Standard bead row notation (SBRN)** — the canonical presentation of a bead as a
  list item or heading: `<status>[<dot>] <kind> <priority> · <title> · <id>`. The
  status glyph is always there; the dot slot (the HITL session look, else the
  Effort liveness view's ● / ○, else a dim `·`) belongs only to live-tracking
  views (Ideate, Defog, Execute, Graph, the Initiative drill-in) and is dropped
  entirely, placeholder included, everywhere else. A row's extras follow the id
  behind `·`. A bead named inside prose is not presented, so not SBRN.
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
  it; not Session cost, and not mugicrew's own share of it. Each good read the
  TUI takes is logged as a `usage.read` event (ADR 0260).
  _Avoid:_ bare "usage", "quota", "budget".
- **Abandoned effort** — an in-flight effort the loop gave up on during a
  force stop, or a run-now effort whose host gave up on a second signal: its
  bookkeeping ended without releasing its claim (`effort.abandoned`, ADR 0029).
  The work itself may still finish.
  _Avoid:_ calling it a failed or ended effort, which releases its claim (ADR 0034).
  _History_: a confirmed TUI quit also abandoned run-now efforts before the
  run-now host (ADR 0330) took them out of the TUI.
- **Run-now host** — the detached `mugicrew run <id>` process that carries one
  run-now (`t`) AFK effort, so the effort outlives the TUI. The TUI claims,
  spawns it and hands over the effort flock by fd; the host dispatches, lifts the
  failing and foreign-hold pauses, releases, and exits. It absorbs a first
  SIGTERM/SIGINT and abandons its effort on a second (ADR 0330, ADR 0053 is the
  precedent). It still ignores the Plan usage limit, as run-now did in the TUI.
  _Avoid:_ "run-now goroutine"; calling it the loop.
- **Aborted effort** — an in-flight AFK effort the human stopped with
  `mugicrew abort` or TUI `x`: its bead is paused _manual_ first, then its
  processes are terminated, and a live host cleans up as for a failure (ADR 0083).
  _Avoid:_ calling it abandoned, which leaves its claim and tree behind.
- **Withdraw** — the human dropping one unfinished chain, one Errand, one Latch or one defog leaf, inside an
  Initiative or not: for a chain or an Errand, its unclosed head and every
  unclosed bead under it close with reason `withdrawn` and no successor, and
  their effort worktrees and branches go with them. A chain's withdraw also
  **sweeps** the Errands ordered after what it closes, closing them with reason
  `withdrawn: <head>`, and **before-closes** the Errands ordered before what
  it closes the same way, unless one still holds a bead outside it, which is
  **left open** (ADR 0342, ADR 0351). Dropping a whole Initiative is **cancel**
  (ADR 0310). A withdrawn Latch closes alone with reason `withdrawn`: the
  beads it blocked stay open and are released, and its `blocks` edges stay
  (ADR 0389). A withdrawn defog leaf (grilling, research or prototype) likewise
  closes alone with `withdrawn`, sweeping nothing, and is refused for an
  escalation grilling while its chart-conflict is open (ADR 0417).
  _Avoid:_ "abandon" (an abandoned effort), "cancel" for one chain, "reject"
  (a triage verdict on a Signal), "swept" for a before-closed Errand (swept is the
  Errands after).
- **Mouse mode** — the opt-in capture of the mouse in the main TUI, held in the
  layered `[tui] mouse` setting (machine-local override, then the checked-in
  `.mugicrew/config.toml`, then built-in off) and edited from the Settings tab's
  Mouse mode row, which switches capture live; the TUI's own tmux session
  follows the setting (its session-scoped `mouse` option, set at start and on
  every switch). While it is on the terminal's own
  text selection gives way to Shift-drag (Option/Alt on some terminals). A bead
  row's right-click menu offers `+ raise prio` and `- lower prio` in place of
  `+/- prio`; running one steps the priority and leaves the menu open, at the
  same place and on the same bead, so it can be clicked again, and an item at
  its bound (no raise at P0, no lower at P4) drops out (ADR 0412).
  While it is on, the Settings tab's selected row draws a `◀ value ▶` stepper
  around its value (the pool, Auto-exit, Auto-approve, Mouse mode, Theme and
  each Kind profile row; not the read-only Initiatives row). A press on an arrow
  stages one step through the normal key path, as `-`/`+` do; nothing is
  saved. An arrow at a bound is a blank, inert cell, and a double-click is two
  steps, never enter (ADR 0412).
  A staged row's gloss line starts, two columns in, with `✓ save  ✗ discard`
  (a staged Kind profile row, which has no gloss line, gains one extra line under
  it, after any unrunnable reason, carrying them). `✓ save` sends enter and
  `✗ discard` sends esc through the normal key path; only the drawn labels act,
  the rest of the gloss line still selects its row. Saving Mouse mode off by a
  click ends capture at once (ADR 0412).
  In a Signal or Initiative form the focused priority field draws `◀ P2 ▶` the
  same way: ◀ steps toward P0, ▶ toward P4, an arrow at a bound is hidden and
  inert, a step keeps focus, and a click on the unfocused field only focuses it
  (ADR 0412). The wheel over the arrows never edits.
  _Avoid:_ "mouse support" (it is a setting a human turns on, not a capability).
- **Theme** — the layered `[tui] theme` setting (machine-local override, then the
  checked-in `.mugicrew/config.toml`, then built-in dark) that picks one of three
  presets, `dark`, `light` and `high-contrast`, for the main TUI and the F10
  bead-detail pane. It is previewed live from the Settings tab's Theme row. An
  unknown value reads dark. _Avoid:_ "skin" and "color scheme" for the setting, and
  "palette" except for a preset's role values.
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
  it (ADR 0099); in the TUI an armed Auto-exit and Closing do too, as Done
  does. _Avoid:_ "idle" (a quiet pane is not necessarily waiting) and
  "pending".
- **Finished (HITL session)** — the session exited cleanly: its Drafts are
  published and it is gone from the session list. The human finishes a session
  with `/exit`, and F6 only goes back to the TUI (ADR 0067). _Avoid:_ "detached"
  for a session that has ended. No human-facing label says "detached".
- **Reattach** — joining an _alive_ detached session's live process again; the
  conversation and any running tool are as they were left. The human's terminal
  moves from their TUI to the session, and F6 moves it back (ADR 0065).
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
- **Orphaned claim** — an AFK bead's claim (`in_progress`) that no live host
  holds: no dispatcher holds it, no process carries it, and no process runs
  inside its tree. It takes no AFK pool slot, and only the human's recover
  repairs it (ADR 0391).
  _Avoid:_ "abandoned" (the pause a forced loop stop writes), "stranded" for the
  claim alone.
- **Effort branch** — the branch a leaf bead's work happens on, `effort/<bead-id>`,
  cut from its container's branch. A container's branch is never checked out;
  work reaches it only by integrate (ADR 0102).
  _Avoid:_ "private branch", "feature branch".
- **Integrate** — moving an effort branch's work onto its container's branch
  as one squash commit, before the bead closes. Distinct from _land_, which
  moves a container onto mainline through a Landing (ADR 0102, ADR 0147).
  _Avoid:_ "land" or "merge" for this step.
- **Land** — moving a container's branch (a Chart's or a Checkpoint's) onto
  mainline as one commit, its landed base, through a Landing. Only containers
  land; an effort branch integrates.
  _Avoid:_ "land" for pushing to a side ref or committing a doc; "merge".
- **Mainline** — the Workspace's trunk branch (`main`). Lands advance it and
  container branches are cut from it; no crewmate commits to it directly.
  _Avoid:_ "trunk", "master".
- **Held** — integrate's refusal when the container branch it would move is
  checked out in another worktree: the container is left untouched and the bead
  stays open until the branch is free.
  _Avoid:_ confusing it with a foreign hold (the effort branch) or a pause (a
  bead the loop won't dispatch).
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
  run-now lifts it), _abandoned_ (the loop or a run-now host abandoned its effort, whose
  orphaned process may still be running; only the human lifts it),
  _integrate-conflict_ (the backstop couldn't integrate its effort branch;
  only a successful integrate or the human lifts it), _not-built_ (the
  backstop integrated a Build but found no integrate trailer for it on its
  Checkpoint; a run-now re-run that integrates or the human lifts it, ADR
  0248), _unmet_ (Brook wrote it on a Latch whose watched MR is closed
  unmerged or missing; a human unpause, or a run-now that reads the MR open
  or merged, lifts it, ADR 0389), _profile_ (its kind
  resolves to an unrunnable Agent Profile; the loop paused it before any claim
  and lifts it once the profile runs again), _checkup_ (Chopper's fence on a
  Checkup's target while the Checkup is open; the session lifts it last, and
  recover lifts it once the session is gone, ADR 0272), _recovering_ (Chopper's
  fence while it repairs a stranded bead; left behind only by a repair that
  failed partway, and lifted by re-running it, ADR 0127) or _manual_
  (the human paused it, and only the human lifts it). A bead can carry several causes at once; it stays paused until every
  cause is lifted, and each lifter removes only its own (ADR 0070, ADR 0078, ADR 0082, ADR 0104, ADR 0123, ADR 0127, ADR 0248, ADR 0389). A pause never stops an effort already running (ADR 0055).
  A _manual_ pause on a Chart or epic also pauses every member beneath it
  (_inherited pause_); loop-written causes pause only their own bead (ADR 0081).
  _Avoid:_ "held" (that is a branch's foreign hold), "suppressed", "deferred".
- **Verdict** — the eligibility module's answer for one bead in one mode (loop or
  run-now): runnable with an action (`run`, `spec`, `land`, `retry`, `retry hold`,
  loop-only `review`), or not runnable with one **cause** (ADR 0378).
  _Avoid:_ "decision", "eligibility result".
- **Cause** (of a verdict) — the first leg that fails, from a closed list in a
  fixed order (`not-candidate`, `paused`, `inherited-pause`, `container-frozen`,
  `blocked`, `paced`, then the arm's legs). A `paused` verdict carries the
  bead's top pause cause verbatim (ADR 0378).
  _Avoid:_ "reason" for the cause itself (the reason is the cause plus its holders).
- **Holder** (of a verdict) — a bead that keeps the verdict's cause standing,
  carried whole, with its effort branch when it is a held bead. A foreign landing's
  holder is the foreign sha (ADR 0378).
  _Avoid:_ "blocker" unless it is a blocks edge.
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

Every bead is `open → closed`; there is no stored intermediate state. Where an
open Checkpoint is in its life is **derived**, never stored: its **Rollup**. The
skeleton (Checkpoint epic + Build beads) is laid atomically, so a Checkpoint is
born already **cut**; Review/Critique/Approval beads are spawned on demand.

- **Unclosed** — a bead in any status except `closed` (open, in_progress,
  blocked, deferred, …). It is the default status facet of the Graph (ADR 0175).
  _Avoid:_ "live" or "active" (those words mean Effort liveness), and "open"
  (that is one specific status).

- **Rollup** — an open Checkpoint's one status word for where it is in its land
  cycle, derived from its members, blockers and Landing by a waterfall (first
  match wins): landed, land failed, landing, conflict, blocked on <Kind>,
  fixing, awaiting approval, in review, in build, approved, review due, cut.
  Qualifiers (round N, re-review, stale pin, under gitlab `!<iid>`, `<context>!<iid>` on a nested
  Landing, the MR sub-state, forge review, retrying) ride beside it, never in it
  (ADR 0198, ADR 0226, ADR 0230, ADR 0320). _Avoid:_ branch-state, branch-state chip, status.
- **Chart status word** — an open Chart's one status word for where it is on its
  way to a Spec, derived by a waterfall (first match wins): speccing, landed,
  land failed, landing, conflict, blocked on <Kind>, failing/paused,
  charting/survey due, in defog session/your turn, defogging/defog queued, fog
  blocked on <Kind>, drafts pending, integrate due, judging, judge due, landing
  due. It reuses Rollup spellings for shared meanings. Qualifiers (round N,
  re-land, forge review, retrying) ride beside it (ADR 0204, ADR 0320). _Avoid:_ Chart rollup, Chart
  stage, status.
