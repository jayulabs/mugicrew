# An offshoot Signal is filed through `signal offshoot`, which orders it against its cause's container or files it unordered with the reason

Amends: crew doc signal-filing.md's Offshoot form ("The `discovered-from` edge, set on the create, is its only edge").

An offshoot Signal had `discovered-from` as its only edge, so it recorded no order with the work it was found from. The Chart "Offshoot Signals record a blocking order with the work they were discovered from" settled that the order targets the cause's container (its Checkpoint, or its Chart for a defog cause), never the cause bead, and that a mugicrew verb records it, not a hand-written `--deps` in each prompt (docs/research/offshoot-blocking-edge-bd-mechanics.md, docs/research/offshoot-signal-filing-sites-and-cause-containers.md). The grilling "Surface of the offshoot-ordering verb" settled the surface:

- **The verb files the whole offshoot.** `mugicrew signal offshoot --from <cause> --drafted-by <bead> --order follow-up|prerequisite|none --title T --description D`. It resolves the container first, then runs one atomic `bd create --deps discovered-from:<cause>,blocked-by:<container>` (follow-up) or `…,blocks:<container>` (prerequisite), and prints the new id. A refused pair therefore leaves no bead behind, and the edge never rides the ADR 0059 publish batch, where a refused pair would roll back the whole publish.
- **`--order` is required.** The crewmate must choose. `none` is the answer for separable work with no order. The reason for the choice goes in the description (a HITL session puts the order in its chat proposal), not in a flag. `--drafted-by` is required too: every caller is a crewmate session filing a Draft.
- **The container is the cause's parent when that parent is a Checkpoint or a Chart.** Otherwise there is no container (Task, Discussion, triage split, Initiative parent).
- **Skip, never fail.** No container, a closed container, or a pair bd refuses (`cycle`, `type-clash`, `hierarchy`, in `carrySkipReason`'s words) files the Signal unordered. The verb exits 0, prints the reason, and the body says why. Only a real failure (an unknown cause, a failed create) exits non-zero.
- **Every filing ends its body with one `Order:` line**, citing titles, not ids: `Order: follow-up of <container title>`, `Order: prerequisite of <container title>`, `Order: none`, or `Order: unordered (<reason>)`, where the reason is `<cause title> has no container`, `<container title> is closed`, or the refused pair's word. The verb writes no eventlog event (whether the human needs more than this line is still open on the Chart).
- **The Go-side filers stay unordered.** `nami.FlagStalePin` and the stranded-commits backstop always file against a container that has already landed, so they keep `SpawnOffshootSignal…IfAbsent`. Because the verb therefore never meets a container that has landed but is still status-open, a plain `closed` status check is enough for its skip.
- **A Survey's offshoot is discovered-from the Survey.** `charting.md` files it discovered-from the Survey, not the Chart. The parent rule then holds with no special case, and a follow-up no longer collides with a `discovered-from` edge on the same (offshoot, Chart) pair. The Initiative climb (Survey → Chart → Initiative) stays within its two levels.
- **One rule reaches every prompt filer.** signal-filing.md's Offshoot form becomes the verb. crew docs defog-session.md and chart-body.md point at it. The triage split (no container), research and chart-conflict (no filing site) are left as they are.

The replacement Offshoot form:

```
## Offshoot form

A Signal for work you found, not a member of any container:

    mugicrew signal offshoot --from <the bead that found it> --drafted-by <your bead id> \
      --order follow-up|prerequisite|none \
      --title "<the work, no kind prefix>" --description "<what and why; Sources: …>"

Pick the order and say why in the description:
- follow-up: the work waits until what you were working on lands;
- prerequisite: what you were working on can't land until this work lands;
- none: separable work with no order either way.

In a HITL session, include the order in your chat proposal. The verb finds the
container, records the order, and prints the new id. If the order can't apply
(no container, a closed one, or a pair bd refuses), it files the Signal
unordered and says why on stdout and in the body. Don't add edges yourself.
```

The Retry section applies to the verb's create unchanged. The Initiative member form keeps its raw `bd create --parent`.

Resolved by ADR 0268 (a-chart-level-prerequisite-gates-the-whole-chart…) and ADR 0269 (…checkpoint-level-prerequisite-holds-only-the-land). Was open: a prerequisite on a Checkpoint or Chart freezes the whole container in `bd ready` (`Readmit` re-admits only `kind:task` blockers), and whether a Chart-level prerequisite carries to its Spec. The Spec settles these with the Chart's other defog beads.

## Considered options

- **A verb that only orders an already-filed Draft (`signal order`).** Rejected: it takes two steps, a forgotten second step leaves the offshoot silently unordered, and a refused `dep add` leaves a half-filed state.
- **Optional `--follow-up` / `--prerequisite` booleans, where neither means unordered.** Rejected: the default would let a crewmate skip the choice.
- **Fail on a refused pair.** Rejected: the work is still worth filing. Skipping matches the no-container and closed-container rule.
- **Route the Go filers through the verb.** Rejected: their container has always landed, so they would always skip, and they would need a "landed" check that no single bd read gives.
- **Keep the Survey's provenance on the Chart and special-case "the cause is its own container".** Rejected in favour of one parent rule.
