# ADR 0219: The ADR-number guard is a numbered-file clash check over every configured sequence

Status: Accepted
Date: 2026-10-16

From Chart mc-8okw (Per-Workspace numbered-sequence clash rules) and its Spec,
Numbered-sequence clash check with per-Workspace `{n}` template rules. Amends ADR 0030
(adr-number-collision-guard) and ADR 0215 (an-unusable-or-overlapping…). Cites ADR 0214
(a-numbered-sequence-template-compiles…) unchanged.

## Decision

1. **The guard covers every configured sequence (amends ADR 0030).** Land's in-lock
   check, the Chart-conflict post-resolve check and Land's re-check after a Checkpoint
   conflict run every rule that applies to the landing repo. The ADR rule
   `**/docs/adr/{n:4}-*.md` is built in and always on; config only adds sequences. ADR 0030's
   other points stand: only added numbers are checked, the landing branch moves,
   mainline's numbers stay fixed, and the sweep is scoped to what the branch owns (§5).
2. **The variant is renamed `number-clash` (amends ADR 0030 §3).** The clash type says
   "numbered-file clash". Each clash renders as `<added> (rule <template>) (vs
   mainline <p>[, <p>…])`. Both Conflict bodies carry that string and cite this ADR.
   Nami's chart-conflict prompt also accepts the legacy `adr-number-clash` token, so
   beads filed before the rename still resolve.
3. **The `renumber = rename|sweep` value is dropped (amends ADR 0215 §1).** A rule is
   unusable if its template has no `{n}`, more than one `{n}`, a `{n}` outside the
   file name, a bad `{n:W}`, does not compile, or sits in an unknown key's place. The
   per-rule-mode rationale for refusing on overlap no longer applies; overlap still
   refuses, because a file may not belong to two number spaces.
4. **Every sequence is renumbered with a reference sweep**, ADRs included; a sweep that
   finds nothing amounts to a rename. References are recognised by reading, guided by
   the forms the crew doc names (name stem, path, `V{n}`-style token, "migration
   {n}" prose, ADR NNNN). No per-rule reference pattern is configured. An in-scope
   reference whose referent can't be told apart escalates.
5. **Invariants over Workspace guidance.** Franky and Nami follow the Workspace's
   CONTRIBUTING.md/README.md renumber guidance on how; mugicrew's invariants win on
   what (mainline numbers fixed, branch moves, sweep scope, escalate on ambiguity). A
   conflict between them escalates. A renumber keeps the file's own width and is
   refused if the new path leaves its rule (ADR 0214 §4); a number the file states
   about itself (an ADR's title line) is fixed too.
6. **Config shape.** `sequences = ["db/migration/V{n}__*.sql"]` at the top level of the
   checked-in `.mugicrew/config.toml` applies to the Workspace repo, including every
   `path =` subtree context in it. A repo = child declares its own under
   `[context.<name>]`, so the child needs no config of its own. A `sequences` key in a
   `path =` context is unusable: its templates are relative to the shared Workspace
   repo and belong at the top level. Rules come from the checked-in config only (no
   machine-local layer), read from the Workspace root on disk when the check runs.
7. **The path = context rule.** A `path =` context shares its Workspace repo's
   sequences; only `repo =` contexts have a rule set of their own (plus the built-in).

## Consequences

- A migration number two Builds both took is a Conflict Franky renumbers, not a gate Critique.
- ADR 0030 §2's Chart-only ceiling and its "ADR-number" wording are superseded
  by this ADR; ADR 0215's `renumber` value no longer exists.
