import React from 'react';
import Layout from '@theme/Layout';
import LensLinks from '../components/LensLinks';
import ChainDiagram from '../components/ChainDiagram';
import MembershipDiagram from '../components/MembershipDiagram';
import Recording from '../components/Recording';
import BranchTopology from '../components/BranchTopology';

const README = 'https://github.com/jayulabs/mugicrew/blob/main/README.md';

export default function Page() {
  return (
    <Layout title="The technical framework" description="How does it work under the hood?">
      <main className="container margin-vert--lg">
        <h1>The technical framework</h1>
        <p><em>How does it work under the hood?</em></p>

        <section>
          <h2>A Checkpoint Producer over git and bd</h2>
          <p>
            mugicrew is a Checkpoint Producer: it turns an idea into a candidate next state of your
            repo, built on plain git and the bd issue tracker. It runs in any git repo where bd
            resolves a Bead Graph, so there is no server to host and no database of its own.
          </p>
          <p>
            Its crew of agents never call each other. Every crewmate reads its work from a bead
            and writes its result back to the graph, so the graph is the only channel between them.
            Anything one agent needs to hand another, it files as a bead.
          </p>
        </section>

        <section>
          <h2>Chain, not mutation</h2>
          <p>
            A unit of work is a chain of distinct beads, not one bead that changes shape. When work
            crosses a stage boundary, a new bead is created in succession to the old one: the
            Signal is followed by a Chart, then a Spec, then a Checkpoint. A bead&apos;s kind never
            changes, and so neither does the crewmate that owns it.
          </p>
          <p>
            The predecessor closes but is kept. Closed beads are the lineage, so you can always
            walk back from a landed commit to the idea that started it.
          </p>
          <ChainDiagram />
        </section>

        <section>
          <h2>Two relationships, two jobs</h2>
          <p>
            Parent/child is membership: it says which beads belong to the same Checkpoint or
            Chart. Blocking edges are ordering: they say what must finish first, and they alone
            decide which beads are ready to be worked.
          </p>
          <p>
            Reading a graph is then simple. Look at the frame to see what belongs together, and at
            the blocking arrows to see what runs next.
          </p>
          <MembershipDiagram />
        </section>

        <section>
          <h2>Branch topology</h2>
          <p>
            Each Build works on its own effort branch. Leaves integrate: an effort branch is
            squashed into its container, a chart/ or checkpoint/ branch. Only containers land. When
            a Checkpoint is approved, its container fast-forwards onto your mainline, so nothing
            reaches main until the whole candidate is ready.
          </p>
          <BranchTopology />
        </section>

        <section>
          <h2>The AFK loop</h2>
          <p>
            The AFK loop dispatches unattended work: it finds beads that are ready, hands each to
            a crewmate in its own worktree, and integrates the result. Human-in-the-loop sessions,
            such as grilling or approval, run in tmux windows beside the TUI, so you and the loop
            work the same graph at the same time. <code>mugicrew ps</code> lists the live efforts.
          </p>
          <Recording name="loop-start-ps">
            starting the loop, then <code>mugicrew ps</code> showing live efforts.
          </Recording>
        </section>

        <section>
          <h2>Observable by design</h2>
          <p>
            Everything the crew does is written to an event log. From it mugicrew reports session
            cost, agent time and stats per kind or per profile, so you can see what a chart, a
            build or a whole Checkpoint cost before you decide to do the next one.
          </p>
          <Recording name="tui-stats">
            the TUI Stats tab.
          </Recording>
        </section>

        <section>
          <h2>Configurable where it matters</h2>
          <p>
            The knobs exist where they matter: Agent Profiles choose the model and effort behind
            each crewmate, land strategies choose how a Checkpoint reaches main, and Contexts
            scope the vocabulary and decisions of a part of your repo. They are documented in the{' '}
            <a href={README}>README on GitHub</a>, not here.
          </p>
        </section>

        <LensLinks current="framework" />
      </main>
    </Layout>
  );
}
