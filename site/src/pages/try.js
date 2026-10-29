import React from 'react';
import Layout from '@theme/Layout';
import Link from '@docusaurus/Link';
import Recording from '../components/Recording';

const REPO = 'https://github.com/jayulabs/mugicrew/blob/main/';

export default function Try() {
  return (
    <Layout title="Try it" description="Three commands to your first Signal.">
      <main className="container margin-vert--lg">
        <h1>Try it</h1>
        <p>Three commands to your first Signal.</p>

        <section>
          <h2>Three commands</h2>
          <p>Install mugicrew, set up your repo, then ask it whether everything is ready.</p>
          <pre><code>npm i -g @jayulabs/mugicrew</code></pre>
          <pre><code>mugicrew init</code></pre>
          <pre><code>mugicrew doctor</code></pre>
          <p>
            If <code>mugicrew doctor</code> reports problems, run <code>mugicrew doctor fix</code> and
            work through any manual steps it lists. Repeat until doctor passes.
          </p>
          <Recording name="install">install, init, doctor and doctor fix, start to finish.</Recording>
        </section>

        <section>
          <h2>Once per repo</h2>
          <p>
            <code>mugicrew init workspace</code> sets a repo up as a Workspace, and it runs once per repo.
            Land it from a worktree, like any other change, so your main branch only ever moves by a
            reviewed land.
          </p>
        </section>

        <section>
          <h2>Your first Signal</h2>
          <p>
            File a Signal for something you want done, triage it from the Inbox, and watch the crew chart
            it. Accepting a Signal starts the Chart; from there the crew asks you questions and you
            approve each turn.
          </p>
          <Recording name="first-signal">filing and triaging a Signal in the TUI.</Recording>
        </section>

        <section>
          <h2>The full install guide</h2>
          <p>
            Prerequisites, tool versions and troubleshooting live in the{' '}
            <Link to="/try/install">full install guide</Link>.
          </p>
        </section>

        <section id="where-next">
          <h2>Where next</h2>
          <ul>
            <li><Link to="/try/install">The full install guide</Link>, on this site.</li>
            <li><a href={`${REPO}README.md`}>The README</a>, for how a Workspace is configured and operated.</li>
            <li><a href={`${REPO}GLOSSARY.md`}>The GLOSSARY</a>, for the terms the crew uses.</li>
            <li><a href="https://www.npmjs.com/package/@jayulabs/mugicrew">The npm package</a>.</li>
          </ul>
        </section>
      </main>
    </Layout>
  );
}
