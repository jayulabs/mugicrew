import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const dir = path.join(path.dirname(fileURLToPath(import.meta.url)), '..', 'recording-kit', 'tapes');
const settings = fs.readFileSync(path.join(dir, '..', 'settings.tape'), 'utf8');
const tapes = fs.readdirSync(dir).filter((f) => f.endsWith('.tape'));

test('the kit holds a tape for every scene', () => {
  const want = ['hero-loop', 'grilling', 'approval', 'loop-start-ps', 'tui-stats', 'graph-map', 'install', 'first-signal'];
  assert.deepEqual(tapes.map((f) => f.replace('.tape', '')).sort(), want.sort());
});

for (const f of tapes) {
  test(`${f} follows the kit's rules`, () => {
    const s = fs.readFileSync(path.join(dir, f), 'utf8');
    const name = f.replace('.tape', '');
    assert.ok(s.includes(`Output "/out/${name}.mp4"`), 'outputs /out/<name>.mp4');
    assert.ok(s.includes('Source /kit/settings.tape'), 'sources the shared settings');
    for (const set of ['Width 1280', 'Height 720', 'FontSize 16']) assert.ok(settings.includes(`Set ${set}`), set);
    assert.match(s, /^Hide$/m, 'setup runs under Hide');
    assert.match(s.slice(s.search(/^Show$/m)), /Sleep \d+(ms|s)?/, 'the shown part is paced by Sleeps');
    assert.doesNotMatch(s, /\b(demo|mc)-[a-z0-9]{3,}\b/, 'navigates by keys, never a bead id');
  });
}

test('the Containerfile copies every /kit path a tape or inside.sh uses', () => {
  const kit = path.join(dir, '..');
  const copied = [...fs.readFileSync(path.join(kit, 'Containerfile'), 'utf8').matchAll(/^COPY\s+(?:--\S+\s+)*\S+\s+(\/kit\/\S+)/gm)].map((m) => m[1].replace(/\/$/, ''));
  const used = new Set();
  const scan = (s) => { for (const m of s.matchAll(/\/kit\/([\w.-]+)/g)) used.add(`/kit/${m[1]}`); };
  scan(fs.readFileSync(path.join(kit, 'inside.sh'), 'utf8'));
  for (const f of tapes) scan(fs.readFileSync(path.join(dir, f), 'utf8'));
  for (const p of used) assert.ok(copied.includes(p), `${p} has a COPY in the Containerfile`);
});
