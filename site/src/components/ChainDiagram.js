import React from 'react';
import Figure, { ArrowHead } from './Figure';

const BEADS = [
  ['Signal', 'closed'],
  ['Chart', 'closed'],
  ['Spec', 'closed'],
  ['Checkpoint', 'open'],
];
const XS = [10, 180, 350, 520];

// A unit of work as a chain: each stage is a new bead in succession; the
// predecessors close and stay as lineage.
export default function ChainDiagram() {
  return (
    <Figure
      title="Chain of succession: Signal, Chart, Spec and Checkpoint are four distinct beads, each new bead succeeding a closed predecessor"
      caption="Each stage is a new bead in succession. Kinds never change; closed predecessors stay as lineage."
      viewBox="0 0 700 150"
    >
      {(id) => (
        <>
          <ArrowHead id={`${id}-arrow`} />
          {BEADS.map(([kind, state], i) => {
            const open = state === 'open';
            return (
              <g key={kind}>
                <rect className={`chart-figure__node${open ? ' chart-figure__hot' : ''}`} x={XS[i]} y="30" width="170" height="60" rx="8" style={open ? undefined : { strokeDasharray: '5 4' }} />
                <text className={open ? 'chart-figure__on-hot' : 'chart-figure__text'} x={XS[i] + 85} y="57" textAnchor="middle">{kind}</text>
                <text className="chart-figure__small" x={XS[i] + 85} y="76" textAnchor="middle" style={open ? { fill: 'var(--parchment)', opacity: 0.8 } : undefined}>{state}</text>
              </g>
            );
          })}
          <text className="chart-figure__small" x="350" y="125" textAnchor="middle">each bead succeeds the last · kind and owner never change</text>
        </>
      )}
    </Figure>
  );
}
