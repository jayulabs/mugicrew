# ADR 0202: a vocabulary.md crew doc ships a verbatim subset of GLOSSARY.md

Status: Accepted
Date: 2026-10-15
Amends: ADR 0088 §5

## Context

ADR 0088 §5 shipped no mugicrew glossary: each doc was to define the terms it
uses where it uses it. In practice the cross-cutting terms (Draft, publish,
drained, land vs. integrate, held, Mainline) are used across several crew docs
and defined in none, so a crewmate on a foreign Workspace, which has no
`apps/mugicrew/GLOSSARY.md`, misreads them
(docs/research/mugicrew-terms-in-shipped-crewmate-text.md).

## Decision

1. **One `vocabulary.md` crew doc** carries the cross-cutting mugicrew terms.
   Inline definitions that crew docs already give (offshoot, pivot, Critique,
   claim, effort branch, …) stay where they are; vocabulary.md does not point
   at them.
2. **Entries are verbatim GLOSSARY.md entries**, with `(ADR …)` citations and
   `_History_` lines stripped by one fixed rule (ADR numbers would collide with
   a foreign Workspace's, ADR 0088 §6). The drift guard applies the same rule
   before comparing: a `crewdocs` Go test extracts each term vocabulary.md
   holds from `../GLOSSARY.md`, strips it, and requires a byte-equal entry,
   printing the expected text on failure; it skips when GLOSSARY.md is absent
   (the nix build), so it guards only in the dev tree and publish CI.
3. **vocabulary.md is a strict subset of GLOSSARY.md.** A sense the crew docs
   need but the glossary lacks (e.g. *held*, *Mainline*) gets a GLOSSARY.md
   entry first; vocabulary.md never holds an entry of its own.
4. **Reached through CREWMATE.md's Domain-vocabulary bullet**, which names
   vocabulary.md and says mugicrew's terms win for the bead graph and the
   crew's process while the Workspace's glossary wins for its own domain. The
   preamble stays within its 2048-byte cap by trimming, not by raising it.
5. **Read always, once per session**: the misreads come from words that look
   familiar ("held", "land", "deferred"), which on-demand reading never
   catches.
6. **Colliding senses in crew docs are reworded** where a plain word works
   ("abandoned Surgery", "land" to a side ref); a glossary sense note is added
   only where the word must stay (bd's `deferred` status).

## Considered and rejected

- **Inline glosses only (ADR 0088 §5 as written).** Draft alone would be
  glossed in five docs: many small copies to drift instead of one checkable
  copy.
- **Ship GLOSSARY.md whole.** 851 lines, mostly terms no crewmate reads.
- **Hand-shortened entries.** Reads better, but a guard could only flag that
  the source changed, not compare text.
- **An index line in CREWMATE.md with a raised cap.** Loosens the preamble
  budget for a pointer the vocabulary bullet already carries.
