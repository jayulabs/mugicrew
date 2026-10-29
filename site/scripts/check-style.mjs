// Asserts the built site wears the shared style: header and footer on every
// route, the compass-rose favicon, and no request to a third-party origin.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { ROUTES, htmlPages } from './check-build.mjs';
import { walk, run } from './lib.mjs';

// What a page loads: <link>, <script>, <img>, <source> and <video> with an
// absolute URL (bar canonical/alternate metadata), and CSS url()/@import. Anchors are links, not loads.
const HTML_LOAD = /<(?:link|script|img|source|video|iframe)\b[^>]*\b(?:src|href|poster)=["'](?:https?:)?\/\/[^"']+["'][^>]*>/g;
const CSS_LOAD = /url\(\s*["']?(?:https?:)?\/\/[^)]+\)|@import\s+["']?(?:https?:)?\/\//g;

const NPM_PACKAGE = 'https://www.npmjs.com/package/@jayulabs/mugicrew';
const NPM_LINK = new RegExp(`href="${NPM_PACKAGE.replace(/[./]/g, '\\$&')}"`);

export function checkStyle(dir) {
  const errs = [];
  for (const route of ROUTES) {
    const file = path.join(dir, route);
    if (!fs.existsSync(file)) continue;
    const html = fs.readFileSync(file, 'utf8');
    if (!/<nav[^>]*class="[^"]*navbar/.test(html)) errs.push(`${route}: no header`);
    if (!/<footer[^>]*class="[^"]*footer/.test(html)) errs.push(`${route}: no footer`);
    if (!/<link[^>]*rel="icon"[^>]*compass-rose\.svg/.test(html)) errs.push(`${route}: favicon is not the compass rose`);
    if (!/https:\/\/github\.com\/jayulabs\/mugicrew/.test(html.split('<footer')[1] ?? '')) errs.push(`${route}: footer has no GitHub link`);
    const [head, foot = ''] = html.split('<footer');
    if (!NPM_LINK.test(head)) errs.push(`${route}: header has no link to ${NPM_PACKAGE}`);
    if (!NPM_LINK.test(foot)) errs.push(`${route}: footer has no link to ${NPM_PACKAGE}`);
  }
  for (const f of htmlPages(dir)) {
    for (const m of fs.readFileSync(path.join(dir, f), 'utf8').match(HTML_LOAD) ?? []) {
      if (/<link\b/.test(m) && /rel=["'](?:canonical|alternate)["']/.test(m)) continue;
      errs.push(`${f}: third-party load ${m}`);
    }
  }
  for (const f of walk(dir, (n) => n.endsWith('.css'))) {
    for (const m of fs.readFileSync(path.join(dir, f), 'utf8').match(CSS_LOAD) ?? []) errs.push(`${f}: third-party load ${m}`);
  }
  return errs;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const dir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'build');
  run(checkStyle(dir), 'every route wears the shared style');
}
