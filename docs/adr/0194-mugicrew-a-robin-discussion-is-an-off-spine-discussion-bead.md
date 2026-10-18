# A Robin discussion is an off-spine `discussion` bead, opened with shell-wide `D`

Status: Accepted
Supersedes in part: ADR 0192 §1–4 (the `C` key, the synthetic id, the rows from the tmux session table)
Date: 2026-10-15

Chart mc-4ryp.2. Settles the grilling "What a Robin-drafted Signal carries"
(mc-4ryp.2.4). This reverses the Chart's first choice, a session with no bead
and a synthetic `robin-<ts>` id. A discussion is now a bead of a new kind,
`discussion`, owned by Robin. A real bead gives her sessions what every other
crewmate's get for free: events tagged `crewmate=robin` and `kind=discussion`,
which the Logs tab and `mugicrew stats` (ADR 0061) need to attribute them; the
rule "a crewmate is invoked with a bead" stays true, with no exception to
document; Drafts carry a real `drafted-by:<bead-id>` (ADR 0059 §2); and her
Signals get the standard offshoot edge, `discovered-from:<discussion>`, as
provenance. It also removes the one hard break the seam inventory found:
`hitl-run`'s clean-exit close reads the bead (mc-4ryp.2.2).

## Decision

1. **Kind.** `discussion` is free-standing and off the spine, like a Task
   (ADR 0113). Only a human launches it, the loop never dispatches it, and it
   is never a member of a Chart, Checkpoint or Initiative.
2. **Key and modal.** Shell-wide `D` opens the Robin modal (ADR 0192 §2's
   picker chrome; lowercase `d` is untouched). `+ New discussion` creates the
   bead, titled `Discussion <YYYY-MM-DD HH:MM>`, and launches it, claimed like
   any HITL bead. The rows are the open `discussion` beads, joined with their
   tmux state, and show title, age and state. The status bar reads
   `<title> · Robin discussion` and flips the normal way (ADR 0066 §4).
3. **Done.** When the human says they are finished, Robin retitles her bead to
   the topic, writes a short summary in its notes, closes it, and prints the
   standard closing line. Auto-exit (ADR 0111) fires, and the clean exit
   publishes her Drafts on the normal `settle` path.
4. **Drop and resume.** `x`, behind the y/n dialog, deletes her Drafts and
   closes the bead as `dropped`. A non-zero exit leaves the bead open and
   claimed, and the modal resumes it.
5. **The discussion bead is visible** in the Graph tab like a Task, and in
   Logs and stats. It is not filtered out.
6. **A Signal Robin drafts** on the human's yes has the standard offshoot
   shape: a title that names the work, with no kind prefix, and a body with
   what and why, plus a **Sources** line citing the paths, ADRs and bead
   titles the discussion relied on (ADR 0193). It keeps bd's default
   priority, since Luffy sets the priority at triage (ADR 0129 §2); any
   urgency goes in the body. It has no parent: it joins an Initiative only by
   adoption. Its one edge is `discovered-from:<discussion>`; beads it is
   about are cited by title, not linked.

## Considered Options

- **Keep the synthetic id** (ADR 0192). Rejected. The events would not name
  Robin, the clean-exit close needs its own path, the crewmate rule needs an
  exception, and a published Signal has nothing to point back to, because
  `drafted-by` is removed on publish.
- **A general class of hidden kinds** for phases 2 and 3 of the Initiative.
  Deferred until those phases need one. Task already shows how to keep a kind
  off the spine.
- **A provenance label (`via:robin`) or a body footer.** Rejected. The
  `discovered-from` edge does this job.
- **Hide discussion beads from the Graph tab.** Deferred until the clutter
  is real.
