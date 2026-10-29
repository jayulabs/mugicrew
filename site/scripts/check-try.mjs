// Asserts the Try it page at /try carries FRs 30-32 and 35 in order, with the
// commands copyable as code and the Where next links resolving: the install
// guide on-site, README and GLOSSARY on GitHub, and the npm package page.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { run } from './lib.mjs';

const MARKERS = [
  ['install command', '<code>npm i -g @jayulabs/mugicrew</code>'],
  ['init command', '<code>mugicrew init</code>'],
  ['doctor command', '<code>mugicrew doctor</code>'],
  ['doctor fix command', '<code>mugicrew doctor fix</code>'],
  ['install recording spot', 'data-recording="install"'],
  ['init workspace note', '<code>mugicrew init workspace</code>'],
  ['first Signal recording spot', 'data-recording="first-signal"'],
  ['Where next', 'id="where-next"'],
];
const LINKS = [
  ['install guide', /href="[^"]*\/try\/install"/],
  ['README', /href="https:\/\/github\.com\/jayulabs\/mugicrew\/blob\/main\/README\.md"/],
  ['GLOSSARY', /href="https:\/\/github\.com\/jayulabs\/mugicrew\/blob\/main\/GLOSSARY\.md"/],
  ['npm package', /href="https:\/\/www\.npmjs\.com\/package\/@jayulabs\/mugicrew"/],
];

export function checkTry(dir) {
  const errs = [];
  const file = path.join(dir, 'try.html');
  if (!fs.existsSync(file)) return ['try.html: missing'];
  const html = fs.readFileSync(file, 'utf8');
  let last = -1;
  for (const [name, needle] of MARKERS) {
    const at = html.indexOf(needle);
    if (at < 0) errs.push(`try.html: no ${name}`);
    else if (at < last) errs.push(`try.html: ${name} is out of order`);
    else last = at;
  }
  const next = html.indexOf('id="where-next"');
  const tail = next < 0 ? '' : html.slice(next);
  for (const [name, re] of LINKS) if (!re.test(tail)) errs.push(`try.html: Where next has no ${name} link`);
  return errs;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const dir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'build');
  run(checkTry(dir), 'try page carries FRs 30-32 and 35 in order');
}
