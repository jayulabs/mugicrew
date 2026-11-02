// Asserts the generated bead glossary page exists, holds an entry (with its
// anchor) for every bead kind in GLOSSARY.md, cites no ADR, and was built.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { beadKinds, kindSlug } from '../src/plugins/bead-glossary.mjs';
import { GLOSSARY, PAGE } from './gen-glossary.mjs';
import { run } from './lib.mjs';

export function checkGlossary(page, glossaryMd, built) {
  const errs = [];
  if (page === null) return ['learn/bead-glossary.md is missing'];
  for (const { term } of beadKinds(glossaryMd)) {
    const id = kindSlug(term);
    if (!page.includes(`### ${term} {#${id}}`)) errs.push(`bead glossary has no entry for ${term}`);
    if (built !== null && !built.includes(`id="${id}"`)) errs.push(`built bead glossary has no anchor #${id}`);
  }
  if (/ADR\s+\d{4}/.test(page)) errs.push('bead glossary cites an ADR');
  return errs;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const read = (f) => (fs.existsSync(f) ? fs.readFileSync(f, 'utf8') : null);
  const built = path.resolve(path.dirname(PAGE), '..', 'build', 'learn', 'bead-glossary.html');
  run(checkGlossary(read(PAGE), fs.readFileSync(GLOSSARY, 'utf8'), read(built)), 'bead glossary holds every kind');
}
