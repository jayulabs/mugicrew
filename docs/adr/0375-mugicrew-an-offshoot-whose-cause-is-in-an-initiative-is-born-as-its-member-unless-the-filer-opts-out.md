# ADR 0375: mugicrew an offshoot whose cause is in an Initiative is born as its member, unless the filer opts out

Status: Accepted
Date: 2026-10-28
Amends: ADR 0352 (every-task-joins-the-initiative…) §2; ADR 0348 (a-filing-with-no-plain-member-to-climb…) §6 and §7; ADR 0267 (an-offshoot-signal-is-filed-through-signal-offshoot…), its unordered fallbacks.

## Context

Only an ordered offshoot (a follow-up or prerequisite with its edge set) joined
its cause's Initiative. An offshoot filed `--order none`, and every unordered
fallback, was born loose with `Initiative: outside (unordered)`, even when the
work plainly served the cause's Initiative. The bead lifecycle audit filed 36 such
offshoots, each adopted by hand. The Chart "An offshoot whose cause is in an
Initiative is born as its member" settled that the order does not decide
membership.

## Decision

1. **An offshoot of any order joins its cause's Initiative** (amends ADR 0352
   §2, which kept offshoots ordered against nothing outside). The climb runs for
   `follow-up`, `prerequisite` and `none`, and for every unordered fallback
   (no container, a closed container, a pair bd refuses), as ADR 0267's
   fallbacks now keep the climbed Initiative parent. A refused edge loses only
   the order, not the membership.
2. **The climb is no longer gated on an edge** (amends ADR 0348 §6). Any direct
   member of an Initiative counts as the cause, not only an unclosed Errand:
   an Errand, a member Signal, Chart, Spec or Checkpoint, in any status.
   Brook's offshoot joins when its Errand is a member. A Discussion still has
   no container and stays outside.
3. **The stays-outside list changes** (amends ADR 0348 §7). The abandoned MR
   Watch Signal joins the stale-pin and stranded-commits Signals on it. Repair
   Signals, a Latch and a Pivot stay outside and join only by adoption. Go-side
   filers, Discussion offshoots and Checkup offshoots stay outside too.
4. **The filer opts out** with `--outside "<why>"`, and the surface (flag,
   `--dry-run`, the `Initiative:` line) is ADR 0371 (an-offshoot-filer-opts-out…).
5. **Going forward only**, keeping ADR 0352 §5. Existing loose offshoots are
   not migrated.
6. **An unordered member is placed by ADR 0255** (member-triage-places-the-signal…)
   like any member Signal. ADR 0347 (an-ordered-offshoot-member-is-placed…)
   still applies only to ordered members, and member triage's release stays the
   safety net for a member that doesn't belong.

## Considered Options

- **Keep the order as the membership test.** Rejected: it left every `none`
  offshoot loose and cost a hand adoption each, with no reason on the bead.
- **Opt in with a flag.** Rejected: the common case is related work, so
  born-in-place is the default and only the exception costs a flag.
