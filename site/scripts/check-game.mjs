// Asserts the game lens at /game carries FRs 23-28 in order, its two
// diagrams, the Try it call to action and the two cross-links, and names no
// crewmate (FR 29).
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { run } from './lib.mjs';

const HEADINGS = [
  'Your experience',
  'The bead graph is the map',
  'Your moves',
  'What winning is',
  'Resources and pacing',
  'The loop',
];
const CREWMATES = [
  'Luffy', 'Zoro', 'Nami', 'Usopp', 'Sanji', 'Chopper', 'Robin', 'Franky', 'Brook', 'Jinbe',
];

const text = (html) => html.replace(/<(script|style)\b.*?<\/\1>/gs, ' ').replace(/<[^>]*>/g, ' ');

export function checkGame(dir) {
  const errs = [];
  const file = path.join(dir, 'game.html');
  if (!fs.existsSync(file)) return ['game.html: missing'];
  const html = fs.readFileSync(file, 'utf8');
  let last = -1;
  for (const h of HEADINGS) {
    const m = new RegExp(`<h2[^>]*>\\s*${h}\\b`).exec(html);
    if (!m) errs.push(`game.html: no heading "${h}"`);
    else if (m.index < last) errs.push(`game.html: heading "${h}" is out of order`);
    else last = m.index;
  }
  const figures = html.match(/<figure class="chart-figure"[\s\S]*?<\/figure>/g) || [];
  if (figures.length < 2) errs.push('game.html: want the fog-chart and loop diagrams');
  for (const f of figures) {
    if (!/<title[^>]*>[^<]+<\/title>/.test(f) || !/aria-labelledby=/.test(f)) errs.push('game.html: a diagram has no title/aria-label');
    if (!/<figcaption>[^<]+<\/figcaption>/.test(f)) errs.push('game.html: a diagram has no caption');
  }
  if (!html.includes('data-recording="graph-map"')) errs.push('game.html: no Graph tab recording spot');
  if (!/href="[^"]*\/try"/.test(html)) errs.push('game.html: no call to action to Try it');
  const wi = html.search(/href="[^"]*\/workspace"[^>]*>See it as a (?:<!-- -->)?workspace/);
  const fi = html.search(/href="[^"]*\/framework"[^>]*>See it as a (?:<!-- -->)?framework/);
  if (wi < 0) errs.push('game.html: no "See it as a workspace" link');
  if (fi < 0) errs.push('game.html: no "See it as a framework" link');
  if (wi >= 0 && fi >= 0 && fi < wi) errs.push('game.html: cross-links are out of order');
  const body = text(html);
  for (const n of CREWMATES) if (new RegExp(`\\b${n}\\b`).test(body)) errs.push(`game.html: names crewmate ${n}`);
  return errs;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const dir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'build');
  run(checkGame(dir), 'game page carries FRs 23-28 in order and names no crewmate');
}
