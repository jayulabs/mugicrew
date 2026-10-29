import React from 'react';
import Figure, { ArrowHead } from './Figure';

const STAGES = [
  ['Signal', 'an idea, a log'],
  ['Chart', 'clear the fog'],
  ['Spec', 'one plan'],
  ['Checkpoint', 'a candidate state'],
  ['Build', 'the work'],
];
const XS = [10, 150, 290, 430, 570];

// The spine: Signal → Chart → Spec → Checkpoint → Build, then the
// Checkpoint lands on main.
export default function SpineDiagram() {
  return (
    <Figure
      title="The spine: Signal, Chart, Spec, Checkpoint, Build, land"
      caption="Signal → Chart → Spec → Checkpoint → Build, then the Checkpoint lands on main."
      viewBox="0 0 700 220"
    >
      {(id) => (
        <>
          <ArrowHead id={`${id}-arrow`} />
          {STAGES.map(([name, sub], i) => {
            const hot = i === 3;
            return (
              <g key={name}>
                <rect className={`chart-figure__node${hot ? ' chart-figure__hot' : ''}`} x={XS[i]} y="30" width="120" height="58" rx="8" />
                <text className={hot ? 'chart-figure__on-hot' : 'chart-figure__text'} x={XS[i] + 60} y="57" textAnchor="middle">{name}</text>
                <text className="chart-figure__small" x={XS[i] + 60} y="74" textAnchor="middle" style={hot ? { fill: 'var(--parchment)', opacity: 0.8 } : undefined}>{sub}</text>
              </g>
            );
          })}
          {XS.slice(0, 4).map((x) => (
            <path key={x} className="chart-figure__edge" d={`M${x + 122} 59 L${x + 146} 59`} markerEnd={`url(#${id}-arrow)`} />
          ))}
          <path className="chart-figure__route" d="M630 90 C630 130 520 130 500 92" markerEnd={`url(#${id}-arrow)`} />
          <text className="chart-figure__small" x="580" y="138" textAnchor="middle">integrates</text>
          <path className="chart-figure__main" d="M20 190 L680 190" />
          <text className="chart-figure__small" x="20" y="212">main</text>
          <path className="chart-figure__edge" d="M490 90 L490 182" markerEnd={`url(#${id}-arrow)`} style={{ stroke: 'var(--accent)', strokeWidth: 2.5 }} />
          <text className="chart-figure__text" x="500" y="160">lands</text>
        </>
      )}
    </Figure>
  );
}
