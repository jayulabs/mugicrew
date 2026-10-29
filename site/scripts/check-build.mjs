// Asserts the production build holds exactly the six routes (plus the 404
// page) and no page rendered from README, GLOSSARY, ADRs or crew docs.
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { walk, run } from './lib.mjs';

export const ROUTES = ['index.html', 'workspace.html', 'framework.html', 'game.html', 'try.html', 'try/install.html'];

export function htmlPages(dir) {
  return walk(dir, (n) => n.endsWith('.html'));
}

export function check(dir) {
  const want = [...ROUTES, '404.html'].sort();
  const got = htmlPages(dir);
  const errs = [];
  for (const w of want) if (!got.includes(w)) errs.push(`missing route page ${w}`);
  for (const g of got) if (!want.includes(g)) errs.push(`unexpected page ${g}`);
  return errs;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const dir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'build');
  run(check(dir), 'build holds exactly the six routes');
}
