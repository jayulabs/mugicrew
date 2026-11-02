import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import remarkRewriteLinks, { learnGuides } from './src/plugins/rewrite-links.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const appRoot = path.resolve(here, '..');

// The only Markdown the site renders (ADR 0366), relative to the Application root.
// Docs also shown as Learn pages: repo file -> Learn doc id. The docs plugin keys a file by its
// real path, so Learn gets a gitignored byte-for-byte copy, refreshed on every config load.
const learnPages = {
  'docs/configuration.md': 'customization/configuration',
  'docs/gitlab.md': 'customization/gitlab',
  'docs/jira.md': 'customization/jira',
};
const learnDir = 'site/learn';
const origins = {};
const rendered = {
  'docs/install.md': '/try/install',
  ...learnGuides(learnDir, fs.readdirSync(path.join(appRoot, learnDir))),
};
// Build-time generators: this loop copies each rendered doc into site/learn/customization as a side
// effect of loading the config; scripts/gen-glossary.mjs writes learn/bead-glossary.md. Both outputs are git-ignored.
for (const [src, id] of Object.entries(learnPages)) {
  const dest = path.join(appRoot, learnDir, `${id}.md`);
  fs.mkdirSync(path.dirname(dest), { recursive: true });
  fs.copyFileSync(path.join(appRoot, src), dest);
  origins[`${learnDir}/${id}.md`] = src;
  rendered[src] = `/learn/${id}`;
}

/** @type {import('@docusaurus/types').Config} */
export default {
  title: 'mugicrew',
  tagline: 'Chart the fog before you build.',
  url: 'https://jayulabs.github.io',
  baseUrl: '/mugicrew/',
  trailingSlash: false,
  favicon: 'img/compass-rose.svg',
  onBrokenLinks: 'throw',
  onBrokenAnchors: 'throw',
  onDuplicateRoutes: 'throw',
  markdown: {
    format: 'detect',
    hooks: { onBrokenMarkdownLinks: 'throw', onBrokenMarkdownImages: 'throw' },
  },
  plugins: [
    [
      '@docusaurus/plugin-content-docs',
      {
        id: 'learn',
        path: './learn',
        routeBasePath: 'learn',
        sidebarPath: './learn-sidebars.js',
        // Before the defaults: Docusaurus's own link resolver would otherwise reject links to docs outside Learn.
        beforeDefaultRemarkPlugins: [[remarkRewriteLinks, { root: appRoot, rendered, exists: fs.existsSync, origins }]],
      },
    ],
  ],
  presets: [
    [
      'classic',
      {
        docs: {
          path: '../docs',
          include: ['install.md'],
          routeBasePath: 'try',
          sidebarPath: false,
          // Before the defaults, as for Learn: docs rendered into Learn must resolve to their Learn routes first.
          beforeDefaultRemarkPlugins: [[remarkRewriteLinks, { root: appRoot, rendered, exists: fs.existsSync }]],
        },
        blog: false,
        theme: { customCss: './src/css/custom.css' },
      },
    ],
  ],
  themeConfig: {
    colorMode: { defaultMode: 'light', disableSwitch: true, respectPrefersColorScheme: false },
    navbar: {
      title: 'mugicrew',
      logo: { alt: 'mugicrew compass rose', src: 'img/compass-rose.svg' },
      items: [
        { to: '/workspace', label: 'Workspace', position: 'left' },
        { to: '/framework', label: 'Framework', position: 'left' },
        { to: '/game', label: 'Game', position: 'left' },
        { to: '/try', label: 'Try it', position: 'left' },
        { to: '/learn', label: 'Learn', position: 'left' },
        { href: 'https://github.com/jayulabs/mugicrew', label: 'GitHub', position: 'right' },
        { href: 'https://www.npmjs.com/package/@jayulabs/mugicrew', label: 'npm', position: 'right' },
      ],
    },
    footer: {
      links: [
        {
          title: 'mugicrew',
          items: [
            { label: 'GitHub', href: 'https://github.com/jayulabs/mugicrew' },
            { label: 'npm', href: 'https://www.npmjs.com/package/@jayulabs/mugicrew' },
          ],
        },
      ],
    },
  },
};
