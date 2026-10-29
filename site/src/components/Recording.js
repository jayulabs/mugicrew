import React from 'react';
import LoopVideo from './LoopVideo';

// A TUI recording by tape name (label overrides the default aria label): /recordings/<name>.mp4 with its .jpg poster.
export default function Recording({ name, label, children }) {
  return (
    <div className="recording" data-recording={name}>
      <LoopVideo src={`/recordings/${name}.mp4`} poster={`/recordings/${name}.jpg`} label={label ?? `Recording of the mugicrew TUI: ${name.replace(/-/g, ' ')}`} />
      {children ? <p className="recording__caption">{children}</p> : null}
    </div>
  );
}
