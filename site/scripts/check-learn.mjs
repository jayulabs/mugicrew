// Asserts the Learn section's frame: every /learn page carries the sidebar,
// the explainer pages carry none, the header has a Learn item on every route,
// and no Learn page links to an ADR or crew-doc path.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { ROUTES, LEARN_ROUTES, htmlPages } from './check-build.mjs';
import { run } from './lib.mjs';

const SIDEBAR = /<[a-z]+[^>]*class="[^"]*theme-doc-sidebar-container/;
const LEARN_ITEM = /<a[^>]*class="[^"]*navbar__link[^"]*"[^>]*href="[^"]*\/learn"[^>]*>\s*Learn\s*<\/a>/;
const FORBIDDEN_LINK = /href="[^"]*(?:\/docs\/adr\/|\/adr\/\d|crewdocs)[^"]*"/;

export function checkLearn(dir) {
  const errs = [];
  const read = (r) => {
    const file = path.join(dir, r);
    return fs.existsSync(file) ? fs.readFileSync(file, 'utf8') : null;
  };
  // Explainer routes (the lens pages and the install guide) carry no sidebar.
  for (const r of ROUTES) {
    const html = read(r);
    if (html !== null && SIDEBAR.test(html)) errs.push(`${r}: explainer page carries the Learn sidebar`);
  }
  for (const r of [...ROUTES, ...LEARN_ROUTES]) {
    const html = read(r);
    if (html !== null && !LEARN_ITEM.test(html)) errs.push(`${r}: header has no Learn item`);
  }
  for (const r of LEARN_ROUTES) {
    const html = read(r);
    if (html === null) continue;
    if (!SIDEBAR.test(html)) errs.push(`${r}: no sidebar`);
  }
  for (const f of htmlPages(dir)) {
    if (!f.startsWith('learn')) continue;
    const m = fs.readFileSync(path.join(dir, f), 'utf8').match(FORBIDDEN_LINK);
    if (m) errs.push(`${f}: links to an ADR or crew-doc path ${m[0]}`);
  }
  return errs;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const dir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'build');
  run(checkLearn(dir), 'Learn pages carry the sidebar, explainer pages none, every header a Learn item');
}
