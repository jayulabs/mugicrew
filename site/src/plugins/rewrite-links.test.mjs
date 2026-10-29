import test from 'node:test';
import assert from 'node:assert/strict';
import remarkRewriteLinks, { rewriteUrl } from './rewrite-links.mjs';

const ctx = {
  file: 'docs/install.md',
  root: '/app',
  rendered: ['docs/install.md'],
  exists: (p) => ['/app/README.md', '/app/docs/install.md', '/app/docs/release.md'].includes(p),
};

test('outside-set relative link becomes a GitHub blob URL on main', () => {
  assert.equal(rewriteUrl('../README.md', ctx), 'https://github.com/jayulabs/mugicrew/blob/main/README.md');
  assert.equal(
    rewriteUrl('release.md#cut', ctx),
    'https://github.com/jayulabs/mugicrew/blob/main/docs/release.md#cut',
  );
});

test('on-set link stays on-site', () => {
  assert.equal(rewriteUrl('./install.md', ctx), './install.md');
});

test('absolute URLs, root paths and fragments are untouched', () => {
  for (const u of ['https://example.com/x', 'mailto:a@b.c', '/try', '#top'])
    assert.equal(rewriteUrl(u, ctx), u);
});

test('unresolvable paths throw', () => {
  assert.throws(() => rewriteUrl('missing.md', ctx), /does not resolve/);
  assert.throws(() => rewriteUrl('../../outside.md', ctx), /escapes/);
});

test('the remark plugin rewrites link nodes in a tree', () => {
  const tree = { type: 'root', children: [{ type: 'paragraph', children: [{ type: 'link', url: '../README.md', children: [] }] }] };
  remarkRewriteLinks({ root: '/app', rendered: ctx.rendered, exists: ctx.exists })(tree, { path: '/app/docs/install.md' });
  assert.equal(tree.children[0].children[0].url, 'https://github.com/jayulabs/mugicrew/blob/main/README.md');
});

test('the remark plugin leaves image nodes alone', () => {
  const tree = { type: 'root', children: [{ type: 'image', url: '../README.md', alt: '' }] };
  remarkRewriteLinks({ root: '/app', rendered: ctx.rendered, exists: ctx.exists })(tree, { path: '/app/docs/install.md' });
  assert.equal(tree.children[0].url, '../README.md');
});
