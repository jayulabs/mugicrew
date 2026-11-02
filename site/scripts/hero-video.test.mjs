import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { checkHero, VERB_HEADINGS, DISCLOSURE } from './check-hero.mjs';

const tmp = () => fs.mkdtempSync(path.join(os.tmpdir(), 'site-'));
const put = (d, f, s = '') => {
  fs.mkdirSync(path.dirname(path.join(d, f)), { recursive: true });
  fs.writeFileSync(path.join(d, f), s);
};

const GOOD_VIDEO_ATTRS = 'autoplay loop muted playsinline poster="/mugicrew/recordings/hero-loop.jpg"';
const MP4 = '/mugicrew/recordings/hero-loop.mp4';
const video = (attrs = GOOD_VIDEO_ATTRS, src = MP4) =>
  `<div data-recording="hero-loop"><video ${attrs}><source src="${src}" type="video/mp4"></video></div>`;
const INSTALL = '<code>npm i -g @jayulabs/mugicrew</code>';
const LINKS = '<a href="/mugicrew/try/install">Full install guide</a><a href="/mugicrew/try">Try it</a>';
const LEARN = '<a href="/mugicrew/learn"><span>First time with mugicrew?</span><strong>Start learning</strong></a>';
const verbs = (headings = VERB_HEADINGS) => headings.map((h) => `<section><h2>${h}</h2></section>`).join('');
const DISCLOSURE_LINE = `<p>${DISCLOSURE}</p>`;

// One hero page; each part can be swapped to build a broken variant.
const page = ({ hero = INSTALL + LINKS, recording = video(), learn = LEARN, sections = verbs() + DISCLOSURE_LINE, extra = '' } = {}) =>
  `<nav><a href="/mugicrew/workspace">Workspace</a></nav><div class="main-wrapper"><main><p id="what-it-is">Build</p><p>Free and open-source forever.</p>${hero}${recording}${learn}${sections}${extra}</main></div><footer><a href="/mugicrew/game">Game</a></footer>`;

function site(html, { built = true } = {}) {
  const d = tmp();
  if (built) {
    put(d, 'recordings/hero-loop.mp4');
    put(d, 'recordings/hero-loop.jpg');
  }
  put(d, 'index.html', html);
  return d;
}

test('checkHero accepts the composition in order', () => {
  assert.deepEqual(checkHero(site(page())), []);
});

test('checkHero wants the hero loop playing as a muted looping MP4 with a poster, files built', () => {
  const still = page({ recording: '<div data-recording="hero-loop"><span>soon</span></div>' });
  assert.ok(checkHero(site(still)).includes('index.html: hero loop is not a video'));
  const noAutoplay = page({ recording: video('loop muted playsinline poster="/mugicrew/recordings/hero-loop.jpg"') });
  assert.ok(checkHero(site(noAutoplay)).includes('index.html: hero video lacks autoplay'));
  const unbuilt = checkHero(site(page(), { built: false }));
  assert.ok(unbuilt.includes('index.html: built file recordings/hero-loop.mp4 is missing'));
  assert.ok(unbuilt.includes('index.html: built file recordings/hero-loop.jpg is missing'));
});

test('checkHero fails on retired doors and spine', () => {
  assert.ok(checkHero(site(page({ extra: '<a href="/mugicrew/workspace">Course 01</a>' }))).includes('index.html: link to /workspace is back'));
  assert.ok(checkHero(site(page({ extra: '<div id="doors"></div>' }))).includes('index.html: lens doors are back'));
  assert.ok(checkHero(site(page({ extra: '<svg class="chart-figure"></svg>' }))).includes('index.html: spine figure is back'));
});

test('checkHero wants a main landmark around the page body', () => {
  const noMain = page().replace('<main>', '<div>').replace('</main>', '</div>');
  assert.ok(checkHero(site(noMain)).includes('index.html: no main landmark'));
});

test('checkHero fails on a missing or misplaced Learn link', () => {
  assert.ok(checkHero(site(page({ learn: '' }))).includes('index.html: no Learn band link'));
  const afterRecording = page({ recording: '', learn: LEARN + video() });
  assert.ok(checkHero(site(afterRecording)).includes('index.html: Learn band link is out of order'));
});

test('checkHero wants the hero parts, the verb headings and the disclosure in order', () => {
  assert.ok(checkHero(site(page({ hero: LINKS + INSTALL }))).some((e) => e.includes('out of order')));
  assert.ok(checkHero(site('<p id="what-it-is"></p>')).includes('index.html: no install line'));
  const reversed = page({ sections: verbs([...VERB_HEADINGS].reverse()) + DISCLOSURE_LINE });
  assert.ok(checkHero(site(reversed)).some((e) => e.includes('is out of order')));
  assert.ok(checkHero(site(page({ sections: verbs() }))).includes('index.html: no AI content disclosure line'));
  const disclosureFirst = page({ sections: DISCLOSURE_LINE + verbs() });
  assert.ok(checkHero(site(disclosureFirst)).some((e) => e.includes('AI content disclosure line is out of order')));
});
