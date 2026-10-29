// Asserts the built /workspace page carries FRs 8-15 in order, the FogChart
// and CheckpointLifecycle diagrams each with an accessible name and caption,
// the grilling and approval recording spots, and the /try call to action and
// the two lens cross-links.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { run } from './lib.mjs';

export const HEADINGS = [
  'Everything starts as a Signal',
  'Charting clears the fog',
  'One Chart, one Spec',
  'A Checkpoint is a candidate next state',
  'HITL is authorization, not authorship',
  'When something goes wrong',
  'Ask questions any time',
  'Initiatives group Checkpoints',
];
const SPOTS = ['grilling', 'approval'];

export function checkWorkspace(html) {
  const errs = [];
  let at = -1;
  for (const h of HEADINGS) {
    const i = html.indexOf(`>${h}</h2>`);
    if (i < 0) errs.push(`missing heading ${h}`);
    else if (i < at) errs.push(`heading out of order: ${h}`);
    else at = i;
  }
  const figures = html.match(/<figure\b[\s\S]*?<\/figure>/g) ?? [];
  if (figures.length !== 2) errs.push(`want 2 diagrams, got ${figures.length}`);
  for (const f of figures) {
    if (!/<svg[^>]*aria-labelledby=/.test(f) || !/<title[^>]*>[^<]+<\/title>/.test(f)) errs.push('diagram without title/aria-label');
    if (!/<figcaption>[^<]+<\/figcaption>/.test(f)) errs.push('diagram without caption');
  }
  let spot = -1;
  for (const s of SPOTS) {
    const i = html.indexOf(`data-recording="${s}"`);
    if (i < 0) errs.push(`missing ${s} recording`);
    else if (i < spot) errs.push(`${s} recording is out of order`);
    else spot = i;
  }
  if (!/href="[^"]*\/try"/.test(html)) errs.push('no link to /try');
  const wi = html.search(/href="[^"]*\/framework"[^>]*>See it as a/);
  const gi = html.search(/href="[^"]*\/game"[^>]*>See it as a/);
  if (wi < 0) errs.push('no See it as a link to framework');
  if (gi < 0) errs.push('no See it as a link to game');
  if (wi >= 0 && gi >= 0 && gi < wi) errs.push('cross-links are out of order');
  return errs;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const file = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'build', 'workspace.html');
  run(checkWorkspace(fs.readFileSync(file, 'utf8')), 'workspace page carries FRs 8-15, both diagrams and both cross-links');
}
