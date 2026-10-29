import React from 'react';
import Layout from '@theme/Layout';
import Link from '@docusaurus/Link';
import SpineDiagram from '../components/SpineDiagram';
import Recording from '../components/Recording';

const DOORS = [
  {
    to: '/workspace',
    course: 'Course 01',
    name: 'The problem-solving workspace',
    question: 'What is it like to get a real problem solved with it?',
  },
  {
    to: '/framework',
    course: 'Course 02',
    name: 'The technical framework',
    question: 'How does it work under the hood?',
  },
  {
    to: '/game',
    course: 'Course 03',
    name: 'The creative strategy game',
    question: 'Why does using it feel like playing a strategy game?',
  },
];

const TAGLINE =
  'A crew of agents that carries an idea to a landed commit in your repo, with you approving each turn.';

export default function Home() {
  return (
    <Layout title="mugicrew" description={TAGLINE}>
      <main className="container margin-vert--lg hero-page">
        <section>
          <h1>mugicrew</h1>
          <h2 id="what-it-is">What mugicrew is, in one breath</h2>
          <p className="hero-page__lede">{TAGLINE}</p>
          <p>
            You hand it a rough idea, an error log or an alert. The crew charts the open questions,
            writes one plan, builds it on a branch and reviews it, and nothing reaches your main
            branch until you say so.
          </p>
        </section>

        <section>
          <h2>The spine, at a glance</h2>
          <p>
            Every piece of work travels the same road. A Signal is charted into a plan, the plan
            becomes a Checkpoint, and the Checkpoint is built, reviewed and landed.
          </p>
          <SpineDiagram />
        </section>

        <section>
          <h2>See it running</h2>
          <p>One Signal moving from the Inbox, through a Chart and a Checkpoint, to the Graph.</p>
          <Recording
            name="hero-loop"
            label="Recording of the mugicrew TUI moving from the Inbox through a Chart and a Checkpoint to the Graph"
          />
        </section>

        <section>
          <h2>Install it</h2>
          <p>One command, then you are in the crew.</p>
          <pre className="hero-page__install">
            <code>npm i -g @jayulabs/mugicrew</code>
          </pre>
          <p>
            <Link to="/try">Try it &rarr;</Link>
          </p>
        </section>

        <section>
          <h2 id="doors">One machine, three ways to see it</h2>
          <p>
            These are three lenses on the same machine, not three products. Pick the question that
            brought you here.
          </p>
          <ol className="hero-page__doors">
            {DOORS.map((d) => (
              <li key={d.to}>
                <Link className="hero-page__door" to={d.to}>
                  <span className="hero-page__course">{d.course}</span>
                  <strong>{d.name}</strong>
                  <em>{d.question}</em>
                </Link>
              </li>
            ))}
          </ol>
        </section>
      </main>
    </Layout>
  );
}
