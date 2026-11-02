import React from 'react';
import Layout from '@theme/Layout';
import Link from '@docusaurus/Link';
import Recording from '../components/Recording';

const TAGLINE = 'Build software with decisions instead of code.';

const VERBS = [
  {
    heading: 'Capture your ideas as signals.',
    body: 'Anything can be a signal. A vague idea, a passing thought, an error log, a full spec—a signal can be as big or small as you want. Agents will help you refine the signals into actionable goals.',
  },
  {
    heading: 'Chart the course and clear the fog.',
    body: 'Your agent keeps track of key decisions until it reaches a shared understanding of your goal.',
  },
  {
    heading: 'Let agents handle everything else.',
    body: 'No need to manually delegate—the agents will keep track of what\'s next. You only need to pay attention to things that actually need your judgment.',
  },
  {
    heading: 'Work on any codebase.',
    body: 'Use it on any new or existing codebase, mono-repo or nested multi-repo, and any language.',
  },
  {
    heading: 'Use your existing code harness.',
    body: (
      <>
        Runs the same <code>claude</code> harness you&apos;re already using.
      </>
    ),
    note: 'Note: Support for other harnesses is planned.',
  },
];

export default function Home() {
  return (
    <Layout title="mugicrew" description={TAGLINE}>
      <main className="hero-page">
      <div className="container">
        <section className="hero-page__split">
          <div className="hero-page__copy">
            <h1>mugicrew</h1>
            <p id="what-it-is" className="hero-page__lede">{TAGLINE}</p>
            <p>Free and open-source forever.</p>
            <pre className="hero-page__install">
              <code>npm i -g @jayulabs/mugicrew</code>
            </pre>
            <p className="hero-page__facts">
              Beta · Linux and macOS · <Link to="/try/install">Full install guide &rarr;</Link>
            </p>
            <p>
              <Link to="/try">Try it &rarr;</Link>
            </p>
          </div>
          <div className="hero-page__media">
            <Recording
              name="hero-loop"
              label="Recording of the mugicrew TUI moving from the Inbox through a Chart and a Checkpoint to the Graph"
            />
          </div>
        </section>
      </div>
      <Link className="hero-page__learn" to="/learn">
        <span>First time with mugicrew?</span>
        <strong>Start learning &rarr;</strong>
      </Link>
      <div className="container">
        <div className="hero-page__verbs">
          {VERBS.map(({ heading, body, note }) => (
            <section key={heading} className="hero-page__verb">
              <h2>{heading}</h2>
              <p>{body}</p>
              {note && <p className="hero-page__note">{note}</p>}
            </section>
          ))}
        </div>
        <p className="hero-page__disclosure">
          AI Content Disclosure: No generated text was used for any content on this page.
        </p>
      </div>
      </main>
    </Layout>
  );
}
