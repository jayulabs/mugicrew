import React from 'react';
import Figure, { ArrowHead } from './Figure';

const BUILDS = [
  [120, 'Build 1'],
  [320, 'Build 2'],
  [520, 'Build 3'],
];

// Two relationships, two jobs: parent/child draws the container around its
// members; blocking edges order them and decide what is ready.
export default function MembershipDiagram() {
  return (
    <Figure
      title="Membership versus ordering: a Checkpoint contains three Builds (parent and child), while blocking edges order them Build 1, then 2, then 3"
      caption="Membership (the frame) says what belongs together; blocking edges (the arrows) say what is ready next."
      viewBox="0 0 700 220"
    >
      {(id) => (
        <>
          <ArrowHead id={`${id}-arrow`} />
          <rect className="chart-figure__node" x="20" y="20" width="660" height="180" rx="10" style={{ strokeDasharray: '2 5' }} />
          <text className="chart-figure__text" x="36" y="46">Checkpoint · parent of its Builds</text>
          {BUILDS.map(([x, label]) => (
            <g key={label}>
              <rect className="chart-figure__node" x={x - 70} y="90" width="140" height="56" rx="8" />
              <text className="chart-figure__text" x={x} y="123" textAnchor="middle">{label}</text>
            </g>
          ))}
          <path className="chart-figure__edge" d="M192 118 L248 118" markerEnd={`url(#${id}-arrow)`} style={{ stroke: 'var(--accent)', strokeWidth: 2.5 }} />
          <path className="chart-figure__edge" d="M392 118 L448 118" markerEnd={`url(#${id}-arrow)`} style={{ stroke: 'var(--accent)', strokeWidth: 2.5 }} />
          <text className="chart-figure__small" x="220" y="108" textAnchor="middle">blocks</text>
          <text className="chart-figure__small" x="420" y="108" textAnchor="middle">blocks</text>
          <text className="chart-figure__small" x="350" y="182" textAnchor="middle">Build 1 done → Build 2 is ready; Build 3 waits</text>
        </>
      )}
    </Figure>
  );
}
