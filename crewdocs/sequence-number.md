<!-- hand-kept · summarises ADR 0030 (adr-number-collision-guard), ADR 0214 (a-numbered-sequence-template-compiles-to-a-regexp), ADR 0219 (the-adr-number-guard-is-a-numbered-file-clash-check…), ADR 0088 (this doc's split) -->

# Numbering a new numbered file

This overrides ADR-FORMAT.md's Numbering section ("scan the target context's
`docs/adr/` for the highest existing number"): that scan sees only mainline plus
your own branch, so two concurrent containers take the same number. Number every
new file in a numbered sequence this way instead: an ADR
(`docs/adr/NNNN-<slug>.md`, the built-in example) or any file a configured
sequence matches (e.g. `db/migration/V{n}__*.sql`).

1. From the worktree you are writing in, run `mugicrew sequence next <would-be
   path>`. The number in the path is a placeholder of zeros at the rule's width
   (e.g. `apps/mugicrew/docs/adr/0000-<slug>.md`). The file need not exist yet.
   A non-numeric placeholder such as `NNNN` matches no sequence rule, so that
   error does not mean the verb is broken: retype the zeros.
2. Name the file with exactly the number the verb printed, at the width it
   printed. The number is free above mainline and every open `checkpoint/*`,
   `chart/*` and `effort/*` branch.
3. Commit the file on your branch in the same step, so other containers' calls to
   the verb (which see only committed branches) see your number.
4. For several new files in one sequence, number them in their intended order:
   re-run the verb after committing each one, or number them consecutively up
   from one result.
5. If the verb errors (no rule matched, or the number outgrows the rule's width),
   escalate if your prompt gives you a way to; otherwise stop and say so. Never
   fall back to scanning by hand.

A clash can still turn up at land time. That is not a failure of first-numbering:
it goes through crew doc sequence-renumber.md, the backstop.
