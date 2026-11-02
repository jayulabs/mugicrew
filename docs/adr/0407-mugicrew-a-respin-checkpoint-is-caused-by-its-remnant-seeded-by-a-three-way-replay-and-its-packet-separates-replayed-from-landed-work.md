# ADR 0407: A respin Checkpoint is caused by its Remnant, seeded by a three-way replay, and its Packet separates replayed from landed work

Status: Accepted
Date: 2026-10-29

Chart "A Forge merge that strands in-flight Checkpoint work asks the human how to recover it" gives a Remnant three exits. The respin exit cuts a new Checkpoint for the landed Checkpoint's original Spec from fresh mainline, and replays the stranded `Tip..head` onto it (`Tip` is the landed Checkpoint's approved pin, which the Forge merge landed). ADR 0402 (a-remnant-s-respin-carries-builds-and-critiques…) fixed which stranded children it re-files. This ADR fixes how the respin is linked to the Spec and the landed Checkpoint, how its replay seeds and conflicts, and what its Decision Packet says. It follows the research doc `docs/research/respin-checkpoint-cut-replay-and-cross-container-integrate.md`.

## Decision

1. **Lineage reuses the Rollback-successor shape** (ADR 0048 (mugicrew-rollback-successor)).
   - The respin is `caused-by` its Remnant, spawned idempotently per Remnant (`SpawnSuccessorIfAbsent(remnant, checkpoint)`).
   - The Remnant is `related` to the landed Checkpoint.
   - `bd.SpecOf` gains a Remnant leg next to its Rollback leg: respin → Remnant → related landed Checkpoint → its Spec. Every walk that copies `SpecOf` gains the same leg.
   - There is no direct edge from the respin to the landed Checkpoint. It is never `caused-by` the landed Checkpoint, because that is how `RenumberPred` recognises a Renumber Checkpoint. It is never `supersedes` the landed Checkpoint either, since a landed Checkpoint is history.
2. **Title and body.**
   - The title is `Respin of <landed Checkpoint's title>`, by title rather than by id.
   - The body carries one fact per line:
     - `Respin of <landed id>, which a Forge merge landed at <landed squash sha>; Remnant <remnant id>.`
     - `Spec: <spec id>`
     - `Landed-tip: <Tip>`
     - `Stranded: <Tip>..<old head>`, or for a branch rewritten after its pin, the note that `range-diff Tip...head` shows it.
   - It never carries `Base:`/`Tip:` lines, which mean replace mode to `route`.
3. **Initiative.** The respin joins the landed Checkpoint's Initiative while that Initiative is unclosed, as the Rollback successor does.
4. **The replay runs in Go at cut time, and the human sees a dry-run first.**
   - **Dry-run.** In the Remnant's HITL session, Zoro runs a read-only `git merge-tree --write-tree --merge-base=<Tip> <mainline> <old head>`. It shows the conflicting paths, or "replays clean", next to its recommended exit.
   - **Clean replay.** On the human's yes, `mugicrew remnant respin` cuts `checkpoint/<new>` at mainline plus one seed commit. The seed's subject is `replay: stranded work of <landed title>`, with trailers `Replayed: <Tip>..<old head>` and `Remnant: <id>`. The re-filed members start unblocked.
   - **Conflicting replay.** The cut is at mainline with no seed, and a new **`replay` Conflict variant** is filed as a member, pinned `Base: <mainline>` and `Tip: <old head>`, with the range start `<Tip>` in its body. Franky resolves it AFK: his leaf runs the same three-way merge-tree, not `rebase <Base>`, resolves the markers, commits one seed commit with the same trailers, and integrates it as Replace onto the empty cut. The re-filed members are blocked by this Conflict. The command prints the Conflict's title, and the Remnant closes.
   - **Why a three-way merge.** It needs no ancestry between `Tip` and the head, so the same call covers a branch rewritten after its pin (ADR 0263 (a-container-rewrite-leaves-a-pending-gitlab-landing…)).
5. **The Decision Packet separates replayed work from landed work.** The branch is cut from mainline, so the diff already leaves out what landed. The Packet says so, and tags where each change came from (crew doc approval-body.md, "A respin's Packet"):
   - **Background** says that the Spec's work up to `Landed-tip` is already on mainline.
   - **Summary** tags each row **replayed** (from the seed commit) or **respin** (built by a member).
   - **Execution Notes** gives the replay's outcome and the re-filed members.
   - **Approval History** ends with the landed Checkpoint's last approved pin, and says that nothing after it was approved.

## Considered Options

- **Seed in a leaf.** An empty cut, plus a seed Build that adopts the old head as its effort, with its cut ref at `Tip`, so `integrate` replays it through `rebase --onto`. Rejected: on a branch rewritten after its pin, the rebased copies of `Tip`'s commits replay a second time, and the human only sees a conflict after choosing.
- **Resolve the replay conflict live in the Remnant session.** Rejected: the HITL session authorizes an exit, and conflict resolution is AFK crew work.
- **A direct `related` edge from the respin to the landed Checkpoint, or `supersedes`.** Rejected: the body line already serves human readers, and ADR 0048 already rejected `supersedes` for a Checkpoint that landed.

## Consequences

- `route` needs a `replay` arm, and Franky needs a leaf prompt that runs the three-way merge-tree rather than `rebase <Base>`. The Conflict variants grow to `rebase-conflict`, `number-clash`, `requested`, `replay`.
- `bd.SpecOf`, `bd/lineage.go`'s copy of it, and any other Spec walk (the stats spine, the TUI's `specOfCP`) need the Remnant leg.
- What happens when the same land also mints a Renumber Checkpoint (ADR 0225) is left to its own decision.
