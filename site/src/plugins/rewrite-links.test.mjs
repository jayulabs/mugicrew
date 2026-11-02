import test from 'node:test';
import path from 'node:path';
import assert from 'node:assert/strict';
import remarkRewriteLinks, { rewriteUrl, learnGuides } from './rewrite-links.mjs';

const ctx = {
  file: 'docs/install.md',
  root: '/app',
  rendered: {
    'docs/install.md': '/try/install',
    'docs/configuration.md': '/learn/customization/configuration',
    'docs/gitlab.md': '/learn/customization/gitlab',
    'docs/jira.md': '/learn/customization/jira',
  },
  exists: (p) =>
    ['/app/README.md', '/app/docs/install.md', '/app/docs/configuration.md', '/app/docs/gitlab.md', '/app/docs/jira.md', '/app/docs/release.md'].includes(p),
};

test('outside-set relative link becomes a GitHub blob URL on main', () => {
  assert.equal(rewriteUrl('../README.md', ctx), 'https://github.com/jayulabs/mugicrew/blob/main/README.md');
  assert.equal(
    rewriteUrl('release.md#cut', ctx),
    'https://github.com/jayulabs/mugicrew/blob/main/docs/release.md#cut',
  );
});

test('rendered-to-rendered links become site links across all four docs', () => {
  const docs = Object.keys(ctx.rendered);
  for (const from of docs)
    for (const to of docs) {
      const url = path.posix.relative(path.posix.dirname(from), to);
      assert.equal(rewriteUrl(url, { ...ctx, file: from }), ctx.rendered[to]);
    }
  assert.equal(rewriteUrl('gitlab.md#setup', ctx), '/learn/customization/gitlab#setup');
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

test('a copied Learn page resolves its links from its origin file', () => {
  const tree = { type: 'root', children: [{ type: 'link', url: 'gitlab.md', children: [] }, { type: 'link', url: '../README.md', children: [] }] };
  remarkRewriteLinks({
    root: '/app',
    rendered: ctx.rendered,
    exists: ctx.exists,
    origins: { 'site/learn/customization/configuration.md': 'docs/configuration.md' },
  })(tree, { path: '/app/site/learn/customization/configuration.md' });
  assert.equal(tree.children[0].url, '/learn/customization/gitlab');
  assert.equal(tree.children[1].url, 'https://github.com/jayulabs/mugicrew/blob/main/README.md');
});

test('a rendered doc linking a hand-written Learn guide lands on its Learn route', () => {
  const rendered = { ...ctx.rendered, ...learnGuides('site/learn', ['organizing-with-initiatives.mdx', 'index.mdx', 'bead-glossary.md']) };
  assert.deepEqual(Object.keys(rendered).filter((k) => k.startsWith('site/')), ['site/learn/organizing-with-initiatives.mdx', 'site/learn/index.mdx']);
  const exists = (p) => p === '/app/site/learn/organizing-with-initiatives.mdx';
  assert.equal(
    rewriteUrl('../site/learn/organizing-with-initiatives.mdx', { file: 'docs/jira.md', root: '/app', rendered, exists }),
    '/learn/organizing-with-initiatives',
  );
  assert.equal(rendered['site/learn/index.mdx'], '/learn');
});
