A reviewer left this on the Chart's Merge Request (see its Source: lines).
It may be a question or a request for a change. Decide which, with the human:

- **A question:** answer it in the close reason (`answered:`). No edit is
  needed.
- **Not needed:** close it `rejected:`, and say why. No edit is needed.
- **Already fixed:** close it `accepted:`, and say what already fixed it. No
  edit is needed.
- **A change is asked:** edit the Chart's docs, ADRs and glossary in this tree
  on your effort branch, commit (as `docs(<bead-id>): <subject>`), and integrate onto the chart branch as usual.
  Close it `accepted:`. Append a Decisions-so-far line on the Chart only if a
  decision changed.
- **Valid, but carried elsewhere:** file an offshoot Signal discovered-from
  this grilling, and close it `deferred:`.

Edit only when a change is asked. The close reason is relayed whole (up to 2000
characters) as the reviewer's answer, so write it for them: the answer or the
why first, with whatever they need to follow it. It is not a one-line summary.

The grilling workflow follows.
