import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { check, LEARN_ROUTES } from './check-build.mjs';
import { scan } from './denylist.mjs';
import { checkStyle } from './check-style.mjs';
import { checkGame } from './check-game.mjs';
import { checkLearn } from './check-learn.mjs';
import { checkTry } from './check-try.mjs';
import { checkFramework, HEADINGS } from './check-framework.mjs';
import { checkWorkspace, HEADINGS as WORKSPACE_HEADINGS } from './check-workspace.mjs';

const tmp = () => fs.mkdtempSync(path.join(os.tmpdir(), 'site-'));
const put = (d, f, s = '') => {
  fs.mkdirSync(path.dirname(path.join(d, f)), { recursive: true });
  fs.writeFileSync(path.join(d, f), s);
};

test('check flags a missing route and an extra page', () => {
  const d = tmp();
  const learn = LEARN_ROUTES.map((r) => r.replace(/\.html$/, ''));
  for (const f of ['index', 'workspace', 'framework', 'game', 'try', ...learn, '404']) put(d, f + '.html');
  put(d, 'README.html');
  const errs = check(d);
  assert.deepEqual(errs.sort(), ['missing route page try/install.html', 'unexpected page README.html']);
});

test('denylist scan flags content and ignores node_modules', () => {
  const d = tmp();
  put(d, 'a.txt', 'hello world');
  put(d, 'node_modules/x.txt', 'nen' + 'oe');
  assert.deepEqual(scan(d), []);
  put(d, 'b.txt', 'see /home/sleepy' + 'sort/x');
  assert.equal(scan(d).length, 1);
});

test('checkStyle wants header, footer and favicon on every route and no third-party load', () => {
  const d = tmp();
  const NPM = 'https://www.npmjs.com/package/@jayulabs/mugicrew';
  const page = (extra = '') => `<html><head><link rel="icon" href="/mugicrew/img/compass-rose.svg">${extra}</head><body><nav class="navbar"><a href="${NPM}">npm</a></nav><footer class="footer"><a href="https://github.com/jayulabs/mugicrew">GitHub</a><a href="${NPM}">npm</a></footer></body></html>`;
  for (const f of ['index', 'workspace', 'framework', 'game', 'try', 'try/install', 'learn', 'learn/working-the-spine', 'learn/failure-modes', 'learn/bead-glossary', 'learn/cli-verbs']) put(d, f + '.html', page());
  assert.deepEqual(checkStyle(d), []);
  put(d, 'game.html', page('<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Fraunces">'));
  put(d, 'assets/a.css', '@font-face{src:url(https://cdn.example.com/f.woff2)}');
  put(d, 'try.html', '<html><head></head><body></body></html>');
  const errs = checkStyle(d);
  assert.ok(errs.some((e) => e.startsWith('game.html: third-party load')));
  assert.ok(errs.some((e) => e.startsWith('assets/a.css: third-party load')));
  assert.ok(errs.includes('try.html: no header'));
  put(d, 'workspace.html', page().replaceAll(NPM, 'https://www.npmjs.com/package/mugicrew'));
  const wrong = checkStyle(d);
  assert.ok(wrong.some((e) => e.startsWith('workspace.html: header has no link')));
  assert.ok(wrong.some((e) => e.startsWith('workspace.html: footer has no link')));
  assert.ok(errs.includes('try.html: no footer'));
  assert.ok(errs.includes('try.html: favicon is not the compass rose'));
});

test('checkGame wants the six headings in order, two captioned diagrams, the links and no crewmate', () => {
  const d = tmp();
  const fig = '<figure class="chart-figure"><svg role="img" aria-labelledby="a"><title id="a">t</title></svg><figcaption>c</figcaption></figure>';
  const links = '<a href="/mugicrew/try">Try it</a><a href="/mugicrew/workspace">See it as a workspace</a><a href="/mugicrew/framework">See it as a framework</a>';
  const hs = ['Your experience', 'The bead graph is the map', 'Your moves', 'What winning is', 'Resources and pacing', 'The loop'];
  const page = (list, extra = '') =>
    list.map((h) => `<h2>${h}</h2>`).join('') + fig + fig + '<div data-recording="graph-map"></div>' + links + extra;
  put(d, 'game.html', page(hs));
  assert.deepEqual(checkGame(d), []);
  put(d, 'game.html', page([hs[1], hs[0], ...hs.slice(2)]));
  assert.ok(checkGame(d).some((e) => e.includes('out of order')));
  put(d, 'game.html', page(hs, '<p>Zoro cuts it</p>'));
  assert.ok(checkGame(d).includes('game.html: names crewmate Zoro'));
  put(d, 'game.html', '<h2>Your experience</h2>');
  assert.ok(checkGame(d).includes('game.html: want the fog-chart and loop diagrams'));
});

test('checkTry wants the commands, recording spots and Where next links in order', () => {
  const d = tmp();
  const gh = 'https://github.com/jayulabs/mugicrew/blob/main/';
  const page = (links) =>
    '<code>npm i -g @jayulabs/mugicrew</code><code>mugicrew init</code><code>mugicrew doctor</code><code>mugicrew doctor fix</code>' +
    '<aside data-recording="install"></aside><code>mugicrew init workspace</code><aside data-recording="first-signal"></aside>' +
    `<h2 id="where-next"></h2>${links}`;
  const all =
    '<a href="/mugicrew/try/install">i</a>' +
    `<a href="${gh}README.md">r</a><a href="${gh}GLOSSARY.md">g</a>` +
    '<a href="https://www.npmjs.com/package/@jayulabs/mugicrew">n</a>';
  put(d, 'try.html', page(all));
  assert.deepEqual(checkTry(d), []);
  put(d, 'try.html', page(all.replace(/<a href="https:\/\/www[^>]*>n<\/a>/, '')));
  assert.deepEqual(checkTry(d), ['try.html: Where next has no npm package link']);
  put(d, 'try.html', '<code>mugicrew init</code>');
  assert.ok(checkTry(d).includes('try.html: no install command'));
});

test('checkFramework wants the seven headings, three captioned diagrams, README link and placeholders', () => {
  assert.ok(checkFramework('<html></html>').length > 5);
});

test('checkFramework passes a conforming page', () => {
  const README = 'https://github.com/jayulabs/mugicrew/blob/main/README.md';
  const fig = '<figure><svg aria-labelledby="t"><title id="t">x</title></svg><figcaption>c</figcaption></figure>';
  const html = HEADINGS.map((h, i) => `<h2>${h}</h2>` + (i < 3 ? fig : '') + (i === 6 ? `<p><a href="${README}">README</a></p>` : '')).join('') +
    '<a href="/mugicrew/try">Try it</a><a href="/mugicrew/workspace">See it as a workspace</a><a href="/mugicrew/game">See it as a game</a>' +
    '<div data-recording="loop-start-ps"></div><div data-recording="tui-stats"></div>';
  assert.deepEqual(checkFramework(html), []);
});

test('checkWorkspace wants the eight headings, two captioned diagrams, two recordings and the cross-links', () => {
  assert.ok(checkWorkspace('<html></html>').length > 8);
});

test('checkWorkspace passes a conforming page', () => {
  const fig = '<figure><svg aria-labelledby="t"><title id="t">x</title></svg><figcaption>c</figcaption></figure>';
  const html = WORKSPACE_HEADINGS.map((h, i) => `<h2>${h}</h2>` + (i === 1 ? fig + '<div data-recording="grilling"></div>' : '') + (i === 3 ? fig : '') + (i === 4 ? '<div data-recording="approval"></div>' : '')).join('') +
    '<a href="/mugicrew/try">Try it</a><a href="/mugicrew/framework">See it as a framework</a><a href="/mugicrew/game">See it as a game</a>';
  assert.deepEqual(checkWorkspace(html), []);
});

test('checkLearn wants a sidebar on Learn, none on explainers, a Learn item everywhere and no ADR links', () => {
  const d = tmp();
  const nav = '<a class="navbar__item navbar__link" href="/mugicrew/learn">Learn</a>';
  const side = '<div class="theme-doc-sidebar-container x"></div>';
  const page = (extra = '') => `<html><body>${nav}${extra}</body></html>`;
  for (const f of ['index', 'workspace', 'framework', 'game', 'try', 'try/install']) put(d, f + '.html', page());
  put(d, 'learn.html', page(side));
  assert.deepEqual(checkLearn(d), []);
  put(d, 'learn.html', page());
  assert.deepEqual(checkLearn(d), ['learn.html: no sidebar']);
  put(d, 'learn.html', page(side + '<a href="/mugicrew/adr/0001-x">adr</a>'));
  assert.ok(checkLearn(d).some((e) => e.startsWith('learn.html: links to an ADR')));
  put(d, 'learn.html', page(side));
  put(d, 'game.html', page(side));
  assert.deepEqual(checkLearn(d), ['game.html: explainer page carries the Learn sidebar']);
  put(d, 'game.html', '<html></html>');
  assert.deepEqual(checkLearn(d), ['game.html: header has no Learn item']);
});

test('checkGlossary wants an anchored entry per kind, no ADR citation, and a built anchor', async () => {
  const { checkGlossary } = await import('./check-glossary.mjs');
  const md = '## Bead kinds\n\n- **Signal** — s.\n  - _Owner:_ Luffy\n  - _Structure:_ leaf\n';
  assert.deepEqual(checkGlossary('### Signal {#signal}\n', md, '<h3 id="signal">'), []);
  assert.deepEqual(checkGlossary(null, md, null), ['learn/bead-glossary.md is missing']);
  assert.deepEqual(checkGlossary('x ADR 0001', md, null), ['bead glossary has no entry for Signal', 'bead glossary cites an ADR']);
  assert.deepEqual(checkGlossary('### Signal {#signal}\n', md, '<h3>'), ['built bead glossary has no anchor #signal']);
});
