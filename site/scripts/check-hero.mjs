// Asserts the hero page at / carries the ADR 0422 composition in order: a main
// landmark wrapping the page body, the
// tagline, the open-source line, the install line, the install-guide and Try it
// links, the hero-loop recording, the Learn band link, the five verb headings
// and the AI content disclosure line; and that the lens doors and the spine
// figure stay retired.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { run } from './lib.mjs';

// Markers in document order: [name, test] where test finds the marker's index.
const link = (to, text) => (html) => {
  const m = new RegExp(`<a [^>]*href="[^"]*${to}"[^>]*>(?:(?!</a>).)*${text ?? ''}`, 's').exec(html);
  return m ? m.index : -1;
};
export const VERB_HEADINGS = [
  'Capture your ideas as signals.',
  'Chart the course and clear the fog.',
  'Let agents handle everything else.',
  'Work on any codebase.',
  'Use your existing code harness.',
];
export const DISCLOSURE = 'AI Content Disclosure: No generated text was used for any content on this page.';
const MARKERS = [
  ['main landmark', (h) => h.indexOf('<main')],
  ['tagline anchor', (h) => h.indexOf('id="what-it-is"')],
  ['open-source line', (h) => h.indexOf('Free and open-source forever.')],
  ['install line', (h) => h.indexOf('npm i -g @jayulabs/mugicrew')],
  ['link to the full install guide', link('/try/install')],
  ['link to Try it', link('/try', 'Try it')],
  ['recording spot', (h) => h.indexOf('data-recording="hero-loop"')],
  ['Learn band link', link('/learn', 'Start learning')],
  ...VERB_HEADINGS.map((t) => [`verb heading "${t}"`, (h) => h.indexOf(t)]),
  ['AI content disclosure line', (h) => h.indexOf(DISCLOSURE)],
];
const RETIRED_DOORS = ['/workspace', '/framework', '/game'];

// The page body between the shared navbar and footer, which keep their lens links.
function body(html) {
  const start = html.indexOf('main-wrapper');
  const end = html.indexOf('<footer');
  return html.slice(Math.max(start, 0), end < 0 ? html.length : end);
}

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
  const page = body(html);
  let last = -1;
  for (const [name, find] of MARKERS) {
    const at = find(page);
    if (at < 0) errs.push(`index.html: no ${name}`);
    else if (at < last) errs.push(`index.html: ${name} is out of order`);
    else last = at;
  }
  errs.push(...checkVideo(dir, html));
  if (page.includes('id="doors"')) errs.push('index.html: lens doors are back');
  for (const to of RETIRED_DOORS) {
    if (link(to)(page) >= 0) errs.push(`index.html: link to ${to} is back`);
  }
  if (page.includes('class="chart-figure')) errs.push('index.html: spine figure is back');
  return errs;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const dir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'build');
  run(checkHero(dir), 'hero page carries the ADR 0422 composition in order');
}
