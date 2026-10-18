# ADR 0200: Jinbe's thread reply carries a hidden Qualified Bead Id marker

Status: Accepted
Date: 2026-10-15

Chart mc-t72m.9 (Jinbe files actionable Merge Request comments on the
container). Settles grilling mc-t72m.9.6. Charting has Jinbe reply on a filed
thread when its bead closes, then resolve it. The reply posts as the human's
glab user, so author can't tell it from a human note. Research
(docs/research/merge-request-thread-filing-key-and-reply-after-close.md §5)
makes a marker in the reply load-bearing: it anchors reply-after-close
detection, and it stops a crash between reply and resolve from posting a second
reply. Under `land_strategy=gitlab` several personal Bead Graphs can share an
`issue_prefix` (ADR 0187), so a bead id alone can't name the bead.

## Decision

1. **Qualified Bead Id.** A bead is named across graphs as
   `<bead-id>@<graph-id>`, where the graph id is the full 32-character Dolt
   root hash (ADR 0187). A bead id never contains `@`, so the form splits on
   the first `@`. Its display form, `<bead-id>@<graph-name>`, is for people
   only and is never matched.
2. **Graph name.** The graph name is the GitLab username from `glab api user`.
   The Signal "Each personal Bead Graph syncs to a ref on the Forge" names the
   graph's sync ref after the same username, so the two must stay equal. This
   Chart doesn't wait on that Signal.
3. **The reply.** It is one template for Checkpoint MRs (Critique) and Chart
   MRs (grilling):

   ```
   🤖 mugicrew: addressed by **<title>** (<bead-id>@<graph-name>) in <sha8>: <reason>
   <!-- mugicrew-reply: <bead-id>@<graph-id> <graph-name> -->
   ```

   - `<sha8>` is the first 8 characters of the newest commit on the container
     branch whose `<Kind>: <bead-id>` integrate trailer names the bead. GitLab
     links it to the commit itself. If no such commit exists, the bead closed
     without a fix, and ` in <sha8>` is left out.
   - `<reason>` is the first line of the bead's close reason, cut at 200
     characters with `…`.
   - The reply carries no Landing or pipeline link. The Landing that carries
     the fix does not exist yet when the bead closes.
4. **The marker.** It is the reply's last line, an HTML comment. GitLab keeps
   it in the raw note body and does not render it. Jinbe matches it as a whole
   line. Rule §5's "marker for B" matches only when both the bead id and the
   graph id are equal. The trailing graph name is a label and is ignored, so a
   marker naming another graph reads as a human note.

## Considered Options

- **A visible `mugicrew: <bead-id>` line.** Rejected. A human could type or
  paste it by accident, and it collides across graphs that share a prefix.
- **`<graph-id>:<bead-id>`, or a shortened hash.** Rejected in favour of
  `<bead-id>@<graph-id>`, which reads bead-first. The full hash keeps the
  "root hashes don't collide" guarantee.
- **The graph name as part of the identity.** Rejected. Usernames can be
  renamed, and two people can pick the same one. The hash is the identity.
- **Name the container's tip at reply time.** Rejected. It picks up unrelated
  later commits and can't show a close with no fix.
- **Retrofit the land trailers (`<Kind>: <id>` + `Graph:`) onto the Qualified
  Bead Id.** Deferred to an offshoot Signal. Those trailers are landed history
  with existing parsers.
