# ADR 0423: Hand-run verbs and the Initiative-ref race are outside the serializer contract

Status: Accepted
Date: 2026-11-02

Amends: ADR 0070 (mugicrew-one-bd-write-rule) §1, which says a read-then-write
site with no serializer is a defect. This ADR names two exceptions, in the way
§4 of that ADR accepts the Cap overshoot.

Chart mc-5bg93.11 (nami and luffy reach bd only through bd.Core, their tests
seeding a bdtest graph), settled by its grilling mc-5bg93.11.4. The research
doc `docs/research/nami-luffy-spawn-sites-and-the-claims-that-serialize-them.md`
(§5) found sites that run under no held claim when a human runs the verb from
their own shell, and one cross-Signal check-then-create that no claim covers.

## Decision

1. **A verb a human runs from their own shell holds no claim and is outside
   the serializer contract.** This covers hand-run `mugicrew reconcile` on any
   kind (Signal, Chart, chart-conflict, Approval, and Spec as zoro already
   documents) and `mugicrew pivot raise` on an Errand that no TUI launch has
   claimed. The verb neither refuses nor takes a claim. Each handler documents
   the exclusion in its doc comment. The same verb run by an agent inside a
   launched, claimed session (the triage prompt's `reconcile <signal> --spec`,
   brook's `pivot raise`) is inside the contract, serialized by that session's
   claim.
2. **The Initiative-ref uniqueness check is an accepted race.** luffy's
   `refClash` check-then-create in `SeedInitiative` spans Initiatives across
   Signals, so no single claim serializes it. Two Signals triaged at the same
   moment with the same external ref may both mint an Initiative carrying it.
   That is accepted: no lock is added, and the site's comment cites this ADR.
3. **A Launch invoker is not a shell verb.** `mugicrew signal triage` launches
   a session the way the TUI does, so it is not excluded by §1. Its missing
   claim is a gap to fix (an offshoot Signal of this Chart). Until then, tests
   seed the triaged Signal claimed, as the TUI launch leaves it.

## Considered and rejected

- **Refuse a hand-run verb unless this actor holds the claim.** It is new
  behaviour in every handler, and it blocks a legitimate operator repair. The
  real cli ignores the Serializer, so the exclusion carries no production risk,
  only a gap in what the fake checks.
- **The hand-run verb takes and releases the claim itself.** It would collide
  with a session that holds the bead, and a human shell has no settle step to
  release it on a crash.
- **A lock token (ADR 0380 §3) for Initiative refs.** The race needs two humans
  triaging Signals with the same ref at once, its result is visible and fixable
  by hand, and ADR 0070 §1 adds no new lock where the cost exceeds the race.
- **Declare `signal triage` sessions outside the contract.** It is a documented
  front door, so that would leave every Signal site permanently unserialized on
  one of its two launch paths.
