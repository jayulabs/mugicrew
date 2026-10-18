# ADR 0168: A nested effort integrates from the host, and the land gate is a per-repo `gate =` key

Status: Accepted
Date: 2026-10-14

Chart mc-3hri.6 ("Nested-repo contexts"), grilling mc-3hri.6.7. The throwaway
behind ADR 0166 skipped two things. `mugicrew integrate` reads the bead off the
cwd's `effort/<id>` branch, but a nested effort's host tree is detached at
Workspace mainline while the child at `<host>/<dir>` holds `effort/<id>`. The
pre-land gate builds `#mugicrew`, which a foreign child does not have. nix fails
at eval, the failure is classed as infra, and so the Landing never lands.

## Decision

1. **Integrate runs from the host, and finds the child with a probe on the failure
   path.** When the cwd is detached and no rebase is in progress, `mugicrew
   integrate` reads the declared `repo =` contexts. It picks the one child under
   cwd that is on `effort/<id>` or mid-rebase on it, and integrates that child
   through a `git.Repo` under the Workspace land lock (ADR 0166). If more than
   one child matches, it exits 1. The verb still takes no arguments (Spec D3),
   and the prompt and the Go backstop are unchanged. A Workspace without
   contexts never reaches the probe, so it pays nothing.
2. **Integrate refuses to run from inside a child.** Run there, it exits 1 with
   "run mugicrew integrate from the Workspace tree". Otherwise the land lock and
   config would resolve against the child. Only a bead carrying a `context:`
   label is checked.
3. **The land gate is an optional `gate = "<attr>"` key, with one meaning at the
   top level and in `[context.<name>]`.** It builds
   `git+file://<repo common dir>?ref=refs/mugicrew/gate/<landing>&rev=<composed>#<attr>`
   in that repo, which is today's form with the target made configurable. A
   missing key means no gate, and a `gate.skipped` event records every gateless
   land. A context does not inherit the root's gate, because the root's
   attribute names the root flake. NenOE's config gains `gate = "mugicrew"` in
   the same change, so the mugicrew-specific default leaves the code.
4. **A gate target that will not evaluate stays an infra failure.** The Landing
   stays open and retries, `gate.infra` events surface it, and a config edit
   unblocks the next tick. No new pause cause is added.

## Considered Options

- Integrate: an optional `<id>` argument (amends Spec D3, and the nested prompt
  has to pass it). Running inside the child (every Workspace site would have to
  find the Workspace again). Putting the host on a ghost Workspace `effort/<id>`.
  `MUGICREW_EFFORT` (set only for dispatched efforts, never for HITL panes).
- Gate: skip it for every nested land; require `packages.mugicrew` of the child;
  require `gate =` whenever `repo =` is set. Keep the root default at
  `"mugicrew"`, which leaves any non-NenOE Workspace stuck as it is today.
