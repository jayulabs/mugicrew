# A container rewrite leaves a pending GitLab Landing's Merge Request open and mergeable at its approved pin

Under `land_strategy=gitlab`, a requested rebase rewrites the container through `IntegrateReplace`, and so does the resolution of a Conflict or Chart-conflict. Either way, a rewrite makes the pending Landing close `obsolete: branch advanced without landing`, but the Forge branch and the open MR stay at the old `Commit:` pin with the full trailer block. A human merge, or auto-merge a human armed, can still land that pin until the next Landing force-pushes and reuses the MR. We accept this window and make **no Forge write** on an obsolete close, which keeps "an obsolete Landing makes no Forge write" (`jinbe/jinbe.go`) and ADR 0189 (under-gitlab-a-human-merges-the-merge-request…) §1 unchanged. The old pin was Approved, so landing it is an authorized land, and the stale-pin check of ADR 0185 (an-open-merge-request-outlives-its-landings…) §6 already handles it. Conflict's resolution has lived with the same window since ADR 0189 §4. Settled by grilling (bead "How a requested rebase treats a pending GitLab Landing's open Merge Request").

- **Filing warns, never refuses.** When the container has an open MR, `mugicrew rebase <container>` and the New menu's `r` row name the MR's URL and say it stays mergeable at the old pin until the next Landing. They file the rebase anyway. This holds for Charts and Checkpoints alike. If the MR lookup fails (Forge unreachable, 429), they still file and say they could not check for an open MR. No auto-merge field is decoded.

## Considered options

- **Mark the MR Draft on the obsolete close.** GitLab won't merge a Draft MR, and the next Landing's `SetMR` would clear it. Rejected: it adds a Forge write to every obsolete close to guard a land that a human already approved.
- **Strip the trailer from the description.** Rejected: a merge still happens, and it turns into a trailerless-merge Task.
- **The rebase force-pushes the rewritten head to the Forge branch.** Rejected: the open MR would then carry a head that was never Reviewed or Approved, which bypasses the gate.
- **Refuse filing while an MR is open, or warn only when auto-merge is armed.** Rejected: the human filing the rebase is the human who can disarm or close the MR, and the armed-only check needs a field `forge.MR` doesn't decode.
