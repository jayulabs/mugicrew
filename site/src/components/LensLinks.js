import React from 'react';
import Link from '@docusaurus/Link';

const LENSES = [
  { to: '/workspace', label: 'workspace' },
  { to: '/framework', label: 'framework' },
  { to: '/game', label: 'game' },
];

// Every lens ends at Try it and offers the other two lenses, in
// workspace → framework → game order (FR 7).
export default function LensLinks({ current }) {
  return (
    <>
      <p><Link to="/try">Try it</Link></p>
      <p>
        {LENSES.filter((l) => l.to !== `/${current}`).map((l, i) => (
          <React.Fragment key={l.to}>
            {i > 0 ? ' · ' : null}
            <Link to={l.to}>See it as a {l.label}</Link>
          </React.Fragment>
        ))}
      </p>
    </>
  );
}
