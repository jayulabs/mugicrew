// Asserts the production build holds exactly the six explainer routes, the Learn routes (plus the 404
// page) and no page rendered from README, GLOSSARY, ADRs or crew docs.
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { walk, run } from './lib.mjs';

export const ROUTES = ['index.html', 'workspace.html', 'framework.html', 'game.html', 'try.html', 'try/install.html'];

// The Learn routes: the guides, the generated glossary, the verbs page and Customization.
export const LEARN_ROUTES = [
  'learn.html',
  'learn/basic-concepts.html',
  'learn/working-the-spine.html',
  'learn/discussions.html',
  'learn/organizing-with-initiatives.html',
  'learn/failure-modes.html',
  'learn/bead-glossary.html',
  'learn/cli-verbs.html',
  'learn/customization/configuration.html',
  'learn/customization/gitlab.html',
  'learn/customization/jira.html',
];

export function htmlPages(dir) {
  return walk(dir, (n) => n.endsWith('.html'));
}

export function check(dir) {
  const want = [...ROUTES, ...LEARN_ROUTES, '404.html'].sort();
  const got = htmlPages(dir);
  const errs = [];
  for (const w of want) if (!got.includes(w)) errs.push(`missing route page ${w}`);
  for (const g of got) if (!want.includes(g)) errs.push(`unexpected page ${g}`);
  return errs;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const dir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'build');
  run(check(dir), 'build holds exactly the explainer and Learn routes');
}
