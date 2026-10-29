import React from 'react';
import Figure, { ArrowHead } from './Figure';

const STEPS = [
  ['Signal', 'something to chase', 60],
  ['Chart', 'clear the fog', 220],
  ['Decide', 'your judgment', 380],
  ['Land', 'a commit on main', 540],
];

// The game loop: Signal → chart → decide → land, and landing one change
// often turns up new Signals that start the loop again.
export default function LoopDiagram() {
  return (
    <Figure
      title="The loop: a Signal is charted, you decide, the change lands, and landing raises new Signals"
      caption="Signal → chart → decide → land → new Signals: landing one change often charts the next."
      viewBox="0 0 720 220"
    >
      {(id) => (
        <>
          <ArrowHead id={`${id}-arrow`} />
          {STEPS.map(([name, sub, x], i) => {
            const hot = name === 'Decide';
            return (
              <g key={name}>
                <rect className={`chart-figure__node${hot ? ' chart-figure__hot' : ''}`} x={x} y="30" width="120" height="58" rx="8" />
                <text className={hot ? 'chart-figure__on-hot' : 'chart-figure__text'} x={x + 60} y="57" textAnchor="middle">{name}</text>
                <text className="chart-figure__small" x={x + 60} y="74" textAnchor="middle" style={hot ? { fill: 'var(--parchment)', opacity: 0.8 } : undefined}>{sub}</text>
                {i < 3 ? <path className="chart-figure__edge" d={`M${x + 122} 59 L${x + 156} 59`} markerEnd={`url(#${id}-arrow)`} /> : null}
              </g>
            );
          })}
          <path className="chart-figure__route" d="M600 90 C600 190 120 190 120 92" markerEnd={`url(#${id}-arrow)`} />
          <text className="chart-figure__text" x="360" y="178" textAnchor="middle">new Signals</text>
        </>
      )}
    </Figure>
  );
}
