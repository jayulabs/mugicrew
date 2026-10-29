import React from 'react';
import useBaseUrl from '@docusaurus/useBaseUrl';

// A muted, looping, inline recording (MP4 only) with a poster frame.
export default function LoopVideo({ src, poster, label }) {
  return (
    <video className="loop-video" autoPlay loop muted playsInline aria-label={label} poster={useBaseUrl(poster)}>
      <source src={useBaseUrl(src)} type="video/mp4" />
    </video>
  );
}
