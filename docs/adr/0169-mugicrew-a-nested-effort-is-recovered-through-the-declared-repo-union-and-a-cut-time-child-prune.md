# A nested effort is recovered through the declared-repo union and a cut-time child prune

Status: accepted

A nested-repo effort's host tree is detached at mainline, and `effort/<id>` is
checked out in the child repo (ADR 0166). So chopper, `DropOrphan` and the TUI/v2
orphan confirm, which all look for the holder in the cwd repo only, miss it. A
crash between the child-first teardown's two removes also leaves a prunable entry
in the child that holds `effort/<id>`, and any re-cut then fails. We find the
child through the **declared `repo =` dirs** (config), the same union as ADR
0167's `chopper ps/list` and ADR 0168's integrate probe, and we make the re-cut
heal itself.

- **The holder of `effort/<id>` is looked up across the declared repos.** One
  git/ helper checks the cwd repo first, then each declared `repo =` dir with
  `-C <workspace>/<dir>`, using `worktreeOn` and then `rebasingOn`. The host is
  the child's path with `/<dir>` stripped. chopper `Take`, the TUI and v2's
  orphan confirm all use it. With no `repo =` declared it adds 0 git calls, and
  every caller is human-triggered, so the per-tick cost stays 0 (ADR 0157).
- **`DropOrphan(wt)` drops child first, by config.** For each declared dir
  whose `<wt>/<dir>/.git` is a file, it runs `git -C <workspace>/<dir> worktree
  remove --force <wt>/<dir>`, then the host remove and the Workspace prune, then
  a prune in each child.
- **Recover keeps ADR 0127's fixed order, applied to the child.** It aborts a
  stopped rebase in `<host>/<dir>` and counts dirty files in both the child and
  the host (the host is blind to the ignored child), with `--discard` covering
  both. Then it drops the child, drops the host, prunes both repos, deletes the
  Drafts, unclaims and lifts. `recover.done` gains `repo=<dir>`.
- **A nested cut prunes the child before `worktree add`.** A stale prunable
  entry (the host dir is gone after a crash or reboot) never blocks a
  re-dispatch, whether chopper ran or not and whether the context drifted or
  not. This costs 1 git call per nested cut and 0 for a no-context Workspace.
- **Drift is not chased.** A child whose `repo =` was removed or edited
  (`paused:context`, ADR 0167) is not probed. Recover reports what it sees and
  tells the human to clear the context hold (revert `repo =` or relabel
  `repo:<dir>`) and re-run. The stamp is never used as an address.
- **`chopper ps/list` attributes a nested host to its bead.** A detached temp
  tree reached through a child on `effort/<id>` is owned, not listed as
  "unowned".
- **zoro plays no part.** Its sweep force-closes beads and never touches a tree.

## Considered Options

- **A `.git`-file probe under `<wt>`.** Rejected. A `repo =` can be deep, so the
  probe has to walk the tree. The cut-time prune already covers the undeclared
  child it would have found.
- **Resolve the repo from the bead's container.** Rejected. Leaves don't carry
  `context:`, so this needs a parent walk, and under drift it would turn the
  stamp into an address.
