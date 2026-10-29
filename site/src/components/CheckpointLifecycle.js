import React from 'react';
import Figure, { ArrowHead } from './Figure';

const STATES = [
  ['cut', 'branched from main'],
  ['in review', 'Builds and Reviews accrue'],
  ['approved', 'you authorize the land'],
  ['landed', 'fast-forwarded into main'],
];
const XS = [10, 185, 360, 535];

// The Checkpoint lifecycle: cut → in review → approved → landed, with the
// Review able to send it back for more Builds or Critiques.
export default function CheckpointLifecycle() {
  return (
    <Figure
      title="Checkpoint lifecycle: cut, in review, approved, landed"
      caption="A Checkpoint is cut from main, collects Builds and Reviews, waits for your approval, and only then lands on main."
      viewBox="0 0 700 200"
    >
      {(id) => (
        <>
          <ArrowHead id={`${id}-arrow`} />
          {STATES.map(([name, sub], i) => {
            const hot = i === 3;
            return (
              <g key={name}>
                <rect className={`chart-figure__node${hot ? ' chart-figure__hot' : ''}`} x={XS[i]} y="30" width="155" height="62" rx="8" />
                <text className={hot ? 'chart-figure__on-hot' : 'chart-figure__text'} x={XS[i] + 77} y="58" textAnchor="middle">{name}</text>
                <text className="chart-figure__small" x={XS[i] + 77} y="77" textAnchor="middle" style={hot ? { fill: 'var(--parchment)', opacity: 0.8 } : undefined}>{sub}</text>
              </g>
            );
          })}
          {XS.slice(0, 3).map((x) => (
            <path key={x} className="chart-figure__edge" d={`M${x + 157} 61 L${x + 181} 61`} markerEnd={`url(#${id}-arrow)`} />
          ))}
          <path className="chart-figure__route" d="M437 94 C437 150 262 150 262 96" markerEnd={`url(#${id}-arrow)`} />
          <text className="chart-figure__small" x="350" y="160" textAnchor="middle">a Critique sends it back for more work</text>
        </>
      )}
    </Figure>
  );
}
