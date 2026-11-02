// Build-time shell over the bead glossary: reads GLOSSARY.md and writes the
// generated, git-ignored Learn page learn/bead-glossary.md.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { beadGlossaryPage } from '../src/plugins/bead-glossary.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
export const GLOSSARY = path.resolve(here, '..', '..', 'GLOSSARY.md');
export const PAGE = path.resolve(here, '..', 'learn', 'bead-glossary.md');

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  fs.writeFileSync(PAGE, beadGlossaryPage(fs.readFileSync(GLOSSARY, 'utf8')));
  console.log(`wrote ${path.relative(process.cwd(), PAGE)}`);
}
