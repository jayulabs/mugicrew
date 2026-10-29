// Helpers shared by the check scripts.
import fs from 'node:fs';
import path from 'node:path';

// Every file under dir (recursively) whose name passes keep, as dir-relative
// slash paths, sorted; directories passing skip are not entered.
export function walk(dir, keep = () => true, skip = () => false) {
  const out = [];
  const visit = (d) => {
    for (const e of fs.readdirSync(d, { withFileTypes: true })) {
      const p = path.join(d, e.name);
      if (skip(e.name)) continue;
      if (e.isDirectory()) visit(p);
      else if (keep(e.name)) out.push(path.relative(dir, p).split(path.sep).join('/'));
    }
  };
  visit(dir);
  return out.sort();
}

// The CLI main of a check: print the errors and exit 1, else print okMessage.
export function run(errs, okMessage) {
  if (errs.length) {
    console.error(errs.join('\n'));
    process.exit(1);
  }
  console.log(okMessage);
}
