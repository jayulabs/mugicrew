// Content scan over site sources and assets: nothing may name the OE that
// hosts this checkout or its owner (the public denylist).
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { walk, run } from './lib.mjs';

// Spelled by concatenation so this file does not trip its own scan.
export const PATTERNS = [
  new RegExp('nen' + 'oe'),
  new RegExp('sleepy' + 'sort'),
  new RegExp('sha' + 'wnsmlee'),
  new RegExp('sleepy' + 'lab'),
  new RegExp('\\bsha' + 'wn\\b', 'i'),
];
const SKIP = new Set(['node_modules', 'build', '.docusaurus']);

export function scan(root) {
  const hits = [];
  for (const rel of walk(root, () => true, (n) => SKIP.has(n))) {
    const p = path.join(root, rel);
    const text = fs.readFileSync(p).toString('latin1');
    for (const re of PATTERNS) if (re.test(text) || re.test(p)) hits.push(`${rel}: ${re}`);
  }
  return hits;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
  const hits = scan(root);
  run(hits.length ? ['denylist hits:', ...hits] : [], 'denylist scan clean');
}
