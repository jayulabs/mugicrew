import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import remarkRewriteLinks from './src/plugins/rewrite-links.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const appRoot = path.resolve(here, '..');

// The only Markdown the site renders (ADR 0366), relative to the Application root.
const rendered = ['docs/install.md'];

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
  presets: [
    [
      'classic',
      {
        docs: {
          path: '../docs',
          include: ['install.md'],
          routeBasePath: 'try',
          sidebarPath: false,
          remarkPlugins: [[remarkRewriteLinks, { root: appRoot, rendered, exists: fs.existsSync }]],
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
