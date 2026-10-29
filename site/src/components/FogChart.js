import React from 'react';
import Figure, { ArrowHead } from './Figure';

const CLEARED = [
  [220, 150, 'where the site lives'],
  [370, 200, 'which generator'],
  [500, 130, 'content outline'],
];
const FOG = [
  [300, 70, 'visual style?'],
  [460, 245, 'TUI recordings?'],
  [150, 60, 'site shape?'],
];
const GRID_Y = [40, 80, 120, 160, 200, 240, 280];
const GRID_X = [90, 180, 270, 360, 450, 540, 630];

// The bead graph as a sea chart: cleared questions are charted waters, open
// ones are blurred fog, a route runs through the cleared waypoints to the
// Destination flag.
export default function FogChart() {
  return (
    <Figure
      title="A Chart as a sea chart with fog: a route from the Signal through cleared waypoints to the Destination"
      caption="Cleared questions are charted waters; open ones are fog that your answers clear on the way to the Destination."
      viewBox="0 0 720 300"
    >
      {(id) => (
        <>
          <ArrowHead id={`${id}-arrow`} />
          <defs>
            <filter id={`${id}-blur`} x="-30%" y="-30%" width="160%" height="160%">
              <feGaussianBlur stdDeviation="9" />
            </filter>
          </defs>
          <rect className="chart-figure__sea" x="0" y="0" width="720" height="300" rx="6" />
          {GRID_Y.map((y) => <path key={y} className="chart-figure__grid" d={`M0 ${y} L720 ${y}`} />)}
          {GRID_X.map((x) => <path key={x} className="chart-figure__grid" d={`M${x} 0 L${x} 300`} />)}
          <path className="chart-figure__land" d="M610 40 C660 30 710 60 705 110 C700 170 690 240 640 262 C600 280 575 230 590 190 C600 150 570 120 585 80 C590 60 595 45 610 40 Z" />
          <path className="chart-figure__route" d="M50 230 C120 210 150 150 220 150 C290 150 300 210 370 200 C420 192 440 140 500 130 C540 124 570 140 600 150" markerEnd={`url(#${id}-arrow)`} />
          <polygon className="chart-figure__hot" points="34,232 58,222 58,242" />
          <text className="chart-figure__text" x="20" y="268">Signal</text>
          {CLEARED.map(([x, y, label]) => (
            <g key={label}>
              <circle className="chart-figure__waypoint" cx={x} cy={y} r="7" />
              <text className="chart-figure__small" x={x} y={y + 24} textAnchor="middle">✓ {label}</text>
            </g>
          ))}
          {FOG.map(([x, y, label]) => (
            <g key={label}>
              <ellipse className="chart-figure__fog" filter={`url(#${id}-blur)`} cx={x} cy={y} rx="72" ry="30" opacity=".85" />
              <text className="chart-figure__text chart-figure__fog-label" x={x} y={y + 5} textAnchor="middle">{label}</text>
            </g>
          ))}
          <g transform="translate(645 150)">
            <path className="chart-figure__edge" d="M0 -26 L0 22" />
            <path className="chart-figure__hot" d="M0 -26 L26 -17 L0 -8 Z" />
          </g>
          <text className="chart-figure__text" x="620" y="190">Destination</text>
        </>
      )}
    </Figure>
  );
}
