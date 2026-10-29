// Asserts the hero page at / carries FRs 1-5 in order: the one-breath
// statement, the spine, the recording spot, the install line with a jump to
// Try it, and the three lens doors in workspace → framework → game order.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { run } from './lib.mjs';

const MARKERS = [
  ['one breath', 'id="what-it-is"'],
  ['spine diagram', 'class="chart-figure'],
  ['recording spot', 'data-recording="hero-loop"'],
  ['install line', 'npm i -g @jayulabs/mugicrew'],
  ['lens doors', 'id="doors"'],
];
const DOORS = [
  ['Course 01', '/workspace'],
  ['Course 02', '/framework'],
  ['Course 03', '/game'],
];

// The hero loop plays inline: a muted looping autoplaying MP4 with a poster,
// both files present in the build.
function checkVideo(dir, html) {
  const at = html.indexOf('data-recording="hero-loop"');
  const tag = at < 0 ? null : /<video\b[^>]*>/.exec(html.slice(at));
  if (!tag) return ['index.html: hero loop is not a video'];
  const errs = [];
  for (const attr of ['autoplay', 'loop', 'muted', 'playsinline']) {
    if (!new RegExp(`\\b${attr}\\b`).test(tag[0])) errs.push(`index.html: hero video lacks ${attr}`);
  }
  const src = /<source [^>]*src="[^"]*?\/(recordings\/[^"]+\.mp4)"/.exec(html.slice(at));
  const poster = /poster="[^"]*?\/(recordings\/[^"]+)"/.exec(tag[0]);
  if (!src) errs.push('index.html: hero video has no MP4 source');
  if (!poster) errs.push('index.html: hero video has no poster');
  for (const m of [src, poster]) {
    if (m && !fs.existsSync(path.join(dir, m[1]))) errs.push(`index.html: built file ${m[1]} is missing`);
  }
  return errs;
}

export function checkHero(dir) {
  const errs = [];
  const file = path.join(dir, 'index.html');
  if (!fs.existsSync(file)) return ['index.html: missing'];
  const html = fs.readFileSync(file, 'utf8');
  let last = -1;
  for (const [name, needle] of MARKERS) {
    const at = html.indexOf(needle);
    if (at < 0) errs.push(`index.html: no ${name}`);
    else if (at < last) errs.push(`index.html: ${name} is out of order`);
    else last = at;
  }
  errs.push(...checkVideo(dir, html));
  if (!/href="[^"]*\/try"[^>]*>/.test(html)) errs.push('index.html: no jump to Try it');
  let prev = -1;
  for (const [label, to] of DOORS) {
    const m = new RegExp(`<a [^>]*href="[^"]*${to}"[^>]*>(?:(?!</a>).)*${label}`, 's').exec(html);
    if (!m) errs.push(`index.html: no door "${label}" to ${to}`);
    else if (m.index < prev) errs.push(`index.html: door "${label}" is out of order`);
    else prev = m.index;
  }
  return errs;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const dir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'build');
  run(checkHero(dir), 'hero page carries FRs 1-5 in order');
}
