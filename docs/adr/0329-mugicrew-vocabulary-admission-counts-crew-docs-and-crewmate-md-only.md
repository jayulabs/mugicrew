# ADR 0329: vocabulary.md admission counts uses in crew docs and CREWMATE.md only

Status: Accepted
Date: 2026-10-21
Amends: ADR 0202 (mugicrew-a-vocabulary-crew-doc…)

ADR 0202's first admission test, that packaged text uses the term, was read two
ways: the vocabulary_test.go comment says crew docs, while audits counted role
prompts too. It counts **crew docs and CREWMATE.md only**. A term used only in
role prompts is glossed inline in its prompt, by ADR 0088
(mugicrew-crewmate-content-split)'s placement rule that single-consumer text
goes in its role prompt. That keeps vocabulary.md to the words every crewmate
reads, and a prompt-only term keeps its gloss beside the one use that needs it.
