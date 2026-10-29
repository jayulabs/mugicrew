# ADR 0397: A renumber-only head is the approved squash replayed on the new base, modulo its own renumber

Status: Accepted
Date: 2026-10-29

Chart "A container's pre-land renumber no longer loses the race to mainline's next numbered file" settled that a head that differs from the last approved head only by a renumber carries that Approval: no Review is spawned, and Go only verifies. This ADR fixes the verification rule. It was measured by a throwaway verifier (side ref `refs/mugicrew/prototypes/mc-6uc6p`) over the real pins of Checkpoint "Typed bead-level bd core with a sealing bdtest fake and contract suite, with dispatch migrated off its bd vars", which renumbered one ADR 0381 → 0385 → 0388 → 0390 → 0392 → 0394 → 0395. The verifier was also run against 14 negative controls. The full Review/Approval path that ADR 0225 (mugicrew-a-forge-merged-number-clash…) §4 keeps for a wrong sweep stays the fallback for every case this rule does not accept.

## Decision

Given the last approved pin `old` (on base `oldBase`) and the new head `new` (on base `newBase`), each base taken as `merge-base(mainline, pin)`:

1. **Pair the numbered files.** Each sequence's files that `old`'s squash adds must match the files `new`'s squash adds one to one, by the same directory and the same rest of the name (the slug). Only the captured number may differ, at the rule's width. This yields a map from old number to new number. Two different old numbers mapped to the same new number, or the reverse, is a reject. So is any change of slug or directory, or any added or dropped numbered file.
2. **Replay the approval.** `git merge-tree --write-tree --merge-base=oldBase newBase old` must merge clean. Its tree `T` is what the approved change becomes on the new base. A conflict is a reject: the hand resolution is new content that needs a Review.
3. **Compare whole files, in order.** Every file in which `T` and `new` differ is compared line by line, with renamed files paired by their old path in `T`. Both sides must have the same number of lines. Each differing pair must satisfy `new line == subst(T line)`, where `subst` rewrites each old-number token to its new number. A token is the number at the file rule's padded width (`0394`), standing alone: no letter or digit immediately before or after it. The unpadded form (`394`) is never substituted.
4. **Substitute only the approval's own lines.** A line may differ through `subst` only if `old`'s squash added that exact line to that file. A line that came from mainline must arrive unchanged: there, a number means mainline's file.
5. **The sweep must be complete.** Every line `old`'s squash added that holds an old-number token must arrive as its substitution. A line that keeps the old number is a reject, because that number now names mainline's file.
6. **Anything else is a reject**, and a reject falls back to a full Review and Approval, never to a partial carry.

Measured outcome: all three real Approval-to-Approval laps were accepted (0390 → 0392, 0392 → 0394, 0394 → 0395). Every negative was rejected. The negatives were:
- a content edit, in a branch file, in a shared file the branch never touched (`GLOSSARY.md`), and in a shared file both sides edit (`bd/bd.go`);
- a renumber plus a changed reference to a different number, or a reference retargeted at the new number;
- a slug change and a directory change;
- an added line moved within its function;
- a sweep that rewrote mainline's own 0394;
- a sweep that missed the ADR's title;
- a Go literal `394 → 395`;
- three real non-approved pins: a hand-resolved rebase conflict, the Serializer test fix, and Review-era building.

## Considered Options

Each option below fails at least one measured case, so the rule above is the smallest that is safe on this history.

- **Compare the two `-U0` squash diffs line by line, hunk headers kept** (the proposal). Rejected: it rejects lap 0392 → 0394. Mainline's edits to `bd/bd.go` shifted 26 `@@` line numbers while every `+`/`-` line stayed the same. Base drift sinks it on real laps.
- **Drop `@@` line numbers, as `git patch-id` and a Chart's `Patch-id:` do.** Rejected: it accepts an added line moved one line up in the same function. It is fine for "content unchanged" on an un-drafted MR, but not as authority to skip a Review.
- **Replay without point 4.** Rejected: it accepts a sweep that rewrites mainline's own ADR 0394 title to 0395, the exact wrong-sweep error ADR 0225 §4 guards against.
- **Replay without point 5.** Rejected: it accepts a sweep that leaves the ADR's own title at the old number, which now points at mainline's file.
- **Also substitute unpadded tokens.** Rejected: it accepts a Go literal `394 → 395`. No real lap needed an unpadded substitution, so dropping it only costs a Review when a branch cites its own file unpadded.
- **Compare per file only where the base did not touch the file.** Not needed: the replay absorbs the drift, and a base edit that collides with the approved lines surfaces as a merge conflict, which point 2 rejects.

## Consequences

- A pre-land lap where mainline absorbed one of the branch's lines, or where the rebase needed a hand resolution, gets a full Review. Both happened on this Checkpoint before its first Approval. Both are correct rejects.
- The check needs only git (`merge-tree --write-tree --merge-base`, git ≥ 2.40) and the configured sequence rules (ADR 0219 (mugicrew-the-adr-number-guard…)). It reads no bead other than the Approval's `Commit:` pin, and it never writes the tree.
