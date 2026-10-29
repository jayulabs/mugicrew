import React from 'react';
import Figure, { ArrowHead } from './Figure';

const EFFORTS = [60, 120, 180];

// Efforts integrate into a container branch; only the container lands on
// the mainline.
export default function BranchTopology() {
  return (
    <Figure
      title="Branch topology: effort branches integrate into a chart or checkpoint container branch, and only the container lands on main"
      caption="Leaves integrate into their container; only the container lands on main."
      viewBox="0 0 700 250"
    >
      {(id) => (
        <>
          <ArrowHead id={`${id}-arrow`} />
          {EFFORTS.map((y, i) => (
            <g key={y}>
              <rect className="chart-figure__node" x="20" y={y - 20} width="170" height="40" rx="8" />
              <text className="chart-figure__text" x="105" y={y + 5} textAnchor="middle">effort/…{i + 1}</text>
              <path className="chart-figure__edge" d={`M192 ${y} L300 120`} markerEnd={`url(#${id}-arrow)`} />
            </g>
          ))}
          <text className="chart-figure__small" x="250" y="52" textAnchor="middle">integrate</text>
          <rect className="chart-figure__node chart-figure__hot" x="302" y="95" width="190" height="50" rx="8" />
          <text className="chart-figure__on-hot" x="397" y="117" textAnchor="middle">chart/ or checkpoint/</text>
          <text className="chart-figure__small" x="397" y="134" textAnchor="middle" style={{ fill: 'var(--parchment)', opacity: 0.8 }}>container</text>
          <path className="chart-figure__main" d="M20 215 L680 215" />
          <text className="chart-figure__small" x="20" y="238">main (mainline)</text>
          <path className="chart-figure__edge" d="M397 147 L397 207" markerEnd={`url(#${id}-arrow)`} style={{ stroke: 'var(--accent)', strokeWidth: 2.5 }} />
          <text className="chart-figure__text" x="410" y="185">lands</text>
        </>
      )}
    </Figure>
  );
}
