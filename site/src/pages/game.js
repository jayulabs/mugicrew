import React from 'react';
import Layout from '@theme/Layout';
import FogChart from '../components/FogChart';
import LoopDiagram from '../components/LoopDiagram';
import Recording from '../components/Recording';
import LensLinks from '../components/LensLinks';

const TITLE = 'The creative strategy game';
const INTRO = 'Why does using it feel like playing a strategy game?';

export default function Page() {
  return (
    <Layout title={TITLE} description={INTRO}>
      <main className="container margin-vert--lg">
        <h1>{TITLE}</h1>
        <p>{INTRO}</p>

        <section>
          <h2>Your experience</h2>
          <p>
            Building with mugicrew is a strategy game, and the claim is literal. You have
            requirements you want to see in your repo, and you manifest them by judging: choosing
            what to chase, answering what is unclear, picking between options, approving or
            sending work back. You play with judgment rather than keystrokes; the crew does the
            typing.
          </p>
        </section>

        <section>
          <h2>The bead graph is the map</h2>
          <p>
            Everything mugicrew knows lives in the bead graph, and it reads like a sea chart. A
            Chart is a stretch of water. Open questions are fog. Each answer you give clears a
            patch, until a route of cleared waypoints runs from where you started to the
            Destination.
          </p>
          <FogChart />
          <Recording name="graph-map">the mugicrew TUI Graph tab as the map.</Recording>
        </section>

        <section>
          <h2>Your moves</h2>
          <p>These are the decisions you actually make:</p>
          <ul>
            <li>Which Signals to chase, and which to leave in the Inbox.</li>
            <li>Grilling answers that clear the fog on a Chart.</li>
            <li>Prototype picks, when a question is easier to feel than to describe.</li>
            <li>Approve or send back, at each point where the crew stops for you.</li>
            <li>Withdraw work you no longer want, or roll back a landed change that broke something.</li>
          </ul>
        </section>

        <section>
          <h2>What winning is</h2>
          <p>
            You win when your requirements, as you judged them, become landed commits on your main
            branch. The goal is concrete: not a plan, not a branch, but the change in your repo.
          </p>
        </section>

        <section>
          <h2>Resources and pacing</h2>
          <p>
            Every game has supplies. Crew capacity is the AFK pool, the limit on how many efforts
            can run at once. Plan usage and session cost are what you spend each time the crew
            works. They set the pace, so you choose where to spend them.
          </p>
        </section>

        <section>
          <h2>The loop</h2>
          <p>
            A Signal is charted, you decide, the change lands, and landing it often shows you the
            next thing worth doing. Those are new Signals, and the loop starts again.
          </p>
          <LoopDiagram />
        </section>

        <LensLinks current="game" />
      </main>
    </Layout>
  );
}
