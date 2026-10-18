# ADR 0220: Switching Require Initiatives on adds no list and no gate; grandfathered work shows where it already does

Status: Accepted
Date: 2026-10-16

From Chart mc-8ymw.5 (Opt-in Require Initiatives and Enforce external ID
Workspace settings), grilling "How switching the settings on lists
grandfathered chains and handles Initiatives with no external ref". The human
had already ruled that work outside an Initiative when the settings are switched
on is grandfathered: it finishes where it is and stays visible to the human.
This ADR records how little that takes.

## Decision

1. **The No Initiative section is the list.** The Ideate, Defog and Execute
   tabs already list every freestanding bead in their **No Initiative**
   section. That section is always shown, whether or not `required` is on,
   and does not change. Switching the setting on adds no new list, no
   `mugicrew doctor` row, no Actionable row and no Settings-tab row.
2. **"Grandfathered" is not a state.** Nothing stores, derives or marks it,
   and the section is not split into sub-groups. Once the setting is on,
   triage is the only way into work, so No Initiative holds only grandfathered
   chains, untriaged Signals and the exempt kinds. It shrinks as the
   grandfathered chains finish.
3. **An existing Initiative with no ref is grandfathered too.** "Required at
   birth" covers only Initiatives born after `external_ref` is switched on. One
   without a ref keeps taking members, and triage may still adopt into it.
4. **It carries a `no ref` marker.** While `external_ref` is on, an open
   Initiative with an empty external ref shows `no ref` on its Initiatives-tab
   row and its drill-in head. The marker sits beside `no body yet` when both
   apply. Like Bodyless, it is a render-time qualifier, not a lifecycle state.
   Closed Initiatives are not marked, and neither is the ON LAUNCH preview,
   because nothing at launch acts on the ref.
5. **The human sets a late ref with bd.** `bd update <id> --external-ref R`,
   documented beside the setting. No mugicrew verb or TUI field is added, and
   nothing checks for a duplicate on that path. This is the Chart's standing
   rule: a human with bd can always bypass the settings.

## Considered Options

- **A `mugicrew doctor` row counting outside chains.** Rejected: doctor answers
  "is this Workspace ready to run mugicrew". Grandfathered work is not a
  readiness fault.
- **Gating triage on a missing ref** (refuse to adopt into or seed an
  Initiative with no ref). Rejected: it stalls in-flight work over a field
  nothing reads until tracker sync, which is phase 3 and may never come.
- **A ref-setting verb that checks uniqueness.** Rejected for now: setting a
  ref after birth is the rare, grandfathered case.
