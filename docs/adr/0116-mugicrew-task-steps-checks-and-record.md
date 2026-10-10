# ADR 0116: mugicrew Task — steps are a markdown Do/Check list; each passed step leaves one value-free comment

Status: Accepted
Date: 2026-10-09

## Context

A Task's session is an in-session guide that runs every check and resumes by
re-checking (ADR 0114). That left open how a Task's steps and checks are
written in the bead, where progress is recorded, how a resumed session uses
the record, and what must never be recorded. The mugicrew cutover's checks
(`mugicrew loop status` prints `on`; a no-op `mugicrew integrate` after a
restart) had to fit. Decided in grilling.

## Decision

1. **Steps are a numbered `## Steps` list in the Task body.** Each step has a
   title line, a `Do:` paragraph and a `Check:` paragraph. It is plain
   markdown read by the guide. There is no parser or linter.
2. **Only two markers**, written after the title: `[secret]` (a hidden-prompt
   step whose value never passes through the session, ADR 0114 §5) and
   `[elevated]` (sudo/root/admin, so the human runs it). The guide recognises a
   web-UI step by its Do having no command.
3. **A Check is runnable wherever anything can be probed**: a command and its
   expected result. `Ask:` replaces `Check:` only when nothing runnable
   exists. The human confirms it, and the label shows it is weaker.
4. **A Check must be safe to re-run**: read-only, or an idempotent no-op (e.g.
   `mugicrew integrate` exiting 0 with nothing ahead). Anything that changes
   state on a second run belongs in a Do.
5. **The record is one `bd comments add` per passed step**:
   `step N (<title>) passed: <check gist> → <observation>`. Comments are
   append-only and timestamped, and they keep the run record apart from the
   authored steps. Only passes are recorded; a failure lives in the session
   and is re-derived on resume.
6. **Never recorded**: secret values, tokens, passwords, private keys, and any
   URL or output carrying a credential. The observation is the guide's
   value-free summary (status, names, modes), never raw check output.
7. **Resume.** Every runnable Check re-runs in order. An `Ask:` step is trusted
   if a pass comment names its number and title as they are now; otherwise
   it is re-asked. The first failing step is current, even when it has an
   earlier pass record (drift). The guide reports the drift, and the old
   comments stay as history.
8. **Amending mid-run.** If a step turns out wrong or missing, the guide
   proposes the edit, and with the human's approval rewrites the body and adds
   `step N amended: <why>`. A whole-body write is safe because only the Task's
   own session edits it while it runs.
9. **No ordering in the body.** `blocks` edges are the only truth for ordering
   (ADR 0113). The body never repeats `after:`/`before:` lines.

## Considered Options

- **Checklist rewrite of the body (`[x]`):** rejected. It risks lost updates
  and mixes the run record into the authored steps.
- **Child bead per step:** rejected as graph weight with no payoff.
- **`--notes`:** rejected. It is a single overwritable field, not an
  append-only log.
- **Structured (YAML) steps with a parser:** rejected as unneeded. The reader
  is a Claude session.
