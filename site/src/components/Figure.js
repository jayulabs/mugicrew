import React from 'react';

// A diagram's frame: the drawing, an accessible name, and a caption that
// lets the page read without the drawing. Children are the SVG body.
export default function Figure({ title, caption, viewBox, children }) {
  const id = `fig${React.useId().replace(/[^a-zA-Z0-9]/g, "")}`;
  return (
    <figure className="chart-figure">
      <svg viewBox={viewBox} role="img" aria-labelledby={`${id}-title`}>
        <title id={`${id}-title`}>{title}</title>
        {children(id)}
      </svg>
      <figcaption>{caption}</figcaption>
    </figure>
  );
}

export function ArrowHead({ id }) {
  return (
    <defs>
      <marker id={id} viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
        <path className="chart-figure__arrow" d="M0 0 L10 5 L0 10 z" />
      </marker>
    </defs>
  );
}
