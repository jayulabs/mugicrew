<!-- from mattpocock/skills@d80fa0f4ebe0c5714af0adf8670336065233ecc6 skills/engineering/tdd/SKILL.md · lock-hash 9ce12c4a1e209e557fcf9d02086ade3d7ffdb976186018fc1b063b4164246004 · distilled -->

# Test-first

Test-first is the red → green loop. This doc is what makes that loop produce
tests worth keeping: what a good test is, where tests go, the anti-patterns,
and the rules of the loop. Every section applies on every cycle.

Name tests and interfaces in the Workspace's domain vocabulary.

## What a good test is

A test verifies behavior through a public interface, not implementation
details. The code can change entirely; the test shouldn't. A good test reads
like a specification ("user can checkout with valid cart") and survives
refactors because it doesn't care about internal structure:

- it uses the public interface only, and verifies through it (create, then
  read back through the interface, not by querying the store underneath);
- it describes WHAT, not HOW, with one logical assertion;
- it is integration-style: real collaborators, not mocks of internal parts.

Mock only at **system boundaries**: external APIs, time and randomness,
sometimes a database or the file system. Never mock your own modules or
internal collaborators. At a boundary, take the dependency as a parameter, and
prefer one function per external operation over one generic fetcher, so each
fake returns one shape with no logic in it.

## Seams: where tests go

A **seam** is the public boundary you test at: the interface where you observe
behavior without reaching inside. Tests live at seams, never against internals.

**Test only at seams you have written down before the first test.** No human
is there to agree them: take them from the Spec's build-sliceable
implementation decisions (the interfaces, modules and behaviors it names) and
from your bead's acceptance criteria (each criterion is a behavior some seam
must show). Prefer the seams the existing tests in the area already use. You
can't test everything; fixing the seams up front puts the testing effort on
the critical paths and the complex logic instead of every edge case.

## Anti-patterns

- **Implementation-coupled**: mocks internal collaborators, tests private
  methods, asserts call counts or order, or verifies through a side channel.
  The tell: it breaks on a refactor that didn't change behavior.
- **Tautological**: the expected value is recomputed the way the code computes
  it (`add(a, b) == a + b`, a constant asserted equal to itself), so it passes
  by construction. Expected values come from an independent source of truth:
  a known-good literal, a worked example, the Spec.
- **Horizontal slicing**: all tests first, then all implementation. Bulk tests
  verify imagined behavior and fix the test structure before you understand
  the code. Work in **vertical slices**: one test → one implementation →
  repeat, each test a tracer bullet that responds to what the last cycle
  taught you.

## Rules of the loop

- **Red before green.** Write the failing test first and watch it fail, then
  write only enough code to pass it. Don't anticipate future tests or add
  speculative features.
- **One slice at a time.** One seam, one test, one minimal implementation per
  cycle.
- **Refactoring is not part of the loop.** It belongs to the review stage
  (crew doc code-review.md), not the red → green cycle.
