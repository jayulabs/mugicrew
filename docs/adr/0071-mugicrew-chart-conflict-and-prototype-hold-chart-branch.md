# ADR 0071: mugicrew chart-conflict and prototype sessions hold chart/<id> through a Worktree route; the prototype throwaway is built in a second, detached tree

Status: Accepted
Date: 2026-10-06

## Context

ADR 0068 §2 says every container checkout goes through `git.NewWorktree`. Two
session kinds did not:

- **Chart-conflict** routed to the repo root (`route.Route` default arm). Its
  prompt then reused any tree that already had the branch checked out, or ran
  its own `git worktree add ../chart-conflict-<id>`. That path is not
  temp-shaped, so `ownContention` reads it as a foreign hold. "Work there" also
  put two efforts in one tree, which breaks ADR 0068 §1.
- **Prototype** routed to `SideRef`, and `session.placeTree` ran it from the
  repo root. The prompt cut a scratch tree on `chart/<parent>` and removed it
  after the build. Its Chart-body append (Decisions-so-far) and its
  ADR/CONTEXT commits came after the removal, so no branch hold serialized the
  append (ADR 0070 §2), and the docs had no tree to land in.

Decided in grilling.

## Decision

1. **Chart-conflict is a Worktree kind.** `route.Route` sends
   `chart-conflict` to `Worktree` on `chart/<parent>`. `placeTree` cuts the
   tree through `git.NewWorktree` and the session runs inside it. The prompt
   drops its own `git worktree add` and the "work there if already checked
   out" branch. A busy branch refuses the cut, and the normal contention and
   foreign-hold handling applies (ADR 0054, 0055).
2. **Prototype holds `chart/<parent>` for the whole session.** `route.Route`
   sends `prototype` to `Worktree` on `chart/<parent>`. That hold serializes
   the Chart-body append (ADR 0070 §2) and gives the ADR/CONTEXT commits a
   home on the chart branch.
3. **The throwaway is built in a second, detached tree.** The prompt cuts it
   with a plain `git worktree add --detach <tmp> chart/<chart-id>` at the
   wrapper-supplied temp-shaped path. The throwaway is committed there,
   `update-ref`'d to `refs/mugicrew/prototypes/<chart-id>`, and the tree is
   removed. It never goes through `NewWorktree`: a detached tree holds no
   branch, so it can't collide. A lingering tree is temp-shaped, so it reads as
   mugicrew's and blocks nothing.
4. **The side ref stays route-derived.** For prototype, the Worktree
   destination carries both `Branch` (`chart/<parent>`) and `Ref` (the side
   ref). The `SideRef` placement constant and `session.Placement`'s SideRef
   fallthrough are deleted.
5. **FR4 is reread.** The Spec's FR4 ("prototype never checks out
   chart/<id>") now means that **the throwaway never lands on `chart/<id>`**.
   The session may hold the branch, and FR5 no longer lists chart-conflict as a
   repo-root leaf.

## Consequences

- `ownContention` covers both kinds unchanged, because it already routes
  in-flight beads through `route.Route`.
- HITL launches follow the route through `placeTree` as before.
- A prototype now waits behind a grilling, research or charting session on the
  same Chart (one effort per container branch, ADR 0068), and those sessions
  wait behind it. This is the price of a serialized Chart-body append.
- Considered and rejected: a `mugicrew worktree` CLI verb for chart-conflict,
  which needs a new surface and meets ADR 0068 §2 only by convention. Also
  rejected: a single-tree prototype that commits the throwaway on the chart
  branch and then resets it back, which is fragile and puts the throwaway on
  `chart/<id>`. And a detached-only prototype with some other body serializer,
  which ADR 0070 rules out because it adds no new primitive.

## Amendment: prototype works on `effort/<id>` (ADR 0102 §11)

§2 no longer holds. `route.Route` sends a prototype to `Worktree` on its own
`effort/<id>`, cut from `chart/<parent>`, and its ADR, GLOSSARY and Chart-note
commits reach the Chart through `mugicrew integrate`. The Chart-body edit is
serialized by `mugicrew chart append|drop` (ADR 0103), not a branch hold. The
throwaway's detached tree and side ref are unchanged. The chart-conflict arm
still checks out `chart/<parent>`.
