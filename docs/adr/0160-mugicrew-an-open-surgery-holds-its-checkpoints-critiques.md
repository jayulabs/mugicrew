# ADR 0160: mugicrew an open Surgery holds its Checkpoint's Critiques; a hand fix leaves the Critique to Franky's roll-forward

Status: Accepted
Date: 2026-10-13

Amends: ADR 0094 §2 (Critiques go to Franky AFK) with a hold while a Surgery
is open. Builds on the Chart "Surgery on an existing Checkpoint". Decided in
grilling.

## Context

A Surgery can now join an open Checkpoint. One reason to do that is to fix by
hand what an open Critique asks for, when waiting on the Critique would take too
long. Franky would otherwise dispatch that Critique while the surgery session
edits the same branch, so both fix the same lines and race at integrate. The
integrate fails loudly rather than losing work, but it is still noise.

## Decision

1. **The Critique stays open, and Franky's roll-forward closes it.** When a
   Surgery fixes the defect an open Critique names, nothing closes the Critique
   or links it to the Surgery. After the Surgery ends, Franky runs the Critique
   AFK as usual. His test-first run finds the fix already present, so he either
   commits nothing or commits only a regression test, and the Critique closes.
   This is the check that the defect is really fixed.
2. **An open Surgery holds its Checkpoint's Critiques.** While any Surgery on a
   Checkpoint is open, whether a session is running or it is idle, Franky does
   not dispatch that Checkpoint's open Critiques. Ending the Surgery releases
   them. A Critique already in progress when the Surgery is filed finishes
   normally.
3. **The surgery session lists the held Critiques.** Its prompt names the
   Checkpoint's open Critiques (ID and title) as context, so the human can
   choose to fix one by hand. The session never claims or closes them.

## Considered Options

- **The surgery session closes the Critique it fixed (`fixed-by:`).**
  Rejected: it skips the verification, and it has an agent close a bead that is
  not its own.
- **A filing link: the Surgery is filed against the Critique, which then
  depends on it.** Rejected: the hold in (2) covers the race without adding a
  new edge or a filing choice.
- **Hold only while a session is running.** Rejected: Franky could then start a
  Critique between two sessions of the same Surgery, and the race comes back.
- **No hold.** Rejected: the race described in the Context.
