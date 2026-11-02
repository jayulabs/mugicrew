// beadKinds(glossaryMd) → [{ term, markdown }]: the bead-kind entries of
// GLOSSARY.md's "## Bead kinds" section, in file order, stripped for people.
// The strip rule is crewdocs/vocabulary_test.go's stripEntry (drop "(ADR NNNN, …)"
// parentheticals with their leading space; drop a History line with its
// continuation lines), plus whole "See ADR …" sentences. A residual ADR citation
// is an error naming its entry: reword GLOSSARY.md, never loosen the rule.

const SECTION = /^## Bead kinds\s*$/;
const ENTRY_HEAD = /^- \*\*([^*]+)\*\* — /;
const KIND_BULLET = /^\s+- (?:_|\*)(?:Owner|Structure):?(?:_|\*)/;
const HISTORY_BULLET = /^(\s*)(?:- )?(?:_|\*)History:?(?:_|\*)/;
const ADR_CITE = / \(ADR \d{4}(?:, ADR \d{4})*\)/g;
// A "See ADR …" sentence ends at a period followed by space or end of line (it may span lines), so "§1.2." is stripped whole.
const SEE_ADR = / *See ADR[\s\S]*?\.(?=\s|$)/g;
const RESIDUAL = /ADR\s+\d{4}/;
const EMPTY_BULLET = /^\s+- (?:_|\*)[A-Za-z]+:?(?:_|\*)\s*$/;

const indentOf = (l) => l.length - l.trimStart().length;

// Splits the section into "- **Term** — …" entries with their indented
// continuation lines (blank lines inside an entry are kept when more indented
// text follows).
function sectionEntries(md) {
  const lines = md.split('\n');
  const start = lines.findIndex((l) => SECTION.test(l));
  if (start < 0) throw new Error('GLOSSARY.md has no "## Bead kinds" section');
  const out = [];
  let cur = null;
  for (let i = start + 1; i < lines.length; i++) {
    const l = lines[i];
    if (/^## /.test(l)) break;
    const m = ENTRY_HEAD.exec(l);
    if (m) {
      cur = { term: m[1], lines: [l] };
      out.push(cur);
    } else if (cur && l.trim() === '') {
      cur.lines.push(l);
    } else if (cur && l.startsWith('  ')) {
      cur.lines.push(l);
    } else {
      cur = null;
    }
  }
  for (const e of out) while (e.lines.at(-1).trim() === '') e.lines.pop();
  return out;
}

function stripHistory(lines) {
  const out = [];
  let skipIndent = -1;
  for (const l of lines) {
    const h = HISTORY_BULLET.exec(l);
    if (h) {
      skipIndent = indentOf(l);
      continue;
    }
    if (skipIndent >= 0) {
      if (l.trim() === '') continue;
      if (indentOf(l) > skipIndent) continue;
      skipIndent = -1;
    }
    out.push(l);
  }
  return out;
}

export function stripEntry(term, lines) {
  let s = stripHistory(lines).join('\n');
  s = s.replace(ADR_CITE, '').replace(SEE_ADR, '');
  s = s
    .split('\n')
    .filter((l) => !EMPTY_BULLET.test(l))
    .join('\n')
    .replace(/[ \t]+$/gm, '')
    .replace(/\n{3,}/g, '\n\n');
  const m = RESIDUAL.exec(s);
  if (m) {
    const line = s.slice(0, m.index).split('\n').length - 1;
    throw new Error(
      `${term}: stripped entry still cites an ADR; reword GLOSSARY.md: ${s.split('\n')[line].trim()}`,
    );
  }
  return s;
}

export function beadKinds(md) {
  return sectionEntries(md)
    .filter((e) => e.lines.some((l) => KIND_BULLET.test(l)))
    .map((e) => ({ term: e.term, markdown: stripEntry(e.term, e.lines) }));
}

// GitHub-style heading slug, as Docusaurus derives anchors; guides deep-link
// a kind as /learn/bead-glossary#<slug>.
export const kindSlug = (term) =>
  term.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');

// beadGlossaryPage(glossaryMd) → the Markdown of the Learn bead glossary page:
// one heading per kind, anchored by its term, over the entry's description and
// sub-bullets (owner, structure, operations, notes).
export function beadGlossaryPage(md) {
  const sections = beadKinds(md).map(({ term, markdown }) => {
    const body = markdown
      .split('\n')
      .map((l, i) => (i === 0 ? l.replace(ENTRY_HEAD, '') : l.replace(/^ {2}/, '')))
      .join('\n')
      .replace(/\n(- )/, '\n\n$1');
    return `### ${term} {#${kindSlug(term)}}\n\n${body}\n`;
  });
  return [
    '---',
    'id: bead-glossary',
    'title: Bead glossary',
    '---',
    '',
    '<!-- Generated from GLOSSARY.md by scripts/gen-glossary.mjs; do not edit. -->',
    '',
    '# Bead glossary',
    '',
    'Every kind of bead you meet in the TUI: what it is, who owns it, whether it is a leaf or a container, what crewmates do with it, and any notes.',
    '',
    ...sections,
  ].join('\n');
}
