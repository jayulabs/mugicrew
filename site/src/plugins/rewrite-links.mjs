import path from 'node:path';

const REPO = 'https://github.com/jayulabs/mugicrew/blob/main/';

// A link is relative when it has no scheme, no authority and no leading slash
// or fragment-only form: those are left alone.
const isRelative = (url) =>
  url !== '' && !url.startsWith('#') && !url.startsWith('/') && !/^[a-z][a-z0-9+.-]*:/i.test(url);

/**
 * Rewrite `url` (found in the file at `file`, a path relative to `root`) to its
 * GitHub blob URL when it points outside `rendered`; keep it when it points at
 * a rendered file. Throws when the target leaves `root` or does not exist.
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
  if (rendered.includes(target)) return url;
  return REPO + target + (m[2] ?? '');
}

function walk(node, fn) {
  fn(node);
  for (const c of node.children ?? []) walk(c, fn);
}

/** remark plugin: options { root, rendered, exists } */
export default function remarkRewriteLinks({ root, rendered, exists }) {
  return (tree, vfile) => {
    const file = path.relative(root, vfile.path).split(path.sep).join('/');
    walk(tree, (n) => {
      if ((n.type === 'link' || n.type === 'definition') && n.url) {
        n.url = rewriteUrl(n.url, { file, root, rendered, exists });
      }
    });
  };
}
