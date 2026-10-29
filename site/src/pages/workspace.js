import React from 'react';
import Layout from '@theme/Layout';
import FogChart from '../components/FogChart';
import CheckpointLifecycle from '../components/CheckpointLifecycle';
import Recording from '../components/Recording';
import LensLinks from '../components/LensLinks';

export default function Page() {
  return (
    <Layout title="The problem-solving workspace" description="What is it like to get a real problem solved with it?">
      <main className="container margin-vert--lg">
        <h1>The problem-solving workspace</h1>
        <p>What is it like to get a real problem solved with it? You bring the problem. A crew of agents carries it to a commit in your repo, and you decide at each turn.</p>

        <section>
          <h2>Everything starts as a Signal</h2>
          <p>
            An idea, an error log, an alert: whatever you bring becomes a Signal in the Inbox. Nothing is
            built from a Signal directly. Triage comes first, and it has three outcomes. Accept it and it
            goes on to be charted. Fast-lane it when the fix is small and obvious. Reject it when it
            isn't worth doing, and say why.
          </p>
        </section>

        <section>
          <h2>Charting clears the fog</h2>
          <p>
            An accepted Signal is rarely ready to build. A Chart turns what you don't yet know into open
            questions, called defog beads, and each one is cleared in one of three ways: the crew
            interviews you (grilling), an agent researches the answer, or a throwaway prototype settles
            it. Your judgment enters at the grilling and at the decisions the research surfaces. The Chart
            is done when the fog is gone and the route to its Destination is clear.
          </p>
          <FogChart />
          <Recording name="grilling">a grilling session answering one question.</Recording>
        </section>

        <section>
          <h2>One Chart, one Spec</h2>
          <p>
            A defogged Chart becomes exactly one Spec: a single document that says what is being built
            and why. The Spec is then sliced into Builds, each a small vertical piece of work that an
            agent can finish test-first. The plan is one concrete document you can read, not a pile of
            tickets.
          </p>
        </section>

        <section>
          <h2>A Checkpoint is a candidate next state</h2>
          <p>
            Builds do not touch your main branch. They accrue on a Checkpoint: a branch that is a
            candidate next state of your repo, where Builds, Reviews and Critiques pile up until the
            whole is ready. Nothing reaches main until the Checkpoint is approved and lands.
          </p>
          <CheckpointLifecycle />
        </section>

        <section>
          <h2>HITL is authorization, not authorship</h2>
          <p>
            You never have to write the code, but you stay in control. When a Checkpoint is ready, the
            crew prepares a Decision Packet: the case for the land, with what changed, what was
            reviewed and what is still open. You read it and approve the land, or send it back.
            Human-in-the-loop here means you authorize each turn; the crew does the authoring.
          </p>
          <Recording name="approval">an approval with its Decision Packet.</Recording>
        </section>

        <section>
          <h2>When something goes wrong</h2>
          <p>
            The happy path is not the only path. If the Spec turns out to be wrong, a Pivot corrects the
            plan instead of improvising around it. If a landed change was a mistake, a Rollback undoes
            it. If the bead graph itself gets tangled, Chopper repairs it. Each has a named move, so
            you are not betting your repo on everything going right.
          </p>
        </section>

        <section>
          <h2>Ask questions any time</h2>
          <p>
            You are never stuck guessing. Start a Discussion with Robin and she explains your Workspace
            from cited sources: the code, the docs, the decisions on record. She can also draft a Signal
            from what you talk through, so a good question can become the next piece of work.
          </p>
        </section>

        <section>
          <h2>Initiatives group Checkpoints</h2>
          <p>
            A bigger goal doesn't need a bigger plan, only more chains. An Initiative groups several
            Signal-to-landed chains into phases toward one finish line, so the same loop that solves one
            problem scales to a larger one.
          </p>
        </section>

        <LensLinks current="workspace" />
      </main>
    </Layout>
  );
}
