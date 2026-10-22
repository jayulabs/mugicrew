# ADR 0339: A close-out reply reads its verdict from a fixed close-reason prefix

Status: Accepted
Date: 2026-10-21

Chart mc-116gv (Close-out replies on Merge Request feedback state a clear
verdict and rationale). Settles grilling mc-116gv.3. Amends ADR 0200
(jinbe-s-thread-reply…) §3 and ADR 0319 (a-filed-comment-is-closed-out…) §4.
Today's reply says "addressed by" on every close, even one closed as not needed.
Charting settled that the verdict is a fixed prefix on the close reason
(`accepted:` / `rejected:` / `answered:` / `deferred:` + rationale), with
Jinbe inferring a verdict when there is no prefix. The research
(docs/research/merge-request-filed-bead-close-paths-and-close-reasons.md) lists
every close path. This ADR settles how each path gets the prefix in, and how
the reply renders it. The marker (ADR 0200 §4) is unchanged.

## Decision

1. **Parsing.** Jinbe trims leading whitespace from the close reason and
   matches `accepted:`, `rejected:`, `answered:`, `deferred:` or `pivoted:`
   case-insensitively. The rest is the rationale. Any other prefix (`fixed:`,
   `wontfix:`) counts as no prefix: the verdict comes from the charted
   fallback, and the whole reason is kept verbatim as the rationale.
2. **Franky's finish (a fixed Critique).** The build session leaves its
   rationale as a bead comment, `bd comments add <id> "Rationale: <…>"`. The
   newest one wins, mirroring the `Not built:` note (ADR 0248). For a
   `kind:critique` only, `finish` closes with `accepted: <rationale>`. With no
   `Rationale:` comment it closes with no reason, as today, and the fallback
   infers the verdict (commit → Accepted, rationale = commit subject). `finish`
   cannot prove a change was made, so it never claims Accepted on its own.
   Builds still close with no reason.
3. **The build session's no-change close.** It writes `rejected: <why>` for
   not needed, and `accepted: <what fixed it>` for already fixed elsewhere.
   This is a prompt change only.
4. **`pivoted:` reads as Deferred.** The shared writer (`bd/pivot.go`,
   `pivoted: <ids>`, read by Robin and Zoro) is unchanged. On a filed bead
   Jinbe maps `pivoted:` to ⏭️ Deferred. The carrier is the pivot named by the
   reason's first id. The rationale is a fixed sentence: "The Spec is being
   revised; this ask moves with it." A pivot's `Evidence:` is written for
   Zoro, not for the reviewer.
5. **The carrier of `deferred:`.** Jinbe reads the Signals that are
   `discovered-from` the bead, at any status (`bd.SignalsFrom`). It does not
   read a `caused-by` edge, which is the Signal-reroute edge (ADR 0323
   (a-signal-raises-a-pivot-through-caused-by…) §1), and it does not read an id
   written in the reason. If there are several carriers, it names them all,
   comma-joined. If there are none, the header is ⏭️ Deferred without
   "tracked by".
6. **Layout.** The header names the filed bead once, then a blank line, then
   the rationale, then the marker as the last line:

   ```
   🤖 mugicrew — ✅ Accepted · fixed in <sha8> by **<title>** (<bead-id>@<graph-name>)

   **Rationale:** <rationale>
   <!-- mugicrew-reply: <bead-id>@<graph-id> <graph-name> -->
   ```

   - `❌ Rejected` and `💬 Answered` replace the verdict and drop `fixed in <sha8>`.
   - An Accepted with no integrate commit drops ` fixed in <sha8>` and keeps
     `by **<title>** (…)`.
   - `⏭️ Deferred · **<title>** (…) · tracked by **<carrier title>**[, …]`
     names the carrier by title only, with no id.
   - The neutral fallback is `☑️ Closed · **<title>** (…)`.
   - The rationale is cut at 2000 runes with `…` (ADR 0319 §4). With an empty
     rationale and no commit subject, the blank line and Rationale line are
     left out.
   - The stranded reply (ADR 0319 §5) is unchanged.

## Considered Options

- **A notes line, a scraped final session line, or the commit subject alone for
  Franky's rationale.** Rejected. A comment has a precedent (`Not built:`), a
  reader in `bd`, and a newest-wins rule. A scraped line is lost when the
  session crashes. The commit subject stays the fallback.
- **Rewrite the `pivoted:` writer to `deferred:` for Critiques.** Rejected.
  Robin and Zoro give `pivoted:` its meaning (ADR 0323, ADR 0043). A mapping
  inside Jinbe needs no new writer.
- **A qualified carrier id in the `deferred:` reason.** Rejected. The
  `discovered-from` edge already exists for offshoots and pivots, and an id
  typed into the reason can be mistyped.
- **A case-sensitive prefix, or stripping an unknown prefix.** Rejected. A
  human may type `Rejected:`. Stripping a prefix we don't know drops words the
  closer meant.
