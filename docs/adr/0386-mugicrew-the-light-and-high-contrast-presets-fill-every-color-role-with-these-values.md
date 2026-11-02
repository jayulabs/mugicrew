# ADR 0386: The light and high-contrast presets fill every color role with these values, and light's shades carry an explicit ink

Status: Accepted (its P0/P1 tint and tint-ink values are retired by ADR 0410 (mugicrew-effective-priority-is-the-most-urgent…))
Date: 2026-10-28

Chart Named color themes for the mugicrew TUI (mc-0f831) needed values for its light
and high-contrast presets. A throwaway ran the real TUI's View on a fixture, kept on
`refs/mugicrew/prototypes/mc-0f831`. The fixture held every kind, every pause cause, an
inherited pause and the P0 and P1 tints. Mouse mode was on, so hover and the right-click
menu were the real ones. It rebuilt every package style in place for each candidate and
emulated the terminal's background with the tmux pane style. A swatch screen showed each
role on the plain background and on every shade. The human tried it on Defog, Graph,
Settings, the hints bar, hover and the menu, at 80 and 120 columns, and picked:

1. **Light is candidate A: a pale-blue selection bar with dark ink.** The selection is
   bg 153 with fg 16 bold, so the kind badge, priority, state glyph and liveness dot it
   keeps (relationship 9 in `docs/research/tui-color-literals-to-semantic-roles.md`)
   stay legible on it. Hover is lavender 189, and the ancestor shade is gray 254, so
   hover reads apart from both the selection and the shade. Rejected: B, dark's deep-blue
   bar (25) with white ink, where the darker light-preset kind hues sink into the bar;
   C, a yellow highlighter bar (229).
2. **High-contrast is candidate A: a saturated blue bar (27) with white ink.** Hover is
   navy 18, and the ancestor shade is 238. The grays are 248 and up, so no dim 240/244
   gray remains. Inactive tabs, hovered tabs and the reference keys go bold. Rejected: B,
   a yellow bar (226) with black ink and a cyan accent.
3. **Light's hover, ancestor and picker shades carry an explicit ink (235).** Text with
   no colour of its own (titles) fell back to the terminal's default foreground under
   these shades. In the throwaway that foreground was light, and the title text could not
   be read. With the ink in the shade's style, a reset inside the row restores it. Colored
   spans still set their own foreground, so ADR 0372 §1's "hover keeps the row's colours"
   holds. Dark and high-contrast have no shade ink: their shades are dark, as the
   terminal's background is.
4. **Caution has its own role.** It is the same as dark's amber (214) in dark, 136 in
   light and 221 in high-contrast, so amber keeps meaning only "someone waits on you"
   (ADR 0179 §2) in the new presets. The paused count `N‖` stays on the waits-on-you
   role in all three, as the code has it today.
5. **A Container chip's ink is picked per preset.** It is 0 in dark, 231 in light (on the
   darker light kind hues) and 16 in high-contrast. A priority tint's ink is 16 in all
   three.

The values, as dark / light / high-contrast xterm-256 indices. Dark is today's values, and
the roles are those of the research note above:

| Group | Values |
|---|---|
| chrome | accent 212/162/207; selection bg 25/153/27, ink 15/16/231, marker 111/25/123; hover bg 17/189/18; ancestor shade 236/254/238; picker shade 237/253/239; shade ink —/235/—; hint bar bg 236/253/235, key 252/235/231, desc 245/241/252; inactive tab border 240/248/248, label 245/243/252; hovered tab border 245/240/252, label 252/235/231; dim 240/247/248; soft 244/243/251; warn 203/160/196; waits on you 214/166/214; caution 214/136/221; form hint 15/236/231; picker check 114/28/46; create-new 213/127/207; reference key 241/238/252, desc 238/243/250, separator 237/248/248; Markdown code 180/94/222, link 111/25/117; stats bars 109/30/87; burn line 180/94/222 |
| liveness | alive 114/28/46; AFK 151/66/158; blocker pink 210/168/204 |
| state | in progress 220/136/226; P0 fg 203/160/196, tint 203/210/196; P1 fg 217/131/217, tint 217/224/217; chip ink 0/231/16; tint ink 16/16/16; unknown kind 244/243/251 |
| pause | foreign-hold 214/166/214; failing 203/160/196; abandoned 208/202/202; recovering 172/130/178; checkup 179/137/222; manual 213/163/201; integrate-conflict 44/30/51; not-built 220/136/226; profile 75/26/81; context 141/91/177; inherited 127/139/170 |
| kind | Signal 186/100/228; Survey 117/31/123; Grilling 202/166/208; Research 60/60/105; Prototype 170/127/207; Chart 68/25/75; ChartConflict 89/89/163; Spec 49/29/48; Checkpoint 180/137/223; Build 224/138/230; Review 108/65/120; Critique 167/167/203; Conflict 88/88/197; Approval 107/64/154; Pivot 160/160/196; Rollback 94/94/178; Errand 248/242/252; Discussion 246/244/250; Checkup 246/244/250; Surgery 93/55/141; Initiative 255/235/231; Landing 209/130/215; MRWatch 209/130/215; ForgeReview 109/66/152 |

The relationships the ADRs protect still hold. P1 is not the blocker pink. Manual and
inherited are magenta and a dimmer magenta. Discussion/Checkup and Landing/MRWatch share
a hue as they do in dark. mdrender's palette takes accent, soft, Markdown code and
Markdown link. The bubbles help Full* styles take the reference roles.

## Consequences

- The light values avoid indices 0–15, which the terminal's theme can remap. The one
  exception is dark's chip ink 0, which stays as it is today.
- The throwaway's in-place rebuild (mechanism A of
  `docs/research/tui-live-theme-switch-style-rebuild.md`) needed six function-local
  literals lifted to vars (unknown kind, chip ink, in-progress glyph, both tints, tint
  ink) and mdrender's constants made settable. The Spec still picks the mechanism.
