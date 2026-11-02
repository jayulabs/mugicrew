import test from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const sidebars = createRequire(import.meta.url)('../learn-sidebars.js');

test('the Learn sidebar lists the index first, then the guides, the references and Customization', () => {
  const order = sidebars.learn.map((i) => (typeof i === 'string' ? i : i.label));
  assert.deepEqual(order, [
    'index',
    'basic-concepts',
    'working-the-spine',
    'organizing-with-initiatives',
    'discussions',
    'failure-modes',
    'bead-glossary',
    'cli-verbs',
    'Customization',
  ]);
});
