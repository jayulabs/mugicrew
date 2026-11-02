# ADR 0424: The clash guard also flags two files a branch adds under one number

Status: Accepted
Date: 2026-11-02

From Chart mc-0jofq (Land's numbered-file clash guard also catches two files a
container adds under one number), settled by its Survey. Amends ADR 0030
(adr-number-collision-guard) and ADR 0219 (the-adr-number-guard-is-a-numbered-file-clash-check…);
amends ADR 0225 (a-forge-merged-number-clash-is-renumbered…). Root ADR 0097 landed
twice in one squash land because the guard compared a branch's added files only
against mainline's.

## Decision

1. **Intra-branch duplicates clash (amends ADR 0030 §1, ADR 0219 §1).** Land's
   in-lock check, the Chart-conflict post-resolve check and the post-land check
   also flag two or more files a landing container adds under one number in one
   number space (same rule, same directory), none of them on mainline. The guard
   stays the backstop for every cause; why two numbers were handed out twice is
   not fixed here.
2. **The later-added file moves.** Of the files sharing the number, the one whose
   introducing commit is earliest on the container branch, by ancestry, keeps it;
   each later one moves. Files a single commit adds are ordered by path, the
   lowest keeping the number. After a squash land (the post-land check) every
   file comes from one commit, so the path order decides there.
3. **Rendering (amends ADR 0219 §2).** One clash line per file that moves:
   `<added> (rule <template>) (vs this branch's <earlier>[, <earlier>…])`. The
   file that keeps its number gets no line.
4. **Mainline wins.** A file whose number mainline holds is reported only as a
   mainline clash; the intra-branch check applies only to numbers mainline does
   not hold. Moving every branch file off a mainline number clears any sibling
   clash among them too.
5. **`mugicrew sequence next` counts the branch's own other adds.** The printed
   number sits above mainline's, other open branches', and every file the current
   branch adds in that directory except the one being moved, so a moved file never
   lands on its own branch's later number. The renumber crew doc's "move
   non-colliding files up too" ordering clause is simplified to match.
6. **Post-land (amends ADR 0225).** A pair of files one landed commit adds under one
   number, both on mainline's tip and neither on mainline before it, is a post-land
   clash; the mover (by §2's path order) goes through ADR 0225's renumber
   Checkpoint. A pair already on mainline before the land stays a skipped
   pre-existing duplicate.

## Considered Options

- **Always path order** (§2): simpler, but arbitrary; the first-added file is the
  one beads and other ADRs most likely cite already.
- **Leave `sequence next` alone** (§5): the ordering clause would renumber files
  that never clashed, rewriting their citations.
- **A second guard at Integrate time**: catches the duplicate earlier, but the
  land-time guard already catches every cause; left out.
