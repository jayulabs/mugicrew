import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { checkHero } from './check-hero.mjs';

const tmp = () => fs.mkdtempSync(path.join(os.tmpdir(), 'site-'));
const put = (d, f, s = '') => {
  fs.mkdirSync(path.dirname(path.join(d, f)), { recursive: true });
  fs.writeFileSync(path.join(d, f), s);
};

test('checkHero wants the hero loop playing as a muted looping MP4 with a poster, files built', () => {
  const d = tmp();
  const door = (n, to) => `<a href="/mugicrew${to}">Course 0${n}</a>`;
  const rest = `<h2 id="what-it-is"></h2><svg class="chart-figure"></svg>`;
  const tail = `<code>npm i -g @jayulabs/mugicrew</code><a href="/mugicrew/try">Try it</a><div id="doors">${door(1, '/workspace')}${door(2, '/framework')}${door(3, '/game')}</div>`;
  const video = (attrs, src) =>
    `<div data-recording="hero-loop"><video ${attrs}><source src="${src}" type="video/mp4"></video></div>`;
  const good = 'autoplay loop muted playsinline poster="/mugicrew/recordings/hero-loop.jpg"';
  const mp4 = '/mugicrew/recordings/hero-loop.mp4';
  put(d, 'index.html', rest + '<div data-recording="hero-loop"><span>soon</span></div>' + tail);
  assert.ok(checkHero(d).includes('index.html: hero loop is not a video'));
  put(d, 'index.html', rest + video('loop muted playsinline poster="/mugicrew/recordings/hero-loop.jpg"', mp4) + tail);
  assert.ok(checkHero(d).includes('index.html: hero video lacks autoplay'));
  put(d, 'index.html', rest + video(good, mp4) + tail);
  assert.ok(checkHero(d).includes('index.html: built file recordings/hero-loop.mp4 is missing'));
  assert.ok(checkHero(d).includes('index.html: built file recordings/hero-loop.jpg is missing'));
  put(d, 'recordings/hero-loop.mp4');
  put(d, 'recordings/hero-loop.jpg');
  assert.deepEqual(checkHero(d), []);
});
