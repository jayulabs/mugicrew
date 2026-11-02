import path from 'node:path';

const REPO = 'https://github.com/jayulabs/mugicrew/blob/main/';

// A link is relative when it has no scheme, no authority and no leading slash
// or fragment-only form: those are left alone.
const isRelative = (url) =>
  url !== '' && !url.startsWith('#') && !url.startsWith('/') && !/^[a-z][a-z0-9+.-]*:/i.test(url);

/**
 * Rewrite `url` (found in the file at `file`, a path relative to `root`) to its
 * GitHub blob URL when it points outside `rendered`; send it to its site route
 * when it points at a rendered file. `rendered` maps each rendered file (relative
 * to `root`) to its site route. Throws when the target leaves `root` or does not exist.
 */
export function rewriteUrl(url, { file, root, rendered, exists }) {
  if (!isRelative(url)) return url;
  const m = /^([^?#]*)([?#].*)?$/.exec(url);
  const target = path.posix.normalize(path.posix.join(path.posix.dirname(file), m[1]));
  if (target.startsWith('..') || path.posix.isAbsolute(target)) {
    throw new Error(`${file}: link ${url} escapes the Application root`);
  }
  if (!exists(path.join(root, target))) {
    throw new Error(`${file}: link ${url} does not resolve to a file (${target})`);
  }
  if (Object.hasOwn(rendered, target)) return rendered[target] + (m[2] ?? '');
  return REPO + target + (m[2] ?? '');
}

/**
 * Site routes of the hand-written Learn guides: each `<dir>/<id>.mdx` in `files`
 * (the entries of `dir`, relative to the root) routes to `/learn/<id>`; `index` is `/learn`.
 */
export function learnGuides(dir, files) {
  const routes = {};
  for (const f of files) {
    if (!f.endsWith('.mdx')) continue;
    const id = f.slice(0, -'.mdx'.length);
    routes[`${dir}/${f}`] = id === 'index' ? '/learn' : `/learn/${id}`;
  }
  return routes;
}

function walk(node, fn) {
  fn(node);
  for (const c of node.children ?? []) walk(c, fn);
}

/**
 * remark plugin: options { root, rendered, exists, origins }. `origins` maps a
 * copied file (relative to `root`) back to the repo file it was copied from, so
 * its links resolve from where the author wrote them.
 */
export default function remarkRewriteLinks({ root, rendered, exists, origins = {} }) {
  return (tree, vfile) => {
    const copy = path.relative(root, vfile.path).split(path.sep).join('/');
    const file = origins[copy] ?? copy;
    walk(tree, (n) => {
      if ((n.type === 'link' || n.type === 'definition') && n.url) {
        n.url = rewriteUrl(n.url, { file, root, rendered, exists });
      }
    });
  };
}
