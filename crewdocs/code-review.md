<!-- from mattpocock/skills@5c89081d4bbeb3d039a42093653f90bb698d780e skills/engineering/code-review/SKILL.md · lock-hash caa9a086baaf9e0f7cd71f64edfa83da6821c05e826b083221f3d02e3d6a1905 · distilled -->

# Two-axis code review

Review a diff along two axes:

- **Standards**: does the code conform to this repo's documented coding
  standards?
- **Spec**: does the code faithfully implement the originating Spec?

Your prompt supplies the fixed point (the diff to review), the Spec source and
what to do with the findings. This doc is the review itself.

## Standards sources

Anything in the repo that documents how code should be written, such as
CODING_STANDARDS.md or CONTRIBUTING.md.

On top of whatever the repo documents, the Standards axis always carries the
**smell baseline** below: a fixed set of Fowler code smells (_Refactoring_,
ch.3) that applies even when a repo documents nothing. Two rules bind it:

- **The repo overrides.** A documented repo standard always wins; where it
  endorses something the baseline would flag, suppress the smell.
- **Always a judgement call.** Each smell is a labelled heuristic ("possible
  Feature Envy"), never a hard violation. Like any standard here, skip anything
  tooling already enforces.

Each smell reads _what it is_ → _how to fix_; match it against the diff:

- **Mysterious Name**: a function, variable, or type whose name doesn't reveal what it does or holds. → rename it; if no honest name comes, the design's murky.
- **Duplicated Code**: the same logic shape appears in more than one hunk or file in the change. → extract the shared shape, call it from both.
- **Feature Envy**: a method that reaches into another object's data more than its own. → move the method onto the data it envies.
- **Data Clumps**: the same few fields or params keep travelling together (a type wanting to be born). → bundle them into one type, pass that.
- **Primitive Obsession**: a primitive or string standing in for a domain concept that deserves its own type. → give the concept its own small type.
- **Repeated Switches**: the same `switch`/`if`-cascade on the same type recurs across the change. → replace with polymorphism, or one map both sites share.
- **Shotgun Surgery**: one logical change forces scattered edits across many files in the diff. → gather what changes together into one module.
- **Divergent Change**: one file or module is edited for several unrelated reasons. → split so each module changes for one reason.
- **Speculative Generality**: abstraction, parameters, or hooks added for needs the spec doesn't have. → delete it; inline back until a real need shows.
- **Message Chains**: long `a.b().c().d()` navigation the caller shouldn't depend on. → hide the walk behind one method on the first object.
- **Middle Man**: a class or function that mostly just delegates onward. → cut it, call the real target direct.
- **Refused Bequest**: a subclass or implementer that ignores or overrides most of what it inherits. → drop the inheritance, use composition.

## Run the two axes

Keep the axes separate so neither pollutes the other's context: if your harness
can, dispatch one sub-agent per axis, in parallel, and give each everything
below (it has no other access to it); otherwise run them inline, one after the
other, finishing the Standards pass before you start the Spec pass.

**Standards axis.** Give it the diff command and commit list, the standards
sources you found, and the smell baseline above in full. Brief: report, per
file/hunk where relevant, (a) every place the diff violates a documented
standard, citing the standard (file + the rule); and (b) any baseline smell,
naming it and quoting the hunk. Distinguish hard violations from judgement
calls: documented-standard breaches can be hard, but baseline smells are always
judgement calls, and a documented repo standard overrides the baseline. Skip
anything tooling enforces. Under 400 words.

**Spec axis.** Give it the diff command and commit list and the Spec's
contents. Brief: report (a) requirements the Spec asked for that are missing or
partial; (b) behaviour in the diff that wasn't asked for (scope creep); (c)
requirements that look implemented but where the implementation looks wrong.
Quote the Spec line for each finding. Under 400 words.

With no Spec but a **Surgery** (a human-dictated change on its own Checkpoint),
the Surgery bead's description and comments are the intent source instead. Run
the Surgery check: report only (a) a stated change that is missing or partial
and (c) a stated change that is implemented wrong, quoting the Surgery line;
there is no scope-creep check, since the human dictated the diff. With neither
a Spec nor a Surgery, skip this axis and report "no spec available".

## Keep the axes apart

Do **not** merge or rerank findings across the axes. A change can pass one and
fail the other:

- Code that follows every standard but implements the wrong thing → **Standards
  pass, Spec fail.**
- Code that does exactly what the Spec asked but breaks the project's
  conventions → **Spec pass, Standards fail.**

Judging them separately stops one axis from masking the other.
