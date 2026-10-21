<!-- from mattpocock/skills@321658273cb1d20b76026717d027d505790106d4 skills/engineering/prototype/SKILL.md · lock-hash d3fc74689bd993e39c44bfae510d014eecffbeec2922764b065377fd1327c0f6 · distilled -->

# Prototype

A prototype is **throwaway code that answers a question**. The question decides
the shape. Everything it produces, the throwaway and any code folded from it,
is pushed to the side ref your prompt names, never to a branch that merges.

## Pick a branch

Identify which question is being answered, from the bead, the surrounding code,
or by asking the human:

- **"Does this logic / state model feel right?"** → the LOGIC branch below. Build
  a single shareable HTML file (free-play buttons plus tabbed guided
  walkthroughs) that pushes the state machine through cases that are hard to
  reason about on paper, and that a non-developer can drive.
- **"What should this look like?"** → the UI branch below. Generate several
  radically different UI variations on a single route, switchable via a URL
  search param and a floating bottom bar.

The two branches produce very different artifacts, so getting this wrong wastes
the whole prototype. Ask the human when the question is ambiguous; if they
aren't reachable, default to whichever branch better matches the surrounding
code (a backend module → logic; a page or component → UI) and state the
assumption at the top of the prototype.

## Rules that apply to both

1. **Throwaway from day one, and clearly marked as such.** Locate the prototype
   code close to where it will actually be used (next to the module or page it's
   prototyping for) so context is obvious, but name it so a casual reader can see
   it's a prototype, not production. For throwaway UI routes, obey whatever
   routing convention the project already uses; don't invent a new top-level
   structure.
2. **Trivial to run.** A UI prototype starts from one command in the project's
   task runner: `pnpm <name>`, `python <path>`, `bun <path>`, etc. A logic demo
   is a single self-contained HTML file opened through a static server at
   hand-over. Either way, no thinking required to start it.
3. **No persistence by default.** State lives in memory. Persistence is the thing
   the prototype is _checking_, not something it should depend on. If the
   question explicitly involves a database, hit a scratch DB or a local file with
   a clear "PROTOTYPE, wipe me" name.
4. **Skip the polish.** No tests, no error handling beyond what makes the
   prototype _runnable_, no abstractions. The point is to learn something fast.
5. **Surface the state.** After every action (logic) or on every variant switch
   (UI), print or render the full relevant state so the human can see what
   changed.
6. **Capture it when done.** Commit the prototype, with any code folded from it,
   in its throwaway tree and push it to the side ref as a **primary source**.
   Record the answer (the verdict and the question it settled) on the bead. What
   merges is only the validated decision, written as a doc, never the code.

## The LOGIC branch

A single, self-contained HTML file (a **shareable demo**) that lets anyone drive
a state model by clicking buttons. Use it when the question is about **business
logic, state transitions, or data shape**: the kind of thing that looks
reasonable on paper but only feels wrong once you push it through real cases.
Being one file with nothing to install, a non-developer can feel the model for
themselves, so it speaks their language, not the code's.

1. **State the question.** Before writing code, write down what state model and
   what question you're prototyping: one paragraph, in a visible intro at the
   top of the demo, so it can be checked later.
2. **Isolate the logic in a portable module.** Put the logic that answers the
   question in a single `<script>` block written as a small, pure module that
   could be lifted into the real codebase later. Pick the shape that fits the
   question, not the easiest to wire to a page: a pure reducer
   `(state, action) => state`; a state machine with explicit states and
   transitions (when "which actions are legal now" is part of the question); a
   small set of pure functions over a plain data type; or a class with a clear
   method surface when the logic genuinely owns ongoing state. Keep it pure: no
   DOM, no `document`, no button handlers reaching inside it. The page calls into
   it; nothing flows the other direction.
3. **Build the shareable HTML file.** One file, plain HTML/CSS/JS: no framework,
   no bundler, no server, everything inline. Every label is in **domain
   language**, not code. Lay it out top to bottom: the title and a one-line
   explanation (the question); the **current state** as a readable panel
   (labelled fields, not a raw JSON dump), re-rendered after every click, calling
   out what just changed; **free-play buttons**, one per action, always
   available; **guided walkthroughs**, one scenario per tab, each a short
   plain-language description plus the ordered buttons to press, starting from a
   reset known state. Pick scenarios that show the awkward cases: the happy
   path, a tricky edge case, an attempt at something that should be illegal.
   Clean typography, generous spacing, one accent colour; no animations.
4. **Hand it over.** Open it for the human. The interesting moments are "wait,
   that shouldn't be possible" or "huh, I assumed X": those are the bugs in the
   _idea_. Add actions or scenarios as they ask.
5. **Capture the answer and the prototype.** Once it has answered its question,
   record the answer, then push the whole prototype to the side ref as rule 6
   says: the HTML shell and the validated reducer / machine / function set
   together, the module lifted into the real module's place in that tree if the
   human wants it shaped for later. Being one file, it stays re-runnable there.

Anti-patterns: tests; the real database (use in-memory state unless the question
is about persistence); generalising for "what if we support X later"; logic that
touches the DOM; a framework, bundler or server; shipping the HTML shell into
production (the logic module is the bit worth keeping).

## The UI branch

Generate **several radically different UI variations** on a single route,
switchable from a floating bottom bar. The human flips between variants, picks
one (or steals bits from each), and the rest are thrown away.

**Sub-shapes: strongly prefer A.** A UI prototype is easier to judge **butting
up against the rest of the app** (real header, sidebar, data, density); a route
on its own is a vacuum where every variant looks fine.

- **A: adjustment to an existing page (preferred).** Variants render on the same
  route, gated by a `?variant=` search param; the existing data fetching, params
  and auth stay, only the rendering swaps. Something that would naturally live
  inside an existing page (a new section, card or step) is still A: mount the
  variants inside the host page.
- **B: a new page (last resort).** Only when nothing has a sensible host page: a
  throwaway route following the project's routing convention, named so it's
  obviously a prototype (e.g. `/prototype/<name>`), same `?variant=` pattern.
  Sanity-check first: is there really no existing page to embed it in?

1. **State the question and pick N.** Default to **3 variants**, cap at 5. Write
   the plan in one line in a top-of-file comment, e.g. "Three variants of the
   settings page, switchable via `?variant=`, on the existing settings route."
2. **Generate radically different variants.** Each uses the page's purpose and
   data, the project's component library and styling, and a clear exported name
   (`VariantA`, `VariantB`, …). Variants differ in **structure** (layout,
   information hierarchy, primary affordance), not colour. If two come out too
   similar, redo one with explicit "do not use a card grid" guidance.
3. **Wire them together.** One switcher on the route:

   ```tsx
   // pseudo-code, adapt to the project's framework
   const variant = searchParams.get('variant') ?? 'A';
   return (
     <>
       {variant === 'A' && <VariantA {...data} />}
       {variant === 'B' && <VariantB {...data} />}
       {variant === 'C' && <VariantC {...data} />}
       <PrototypeSwitcher variants={['A','B','C']} current={variant} />
     </>
   );
   ```

   In A, keep the existing data fetching above the switcher.
4. **Build the floating switcher.** A fixed bar at the bottom-centre: a left
   arrow (previous, wraps), the variant label (key plus name, e.g.
   `B (Sidebar layout)`), a right arrow (next, wraps). Arrows update the URL
   search param through the framework's router so the variant is shareable and
   reload-stable; `←`/`→` also cycle, except while an `<input>`, `<textarea>` or
   `[contenteditable]` is focused. Visually distinct from the page. Hidden in
   production builds (`process.env.NODE_ENV !== 'production'` or equivalent).
   One shared component, wherever shared UI lives.
5. **Hand it over.** Surface the URL and the `?variant=` keys. The useful
   feedback is usually "the header from B with the sidebar from C": the design
   they actually want.
6. **Capture the answer and clean up.** Once a variant has won, record which and
   why, then push it all to the side ref as rule 6 says: fold the winner into
   the page (A) or promote it to a real route (B) in the throwaway tree, and keep
   the losing variants and the switcher beside it there, as the primary source.
   Nothing of it merges; the decision does, as a doc.

Anti-patterns: variants differing only in colour or copy; a shared `<Layout>`
across variants (a shared `<Header>` is fine); variants wired to real mutations
(point them at a stub); treating variant code as production-ready (it was
written under prototype constraints: rewrite it properly when folded in).
