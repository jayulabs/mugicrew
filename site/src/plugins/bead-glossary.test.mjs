import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import { beadKinds } from './bead-glossary.mjs';

const wrap = (body) => `# G\n\n## Other\n\n- **Nope** — x.\n  - _Owner:_ nobody\n\n## Bead kinds\n\n${body}\n\n## Next\n\n- **After** — y.\n  - _Owner:_ nobody\n`;

test('ADR parentheticals are removed with their leading space', () => {
  const [e] = beadKinds(wrap('- **A** — alpha (ADR 0001, ADR 0002).\n  - _Owner:_ Luffy (steward) (ADR 0003)\n  - _Structure:_ leaf'));
  assert.equal(e.markdown, '- **A** — alpha.\n  - _Owner:_ Luffy (steward)\n  - _Structure:_ leaf');
});

test('History bullets go with their continuation lines', () => {
  const [e] = beadKinds(wrap('- **A** — alpha.\n  - _Owner:_ Luffy\n  - _History:_ was B until ADR 0354,\n    then renamed.\n  - _Structure:_ leaf'));
  assert.equal(e.markdown, '- **A** — alpha.\n  - _Owner:_ Luffy\n  - _Structure:_ leaf');
});

test('See ADR sentences are dropped, and a bullet left empty goes too', () => {
  const [e] = beadKinds(wrap('- **A** — alpha.\n  - _Owner:_ Luffy\n  - _Notes:_ See ADR 0004, ADR 0010.\n  - _Operations:_ does x. See ADR 0005, 0006. Then y.'));
  assert.equal(e.markdown, '- **A** — alpha.\n  - _Owner:_ Luffy\n  - _Operations:_ does x. Then y.');
});

test('a residual citation is rejected, naming its entry', () => {
  assert.throws(
    () => beadKinds(wrap('- **Fine** — ok.\n  - _Owner:_ Luffy\n- **Leaky** — see ADR 0102 here.\n  - _Owner:_ Luffy')),
    /^Error: Leaky:.*ADR/,
  );
});

test('settings, preamble and trailing prose are excluded; file order kept', () => {
  const got = beadKinds(
    wrap(
      'Preamble mentions **kind** (ADR 0002).\n\n- **Zed** — z.\n  - _Owner:_ A\n- **Setting** — a Workspace setting.\n  Plain prose only.\n- **Alpha** — a.\n  - *Owner:* B\n\nTrailing prose with ADR 0010.',
    ),
  );
  assert.deepEqual(got.map((e) => e.term), ['Zed', 'Alpha']);
});

test('the real GLOSSARY.md yields every bead kind, clean', () => {
  const md = fs.readFileSync(new URL('../../../GLOSSARY.md', import.meta.url), 'utf8');
  const terms = beadKinds(md).map((e) => e.term);
  for (const k of ['Signal', 'Chart', 'Spec', 'Checkpoint', 'Build', 'Review', 'Critique', 'Pivot', 'Landing', 'Approval', 'Initiative']) {
    assert.ok(terms.includes(k), k);
  }
  assert.ok(terms.indexOf('Signal') < terms.indexOf('Landing'));
  assert.ok(!terms.includes('Require Initiatives') && !terms.includes('Enforce external ID'));
  for (const e of beadKinds(md)) assert.doesNotMatch(e.markdown, /ADR\s+\d{4}/, e.term);
});

test('the page anchors each kind by its term and keeps order', async () => {
  const { beadGlossaryPage, kindSlug } = await import('./bead-glossary.mjs');
  const page = beadGlossaryPage(wrap('- **Two Words** — beta.\n  - _Owner:_ Luffy\n  - _Structure:_ leaf\n- **A** — alpha.\n  - _Owner:_ Luffy\n  - _Structure:_ leaf'));
  assert.equal(kindSlug('Two Words'), 'two-words');
  assert.ok(page.includes('### Two Words {#two-words}\n\nbeta.\n\n- _Owner:_ Luffy\n- _Structure:_ leaf'));
  assert.ok(page.indexOf('{#two-words}') < page.indexOf('{#a}'));
  assert.ok(!page.includes('Nope'));
});
