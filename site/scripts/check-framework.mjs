// Asserts the built /framework page carries FRs 16-22 in order, three
// diagrams each with an accessible name and caption, the configurability
// paragraph linking the README, the /try call to action and the two lens
// cross-links, and plays the two recordings inline.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { run } from './lib.mjs';

export const HEADINGS = [
  'A Checkpoint Producer over git and bd',
  'Chain, not mutation',
  'Two relationships, two jobs',
  'Branch topology',
  'The AFK loop',
  'Observable by design',
  'Configurable where it matters',
];
const README = 'https://github.com/jayulabs/mugicrew/blob/main/README.md';
const SPOTS = ['loop-start-ps', 'tui-stats'];

export function checkFramework(html) {
  const errs = [];
  let at = -1;
  for (const h of HEADINGS) {
    const i = html.indexOf(`>${h}</h2>`);
    if (i < 0) errs.push(`missing heading ${h}`);
    else if (i < at) errs.push(`heading out of order: ${h}`);
    else at = i;
  }
  const figures = html.match(/<figure\b[\s\S]*?<\/figure>/g) ?? [];
  if (figures.length !== 3) errs.push(`want 3 diagrams, got ${figures.length}`);
  for (const f of figures) {
    if (!/<svg[^>]*aria-labelledby=/.test(f) || !/<title[^>]*>[^<]+<\/title>/.test(f)) errs.push('diagram without title/aria-label');
    if (!/<figcaption>[^<]+<\/figcaption>/.test(f)) errs.push('diagram without caption');
  }
  if ((html.match(new RegExp(`href="${README}"`, 'g')) ?? []).length !== 1) errs.push('want exactly one README link');
  const cfg = html.slice(html.indexOf(`>${HEADINGS[6]}</h2>`)).match(/<\/h2>([\s\S]*)/)?.[1] ?? '';
  if ((cfg.match(/<p>/g) ?? []).length < 1 || !cfg.includes(README)) errs.push('configurability section lacks README paragraph');
  if (!/href="[^"]*\/try"/.test(html)) errs.push('no link to /try');
  for (const l of ['workspace', 'game']) if (!new RegExp(`href="[^"]*/${l}"[^>]*>See it as a`).test(html)) errs.push(`no See it as a link to ${l}`);
  for (const s of SPOTS) if (!html.includes(`data-recording="${s}"`)) errs.push(`missing ${s} recording`);
  return errs;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const file = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'build', 'framework.html');
  run(checkFramework(fs.readFileSync(file, 'utf8')), 'framework page carries FRs 16-22, three diagrams and both cross-links');
}
