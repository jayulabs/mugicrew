# ADR 0422: The landing page is the human's own copy: a split hero, a Learn band and five verb sections

Status: Accepted
Date: 2026-11-02

Chart mc-k6k1r.7 (landing page redesign of the mugicrew site). Settles
grilling mc-k6k1r.7.3 (what each content block on the landing page says). The
human wrote the copy of `/` themselves and edited it live in a preview of
variant B. This ADR supersedes §3 to §5 of ADR 0404
(the-landing-page-is-a-split-hero…) and sets the words on `/`. ADR 0404's
split hero (§1) and Learn band (§2) stand.

## Decision

`/` is still written for the stranger deciding whether to try mugicrew. Its
words are the human's, verbatim. The human read the few lines the crew
proposed (the Learn band, the facts line and the Try link) and adopted them as
their own. Top to bottom:

1. **Split hero.** On the left:
   - **mugicrew** (the h1)
   - *Build software with decisions instead of code.* (the tagline, also the
     page description)
   - *Free and open-source forever.*
   - the install line `npm i -g @jayulabs/mugicrew`
   - a light line: *Beta · Linux and macOS ·* followed by *Full install guide →*,
     linking to `/try/install`
   - *Try it →*, linking to `/try`

   On the right: the hero-loop recording, as ADR 0404 §1 describes.
2. **Learn band.** It links to `/learn` and reads *First time with mugicrew?*,
   then *Start learning →*.
3. **Five sections**, in a two-column grid that stacks below about 900px. Each
   heading starts with a verb.
   - **Capture your ideas as signals.** "Anything can be a signal. A vague idea,
     a passing thought, an error log, a full spec—a signal can be as big or
     small as you want. Agents will help you refine the signals into actionable
     goals."
   - **Chart the course and clear the fog.** "Your agent keeps track of key
     decisions until it reaches a shared understanding of your goal."
   - **Let agents handle everything else.** "No need to manually delegate—the
     agents will keep track of what's next. You only need to pay attention to
     things that actually need your judgment."
   - **Work on any codebase.** "Use it on any new or existing codebase, mono-repo
     or nested multi-repo, and any language."
   - **Use your existing code harness.** "Runs the same `claude` harness you're
     already using." It is followed by the note "Note: Support for other
     harnesses is planned."
4. **Disclosure line** at the foot of the page: "AI Content Disclosure: No
   generated text was used for any content on this page."

`/` has no lens doors, spine diagram, worked example, list of human gates or
prerequisites block. The full install guide carries the tool versions.

## Considered Options

- **The crew's drafted blocks** (the problem beside the spine, a worked example
  of one Signal, a numbered list of the human gates, a prerequisites block):
  rejected. The human wanted the page in their own voice, and the lenses and
  Learn already explain the spine and the gates in depth.
- **Keeping the three lens doors** (ADR 0363, explainer-is-pick-your-lens…): the
  human dropped them because they were "fighting the design too much".
  Retiring the lens pages site-wide is beyond this Chart's page and goes to an
  offshoot Signal.
- **"This site" in the disclosure**: rejected for now. The lens pages and `/try`
  are crew-written, so the line covers only this page until an offshoot Signal
  rewrites the rest.

## Consequences

The disclosure is a promise about `/`. Any later crew-authored text on `/`
needs the human to write it or adopt it, or the line has to change.
