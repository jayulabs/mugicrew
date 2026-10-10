# ADR 0114: mugicrew Task — the walk-through is an in-session guide, not a generated wizard

Status: Accepted
Date: 2026-10-09

## Context

A Task (the Task Chart) walks a human through changing the external world. The
question was whether its HITL session should be a Claude session that guides
the human, or a generated bash wizard (the installed `wizard` skill) that the
human runs. Both were prototyped on one sample Task, "create the public repo and
set the Publisher remote", against a sandbox (prototype on a side ref). They were judged on resumability, check
fidelity and secrets handling.

## Decision

A Task's session is an **in-session guide**: a Claude HITL session that reads
the Task's steps and drives them one at a time.

1. **CLI steps are run by the guide.** The guide explains what the command does,
   asks for approval, then runs it. This includes commands that act with the
   human's credentials (`gh repo create` as their account). If the credentials
   don't work, the guide stops and tells the human; it does not work around them.
2. **The human runs elevated steps.** Only `sudo`, root or admin rights count as
   elevated. The guide hands the human the command and checks the outcome.
3. **Steps the guide can't reach are walked through.** Web UIs and other
   systems without CLI access get click-by-click directions, and the guide waits
   for the human to say each one is done.
4. **The guide runs every check itself.** When a check fails, the guide shows
   what it observed, diagnoses it and offers a fix. It does not advance past a
   failing step.
5. **Secrets never pass through the session.** The guide never asks for a
   secret in chat. It hands the human a command with a hidden prompt to run in
   their own terminal, wrapped so it works in any shell (`bash -c '…'`; zsh's
   `read -p` broke in the prototype). Then it checks only the outcome (status,
   file mode, names), never the value.
6. **Resume by re-checking.** A started or resumed session runs every step's
   check in order. The first one that fails is where the session resumes. Each
   passed step leaves a one-line record on the Task bead, which never contains
   a value. The record's exact shape is a separate grilling, "Decide how a
   Task's steps and per-step checks are written and recorded".

## Considered Options

- **A generated wizard script (the `wizard` skill).** It is best at secrets
  (hidden `read -s`, no transcript). The human liked it, but it is too
  restrictive for a Task: it can't diagnose a failed check, adapt to the human's
  environment, or run credentialed steps on approval, and mugicrew can't see how
  far it got. It may have a place later. It is not a Task form now.
- **A hybrid where the guide hands secret steps to a mini-wizard.** Not needed:
  a shell-agnostic one-liner with a hidden prompt covered the secret step.
