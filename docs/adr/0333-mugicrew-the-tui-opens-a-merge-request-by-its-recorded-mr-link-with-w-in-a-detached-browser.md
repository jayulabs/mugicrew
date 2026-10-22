# ADR 0333: The TUI opens a Merge Request by its recorded MR link with w, in a detached browser

Status: Accepted; amends ADR 0226 (a-gitlab-landing-s-mr-iid-and-sub-state-ride-the-rollup…) §3 and its Considered Options. Amended by Spec mc-j3mw8: §1–§3 and the last Considered Option now name two Merge Requests sections, Execute's for Checkpoint MRs and Defog's for Chart MRs.
Date: 2026-10-21

Spec mc-rqbew (Merge Requests section in the Execute tab, backed by MR link:
lines Jinbe records). Research:
`docs/research/tui-browser-launch-detached-and-failure-predicate.md`,
`docs/research/tui-land-strategy-resolution-and-refresh.md`.

ADR 0226 §3 said "No MR URL: the TUI has none". Jinbe now records the Merge
Request's GitLab web URL as an `MR link:` line beside every `MR:` line it
writes, so the TUI has one to show and open.

## Decision

1. **The URL is `MR link: <web_url>`**, read only from that body line, never
   derived from the iid, project path or Forge host. The Merge Requests
   sections of the Execute and Defog tabs show it in `d` and open it with `w`.
   Each lists its own container kind's MR Watches (Checkpoint on Execute,
   Chart on Defog), and no MR is on both. ADR 0226's rejection of "MR URL in
   `d`" is reversed for those two sections only.
2. **Everywhere else stays URL-free**, meaning outside those two sections. The rollup's `!iid` qualifier, its `d`
   `why:` line and the Checkpoint rows are unchanged.
3. **The status word for Chart MRs shows in Defog's section**, which ADR 0226 §4
   kept off the rollup. The word still comes from the container's open,
   unpaused Landing, and is left out for a paused one (§2) or an unmapped status.
4. **`w` launches from an ordinary command, never `tea.ExecProcess`**, which
   would release the terminal for the browser's whole lifetime. The child has
   nil stdio, leads its own session (`Setsid`) and is `Run` to completion on the
   command's goroutine, so it leaves no zombie and its exit status is kept.
   Candidates are tried in order, the first clean exit wins:
   1. each non-empty `$BROWSER` entry (split on `:`), run as
      `sh -c '<entry>' sh "$url"` with `%s` replaced by `"$1"`, else `"$1"`
      appended, which keeps the URL out of shell parsing;
   2. with `$BROWSER` unset, `xdg-open` on linux, only when `DISPLAY` or
      `WAYLAND_DISPLAY` is set, so an SSH session never opens a tab on a far
      screen;
   3. `open` on darwin.
   A direct `$BROWSER` is not display-gated: the operator chose it.
5. **Failure is a non-nil `Run`, or no candidate at all.** The URL then goes to
   the clipboard over OSC 52 and the status line says so. Exit 0 is not proof a
   tab opened, so the success wording claims only the launch ("opened !42 in
   browser").
6. **A row with no `MR link:` line yet still lists.** `w` says there is no link
   yet and runs nothing; Jinbe backfills the line on its next read of the MR.
7. **The section's gate shows the error.** An unusable `land_strategy` shows
   the section with the resolve error where the none placeholder would be,
   never a fallback to local (ADR 0147 (a-land-is-a-landing-bead-behind-a-land-strategy-seam) §3).
   The error is not a status-line message.

## Considered Options

- **`tea.ExecProcess` for the browser.** Rejected. It suspends the TUI for as
  long as the browser lives, and a GUI browser may never exit.
- **`Start` without `Run`.** Rejected. It leaks a zombie and loses the exit
  status the fallback needs.
- **Deriving the URL from the iid and the Forge host.** Rejected. Nested repos
  and self-hosted GitLab make it a guess; `web_url` is GitLab's own.
- **Gating `$BROWSER` on a display too.** Rejected. A terminal browser needs
  none, and the operator chose it.
- **URL on the rollup qualifier or its `why:` line.** Rejected. The Merge
  Requests sections are the open-in-browser places.
